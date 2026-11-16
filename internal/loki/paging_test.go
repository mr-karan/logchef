package loki

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestQueryStep(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		span       time.Duration
		requested  time.Duration
		want       time.Duration
		wantRaised bool
	}{
		{"1 hour", time.Hour, 0, time.Second, false},
		{"24 hours", 24 * time.Hour, 0, 8 * time.Second, false},
		{"30 days", 30 * 24 * time.Hour, 0, 236 * time.Second, false},
		{"requested step that fits", time.Hour, 5 * time.Minute, 5 * time.Minute, false},
		{"requested step below the point limit", 24 * time.Hour, time.Second, 8 * time.Second, true},
		{"fractional requested step rounds up", time.Hour, 1500 * time.Millisecond, 2 * time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step, raised := queryStep(start, start.Add(tt.span), tt.requested)
			if step != tt.want || raised != tt.wantRaised {
				t.Fatalf("step %v raised %v, want %v %v", step, raised, tt.want, tt.wantRaised)
			}
			if points := tt.span / step; points > maxPointsPerSeries {
				t.Fatalf("%d points exceed Loki's limit", points)
			}
		})
	}
}

// seed fills the fake with total entries one millisecond apart across three
// streams, and tie extra entries that share one timestamp, placed so that
// newer entries number tieRank.
func seed(f *fakeLoki, base time.Time, total, tie, tieRank int) {
	streams := []map[string]string{
		{"service_name": "api", "pod": "a"},
		{"service_name": "api", "pod": "b"},
		{"service_name": "web", "pod": "c"},
	}
	for i := range total {
		f.entries = append(f.entries, fakeEntry{
			ts:     base.Add(time.Duration(i) * time.Millisecond).UnixNano(),
			labels: streams[i%3],
			line:   fmt.Sprintf("line %d", i),
		})
	}
	tieTS := base.Add(time.Duration(total-1-tieRank)*time.Millisecond + 500*time.Microsecond).UnixNano()
	for i := range tie {
		f.entries = append(f.entries, fakeEntry{ts: tieTS, labels: streams[i%3], line: fmt.Sprintf("tie %d", i)})
	}
}

func assertNewestFirstDistinct(t *testing.T, rows []map[string]any) {
	t.Helper()
	seen := map[string]bool{}
	for i, row := range rows {
		key := fmt.Sprint(row["_time"], row["pod"], row["_msg"])
		if seen[key] {
			t.Fatalf("row %d repeats %s", i, key)
		}
		seen[key] = true
		if i > 0 && row["_time"].(string) > rows[i-1]["_time"].(string) {
			t.Fatalf("row %d (%v) is newer than row %d (%v)", i, row["_time"], i-1, rows[i-1]["_time"])
		}
	}
}

func TestQueryLogsPagesAcrossABoundaryTimestamp(t *testing.T) {
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		limit          int
		wantRows       int
		wantTruncation models.Truncation
	}{
		{"every entry", 1000, 590, models.TruncationNone},
		{"limit inside the data", 400, 400, models.TruncationRows},
		{"limit equal to the data", 590, 590, models.TruncationNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, server := newFakeLoki(t)
			f.maxEntries = 100
			// 560 entries plus 30 on one timestamp that the first page cuts:
			// 90 entries are newer, so the first page of 100 holds 10 of them.
			seed(f, base, 560, 30, 90)
			source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL, Limits: &models.LokiLimits{MaxEntriesPerQuery: 100}})
			start, end := timeRange(base.Add(-time.Minute), time.Hour)

			result, err := testProvider().QueryLogs(t.Context(), source, datasource.QueryRequest{
				RawQuery: `{service_name=~".+"}`, StartTime: start, EndTime: end, Limit: tt.limit, MaxLimit: 10000,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Logs) != tt.wantRows || result.Stats.Truncation != tt.wantTruncation {
				t.Fatalf("got %d rows truncation %q, want %d %q", len(result.Logs), result.Stats.Truncation, tt.wantRows, tt.wantTruncation)
			}
			assertNewestFirstDistinct(t, result.Logs)
			calls := f.rangeCalls()
			for i, call := range calls {
				if call.direction != "backward" || call.limit > 100 || call.start != start.UnixNano() {
					t.Fatalf("call %d: %+v", i, call)
				}
				if i > 0 && call.end >= calls[i-1].end {
					t.Fatalf("call %d did not move end back: %+v after %+v", i, call, calls[i-1])
				}
			}
		})
	}
}

