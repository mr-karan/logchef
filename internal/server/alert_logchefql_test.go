package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

// statsUpstream is a VictoriaLogs stats_query that records each query and
// time. A query with by (service) gets two series, any other one value.
type statsUpstream struct {
	mu      sync.Mutex
	queries []string
	times   []time.Time
}

func (u *statsUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.URL.Path != "/select/logsql/stats_query" {
		http.NotFound(w, r)
		return
	}
	at, err := time.Parse(time.RFC3339Nano, r.Form.Get("time"))
	if err != nil {
		http.Error(w, "bad time", http.StatusBadRequest)
		return
	}
	query := r.Form.Get("query")
	u.mu.Lock()
	u.queries = append(u.queries, query)
	u.times = append(u.times, at)
	u.mu.Unlock()
	if strings.Contains(query, "by (service)") {
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"__lc_v0","service":"checkout"},"value":[%[1]d,"7"]},{"metric":{"__name__":"__lc_v0","service":"search"},"value":[%[1]d,"2"]}]}}`, at.Unix())
		return
	}
	_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"__lc_v0"},"value":[%d,"5"]}]}}`, at.Unix())
}

func (u *statsUpstream) seen() ([]string, []time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.queries), slices.Clone(u.times)
}

// newConditionRouteEnv is a server with alerting on and a VictoriaLogs
// source, behind upstream, that the admin's team sees.
func newConditionRouteEnv(t *testing.T, upstream http.Handler) (*auditRouteEnv, models.SourceID) {
	t.Helper()
	backend := httptest.NewServer(upstream)
	t.Cleanup(backend.Close)
	e := newAuditRouteEnv(t, withConfig(func(c *config.Config) { c.Alerts.Enabled = true }))
	conn, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "vl-conditions", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: conn}
	if err := e.db.CreateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	team := &models.Team{Name: "conditions"}
	if err := e.db.CreateTeam(t.Context(), team); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddTeamSource(t.Context(), team.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddTeamMember(t.Context(), team.ID, e.admin.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	return e, source.ID
}

func conditionBody(t *testing.T, sourceID models.SourceID, fields map[string]any) string {
	t.Helper()
	body := map[string]any{
		"source_id": sourceID, "name": "errors", "query_language": "logchefql", "editor_mode": "condition",
		"condition_json": `level="error"`, "query": `level="error" | count()`, "lookback_seconds": 300,
		"threshold_operator": "gte", "threshold_value": 5, "frequency_seconds": 60, "severity": "warning", "is_active": true,
	}
	maps.Copy(body, fields)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// The server compiles a LogchefQL condition at save: the by keys become
// group_by, and the test route runs the compiled LogsQL over the lookback
// (RFC 0006 unit Q1).
func TestLogchefQLAlertRoutes(t *testing.T) {
	upstream := &statsUpstream{}
	e, sourceID := newConditionRouteEnv(t, upstream)
	grouped := `level="error" | count() by service`

	resp := e.do(http.MethodPost, "/api/v1/alerts", conditionBody(t, sourceID, map[string]any{"query": grouped}), e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var alert models.Alert
	created(t, resp, &alert)
	if alert.QueryLanguage != models.QueryLanguageLogchefQL || alert.EditorMode != models.AlertEditorModeCondition || alert.Query != grouped || !slices.Equal(alert.GroupBy, []string{"service"}) {
		t.Fatalf("stored rule = %s %s %q group_by %q", alert.QueryLanguage, alert.EditorMode, alert.Query, alert.GroupBy)
	}
	path := "/api/v1/alerts/" + strconv.Itoa(int(alert.ID))
	resp = e.do(http.MethodPut, path, `{"group_by":["host"]}`, e.cookie)
	if resp.status != http.StatusBadRequest || !strings.Contains(string(resp.body), "differs from the by keys") {
		t.Fatalf("group_by that differs from the query: %d %s", resp.status, resp.body)
	}
	wantStatus(t, e.do(http.MethodPut, path, `{"group_by":["service"],"name":"renamed"}`, e.cookie), http.StatusOK)

	resp = e.do(http.MethodPost, "/api/v1/alerts/test", conditionBody(t, sourceID, map[string]any{"query": grouped}), e.cookie)
	wantStatus(t, resp, http.StatusOK)
	var test struct {
		Data models.TestAlertQueryResponse `json:"data"`
	}
	if err := json.Unmarshal(resp.body, &test); err != nil {
		t.Fatal(err)
	}
	if test.Data.GroupsTotal != 2 || !test.Data.ThresholdMet || len(test.Data.Samples) != 2 || test.Data.Samples[0].Group["service"] != "checkout" || test.Data.Samples[0].Value != 7 {
		t.Fatalf("test result = %+v, want checkout 7 and search 2", test.Data)
	}
	queries, _ := upstream.seen()
	if want := `_time:5m level:="error" | stats by (service) count() as __lc_v0 | sort by (__lc_v0 desc, service)`; len(queries) != 1 || queries[0] != want {
		t.Fatalf("upstream queries = %q, want %q", queries, want)
	}

	// VictoriaLogs returns a group under its field name; the rule reads it
	// under its alias.
	resp = e.do(http.MethodPost, "/api/v1/alerts/test", conditionBody(t, sourceID, map[string]any{"query": `level="error" | count() by service as svc`}), e.cookie)
	wantStatus(t, resp, http.StatusOK)
	if err := json.Unmarshal(resp.body, &test); err != nil {
		t.Fatal(err)
	}
	if test.Data.GroupsTotal != 2 || test.Data.Samples[0].Group["svc"] != "checkout" || test.Data.Samples[0].Value != 7 {
		t.Fatalf("aliased test result = %+v, want svc=checkout 7", test.Data)
	}

	for _, tc := range []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"no aggregation", map[string]any{"query": `level="error"`}, "an alert condition needs an aggregation"},
		{"time key", map[string]any{"query": `| count() by time(5m)`}, "one window per evaluation"},
		{"two values", map[string]any{"query": `| count(), sum(dur)`}, "computes one value"},
		{"five keys", map[string]any{"query": `| count() by a, b, c, d, e`}, "at most 4 keys"},
		{"syntax", map[string]any{"query": `level= | count()`}, "invalid LogchefQL condition"},
		{"native editor", map[string]any{"editor_mode": "native"}, "query_language logchefql needs editor_mode condition"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := e.do(http.MethodPost, "/api/v1/alerts/test", conditionBody(t, sourceID, tc.fields), e.cookie)
			if resp.status != http.StatusBadRequest || !strings.Contains(string(resp.body), tc.want) {
				t.Fatalf("status %d %s, want 400 with %q", resp.status, resp.body, tc.want)
			}
		})
	}
}

