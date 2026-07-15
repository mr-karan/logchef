package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestSeedLiveDashboardFixture is a live-proof helper, not a unit test. It is
// skipped in normal runs and only executes when LOGCHEF_SEED_DB is set. It
// seeds a SQLite DB (at that path) with two members in separate teams, a
// dashboard owned by userA on teamA, and scope:* API tokens, printing the
// tokens + ids so a running server (using the same api_token_secret) can be
// curl-tested for the B1/B2/A3 authz behavior.
func TestSeedLiveDashboardFixture(t *testing.T) {
	dbPath := os.Getenv("LOGCHEF_SEED_DB")
	if dbPath == "" {
		t.Skip("set LOGCHEF_SEED_DB to seed a live fixture")
	}
	secret := os.Getenv("LOGCHEF_SEED_SECRET")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, err := sqlite.New(ctx, sqlite.Options{
		Logger: log,
		Config: config.SQLiteConfig{Path: dbPath},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	defer db.Close()

	mk := func(email string, role models.UserRole) *models.User {
		u := &models.User{Email: email, FullName: email, Role: role, Status: models.UserStatusActive}
		if err := db.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return u
	}
	userA := mk("a@logchef.internal", models.UserRoleMember)
	userB := mk("b@logchef.internal", models.UserRoleMember)

	teamA, srcA := seedTeamWithSource(t, db, "team-a", userA)
	seedTeamWithSource(t, db, "team-b", userB)

	dash, err := CreateDashboard(ctx, db, log, userA, &models.CreateDashboardRequest{Name: "A's dashboard", Panels: panelBlob(teamA.ID, srcA.ID)})
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	authCfg := &config.AuthConfig{APITokenSecret: secret}
	tok := func(u *models.User) string {
		resp, err := CreateAPIToken(ctx, db, log, authCfg, u.ID, "live", nil, []models.TokenScope{models.TokenScopeAll})
		if err != nil {
			t.Fatalf("CreateAPIToken(%s): %v", u.Email, err)
		}
		return resp.Token
	}
	fmt.Printf("SEED_DASHBOARD_ID=%d\n", dash.ID)
	fmt.Printf("SEED_TOKEN_A=%s\n", tok(userA))
	fmt.Printf("SEED_TOKEN_B=%s\n", tok(userB))
}

// TestSeedRedactionFixture is a live-proof helper for the B1 per-panel
// redaction delta (not a unit test). Gated on LOGCHEF_SEED_DB like the fixture
// above. It seeds a creator who belongs to BOTH team-a and team-b, a "partial"
// viewer who belongs to team-a only, and one dashboard whose panel p1 targets
// team-a/src-a and panel p2 targets team-b/src-b. It prints the dashboard id
// plus scope:* tokens so a running server can be curled: the creator's GET
// returns both panels' query text; the partial viewer's GET returns p2 blanked
// and locked while p1 stays intact.
func TestSeedRedactionFixture(t *testing.T) {
	dbPath := os.Getenv("LOGCHEF_SEED_DB")
	if dbPath == "" {
		t.Skip("set LOGCHEF_SEED_DB to seed a live fixture")
	}
	secret := os.Getenv("LOGCHEF_SEED_SECRET")
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, err := sqlite.New(ctx, sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: dbPath}})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	defer db.Close()

	mk := func(email string) *models.User {
		u := &models.User{Email: email, FullName: email, Role: models.UserRoleMember, Status: models.UserStatusActive}
		if err := db.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return u
	}
	creator := mk("creator@logchef.internal")
	partial := mk("partial@logchef.internal")

	// creator is in both teams (so CreateDashboard's B2 check passes); partial
	// is in team-a only.
	teamA, srcA := seedTeamWithSource(t, db, "team-a", creator, partial)
	teamB, srcB := seedTeamWithSource(t, db, "team-b", creator)

	dash, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{
		Name:   "Cross-team dashboard",
		Panels: twoPanelBlob(teamA.ID, srcA.ID, teamB.ID, srcB.ID),
	})
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	authCfg := &config.AuthConfig{APITokenSecret: secret}
	tok := func(u *models.User) string {
		resp, err := CreateAPIToken(ctx, db, log, authCfg, u.ID, "live", nil, []models.TokenScope{models.TokenScopeAll})
		if err != nil {
			t.Fatalf("CreateAPIToken(%s): %v", u.Email, err)
		}
		return resp.Token
	}
	fmt.Printf("SEED_DASHBOARD_ID=%d\n", dash.ID)
	fmt.Printf("SEED_TOKEN_CREATOR=%s\n", tok(creator))
	fmt.Printf("SEED_TOKEN_PARTIAL=%s\n", tok(partial))
}