func TestQueryLogsStopsWhenOneTimestampHoldsMoreThanAPage(t *testing.T) {
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	f, server := newFakeLoki(t)
	seed(f, base, 50, 150, 10)
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL, Limits: &models.LokiLimits{MaxEntriesPerQuery: 100}})
	start, end := timeRange(base, time.Hour)

	result, err := testProvider().QueryLogs(t.Context(), source, datasource.QueryRequest{
		RawQuery: `{service_name=~".+"}`, StartTime: start, EndTime: end, Limit: 1000, MaxLimit: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.Truncation != models.TruncationRows {
		t.Fatalf("truncation %q, want rows", result.Stats.Truncation)
	}
	if !hasWarning(result, "TIMESTAMP_TIE", "More than 100 entries share one timestamp") {
		t.Fatalf("warnings %+v", result.Warnings)
	}
	assertNewestFirstDistinct(t, result.Logs)
	if calls := len(f.rangeCalls()); calls > 3 {
		t.Fatalf("%d requests, want the pager to stop", calls)
	}
}

func hasWarning(result *models.QueryResult, code, text string) bool {
	for _, w := range result.Warnings {
		if w.Code == code && strings.Contains(w.Message, text) {
			return true
		}
	}
	return false
}

func TestQueryLogsByteBudget(t *testing.T) {
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	start, end := timeRange(base, time.Hour)
	run := func(t *testing.T, url string, budget int) (*models.QueryResult, error) {
		t.Helper()
		source := testSource(t, models.LokiConnectionInfo{BaseURL: url, Limits: &models.LokiLimits{MaxEntriesPerQuery: 100}})
		return testProvider().QueryLogs(t.Context(), source, datasource.QueryRequest{
			RawQuery: `{service_name=~".+"}`, StartTime: start, EndTime: end, Limit: 1000, MaxLimit: 10000, MaxResponseBytes: budget,
		})
	}

	t.Run("a later page that crosses the budget is dropped", func(t *testing.T) {
		f, server := newFakeLoki(t)
		seed(f, base, 300, 0, 0)
		pageBytes := measurePage(t, server.URL)
		result, err := run(t, server.URL, pageBytes+pageBytes/2)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Logs) != 100 || result.Stats.Truncation != models.TruncationBytes || result.Stats.TruncatedReason != "byte_limit" {
			t.Fatalf("got %d rows, stats %+v", len(result.Logs), result.Stats)
		}
	})

	t.Run("a first page over the budget is retried with a tenth of the limit", func(t *testing.T) {
		f, server := newFakeLoki(t)
		seed(f, base, 300, 0, 0)
		pageBytes := measurePage(t, server.URL)
		result, err := run(t, server.URL, pageBytes/2)
		if err != nil {
			t.Fatal(err)
		}
		calls := f.rangeCalls()
		if calls[1].limit != 100 || calls[2].limit != 10 {
			t.Fatalf("calls %+v, want the first page retried with limit 10", calls)
		}
		if len(result.Logs) == 0 || result.Stats.Truncation != models.TruncationBytes {
			t.Fatalf("got %d rows, stats %+v", len(result.Logs), result.Stats)
		}
	})

	t.Run("a small first page over the budget is a 400", func(t *testing.T) {
		f, server := newFakeLoki(t)
		seed(f, base, 300, 0, 0)
		_, err := run(t, server.URL, 200)
		validation, ok := errors.AsType[*datasource.ValidationError](err)
		if !ok || !strings.Contains(validation.Message, "exceeds the size limit") {
			t.Fatalf("error %v, want the size limit validation error", err)
		}
		if calls := len(f.rangeCalls()); calls != 2 {
			t.Fatalf("%d requests, want one retry", calls)
		}
	})
}