// A backtest replays the compiled condition: the same LogsQL at every step,
// each evaluation with its own time.
func TestLogchefQLAlertBacktest(t *testing.T) {
	upstream := &statsUpstream{}
	e, sourceID := newConditionRouteEnv(t, upstream)
	resp := e.do(http.MethodPost, "/api/v1/alerts/backtest", conditionBody(t, sourceID, map[string]any{"range_seconds": 3600, "lookback_seconds": 60}), e.cookie)
	wantStatus(t, resp, http.StatusOK)
	var got struct {
		Data models.BacktestResult `json:"data"`
	}
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Data.Evaluations != 60 || got.Data.Summary.Errors != 0 || got.Data.Coverage != 1 {
		t.Fatalf("backtest = %+v", got.Data)
	}
	queries, times := upstream.seen()
	if len(queries) != 60 {
		t.Fatalf("upstream saw %d evaluations, want 60", len(queries))
	}
	for i, query := range queries {
		if query != `_time:1m level:="error" | stats count() as __lc_v0` || !times[i].Equal(got.Data.Series[i].T) {
			t.Fatalf("evaluation %d ran %q at %s; series has %s", i, query, times[i], got.Data.Series[i].T)
		}
	}
}

// A LogchefQL condition may leave out condition_json: the server takes the
// filter in front of the aggregation pipe, on create, test and backtest. A
// rule with no filter still needs one.
func TestLogchefQLConditionDerivesConditionJSON(t *testing.T) {
	upstream := &statsUpstream{}
	e, sourceID := newConditionRouteEnv(t, upstream)
	noConditionJSON := map[string]any{"condition_json": nil, "query": `level="error" and service="api" | count() by service`}

	resp := e.do(http.MethodPost, "/api/v1/alerts", conditionBody(t, sourceID, noConditionJSON), e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var alert models.Alert
	created(t, resp, &alert)
	if want := `level="error" and service="api"`; alert.ConditionJSON != want {
		t.Fatalf("condition_json = %q, want %q", alert.ConditionJSON, want)
	}

	wantStatus(t, e.do(http.MethodPost, "/api/v1/alerts/test", conditionBody(t, sourceID, noConditionJSON), e.cookie), http.StatusOK)
	backtest := maps.Clone(noConditionJSON)
	backtest["range_seconds"] = 3600
	wantStatus(t, e.do(http.MethodPost, "/api/v1/alerts/backtest", conditionBody(t, sourceID, backtest), e.cookie), http.StatusOK)

	resp = e.do(http.MethodPost, "/api/v1/alerts/test", conditionBody(t, sourceID, map[string]any{"condition_json": nil, "query": `| count()`}), e.cookie)
	if resp.status != http.StatusBadRequest || !strings.Contains(string(resp.body), "no filter in front of its aggregation") {
		t.Fatalf("rule without a filter: status %d %s, want 400", resp.status, resp.body)
	}
}

// The test route puts the group closest to firing first: the lowest value
// for lt, the highest for gt, and warnings is a list even when empty.
func TestAlertTestValueAndWarnings(t *testing.T) {
	upstream := &statsUpstream{}
	e, sourceID := newConditionRouteEnv(t, upstream)
	grouped := `level="error" | count() by service`
	test := func(fields map[string]any) (models.TestAlertQueryResponse, string) {
		t.Helper()
		resp := e.do(http.MethodPost, "/api/v1/alerts/test", conditionBody(t, sourceID, fields), e.cookie)
		wantStatus(t, resp, http.StatusOK)
		var parsed struct {
			Data models.TestAlertQueryResponse `json:"data"`
		}
		if err := json.Unmarshal(resp.body, &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed.Data, string(resp.body)
	}

	got, raw := test(map[string]any{"query": grouped, "threshold_operator": "lt", "threshold_value": 5})
	if got.Value != 2 || got.Samples[0].Group["service"] != "search" || !got.ThresholdMet {
		t.Fatalf("lt: value %v, first sample %v, want search with 2", got.Value, got.Samples[0])
	}
	if !strings.Contains(raw, `"warnings":[]`) {
		t.Errorf("warnings of a clean test is not an empty list: %s", raw)
	}
	if got, _ := test(map[string]any{"query": grouped, "threshold_operator": "gt", "threshold_value": 5}); got.Value != 7 || got.Samples[0].Group["service"] != "checkout" {
		t.Fatalf("gt: value %v, first sample %v, want checkout with 7", got.Value, got.Samples[0])
	}

}
