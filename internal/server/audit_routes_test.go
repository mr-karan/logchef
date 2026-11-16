package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/oauth"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// auditExemption is a mutating route that records no audit event.
type auditExemption struct{ method, path, reason string }

const teamSourcePrefix = "/api/v1/teams/:teamID/sources/:sourceID"

// auditExemptRoutes lists every POST, PUT, PATCH and DELETE route without an
// audit action, and why (RFC 0009, "Every mutating route and its event").
var auditExemptRoutes = []auditExemption{
	{http.MethodPut, "/api/v1/me/preferences", "UI preferences of the caller"},
	{http.MethodPost, "/api/v1/admin/sources/validate", "tests a connection, stores nothing"},
	{http.MethodPost, teamSourcePrefix + "/logs/query", "query execution, query history owns it"},
	{http.MethodPost, teamSourcePrefix + "/logs/windowed", "query execution, query history owns it"},
	{http.MethodPost, teamSourcePrefix + "/logs/histogram", "query execution, query history owns it"},
	{http.MethodPost, teamSourcePrefix + "/logs/context", "query execution, query history owns it"},
	{http.MethodPost, teamSourcePrefix + "/logs/query/:queryID/cancel", "cancels the caller's own running query"},
	{http.MethodPost, teamSourcePrefix + "/generate-sql", "returns SQL text, stores nothing"},
	{http.MethodPost, teamSourcePrefix + "/logchefql/translate", "query execution"},
	{http.MethodPost, teamSourcePrefix + "/logchefql/validate", "query execution"},
	{http.MethodPost, teamSourcePrefix + "/logchefql/query", "query execution"},
	{http.MethodPost, "/api/v1" + askChefTurnsPath, "read-only assistant"},
	{http.MethodPost, "/api/v1/alerts/test", "runs a query, stores nothing"},
	{http.MethodPost, oauth.AuthorizePath, "creates a pending request, the decision is the event"},
	{http.MethodPost, oauth.TokenPath, "code exchange and refresh, the grant is the event"},
	{http.MethodPost, oauth.RevokePath, "internal/oauth records oauth_grant.revoke for this route"},
	{http.MethodPost, MCPPath, "every MCP tool reads"},
	{http.MethodDelete, MCPPath, "answers 405, the endpoint is stateless"},
	{http.MethodPost, "/api/v1/reports/:reportID/run", "a report run stays in the run history of RFC 0008"},
	{http.MethodPost, "/api/v1/reports/:reportID/preview", "runs the report's queries, stores nothing but the cooldown"},
}

// auditDecisionProblems names each mutating route that has neither a
// registered action nor an exemption, and each exemption that matches no
// route.
func auditDecisionProblems(routes []fiber.Route, exempt []auditExemption) []string {
	var problems []string
	used := make([]bool, len(exempt))
	for i := range routes {
		r := &routes[i]
		switch r.Method {
		case fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete:
		default:
			continue
		}
		ex := slices.IndexFunc(exempt, func(e auditExemption) bool { return e.method == r.Method && e.path == r.Path })
		_, named := audit.Lookup(r.Name)
		switch {
		case named && ex >= 0:
			problems = append(problems, fmt.Sprintf("%s %s has action %q and an exemption", r.Method, r.Path, r.Name))
		case named:
		case ex >= 0:
			used[ex] = true
		default:
			problems = append(problems, fmt.Sprintf("%s %s has no audit action (name %q) and no exemption", r.Method, r.Path, r.Name))
		}
	}
	for i, e := range exempt {
		if !used[i] {
			problems = append(problems, fmt.Sprintf("exemption %s %s matches no route", e.method, e.path))
		}
	}
	return problems
}

func TestEveryMutatingRouteHasAnAuditDecision(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	routes := e.srv.app.GetRoutes(true)
	for _, p := range auditDecisionProblems(routes, auditExemptRoutes) {
		t.Error(p)
	}

	// Removing one name makes the check fail.
	stripped := slices.Clone(routes)
	i := slices.IndexFunc(stripped, func(r fiber.Route) bool {
		return r.Method == fiber.MethodPut && r.Path == "/api/v1/admin/sources/:sourceID"
	})
	if i < 0 {
		t.Fatal("PUT /api/v1/admin/sources/:sourceID is not registered")
	}
	stripped[i].Name = ""
	if got := auditDecisionProblems(stripped, auditExemptRoutes); len(got) != 1 || !strings.Contains(got[0], "/api/v1/admin/sources/:sourceID has no audit action") {
		t.Errorf("problems after removing a name = %q", got)
	}
	// An exemption that matches no route fails too.
	extra := append(slices.Clone(auditExemptRoutes), auditExemption{http.MethodPost, "/api/v1/gone", "removed"})
	if got := auditDecisionProblems(routes, extra); len(got) != 1 || !strings.Contains(got[0], "/api/v1/gone matches no route") {
		t.Errorf("problems with a stale exemption = %q", got)
	}
}