// panelBlob builds a single-panel dashboard blob targeting the given team/source.
func panelBlob(teamID models.TeamID, sourceID models.SourceID) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"a","type":"stat","team_id":%d,"source_id":%d,"query":"x","query_language":"logchefql"}]}`,
		int(teamID), int(sourceID),
	))
}

// seedTeamWithSource creates a team + linked source and adds each member.
func seedTeamWithSource(t *testing.T, db *sqlite.DB, name string, members ...*models.User) (*models.Team, *models.Source) {
	t.Helper()
	ctx := context.Background()
	log := discardLogger()
	team, err := CreateTeam(ctx, db, log, name, "")
	if err != nil {
		t.Fatalf("CreateTeam(%s): %v", name, err)
	}
	src := newTestSource(t, db, name+"-src")
	if err := AddTeamSource(ctx, db, log, team.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource(%s): %v", name, err)
	}
	for _, m := range members {
		if err := AddTeamMember(ctx, db, log, team.ID, m.ID, models.TeamRoleMember); err != nil {
			t.Fatalf("AddTeamMember(%s): %v", m.Email, err)
		}
	}
	return team, src
}

// TestCreateDashboardDanglingAndForeignRefs covers B4 (nonexistent / unlinked
// team+source refs are rejected) and B2 (a non-member is forbidden).
func TestCreateDashboardDanglingAndForeignRefs(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	member := newTestUser(t, db, "member@test.dev", "Member")
	team, src := seedTeamWithSource(t, db, "team-a", member)

	// An orphan source not linked to team-a (for the unlinked case).
	orphanSrc := newTestSource(t, db, "orphan")

	tests := []struct {
		name    string
		user    *models.User
		blob    json.RawMessage
		wantErr error
	}{
		{"nonexistent team", member, panelBlob(9999, src.ID), ErrInvalidDashboard},
		{"nonexistent source", member, panelBlob(team.ID, 9999), ErrInvalidDashboard},
		{"source not linked to team", member, panelBlob(team.ID, orphanSrc.ID), ErrInvalidDashboard},
		{"valid for member", member, panelBlob(team.ID, src.ID), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CreateDashboard(ctx, db, log, tc.user, &models.CreateDashboardRequest{Name: "d", Panels: tc.blob})
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	// A stranger who is not a member of team-a is forbidden (B2), even though
	// the refs are otherwise valid.
	stranger := newTestUser(t, db, "stranger@test.dev", "Stranger")
	_, err := CreateDashboard(ctx, db, log, stranger, &models.CreateDashboardRequest{Name: "d", Panels: panelBlob(team.ID, src.ID)})
	if !errors.Is(err, ErrDashboardForbidden) {
		t.Fatalf("stranger create: err = %v, want ErrDashboardForbidden", err)
	}
}

// TestUpdateDashboardRejectsForeignTeam covers B2 on the edit path: the creator
// cannot add a panel targeting a team they do not belong to.
func TestUpdateDashboardRejectsForeignTeam(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	teamA, srcA := seedTeamWithSource(t, db, "team-a", creator)
	// team-b exists and is valid, but the creator is NOT a member.
	teamB, srcB := seedTeamWithSource(t, db, "team-b")

	dash, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: "d", Panels: panelBlob(teamA.ID, srcA.ID)})
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	// Editing to point at team-b (a foreign team) must be forbidden.
	_, err = UpdateDashboard(ctx, db, log, dash.ID, creator, &models.UpdateDashboardRequest{Name: "d", Panels: panelBlob(teamB.ID, srcB.ID)})
	if !errors.Is(err, ErrDashboardForbidden) {
		t.Fatalf("update to foreign team: err = %v, want ErrDashboardForbidden", err)
	}

	// A global admin may target any (existing, linked) team.
	admin := newTestUser(t, db, "admin@test.dev", "Admin")
	admin.Role = models.UserRoleAdmin
	if err := db.UpdateUser(ctx, admin); err != nil {
		t.Fatalf("UpdateUser(admin): %v", err)
	}
	if _, err := UpdateDashboard(ctx, db, log, dash.ID, admin, &models.UpdateDashboardRequest{Name: "d", Panels: panelBlob(teamB.ID, srcB.ID)}); err != nil {
		t.Fatalf("admin update to team-b: %v", err)
	}
}

// TestListDashboardsVisibility covers B1: a dashboard is visible to its creator,
// to members of any referenced team, and to global admins — but not to outsiders.
func TestListDashboardsVisibility(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	teamMember := newTestUser(t, db, "teammate@test.dev", "Teammate")
	outsider := newTestUser(t, db, "outsider@test.dev", "Outsider")
	admin := newTestUser(t, db, "admin@test.dev", "Admin")
	admin.Role = models.UserRoleAdmin
	if err := db.UpdateUser(ctx, admin); err != nil {
		t.Fatalf("UpdateUser(admin): %v", err)
	}

	// team-a has creator + teamMember; the dashboard panels reference team-a.
	teamA, srcA := seedTeamWithSource(t, db, "team-a", creator, teamMember)
	dash, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: "shared", Panels: panelBlob(teamA.ID, srcA.ID)})
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	sees := func(user *models.User) bool {
		list, err := ListDashboards(ctx, db, log, user)
		if err != nil {
			t.Fatalf("ListDashboards(%s): %v", user.Email, err)
		}
		for _, d := range list {
			if d.ID == dash.ID {
				return true
			}
		}
		return false
	}

	cases := []struct {
		user *models.User
		want bool
	}{
		{creator, true},
		{teamMember, true}, // member of the referenced team, not the creator
		{admin, true},
		{outsider, false},
	}
	for _, tc := range cases {
		if got := sees(tc.user); got != tc.want {
			t.Errorf("%s sees dashboard = %v, want %v", tc.user.Email, got, tc.want)
		}
		// UserCanViewDashboard (the get-path gate) must agree with the list.
		can, err := UserCanViewDashboard(ctx, db, tc.user, dash)
		if err != nil {
			t.Fatalf("UserCanViewDashboard(%s): %v", tc.user.Email, err)
		}
		if can != tc.want {
			t.Errorf("%s can view = %v, want %v", tc.user.Email, can, tc.want)
		}
	}
}

// twoPanelBlob builds a dashboard blob with panel p1 on (teamA, srcA) and panel
// p2 on (teamB, srcB), each carrying a distinct query so redaction is
// observable.
func twoPanelBlob(teamA models.TeamID, srcA models.SourceID, teamB models.TeamID, srcB models.SourceID) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p2","x":6,"y":0,"w":6,"h":2}],`+
			`"panels":[`+
			`{"id":"p1","title":"A","type":"stat","team_id":%d,"source_id":%d,"query":"queryA","query_language":"logchefql","options":{"limit":10}},`+
			`{"id":"p2","title":"B","type":"stat","team_id":%d,"source_id":%d,"query":"queryB","query_language":"logchefql","options":{"limit":20}}`+
			`]}`,
		int(teamA), int(srcA), int(teamB), int(srcB),
	))
}

