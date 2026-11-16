package server

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/oauth"
	"github.com/mr-karan/logchef/pkg/models"
)

// eventsOf returns the stored events of one action, oldest first.
func eventsOf(t *testing.T, e *oauthEnv, action audit.Action) []models.AuditEvent {
	t.Helper()
	got, err := e.db.ListAuditEvents(context.Background(), models.AuditFilter{Ascending: true, Limit: 1000, Action: string(action)})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (e *oauthEnv) postRevoke(form url.Values) int {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, testIssuer+oauth.RevokePath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "logchef-cli/test")
	return e.do(req).StatusCode
}

// checkGrantRevoke checks an oauth_grant.revoke event for the CLI grant.
func checkGrantRevoke(t *testing.T, ev models.AuditEvent, grantID models.OAuthGrantID, cause models.OAuthRevokeReason, method, route string, status int) {
	t.Helper()
	if ev.Action != string(audit.OAuthGrantRevoke) || ev.Outcome != models.AuditOutcomeSuccess ||
		ev.TargetType != "oauth_grant" || ev.TargetID != strconv.Itoa(int(grantID)) || ev.Route != route || ev.HTTPMethod != method || ev.Status != status {
		t.Errorf("revoke event %+v", ev)
	}
	if got := detailOf(t, ev)["cause"]; got != string(cause) {
		t.Errorf("detail.cause %v, want %s", got, cause)
	}
	if got := changesOf(t, ev); got["client_id"]["from"] != config.OAuthCLIClientID || got["resource"]["from"] != "api" {
		t.Errorf("changes %s, want the grant's client and resource", ev.Changes)
	}
}

func TestAuditOAuthRevokeEndpointRecordsOnce(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	tokens := e.tokens(cliAuthorizeParams(p), p)
	grantID := e.connectedApps(e.session)[0].ID

	revoke := url.Values{"client_id": {config.OAuthCLIClientID}, "token": {tokens.RefreshToken}, "token_type_hint": {"refresh_token"}}
	if status := e.postRevoke(revoke); status != http.StatusOK {
		t.Fatalf("revoke status %d", status)
	}
	got := eventsOf(t, e, audit.OAuthGrantRevoke)
	if len(got) != 1 {
		t.Fatalf("stored %d revoke events, want 1: %+v", len(got), got)
	}
	ev := got[0]
	checkGrantRevoke(t, ev, grantID, models.OAuthRevokeTokenEndpoint, http.MethodPost, oauth.RevokePath, http.StatusOK)
	if ev.ActorType != models.AuditActorUser || ev.ActorID == nil || *ev.ActorID != e.user.ID || ev.ActorLabel != e.user.Email || ev.ClientID != config.OAuthCLIClientID ||
		ev.AuthMethod != models.AuditAuthOAuthAPI || ev.CredentialID != strconv.Itoa(int(grantID)) || ev.IP == "" || ev.UserAgent != "logchef-cli/test" {
		t.Errorf("revoke actor %+v", ev)
	}
	if strings.Contains(string(ev.Changes)+string(ev.Detail), tokens.RefreshToken) {
		t.Error("the revoke event holds the refresh token")
	}

	// The grant is already revoked, and the token is unknown: both answer
	// 200 and record nothing.
	if status := e.postRevoke(revoke); status != http.StatusOK {
		t.Fatalf("second revoke status %d", status)
	}
	unknown := url.Values{"client_id": {config.OAuthCLIClientID}, "token": {strings.Repeat("A", 43)}, "token_type_hint": {"refresh_token"}}
	if status := e.postRevoke(unknown); status != http.StatusOK {
		t.Fatalf("unknown token revoke status %d", status)
	}
	if got := eventsOf(t, e, audit.OAuthGrantRevoke); len(got) != 1 {
		t.Errorf("stored %d revoke events after the repeats, want 1", len(got))
	}
}

func TestAuditOAuthCodeReplayRecordsOnce(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := cliAuthorizeParams(p)
	code := e.code(params)
	if status, out := e.postToken(codeExchangeForm(params, code, p.verifier)); status != http.StatusOK {
		t.Fatalf("code exchange status %d error %q", status, out.Error)
	}
	grantID := e.connectedApps(e.session)[0].ID
	if got := eventsOf(t, e, audit.OAuthGrantRevoke); len(got) != 0 {
		t.Fatalf("a code exchange stored %d revoke events", len(got))
	}

	for range 2 {
		if status, out := e.postToken(codeExchangeForm(params, code, p.verifier)); status != http.StatusBadRequest || out.Error != "invalid_grant" {
			t.Fatalf("replayed code status %d error %q", status, out.Error)
		}
	}
	got := eventsOf(t, e, audit.OAuthGrantRevoke)
	if len(got) != 1 {
		t.Fatalf("stored %d revoke events after two replays, want 1: %+v", len(got), got)
	}
	checkReplayEvent(t, got[0], grantID, models.OAuthRevokeCodeReplay)
}

