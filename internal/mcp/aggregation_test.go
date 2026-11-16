package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mr-karan/logchef/internal/logchefql"
)

// aggregationBackend is a VictoriaLogs stand-in that records the LogsQL it
// runs and answers every query with two groups under the internal value name.
type aggregationBackend struct {
	mu      sync.Mutex
	queries []string
}

func newAggregationBackend(t *testing.T) (backend *aggregationBackend, url string) {
	t.Helper()
	b := &aggregationBackend{}
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/health":
			rw.WriteHeader(http.StatusOK)
		case "/select/logsql/field_names":
			_, _ = rw.Write([]byte(`{"values":[{"value":"service","hits":1}]}`))
		case "/select/logsql/query":
			b.mu.Lock()
			b.queries = append(b.queries, r.Form.Get("query"))
			b.mu.Unlock()
			_, _ = rw.Write([]byte(`{"service":"api","__lc_v0":"12"}` + "\n" + `{"service":"web","__lc_v0":"3"}` + "\n"))
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(stub.Close)
	return b, stub.URL
}

func (b *aggregationBackend) sent() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.queries)
}

func TestQueryLogchefQLRunsAnAggregation(t *testing.T) {
	t.Parallel()
	backend, url := newAggregationBackend(t)
	w := newWorld(t)
	source := w.victoriaLogsSource(t, url)

	result := callTool(t, w.server(), session(w.member), "query_logchefql", map[string]any{
		"team_id": w.team, "source_id": source.ID, "query": "level=error | count() by service",
		"start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z",
	})
	if result.IsError {
		t.Fatalf("query_logchefql: %s", result.text())
	}
	var out struct {
		Logs        []map[string]any           `json:"logs"`
		Aggregation *logchefql.AggregationInfo `json:"aggregation"`
	}
	if err := json.Unmarshal(result.StructuredContent, &out); err != nil {
		t.Fatal(err)
	}
	if out.Aggregation == nil || out.Aggregation.Shape != logchefql.ShapeGrouped {
		t.Fatalf("aggregation = %+v, want shape grouped", out.Aggregation)
	}
	var names []string
	for _, col := range out.Aggregation.Columns {
		names = append(names, col.Name)
	}
	if !slices.Equal(names, []string{"service", "count"}) {
		t.Fatalf("aggregation columns = %v, want service, count", names)
	}
	if len(out.Logs) != 2 || out.Logs[0]["service"] != "api" || out.Logs[0]["count"] != "12" {
		t.Fatalf("logs = %v, want the groups under their output names", out.Logs)
	}
	if sent := backend.sent(); len(sent) != 1 || !strings.Contains(sent[0], "| stats by (service) count() as __lc_v0") {
		t.Fatalf("backend queries = %q, want one stats query", sent)
	}
}

func TestQueryLogchefQLPassesTheInterval(t *testing.T) {
	t.Parallel()
	backend, url := newAggregationBackend(t)
	w := newWorld(t)
	source := w.victoriaLogsSource(t, url)
	args := map[string]any{
		"team_id": w.team, "source_id": source.ID, "query": "| count() by time",
		"start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z",
	}

	result := callTool(t, w.server(), session(w.member), "query_logchefql", args)
	if !result.IsError || !strings.Contains(result.text(), "this query buckets by time, so the request needs an interval") {
		t.Fatalf("query_logchefql without interval = %s, want the interval error", result.text())
	}
	if sent := backend.sent(); len(sent) != 0 {
		t.Fatalf("backend queries = %q, want none", sent)
	}

	args["interval"] = "5m"
	result = callTool(t, w.server(), session(w.member), "query_logchefql", args)
	var out QueryResult
	if err := json.Unmarshal(result.StructuredContent, &out); err != nil || result.IsError {
		t.Fatalf("query_logchefql: %v %s", err, result.text())
	}
	if out.Aggregation == nil || out.Aggregation.Shape != logchefql.ShapeTimeseries || out.Aggregation.Interval != "5m" {
		t.Fatalf("aggregation = %+v, want a 5m timeseries", out.Aggregation)
	}
	if sent := backend.sent(); len(sent) != 1 || !strings.Contains(sent[0], "_time:5m") {
		t.Fatalf("backend queries = %q, want 5m buckets", sent)
	}
}

func TestTranslateLogchefQLDescribesTheAggregation(t *testing.T) {
	t.Parallel()
	_, url := newAggregationBackend(t)
	w := newWorld(t)
	source := w.victoriaLogsSource(t, url)

	result := callTool(t, w.server(), session(w.member), "translate_logchefql", map[string]any{
		"team_id": w.team, "source_id": source.ID, "query": "| p95(duration_ms) by time", "interval": "1h",
	})
	var out TranslateResult
	if err := json.Unmarshal(result.StructuredContent, &out); err != nil || result.IsError {
		t.Fatalf("translate_logchefql: %v %s", err, result.text())
	}
	if !out.Valid || out.Aggregation == nil || out.Aggregation.Interval != "1h" || !strings.Contains(out.GeneratedQuery, "_time:1h") {
		t.Fatalf("translation = %+v, want a 1h aggregation", out)
	}
}

// compare_windows needs log rows, so it keeps the default deny.
func TestCompareWindowsRejectsAnAggregation(t *testing.T) {
	t.Parallel()
	backend, url := newAggregationBackend(t)
	w := newWorld(t)
	source := w.victoriaLogsSource(t, url)

	result := callTool(t, w.server(), session(w.member), "compare_windows", map[string]any{
		"team_id": w.team, "source_id": source.ID, "query": "level=error | count()",
		"window1_start": "2026-10-01 00:00:00", "window1_end": "2026-10-01 01:00:00",
		"window2_start": "2026-10-01 01:00:00", "window2_end": "2026-10-01 02:00:00",
	})
	if !result.IsError || !strings.Contains(result.text(), "this feature needs log rows, and the query aggregates") {
		t.Fatalf("compare_windows = %s, want the aggregation denial", result.text())
	}
	if sent := backend.sent(); len(sent) != 0 {
		t.Fatalf("backend queries = %q, want none", sent)
	}
}
