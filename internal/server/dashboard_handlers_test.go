package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// newDashboardTestServer builds a Server backed by a fresh temp SQLite store.
func newDashboardTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "dash.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &Server{sqlite: s, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// withUser mounts a handler with the given user injected into c.Locals, standing
// in for the requireAuth middleware.
func withUser(app *fiber.App, method, path string, user *models.User, h fiber.Handler) {
	app.Add(method, path, func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return h(c)
	})
}

func mkTestUser(t *testing.T, s store.Store, email string, role models.UserRole) *models.User {
	t.Helper()
	u := &models.User{Email: email, FullName: email, Role: role, Status: models.UserStatusActive}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

// mkTestTeam creates a team, a source, links them, and adds each member. It
// returns the team and source so panel blobs can reference real ids (needed
// now that create/update verify team/source existence and membership).
func mkTestTeam(t *testing.T, s store.Store, name string, members ...*models.User) (*models.Team, *models.Source) {
	t.Helper()
	ctx := context.Background()
	team := &models.Team{Name: name}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam(%s): %v", name, err)
	}
	src := &models.Source{Name: name + "-src", Connection: models.ConnectionInfo{
		Host: "ch:9000", Username: "default", Database: "default", TableName: name + "_src",
	}}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource(%s): %v", name, err)
	}
	if err := s.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	for _, m := range members {
		if err := s.AddTeamMember(ctx, team.ID, m.ID, models.TeamRoleMember); err != nil {
			t.Fatalf("AddTeamMember(%s): %v", m.Email, err)
		}
	}
	return team, src
}

const validDashboardBody = `{"name":"Ops","description":"","panels":{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p2","x":6,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"a","type":"timeseries","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"},{"id":"p2","title":"b","type":"stat","team_id":1,"source_id":1,"query":"y","query_language":"logchefql"}]}}`

func TestHandleCreateDashboardValidation(t *testing.T) {
	s := newDashboardTestServer(t)
	user := mkTestUser(t, s.sqlite, "creator@test.dev", models.UserRoleMember)
	// Seed team 1 + source 1 with the creator as a member so the valid body
	// (which references team_id:1/source_id:1) passes the B2/B4 checks.
	mkTestTeam(t, s.sqlite, "team", user)

	app := fiber.New()
	withUser(app, http.MethodPost, "/dashboards", user, s.handleCreateDashboard)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", validDashboardBody, http.StatusCreated},
		{"missing name", `{"name":"","panels":{"version":1,"layout":[],"panels":[]}}`, http.StatusBadRequest},
		{"invalid json body", `{not json`, http.StatusBadRequest},
		{"too many panels", tooManyPanelsBody(), http.StatusBadRequest},
		{"bad panel type", `{"name":"x","panels":{"version":1,"layout":[],"panels":[{"id":"p1","type":"pie","team_id":1,"source_id":1,"query_language":"logchefql"}]}}`, http.StatusBadRequest},
		{"bad width", `{"name":"x","panels":{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":5,"h":2}],"panels":[]}}`, http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/dashboards", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d (body=%s)", resp.StatusCode, tc.want, body)
			}
		})
	}
}

func tooManyPanelsBody() string {
	panels := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		panels = append(panels, `{"id":"p`+strconv.Itoa(i)+`","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"}`)
	}
	return `{"name":"x","panels":{"version":1,"layout":[],"panels":[` + strings.Join(panels, ",") + `]}}`
}

// TestHandleUpdateDashboardAuthz verifies edit/delete are gated to the creator
// or a global admin (403 otherwise). This is the path that cannot be curl-checked
// with the admin dev token alone.
func TestHandleUpdateDashboardAuthz(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.sqlite, "owner@test.dev", models.UserRoleMember)
	stranger := mkTestUser(t, s.sqlite, "stranger@test.dev", models.UserRoleMember)
	admin := mkTestUser(t, s.sqlite, "admin@test.dev", models.UserRoleAdmin)

	// Seed a dashboard owned by creator.
	dash := &models.Dashboard{
		Name:       "Ops",
		PanelsJSON: json.RawMessage(`{"version":1,"layout":[],"panels":[]}`),
		CreatedBy:  &creator.ID,
	}
	if err := s.sqlite.CreateDashboard(context.Background(), dash); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	newApp := func(user *models.User) *fiber.App {
		app := fiber.New()
		withUser(app, http.MethodPut, "/dashboards/:dashboardID", user, s.handleUpdateDashboard)
		withUser(app, http.MethodDelete, "/dashboards/:dashboardID", user, s.handleDeleteDashboard)
		return app
	}

	path := "/dashboards/" + strconv.Itoa(dash.ID)
	body := `{"name":"Ops v2","panels":{"version":1,"layout":[],"panels":[]}}`

	// Stranger (non-creator, non-admin) is forbidden from updating and deleting.
	for _, m := range []struct {
		method string
		reader io.Reader
	}{
		{http.MethodPut, strings.NewReader(body)},
		{http.MethodDelete, nil},
	} {
		req := httptest.NewRequest(m.method, path, m.reader)
		req.Header.Set("Content-Type", "application/json")
		resp, err := newApp(stranger).Test(req)
		if err != nil {
			t.Fatalf("app.Test(%s): %v", m.method, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s by stranger: status = %d, want 403", m.method, resp.StatusCode)
		}
	}

	// Creator can update.
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := newApp(creator).Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("update by creator: status = %d, want 200 (body=%s)", resp.StatusCode, rb)
	}
	resp.Body.Close()

	// Global admin can delete even though they are not the creator.
	req = httptest.NewRequest(http.MethodDelete, path, http.NoBody)
	resp, err = newApp(admin).Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete by admin: status = %d, want 200", resp.StatusCode)
	}
}