type auditRouteEnv struct {
	t      *testing.T
	s      *Server
	db     *sqlite.DB
	logs   *syncLogBuffer
	admin  *models.User
	cookie *http.Cookie
}

func newAuditRouteEnv(t *testing.T) *auditRouteEnv {
	t.Helper()
	db := newServerTestDB(t)
	logs := &syncLogBuffer{}
	cfg := &config.Config{Auth: config.AuthConfig{
		SessionDuration: time.Hour, MaxConcurrentSessions: 5,
		Local: config.LocalAuthConfig{Enabled: true},
	}}
	s := newServerForTest(t, cfg, db, nil, slog.New(slog.NewJSONHandler(logs, nil)))
	admin := mkTestUser(t, db, "admin@example.com", models.UserRoleAdmin)
	return &auditRouteEnv{t: t, s: s, db: db, logs: logs, admin: admin, cookie: sessionCookie(t, db, admin)}
}

// auditResp is a response whose body is already read.
type auditResp struct {
	status  int
	header  http.Header
	cookies []*http.Cookie
	body    []byte
}

func (e *auditRouteEnv) do(method, path, body string, cookies ...*http.Cookie) auditResp {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}
	resp, err := e.s.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return auditResp{status: resp.StatusCode, header: resp.Header, cookies: resp.Cookies(), body: data}
}

// events returns every stored event, oldest first.
func (e *auditRouteEnv) events() []models.AuditEvent {
	e.t.Helper()
	got, err := e.db.ListAuditEvents(context.Background(), models.AuditFilter{Ascending: true, Limit: 1000})
	if err != nil {
		e.t.Fatal(err)
	}
	return got
}

// only returns the single stored event, failing when there is not exactly one.
func (e *auditRouteEnv) only() models.AuditEvent {
	e.t.Helper()
	got := e.events()
	if len(got) != 1 {
		e.t.Fatalf("stored %d audit events, want 1: %+v", len(got), got)
	}
	return got[0]
}

func (e *auditRouteEnv) reset() {
	e.t.Helper()
	if _, err := e.db.PruneAuditEvents(context.Background(), time.Now().Add(time.Hour), 0, 1000); err != nil {
		e.t.Fatal(err)
	}
}

func detailOf(t *testing.T, ev models.AuditEvent) map[string]any {
	t.Helper()
	var detail map[string]any
	if err := json.Unmarshal(ev.Detail, &detail); err != nil {
		t.Fatal(err)
	}
	return detail
}

func wantStatus(t *testing.T, resp auditResp, want int) {
	t.Helper()
	if resp.status != want {
		t.Fatalf("status %d, want %d: %s", resp.status, want, resp.body)
	}
}

func checkActorAndTarget(t *testing.T, ev models.AuditEvent, action string, actor *models.User, targetType, targetID string) {
	t.Helper()
	if ev.Action != action || ev.Outcome != models.AuditOutcomeSuccess {
		t.Errorf("event %s %s, want %s success", ev.Action, ev.Outcome, action)
	}
	if ev.ActorType != models.AuditActorUser || ev.ActorID == nil || *ev.ActorID != actor.ID || ev.ActorLabel != actor.Email || ev.AuthMethod != models.AuditAuthSession {
		t.Errorf("actor %s %v %q %s, want user %d %q session", ev.ActorType, ev.ActorID, ev.ActorLabel, ev.AuthMethod, actor.ID, actor.Email)
	}
	if ev.TargetType != targetType || ev.TargetID != targetID {
		t.Errorf("target %s %q, want %s %q", ev.TargetType, ev.TargetID, targetType, targetID)
	}
}

