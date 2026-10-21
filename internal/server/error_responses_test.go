package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/config"
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
			status: fiber.StatusBadRequest, message: "Invalid query or query parameters",
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
