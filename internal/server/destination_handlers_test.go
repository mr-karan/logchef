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
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// newDestinationEnv is a full server whose alerts manager tests webhook
// destinations through webhook, with alerting enabled.
func newDestinationEnv(t *testing.T, logger *slog.Logger, webhook notify.Sender) *collectionRouteEnv {
	t.Helper()
	db, err := sqlite.New(context.Background(), sqlite.Options{Logger: logger, Config: config.SQLiteConfig{Path: sqlitetest.Path(t)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Test-only HMAC key for hashing generated API tokens; not a credential.
	cfg := &config.Config{Auth: config.AuthConfig{APITokenSecret: strings.Repeat("t", 32)}, Alerts: config.AlertsConfig{Enabled: true}}
	s := New(ServerOptions{
		Config:        cfg,
		SQLite:        db,
		Audit:         testAuditRecorder(t, db),
		AlertsManager: alerts.NewManager(alerts.Options{DB: db, Logger: logger, Senders: map[models.DestinationType]notify.Sender{models.DestinationTypeWebhook: webhook}}),
		FS:            http.FS(fstest.MapFS{"index.html": {Data: []byte(`<base href="/" />`)}}),
		Logger:        logger,
	})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return &collectionRouteEnv{t: t, s: s, cfg: cfg}
}

const destinationsPath = "/api/v1/notification-destinations"

func destinationID(t *testing.T, data json.RawMessage) models.DestinationID {
	t.Helper()
	var d models.NotificationDestination
	if err := json.Unmarshal(data, &d); err != nil || d.ID == 0 {
		t.Fatalf("destination response %s: %v", data, err)
	}
	return d.ID
}

// TestDestinationAccess walks the access table of RFC 0006: who may create,
// see, edit, delete and use a private, team or global destination.
func TestDestinationAccess(t *testing.T) {
	e := newDestinationEnv(t, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	db := e.s.store
	admin := mkTestUser(t, db, "dest-admin@example.com", models.UserRoleAdmin)
	teamOwner := mkTestUser(t, db, "dest-team-owner@example.com", models.UserRoleMember)
	editor := mkTestUser(t, db, "dest-editor@example.com", models.UserRoleMember)
	member := mkTestUser(t, db, "dest-member@example.com", models.UserRoleMember)
	outsider := mkTestUser(t, db, "dest-outsider@example.com", models.UserRoleMember)
	team, src := mkTestTeam(t, db, "dest-team", teamOwner, editor, member)
	for user, role := range map[*models.User]models.TeamRole{teamOwner: models.TeamRoleAdmin, editor: models.TeamRoleEditor} {
		if err := db.UpdateTeamMemberRole(context.Background(), team.ID, user.ID, role); err != nil {
			t.Fatal(err)
		}
	}
	scopes := []models.TokenScope{models.TokenScopeDestinationsRead, models.TokenScopeDestinationsWrite}
	tok := map[*models.User]string{}
	for _, u := range []*models.User{admin, teamOwner, editor, member, outsider} {
		tok[u] = e.token(u, scopes...)
	}
	webhook := func(name, scope, team string) string {
		return fmt.Sprintf(`{"name":%q,"type":"webhook","scope":%q%s,"secret":"https://hooks.example.com/services/secret-token-%s"}`, name, scope, team, name)
	}
	teamField := fmt.Sprintf(`,"team_id":%d`, team.ID)

	private := destinationID(t, e.expect(tok[member], http.MethodPost, destinationsPath, webhook("member-private", "private", ""), http.StatusCreated))
	e.expect(tok[member], http.MethodPost, destinationsPath, webhook("member-team", "team", teamField), http.StatusForbidden)
	teamDest := destinationID(t, e.expect(tok[editor], http.MethodPost, destinationsPath, webhook("editor-team", "team", teamField), http.StatusCreated))
	e.expect(tok[outsider], http.MethodPost, destinationsPath, webhook("outsider-team", "team", teamField), http.StatusForbidden)
	e.expect(tok[member], http.MethodPost, destinationsPath, webhook("member-global", "global", ""), http.StatusForbidden)
	global := destinationID(t, e.expect(tok[admin], http.MethodPost, destinationsPath, webhook("admin-global", "global", ""), http.StatusCreated))

	listed := func(u *models.User) map[models.DestinationID]bool {
		var list []models.NotificationDestination
		if err := json.Unmarshal(e.expect(tok[u], http.MethodGet, destinationsPath, "", http.StatusOK), &list); err != nil {
			t.Fatal(err)
		}
		ids := map[models.DestinationID]bool{}
		for _, d := range list {
			ids[d.ID] = true
		}
		return ids
	}
	if ids := listed(member); !ids[private] || !ids[teamDest] || !ids[global] {
		t.Errorf("member lists %v, want own private, team and global", ids)
	}
	if ids := listed(outsider); ids[private] || ids[teamDest] || !ids[global] {
		t.Errorf("outsider lists %v, want global only", ids)
	}

	rename := func(name, scope, team string) string {
		return fmt.Sprintf(`{"name":%q,"type":"webhook","scope":%q%s,"secret":""}`, name, scope, team)
	}
	at := func(id models.DestinationID) string { return fmt.Sprintf("%s/%d", destinationsPath, id) }
	e.expect(tok[outsider], http.MethodPut, at(private), rename("x", "private", ""), http.StatusForbidden)
	e.expect(tok[member], http.MethodPut, at(teamDest), rename("x", "team", teamField), http.StatusForbidden)
	e.expect(tok[teamOwner], http.MethodPut, at(teamDest), rename("owned by the team", "team", teamField), http.StatusOK)
	e.expect(tok[member], http.MethodPut, at(global), rename("x", "global", ""), http.StatusForbidden)
	e.expect(tok[member], http.MethodPut, at(private), rename("moved", "global", ""), http.StatusForbidden)
	e.expect(tok[admin], http.MethodPut, at(private), rename("fixed by an admin", "private", ""), http.StatusOK)

	// A blank secret on update kept the stored URL.
	stored, err := db.GetNotificationDestination(context.Background(), private)
	if err != nil || stored.Secret != "https://hooks.example.com/services/secret-token-member-private" {
		t.Fatalf("secret after a blank update = %q, %v", stored.Secret, err)
	}

	// Raw addresses need a global admin; Logchef users do not.
	email := func(config string) string {
		return fmt.Sprintf(`{"name":"mail","type":"email","scope":"private","config":%s}`, config)
	}
	e.expect(tok[member], http.MethodPost, destinationsPath, email(`{"addresses":["oncall@example.com"]}`), http.StatusForbidden)
	e.expect(tok[member], http.MethodPost, destinationsPath, email(fmt.Sprintf(`{"user_ids":[%d]}`, outsider.ID)), http.StatusCreated)
	e.expect(tok[admin], http.MethodPost, destinationsPath, email(`{"addresses":["oncall@example.com"]}`), http.StatusCreated)
	e.expect(tok[member], http.MethodPost, destinationsPath, email(`{"user_ids":[999999]}`), http.StatusBadRequest)
	e.expect(tok[member], http.MethodPost, destinationsPath, `{"name":"sms","type":"sms","scope":"private","secret":"+10000000000"}`, http.StatusBadRequest)

	// A destination in use is refused with the count; a free one deletes.
	alert := &models.Alert{SourceID: src.ID, Name: "uses it", QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
		Query: "SELECT 1", LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan, FrequencySeconds: 60,
		Severity: models.AlertSeverityWarning, IsActive: true, LastState: models.AlertStateResolved}
	if err := db.CreateAlert(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAlertDestinations(context.Background(), alert.ID, []models.DestinationID{teamDest}); err != nil {
		t.Fatal(err)
	}
	if status, data := e.do(tok[teamOwner], http.MethodDelete, at(teamDest), ""); status != http.StatusConflict {
		t.Errorf("delete in use = %d %s, want 409", status, data)
	}
	e.expect(tok[outsider], http.MethodDelete, at(private), "", http.StatusForbidden)
	e.expect(tok[member], http.MethodDelete, at(private), "", http.StatusOK)
	e.expect(tok[member], http.MethodDelete, at(private), "", http.StatusNotFound)

	// Scopes: reading needs destinations:read, writing destinations:write.
	readOnly := e.token(member, models.TokenScopeDestinationsRead)
	e.expect(readOnly, http.MethodGet, destinationsPath, "", http.StatusOK)
	e.expect(readOnly, http.MethodPost, destinationsPath, webhook("read-only", "private", ""), http.StatusForbidden)
	e.expect(e.token(member, models.TokenScopeAlertsRead), http.MethodGet, destinationsPath, "", http.StatusForbidden)
}

// TestDestinationSecretNeverInResponse: no response of the destination or
// alert API carries a webhook URL; the hint is the host only.
func TestDestinationSecretNeverInResponse(t *testing.T) {
	e := newDestinationEnv(t, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	admin := mkTestUser(t, e.s.store, "secret-admin@example.com", models.UserRoleAdmin)
	token := e.token(admin, models.TokenScopeDestinationsRead, models.TokenScopeDestinationsWrite)
	const secret = "https://user:pass@hooks.example.com/services/T000/B000/secret-token?key=secret-query"
	responses := []json.RawMessage{
		e.expect(token, http.MethodPost, destinationsPath, fmt.Sprintf(`{"name":"hook","type":"webhook","scope":"global","secret":%q}`, secret), http.StatusCreated),
	}
	id := destinationID(t, responses[0])
	responses = append(responses,
		e.expect(token, http.MethodPut, fmt.Sprintf("%s/%d", destinationsPath, id), fmt.Sprintf(`{"name":"hook 2","scope":"global","type":"webhook","secret":%q}`, secret), http.StatusOK),
		e.expect(token, http.MethodGet, destinationsPath, "", http.StatusOK),
	)
	for _, data := range responses {
		for _, leak := range []string{"secret-token", "secret-query", "user:pass", "/services/"} {
			if bytes.Contains(data, []byte(leak)) {
				t.Errorf("response leaks %q: %s", leak, data)
			}
		}
	}
	var d models.NotificationDestination
	if err := json.Unmarshal(responses[0], &d); err != nil || !d.HasSecret || d.SecretHint != "hooks.example.com" {
		t.Errorf("create response = %+v, %v; want has_secret and the host as hint", d, err)
	}
}

// TestAlertRequestRejectsLegacyTargets: the old target keys are refused with
// a message naming destination_ids, even with an empty value.
func TestAlertRequestRejectsLegacyTargets(t *testing.T) {
	e := newDestinationEnv(t, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	user := mkTestUser(t, e.s.store, "legacy-alerts@example.com", models.UserRoleAdmin)
	token := e.token(user, models.TokenScopeAlertsWrite)
	for _, body := range []string{`{"source_id":1,"recipient_user_ids":[]}`, `{"source_id":1,"webhook_urls":null}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := e.s.app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "destination_ids") {
			t.Errorf("%s = %d %s, want 400 naming destination_ids", body, resp.StatusCode, raw)
		}
	}
}

type destinationTestResponse struct {
	OK      bool                           `json:"ok"`
	Results []models.DestinationTestResult `json:"results"`
}

func testUnsavedWebhook(t *testing.T, e *collectionRouteEnv, token, url string) (status int, out destinationTestResponse, raw string) {
	t.Helper()
	body := fmt.Sprintf(`{"name":"probe","type":"webhook","scope":"private","secret":%q}`, url)
	status, data := e.do(token, http.MethodPost, destinationsPath+"/test", body)
	_ = json.Unmarshal(data, &out)
	return status, out, string(data)
}

// TestDestinationTestHonorsAllowedHosts: a test send to a webhook goes
// through the webhook dial policy, keeps one connection, and never puts the
// resolved address or the URL's credentials in a response or a log.
func TestDestinationTestHonorsAllowedHosts(t *testing.T) {
	for _, tc := range []struct {
		name         string
		allowedHosts []string
		wantOK       bool
		wantRequests int32
		malformed    bool
	}{
		{name: "malformed status error bounded", allowedHosts: []string{"127.0.0.1"}, wantRequests: 1, malformed: true},
		{name: "loopback denied by default"},
		{name: "unrelated allowlist does not permit loopback", allowedHosts: []string{"example.com"}},
		{name: "allowlisted loopback permitted", allowedHosts: []string{"127.0.0.1"}, wantOK: true, wantRequests: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests, connections atomic.Int32
			receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if tc.malformed {
					connection, buffer, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer connection.Close()
					_, _ = buffer.WriteString("HTTP/1.1 " + strings.Repeat("x", 10000) + "\r\n\r\n")
					_ = buffer.Flush()
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			receiver.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			receiver.Start()
			t.Cleanup(receiver.Close)

			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			allowlist, err := config.ParseWebhookAllowlist(tc.allowedHosts)
			if err != nil {
				t.Fatal(err)
			}
			e := newDestinationEnv(t, logger, notify.NewWebhookSender(notify.WebhookSenderOptions{Logger: logger, AllowedHosts: allowlist}))
			user := mkTestUser(t, e.s.store, "probe@example.com", models.UserRoleMember)
			token := e.token(user, models.TokenScopeDestinationsWrite)
			url := strings.Replace(receiver.URL, "://", "://user:secret-userinfo@", 1) + "/receiver?token=secret-query"
			for range 2 {
				status, out, raw := testUnsavedWebhook(t, e, token, url)
				if status != http.StatusOK || out.OK != tc.wantOK || len(out.Results) != 1 {
					t.Fatalf("test send = %d %s, want 200 with ok=%t", status, raw, tc.wantOK)
				}
				if tc.malformed && (len(raw) > 1024 || strings.Contains(raw, strings.Repeat("x", 100))) {
					t.Errorf("response retained malformed-status parser text, length %d", len(raw))
				}
				if tc.wantRequests == 0 && (strings.Contains(raw, "127.0.0.1") || !strings.Contains(raw, "not allowed")) {
					t.Errorf("blocked send response = %s, want a fixed denial without the address", raw)
				}
				if strings.Contains(raw, "secret-userinfo") || strings.Contains(raw, "secret-query") {
					t.Errorf("response leaks the URL: %s", raw)
				}
			}
			if strings.Contains(logs.String(), "secret-userinfo") || strings.Contains(logs.String(), "secret-query") || strings.Contains(logs.String(), "/receiver") {
				t.Errorf("webhook URL credentials leaked in logs: %s", logs.String())
			}
			if tc.wantRequests == 0 && strings.Count(logs.String(), `msg="webhook destination blocked by policy"`) != 2 {
				t.Errorf("denial logs: %s", logs.String())
			}
			if tc.wantOK && connections.Load() != 1 {
				t.Errorf("test sends used %d connections, want one shared connection", connections.Load())
			}
			if got := requests.Load(); got != 2*tc.wantRequests {
				t.Errorf("webhook requests = %d, want %d", got, 2*tc.wantRequests)
			}
		})
	}
}

func TestDestinationTestTransportErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"TLS", "TLS verification failed"},
		{"refused", "connection refused"},
		{"timeout", "request timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.name == "timeout" {
					<-release
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			if tc.name == "TLS" {
				receiver.StartTLS()
			} else {
				receiver.Start()
			}
			defer receiver.Close()
			defer close(release)
			if tc.name == "refused" {
				receiver.Close()
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			allowlist, err := config.ParseWebhookAllowlist([]string{"127.0.0.1"})
			if err != nil {
				t.Fatal(err)
			}
			timeout := 10 * time.Second
			if tc.name == "timeout" {
				timeout = 30 * time.Millisecond
			}
			e := newDestinationEnv(t, logger, notify.NewWebhookSender(notify.WebhookSenderOptions{Logger: logger, AllowedHosts: allowlist, Timeout: timeout}))
			user := mkTestUser(t, e.s.store, "probe@example.com", models.UserRoleMember)
			status, out, raw := testUnsavedWebhook(t, e, e.token(user, models.TokenScopeDestinationsWrite), receiver.URL)
			if status != http.StatusOK || out.OK || len(out.Results) != 1 || !strings.Contains(out.Results[0].Error, tc.want) || strings.Contains(raw, "127.0.0.1") {
				t.Fatalf("test send = %d %s, want a fixed %q without the address", status, raw, tc.want)
			}
		})
	}
}

// senderFunc is a sender that never leaves the process.
type senderFunc func(dest *models.NotificationDestination) error

func (f senderFunc) Send(_ context.Context, dest *models.NotificationDestination, _ string, _ int64, _ notify.Message) error {
	return f(dest)
}

// TestSlackDestinationURLChecks: a Slack destination saves only with an https
// Slack incoming webhook URL, and has no settings besides it.
func TestSlackDestinationURLChecks(t *testing.T) {
	e := newDestinationEnv(t, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	user := mkTestUser(t, e.s.store, "slack-member@example.com", models.UserRoleMember)
	token := e.token(user, models.TokenScopeDestinationsRead, models.TokenScopeDestinationsWrite)
	slack := func(secret, config string) string {
		return fmt.Sprintf(`{"name":"ops","type":"slack","scope":"private","secret":%q,"config":%s}`, secret, config)
	}
	for _, url := range []string{
		"https://hooks.example.com/services/T000/B000/XXXX",
		"https://hooks.slack.com.example.com/services/T000/B000/XXXX",
		"http://hooks.slack.com/services/T000/B000/XXXX",
		"https://hooks.slack.com:8443/services/T000/B000/XXXX",
		"",
	} {
		e.expect(token, http.MethodPost, destinationsPath, slack(url, "{}"), http.StatusBadRequest)
	}
	e.expect(token, http.MethodPost, destinationsPath, slack("https://hooks.slack.com/services/T000/B000/XXXX", `{"payload_version":1}`), http.StatusBadRequest)
	e.expect(token, http.MethodPost, destinationsPath, slack("https://hooks.slack.com/services/T000/B000/XXXX", `{"user_ids":[1]}`), http.StatusBadRequest)

	id := destinationID(t, e.expect(token, http.MethodPost, destinationsPath, slack("https://hooks.slack-gov.com/services/T000/B000/XXXX", "{}"), http.StatusCreated))
	at := fmt.Sprintf("%s/%d", destinationsPath, id)
	// A blank URL keeps the stored one; another host is refused on update too.
	e.expect(token, http.MethodPut, at, `{"name":"ops 2","type":"slack","scope":"private","secret":""}`, http.StatusOK)
	e.expect(token, http.MethodPut, at, `{"name":"ops 2","type":"slack","scope":"private","secret":"https://example.com/hook"}`, http.StatusBadRequest)
	stored, err := e.s.store.GetNotificationDestination(context.Background(), id)
	if err != nil || stored.Secret != "https://hooks.slack-gov.com/services/T000/B000/XXXX" || stored.Name != "ops 2" {
		t.Fatalf("stored = %+v, %v; want the first URL kept", stored, err)
	}
}

// TestSlackDestinationSecretNeverLeaks: the Slack webhook URL reaches the
// sender and nothing else: no API response, no stored audit event and no
// log line holds it.
func TestSlackDestinationSecretNeverLeaks(t *testing.T) {
	const (
		first  = "https://hooks.slack.com/services/T0LEAKTEAM/B0LEAKHOOK/leak-token-first"
		second = "https://hooks.slack.com/services/T0LEAKTEAM/B0LEAKHOOK/leak-token-second"
	)
	sent := make(chan string, 8)
	sender := senderFunc(func(dest *models.NotificationDestination) error {
		sent <- dest.Secret
		if dest.Name == "failing" {
			return notify.Permanent(errors.New("slack delivery failed: status 404: no_service"))
		}
		return nil
	})
	e := newAuditRouteEnv(t, withAuditLogLines(), withConfig(func(c *config.Config) { c.Alerts.Enabled = true }), func(s *auditEnvSetup) {
		s.opts.AlertsManager = alerts.NewManager(alerts.Options{DB: s.opts.SQLite, Logger: s.opts.Logger, Senders: map[models.DestinationType]notify.Sender{models.DestinationTypeSlack: sender}})
	})
	created := e.do(http.MethodPost, destinationsPath, fmt.Sprintf(`{"name":"ops","type":"slack","scope":"global","secret":%q}`, first), e.cookie)
	wantStatus(t, created, http.StatusCreated)
	var envelope struct {
		Data models.NotificationDestination `json:"data"`
	}
	err := json.Unmarshal(created.body, &envelope)
	if d := envelope.Data; err != nil || !d.HasSecret || d.SecretHint != "hooks.slack.com" {
		t.Fatalf("create response %s: %v; want has_secret and the host as hint", created.body, err)
	}
	d := envelope.Data
	at := fmt.Sprintf("%s/%d", destinationsPath, d.ID)
	responses := []auditResp{
		created,
		e.do(http.MethodPut, at, fmt.Sprintf(`{"name":"ops","type":"slack","scope":"global","secret":%q}`, second), e.cookie),
		e.do(http.MethodGet, destinationsPath, "", e.cookie),
		e.do(http.MethodPost, destinationsPath+"/test", fmt.Sprintf(`{"id":%d}`, d.ID), e.cookie),
		e.do(http.MethodPost, destinationsPath+"/test", fmt.Sprintf(`{"name":"failing","type":"slack","scope":"global","secret":%q}`, first), e.cookie),
		e.do(http.MethodDelete, at, "", e.cookie),
	}
	for i, r := range responses {
		if r.status >= 300 {
			t.Errorf("request %d = %d %s", i, r.status, r.body)
		}
		for _, leak := range []string{"leak-token", "T0LEAKTEAM", "/services/"} {
			if bytes.Contains(r.body, []byte(leak)) {
				t.Errorf("response %d leaks %q: %s", i, leak, r.body)
			}
		}
	}
	if got := []string{<-sent, <-sent}; got[0] != second || got[1] != first {
		t.Errorf("sender received %q, want the stored URL, then the unsaved one", got)
	}
	if !bytes.Contains(responses[4].body, []byte("no_service")) {
		t.Errorf("failed test response = %s, want the Slack error", responses[4].body)
	}

	events := e.events()
	if len(events) != 5 {
		t.Fatalf("stored %d audit events, want create, update, two tests and delete", len(events))
	}
	wantSecretChanged(t, changesOf(t, events[1]), "secret")
	e.wantNoText("leak-token", "T0LEAKTEAM", "/services/")
}

// TestPagerDutyDestinationKeyChecks: a PagerDuty destination saves only with
// a 32-character routing key, and has no settings besides it.
func TestPagerDutyDestinationKeyChecks(t *testing.T) {
	e := newDestinationEnv(t, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	user := mkTestUser(t, e.s.store, "pd-member@example.com", models.UserRoleMember)
	token := e.token(user, models.TokenScopeDestinationsRead, models.TokenScopeDestinationsWrite)
	pd := func(secret, config string) string {
		return fmt.Sprintf(`{"name":"on-call","type":"pagerduty","scope":"private","secret":%q,"config":%s}`, secret, config)
	}
	const key = "0123456789abcdef0123456789abcdef"
	for _, bad := range []string{"0123456789abcdef", "https://events.pagerduty.com/v2/enqueue", key + "0", ""} {
		e.expect(token, http.MethodPost, destinationsPath, pd(bad, "{}"), http.StatusBadRequest)
	}
	e.expect(token, http.MethodPost, destinationsPath, pd(key, `{"payload_version":1}`), http.StatusBadRequest)
	e.expect(token, http.MethodPost, destinationsPath, pd(key, `{"addresses":["a@example.com"]}`), http.StatusBadRequest)

	created := e.expect(token, http.MethodPost, destinationsPath, pd(key, "{}"), http.StatusCreated)
	var d models.NotificationDestination
	if err := json.Unmarshal(created, &d); err != nil || !d.HasSecret || d.SecretHint != "…cdef" {
		t.Fatalf("create response %s, %v; want the last four characters as hint", created, err)
	}
	at := fmt.Sprintf("%s/%d", destinationsPath, d.ID)
	// A blank key keeps the stored one; a bad key is refused on update too.
	e.expect(token, http.MethodPut, at, `{"name":"on-call 2","type":"pagerduty","scope":"private","secret":""}`, http.StatusOK)
	e.expect(token, http.MethodPut, at, `{"name":"on-call 2","type":"pagerduty","scope":"private","secret":"short"}`, http.StatusBadRequest)
	stored, err := e.s.store.GetNotificationDestination(context.Background(), d.ID)
	if err != nil || stored.Secret != key || stored.Name != "on-call 2" {
		t.Fatalf("stored = %+v, %v; want the first key kept", stored, err)
	}
}

// TestPagerDutyDestinationSecretNeverLeaks: the routing key reaches the
// sender and nothing else: no API response, no stored audit event and no
// log line holds it.
func TestPagerDutyDestinationSecretNeverLeaks(t *testing.T) {
	const (
		first  = "LEAKKEYFIRST0123456789abcdefABCD"
		second = "LEAKKEYSECOND0123456789abcdefABC"
	)
	sent := make(chan string, 8)
	sender := senderFunc(func(dest *models.NotificationDestination) error {
		sent <- dest.Secret
		if dest.Name == "failing" {
			return notify.Permanent(errors.New("pagerduty delivery failed: status 400: Event object is invalid"))
		}
		return nil
	})
	e := newAuditRouteEnv(t, withAuditLogLines(), withConfig(func(c *config.Config) { c.Alerts.Enabled = true }), func(s *auditEnvSetup) {
		s.opts.AlertsManager = alerts.NewManager(alerts.Options{DB: s.opts.SQLite, Logger: s.opts.Logger, Senders: map[models.DestinationType]notify.Sender{models.DestinationTypePagerDuty: sender}})
	})
	created := e.do(http.MethodPost, destinationsPath, fmt.Sprintf(`{"name":"on-call","type":"pagerduty","scope":"global","secret":%q}`, first), e.cookie)
	wantStatus(t, created, http.StatusCreated)
	var envelope struct {
		Data models.NotificationDestination `json:"data"`
	}
	err := json.Unmarshal(created.body, &envelope)
	if d := envelope.Data; err != nil || !d.HasSecret || d.SecretHint != "…ABCD" {
		t.Fatalf("create response %s: %v; want has_secret and the last four characters as hint", created.body, err)
	}
	at := fmt.Sprintf("%s/%d", destinationsPath, envelope.Data.ID)
	responses := []auditResp{
		created,
		e.do(http.MethodPut, at, fmt.Sprintf(`{"name":"on-call","type":"pagerduty","scope":"global","secret":%q}`, second), e.cookie),
		e.do(http.MethodGet, destinationsPath, "", e.cookie),
		e.do(http.MethodPost, destinationsPath+"/test", fmt.Sprintf(`{"id":%d}`, envelope.Data.ID), e.cookie),
		e.do(http.MethodPost, destinationsPath+"/test", fmt.Sprintf(`{"name":"failing","type":"pagerduty","scope":"global","secret":%q}`, first), e.cookie),
		e.do(http.MethodDelete, at, "", e.cookie),
	}
	for i, r := range responses {
		if r.status >= 300 {
			t.Errorf("request %d = %d %s", i, r.status, r.body)
		}
		if bytes.Contains(r.body, []byte("LEAKKEY")) {
			t.Errorf("response %d leaks the routing key: %s", i, r.body)
		}
	}
	if got := []string{<-sent, <-sent}; got[0] != second || got[1] != first {
		t.Errorf("sender received %q, want the stored key, then the unsaved one", got)
	}
	if !bytes.Contains(responses[4].body, []byte("Event object is invalid")) {
		t.Errorf("failed test response = %s, want the PagerDuty error", responses[4].body)
	}
	events := e.events()
	if len(events) != 5 {
		t.Fatalf("stored %d audit events, want create, update, two tests and delete", len(events))
	}
	wantSecretChanged(t, changesOf(t, events[1]), "secret")
	e.wantNoText("LEAKKEY")
}