func TestAuditRoutesRecordActorAndTarget(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"values":[]}`)
	}))
	t.Cleanup(backend.Close)

	// A source create.
	resp := e.do(http.MethodPost, "/api/v1/admin/sources",
		fmt.Sprintf(`{"name":"vl-audit","source_type":"victorialogs","meta_ts_field":"_time","connection":{"base_url":%q}}`, backend.URL), e.cookie)
	wantStatus(t, resp, http.StatusCreated)
	var created struct {
		Data struct {
			ID models.SourceID `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.body, &created); err != nil || created.Data.ID == 0 {
		t.Fatalf("decode created source: %v", err)
	}
	sourceID := strconv.Itoa(int(created.Data.ID))
	ev := e.only()
	checkActorAndTarget(t, ev, "source.create", e.admin, "source", sourceID)
	if ev.TargetLabel != "vl-audit" || ev.SourceID == nil || *ev.SourceID != created.Data.ID {
		t.Errorf("source create target label %q, source_id %v", ev.TargetLabel, ev.SourceID)
	}
	if ev.Route != "/api/v1/admin/sources" || ev.HTTPMethod != http.MethodPost || ev.Status != http.StatusCreated {
		t.Errorf("request %s %s %d", ev.HTTPMethod, ev.Route, ev.Status)
	}
	e.reset()

	// A source update.
	wantStatus(t, e.do(http.MethodPut, "/api/v1/admin/sources/"+sourceID, `{"description":"changed"}`, e.cookie), http.StatusOK)
	ev = e.only()
	checkActorAndTarget(t, ev, "source.update", e.admin, "source", sourceID)
	if ev.Route != "/api/v1/admin/sources/:sourceID" {
		t.Errorf("route %q, want the pattern", ev.Route)
	}
	e.reset()

	// A team member add.
	team := &models.Team{Name: "audit-team"}
	if err := e.db.CreateTeam(context.Background(), team); err != nil {
		t.Fatal(err)
	}
	member := mkTestUser(t, e.db, "member@example.com", models.UserRoleMember)
	wantStatus(t, e.do(http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/members", team.ID),
		fmt.Sprintf(`{"user_id":%d,"role":"member"}`, member.ID), e.cookie), http.StatusOK)
	ev = e.only()
	checkActorAndTarget(t, ev, "team_member.add", e.admin, "team_member", strconv.Itoa(int(member.ID)))
	if ev.TeamID == nil || *ev.TeamID != team.ID || !strings.Contains(string(ev.Changes), `"role":{"to":"member"}`) {
		t.Errorf("team member add team_id %v changes %s", ev.TeamID, ev.Changes)
	}
	e.reset()

	// A request that requireAdmin rejects records nothing.
	wantStatus(t, e.do(http.MethodPost, "/api/v1/admin/sources", `{}`, sessionCookie(t, e.db, member)), http.StatusForbidden)
	if got := e.events(); len(got) != 0 {
		t.Errorf("a rejected request stored %d events", len(got))
	}
}

func TestAuditMiddlewareRecordsHandlerFailures(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	name := string(audit.SourceUpdate)
	e.s.app.Post("/test/audit/fiber-error", e.s.requireAuth, func(fiber.Ctx) error {
		return fiber.NewError(fiber.StatusServiceUnavailable, "upstream down")
	}).Name(name)
	e.s.app.Post("/test/audit/error", e.s.requireAuth, func(fiber.Ctx) error {
		return errors.New("plain failure")
	}).Name(name)
	e.s.app.Post("/test/audit/panic", e.s.requireAuth, func(fiber.Ctx) error {
		panic("audit test panic")
	}).Name(name)
	e.s.app.Post("/test/audit/bad-request", e.s.requireAuth, func(c fiber.Ctx) error {
		return SendError(c, fiber.StatusBadRequest, "bad")
	}).Name(name)

	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/test/audit/fiber-error", http.StatusServiceUnavailable},
		{"/test/audit/error", http.StatusInternalServerError},
		{"/test/audit/panic", http.StatusInternalServerError},
	} {
		e.reset()
		wantStatus(t, e.do(http.MethodPost, tc.path, `{}`, e.cookie), tc.status)
		ev := e.only()
		if ev.Outcome != models.AuditOutcomeFailure || ev.Reason != "internal_error" || ev.Status != tc.status || ev.Route != tc.path || ev.ActorID == nil || *ev.ActorID != e.admin.ID {
			t.Errorf("%s: event %s %q status %d route %q actor %v", tc.path, ev.Outcome, ev.Reason, ev.Status, ev.Route, ev.ActorID)
		}
	}
	if !strings.Contains(e.logs.String(), `"msg":"panic recovered"`) {
		t.Error("the panic did not reach recoverMiddleware")
	}

	// A 4xx without a staged outcome records nothing.
	e.reset()
	wantStatus(t, e.do(http.MethodPost, "/test/audit/bad-request", `{}`, e.cookie), http.StatusBadRequest)
	if got := e.events(); len(got) != 0 {
		t.Errorf("a 400 stored %d events", len(got))
	}
}

