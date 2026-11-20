package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// newReadsBackend is a VictoriaLogs stand-in that answers each read with two
// rows, values or buckets. A query that contains "boom" fails upstream.
func newReadsBackend(t *testing.T) string {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/health":
			rw.WriteHeader(http.StatusOK)
		case "/select/logsql/field_names":
			_, _ = rw.Write([]byte(`{"values":[{"value":"level","hits":4},{"value":"service","hits":4}]}`))
		case "/select/logsql/query":
			if strings.Contains(r.Form.Get("query"), "boom") {
				http.Error(rw, "backend unavailable", http.StatusBadGateway)
				return
			}
			_, _ = rw.Write([]byte(`{"_time":"2026-10-01T00:00:01Z","_msg":"a","level":"error"}` + "\n" + `{"_time":"2026-10-01T00:00:02Z","_msg":"b","level":"error"}` + "\n"))
		case "/select/logsql/hits":
			_, _ = rw.Write([]byte(`{"hits":[{"fields":{},"timestamps":["2026-10-01T00:00:00Z","2026-10-01T00:01:00Z"],"values":[3,4],"total":7}]}`))
		case "/select/logsql/field_values":
			_, _ = rw.Write([]byte(`{"values":[{"value":"api","hits":7},{"value":"web","hits":2}]}`))
		case "/select/logsql/facets":
			_, _ = rw.Write([]byte(`{"facets":[{"field_name":"level","values":[{"field_value":"error","hits":3},{"field_value":"info","hits":1}]}]}`))
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(stub.Close)
	return stub.URL
}

func rfc3339(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Each tool call that reads logs records one read, finished once, with the
// tool, its query, its time range and the rows it returned. A tool that reads
// no logs records none.
func TestToolCallsThatReadLogsRecordOneRead(t *testing.T) {
	t.Parallel()
	const from, to = "2026-10-01T00:00:00Z", "2026-10-01T01:00:00Z"
	const from2, to2 = "2026-10-01T02:00:00Z", "2026-10-01T03:00:00Z"
	for _, tc := range []struct {
		tool     string
		args     map[string]any
		query    string
		language models.QueryLanguage
		fields   []string
		limit    int
		rows     int64
		to       string
	}{
		{"query_logs", map[string]any{"raw_sql": "level:error", "limit": 50, "start_time": from, "end_time": to}, "level:error", "", nil, 50, 2, to},
		{"get_log_histogram", map[string]any{"raw_sql": "level:error", "start_time": from, "end_time": to}, "level:error", "", nil, 0, 2, to},
		{"query_logchefql", map[string]any{"query": "level=error", "limit": 50, "start_time": from, "end_time": to}, "level=error", models.QueryLanguageLogchefQL, nil, 50, 2, to},
		{"compare_windows", map[string]any{"query": "level=error", "window1_start": from, "window1_end": to, "window2_start": from2, "window2_end": to2}, "level=error", models.QueryLanguageLogchefQL, nil, 100, 4, to2},
		{"get_field_values", map[string]any{"field_name": "service", "field_type": "String", "query": "level=error", "start_time": from, "end_time": to}, "level=error", models.QueryLanguageLogchefQL, []string{"service"}, 20, 2, to},
		{"top_values", map[string]any{"fields": []string{"service", "level"}, "start_time": from, "end_time": to}, "", models.QueryLanguageLogchefQL, []string{"service", "level"}, 10, 4, to},
		{"get_all_field_dimensions", map[string]any{"start_time": from, "end_time": to}, "", models.QueryLanguageLogchefQL, nil, 10, 2, to},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			source := w.victoriaLogsSource(t, newReadsBackend(t))
			tc.args["team_id"], tc.args["source_id"] = w.team, source.ID
			if result := callTool(t, w.server(), session(w.member), tc.tool, tc.args); result.IsError {
				t.Fatalf("%s: %s", tc.tool, result.text())
			}

			got := w.reads.recorded()
			if len(got) != 1 {
				t.Fatalf("recorded %d reads, want one: %+v", len(got), got)
			}
			r := got[0]
			if r.finished != 1 || r.Err != nil {
				t.Errorf("finished %d times with error %v, want once with none", r.finished, r.Err)
			}
			if r.Tool != tc.tool || r.Query != tc.query || r.Language != tc.language || !slices.Equal(r.Fields, tc.fields) || r.Limit != tc.limit {
				t.Errorf("call %+v, want tool %s query %q language %q fields %v limit %d", r.ReadCall, tc.tool, tc.query, tc.language, tc.fields, tc.limit)
			}
			if r.Source.SourceID() != source.ID || r.Source.TeamID() != w.team {
				t.Errorf("source %d team %d", r.Source.SourceID(), r.Source.TeamID())
			}
			if r.Rows != tc.rows {
				t.Errorf("rows %d, want %d", r.Rows, tc.rows)
			}
			if r.From == nil || r.To == nil || !r.From.Equal(rfc3339(t, from)) || !r.To.Equal(rfc3339(t, tc.to)) {
				t.Errorf("range %v..%v, want %s..%s", r.From, r.To, from, tc.to)
			}
		})
	}

	t.Run("tools that read no logs", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		source := w.victoriaLogsSource(t, newReadsBackend(t))
		s := w.server()
		ids := map[string]any{"team_id": w.team, "source_id": source.ID}
		for name, args := range map[string]map[string]any{
			"get_sources":         {},
			"get_team_sources":    {"team_id": w.team},
			"get_source_schema":   ids,
			"translate_logchefql": {"team_id": w.team, "source_id": source.ID, "query": "level=error", "start_time": from, "end_time": to},
			"validate_logchefql":  {"team_id": w.team, "source_id": source.ID, "query": "level=error"},
			"list_saved_queries":  {},
			"list_alerts":         {},
		} {
			if result := callTool(t, s, session(w.member), name, args); result.IsError {
				t.Errorf("%s: %s", name, result.text())
			}
		}
		if got := w.reads.recorded(); len(got) != 0 {
			t.Errorf("recorded %+v, want no reads", got)
		}
	})
}

