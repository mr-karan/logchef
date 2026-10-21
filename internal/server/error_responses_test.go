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

// internalDetail stands in for driver and upstream text (hosts, ports,
// credentials) that must never reach a client.
const internalDetail = "dial tcp 10.9.8.7:9000: password for user reader rejected"

// newErrorResponseServer builds the routed server with a VictoriaLogs source
// whose upstream is upstream, and returns a global admin's session cookie.
func newErrorResponseServer(t *testing.T, upstream http.HandlerFunc) (*Server, *http.Cookie, *models.Team, models.SourceID) {
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
	if err := db.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.AddTeamMember(ctx, team.ID, admin.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	s.alertsManager = alerts.NewManager(alerts.Options{DB: db, Logger: s.log})
	return s, sessionCookie(t, db, admin), team, src.ID
}

// vlUpstream answers like VictoriaLogs: a 400 with a plain-text reason for a
// request it cannot parse. Hits with a 1m step get a proxy's HTML error page,
// and other queries fail with internal detail in the body.
func vlUpstream(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	switch {
	case r.URL.Path == "/select/logsql/hits" && r.Form.Get("step") == "2x":
		http.Error(w, "cannot parse duration from the arg 'step=2x'", http.StatusBadRequest)
	case r.URL.Path == "/select/logsql/hits":
		_, _ = io.WriteString(w, "<html>bad gateway "+internalDetail+"</html>")
	case r.URL.Path == "/select/logsql/query", r.URL.Path == "/select/logsql/stats_query", r.URL.Path == "/select/logsql/stream_field_names":
		http.Error(w, internalDetail, http.StatusBadGateway)
	default:
		_, _ = io.WriteString(w, `{"values":[]}`)
	}
}

func TestErrorResponses(t *testing.T) {
	s, cookie, team, sourceID := newErrorResponseServer(t, vlUpstream)
	teamPath := fmt.Sprintf("/api/v1/teams/%d", team.ID)
	quietAlert := &models.Alert{
		SourceID: sourceID, Name: "quiet", QueryLanguage: models.QueryLanguageLogsQL, EditorMode: models.AlertEditorModeNative,
		Query: "* | stats count() c", LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan,
		FrequencySeconds: 60, Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
	}
	if err := s.sqlite.CreateAlert(context.Background(), quietAlert); err != nil {
		t.Fatal(err)
	}
	sourcePath := fmt.Sprintf("%s/sources/%d", teamPath, sourceID)
	const window = `"start_time":"2026-10-01T00:00:00Z","end_time":"2026-10-01T01:00:00Z"`

	for _, tc := range []struct {
		name         string
		method, path string
		body         string
		status       int
		message      string // exact message the client must see
		contains     string // or a fragment of it, when the producer owns the wording
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
			status: fiber.StatusBadRequest, contains: "cannot parse duration from the arg 'step=2x'",
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
