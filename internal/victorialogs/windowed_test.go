package victorialogs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestPrepareWindowedQuery(t *testing.T) {
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: "http://localhost:9428", Optimizer: &models.VictoriaLogsOptimizer{Enabled: true}})
	start := time.Date(2026, 9, 1, 0, 0, 0, 123456789, time.UTC)
	end := start.Add(7 * time.Hour)
	req := datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &start, EndTime: &end, Limit: 3}}
	plan, err := provider.PrepareWindowedQuery(source, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Windows) != 3 || !plan.Windows[0].End.Equal(end) || !plan.Windows[2].Start.Equal(start) {
		t.Fatalf("unexpected windows: %+v", plan.Windows)
	}
	for i := 1; i < len(plan.Windows); i++ {
		if !plan.Windows[i].End.Equal(plan.Windows[i-1].Start) {
			t.Fatal("gap between windows")
		}
	}
	req.Cursor = searchCursor(plan, 1, 2)
	continued, err := provider.PrepareWindowedQuery(source, req)
	if err != nil || continued.Index != 1 || continued.Offset != 2 {
		t.Fatalf("cursor: %+v, %v", continued, err)
	}
	req.RawQuery = "level:error"
	if _, err := provider.PrepareWindowedQuery(source, req); err == nil {
		t.Fatal("accepted cursor for different query")
	}
	req.Cursor = ""
	req.RawQuery = "* | stats count()"
	if _, err := provider.PrepareWindowedQuery(source, req); !errors.Is(err, datasource.ErrOperationNotSupported) {
		t.Fatalf("aggregate should use standard path: %v", err)
	}
}