// panelBody returns a valid create/update body whose single panel targets the
// given team/source.
func panelBody(name string, teamID models.TeamID, sourceID models.SourceID) string {
	return fmt.Sprintf(
		`{"name":%q,"panels":{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"a","type":"stat","team_id":%d,"source_id":%d,"query":"x","query_language":"logchefql"}]}}`,
		name, int(teamID), int(sourceID),
	)
}

// TestHandleGetDashboardForbidden verifies the B1 visibility gate: a user who is
// neither the creator, a global admin, nor a member of a referenced team gets
// 403 (not the panel blob).
func TestHandleGetDashboardForbidden(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.sqlite, "owner@test.dev", models.UserRoleMember)
	stranger := mkTestUser(t, s.sqlite, "stranger@test.dev", models.UserRoleMember)
	team, src := mkTestTeam(t, s.sqlite, "team", creator)

	dash := &models.Dashboard{Name: "Ops", PanelsJSON: panelBlobJSON(team.ID, src.ID), CreatedBy: &creator.ID}
	if err := s.sqlite.CreateDashboard(context.Background(), dash); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	path := "/dashboards/" + strconv.Itoa(dash.ID)
	newApp := func(u *models.User) *fiber.App {
		app := fiber.New()
		withUser(app, http.MethodGet, "/dashboards/:dashboardID", u, s.handleGetDashboard)
		return app
	}

	// Stranger is forbidden.
	resp, err := newApp(stranger).Test(httptest.NewRequest(http.MethodGet, path, http.NoBody))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger GET: status = %d, want 403", resp.StatusCode)
	}

	// Creator can read it.
	resp, err = newApp(creator).Test(httptest.NewRequest(http.MethodGet, path, http.NoBody))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("creator GET: status = %d, want 200", resp.StatusCode)
	}
}

