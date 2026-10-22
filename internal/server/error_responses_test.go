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
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

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

func newErrorResponseServer(t *testing.T, upstream http.HandlerFunc, logs ...*slog.Logger) errorResponseEnv {
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
	s := newServerForTest(t, cfg, db, nil, logs...)
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
	if err := s.store.CreateAlert(context.Background(), quietAlert); err != nil {
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
	source, err := env.s.store.GetSource(t.Context(), env.chSource)
	if err != nil {
		t.Fatal(err)
	}
	source.Connection = models.ConnectionInfo{Host: addr, Username: "default", Database: "system", TableName: "numbers"}
	if err := env.s.store.UpdateSource(t.Context(), source); err != nil {
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
	var logs syncLogBuffer
	env := newErrorResponseServer(t, vlUpstream, slog.New(slog.NewTextHandler(&logs, nil)))
	configureErrorResponseClickHouse(t, env, addr)
	logs.Reset()
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query", env.team, env.chSource)
	got := doAuthzRequest(t, env.s, http.MethodPost, path, `{"query_text":"SELECT 1"}`, withSession(env.cookie))
	if got.status != 500 || got.message != logsQueryFailure {
		t.Fatalf("status=%d message=%q, want 500 with generic message", got.status, got.message)
	}
	if count := strings.Count(logs.String(), "level=ERROR"); count != 2 {
		t.Fatalf("failure logged %d times, want 1 mapper + 1 query hook\n%s", count, logs.String())
	}
	// The background health check can log the dead source at any point during the request.
	requestLogs := regexp.MustCompile(`(?m)^.*msg="source unhealthy".*\n`).ReplaceAllString(logs.String(), "")
	if count := strings.Count(requestLogs, "source_id="); count != 2 {
		t.Fatalf("want mapper and access log with source_id\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "level=WARN msg=http") || !strings.Contains(logs.String(), "status=500") {
		t.Fatalf("want access log at Warn with status 500\n%s", logs.String())
	}
}

func TestVictoriaLogsQueryFailureResponse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_VICTORIALOGS_URL")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_VICTORIALOGS_URL for read-only VictoriaLogs checks")
	}
	env := newErrorResponseServer(t, vlUpstream)
	source, err := env.s.store.GetSource(t.Context(), env.vlSource)
	if err != nil {
		t.Fatal(err)
	}
	source.ConnectionConfig, err = json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: addr})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.s.store.UpdateSource(t.Context(), source); err != nil {
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
		{"remote rows read 158", "SELECT * FROM remote('127.0.0.2', system.query_log) SETTINGS max_rows_to_read=1, prefer_localhost_replica=0", 400, "rows"},
		{"remote bytes read 307", "SELECT * FROM remote('127.0.0.2', system.query_log) SETTINGS max_bytes_to_read=1, prefer_localhost_replica=0", 400, "bytes"},
		{"remote result rows 70", "SELECT number FROM remote('127.0.0.2', system.numbers) LIMIT 100 SETTINGS max_result_rows=1, result_overflow_mode='throw', prefer_localhost_replica=0", 400, "max rows"},
		{"time literal in query", "SELECT missing_query_field FROM system.numbers WHERE toDateTime('2026-10-01 10:00:00', 'UTC') < now()", 400, "missing_query_field"},
		{"ip literal in query", "SELECT missing_query_field FROM system.numbers WHERE toString(number) = '10.0.0.1'", 400, "missing_query_field"},
		{"remote cast 70", "SELECT accurateCast(number, 'UInt8') FROM remote('127.0.0.2', system.numbers) LIMIT 1000 SETTINGS prefer_localhost_replica=0", 400, "accurateCast"},
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
			for _, detail := range []string{"127.0.0.2", "Received from", "Stack trace:", "While executing Remote"} {
				if strings.Contains(got.message, detail) {
					t.Errorf("message leaks %q", detail)
				}
			}
			if got.status != tc.status || !strings.Contains(got.message, tc.reason) {
				t.Errorf("want %d with %q", tc.status, tc.reason)
			}
			if count := strings.Count(logs.String(), "level=ERROR"); count > 1 {
				t.Errorf("failure logged %d times, want at most once", count)
			}
		})
	}
}

func TestQueryTransportTimeoutResponses(t *testing.T) {
	stop := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-stop
	}))
	t.Cleanup(backend.Close)
	t.Cleanup(func() { close(stop) })

	dialer := net.Dialer{Deadline: time.Now().Add(-time.Second)}
	conn, dialErr := dialer.DialContext(t.Context(), "tcp", backend.Listener.Addr().String())
	if conn != nil {
		_ = conn.Close()
	}
	var netErr net.Error
	if !errors.As(dialErr, &netErr) || !netErr.Timeout() {
		t.Fatalf("want real dial timeout, got %v", dialErr)
	}
	client := &http.Client{Timeout: 100 * time.Millisecond, Transport: &http.Transport{}}
	t.Cleanup(client.CloseIdleConnections)
	resp, httpErr := client.Get(backend.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.As(httpErr, &netErr) || !netErr.Timeout() {
		t.Fatalf("want HTTP client timeout, got %v", httpErr)
	}
	queryCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	queryRequest, err := http.NewRequestWithContext(queryCtx, http.MethodGet, backend.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	queryClient := &http.Client{Transport: &http.Transport{}}
	t.Cleanup(queryClient.CloseIdleConnections)
	queryResponse, queryErr := queryClient.Do(queryRequest)
	if queryResponse != nil {
		_ = queryResponse.Body.Close()
	}
	if !errors.Is(queryErr, context.DeadlineExceeded) {
		t.Fatalf("want HTTP query deadline, got %v", queryErr)
	}
	for _, tc := range []struct {
		name              string
		err               error
		status, errorLogs int
		message           string
	}{
		{"dial", dialErr, 500, 1, logsQueryFailure},
		{"http client waiting for response", httpErr, 500, 1, logsQueryFailure},
		{"query deadline", fmt.Errorf("query: %w", context.DeadlineExceeded), 408, 0, "Request timed out"},
		{"http query deadline", queryErr, 408, 0, "Request timed out"},
		{"ClickHouse execution deadline", &ch.Exception{Code: 159}, 408, 0, "Request timed out"},
		{"ClickHouse socket timeout", &ch.Exception{Code: 209}, 408, 0, "Request timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			s := &Server{log: log}
			app := fiber.New()
			app.Use(requestLogger(log))
			app.Post("/query", func(c fiber.Ctx) error { return s.sendQueryError(c, 1, tc.err, logsQueryFailure, logsQueryOperation) })
			req := httptest.NewRequest(http.MethodPost, "/query", http.NoBody)
			response, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body struct {
				Message string `json:"message"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Message != tc.message {
				t.Errorf("message=%q, want %q", body.Message, tc.message)
			}
			count := strings.Count(logs.String(), "level=ERROR")
			t.Logf("status=%d error_logs=%d cause=%v", response.StatusCode, count, tc.err)
			if response.StatusCode != tc.status || count != tc.errorLogs {
				t.Fatalf("want status=%d error_logs=%d\n%s", tc.status, tc.errorLogs, logs.String())
			}
		})
	}
}

// syncLogBuffer is safe for the background health check to write while a test reads it.
type syncLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncLogBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}