func TestAuditLocalSessionEvents(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	hash, err := auth.HashLocalPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	user := &models.User{Email: "local@example.com", FullName: "Local", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman}
	if err := e.db.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if err := e.db.SetUserPasswordHash(context.Background(), user.ID, hash); err != nil {
		t.Fatal(err)
	}

	resp := e.do(http.MethodPost, "/api/v1/auth/local/login", `{"email":"local@example.com","password":"correct horse battery staple"}`, nil)
	wantStatus(t, resp, http.StatusOK)
	ev := e.only()
	checkActorAndTarget(t, ev, "session.login", user, "user", strconv.Itoa(int(user.ID)))
	if detailOf(t, ev)["method"] != "local" {
		t.Errorf("detail %s, want method local", ev.Detail)
	}
	if c := changesOf(t, ev); len(c) != 1 || c["expires_at"]["to"] == nil {
		t.Errorf("sign-in changes %s, want the session expiry", ev.Changes)
	}
	var session *http.Cookie
	for _, c := range resp.cookies {
		if c.Name == sessionCookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatal("login set no session cookie")
	}
	e.reset()

	wantStatus(t, e.do(http.MethodPost, "/api/v1/auth/local/login", `{"email":"local@example.com","password":"wrong"}`, nil), http.StatusUnauthorized)
	ev = e.only()
	if ev.Action != "session.login" || ev.Outcome != models.AuditOutcomeFailure || ev.Reason != "invalid_credentials" ||
		ev.ActorType != models.AuditActorAnonymous || ev.ActorID != nil || ev.ActorLabel != "local@example.com" || ev.AuthMethod != models.AuditAuthNone {
		t.Errorf("failed sign-in event %+v", ev)
	}
	if strings.Contains(string(ev.Detail)+string(ev.Changes), "wrong") {
		t.Error("the failed sign-in event holds the password")
	}
	e.reset()

	wantStatus(t, e.do(http.MethodPost, "/api/v1/auth/logout", ``, session), http.StatusOK)
	ev = e.only()
	checkActorAndTarget(t, ev, "session.logout", user, "user", strconv.Itoa(int(user.ID)))
	if c := changesOf(t, ev); len(c) != 1 || c["expires_at"]["from"] == nil || strings.Contains(string(ev.Changes), session.Value) {
		t.Errorf("sign-out changes %s, want the session expiry and no session ID", ev.Changes)
	}
	e.reset()

	// The session is gone, so a second sign-out has no user and records nothing.
	wantStatus(t, e.do(http.MethodPost, "/api/v1/auth/logout", ``, session), http.StatusOK)
	wantStatus(t, e.do(http.MethodPost, "/api/v1/auth/logout", ``, nil), http.StatusOK)
	if got := e.events(); len(got) != 0 {
		t.Errorf("a sign-out without a session stored %d events", len(got))
	}
}