// measurePage returns the body size of a full first page of 100 entries.
func measurePage(t *testing.T, url string) int {
	t.Helper()
	source := testSource(t, models.LokiConnectionInfo{BaseURL: url})
	conn, err := source.LokiConnection()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	page, err := testProvider().fetchPage(t.Context(), conn, rangeRequest{query: "{}", start: start, step: time.Second}, start.Add(3*time.Hour).UnixNano(), 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return page.size
}

func TestQueryLogsMetricResult(t *testing.T) {
	f, server := newFakeLoki(t)
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		writeResult(w, "matrix", []map[string]any{
			{"metric": map[string]string{"level": "warn"}, "values": [][]any{{1794811200, "3"}}},
			{"metric": map[string]string{"level": "error", "value": "shadowed"}, "values": [][]any{{1794810600.5, "1"}, {1794811200, "2.5"}}},
		})
	}
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
	start, end := timeRange(time.Unix(1794810000, 0), 24*time.Hour)

	result, err := testProvider().QueryLogs(t.Context(), source, datasource.QueryRequest{
		RawQuery: `sum by (level) (count_over_time({app="api"}[1m]))`, StartTime: start, EndTime: end, Limit: 2, Step: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := columnNames(result.Columns); got != "value,time,level" {
		t.Fatalf("columns %s", got)
	}
	want := []map[string]any{
		{"value": "1", "time": "2026-11-16T06:30:00.5Z", "level": "error"},
		{"value": "2.5", "time": "2026-11-16T06:40:00Z", "level": "error"},
	}
	if fmt.Sprint(result.Logs) != fmt.Sprint(want) {
		t.Fatalf("rows %v, want %v", result.Logs, want)
	}
	if result.Stats.Truncation != models.TruncationRows {
		t.Fatalf("stats %+v, want a row truncation", result.Stats)
	}
	if !hasWarning(result, "STEP_RAISED", "Step raised to 8 seconds") {
		t.Fatalf("warnings %+v", result.Warnings)
	}
	if calls := f.rangeCalls(); len(calls) != 1 || calls[0].step != "8" || calls[0].direction != "backward" || calls[0].limit != 3 {
		t.Fatalf("calls %+v", calls)
	}
}

func columnNames(columns []models.ColumnInfo) string {
	names := make([]string, len(columns))
	for i, c := range columns {
		names[i] = c.Name
	}
	return strings.Join(names, ",")
}

func TestQueryLogsRowShape(t *testing.T) {
	f, server := newFakeLoki(t)
	ts := time.Date(2026, 11, 1, 10, 0, 0, 120, time.UTC)
	f.entries = []fakeEntry{
		{ts: ts.UnixNano(), labels: map[string]string{"app": "api", "_msg": "label", "trace_id": "t1"}, line: "hello"},
	}
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
	start, end := timeRange(ts.Add(-time.Minute), time.Hour)
	result, err := testProvider().QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: `{app="api"}`, StartTime: start, EndTime: end, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := columnNames(result.Columns); got != "_time,_msg,app,trace_id" {
		t.Fatalf("columns %s", got)
	}
	row := result.Logs[0]
	if row["_time"] != "2026-11-01T10:00:00.000000120Z" || row["_msg"] != "hello" || row["trace_id"] != "t1" {
		t.Fatalf("row %v", row)
	}
	if result.Stats.RowsRead != 7 || result.Stats.ExecutionTimeMs != 2 || result.Stats.LimitApplied != 10 {
		t.Fatalf("stats %+v", result.Stats)
	}
}

func TestQueryLogsRequiresARange(t *testing.T) {
	source := testSource(t, models.LokiConnectionInfo{BaseURL: "http://loki.test"})
	_, err := testProvider().QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: `{app="api"}`})
	validation, ok := errors.AsType[*datasource.ValidationError](err)
	if !ok || validation.Field != "start_time" {
		t.Fatalf("error %v", err)
	}
}

func TestQueryLogsHonoursTheQueryTimeout(t *testing.T) {
	f, server := newFakeLoki(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.handle = func(http.ResponseWriter, *http.Request) {
		f.mu.Unlock()
		defer f.mu.Lock()
		<-release
	}
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
	start, end := timeRange(time.Now(), time.Hour)
	timeout := 1
	began := time.Now()
	_, err := testProvider().QueryLogs(context.Background(), source, datasource.QueryRequest{RawQuery: `{app="api"}`, StartTime: start, EndTime: end, QueryTimeout: &timeout})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v, want the deadline", err)
	}
	if elapsed := time.Since(began); elapsed > 5*time.Second {
		t.Fatalf("took %v", elapsed)
	}
}

// Loki can return one entry several times. A full page of entries that were
// all seen cannot move the end back, so the pager stops with the tie warning
// instead of asking for the same page again.
func TestQueryLogsStopsOnAPageOfRepeatedEntries(t *testing.T) {
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	f, server := newFakeLoki(t)
	same := fakeEntry{ts: base.Add(time.Minute).UnixNano(), labels: map[string]string{"app": "api"}, line: "same"}
	for range 150 {
		f.entries = append(f.entries, same)
	}
	for i := range 50 {
		f.entries = append(f.entries, fakeEntry{ts: base.Add(time.Duration(i) * time.Millisecond).UnixNano(), labels: map[string]string{"app": "api"}, line: fmt.Sprint(i)})
	}
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL, Limits: &models.LokiLimits{MaxEntriesPerQuery: 100}})
	start, end := timeRange(base, time.Hour)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	result, err := testProvider().QueryLogs(ctx, source, datasource.QueryRequest{
		RawQuery: `{app="api"} | line_format "same"`, StartTime: start, EndTime: end, Limit: 1000, MaxLimit: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls := len(f.rangeCalls()); calls > 3 {
		t.Fatalf("%d requests, want the pager to stop", calls)
	}
	if result.Stats.Truncation != models.TruncationRows || !hasWarning(result, "TIMESTAMP_TIE", "share one timestamp") {
		t.Fatalf("stats %+v warnings %+v", result.Stats, result.Warnings)
	}
}
