package mcp

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// newStatsBackend is a VictoriaLogs stats_query that answers 5 for every
// evaluation and records each query.
func newStatsBackend(t *testing.T) (url string, queries func() []string) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []string
	)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/health":
			return
		case "/select/logsql/field_names":
			_, _ = w.Write([]byte(`{"values":[{"value":"level","hits":1}]}`))
			return
		case "/select/logsql/stats_query":
		default:
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		seen = append(seen, r.Form.Get("query"))
		mu.Unlock()
		at, _ := time.Parse(time.RFC3339Nano, r.Form.Get("time"))
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"__lc_v0"},"value":[%d,"5"]}]}}`, at.Unix())
	}))
	t.Cleanup(backend.Close)
	return backend.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

// test_alert and backtest_alert run an unsaved rule with only alerts:read,
// under admission, and take condition_json from the query when it is left
// out.
func TestAlertValidationTools(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	url, queries := newStatsBackend(t)
	source := w.victoriaLogsSource(t, url)
	s := w.server()
	reader := token(w.member, models.TokenScopeAlertsRead)
	rule := map[string]any{
		"team_id": w.team, "source_id": source.ID, "query_language": "logchefql",
		"query": `level="error" | count()`, "threshold_operator": "gt", "threshold_value": 3, "lookback_seconds": 60,
	}

	result := callTool(t, s, reader, "test_alert", rule)
	var test models.TestAlertQueryResponse
	if err := json.Unmarshal([]byte(result.text()), &test); err != nil || result.IsError {
		t.Fatalf("test_alert: %v %s", err, result.text())
	}
	if test.Value != 5 || !test.ThresholdMet {
		t.Fatalf("test_alert = %+v, want value 5 over the threshold", test)
	}

	backtest := maps.Clone(rule)
	backtest["frequency_seconds"] = 60
	backtest["range_seconds"] = 3600
	result = callTool(t, s, reader, "backtest_alert", backtest)
	var replay models.BacktestResult
	if err := json.Unmarshal([]byte(result.text()), &replay); err != nil || result.IsError {
		t.Fatalf("backtest_alert: %v %s", err, result.text())
	}
	if replay.Evaluations != 60 || replay.Summary.Episodes != 1 || replay.Summary.Errors != 0 {
		t.Fatalf("backtest_alert = %+v, want 60 evaluations in one episode", replay)
	}
	if got := len(queries()); got != 61 {
		t.Fatalf("backend saw %d queries, want 1 test and 60 replays", got)
	}
	if _, admitted := w.admits.counts(); admitted != 2 {
		t.Fatalf("admitted %d calls, want 2", admitted)
	}
	// Every evaluation fires, so the series holds the one change from ok.
	if len(replay.Series) != 1 || replay.Series[0].Status != models.BacktestStatusFiring {
		t.Fatalf("series = %+v, want only the change to firing", replay.Series)
	}

	backtest["include_series"] = true
	backtest["end_time"] = time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	result = callTool(t, s, reader, "backtest_alert", backtest)
	replay = models.BacktestResult{}
	if err := json.Unmarshal([]byte(result.text()), &replay); err != nil || result.IsError {
		t.Fatalf("backtest_alert with end_time: %v %s", err, result.text())
	}
	if len(replay.Series) != 60 || time.Since(replay.End) < 47*time.Hour {
		t.Fatalf("series %d points ending %s, want every evaluation of the window two days ago", len(replay.Series), replay.End)
	}
	delete(backtest, "include_series")
	delete(backtest, "end_time")

	backtest["range_seconds"] = 60
	if result := callTool(t, s, reader, "backtest_alert", backtest); !result.IsError || !strings.Contains(result.text(), "range_seconds must be between") {
		t.Fatalf("short range = %s, want the range error", result.text())
	}
	outsideRule := maps.Clone(rule)
	outsideRule["team_id"] = w.otherTeam
	if result := callTool(t, s, reader, "test_alert", outsideRule); !result.IsError {
		t.Fatalf("test_alert through a team the caller is not in ran: %s", result.text())
	}
}

func TestListAlertSilencesTool(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := w.server()
	weekly := &models.AlertSilenceRequest{
		SourceID: w.source.ID, Comment: "quiet hours", ScheduleKind: models.SilenceWeekly,
		Weekly: &models.SilenceWeeklyWindow{Weekdays: []int{0, 6}, Start: "00:00", DurationMinutes: 1440, Timezone: "Asia/Kolkata"},
	}
	if _, err := core.CreateSilenceForPrincipal(t.Context(), w.db, discardLogger(), *session(w.member), weekly, time.Now()); err != nil {
		t.Fatalf("CreateSilenceForPrincipal: %v", err)
	}

	result := callTool(t, s, token(w.member, models.TokenScopeAlertsRead), "list_alert_silences", map[string]any{"source_id": w.source.ID})
	var silences []models.AlertSilence
	if err := json.Unmarshal([]byte(result.text()), &silences); err != nil || result.IsError {
		t.Fatalf("list_alert_silences: %v %s", err, result.text())
	}
	if len(silences) != 1 || silences[0].Comment != "quiet hours" || silences[0].Weekly == nil {
		t.Fatalf("silences = %+v, want the weekly quiet hours silence", silences)
	}
	if result := callTool(t, s, session(w.member), "list_alert_silences", map[string]any{"source_id": w.otherSource.ID}); !result.IsError {
		t.Fatalf("silences of a source the caller cannot see: %s", result.text())
	}
	if result := callTool(t, s, session(w.member), "list_alert_silences", map[string]any{}); !result.IsError || !strings.Contains(result.text(), "source_id is required") {
		t.Fatalf("no source_id = %s", result.text())
	}
}

func TestListCollectionsTool(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := w.server()
	if _, err := core.CreateCollection(t.Context(), w.db, discardLogger(), "payments on-call", "", w.member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := core.CreateCollection(t.Context(), w.db, discardLogger(), "someone else's", "", w.outsider.ID); err != nil {
		t.Fatal(err)
	}

	result := callTool(t, s, token(w.member, models.TokenScopeCollectionsRead), "list_collections", map[string]any{})
	var collections []models.Collection
	if err := json.Unmarshal([]byte(result.text()), &collections); err != nil || result.IsError {
		t.Fatalf("list_collections: %v %s", err, result.text())
	}
	if len(collections) != 1 || collections[0].Name != "payments on-call" || collections[0].CallerRole != models.CollectionRoleOwner {
		t.Fatalf("collections = %+v, want only the caller's own", collections)
	}
}

func TestListDestinationsToolHidesConfig(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := w.server()
	for owner, name := range map[*models.User]string{w.member: "mine", w.outsider: "theirs"} {
		if _, err := core.CreateDestination(t.Context(), w.db, owner, &models.DestinationRequest{
			Name: name, Type: models.DestinationTypeEmail, Scope: models.DestinationScopePrivate,
			Config: models.DestinationConfig{UserIDs: []models.UserID{owner.ID}},
		}); err != nil {
			t.Fatalf("CreateDestination(%s): %v", name, err)
		}
	}

	result := callTool(t, s, token(w.member, models.TokenScopeDestinationsRead), "list_destinations", map[string]any{})
	var destinations []map[string]any
	if err := json.Unmarshal([]byte(result.text()), &destinations); err != nil || result.IsError {
		t.Fatalf("list_destinations: %v %s", err, result.text())
	}
	if len(destinations) != 1 || destinations[0]["name"] != "mine" {
		t.Fatalf("destinations = %v, want only the caller's own", destinations)
	}
	if keys := slices.Sorted(maps.Keys(destinations[0])); !slices.Equal(keys, []string{"id", "is_active", "name", "scope", "type"}) {
		t.Fatalf("destination keys = %v, want the summary only", keys)
	}
}

// get_alert_history returns the newest episodes up to limit, filters by
// status, and states the rule's query once instead of in every entry.
func TestGetAlertHistoryFilters(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	alert := &models.Alert{
		SourceID: w.source.ID, Name: "errors", QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
		Query: "SELECT count() AS value FROM logs WHERE {{__time_filter}}", LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan,
		ThresholdValue: 10, FrequencySeconds: 60, Severity: models.AlertSeverityWarning, IsActive: true, LastState: models.AlertStateResolved, CreatedBy: &w.member.ID,
	}
	if err := w.db.CreateAlert(t.Context(), alert); err != nil {
		t.Fatal(err)
	}
	for i := range 30 {
		status, payload := models.AlertStatusTriggered, map[string]any{"annotations": map[string]any{"query": alert.Query, "runbook": "r"}}
		if i%3 == 0 {
			status, payload = models.AlertStatusError, map[string]any{"query": alert.Query, "error": "boom"}
		}
		if _, err := w.db.InsertAlertHistory(t.Context(), alert.ID, "", nil, status, nil, fmt.Sprintf("entry %d", i), payload); err != nil {
			t.Fatal(err)
		}
	}
	history := func(args map[string]any) map[string]any {
		t.Helper()
		args["alert_id"] = alert.ID
		result := callTool(t, w.server(), session(w.member), "get_alert_history", args)
		var out map[string]any
		if err := json.Unmarshal([]byte(result.text()), &out); err != nil || result.IsError {
			t.Fatalf("get_alert_history %v: %v %s", args, err, result.text())
		}
		return out
	}

	out := history(map[string]any{})
	entries := out["history"].([]any)
	if len(entries) != 20 || out["alert"].(map[string]any)["query"] != alert.Query {
		t.Fatalf("default: %d entries, alert %v; want 20 and the query once", len(entries), out["alert"])
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "__time_filter") || !strings.Contains(string(raw), "runbook") {
		t.Fatalf("entries repeat the query or lost other payload: %s", raw)
	}
	if entries := history(map[string]any{"status": "error", "limit": 5})["history"].([]any); len(entries) != 5 || entries[0].(map[string]any)["status"] != "error" {
		t.Fatalf("status error, limit 5: %v", entries)
	}
	if result := callTool(t, w.server(), session(w.member), "get_alert_history", map[string]any{"alert_id": alert.ID, "limit": 500}); !result.IsError {
		t.Fatalf("limit 500 accepted: %s", result.text())
	}
}
