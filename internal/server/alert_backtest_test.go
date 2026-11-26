package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/pkg/models"
)

// backtestUpstream is a VictoriaLogs stats_query that counts 50 rows for an
// evaluation whose time falls in [burst, burst+5m) and 1 otherwise, and
// records the time of each request.
type backtestUpstream struct {
	burst time.Time
	mu    sync.Mutex
	times []time.Time
}

func (u *backtestUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	u.mu.Lock()
	u.times = append(u.times, at)
	u.mu.Unlock()
	value := "1"
	if !at.Before(u.burst) && at.Before(u.burst.Add(5*time.Minute)) {
		value = "50"
	}
	_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"value"},"value":[%d,%q]}]}}`, at.Unix(), value)
}

type backtestResponse struct {
	status  int
	message string
	result  models.BacktestResult
}

func postBacktest(t *testing.T, s *Server, cookie *http.Cookie, body string) backtestResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/backtest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := s.app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Message string                `json:"message"`
		Data    models.BacktestResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return backtestResponse{status: resp.StatusCode, message: parsed.Message, result: parsed.Data}
}

// TestAlertBacktestRoute replays a VictoriaLogs rule over one hour against a
// burst 30 minutes ago and sees one episode, each evaluation sent with its
// own time, and no alert state written.
func TestAlertBacktestRoute(t *testing.T) {
	upstream := &backtestUpstream{burst: time.Now().Add(-30 * time.Minute)}
	env := newErrorResponseServer(t, upstream.ServeHTTP)

	got := postBacktest(t, env.s, env.cookie, fmt.Sprintf(`{"source_id":%d,"query_language":"logsql","editor_mode":"native","query":"* | stats count() as value","threshold_operator":"gt","threshold_value":10,"lookback_seconds":60,"frequency_seconds":60,"range_seconds":3600}`, env.vlSource))
	if got.status != http.StatusOK {
		t.Fatalf("status=%d message=%q", got.status, got.message)
	}
	r := got.result
	if r.Evaluations != 60 || r.StepSeconds != 60 || r.Coverage != 1 || r.Partial || len(r.Series) != 60 {
		t.Fatalf("evaluations=%d step=%d coverage=%v partial=%v series=%d", r.Evaluations, r.StepSeconds, r.Coverage, r.Partial, len(r.Series))
	}
	if r.Summary.Episodes != 1 || r.Summary.Notifications != 2 || r.Summary.Errors != 0 || r.Summary.FiringSeconds != 5*60 {
		t.Errorf("summary = %+v, want one five-minute episode with its resolve", r.Summary)
	}

	upstream.mu.Lock()
	times := upstream.times
	upstream.mu.Unlock()
	if len(times) != 60 {
		t.Fatalf("upstream saw %d evaluations, want 60", len(times))
	}
	for i, at := range times {
		if !at.Equal(r.Series[i].T) || (i > 0 && at.Sub(times[i-1]) != time.Minute) {
			t.Fatalf("evaluation %d ran at %s; series has %s", i, at, r.Series[i].T)
		}
	}

	alerts, err := env.s.store.ListAlertsBySource(t.Context(), env.vlSource)
	if err != nil || len(alerts) != 0 {
		t.Fatalf("alerts after a backtest: %v, %v", alerts, err)
	}
}

// TestAlertBacktestRefusals: a query that cannot be replayed is 422 with the
// reason, a range outside the bounds is 400, a second backtest while one
// runs is 429, and a global admin outside the source's teams is 403.
func TestAlertBacktestRefusals(t *testing.T) {
	env := newErrorResponseServer(t, vlUpstream)
	ch := func(query string, rangeSeconds int) string {
		body, err := json.Marshal(map[string]any{
			"source_id": env.chSource, "query_language": "clickhouse-sql", "editor_mode": "native", "query": query,
			"threshold_operator": "gt", "threshold_value": 1, "lookback_seconds": 300, "frequency_seconds": 60, "range_seconds": rangeSeconds,
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	cases := []struct {
		name    string
		body    string
		status  int
		message string
	}{
		{"now()", ch("SELECT count() FROM logs.app WHERE timestamp > now() - INTERVAL 5 MINUTE", 3600), http.StatusUnprocessableEntity, "This query reads the current time. Replace the time filter with {{__time_filter}} to backtest it."},
		{"now() beside a macro", ch("SELECT count() FROM logs.app WHERE {{__time_filter}} AND timestamp > now() - INTERVAL 1 HOUR", 3600), http.StatusUnprocessableEntity, "This query reads the current time."},
		{"no window", ch("SELECT count() FROM logs.app", 3600), http.StatusUnprocessableEntity, "This query has no time window."},
		{"range too short", ch("SELECT count() FROM logs.app WHERE {{__time_filter}}", 600), http.StatusBadRequest, "range_seconds must be between 3600 and 604800"},
		{"range too long", ch("SELECT count() FROM logs.app WHERE {{__time_filter}}", 8*24*3600), http.StatusBadRequest, "range_seconds must be between 3600 and 604800"},
		{"frequency too short", strings.Replace(ch("SELECT count() FROM logs.app WHERE {{__time_filter}}", 3600), `"frequency_seconds":60`, `"frequency_seconds":5`, 1), http.StatusBadRequest, "frequency_seconds must be at least 10"},
		{"frequency longer than the range", strings.Replace(ch("SELECT count() FROM logs.app WHERE {{__time_filter}}", 3600), `"frequency_seconds":60`, `"frequency_seconds":9223372036854775807`, 1), http.StatusBadRequest, "frequency_seconds must not exceed range_seconds"},
		{"initialQueryStartTime()", ch("SELECT count() FROM logs.app WHERE {{__time_filter}} AND timestamp >= initialQueryStartTime() - INTERVAL 5 MINUTE", 3600), http.StatusUnprocessableEntity, "This query reads the current time."},
		{"invalid query", ch("SELEC count() FROM logs.app", 3600), http.StatusBadRequest, "invalid alert query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := postBacktest(t, env.s, env.cookie, tc.body)
			if got.status != tc.status || !strings.Contains(got.message, tc.message) {
				t.Fatalf("status=%d message=%q, want %d with %q", got.status, got.message, tc.status, tc.message)
			}
		})
	}

	t.Run("busy", func(t *testing.T) {
		user, err := env.s.store.GetUserByEmail(t.Context(), "admin@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if err := queryTracker.StartQueryWithID("running-backtest", QueryClassBacktest, user.ID, env.chSource, 0, "", func() {}, backtestMaxPerUser, backtestMaxGlobal); err != nil {
			t.Fatal(err)
		}
		defer queryTracker.RemoveQuery("running-backtest")
		got := postBacktest(t, env.s, env.cookie, ch("SELECT count() FROM logs.app WHERE timestamp > now()", 3600))
		if got.status != http.StatusTooManyRequests || got.message != "A backtest is already running" {
			t.Fatalf("status=%d message=%q", got.status, got.message)
		}
	})

	t.Run("global admin outside the source's teams", func(t *testing.T) {
		outsider := mkTestUser(t, env.s.store, "outsider@example.com", models.UserRoleAdmin)
		got := postBacktest(t, env.s, sessionCookie(t, env.s.store, outsider), ch("SELECT count() FROM logs.app WHERE {{__time_filter}}", 3600))
		if got.status != http.StatusForbidden {
			t.Fatalf("status=%d message=%q, want 403", got.status, got.message)
		}
	})
}

// TestAlertBacktestEvaluationErrors: evaluations that fail are counted and
// the first one is reported the way the query routes report it, without the
// backend's internal detail.
func TestAlertBacktestEvaluationErrors(t *testing.T) {
	env := newErrorResponseServer(t, vlUpstream)
	got := postBacktest(t, env.s, env.cookie, fmt.Sprintf(`{"source_id":%d,"query_language":"logsql","editor_mode":"native","query":"* | stats count() as value","threshold_operator":"gt","threshold_value":10,"lookback_seconds":60,"frequency_seconds":60,"range_seconds":3600}`, env.vlSource))
	if got.status != http.StatusOK || got.result.Summary.Errors != 60 || got.result.Evaluations != 60 {
		t.Fatalf("status=%d summary=%+v evaluations=%d", got.status, got.result.Summary, got.result.Evaluations)
	}
	if got.result.FirstError == "" || strings.Contains(got.result.FirstError, internalDetail) || strings.Contains(got.result.FirstError, "10.9.8.7") {
		t.Fatalf("first_error = %q", got.result.FirstError)
	}
}

// TestAlertValidationNeedsOnlyAlertsRead: test and backtest write nothing and
// send nothing, so a token with alerts:read runs them and a token without an
// alerts scope gets 403 naming alerts:read.
func TestAlertValidationNeedsOnlyAlertsRead(t *testing.T) {
	upstream := &backtestUpstream{burst: time.Now().Add(-30 * time.Minute)}
	env := newErrorResponseServer(t, upstream.ServeHTTP)
	user, err := env.s.store.GetUserByEmail(t.Context(), "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	reader := apiToken(t, env.s, user, models.TokenScopeAlertsRead)
	logsOnly := apiToken(t, env.s, user, models.TokenScopeLogsRead)
	rule := fmt.Sprintf(`"source_id":%d,"query_language":"logsql","editor_mode":"native","query":"* | stats count() as value","threshold_operator":"gt","threshold_value":10,"lookback_seconds":60`, env.vlSource)

	for _, route := range []struct{ path, body string }{
		{"/api/v1/alerts/test", "{" + rule + "}"},
		{"/api/v1/alerts/backtest", "{" + rule + `,"frequency_seconds":60,"range_seconds":3600}`},
	} {
		if got := doAuthzRequest(t, env.s, http.MethodPost, route.path, route.body, withBearer(reader)); got.status != http.StatusOK {
			t.Errorf("%s with alerts:read: status %d message %q, want 200", route.path, got.status, got.message)
		}
		got := doAuthzRequest(t, env.s, http.MethodPost, route.path, route.body, withBearer(logsOnly))
		if got.status != http.StatusForbidden || got.message != "API token does not have the required scope alerts:read" {
			t.Errorf("%s without alerts:read: status %d message %q, want 403 naming alerts:read", route.path, got.status, got.message)
		}
	}
}

// TestAlertBacktestEndTime replays a past window that holds a burst, at full
// coverage, and refuses an end in the future.
func TestAlertBacktestEndTime(t *testing.T) {
	burst := time.Now().Add(-5 * time.Hour).Truncate(time.Minute)
	upstream := &backtestUpstream{burst: burst}
	env := newErrorResponseServer(t, upstream.ServeHTTP)
	body := func(end time.Time) string {
		return fmt.Sprintf(`{"source_id":%d,"query_language":"logsql","editor_mode":"native","query":"* | stats count() as value","threshold_operator":"gt","threshold_value":10,"lookback_seconds":60,"frequency_seconds":60,"range_seconds":3600,"end_time":%q}`, env.vlSource, end.UTC().Format(time.RFC3339))
	}

	got := postBacktest(t, env.s, env.cookie, body(burst.Add(30*time.Minute)))
	if got.status != http.StatusOK {
		t.Fatalf("status=%d message=%q", got.status, got.message)
	}
	r := got.result
	if !r.End.Equal(burst.Add(30*time.Minute)) || r.Coverage != 1 || r.Summary.Episodes != 1 || r.Summary.FiringSeconds != 5*60 {
		t.Fatalf("end=%s coverage=%v summary=%+v, want the burst window replayed in full", r.End, r.Coverage, r.Summary)
	}

	if got := postBacktest(t, env.s, env.cookie, body(time.Now().Add(time.Hour))); got.status != http.StatusBadRequest || !strings.Contains(got.message, "in the future") {
		t.Fatalf("future end_time: status=%d message=%q, want 400", got.status, got.message)
	}
}