func TestIntegrationWindowedSearch(t *testing.T) {
	baseURL := integrationBaseURL(t)
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	runID := newTestRunID(t)
	base := time.Now().UTC().Truncate(time.Second).Add(-5 * time.Minute)
	rows := []fixtureRow{
		{msg: "old", service: "a", offset: 0},
		{msg: "boundary", service: "a", offset: time.Minute},
		{msg: "same", service: "b", durationMs: 1, offset: 2 * time.Minute},
		{msg: "same", service: "b", durationMs: 2, offset: 2 * time.Minute},
		{msg: "same", service: "b", durationMs: 3, offset: 2 * time.Minute},
		{msg: "same", service: "b", durationMs: 4, offset: 2 * time.Minute},
		{msg: "new", service: "a", offset: 3 * time.Minute},
	}
	ingestFixtures(t, baseURL, runID, base, rows)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: "test_run:=" + runID}, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 60, Concurrency: 2}})
	end := base.Add(4 * time.Minute)
	waitForFixtures(t, provider, source, queryWindow{start: base, end: end}, len(rows))
	req := datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &base, EndTime: &end, Limit: 2, MaxResponseBytes: 1 << 20}}
	var all []map[string]any
	for range 10 {
		plan, err := provider.PrepareWindowedQuery(source, req)
		if err != nil {
			t.Fatal(err)
		}
		cursor := ""
		count := 0
		if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error {
			if event.Status == "failed" {
				t.Fatalf("window failed: %+v", event)
			}
			all = append(all, event.Logs...)
			count += len(event.Logs)
			if event.Type == "end" {
				cursor = event.Cursor
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if count > 2 {
			t.Fatal("page limit exceeded")
		}
		if cursor == "" {
			break
		}
		req.Cursor = cursor
	}
	if len(all) != len(rows) {
		t.Fatalf("got %d rows, want %d: %+v", len(all), len(rows), all)
	}
	seen := make(map[string]bool)
	for i, row := range all {
		key := row["_msg"].(string) + ":" + row["duration_ms"].(string)
		if seen[key] {
			t.Fatalf("duplicate row: %s", key)
		}
		seen[key] = true
		if i > 0 && all[i-1]["_time"].(string) < row["_time"].(string) {
			t.Fatal("rows not newest first")
		}
	}
	req.Cursor = ""
	req.Count, req.Step = true, "1m"
	plan, err := provider.PrepareWindowedQuery(source, req)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error {
		if event.Status == "failed" {
			t.Fatalf("count failed: %+v", event)
		}
		for _, bucket := range event.Buckets {
			total += bucket.LogCount
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if total != len(rows) {
		t.Fatalf("count=%d, rows=%d", total, len(rows))
	}
	values, err := provider.GetFieldValues(context.Background(), source, datasource.FieldValuesRequest{FieldName: "service", StartTime: base, EndTime: end, Limit: 1, QueryText: "*", Language: models.QueryLanguageLogsQL})
	if err != nil {
		t.Fatal(err)
	}
	if values.TotalDistinct != 2 || !reflect.DeepEqual(values.Values, []datasource.FieldValueInfo{{Value: "b", Count: 4}}) {
		t.Fatalf("incorrect values: %+v", values)
	}
}

func TestWindowedFailureRetryAndSkip(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Minute)
	var fail atomic.Bool
	fail.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		timestamp := r.Form.Get("start")
		if timestamp == formatAPITime(end.Add(-time.Minute)) && fail.Load() {
			http.Error(w, "upstream failure", http.StatusInternalServerError)
			return
		}
		row := map[string]string{"_time": timestamp, "_msg": timestamp}
		packed, err := json.Marshal(row)
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]string{"_logchef_row": string(packed)}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 60, Concurrency: 2}})
	req := datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &start, EndTime: &end, Limit: 10}}
	run := func(request datasource.WindowedRequest) []datasource.WindowedEvent {
		t.Helper()
		plan, err := provider.PrepareWindowedQuery(source, request)
		if err != nil {
			t.Fatal(err)
		}
		var events []datasource.WindowedEvent
		if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error { events = append(events, event); return nil }); err != nil {
			t.Fatal(err)
		}
		return events
	}
	events := run(req)
	last := events[len(events)-1]
	if last.Cursor == "" || last.Complete {
		t.Fatalf("failed window lost continuation: %+v", events)
	}
	for _, event := range events {
		if len(event.Logs) > 0 {
			t.Fatal("older rows passed a failed newer window")
		}
	}
	fail.Store(false)
	req.Cursor = last.Cursor
	index := 0
	req.WindowIndex = &index
	retried := run(req)
	returned := 0
	for _, event := range retried {
		returned += len(event.Logs)
		if len(event.Logs) > 0 && (event.Window.Index != 0 || event.Cursor == "") {
			t.Fatal("retry did not retain continuation")
		}
	}
	if returned != 1 {
		t.Fatalf("retry returned %d rows", returned)
	}
	req.WindowIndex = nil
	req.SkipWindow = &index
	skipped := run(req)
	var indices []int
	for _, event := range skipped {
		if event.Type == "rows" {
			indices = append(indices, event.Window.Index)
		}
	}
	if !reflect.DeepEqual(indices, []int{1, 2}) {
		t.Fatalf("skip returned windows %v", indices)
	}
}

func TestWindowedSourceConcurrencyAndCancellation(t *testing.T) {
	var active, maximum atomic.Int32
	started := make(chan struct{}, 20)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(r.Form.Get("query"), "stats") {
			_, _ = fmt.Fprintln(w, `{"_time":"2026-09-01T00:00:00Z","log_count":"1"}`)
		}
	}))
	defer server.Close()
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 60, Concurrency: 2}})
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	failures := make(chan error, 2)
	for _, count := range []bool{false, true} {
		plan, err := provider.PrepareWindowedQuery(source, datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &start, EndTime: &end, Limit: 2}, Count: count, Step: "1m"})
		if err != nil {
			t.Fatal(err)
		}
		workers.Go(func() {
			failures <- provider.StreamWindowedQuery(ctx, source, plan, func(datasource.WindowedEvent) error { return nil })
		})
	}
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("parallel windows did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("shared source concurrency exceeded")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maximum.Load() != 2 {
		t.Fatalf("peak concurrency=%d", maximum.Load())
	}
	limiter := provider.windowLimiter(source.ID, 2)
	if limiter.active != 0 {
		t.Fatal("source slots leaked")
	}
}

