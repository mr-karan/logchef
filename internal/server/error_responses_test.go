package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const internalDetail = "dial tcp 10.9.8.7:9000: password for user reader rejected"

type errorResponseEnv struct {
	s        *Server
	cookie   *http.Cookie
	team     models.TeamID
	vlSource models.SourceID
	chSource models.SourceID
}

func newErrorResponseServer(t *testing.T, upstream http.HandlerFunc) errorResponseEnv {
	t.Helper()
	backend := httptest.NewServer(upstream)
	t.Cleanup(backend.Close)

	db := newServerTestDB(t)
	cfg := &config.Config{
		Query: config.QueryConfig{
			DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 30, DefaultPreviewLimit: 100, MaxPreviewLimit: 1000,
			MaxResponseBytes: 1 << 20, MaxConcurrentPerUser: 3, MaxConcurrentGlobal: 30,
		},
		Alerts: config.AlertsConfig{Enabled: true},
	}
	s := newServerForTest(t, cfg, db, nil)
	s.app.Post("/test/panic", func(fiber.Ctx) error { panic(internalDetail) })
	s.app.Post("/test/error", func(fiber.Ctx) error { return errors.New(internalDetail) })
	s.app.Post("/test/fiber-error", func(fiber.Ctx) error {
		return fiber.NewError(fiber.StatusServiceUnavailable, internalDetail)
	})

	ctx := context.Background()
	admin := mkTestUser(t, db, "admin@example.com", models.UserRoleAdmin)
	team := &models.Team{Name: "vl-team"}
	if err := db.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	conn, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: backend.URL, Optimizer: &models.VictoriaLogsOptimizer{Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	src := &models.Source{Name: "vl", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: conn}
	if err := db.CreateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	chSrc := &models.Source{Name: "ch", MetaTSField: "timestamp", Connection: models.ConnectionInfo{Host: "10.9.8.7:9000", Username: "reader", Database: "logs", TableName: "app"}}
	if err := db.CreateSource(ctx, chSrc); err != nil {
		t.Fatal(err)
	}
	for _, id := range []models.SourceID{src.ID, chSrc.ID} {
		if err := db.AddTeamSource(ctx, team.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AddTeamMember(ctx, team.ID, admin.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	s.alertsManager = alerts.NewManager(alerts.Options{DB: db, Logger: s.log})
	return errorResponseEnv{s: s, cookie: sessionCookie(t, db, admin), team: team.ID, vlSource: src.ID, chSource: chSrc.ID}
}

func vlUpstream(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	switch {
	case r.URL.Path == "/select/logsql/hits" && r.Form.Get("step") == "2x":
		http.Error(w, "cannot parse duration from the arg 'step=2x': "+internalDetail, http.StatusBadRequest)
	case r.URL.Path == "/select/logsql/hits":
		_, _ = io.WriteString(w, "<html>bad gateway "+internalDetail+"</html>")
	case r.URL.Path == "/select/logsql/query", r.URL.Path == "/select/logsql/stats_query", r.URL.Path == "/select/logsql/stream_field_names":
		http.Error(w, internalDetail, http.StatusBadGateway)
	default:
		_, _ = io.WriteString(w, `{"values":[]}`)
	}
}

func TestErrorResponses(t *testing.T) {
	env := newErrorResponseServer(t, vlUpstream)
	s, cookie, sourceID := env.s, env.cookie, env.vlSource
	teamPath := fmt.Sprintf("/api/v1/teams/%d", env.team)
	quietAlert := &models.Alert{
		SourceID: sourceID, Name: "quiet", QueryLanguage: models.QueryLanguageLogsQL, EditorMode: models.AlertEditorModeNative,
		Query: "* | stats count() c", LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan,
		FrequencySeconds: 60, Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
	}
	if err := s.sqlite.CreateAlert(context.Background(), quietAlert); err != nil {
		t.Fatal(err)
	}
	sourcePath := fmt.Sprintf("%s/sources/%d", teamPath, sourceID)
	chSourcePath := fmt.Sprintf("%s/sources/%d", teamPath, env.chSource)
	const window = `"start_time":"2026-10-01T00:00:00Z","end_time":"2026-10-01T01:00:00Z"`

	for _, tc := range []struct {
		name         string
		method, path string
		body         string
		status       int
		message      string
		contains     string
	}{
		{
			name: "panic", method: http.MethodPost, path: "/test/panic",
			status: fiber.StatusInternalServerError, message: "Internal server error",
		},
		{
			name: "untyped handler error", method: http.MethodPost, path: "/test/error",
			status: fiber.StatusInternalServerError, message: "Internal server error",
		},
		{
			name: "fiber server error", method: http.MethodPost, path: "/test/fiber-error",
			status: fiber.StatusServiceUnavailable, message: "Internal server error",
		},
		{
			name: "source validation with an internal cause", method: http.MethodPost, path: "/api/v1/admin/sources/validate",
			body:   `{"source_type":"victorialogs","connection":{"base_url":"http://10.9.8.7:9000/%zz"}}`,
			status: fiber.StatusBadRequest, message: "base_url must be a valid URL",
		},
		{
			name: "source creation with an internal cause", method: http.MethodPost, path: "/api/v1/admin/sources",
			body:   `{"name":"invalid-url","source_type":"victorialogs","meta_ts_field":"_time","connection":{"base_url":"http://10.9.8.7:9000/%zz"}}`,
			status: fiber.StatusBadRequest, message: "base_url must be a valid URL",
		},
		{
			name: "source update with an internal cause", method: http.MethodPut, path: fmt.Sprintf("/api/v1/admin/sources/%d", sourceID),
			body:   `{"connection":{"base_url":"http://10.9.8.7:9000/%zz"}}`,
			status: fiber.StatusBadRequest, message: "base_url must be a valid URL",
		},
		{
			name: "team name too short", method: http.MethodPost, path: "/api/v1/admin/teams",
			body:   `{"name":"a"}`,
			status: fiber.StatusBadRequest, contains: "team name must be between 2 and 50 characters",
		},
		{
			name: "team renamed too short", method: http.MethodPut, path: teamPath,
			body:   `{"name":"a"}`,
			status: fiber.StatusBadRequest, contains: "team name must be between 2 and 50 characters",
		},
		{
			name: "team member with unknown role", method: http.MethodPost, path: teamPath + "/members",
			body:   `{"user_id":1,"role":"owner"}`,
			status: fiber.StatusBadRequest, contains: "role must be 'admin', 'editor', or 'member'",
		},
		{
			name: "windowed LogchefQL that does not compile", method: http.MethodPost, path: sourcePath + "/logs/windowed",
			body:   `{"query_language":"logchefql","query_text":"level=",` + window + `}`,
			status: fiber.StatusBadRequest, contains: `unexpected token "<EOF>"`,
		},
		{
			name: "windowed search with a forged cursor", method: http.MethodPost, path: sourcePath + "/logs/windowed",
			body:   `{"query_language":"logsql","query_text":"*","cursor":"!!",` + window + `}`,
			status: fiber.StatusBadRequest, contains: "invalid search cursor",
		},
		{
			name: "histogram window the datasource rejects", method: http.MethodPost, path: sourcePath + "/logs/histogram",
			body:   `{"query_text":"*","window":"2x",` + window + `}`,
			status: fiber.StatusBadRequest, message: "invalid histogram window: use a positive duration such as 1h30m, or a single day or week value such as 1d",
		},
		{
			name: "compound histogram window", method: http.MethodPost, path: sourcePath + "/logs/histogram",
			body:   `{"query_text":"*","window":"1d12h",` + window + `}`,
			status: fiber.StatusBadRequest, message: "invalid histogram window: use a positive duration such as 1h30m, or a single day or week value such as 1d",
		},
		{
			name: "year histogram window", method: http.MethodPost, path: sourcePath + "/logs/histogram",
			body:   `{"query_text":"*","window":"1y",` + window + `}`,
			status: fiber.StatusBadRequest, message: "invalid histogram window: use a positive duration such as 1h30m, or a single day or week value such as 1d",
		},
		{
			name: "ClickHouse histogram window that does not exist", method: http.MethodPost, path: chSourcePath + "/logs/histogram",
			body:   `{"query_text":"SELECT * FROM logs.app","window":"2x",` + window + `}`,
			status: fiber.StatusBadRequest, contains: "invalid histogram window: 2x",
		},
		{
			name: "ClickHouse histogram without a connection", method: http.MethodPost, path: chSourcePath + "/logs/histogram",
			body:   `{"query_text":"SELECT * FROM logs.app","window":"1m",` + window + `}`,
			status: fiber.StatusInternalServerError, message: "Failed to generate histogram data",
		},
		{
			name: "histogram upstream answers with garbage", method: http.MethodPost, path: sourcePath + "/logs/histogram",
			body:   `{"query_text":"*","window":"1m",` + window + `}`,
			status: fiber.StatusInternalServerError, message: "Failed to generate histogram data",
		},
		{
			name: "stream fields upstream failure", method: http.MethodGet, path: sourcePath + "/logs/stream-fields",
			status: fiber.StatusInternalServerError, message: "Could not discover stream fields",
		},
		{
			name: "logs query upstream failure", method: http.MethodPost, path: sourcePath + "/logs/query",
			body:   `{"raw_sql":"*",` + window + `}`,
			status: fiber.StatusInternalServerError, message: "Failed to query logs",
		},
		{
			name: "alert test query upstream failure", method: http.MethodPost, path: "/api/v1/alerts/test",
			body:   fmt.Sprintf(`{"source_id":%d,"query_language":"logsql","editor_mode":"native","query":"* | stats count() c","threshold_operator":"gt","threshold_value":1}`, sourceID),
			status: fiber.StatusInternalServerError, message: "Failed to test alert query",
		},
		{
			name: "resolve an alert that is not firing", method: http.MethodPost, path: fmt.Sprintf("/api/v1/alerts/%d/resolve", quietAlert.ID),
			body:   `{}`,
			status: fiber.StatusNotFound, message: "Alert is not active",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := doAuthzRequest(t, s, tc.method, tc.path, tc.body, withSession(cookie))
			if got.status != tc.status {
				t.Errorf("status = %d, want %d (message %q)", got.status, tc.status, got.message)
			}
			if tc.message != "" && got.message != tc.message {
				t.Errorf("message = %q, want %q", got.message, tc.message)
			}
			if tc.contains != "" && !strings.Contains(got.message, tc.contains) {
				t.Errorf("message = %q, want it to contain %q", got.message, tc.contains)
			}
			if strings.Contains(got.message, "10.9.8.7") || strings.Contains(got.message, "invalid character") {
				t.Errorf("message leaks internal detail: %q", got.message)
			}
		})
	}
}

func configureErrorResponseClickHouse(t *testing.T, env errorResponseEnv, addr string) {
	t.Helper()
	source, err := env.s.sqlite.GetSource(t.Context(), env.chSource)
	if err != nil {
		t.Fatal(err)
	}
	source.Connection = models.ConnectionInfo{Host: addr, Username: "default", Database: "system", TableName: "numbers"}
	if err := env.s.sqlite.UpdateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	manager := clickhouse.NewManager(env.s.log)
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	env.s.datasources.Register(datasource.NewClickHouseProvider(manager, env.s.log))
	if err := env.s.datasources.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
}

func TestClickHouseQueryFailureResponses(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR for read-only ClickHouse checks")
	}
	env := newErrorResponseServer(t, vlUpstream)
	configureErrorResponseClickHouse(t, env, addr)
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs", env.team, env.chSource)
	for _, tc := range []struct{ name, endpoint, body, reason string }{
		{"parser", "/query", `{"query_text":"SELECT ("}`, "invalid SQL syntax"},
		{"non-select", "/query", `{"query_text":"SHOW TABLES"}`, "only SELECT"},
		{"identifier", "/query", `{"query_text":"SELECT missing_query_field FROM system.numbers"}`, "missing_query_field"},
		{"type", "/query", `{"query_text":"SELECT plus(1, [1])"}`, "Illegal"},
		{"aggregation", "/query", `{"query_text":"SELECT sum(number) FROM system.numbers WHERE sum(number) > 1"}`, "Aggregate"},
		{"function", "/query", `{"query_text":"SELECT no_such_query_function(1)"}`, "no_such_query_function"},
		{"histogram", "/histogram", `{"query_text":"SELECT missing_query_field FROM system.numbers","window":"1m","start_time":"2026-10-01T00:00:00Z","end_time":"2026-10-01T01:00:00Z"}`, "missing_query_field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := doAuthzRequest(t, env.s, http.MethodPost, path+tc.endpoint, tc.body, withSession(env.cookie))
			if got.status != 400 || !strings.Contains(got.message, tc.reason) {
				t.Fatalf("status=%d message=%q, want 400 with %q", got.status, got.message, tc.reason)
			}
		})
	}
	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, path+"/query", strings.NewReader(`{"query_text":"SELECT number FROM numbers(3)"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(env.cookie)
		resp, err := env.s.app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body struct {
			Status string `json:"status"`
			Data   struct {
				Data    []map[string]any `json:"data"`
				QueryID string           `json:"query_id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK || body.Status != "success" || len(body.Data.Data) != 3 || body.Data.QueryID == "" {
			t.Fatalf("status=%d body=%+v", resp.StatusCode, body)
		}
	})
	t.Run("alert", func(t *testing.T) {
		got := doAuthzRequest(t, env.s, http.MethodPost, "/api/v1/alerts/test", fmt.Sprintf(`{"source_id":%d,"query_language":"clickhouse-sql","editor_mode":"native","query":"SELECT missing_query_field FROM system.numbers","threshold_operator":"gt","threshold_value":1}`, env.chSource), withSession(env.cookie))
		if got.status != 400 || !strings.Contains(got.message, "missing_query_field") {
			t.Fatalf("status=%d message=%q", got.status, got.message)
		}
	})
	t.Run("admin authentication", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"name": "auth-check", "meta_ts_field": "timestamp", "source_type": "clickhouse", "connection": models.ConnectionInfo{Host: addr, Username: "logchef_nonexistent_query_error_test_user", Database: "system", TableName: "numbers"}})
		if err != nil {
			t.Fatal(err)
		}
		for _, endpoint := range []struct{ method, path string }{
			{http.MethodPost, "/api/v1/admin/sources/validate"},
			{http.MethodPost, "/api/v1/admin/sources"},
			{http.MethodPut, fmt.Sprintf("/api/v1/admin/sources/%d", env.chSource)},
		} {
			got := doAuthzRequest(t, env.s, endpoint.method, endpoint.path, string(body), withSession(env.cookie))
			if got.status != 400 || !strings.Contains(got.message, "authentication failed") || strings.Contains(got.message, addr) || strings.Contains(got.message, "nonexistent_query_error") {
				t.Fatalf("%s status=%d message=%q, want safe authentication cause", endpoint.path, got.status, got.message)
			}
		}
	})
}