// TestHandleGetDashboardRedaction verifies the B1 per-panel redaction delta at
// the handler level: an any-team viewer (member of team-a, not team-b) receives
// the team-b panel blanked + locked while the team-a panel stays intact; the
// creator gets everything; and the stored blob is untouched by the GET.
func TestHandleGetDashboardRedaction(t *testing.T) {
	s := newDashboardTestServer(t)
	ctx := context.Background()
	creator := mkTestUser(t, s.sqlite, "owner@test.dev", models.UserRoleMember)
	partial := mkTestUser(t, s.sqlite, "partial@test.dev", models.UserRoleMember)

	// partial is a member of team-a only; team-b's source is out of reach.
	teamA, srcA := mkTestTeam(t, s.sqlite, "team-a", creator, partial)
	teamB, srcB := mkTestTeam(t, s.sqlite, "team-b", creator)

	blob := json.RawMessage(fmt.Sprintf(
		`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p2","x":6,"y":0,"w":6,"h":2}],`+
			`"panels":[`+
			`{"id":"p1","title":"A","type":"stat","team_id":%d,"source_id":%d,"query":"queryA","query_language":"logchefql"},`+
			`{"id":"p2","title":"B","type":"stat","team_id":%d,"source_id":%d,"query":"queryB","query_language":"logchefql"}`+
			`]}`,
		int(teamA.ID), int(srcA.ID), int(teamB.ID), int(srcB.ID),
	))
	dash := &models.Dashboard{Name: "shared", PanelsJSON: append(json.RawMessage(nil), blob...), CreatedBy: &creator.ID}
	if err := s.sqlite.CreateDashboard(ctx, dash); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	path := "/dashboards/" + strconv.Itoa(dash.ID)
	get := func(u *models.User) map[string]struct {
		Query         string `json:"query"`
		QueryLanguage string `json:"query_language"`
		Locked        bool   `json:"locked"`
		Type          string `json:"type"`
		TeamID        int    `json:"team_id"`
	} {
		app := fiber.New()
		withUser(app, http.MethodGet, "/dashboards/:dashboardID", u, s.handleGetDashboard)
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, http.NoBody))
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s GET: status = %d, want 200", u.Email, resp.StatusCode)
		}
		var env struct {
			Data struct {
				Panels struct {
					Panels []struct {
						ID            string `json:"id"`
						Query         string `json:"query"`
						QueryLanguage string `json:"query_language"`
						Locked        bool   `json:"locked"`
						Type          string `json:"type"`
						TeamID        int    `json:"team_id"`
					} `json:"panels"`
				} `json:"panels"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		out := map[string]struct {
			Query         string `json:"query"`
			QueryLanguage string `json:"query_language"`
			Locked        bool   `json:"locked"`
			Type          string `json:"type"`
			TeamID        int    `json:"team_id"`
		}{}
		for _, p := range env.Data.Panels.Panels {
			out[p.ID] = struct {
				Query         string `json:"query"`
				QueryLanguage string `json:"query_language"`
				Locked        bool   `json:"locked"`
				Type          string `json:"type"`
				TeamID        int    `json:"team_id"`
			}{p.Query, p.QueryLanguage, p.Locked, p.Type, p.TeamID}
		}
		return out
	}

	// Partial viewer: p2 blanked + locked, p1 intact, placeholder fields kept.
	pv := get(partial)
	if pv["p1"].Query != "queryA" || pv["p1"].Locked {
		t.Errorf("partial p1 altered: %+v", pv["p1"])
	}
	if !pv["p2"].Locked || pv["p2"].Query != "" || pv["p2"].QueryLanguage != "" {
		t.Errorf("partial p2 not redacted: %+v", pv["p2"])
	}
	if pv["p2"].Type != "stat" || pv["p2"].TeamID != int(teamB.ID) {
		t.Errorf("partial p2 lost placeholder metadata: %+v", pv["p2"])
	}

	// Creator sees both queries unredacted.
	cv := get(creator)
	if cv["p1"].Query != "queryA" || cv["p2"].Query != "queryB" || cv["p1"].Locked || cv["p2"].Locked {
		t.Errorf("creator got a redacted panel: %+v", cv)
	}

	// The stored blob is untouched by the GET.
	reloaded, err := s.sqlite.GetDashboard(ctx, dash.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.Contains(string(reloaded.PanelsJSON), "queryB") {
		t.Errorf("stored blob mutated: %s", reloaded.PanelsJSON)
	}
}

// TestHandleUpdateDashboardConflict verifies A3: a stale updated_at precondition
// yields 409.
func TestHandleUpdateDashboardConflict(t *testing.T) {
	s := newDashboardTestServer(t)
	creator := mkTestUser(t, s.sqlite, "owner@test.dev", models.UserRoleMember)
	team, src := mkTestTeam(t, s.sqlite, "team", creator)

	dash := &models.Dashboard{Name: "Ops", PanelsJSON: panelBlobJSON(team.ID, src.ID), CreatedBy: &creator.ID}
	if err := s.sqlite.CreateDashboard(context.Background(), dash); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	app := fiber.New()
	withUser(app, http.MethodPut, "/dashboards/:dashboardID", creator, s.handleUpdateDashboard)
	path := "/dashboards/" + strconv.Itoa(dash.ID)

	// Stale precondition (a minute before the stored row) -> 409.
	stale := dash.UpdatedAt.Add(-time.Minute).UTC().Format(time.RFC3339)
	staleBody := fmt.Sprintf(
		`{"name":"Ops v2","updated_at":%q,"panels":{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"a","type":"stat","team_id":%d,"source_id":%d,"query":"x","query_language":"logchefql"}]}}`,
		stale, int(team.ID), int(src.ID),
	)
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(staleBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale update: status = %d, want 409", resp.StatusCode)
	}

	// No precondition -> succeeds (pre-A3 client contract preserved).
	req = httptest.NewRequest(http.MethodPut, path, strings.NewReader(panelBody("Ops v3", team.ID, src.ID)))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("no-precondition update: status = %d, want 200", resp.StatusCode)
	}
}

// panelBlobJSON is the raw panel blob for a single stat panel on team/source.
func panelBlobJSON(teamID models.TeamID, sourceID models.SourceID) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"a","type":"stat","team_id":%d,"source_id":%d,"query":"x","query_language":"logchefql"}]}`,
		int(teamID), int(sourceID),
	))
}

// TestHandleGetDashboardNotFound verifies a missing id yields 404, not a 500 or
// a mis-handled Send* nil return.
func TestHandleGetDashboardNotFound(t *testing.T) {
	s := newDashboardTestServer(t)
	user := mkTestUser(t, s.sqlite, "viewer@test.dev", models.UserRoleMember)

	app := fiber.New()
	withUser(app, http.MethodGet, "/dashboards/:dashboardID", user, s.handleGetDashboard)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/dashboards/99999", http.NoBody))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
