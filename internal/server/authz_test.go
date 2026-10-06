package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Authorization acceptance tests (T-AZ-1..5) through the real HTTP stack:
// requireAuth, requireTeamMember and requireTeamHasSource, with real SQLite.
//
// authzWorld, authzEntrypoint, runTeamSourceAuthzCases and
// runMembershipRemovalCase do not depend on HTTP. A later entrypoint (MCP)
// supplies its own authzEntrypoint and must reach the same verdicts.

type authzVerdict int

const (
	verdictGranted authzVerdict = iota + 1
	verdictDenied
)

func (v authzVerdict) String() string {
	switch v {
	case verdictGranted:
		return "granted"
	case verdictDenied:
		return "denied"
	}
	return fmt.Sprintf("authzVerdict(%d)", int(v))
}

// authzWorld is the seeded state that every authorization case runs against.
type authzWorld struct {
	db       store.Store
	member   *models.User // member of team and bareTeam
	admin    *models.User // global admin, member of no team
	outsider *models.User // member of no team
	team     models.TeamID
	bareTeam models.TeamID // has member, not linked to source
	source   models.SourceID
}

func newAuthzWorld(t *testing.T, db store.Store) authzWorld {
	t.Helper()
	ctx := context.Background()
	w := authzWorld{
		db:       db,
		member:   mkTestUser(t, db, "member@example.com", models.UserRoleMember),
		admin:    mkTestUser(t, db, "admin@example.com", models.UserRoleAdmin),
		outsider: mkTestUser(t, db, "outsider@example.com", models.UserRoleMember),
	}
	team, src := mkTestTeam(t, db, "linked", w.member)
	w.team, w.source = team.ID, src.ID
	bare := &models.Team{Name: "bare"}
	if err := db.CreateTeam(ctx, bare); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := db.AddTeamMember(ctx, bare.ID, w.member.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	w.bareTeam = bare.ID
	return w
}

// authzEntrypoint asks one entrypoint whether user may read the logs of
// source through team, and reports the verdict.
type authzEntrypoint func(t *testing.T, user *models.User, team models.TeamID, source models.SourceID) authzVerdict

// runTeamSourceAuthzCases is T-AZ-3.
func runTeamSourceAuthzCases(t *testing.T, w authzWorld, entry authzEntrypoint) {
	t.Helper()
	for _, tc := range []struct {
		name string
		user *models.User
		team models.TeamID
		want authzVerdict
	}{
		{"member, linked source", w.member, w.team, verdictGranted},
		{"non-member", w.outsider, w.team, verdictDenied},
		{"global admin, not a member", w.admin, w.team, verdictGranted},
		{"member, team without the source", w.member, w.bareTeam, verdictDenied},
		{"global admin, team without the source", w.admin, w.bareTeam, verdictDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := entry(t, tc.user, tc.team, w.source); got != tc.want {
				t.Fatalf("verdict = %s, want %s", got, tc.want)
			}
		})
	}
}

// runMembershipRemovalCase is T-AZ-4: the next request after removal fails.
func runMembershipRemovalCase(t *testing.T, w authzWorld, entry authzEntrypoint) {
	t.Helper()
	if got := entry(t, w.member, w.team, w.source); got != verdictGranted {
		t.Fatalf("before removal: verdict = %s, want granted", got)
	}
	if err := w.db.RemoveTeamMember(context.Background(), w.team, w.member.ID); err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	if got := entry(t, w.member, w.team, w.source); got != verdictDenied {
		t.Fatalf("after removal: verdict = %s, want denied", got)
	}
}

const authzTestTokenSecret = "authz-test-secret-0123456789abcdef"

// newAuthzTestServer builds the full server (all middleware and routes) on a
// fresh SQLite store.
func newAuthzTestServer(t *testing.T) *Server {
	t.Helper()
	db := newServerTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Auth.APITokenSecret = authzTestTokenSecret
	s := New(ServerOptions{
		Config:      cfg,
		SQLite:      db,
		Datasources: datasource.NewService(db, logger),
		Logger:      logger,
		FS:          http.Dir(t.TempDir()),
	})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return s
}