func TestDeadClickHouseQueryResponse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	env := newErrorResponseServer(t, vlUpstream)
	configureErrorResponseClickHouse(t, env, addr)
	var logs bytes.Buffer
	env.s.log = slog.New(slog.NewTextHandler(&logs, nil))
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", env.team, env.chSource)
	got := doAuthzRequest(t, env.s, http.MethodPost, path, `{"query_text":"SELECT 1"}`, withSession(env.cookie))
	if got.status != 500 || got.message != logsQueryFailure {
		t.Fatalf("status=%d message=%q, want 500 with generic message", got.status, got.message)
	}
	if count := strings.Count(logs.String(), "level=ERROR"); count != 1 {
		t.Fatalf("failure logged %d times, want once", count)
	}
}

func TestVictoriaLogsQueryFailureResponse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_VICTORIALOGS_URL")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_VICTORIALOGS_URL for read-only VictoriaLogs checks")
	}
	env := newErrorResponseServer(t, vlUpstream)
	source, err := env.s.sqlite.GetSource(t.Context(), env.vlSource)
	if err != nil {
		t.Fatal(err)
	}
	source.ConnectionConfig, err = json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: addr})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.s.sqlite.UpdateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", env.team, env.vlSource)

	for _, query := range []string{"* | no_such_pipe_for_test", `_msg:"` + strings.Repeat("x", 244) + `" | stats countt()`} {
		t.Run(fmt.Sprintf("length=%d", len(query)), func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"query_text": query})
			if err != nil {
				t.Fatal(err)
			}
			got := doAuthzRequest(t, env.s, http.MethodPost, path, string(body), withSession(env.cookie))
			reason := "no_such_pipe_for_test"
			if strings.Contains(query, "countt") {
				reason = `unknown stats func "countt"`
			}
			t.Logf("query length=%d status=%d message=%q", len(query), got.status, got.message)
			if got.status != 400 || !strings.Contains(got.message, reason) || strings.Contains(got.message, "remoteAddr") || strings.Contains(got.message, "requestURI") || strings.Contains(got.message, "127.0.0.1") {
				t.Fatalf("want 400 with cleaned parse reason %q", reason)
			}
		})
	}
}