// A tool call that fails after its source is authorized records one read
// with the error, which keeps its cause for the reason code. A call that is
// not authorized records none.
func TestToolCallsThatFailRecordTheError(t *testing.T) {
	t.Parallel()
	const from, to = "2026-10-01T00:00:00Z", "2026-10-01T01:00:00Z"
	for _, tc := range []struct {
		name   string
		tool   string
		args   map[string]any
		refuse bool
		want   func(error) bool
	}{
		{"backend error", "query_logs", map[string]any{"raw_sql": "boom", "start_time": from, "end_time": to}, false,
			func(err error) bool { return err != nil && strings.Contains(err.Error(), "status 502") }},
		{"refused admission", "query_logchefql", map[string]any{"query": "", "start_time": from, "end_time": to}, true,
			func(err error) bool { return errors.Is(err, errAdmissionRefused) }},
		{"unsupported", "get_log_context", map[string]any{"timestamp": 1790850000000}, false,
			func(err error) bool { return errors.Is(err, datasource.ErrOperationNotSupported) }},
		{"invalid time range", "get_field_values", map[string]any{"field_name": "service", "field_type": "String", "start_time": "yesterday", "end_time": to}, false,
			func(err error) bool { return errors.Is(err, errInvalidArgument) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			w.admits.refuse = tc.refuse
			source := w.victoriaLogsSource(t, newReadsBackend(t))
			tc.args["team_id"], tc.args["source_id"] = w.team, source.ID
			if result := callTool(t, w.server(), session(w.member), tc.tool, tc.args); !result.IsError {
				t.Fatalf("%s succeeded: %s", tc.tool, result.text())
			}
			got := w.reads.recorded()
			if len(got) != 1 || got[0].finished != 1 || got[0].Tool != tc.tool || !tc.want(got[0].Err) {
				t.Fatalf("recorded %+v, want one finished read of %s with the cause", got, tc.tool)
			}
		})
	}

	t.Run("unauthorized", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		result := callTool(t, w.server(), session(w.member), "query_logs", map[string]any{"team_id": w.otherTeam, "source_id": w.otherSource.ID, "raw_sql": "*"})
		if !result.IsError {
			t.Fatalf("query_logs on another team's source succeeded: %s", result.text())
		}
		if got := w.reads.recorded(); len(got) != 0 {
			t.Errorf("recorded %+v, want no read", got)
		}
	})
}

// A caller-safe message must not hide a cancellation or timeout from the
// query.run event, which classifies the error by its cause.
func TestKnownErrorKeepsTheCauseBehindItsMessage(t *testing.T) {
	for _, tc := range []struct {
		cause error
		msg   string
	}{
		{context.Canceled, "op: cancelled"},
		{context.DeadlineExceeded, "op: timed out"},
		{fmt.Errorf("read: %w", context.Canceled), "op: cancelled"},
	} {
		err := knownError("op", tc.cause)
		if err == nil || err.Error() != tc.msg {
			t.Fatalf("knownError(%v) = %v, want message %q", tc.cause, err, tc.msg)
		}
		if !errors.Is(err, tc.cause) && !errors.Is(err, errors.Unwrap(tc.cause)) {
			t.Errorf("knownError(%v) lost its cause", tc.cause)
		}
	}
}

// top_values fails its read only when it was cancelled or no field could
// be read; one readable field makes it a success with partial results.
func TestTopValuesFailure(t *testing.T) {
	boom := errors.New("boom")
	missing := fmt.Errorf("%w: field %q is not in the source schema", errInvalidArgument, "x")
	for _, tc := range []struct {
		name      string
		cancelled bool
		errs      []error
		want      error
	}{
		{"all read", false, []error{nil, nil}, nil},
		{"partial", false, []error{boom, nil}, nil},
		{"all failed", false, []error{boom, missing}, boom},
		{"all missing from schema", false, []error{missing}, errInvalidArgument},
		{"cancelled with partial results", true, []error{nil, boom}, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			got := topValuesFailure(ctx, tc.errs)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v, want success", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