func TestAuditOIDCSessionEvents(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	idp := newTestIDP(t, "oidc@example.com")
	e.s.config.Auth.AutoProvision = config.AutoProvisionConfig{Enabled: true, AllowedDomains: []string{"example.com"}}
	e.s.oidcProvider = idp.provider(t)

	// A failed callback answers 307, like a success, and records a failure.
	resp := e.do(http.MethodGet, "/api/v1/auth/callback?error=access_denied", ``, nil)
	wantStatus(t, resp, http.StatusTemporaryRedirect)
	ev := e.only()
	if ev.Action != "session.login" || ev.Outcome != models.AuditOutcomeFailure || ev.Reason != "access_denied" ||
		ev.ActorType != models.AuditActorAnonymous || ev.Status != http.StatusTemporaryRedirect || detailOf(t, ev)["method"] != "oidc" {
		t.Errorf("failed callback event %+v", ev)
	}
	e.reset()

	login := e.do(http.MethodGet, "/api/v1/auth/login", ``, nil)
	wantStatus(t, login, http.StatusTemporaryRedirect)
	idp.capture(t, login.header.Get("Location"))
	resp = e.do(http.MethodGet, "/api/v1/auth/callback?code=code&state="+idp.state, ``, login.cookies...)
	if resp.status != http.StatusTemporaryRedirect || strings.Contains(resp.header.Get("Location"), "error=") {
		t.Fatalf("callback %d to %q", resp.status, resp.header.Get("Location"))
	}
	user, err := e.db.GetUserByEmail(context.Background(), "oidc@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ev = e.only()
	checkActorAndTarget(t, ev, "session.login", user, "user", strconv.Itoa(int(user.ID)))
	if detailOf(t, ev)["method"] != "oidc" {
		t.Errorf("detail %s, want method oidc", ev.Detail)
	}
}

func TestAuditSetRecordsChanges(t *testing.T) {
	t.Parallel()
	e := newAuditRouteEnv(t)
	e.s.app.Put("/test/audit/set", e.s.requireAuth, func(c fiber.Ctx) error {
		auditSet(c, audit.SourceIDTarget(7), audit.NewFields().Set("name", "old").Set("kept", 1), audit.NewFields().Set("name", "new").Set("kept", 1))
		return c.SendStatus(fiber.StatusOK)
	}).Name(string(audit.SourceUpdate))
	wantStatus(t, e.do(http.MethodPut, "/test/audit/set", `{}`, e.cookie), http.StatusOK)
	ev := e.only()
	if ev.TargetID != "7" || string(ev.Changes) != `{"name":{"from":"old","to":"new"}}` {
		t.Errorf("target %q changes %s", ev.TargetID, ev.Changes)
	}
}

// strictAuditStore must flag an unstaged success, so a forgotten staging line
// fails the handler tests.
func TestStrictAuditStoreFlagsUnstagedEvents(t *testing.T) {
	t.Parallel()
	ft := &fakeTB{TB: t}
	db := newServerTestDB(t)
	rec := testAuditRecorder(ft, db)
	rec.Record(context.Background(), audit.Event{
		Action: audit.SourceUpdate, Outcome: models.AuditOutcomeSuccess,
		Actor:  audit.Actor{Type: models.AuditActorSystem},
		Detail: audit.NewFields().Set("unstaged", true),
	})
	if !ft.failed {
		t.Error("the strict recorder accepted an unstaged event")
	}
}

type fakeTB struct {
	testing.TB
	failed bool
}

func (f *fakeTB) Errorf(string, ...any) { f.failed = true }

// testIDP is an OIDC provider that signs in one email.
type testIDP struct {
	*httptest.Server
	nonce, state string
}

func (p *testIDP) provider(t *testing.T) *auth.OIDCProvider {
	t.Helper()
	provider, err := auth.NewOIDCProvider(t.Context(), &config.OIDCConfig{ProviderURL: p.URL, AuthURL: p.URL + "/auth", TokenURL: p.URL + "/token", ClientID: "test-client"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

// capture reads the nonce and the state from the
// authorization URL that /auth/login redirected to.
func (p *testIDP) capture(t *testing.T, location string) {
	t.Helper()
	u, err := http.NewRequest(http.MethodGet, location, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	q := u.URL.Query()
	p.nonce, p.state = q.Get("nonce"), q.Get("state")
	if p.nonce == "" || p.state == "" {
		t.Fatalf("authorization URL %q lacks a nonce or a state", location)
	}
}

func newTestIDP(t *testing.T, email string) *testIDP {
	t.Helper()
	signer, publicKey := newTestSigner(t)
	p := &testIDP{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": p.URL, "jwks_uri": p.URL + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(publicKey)
		case "/token":
			raw, err := signer(p.URL, map[string]any{"email": email, "name": "OIDC User", "email_verified": true, "nonce": p.nonce})
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "token_type": "Bearer", "id_token": raw})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.Close)
	return p
}

// newTestSigner returns an RS256 ID token signer and its public key set.
func newTestSigner(t *testing.T) (func(issuer string, claims map[string]any) (string, error), jose.JSONWebKeySet) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(issuer string, claims map[string]any) (string, error) {
		return jwt.Signed(signer).Claims(jwt.Claims{Issuer: issuer, Audience: jwt.Audience{"test-client"}, Expiry: jwt.NewNumericDate(time.Now().Add(time.Hour))}).Claims(claims).Serialize()
	}
	return sign, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, Algorithm: "RS256", Use: "sig"}}}
}