// sessionCookie creates a live session for user and returns its cookie.
func sessionCookie(t *testing.T, db store.Store, user *models.User) *http.Cookie {
	t.Helper()
	sess := &models.Session{
		ID:        models.SessionID(fmt.Sprintf("authz-session-%d-%d", user.ID, time.Now().UnixNano())),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := db.CreateSession(context.Background(), sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: string(sess.ID)}
}

func apiToken(t *testing.T, s *Server, user *models.User, scopes ...models.TokenScope) string {
	t.Helper()
	created, err := core.CreateAPIToken(context.Background(), s.sqlite, s.log, &s.config.Auth, user.ID, "authz", nil, scopes)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return created.Token
}

type authzResponse struct {
	status  int
	message string
}

func doAuthzRequest(t *testing.T, s *Server, method, path, body string, auth func(*http.Request)) authzResponse {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	auth(req)
	resp, err := s.app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var parsed struct {
		Message string `json:"message"`
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &parsed)
	return authzResponse{status: resp.StatusCode, message: parsed.Message}
}

func withSession(cookie *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(cookie) }
}

func withBearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

// httpAuthzEntrypoint asks through the LogchefQL validate route, which needs
// logs:read and touches no datasource, so 200 means authorization passed.
func httpAuthzEntrypoint(s *Server) authzEntrypoint {
	return func(t *testing.T, user *models.User, team models.TeamID, source models.SourceID) authzVerdict {
		t.Helper()
		path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logchefql/validate", team, source)
		resp := doAuthzRequest(t, s, http.MethodPost, path, `{"query":"level=\"error\""}`, withSession(sessionCookie(t, s.sqlite, user)))
		switch resp.status {
		case fiber.StatusOK:
			return verdictGranted
		case fiber.StatusForbidden:
			return verdictDenied
		}
		t.Fatalf("POST %s: unexpected status %d (%q)", path, resp.status, resp.message)
		return 0
	}
}

func TestAuthzTeamSourceHTTP(t *testing.T) {
	s := newAuthzTestServer(t)
	runTeamSourceAuthzCases(t, newAuthzWorld(t, s.sqlite), httpAuthzEntrypoint(s))
}

func TestAuthzMembershipRemovalHTTP(t *testing.T) {
	s := newAuthzTestServer(t)
	runMembershipRemovalCase(t, newAuthzWorld(t, s.sqlite), httpAuthzEntrypoint(s))
}

// logReadRoutes are the team-source routes that require logs:read.
var logReadRoutes = []struct{ method, path string }{
	{http.MethodPost, "/logs/query"},
	{http.MethodGet, "/logs/tail"},
	{http.MethodPost, "/logs/export"},
	{http.MethodPost, "/logs/query/q1/cancel"},
	{http.MethodPost, "/exports"},
	{http.MethodGet, "/exports/e1"},
	{http.MethodGet, "/exports/e1/download"},
	{http.MethodPost, "/logs/histogram"},
	{http.MethodPost, "/logs/context"},
	{http.MethodPost, "/generate-sql"},
	{http.MethodPost, "/logchefql/translate"},
	{http.MethodPost, "/logchefql/validate"},
	{http.MethodPost, "/logchefql/query"},
	{http.MethodGet, "/fields/values"},
	{http.MethodGet, "/fields/level/values"},
}

// T-AZ-1 over HTTP. An OAuth principal cannot reach HTTP until phase 3; its
// scope check is the same Principal.Require, covered in core/access.
func TestAuthzTokenWithoutLogsReadDeniedOnEveryLogRoute(t *testing.T) {
	s := newAuthzTestServer(t)
	w := newAuthzWorld(t, s.sqlite)
	token := apiToken(t, s, w.member, models.TokenScopeSourcesRead)
	base := fmt.Sprintf("/api/v1/teams/%d/sources/%d", w.team, w.source)

	for _, route := range logReadRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			resp := doAuthzRequest(t, s, route.method, base+route.path, `{}`, withBearer(token))
			if resp.status != fiber.StatusForbidden || resp.message != "API token does not have the required scope" {
				t.Fatalf("status %d message %q, want 403 for the missing scope", resp.status, resp.message)
			}
		})
	}

	// The same token with logs:read passes on the same source.
	readToken := apiToken(t, s, w.member, models.TokenScopeLogsRead)
	resp := doAuthzRequest(t, s, http.MethodPost, base+"/logchefql/validate", `{"query":"a=1"}`, withBearer(readToken))
	if resp.status != fiber.StatusOK {
		t.Fatalf("logs:read token: status %d (%q), want 200", resp.status, resp.message)
	}
}