func TestClickHouseResourceFailureResponses(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR for read-only ClickHouse checks")
	}
	env := newErrorResponseServer(t, vlUpstream)
	configureErrorResponseClickHouse(t, env, addr)
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", env.team, env.chSource)
	for _, tc := range []struct {
		name, query string
		status      int
		reason      string
	}{
		{"timeout", "SELECT sleep(3) SETTINGS max_execution_time=1", 408, "Request timed out"},
		{"rows", "SELECT number FROM numbers(100) SETTINGS max_result_rows=1, result_overflow_mode='throw'", 400, "max rows"},
		{"bytes", "SELECT number FROM numbers(100) SETTINGS max_result_bytes=1, result_overflow_mode='throw'", 400, "max bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			env.s.log = slog.New(slog.NewTextHandler(&logs, nil))
			body, err := json.Marshal(map[string]string{"query_text": tc.query})
			if err != nil {
				t.Fatal(err)
			}
			got := doAuthzRequest(t, env.s, http.MethodPost, path, string(body), withSession(env.cookie))
			t.Logf("status=%d message=%q", got.status, got.message)
			if got.status != tc.status || !strings.Contains(got.message, tc.reason) {
				t.Errorf("want %d with %q", tc.status, tc.reason)
			}
			if count := strings.Count(logs.String(), "level=ERROR"); count > 1 {
				t.Errorf("failure logged %d times, want at most once", count)
			}
		})
	}
}
