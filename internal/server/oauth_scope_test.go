package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// A CLI sign-in keeps the scopes it was approved with across refreshes, so a
// sign-in made before saved_views:read existed (server v2.8.0) cannot list
// views. A fresh sign-in can; the old one gets a 403 that names the scope and
// asks for a new sign-in.
func TestOAuthSavedViewsScope(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	team, src := mkTestTeam(t, e.db, "views", e.user)
	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/views", team.ID, src.ID)

	current := e.cliSignIn("profile:read teams:read sources:read logs:read saved_queries:read collections:read alerts:read saved_views:read offline_access")
	if resp := e.apiGet(path, current); resp.StatusCode != http.StatusOK {
		t.Fatalf("list views with saved_views:read: status %d, want 200", resp.StatusCode)
	}

	beforeViews := e.cliSignIn("profile:read teams:read sources:read logs:read saved_queries:read collections:read alerts:read offline_access")
	resp := e.apiGet(path, beforeViews)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("list views without saved_views:read: status %d, want 403", resp.StatusCode)
	}
	if got, want := resp.Header.Get("WWW-Authenticate"), `Bearer error="insufficient_scope", scope="saved_views:read"`; got != want {
		t.Errorf("WWW-Authenticate %q, want %q", got, want)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Message, "saved_views:read") || !strings.Contains(body.Message, "Sign in again") {
		t.Errorf("message %q does not name saved_views:read and ask for a new sign-in", body.Message)
	}
}

// cliSignIn runs the CLI's authorization code flow for scope and returns the
// access token.
func (e *oauthEnv) cliSignIn(scope string) string {
	e.t.Helper()
	p := newPKCE()
	params := cliAuthorizeParams(p)
	params.Set("scope", scope)
	return e.tokens(params, p).AccessToken
}

// An OAuth client lists the destinations it may use as summaries: no
// recipient, user ID, receiver host or secret hint. An admin's OAuth token
// gets no admin bypass, so another user's private destination stays hidden.
func TestOAuthListsDestinationSummaries(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	other := mkTestUser(t, e.db, "other@example.com", models.UserRoleMember)
	for owner, name := range map[*models.User]string{e.user: "mine", other: "theirs"} {
		if _, err := core.CreateDestination(t.Context(), e.db, owner, &models.DestinationRequest{
			Name: name, Type: models.DestinationTypeWebhook, Scope: models.DestinationScopePrivate,
			Secret: "https://hooks.example.com/services/secret-" + name,
		}); err != nil {
			t.Fatalf("CreateDestination(%s): %v", name, err)
		}
	}

	resp := e.apiGet("/api/v1/notification-destinations", e.cliSignIn("destinations:read"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0]["name"] != "mine" {
		t.Fatalf("destinations = %v, want only the caller's own", body.Data)
	}
	keys := slices.Sorted(maps.Keys(body.Data[0]))
	if want := []string{"id", "is_active", "name", "scope", "type"}; !slices.Equal(keys, want) {
		t.Errorf("summary keys = %v, want %v", keys, want)
	}

	resp = e.apiGet("/api/v1/notification-destinations", e.cliSignIn("alerts:read"))
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get("WWW-Authenticate") != `Bearer error="insufficient_scope", scope="destinations:read"` {
		t.Fatalf("without destinations:read: status %d challenge %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
}

// An OAuth client with dashboards:read lists and reads the dashboards its
// user sees as a member. An admin's OAuth token gets no admin bypass: it sees
// the dashboards it created, not other teams' ones, which the admin's
// session still lists.
func TestOAuthDashboardsWithoutAdminBypass(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	other := mkTestUser(t, e.db, "other@example.com", models.UserRoleMember)
	team, src := mkTestTeam(t, e.db, "teamb", other)
	create := func(owner *models.User, name string) *models.Dashboard {
		panels := fmt.Sprintf(`{"version":2,"tabs":[{"id":"main","title":"Main","layout":[{"id":"p","x":0,"y":0,"w":6,"h":2}]}],"panels":[{"id":"p","kind":"query","title":"Errors","team_id":%d,"source_id":%d,"query":{"mode":"count","language":"logchefql","text":"level=\"error\""},"viz":{"type":"stat"}}]}`, team.ID, src.ID)
		d, err := core.CreateDashboard(t.Context(), e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), owner, &models.CreateDashboardRequest{Name: name, Panels: json.RawMessage(panels)})
		if err != nil {
			t.Fatalf("CreateDashboard(%s): %v", name, err)
		}
		return d
	}
	create(e.user, "admin's own")
	theirs := create(other, "team b's")

	list := func(req *http.Request) []string {
		t.Helper()
		req.Header.Set(models.DashboardSchemaHeader, "2")
		resp := e.do(req)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list dashboards: status %d", resp.StatusCode)
		}
		var body struct {
			Data []models.DashboardSummary `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		names := make([]string, len(body.Data))
		for i, s := range body.Data {
			names[i] = s.Name
		}
		slices.Sort(names)
		return names
	}
	token := e.cliSignIn("dashboards:read")
	oauthReq := httptest.NewRequest(http.MethodGet, testIssuer+"/api/v1/dashboards", http.NoBody)
	oauthReq.Header.Set("Authorization", "Bearer "+token)
	if got := list(oauthReq); !slices.Equal(got, []string{"admin's own"}) {
		t.Fatalf("admin OAuth lists %v, want only its own dashboard", got)
	}
	sessionReq := httptest.NewRequest(http.MethodGet, testIssuer+"/api/v1/dashboards", http.NoBody)
	sessionReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: e.session})
	if got := list(sessionReq); !slices.Equal(got, []string{"admin's own", "team b's"}) {
		t.Fatalf("admin session lists %v, want every dashboard", got)
	}
	if resp := e.apiGet(fmt.Sprintf("/api/v1/dashboards/%d", theirs.ID), token); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin OAuth reads another team's dashboard: status %d, want 403", resp.StatusCode)
	}
}