// T-AZ-2 and the error precedence the routes had before the refactor:
// membership, then the source link, then scope.
func TestAuthzHTTPErrorPrecedence(t *testing.T) {
	s := newAuthzTestServer(t)
	w := newAuthzWorld(t, s.sqlite)
	validate := func(team models.TeamID) string {
		return fmt.Sprintf("/api/v1/teams/%d/sources/%d/logchefql/validate", team, w.source)
	}
	noLogs := withBearer(apiToken(t, s, w.member, models.TokenScopeSourcesRead))
	outsiderNoLogs := withBearer(apiToken(t, s, w.outsider, models.TokenScopeSourcesRead))

	for _, tc := range []struct {
		name    string
		path    string
		auth    func(*http.Request)
		status  int
		message string
	}{
		{"session member", validate(w.team), withSession(sessionCookie(t, s.sqlite, w.member)), fiber.StatusOK, ""},
		{"non-member without scope", validate(w.team), outsiderNoLogs, fiber.StatusForbidden, "Team membership required"},
		{"unlinked team without scope", validate(w.bareTeam), noLogs, fiber.StatusForbidden, "Team does not have access to this source"},
		{"linked team without scope", validate(w.team), noLogs, fiber.StatusForbidden, "API token does not have the required scope"},
		{"invalid source id", fmt.Sprintf("/api/v1/teams/%d/sources/x/logchefql/validate", w.team), noLogs, fiber.StatusBadRequest, ""},
		{"no credentials", validate(w.team), func(*http.Request) {}, fiber.StatusUnauthorized, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doAuthzRequest(t, s, http.MethodPost, tc.path, `{"query":"a=1"}`, tc.auth)
			if resp.status != tc.status {
				t.Fatalf("status %d (%q), want %d", resp.status, resp.message, tc.status)
			}
			if tc.message != "" && resp.message != tc.message {
				t.Fatalf("message %q, want %q", resp.message, tc.message)
			}
		})
	}
}

// T-AZ-5 at the HTTP middleware: a request whose auth method is not one
// requireAuth sets is denied, where it used to pass.
func TestRequireTokenScopeDeniesUnknownAuthMethod(t *testing.T) {
	t.Parallel()
	for _, method := range []any{nil, "oauth", "bogus"} {
		t.Run(fmt.Sprint(method), func(t *testing.T) {
			app := fiber.New()
			s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			app.Get("/", func(c fiber.Ctx) error {
				if method != nil {
					c.Locals("auth_method", method)
				}
				c.Locals("user", &models.User{ID: 1, Role: models.UserRoleAdmin})
				return c.Next()
			}, s.requireTokenScope(models.TokenScopeLogsRead), func(c fiber.Ctx) error {
				return c.SendStatus(fiber.StatusNoContent)
			})
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", http.NoBody))
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
		})
	}
}

// authorizeTestSource links sourceID to a new team and returns a global admin
// with the AuthorizedSource that requireTeamHasSource would store. Handler
// tests that bypass the middleware use it to call handlers directly.
func authorizeTestSource(t *testing.T, db store.Store, sourceID models.SourceID) (*models.User, access.AuthorizedSource) {
	t.Helper()
	ctx := context.Background()
	admin := mkTestUser(t, db, fmt.Sprintf("admin-%d@example.com", sourceID), models.UserRoleAdmin)
	team := &models.Team{Name: fmt.Sprintf("team-for-source-%d", sourceID)}
	if err := db.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := db.AddTeamSource(ctx, team.ID, sourceID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	src, err := access.AuthorizeTeamSource(ctx, db, access.SessionPrincipal(admin), team.ID, sourceID, models.TokenScopeLogsRead)
	if err != nil {
		t.Fatalf("AuthorizeTeamSource: %v", err)
	}
	return admin, src
}

// withAuthorizedSource mounts h behind requireTeamHasSource, with a session
// for user standing in for requireAuth.
func withAuthorizedSource(app *fiber.App, s *Server, method, path string, user *models.User, h fiber.Handler) {
	app.Add([]string{method}, path, func(c fiber.Ctx) error {
		c.Locals("user", user)
		c.Locals("auth_method", "session")
		return c.Next()
	}, s.requireTeamHasSource(models.TokenScopeLogsRead), h)
}
