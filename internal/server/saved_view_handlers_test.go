package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/pkg/models"
)

// savedViewWorld is team A with an admin, an editor and two members, team B
// with one member, a source linked to both teams, and a second source linked
// to team A only. The global admin is in no team.
type savedViewWorld struct {
	env                                   *auditRouteEnv
	teamA, teamB                          *models.Team
	src, other                            *models.Source
	teamAdmin, editor, member, member2, b *models.User
	cookies                               map[models.UserID]*http.Cookie
}

func newSavedViewWorld(t *testing.T) *savedViewWorld {
	t.Helper()
	env := newAuditRouteEnv(t)
	ctx := context.Background()
	db := env.db
	w := &savedViewWorld{env: env, cookies: map[models.UserID]*http.Cookie{env.admin.ID: env.cookie}}
	user := func(email string) *models.User {
		u := mkTestUser(t, db, email, models.UserRoleMember)
		w.cookies[u.ID] = sessionCookie(t, db, u)
		return u
	}
	w.teamAdmin, w.editor, w.member, w.member2, w.b = user("ta@example.com"), user("ed@example.com"), user("m1@example.com"), user("m2@example.com"), user("b@example.com")
	w.teamA, w.src = mkTestTeam(t, db, "a")
	w.teamB, w.other = mkTestTeam(t, db, "b")
	for _, link := range []struct {
		team *models.Team
		src  *models.Source
	}{{w.teamB, w.src}, {w.teamA, w.other}} {
		if err := db.AddTeamSource(ctx, link.team.ID, link.src.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.RemoveTeamSource(ctx, w.teamB.ID, w.other.ID); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		team *models.Team
		user *models.User
		role models.TeamRole
	}{
		{w.teamA, w.teamAdmin, models.TeamRoleAdmin},
		{w.teamA, w.editor, models.TeamRoleEditor},
		{w.teamA, w.member, models.TeamRoleMember},
		{w.teamA, w.member2, models.TeamRoleMember},
		{w.teamB, w.b, models.TeamRoleMember},
	} {
		if err := db.AddTeamMember(ctx, m.team.ID, m.user.ID, m.role); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (w *savedViewWorld) path(team *models.Team, src *models.Source, rest string) string {
	return fmt.Sprintf("/api/v1/teams/%d/sources/%d/views%s", team.ID, src.ID, rest)
}

// as sends a request with the user's session and decodes the data envelope.
func (w *savedViewWorld) as(u *models.User, method, path, body string) (status int, data json.RawMessage) {
	w.env.t.Helper()
	resp := w.env.do(method, path, body, w.cookies[u.ID])
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(resp.body, &env)
	return resp.status, env.Data
}

func (w *savedViewWorld) want(u *models.User, method, path, body string, status int) json.RawMessage {
	w.env.t.Helper()
	got, data := w.as(u, method, path, body)
	if got != status {
		w.env.t.Fatalf("%s %s as %s = %d, want %d (%s)", method, path, u.Email, got, status, data)
	}
	return data
}

// create makes a view through the API and returns it.
func (w *savedViewWorld) create(u *models.User, scope models.SavedViewScope, name string) *models.SavedView {
	w.env.t.Helper()
	body := fmt.Sprintf(`{"name":%q,"scope":%q,"definition":{"version":1,"columns":[{"name":"msg"}]}}`, name, scope)
	data := w.want(u, http.MethodPost, w.path(w.teamA, w.src, ""), body, http.StatusCreated)
	var v models.SavedView
	if err := json.Unmarshal(data, &v); err != nil {
		w.env.t.Fatal(err)
	}
	return &v
}

func (w *savedViewWorld) stored(id models.SavedViewID) *models.SavedView {
	w.env.t.Helper()
	v, err := w.env.db.GetSavedView(context.Background(), id)
	if err != nil {
		w.env.t.Fatal(err)
	}
	return v
}

func viewUpdateBody(name string, revision int) string {
	return fmt.Sprintf(`{"name":%q,"definition":{"version":1},"revision":%d}`, name, revision)
}

func viewPath(v *models.SavedView) string { return fmt.Sprintf("/%d", v.ID) }

// TestSavedViewAccessTable walks every cell of RFC 0007's access table.
func TestSavedViewAccessTable(t *testing.T) {
	w := newSavedViewWorld(t)
	admin := w.env.admin
	p := func(rest string) string { return w.path(w.teamA, w.src, rest) }

	t.Run("personal", func(t *testing.T) {
		v := w.create(w.member, models.SavedViewScopePersonal, "mine")
		w.want(w.member, http.MethodGet, p(viewPath(v)), "", http.StatusOK)
		// Another member, and a global admin, cannot see it: 404, not 403.
		w.want(w.member2, http.MethodGet, p(viewPath(v)), "", http.StatusNotFound)
		w.want(admin, http.MethodGet, p(viewPath(v)), "", http.StatusNotFound)
		w.want(w.teamAdmin, http.MethodPut, p(viewPath(v)), viewUpdateBody("x", 1), http.StatusNotFound)
		w.want(w.teamAdmin, http.MethodPut, p(viewPath(v)+"/default"), "", http.StatusNotFound)
		w.want(w.member, http.MethodPut, p(viewPath(v)), viewUpdateBody("mine 2", 1), http.StatusOK)
		w.want(w.member, http.MethodPut, p(viewPath(v)+"/default"), "", http.StatusOK)
		w.want(w.member, http.MethodDelete, p(viewPath(v)+"/default"), "", http.StatusOK)
		w.want(w.member2, http.MethodDelete, p(viewPath(v)), "", http.StatusNotFound)
		w.want(w.member, http.MethodDelete, p(viewPath(v)), "", http.StatusOK)
	})

	t.Run("team", func(t *testing.T) {
		w.want(w.member, http.MethodPost, p(""), `{"name":"t","scope":"team","definition":{"version":1}}`, http.StatusForbidden)
		w.want(w.editor, http.MethodPost, p(""), `{"name":"t","scope":"team","is_default":true,"definition":{"version":1}}`, http.StatusForbidden)
		v := w.create(w.editor, models.SavedViewScopeTeam, "team view")
		w.create(w.teamAdmin, models.SavedViewScopeTeam, "by team admin")
		w.create(admin, models.SavedViewScopeTeam, "by global admin")

		w.want(w.member, http.MethodGet, p(viewPath(v)), "", http.StatusOK)
		w.want(w.member, http.MethodPut, p(viewPath(v)), viewUpdateBody("x", 1), http.StatusForbidden)
		w.want(w.member, http.MethodDelete, p(viewPath(v)), "", http.StatusForbidden)
		w.want(w.member, http.MethodPut, p(viewPath(v)+"/default"), "", http.StatusForbidden)
		w.want(w.editor, http.MethodPut, p(viewPath(v)), viewUpdateBody("team view 2", 1), http.StatusOK)
		w.want(w.editor, http.MethodPut, p(viewPath(v)+"/default"), "", http.StatusForbidden)
		w.want(w.teamAdmin, http.MethodPut, p(viewPath(v)+"/default"), "", http.StatusOK)
		w.want(admin, http.MethodDelete, p(viewPath(v)+"/default"), "", http.StatusOK)
		w.want(admin, http.MethodPut, p(viewPath(v)+"/default"), "", http.StatusOK)

		// Team B reaches the same source and still cannot see team A's view.
		w.want(w.b, http.MethodGet, w.path(w.teamB, w.src, viewPath(v)), "", http.StatusNotFound)
		w.want(w.editor, http.MethodDelete, p(viewPath(v)), "", http.StatusOK)
	})

	t.Run("source", func(t *testing.T) {
		w.want(w.teamAdmin, http.MethodPost, p(""), `{"name":"s","scope":"source","definition":{"version":1}}`, http.StatusForbidden)
		v := w.create(admin, models.SavedViewScopeSource, "everyone")
		w.want(w.member, http.MethodGet, p(viewPath(v)), "", http.StatusOK)
		w.want(w.b, http.MethodGet, w.path(w.teamB, w.src, viewPath(v)), "", http.StatusOK)
		w.want(w.teamAdmin, http.MethodPut, p(viewPath(v)), viewUpdateBody("x", 1), http.StatusForbidden)
		w.want(w.teamAdmin, http.MethodPut, p(viewPath(v)+"/default"), "", http.StatusForbidden)
		w.want(admin, http.MethodPut, p(viewPath(v)+"/default"), "", http.StatusOK)
		w.want(admin, http.MethodPut, p(viewPath(v)), viewUpdateBody("everyone 2", 1), http.StatusOK)
		w.want(admin, http.MethodDelete, p(viewPath(v)), "", http.StatusOK)
	})

	t.Run("another source", func(t *testing.T) {
		v := w.create(w.member, models.SavedViewScopePersonal, "on src")
		w.want(w.member, http.MethodGet, w.path(w.teamA, w.other, viewPath(v)), "", http.StatusNotFound)
		w.want(w.member, http.MethodDelete, w.path(w.teamA, w.other, viewPath(v)), "", http.StatusNotFound)
	})
}

func TestSavedViewManagedDefault(t *testing.T) {
	w := newSavedViewWorld(t)
	admin := w.env.admin
	ctx := context.Background()
	managed := &models.SavedView{SourceID: w.src.ID, Scope: models.SavedViewScopeSource, Name: "provisioned", Definition: models.SavedViewDefinition{Version: 1}, IsDefault: true, Managed: true}
	if err := w.env.db.CreateSavedView(ctx, managed); err != nil {
		t.Fatal(err)
	}
	other := w.create(admin, models.SavedViewScopeSource, "manual")
	p := func(rest string) string { return w.path(w.teamA, w.src, rest) }

	w.want(admin, http.MethodPut, p(viewPath(other)+"/default"), "", http.StatusForbidden)
	if !w.stored(managed.ID).IsDefault || w.stored(other.ID).IsDefault {
		t.Fatal("a refused default swap changed a row")
	}
	w.want(admin, http.MethodPost, p(""), `{"name":"second","scope":"source","is_default":true,"definition":{"version":1}}`, http.StatusForbidden)
	n, err := w.env.db.CountSavedViewsInScope(ctx, models.SavedViewScopeKey{SourceID: w.src.ID, Scope: models.SavedViewScopeSource})
	if err != nil || n != 2 {
		t.Fatalf("a refused create left %d source views (%v), want 2", n, err)
	}
	w.want(admin, http.MethodPut, p(viewPath(managed)), viewUpdateBody("x", 1), http.StatusForbidden)
	w.want(admin, http.MethodDelete, p(viewPath(managed)), "", http.StatusForbidden)
	w.want(admin, http.MethodDelete, p(viewPath(managed)+"/default"), "", http.StatusForbidden)
}

func TestSavedViewRequestErrors(t *testing.T) {
	w := newSavedViewWorld(t)
	p := func(rest string) string { return w.path(w.teamA, w.src, rest) }
	v := w.create(w.member, models.SavedViewScopePersonal, "mine")
	w.create(w.member, models.SavedViewScopePersonal, "taken")

	w.want(w.member, http.MethodPut, p(viewPath(v)), viewUpdateBody("mine", 1), http.StatusOK)
	// The client loaded revision 1; the row is at 2.
	w.want(w.member, http.MethodPut, p(viewPath(v)), viewUpdateBody("mine", 1), http.StatusConflict)
	w.want(w.member, http.MethodPut, p(viewPath(v)), `{"name":"mine","definition":{"version":1}}`, http.StatusBadRequest)
	w.want(w.member, http.MethodPut, p(viewPath(v)), viewUpdateBody("taken", 2), http.StatusConflict)
	if got := w.stored(v.ID); got.Revision != 2 || got.Name != "mine" {
		t.Fatalf("refused updates changed the row: %+v", got)
	}

	for name, body := range map[string]string{
		"duplicate name":     `{"name":"taken","scope":"personal","definition":{"version":1}}`,
		"invalid filter":     `{"name":"f","scope":"personal","definition":{"version":1,"filter":"level = "}}`,
		"unknown key":        `{"name":"f","scope":"personal","definition":{"version":1,"wrap":true}}`,
		"absolute range":     `{"name":"f","scope":"personal","definition":{"version":1,"time_range":"2026-01-01"}}`,
		"missing definition": `{"name":"f","scope":"personal"}`,
		"unknown scope":      `{"name":"f","scope":"global","definition":{"version":1}}`,
		"empty name":         `{"name":" ","scope":"personal","definition":{"version":1}}`,
	} {
		want := http.StatusBadRequest
		if name == "duplicate name" {
			want = http.StatusConflict
		}
		if got, data := w.as(w.member, http.MethodPost, p(""), body); got != want {
			t.Errorf("%s: status %d, want %d (%s)", name, got, want, data)
		}
	}
	// Provisioning rejects a range in seconds, so the API does too: a view
	// that saves must also export and import.
	resp := w.env.do(http.MethodPost, p(""), `{"name":"fast","scope":"personal","definition":{"version":1,"time_range":"30s"}}`, w.cookies[w.member.ID])
	if resp.status != http.StatusBadRequest || !strings.Contains(string(resp.body), `time_range \"30s\" is in seconds`) {
		t.Errorf("a range in seconds: status %d body %s, want 400 naming the seconds unit", resp.status, resp.body)
	}
	w.want(w.member, http.MethodPost, p(""), `{"name":"ok","scope":"personal","definition":{"version":1,"filter":"level = \"error\"","time_range":"1h","display_mode":"compact"}}`, http.StatusCreated)
}

func TestSavedViewScopeCap(t *testing.T) {
	w := newSavedViewWorld(t)
	ctx := context.Background()
	owner := w.member.ID
	for i := range models.MaxSavedViewsPerScope - 1 {
		v := &models.SavedView{SourceID: w.src.ID, Scope: models.SavedViewScopePersonal, UserID: &owner, Name: fmt.Sprintf("v%d", i), Definition: models.SavedViewDefinition{Version: 1}}
		if err := w.env.db.CreateSavedView(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	w.create(w.member, models.SavedViewScopePersonal, "fiftieth")
	w.want(w.member, http.MethodPost, w.path(w.teamA, w.src, ""), `{"name":"over","scope":"personal","definition":{"version":1}}`, http.StatusConflict)
	// The cap is per user: another member still creates.
	w.create(w.member2, models.SavedViewScopePersonal, "over")
}

func TestSavedViewListAndDefaults(t *testing.T) {
	w := newSavedViewWorld(t)
	admin := w.env.admin
	p := func(rest string) string { return w.path(w.teamA, w.src, rest) }
	listOn := func(u *models.User, team *models.Team) models.SavedViewList {
		t.Helper()
		var l models.SavedViewList
		if err := json.Unmarshal(w.want(u, http.MethodGet, w.path(team, w.src, ""), "", http.StatusOK), &l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	list := func(u *models.User) models.SavedViewList { return listOn(u, w.teamA) }

	if l := list(w.member); len(l.Views) != 0 || l.DefaultViewID != nil {
		t.Fatalf("empty list = %+v", l)
	}
	src := w.create(admin, models.SavedViewScopeSource, "a source")
	team := w.create(w.teamAdmin, models.SavedViewScopeTeam, "b team")
	mine := w.create(w.member, models.SavedViewScopePersonal, "c mine")
	w.create(w.member2, models.SavedViewScopePersonal, "not mine")

	w.want(admin, http.MethodPut, p(viewPath(src)+"/default"), "", http.StatusOK)
	if l := list(w.member); l.DefaultViewID == nil || *l.DefaultViewID != src.ID {
		t.Fatalf("source default only: default_view_id = %v, want %d", l.DefaultViewID, src.ID)
	}
	w.want(w.teamAdmin, http.MethodPut, p(viewPath(team)+"/default"), "", http.StatusOK)
	if l := list(w.member); *l.DefaultViewID != team.ID {
		t.Fatalf("team over source: default_view_id = %d, want %d", *l.DefaultViewID, team.ID)
	}
	w.want(w.member, http.MethodPut, p(viewPath(mine)+"/default"), "", http.StatusOK)
	l := list(w.member)
	if *l.DefaultViewID != mine.ID {
		t.Fatalf("personal over team: default_view_id = %d, want %d", *l.DefaultViewID, mine.ID)
	}
	var got []string
	for _, v := range l.Views {
		got = append(got, fmt.Sprintf("%s:%s:%v:%v", v.Scope, v.Name, v.CanEdit, v.CanSetDefault))
	}
	want := []string{"personal:c mine:true:true", "team:b team:false:false", "source:a source:false:false"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("member's list = %v, want %v", got, want)
	}
	// Team B sees the source view, and neither team A's view nor its default.
	if l := listOn(w.b, w.teamB); len(l.Views) != 1 || l.Views[0].ID != src.ID || *l.DefaultViewID != src.ID {
		t.Fatalf("team B's list = %+v", l)
	}
}

func TestSavedViewTokenScopes(t *testing.T) {
	w := newSavedViewWorld(t)
	w.create(w.member, models.SavedViewScopePersonal, "mine")
	path := w.path(w.teamA, w.src, "")
	get := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := w.env.s.app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(apiToken(t, w.env.s, w.member, models.TokenScopeLogsRead, models.TokenScopeSavedQueriesRead)); got != http.StatusForbidden {
		t.Errorf("token without saved_views:read = %d, want 403", got)
	}
	if got := get(apiToken(t, w.env.s, w.member, models.TokenScopeSavedViewsRead)); got != http.StatusOK {
		t.Errorf("token with saved_views:read = %d, want 200", got)
	}
}

// TestSavedViewAuditEvents asserts one event per mutating route with its
// action, actor and target, and that no event holds the filter text.
func TestSavedViewAuditEvents(t *testing.T) {
	w := newSavedViewWorld(t)
	p := func(rest string) string { return w.path(w.teamA, w.src, rest) }
	const filter = `service = "payments-secret-host"`
	w.env.reset()

	data := w.want(w.editor, http.MethodPost, p(""), fmt.Sprintf(`{"name":"errors","scope":"team","definition":{"version":1,"filter":%q}}`, filter), http.StatusCreated)
	var v models.SavedView
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	w.want(w.editor, http.MethodPut, p(viewPath(&v)), fmt.Sprintf(`{"name":"errors","definition":{"version":1,"filter":%q},"revision":1}`, filter+" and level = \"error\""), http.StatusOK)
	w.want(w.teamAdmin, http.MethodPut, p(viewPath(&v)+"/default"), "", http.StatusOK)
	w.want(w.teamAdmin, http.MethodDelete, p(viewPath(&v)+"/default"), "", http.StatusOK)
	w.want(w.editor, http.MethodDelete, p(viewPath(&v)), "", http.StatusOK)
	// A refused change records nothing.
	w.want(w.member, http.MethodPost, p(""), `{"name":"x","scope":"team","definition":{"version":1}}`, http.StatusForbidden)

	events := w.env.events()
	wantActions := []string{"saved_view.create", "saved_view.update", "saved_view.set_default", "saved_view.clear_default", "saved_view.delete"}
	wantActors := []models.UserID{w.editor.ID, w.editor.ID, w.teamAdmin.ID, w.teamAdmin.ID, w.editor.ID}
	if len(events) != len(wantActions) {
		t.Fatalf("stored %d events, want %d: %+v", len(events), len(wantActions), events)
	}
	for i, ev := range events {
		if ev.Action != wantActions[i] || ev.ActorID == nil || *ev.ActorID != wantActors[i] {
			t.Errorf("event %d = %s by %v, want %s by %d", i, ev.Action, ev.ActorID, wantActions[i], wantActors[i])
		}
		if ev.TargetType != "saved_view" || ev.TargetID != fmt.Sprint(v.ID) || ev.TargetLabel != "errors" {
			t.Errorf("event %d target = %s %s %q", i, ev.TargetType, ev.TargetID, ev.TargetLabel)
		}
		if ev.SourceID == nil || *ev.SourceID != w.src.ID || ev.TeamID == nil || *ev.TeamID != w.teamA.ID {
			t.Errorf("event %d envelope source %v team %v", i, ev.SourceID, ev.TeamID)
		}
		if strings.Contains(string(ev.Changes)+string(ev.Detail), "payments-secret-host") {
			t.Errorf("event %d holds the filter text: %s %s", i, ev.Changes, ev.Detail)
		}
	}
	var created map[string]map[string]any
	if err := json.Unmarshal(events[0].Changes, &created); err != nil {
		t.Fatal(err)
	}
	if created["scope"]["to"] != "team" || created["filter_language"]["to"] != "logchefql" || created["filter"]["to"] == nil {
		t.Errorf("create changes = %s", events[0].Changes)
	}
	var updated map[string]map[string]any
	if err := json.Unmarshal(events[1].Changes, &updated); err != nil {
		t.Fatal(err)
	}
	if updated["revision"]["from"] != float64(1) || updated["revision"]["to"] != float64(2) || updated["filter"] == nil {
		t.Errorf("update changes = %s", events[1].Changes)
	}
	if d := detailOf(t, events[2]); d["scope"] != "team" {
		t.Errorf("set_default detail = %v", d)
	}
}