func TestWindowedEarlyStopCancelsOlderWindow(t *testing.T) {
	olderStarted := make(chan struct{})
	olderCancelled := make(chan struct{})
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.Form.Get("start") == formatAPITime(start) {
			close(olderStarted)
			<-r.Context().Done()
			close(olderCancelled)
			return
		}
		<-olderStarted
		for _, message := range []string{"newest", "next", "lookahead"} {
			packed, _ := json.Marshal(map[string]string{"_time": formatAPITime(end.Add(-time.Second)), "_msg": message})
			_ = json.NewEncoder(w).Encode(map[string]string{"_logchef_row": string(packed)})
		}
	}))
	defer server.Close()
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 60, Concurrency: 2}})
	plan, err := provider.PrepareWindowedQuery(source, datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &start, EndTime: &end, Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := provider.StreamWindowedQuery(ctx, source, plan, func(event datasource.WindowedEvent) error { rows += len(event.Logs); return nil }); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("page returned %d rows", rows)
	}
	select {
	case <-olderCancelled:
	case <-ctx.Done():
		t.Fatal("early stop did not cancel older window")
	}
}

func TestWindowedPageByteBudget(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	original, err := json.Marshal(map[string]string{"_time": formatAPITime(start), "_msg": strings.Repeat("x", 80)})
	if err != nil {
		t.Fatal(err)
	}
	packed, err := json.Marshal(map[string]string{"_logchef_row": string(original)})
	if err != nil {
		t.Fatal(err)
	}
	packed = append(packed, '\n')
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(packed) }))
	defer server.Close()
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 60, Concurrency: 2}})
	req := datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &start, EndTime: &end, Limit: 10, MaxResponseBytes: len(packed)}}
	for page := range 2 {
		plan, err := provider.PrepareWindowedQuery(source, req)
		if err != nil {
			t.Fatal(err)
		}
		rows := 0
		cursor := ""
		if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error {
			rows += len(event.Logs)
			if event.Type == "end" {
				cursor = event.Cursor
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if rows != 1 {
			t.Fatalf("page %d returned %d rows", page, rows)
		}
		if (cursor == "") != (page == 1) {
			t.Fatal("incorrect byte-budget continuation")
		}
		req.Cursor = cursor
	}
}

func TestIntegrationOptimizedStreamValues(t *testing.T) {
	baseURL := integrationBaseURL(t)
	runID := newTestRunID(t)
	end := time.Now().UTC()
	start := end.Add(-10 * time.Minute)
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for i := range 125 {
		value := fmt.Sprintf("namespace-%03d", i)
		if i >= 120 {
			value = "z-hot"
		}
		if err := encoder.Encode(map[string]string{"_time": formatAPITime(end.Add(-time.Minute)), "_msg": "stream fixture", "kubernetes.namespace": value, "hits": "real field", "test_run": runID}); err != nil {
			t.Fatal(err)
		}
	}
	response, err := http.Post(strings.TrimRight(baseURL, "/")+"/insert/jsonline?_time_field=_time&_msg_field=_msg&_stream_fields=kubernetes.namespace", "application/stream+json", &body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ingest status %d", response.StatusCode)
	}
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: "test_run:=" + runID}, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, SidebarValuesCap: 100}})
	waitForFixtures(t, provider, source, queryWindow{start: start, end: end}, 100)
	fields, err := provider.StreamFields(context.Background(), source)
	if err != nil || !reflect.DeepEqual(fields, []string{"kubernetes.namespace"}) {
		t.Fatalf("stream fields %v: %v", fields, err)
	}
	values, err := provider.GetFieldValues(context.Background(), source, datasource.FieldValuesRequest{FieldName: "kubernetes.namespace", StartTime: start, EndTime: end, Limit: 3, QueryText: "*", Language: models.QueryLanguageLogsQL})
	if err != nil {
		t.Fatal(err)
	}
	if values.TotalDistinct != 121 || len(values.Values) != 3 || values.Values[0].Value != "z-hot" || values.Values[0].Count != 5 {
		t.Fatalf("cap lost real frequencies or distinct count: %+v", values)
	}
	hits, err := provider.GetFieldValues(context.Background(), source, datasource.FieldValuesRequest{FieldName: "hits", StartTime: start, EndTime: end, Limit: 3, QueryText: "*", Language: models.QueryLanguageLogsQL})
	if err != nil || len(hits.Values) != 1 || hits.Values[0].Value != "real field" || hits.Values[0].Count != 125 {
		t.Fatalf("field alias collision: %+v, %v", hits, err)
	}
	filter, err := json.Marshal(map[string][]string{"kubernetes.namespace": {"z-hot"}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := provider.PrepareWindowedQuery(source, datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &start, EndTime: &end, Limit: 10}, StreamFilters: []string{string(filter)}})
	if err != nil {
		t.Fatal(err)
	}
	matched := 0
	if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error { matched += len(event.Logs); return nil }); err != nil {
		t.Fatal(err)
	}
	if matched != 5 {
		t.Fatalf("JSON stream filter matched %d rows", matched)
	}
}

