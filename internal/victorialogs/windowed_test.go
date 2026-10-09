package victorialogs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
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
	position := datasource.WindowPosition{Time: plan.Windows[1].Start.Add(time.Nanosecond), Skip: 2}
	req.Cursor = searchCursor(plan, 1, position)
	continued, err := provider.PrepareWindowedQuery(source, req)
	if err != nil || continued.Index != 1 || !continued.Position.Time.Equal(position.Time) || continued.Position.Skip != 2 {
		t.Fatalf("cursor: %+v, %v", continued, err)
	}
	for _, outside := range []time.Time{plan.Windows[1].End, plan.Windows[1].Start.Add(-time.Nanosecond)} {
		req.Cursor = searchCursor(plan, 1, datasource.WindowPosition{Time: outside, Skip: 1})
		if _, err := provider.PrepareWindowedQuery(source, req); err == nil {
			t.Fatalf("accepted cursor position %s outside its window", outside)
		}
	}
	// A cursor from the offset-based scheme carries the old fingerprint.
	legacy, err := json.Marshal(struct {
		SourceID models.SourceID `json:"source_id"`
		Revision time.Time       `json:"revision"`
		Query    string          `json:"query"`
		Start    time.Time       `json:"start"`
		End      time.Time       `json:"end"`
		Width    int             `json:"width"`
		Filters  []string        `json:"filters"`
	}{source.ID, source.UpdatedAt, req.RawQuery, start, end, 10800, nil})
	if err != nil {
		t.Fatal(err)
	}
	legacyHash := sha256.Sum256(legacy)
	req.Cursor = base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"fingerprint":%q,"index":1,"offset":2}`, hex.EncodeToString(legacyHash[:])))
	if _, err := provider.PrepareWindowedQuery(source, req); err == nil {
		t.Fatal("accepted an offset-based cursor")
	}
	req.Cursor = searchCursor(plan, 1, position)
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
		if isBoundaryQuery(r) {
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
		if isBoundaryQuery(r) {
			return
		}
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if !isBoundaryQuery(r) {
			_, _ = w.Write(packed)
		}
	}))
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
		if isBoundaryQuery(r) {
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

// isBoundaryQuery reports whether r is the time-only query that bounds a page.
// The fake servers that use it hold fewer rows than a page, so VictoriaLogs
// would return no boundary row.
func isBoundaryQuery(r *http.Request) bool {
	return !strings.Contains(r.Form.Get("query"), "pack_json")
}

func TestIntegrationWindowedTimestampTies(t *testing.T) {
	baseURL := integrationBaseURL(t)
	runID := newTestRunID(t)
	base := time.Now().UTC().Truncate(time.Second).Add(-5 * time.Minute)
	boundary := base.Add(2 * time.Minute)
	tie := boundary.Add(30*time.Second + 123456789)
	older := base.Add(90 * time.Second)
	type fixture struct {
		at  time.Time
		msg string
	}
	fixtures := []fixture{
		{boundary, "edge-new-a"}, {boundary, "edge-new-b"}, {boundary.Add(-time.Nanosecond), "edge-old"},
		{tie.Add(time.Nanosecond), "after-tie"}, {tie.Add(-time.Nanosecond), "before-tie"},
		{tie, "dup"}, {tie, "dup"}, {tie, "dup"},
		{older, "older-dup"}, {older, "older-dup"}, {older, "older-dup"}, {older, "older-dup"}, {older, "older-unique"},
		{base, "oldest"},
	}
	for i := range 6 {
		fixtures = append(fixtures, fixture{tie, fmt.Sprintf("tie-%d", i)})
	}
	want := make(map[string]int)
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, row := range fixtures {
		want[row.msg]++
		// All rows share one stream, so only the row content breaks ties.
		if err := encoder.Encode(map[string]string{"_time": row.at.Format(time.RFC3339Nano), "_msg": row.msg, "test_run": runID}); err != nil {
			t.Fatal(err)
		}
	}
	response, err := http.Post(strings.TrimRight(baseURL, "/")+"/insert/jsonline?_time_field=_time&_msg_field=_msg", "application/stream+json", &body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ingest status %d", response.StatusCode)
	}
	// Record the packed-row queries that reach the real VictoriaLogs instance.
	upstream, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var mu sync.Mutex
	var packedStarts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		if form, err := url.ParseQuery(string(data)); err == nil && strings.Contains(form.Get("query"), "pack_json") {
			mu.Lock()
			packedStarts = append(packedStarts, form.Get("start"))
			mu.Unlock()
		}
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL, Scope: models.VictoriaLogsScope{Query: "test_run:=" + runID}, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 60, Concurrency: 1}})
	end := base.Add(3 * time.Minute)
	waitForFixtures(t, provider, source, queryWindow{start: base, end: end}, len(fixtures))

	type page struct {
		logs   []map[string]any
		cursor string
		window int
	}
	run := func(req datasource.WindowedRequest) page {
		t.Helper()
		plan, err := provider.PrepareWindowedQuery(source, req)
		if err != nil {
			t.Fatal(err)
		}
		result := page{window: -1}
		if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error {
			if event.Status == datasource.WindowFailed {
				t.Fatalf("window failed: %+v", event)
			}
			if event.Type == datasource.WindowRows && result.window < 0 {
				result.window = event.Window.Index
			}
			result.logs = append(result.logs, event.Logs...)
			if event.Type == datasource.WindowEnd {
				result.cursor = event.Cursor
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return result
	}
	collect := func(limit, maxBytes int) []map[string]any {
		t.Helper()
		req := datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &base, EndTime: &end, Limit: limit, MaxResponseBytes: maxBytes}}
		var all []map[string]any
		for range 50 {
			first := run(req)
			if req.Cursor != "" && first.window >= 0 {
				// Retrying the same cursor for its window returns the same rows.
				retryReq := req
				retryReq.WindowIndex = &first.window
				retry := run(retryReq)
				var sameWindow []map[string]any
				for _, row := range first.logs {
					if rowWindow(t, row, base) == first.window {
						sameWindow = append(sameWindow, row)
					}
				}
				if !reflect.DeepEqual(retry.logs, sameWindow) {
					t.Fatalf("retry returned %v, want %v", retry.logs, sameWindow)
				}
			}
			if maxBytes < 1<<20 && len(first.logs) > 2 {
				t.Fatalf("byte budget allowed %d rows", len(first.logs))
			}
			all = append(all, first.logs...)
			if first.cursor == "" {
				return all
			}
			req.Cursor = first.cursor
		}
		t.Fatal("pagination did not finish")
		return nil
	}
	check := func(name string, rows []map[string]any, want map[string]int) {
		t.Helper()
		got := make(map[string]int)
		for i, row := range rows {
			got[row["_msg"].(string)]++
			if i > 0 && rowTimeForTest(t, rows[i-1]).Before(rowTimeForTest(t, row)) {
				t.Fatalf("%s: rows not newest first at %d", name, i)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}

	all := collect(4, 1<<20)
	check("row limit", all, want)
	mu.Lock()
	firstPacked := packedStarts[0]
	mu.Unlock()
	if firstPacked != formatAPITime(tie) {
		t.Fatalf("first page packed rows from %s, want the range from the fifth newest row at %s", firstPacked, formatAPITime(tie))
	}
	maxRow := 0
	for _, row := range all {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		maxRow = max(maxRow, len(data)+1)
	}
	for _, limit := range []int{3, 5} {
		check(fmt.Sprintf("byte limit %d", limit), collect(limit, 2*maxRow+maxRow/2), want)
	}

	// Skip the newest window from a cursor inside its timestamp tie.
	req := datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &base, EndTime: &end, Limit: 4}}
	req.Cursor = run(req).cursor
	skip := 0
	req.SkipWindow = &skip
	skipped := run(req)
	for skipped.cursor != "" {
		req.SkipWindow = nil
		req.Cursor = skipped.cursor
		next := run(req)
		skipped.logs = append(skipped.logs, next.logs...)
		skipped.cursor = next.cursor
	}
	check("skip", skipped.logs, map[string]int{"edge-old": 1, "older-dup": 4, "older-unique": 1, "oldest": 1})
}

// TestIntegrationWindowedShortBoundaryPage covers a bug in the VictoriaLogs
// v1.51-v1.52 last-N optimization. After the binary search extends its range
// downward, it counts rows at the split timestamp twice, so the boundary query
// returns a timestamp that is too new. Rows at exact split points trigger it:
// m1 is the midpoint of the older window, and m2 is the midpoint of the range
// before the tie, which a page resumed inside the tie searches.
func TestIntegrationWindowedShortBoundaryPage(t *testing.T) {
	baseURL := strings.TrimRight(integrationBaseURL(t), "/")
	runID := newTestRunID(t)
	base := time.Now().UTC().Truncate(10 * time.Second).Add(-15 * time.Minute)
	newerStart := base.Add(time.Minute)
	end := newerStart.Add(time.Minute)
	m1 := base.Add(30 * time.Second)
	m2 := newerStart.Add(20 * time.Second)
	tie := newerStart.Add(40 * time.Second)
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	want := make(map[string]int)
	total := 0
	add := func(at time.Time, msgs ...string) {
		for _, msg := range msgs {
			want[msg]++
			total++
			if err := encoder.Encode(map[string]string{"_time": at.Format(time.RFC3339Nano), "_msg": msg, "test_run": runID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	add(tie, "tie-dup", "tie-dup", "tie-dup", "tie-0", "tie-1")
	add(m2, "m2-dup", "m2-dup", "m2-0")
	add(m1, "m1-dup", "m1-dup", "m1-0")
	for _, split := range []struct {
		at  time.Time
		msg string
	}{{m2, "m2-older"}, {m1, "m1-older"}} {
		for i := range 8 {
			add(split.at.Add(-time.Second+time.Duration(i)*time.Millisecond), fmt.Sprintf("%s-%d", split.msg, i))
		}
		add(split.at.Add(-time.Second+8*time.Millisecond), split.msg+"-dup", split.msg+"-dup")
	}
	response, err := http.Post(baseURL+"/insert/jsonline?_time_field=_time&_msg_field=_msg", "application/stream+json", &body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ingest status %d", response.StatusCode)
	}

	// Forward every request to the real VictoriaLogs instance and record the
	// range and response size of each packed-row query.
	type packedQuery struct {
		start, end string
		lines      int
		bytes      int
	}
	var mu sync.Mutex
	var packed []packedQuery
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		forward, err := http.NewRequestWithContext(r.Context(), r.Method, baseURL+r.URL.RequestURI(), bytes.NewReader(data))
		if err != nil {
			t.Error(err)
			return
		}
		forward.Header = r.Header.Clone()
		upstreamResponse, err := http.DefaultClient.Do(forward)
		if err != nil {
			t.Error(err)
			return
		}
		defer upstreamResponse.Body.Close()
		result, err := io.ReadAll(upstreamResponse.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if form, err := url.ParseQuery(string(data)); err == nil && strings.Contains(form.Get("query"), "pack_json") {
			mu.Lock()
			packed = append(packed, packedQuery{start: form.Get("start"), end: form.Get("end"), lines: bytes.Count(result, []byte("\n")), bytes: len(result)})
			mu.Unlock()
		}
		maps.Copy(w.Header(), upstreamResponse.Header)
		w.WriteHeader(upstreamResponse.StatusCode)
		if _, err := w.Write(result); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL, Scope: models.VictoriaLogsScope{Query: "test_run:=" + runID}, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true, MaxWindowSeconds: 60, Concurrency: 1}})
	waitForFixtures(t, provider, source, queryWindow{start: base, end: end}, total)

	run := func(req datasource.WindowedRequest) (logs []map[string]any, cursor string) {
		t.Helper()
		plan, err := provider.PrepareWindowedQuery(source, req)
		if err != nil {
			t.Fatal(err)
		}
		if err := provider.StreamWindowedQuery(context.Background(), source, plan, func(event datasource.WindowedEvent) error {
			if event.Status == datasource.WindowFailed {
				t.Fatalf("window failed: %+v", event)
			}
			logs = append(logs, event.Logs...)
			if event.Type == datasource.WindowEnd {
				cursor = event.Cursor
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return logs, cursor
	}
	windowStart := func(query packedQuery) string {
		if query.end > formatAPITime(newerStart) {
			return formatAPITime(newerStart)
		}
		return formatAPITime(base)
	}
	// collect pages through the range and returns the rows and the packed
	// queries it sent. Each resumed page is retried once for its window.
	collect := func(limit, maxBytes int) ([]map[string]any, []packedQuery) {
		t.Helper()
		mu.Lock()
		packed = nil
		mu.Unlock()
		req := datasource.WindowedRequest{QueryRequest: datasource.QueryRequest{RawQuery: "*", StartTime: &base, EndTime: &end, Limit: limit, MaxResponseBytes: maxBytes}}
		var all []map[string]any
		for range 50 {
			logs, cursor := run(req)
			if req.Cursor != "" && len(logs) > 0 {
				plan, err := provider.PrepareWindowedQuery(source, req)
				if err != nil {
					t.Fatal(err)
				}
				retryReq := req
				retryReq.WindowIndex = &plan.Index
				retry, _ := run(retryReq)
				if len(retry) > len(logs) || !reflect.DeepEqual(retry, logs[:len(retry)]) {
					t.Fatalf("retry returned %v, want a prefix of %v", retry, logs)
				}
			}
			all = append(all, logs...)
			if cursor == "" {
				mu.Lock()
				queries := packed
				mu.Unlock()
				return all, queries
			}
			req.Cursor = cursor
		}
		t.Fatal("pagination did not finish")
		return nil, nil
	}
	check := func(name string, rows []map[string]any) {
		t.Helper()
		got := make(map[string]int)
		for i, row := range rows {
			got[row["_msg"].(string)]++
			if i > 0 && rowTimeForTest(t, rows[i-1]).Before(rowTimeForTest(t, row)) {
				t.Fatalf("%s: rows not newest first at %d", name, i)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
	// fallbackEnds returns the end of each full-window reread that follows a
	// narrowed page query over the same range, and checks that each reread
	// follows a short page that the byte limit did not cut.
	fallbackEnds := func(name string, queries []packedQuery, limit, maxBytes int) map[string]bool {
		t.Helper()
		ends := make(map[string]bool)
		for i := 1; i < len(queries); i++ {
			previous, query := queries[i-1], queries[i]
			if query.end != previous.end || query.start != windowStart(query) || previous.start == query.start {
				continue
			}
			if previous.lines > limit || (maxBytes > 0 && previous.bytes > maxBytes) {
				t.Fatalf("%s: reread after a full or byte-limited page %+v", name, previous)
			}
			ends[query.end] = true
		}
		return ends
	}

	resumedEnd := formatAPITime(tie.Add(time.Nanosecond))
	for _, limit := range []int{3, 4, 5} {
		name := fmt.Sprintf("limit %d", limit)
		rows, queries := collect(limit, 0)
		check(name, rows)
		ends := fallbackEnds(name, queries, limit, 0)
		// Every limit reaches the older window from its start with a boundary
		// that is too new.
		if !ends[formatAPITime(newerStart)] {
			t.Fatalf("%s: older window was not reread from its start: %+v", name, queries)
		}
		// With limit 4, the page resumed inside the tie also gets a boundary
		// that is too new and leaves the page short.
		if limit == 4 && !ends[resumedEnd] {
			t.Fatalf("%s: resumed page was not reread from the window start: %+v", name, queries)
		}
		bounded := false
		for i, query := range queries {
			if query.start != windowStart(query) && (i+1 == len(queries) || queries[i+1].end != query.end || queries[i+1].start != windowStart(query)) {
				bounded = true
			}
		}
		if !bounded {
			t.Fatalf("%s: no page used only the narrowed range: %+v", name, queries)
		}
	}

	// A page that the byte limit cuts is not complete, so a short narrowed
	// page that is cut must not be reread.
	rows, _ := collect(4, 0)
	maxRow := 0
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		maxRow = max(maxRow, len(data)+1)
	}
	maxBytes := 2*maxRow + maxRow/2
	rows, queries := collect(4, maxBytes)
	check("byte limit", rows)
	fallbackEnds("byte limit", queries, 4, maxBytes)
	cut := false
	for i, query := range queries {
		if query.end == formatAPITime(newerStart) && query.start != formatAPITime(base) && query.lines <= 4 && query.bytes > maxBytes {
			cut = true
			if i+1 < len(queries) && queries[i+1].end == query.end && queries[i+1].start == formatAPITime(base) {
				t.Fatalf("byte limit: short page cut by the byte limit was reread: %+v", queries)
			}
		}
	}
	if !cut {
		t.Fatalf("byte limit: no short narrowed page was cut by the byte limit: %+v", queries)
	}
}

func rowTimeForTest(t *testing.T, row map[string]any) time.Time {
	t.Helper()
	timestamp, err := rowTime(row)
	if err != nil {
		t.Fatal(err)
	}
	return timestamp
}

// rowWindow returns the index of the one-minute window that holds row, for
// the three-minute range that starts at base.
func rowWindow(t *testing.T, row map[string]any, base time.Time) int {
	t.Helper()
	return 2 - int(rowTimeForTest(t, row).Sub(base)/time.Minute)
}

func TestSchemaDiscoveryLookback(t *testing.T) {
	type request struct {
		lookback time.Duration
		scope    []string
		account  string
	}
	requests := make(chan request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.URL.Path != "/select/logsql/field_names" {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		start, startErr := time.Parse(time.RFC3339Nano, r.Form.Get("start"))
		end, endErr := time.Parse(time.RFC3339Nano, r.Form.Get("end"))
		if startErr != nil || endErr != nil {
			t.Errorf("invalid discovery range %q %q", r.Form.Get("start"), r.Form.Get("end"))
			return
		}
		requests <- request{end.Sub(start), r.Form["extra_filters"], r.Header.Get("AccountID")}
		_, _ = w.Write([]byte(`{"values":[{"value":"service","hits":1}]}`))
	}))
	defer server.Close()
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i, tc := range []struct {
		name      string
		optimizer *models.VictoriaLogsOptimizer
		want      time.Duration
	}{
		{"standard", nil, 24 * time.Hour},
		{"disabled optimizer", &models.VictoriaLogsOptimizer{SchemaLookbackSeconds: 600}, 24 * time.Hour},
		{"optimizer default", &models.VictoriaLogsOptimizer{Enabled: true}, 5 * time.Minute},
		{"optimizer setting", &models.VictoriaLogsOptimizer{Enabled: true, SchemaLookbackSeconds: 900}, 15 * time.Minute},
	} {
		source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL, Tenant: models.VictoriaLogsTenant{AccountID: "7"}, Scope: models.VictoriaLogsScope{Query: "namespace:=prod"}, Optimizer: tc.optimizer})
		source.ID = models.SourceID(i + 1)
		// PopulateSourceDetails loads the source columns for the explorer.
		if err := provider.PopulateSourceDetails(context.Background(), source); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := <-requests
		if got.lookback != tc.want || !reflect.DeepEqual(got.scope, []string{"namespace:=prod"}) || got.account != "7" {
			t.Fatalf("%s: discovery request %+v, want lookback %s with scope and tenant", tc.name, got, tc.want)
		}
	}
}
