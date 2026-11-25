package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
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