func TestWindowedOversizedHeadRowCanBeSkipped(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		message := "older"
		if r.Form.Get("start") == formatAPITime(end.Add(-time.Minute)) {
			message = strings.Repeat("x", 1000)
		}
		packed, err := json.Marshal(map[string]string{"_time": r.Form.Get("start"), "_msg": message})
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]string{"_logchef_row": string(packed)}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 60, Concurrency: 2}})
	req := datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &start, EndTime: &end, Limit: 10, MaxResponseBytes: 300}}
	run := func(request datasource.WindowedRequest) []datasource.WindowedEvent {
		t.Helper()
		plan, err := provider.PrepareWindowedQuery(source, request)
		if err != nil {
			t.Fatal(err)
		}
		var events []datasource.WindowedEvent
		if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error { events = append(events, event); return nil }); err != nil {
			t.Fatal(err)
		}
		return events
	}
	events := run(req)
	failed := events[len(events)-2]
	last := events[len(events)-1]
	if failed.Status != datasource.WindowFailed || failed.Window.Index != 0 || !strings.Contains(failed.Message, "size limit") || failed.Cursor != last.Cursor {
		t.Fatalf("oversized row did not fail the head window: %+v", events)
	}
	if last.Type != datasource.WindowEnd || last.Cursor == "" || last.Complete || last.Message != "" {
		t.Fatalf("oversized row lost continuation: %+v", last)
	}
	req.Cursor = last.Cursor
	index := 0
	req.SkipWindow = &index
	var messages []any
	skipped := run(req)
	for _, event := range skipped {
		for _, row := range event.Logs {
			messages = append(messages, row["_msg"])
		}
	}
	if skipped[1].Status != datasource.WindowSkipped || skipped[1].Window.Index != 0 || !reflect.DeepEqual(messages, []any{"older"}) || !skipped[len(skipped)-1].Complete {
		t.Fatalf("skip after oversized row: %+v", skipped)
	}
}

func TestIntegrationWindowedCountsUseTimezone(t *testing.T) {
	baseURL := integrationBaseURL(t)
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	runID := newTestRunID(t)
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(kolkata)
	midnight := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, kolkata)
	base := midnight.Add(-3 * time.Hour).UTC()
	// 22:30 IST on the previous day, 00:30 IST and 05:00 IST. All three share one UTC day.
	rows := []fixtureRow{{msg: "previous day", offset: 90 * time.Minute}, {msg: "after midnight", offset: 210 * time.Minute}, {msg: "morning", offset: 8 * time.Hour}}
	ingestFixtures(t, baseURL, runID, base, rows)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: "test_run:=" + runID}, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 3600, Concurrency: 2}})
	end := base.Add(9 * time.Hour)
	waitForFixtures(t, provider, source, queryWindow{start: base, end: end}, len(rows))
	for _, timezone := range []string{"Asia/Kolkata", "UTC"} {
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []string{"1h", "24h"} {
			// The oracle buckets by local calendar hour or day, independent of offsets.
			want := make(map[string]int)
			for _, row := range rows {
				local := base.Add(row.offset).In(loc)
				hour := local.Hour()
				if step == "24h" {
					hour = 0
				}
				want[time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, loc).UTC().Format(time.RFC3339)]++
			}
			plan, err := provider.PrepareWindowedQuery(source, datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &base, EndTime: &end, Timezone: timezone}, Count: true, Step: step})
			if err != nil {
				t.Fatal(err)
			}
			got := make(map[string]int)
			if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error {
				if event.Status == datasource.WindowFailed {
					t.Fatalf("count failed: %+v", event)
				}
				for _, bucket := range event.Buckets {
					got[bucket.Bucket.UTC().Format(time.RFC3339)] += bucket.LogCount
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s %s buckets = %v, want %v", timezone, step, got, want)
			}
		}
	}
}