// decodedPanel is the subset of a panel we assert on after redaction.
type decodedPanel struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Type          string `json:"type"`
	TeamID        int    `json:"team_id"`
	SourceID      int    `json:"source_id"`
	Query         string `json:"query"`
	QueryLanguage string `json:"query_language"`
	Locked        bool   `json:"locked"`
}

func decodePanels(t *testing.T, raw json.RawMessage) map[string]decodedPanel {
	t.Helper()
	var blob struct {
		Panels []decodedPanel `json:"panels"`
	}
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatalf("decode panels: %v", err)
	}
	out := make(map[string]decodedPanel, len(blob.Panels))
	for _, p := range blob.Panels {
		out[p.ID] = p
	}
	return out
}

// TestRedactDashboardPanelsForViewer covers the B1 per-panel redaction delta: a
// viewer with access to only SOME of a dashboard's panels' sources gets the
// others' query text blanked and locked=true, while the creator and global
// admins see everything. It also confirms the STORED blob is never mutated.
func TestRedactDashboardPanelsForViewer(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	partial := newTestUser(t, db, "partial@test.dev", "Partial")
	admin := newTestUser(t, db, "admin@test.dev", "Admin")
	admin.Role = models.UserRoleAdmin
	if err := db.UpdateUser(ctx, admin); err != nil {
		t.Fatalf("UpdateUser(admin): %v", err)
	}

	// partial belongs to team-a only; team-b's source is out of reach for them.
	teamA, srcA := seedTeamWithSource(t, db, "team-a", creator, partial)
	teamB, srcB := seedTeamWithSource(t, db, "team-b", creator)

	original := twoPanelBlob(teamA.ID, srcA.ID, teamB.ID, srcB.ID)
	// Insert straight through the store with the two-team blob (bypasses the
	// create-time B2 membership check, which the partial viewer would fail).
	dash := &models.Dashboard{Name: "shared", PanelsJSON: append(json.RawMessage(nil), original...), CreatedBy: &creator.ID}
	if err := db.CreateDashboard(ctx, dash); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	// Partial viewer: p1 (team-a) intact, p2 (team-b) blanked + locked.
	loaded, err := GetDashboard(ctx, db, log, dash.ID)
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	if err := RedactDashboardPanelsForViewer(ctx, db, log, partial, loaded); err != nil {
		t.Fatalf("RedactDashboardPanelsForViewer(partial): %v", err)
	}
	panels := decodePanels(t, loaded.PanelsJSON)
	if p1 := panels["p1"]; p1.Query != "queryA" || p1.Locked || p1.QueryLanguage != "logchefql" {
		t.Errorf("accessible panel p1 was altered: %+v", p1)
	}
	p2 := panels["p2"]
	if !p2.Locked {
		t.Errorf("foreign panel p2 not locked: %+v", p2)
	}
	if p2.Query != "" || p2.QueryLanguage != "" {
		t.Errorf("foreign panel p2 query not blanked: %+v", p2)
	}
	// The renderable placeholder fields survive.
	if p2.TeamID != int(teamB.ID) || p2.SourceID != int(srcB.ID) || p2.Type != "stat" || p2.Title != "B" {
		t.Errorf("foreign panel p2 lost its layout metadata: %+v", p2)
	}

	// The STORED blob must be untouched by the GET+redact.
	reloaded, err := db.GetDashboard(ctx, dash.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	stored := decodePanels(t, reloaded.PanelsJSON)
	if stored["p2"].Query != "queryB" || stored["p2"].Locked {
		t.Errorf("stored blob was mutated: %+v", stored["p2"])
	}

	// Creator and admin see everything unredacted.
	for _, viewer := range []*models.User{creator, admin} {
		full, err := GetDashboard(ctx, db, log, dash.ID)
		if err != nil {
			t.Fatalf("GetDashboard(%s): %v", viewer.Email, err)
		}
		if err := RedactDashboardPanelsForViewer(ctx, db, log, viewer, full); err != nil {
			t.Fatalf("RedactDashboardPanelsForViewer(%s): %v", viewer.Email, err)
		}
		got := decodePanels(t, full.PanelsJSON)
		if got["p1"].Query != "queryA" || got["p2"].Query != "queryB" || got["p1"].Locked || got["p2"].Locked {
			t.Errorf("%s got a redacted panel: p1=%+v p2=%+v", viewer.Email, got["p1"], got["p2"])
		}
	}

	// And on the list path the partial viewer sees the same redaction.
	list, err := ListDashboards(ctx, db, log, partial)
	if err != nil {
		t.Fatalf("ListDashboards(partial): %v", err)
	}
	var found bool
	for _, d := range list {
		if d.ID == dash.ID {
			found = true
			lp := decodePanels(t, d.PanelsJSON)
			if lp["p1"].Query != "queryA" || !lp["p2"].Locked || lp["p2"].Query != "" {
				t.Errorf("list redaction wrong: p1=%+v p2=%+v", lp["p1"], lp["p2"])
			}
		}
	}
	if !found {
		t.Fatalf("partial viewer did not see the shared dashboard in the list")
	}
}

// TestUpdateDashboardConflict covers A3: a stale updated_at precondition is
// rejected with ErrDashboardConflict, while a matching or absent one proceeds.
func TestUpdateDashboardConflict(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	team, src := seedTeamWithSource(t, db, "team-a", creator)
	dash, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: "d", Panels: panelBlob(team.ID, src.ID)})
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}

	// A precondition older than the stored row is a stale write -> conflict.
	stale := dash.UpdatedAt.Add(-time.Minute)
	_, err = UpdateDashboard(ctx, db, log, dash.ID, creator, &models.UpdateDashboardRequest{
		Name: "d2", Panels: panelBlob(team.ID, src.ID), UpdatedAt: stale,
	})
	if !errors.Is(err, ErrDashboardConflict) {
		t.Fatalf("stale update: err = %v, want ErrDashboardConflict", err)
	}

	// The matching precondition proceeds and returns a fresh updated_at.
	updated, err := UpdateDashboard(ctx, db, log, dash.ID, creator, &models.UpdateDashboardRequest{
		Name: "d3", Panels: panelBlob(team.ID, src.ID), UpdatedAt: dash.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("matching-precondition update: %v", err)
	}
	if updated.Name != "d3" {
		t.Fatalf("update did not apply: name = %q", updated.Name)
	}

	// A zero precondition disables the check (pre-A3 client) and still works.
	if _, err := UpdateDashboard(ctx, db, log, dash.ID, creator, &models.UpdateDashboardRequest{
		Name: "d4", Panels: panelBlob(team.ID, src.ID),
	}); err != nil {
		t.Fatalf("no-precondition update: %v", err)
	}
}

