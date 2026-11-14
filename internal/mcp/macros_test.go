package mcp

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A query copied from a panel runs through the MCP tools: the tool passes the
// range, timezone, interval and variables, and the backend gets the rendered
// query.
func TestQueryToolsRenderMacrosAndVariables(t *testing.T) {
	t.Parallel()
	var (
		mu       sync.Mutex
		received = map[string]map[string]string{}
	)
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		mu.Lock()
		received[r.URL.Path] = map[string]string{"query": r.Form.Get("query"), "step": r.Form.Get("step"), "offset": r.Form.Get("offset")}
		mu.Unlock()
		if r.URL.Path == "/select/logsql/hits" {
			_, _ = rw.Write([]byte(`{"hits":[]}`))
		}
	}))
	t.Cleanup(backend.Close)

	w := newWorld(t)
	source := w.victoriaLogsSource(t, backend.URL)
	s := w.server()
	variables := []map[string]any{{"name": "service", "type": "text", "value": "api"}}
	base := map[string]any{
		"team_id": w.team, "source_id": source.ID,
		"start_time": "2026-11-05T18:30:00Z", "end_time": "2026-11-12T18:30:00Z",
		"timezone": "Asia/Kolkata", "variables": variables,
	}
	with := func(extra map[string]any) map[string]any {
		args := maps.Clone(base)
		maps.Copy(args, extra)
		return args
	}

	result := callTool(t, s, session(w.member), "query_logs", with(map[string]any{
		"raw_sql":  `service:={{service}} _time:[{{__from}}, {{__to}}) | stats by (_time:{{__interval}} offset {{__tz_offset}}) count() as n`,
		"interval": "24h",
	}))
	if result.IsError {
		t.Fatalf("query_logs: %s", result.text())
	}
	want := `service:="api" _time:["2026-11-05T18:30:00Z", "2026-11-12T18:30:00Z") | stats by (_time:1d offset 19800s) count() as n`
	if got := received["/select/logsql/query"]["query"]; got != want {
		t.Fatalf("query_logs sent\n got  %s\n want %s", got, want)
	}

	result = callTool(t, s, session(w.member), "get_log_histogram", with(map[string]any{
		"raw_sql": `service:={{service}} bucket_seconds:{{__interval_s}}`, "window": "24h",
	}))
	if result.IsError {
		t.Fatalf("get_log_histogram: %s", result.text())
	}
	hits := received["/select/logsql/hits"]
	if hits["query"] != `service:="api" bucket_seconds:86400` || hits["step"] != "24h" || hits["offset"] != "19800s" {
		t.Fatalf("get_log_histogram sent %+v", hits)
	}

	for name, args := range map[string]map[string]any{
		"a macro with no interval":       with(map[string]any{"raw_sql": "* | stats by (_time:{{__interval}}) count()"}),
		"an interval outside the set":    with(map[string]any{"raw_sql": "*", "interval": "90m"}),
		"a timezone with a quote":        with(map[string]any{"raw_sql": "*", "timezone": "Asia/Kolkata'"}),
		"a variable named after a macro": with(map[string]any{"raw_sql": "*", "variables": []map[string]any{{"name": "__from", "type": "text", "value": "x"}}}),
	} {
		mu.Lock()
		delete(received, "/select/logsql/query")
		mu.Unlock()
		result := callTool(t, s, session(w.member), "query_logs", args)
		if !result.IsError {
			t.Errorf("%s: query_logs succeeded", name)
		}
		if _, ran := received["/select/logsql/query"]; ran {
			t.Errorf("%s: the query reached the backend", name)
		}
		if strings.Contains(result.text(), "internal") {
			t.Errorf("%s: %s, want a request error", name, result.text())
		}
	}
}