func TestAuditOAuthRefreshReplayRecordsOnce(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := cliAuthorizeParams(p)
	tokens := e.tokens(params, p)
	grantID := e.connectedApps(e.session)[0].ID
	if status, out := e.postToken(refreshForm(params, tokens.RefreshToken)); status != http.StatusOK {
		t.Fatalf("refresh status %d error %q", status, out.Error)
	}

	for range 2 {
		if status, out := e.postToken(refreshForm(params, tokens.RefreshToken)); status != http.StatusBadRequest || out.Error != "invalid_grant" {
			t.Fatalf("replayed refresh status %d error %q", status, out.Error)
		}
	}
	got := eventsOf(t, e, audit.OAuthGrantRevoke)
	if len(got) != 1 {
		t.Fatalf("stored %d revoke events after two replays, want 1: %+v", len(got), got)
	}
	checkReplayEvent(t, got[0], grantID, models.OAuthRevokeRefreshReplay)
}

// checkReplayEvent checks a revocation that Logchef made: the system acted,
// so the event has no user and no IP.
func checkReplayEvent(t *testing.T, ev models.AuditEvent, grantID models.OAuthGrantID, cause models.OAuthRevokeReason) {
	t.Helper()
	checkGrantRevoke(t, ev, grantID, cause, http.MethodPost, oauth.TokenPath, http.StatusBadRequest)
	if ev.ActorType != models.AuditActorSystem || ev.ActorID != nil || ev.ActorLabel != "oauth" || ev.AuthMethod != models.AuditAuthNone || ev.IP != "" || ev.ClientID != config.OAuthCLIClientID {
		t.Errorf("replay actor %s %v %q %s ip %q, want system oauth", ev.ActorType, ev.ActorID, ev.ActorLabel, ev.AuthMethod, ev.IP)
	}
}

func TestAuditOAuthConsentAndConnectedAppFields(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := cliAuthorizeParams(p)
	params.Set("scope", "logs:read offline_access")

	e.decide(e.startRequest(params), e.session, false)
	deny := eventsOf(t, e, audit.OAuthConsentDeny)
	if len(deny) != 1 {
		t.Fatalf("stored %d deny events, want 1", len(deny))
	}
	if c := changesOf(t, deny[0]); c["client_id"]["to"] != config.OAuthCLIClientID || c["resource"]["to"] != "api" ||
		!slices.Equal(c["scopes"]["to"].([]any), []any{"logs:read"}) || c["offline_access"]["to"] != true || c["grant_id"]["to"] != nil {
		t.Errorf("deny changes %s", deny[0].Changes)
	}

	e.code(params)
	grantID := e.connectedApps(e.session)[0].ID
	approve := eventsOf(t, e, audit.OAuthConsentApprove)
	if len(approve) != 1 {
		t.Fatalf("stored %d approve events, want 1", len(approve))
	}
	if c := changesOf(t, approve[0]); c["grant_id"]["to"] != float64(grantID) || c["client_id"]["to"] != config.OAuthCLIClientID {
		t.Errorf("approve changes %s, want grant %d", approve[0].Changes, grantID)
	}

	path := "/api/v1/me/connected-apps/" + strconv.Itoa(int(grantID))
	if resp := e.consentRequest(http.MethodDelete, path, e.session, "", map[string]string{"Origin": testIssuer}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status %d", resp.StatusCode)
	}
	got := eventsOf(t, e, audit.OAuthGrantRevoke)
	if len(got) != 1 {
		t.Fatalf("stored %d revoke events, want 1", len(got))
	}
	checkGrantRevoke(t, got[0], grantID, models.OAuthRevokeUser, http.MethodDelete, "/api/v1/me/connected-apps/:grantID", http.StatusNoContent)
	if got[0].ActorID == nil || *got[0].ActorID != e.user.ID || got[0].AuthMethod != models.AuditAuthSession {
		t.Errorf("connected-app revoke actor %+v", got[0])
	}
}

// TestAuditFailedSignInsAreBounded sends one failed sign-in more than the
// bound allows in a minute: the extra one reaches the log line only.
func TestAuditFailedSignInsAreBounded(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	lines := &syncLogBuffer{}
	e.s.audit = audit.New(audit.Options{
		Store:     strictAuditStore{AuditStore: e.db, t: t},
		Logger:    slog.New(slog.NewJSONHandler(lines, nil)),
		LogEvents: true,
	})
	// An email over the length limit fails before the password check and
	// the login rate limiter, so the requests stay cheap.
	body := `{"email":"` + strings.Repeat("a", models.MaxEmailLength) + `@example.com","password":"wrong"}`
	for range failedSignInEventsPerMinute + 1 {
		wantStatus(t, e.do(http.MethodPost, "/api/v1/auth/local/login", body, nil), http.StatusUnauthorized)
	}

	if got := e.events(); len(got) != failedSignInEventsPerMinute {
		t.Errorf("stored %d events, want %d", len(got), failedSignInEventsPerMinute)
	}
	stored := map[bool]int{}
	sc := bufio.NewScanner(strings.NewReader(lines.String()))
	for sc.Scan() {
		var line struct {
			Msg    string `json:"msg"`
			Action string `json:"action"`
			Stored bool   `json:"stored"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		if line.Msg == "audit" && line.Action == string(audit.SessionLogin) {
			stored[line.Stored]++
		}
	}
	if stored[true] != failedSignInEventsPerMinute || stored[false] != 1 {
		t.Errorf("audit lines stored true %d, false %d; want %d and 1", stored[true], stored[false], failedSignInEventsPerMinute)
	}
}