// TestListDashboardsCorruptRowIsolated covers B12: a row with an unparseable
// panel blob must not break the whole list. It is surfaced to its creator with
// PanelsCorrupt set and its blob nulled out, and valid rows still come back.
func TestListDashboardsCorruptRowIsolated(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	team, src := seedTeamWithSource(t, db, "team-a", creator)

	// A healthy dashboard.
	good, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: "good", Panels: panelBlob(team.ID, src.ID)})
	if err != nil {
		t.Fatalf("CreateDashboard(good): %v", err)
	}

	// A corrupt row inserted straight through the store (bypasses validation).
	corrupt := &models.Dashboard{Name: "corrupt", PanelsJSON: json.RawMessage(`{not valid json`), CreatedBy: &creator.ID}
	if err := db.CreateDashboard(ctx, corrupt); err != nil {
		t.Fatalf("store CreateDashboard(corrupt): %v", err)
	}

	list, err := ListDashboards(ctx, db, log, creator)
	if err != nil {
		t.Fatalf("ListDashboards: %v", err)
	}

	var sawGood, sawCorrupt bool
	for _, d := range list {
		switch d.ID {
		case good.ID:
			sawGood = true
			if d.PanelsCorrupt {
				t.Errorf("good dashboard wrongly flagged corrupt")
			}
		case corrupt.ID:
			sawCorrupt = true
			if !d.PanelsCorrupt {
				t.Errorf("corrupt dashboard not flagged")
			}
			// The nulled-out blob must be valid JSON so the response marshals.
			if err := json.Unmarshal(d.PanelsJSON, new(map[string]any)); err != nil {
				t.Errorf("corrupt row blob not sanitized: %v", err)
			}
		}
	}
	if !sawGood || !sawCorrupt {
		t.Fatalf("list missing rows: good=%v corrupt=%v", sawGood, sawCorrupt)
	}

	// The full response must marshal without error (the B12 failure mode).
	if _, err := json.Marshal(list); err != nil {
		t.Fatalf("list response failed to marshal: %v", err)
	}
}
