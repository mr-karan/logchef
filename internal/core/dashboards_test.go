package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

func dashboardClient(t *testing.T, schema, features string) models.DashboardClient {
	t.Helper()
	var header *string
	if schema != "" {
		header = &schema
	}
	client, err := models.ParseDashboardClient(header, features)
	if err != nil {
		t.Fatalf("ParseDashboardClient(%q): %v", schema, err)
	}
	return client
}

func schema2Client(t *testing.T) models.DashboardClient { return dashboardClient(t, "2", "") }
func schema1Client(t *testing.T) models.DashboardClient { return dashboardClient(t, "", "") }

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
	_, err = UpdateDashboard(ctx, db, log, dash.ID, creator, schema2Client(t), &models.UpdateDashboardRequest{Name: "d", Panels: panelBlob(teamB.ID, srcB.ID), Revision: &dash.Revision})
	if !errors.Is(err, ErrDashboardForbidden) {
		t.Fatalf("update to foreign team: err = %v, want ErrDashboardForbidden", err)
	}

	// A global admin may target any (existing, linked) team.
	admin := newTestUser(t, db, "admin@test.dev", "Admin")
	admin.Role = models.UserRoleAdmin
	if err := db.UpdateUser(ctx, admin); err != nil {
		t.Fatalf("UpdateUser(admin): %v", err)
	}
	if _, err := UpdateDashboard(ctx, db, log, dash.ID, admin, schema2Client(t), &models.UpdateDashboardRequest{Name: "d", Panels: panelBlob(teamB.ID, srcB.ID), Revision: &dash.Revision}); err != nil {
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

// TestBuildDashboardResponseRedaction covers the B1 per-panel redaction
// delta through the one response builder: a viewer with access to only SOME
// of a dashboard's panels' sources gets the others' query blanked and locked
// in both client schemas, the creator and global admins see everything, the
// list summary still counts the locked panel, and the STORED blob is never
// mutated.
func TestBuildDashboardResponseRedaction(t *testing.T) {
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

	build := func(user *models.User, client models.DashboardClient) *models.DashboardResponse {
		t.Helper()
		loaded, err := GetDashboard(ctx, db, log, dash.ID)
		if err != nil {
			t.Fatalf("GetDashboard: %v", err)
		}
		resp, err := BuildDashboardResponse(ctx, db, log, user, client, loaded)
		if err != nil {
			t.Fatalf("BuildDashboardResponse(%s): %v", user.Email, err)
		}
		if resp.Corrupt || resp.PanelsCorrupt {
			t.Fatalf("response for %s is a stub or corrupt: %+v", user.Email, resp)
		}
		return resp
	}

	// Partial viewer, schema 1 client: p1 (team-a) intact, p2 (team-b) blanked + locked.
	panels := decodePanels(t, build(partial, schema1Client(t)).Panels)
	if p1 := panels["p1"]; p1.Query != "queryA" || p1.Locked || p1.QueryLanguage != "logchefql" {
		t.Errorf("accessible panel p1 was altered: %+v", p1)
	}
	p2 := panels["p2"]
	if !p2.Locked || p2.Query != "" || p2.QueryLanguage != "" {
		t.Errorf("foreign panel p2 not redacted: %+v", p2)
	}
	if p2.TeamID != int(teamB.ID) || p2.SourceID != int(srcB.ID) || p2.Type != "stat" || p2.Title != "B" {
		t.Errorf("foreign panel p2 lost its layout metadata: %+v", p2)
	}

	// Partial viewer, schema 2 client: the upgraded blob is redacted the same way.
	v2 := build(partial, schema2Client(t))
	var upgraded struct {
		Version int `json:"version"`
		Panels  []struct {
			ID     string          `json:"id"`
			Title  string          `json:"title"`
			TeamID int             `json:"team_id"`
			Query  json.RawMessage `json:"query"`
			Viz    map[string]any  `json:"viz"`
			Locked bool            `json:"locked"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(v2.Panels, &upgraded); err != nil {
		t.Fatalf("decode schema 2 response: %v", err)
	}
	if upgraded.Version != 2 || len(upgraded.Panels) != 2 {
		t.Fatalf("schema 2 response = %s", v2.Panels)
	}
	if open := upgraded.Panels[0]; open.Locked || !strings.Contains(string(open.Query), "queryA") || open.Viz["reduce"] != "sum" {
		t.Errorf("accessible panel p1 was altered in schema 2: %+v", open)
	}
	if locked := upgraded.Panels[1]; !locked.Locked || locked.Query != nil || locked.Viz["type"] != "stat" || len(locked.Viz) != 1 || locked.Title != "B" || locked.TeamID != int(teamB.ID) {
		t.Errorf("foreign panel p2 not redacted in schema 2: %+v", locked)
	}
	if strings.Contains(string(v2.Panels), "queryB") {
		t.Errorf("schema 2 response leaked the locked query: %s", v2.Panels)
	}

	// The STORED blob must be untouched.
	reloaded, err := db.GetDashboard(ctx, dash.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if string(reloaded.PanelsJSON) != string(original) {
		t.Errorf("stored blob was mutated: %s", reloaded.PanelsJSON)
	}

	// Creator and admin see everything unredacted, in either schema.
	for _, viewer := range []*models.User{creator, admin} {
		got := decodePanels(t, build(viewer, schema1Client(t)).Panels)
		if got["p1"].Query != "queryA" || got["p2"].Query != "queryB" || got["p1"].Locked || got["p2"].Locked {
			t.Errorf("%s got a redacted panel: p1=%+v p2=%+v", viewer.Email, got["p1"], got["p2"])
		}
		if resp := build(viewer, schema2Client(t)); !strings.Contains(string(resp.Panels), "queryB") || !resp.CanEdit {
			t.Errorf("%s got a redacted schema 2 panel or no edit rights: %+v", viewer.Email, resp)
		}
	}

	// The list summary counts the locked panel and names its kind.
	list, err := ListDashboards(ctx, db, log, partial)
	if err != nil {
		t.Fatalf("ListDashboards(partial): %v", err)
	}
	i := slices.IndexFunc(list, func(s *models.DashboardSummary) bool { return s.ID == dash.ID })
	if i < 0 {
		t.Fatalf("partial viewer did not see the shared dashboard in the list")
	}
	if s := list[i]; s.PanelCount != 2 || !slices.Equal(s.PanelTypes, []string{"stat"}) || s.CanEdit || s.Corrupt {
		t.Errorf("list summary = %+v, want 2 stat panels without edit rights", s)
	}
}

// TestBuildDashboardResponseSchemas covers the read table of the client
// schema contract: a schema 1 row is served as stored to a schema 1 client and
// upgraded for a schema 2 client; a schema 2 row is served as stored to a
// schema 2 client and as the stub to a schema 1 client; a row with a feature
// this server does not know is the stub for everyone.
func TestBuildDashboardResponseSchemas(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	team, src := seedTeamWithSource(t, db, "team-a", creator)

	v1 := panelBlob(team.ID, src.ID)
	v2 := panelBlobV2(team.ID, src.ID)
	newer := json.RawMessage(strings.Replace(string(v2), `"features":[]`, `"features":["tabs"]`, 1))
	rows := map[string]*models.Dashboard{}
	for name, blob := range map[string]json.RawMessage{"v1": v1, "v2": v2, "newer": newer} {
		d := &models.Dashboard{Name: name, PanelsJSON: blob, CreatedBy: &creator.ID}
		if err := db.CreateDashboard(ctx, d); err != nil {
			t.Fatalf("CreateDashboard(%s): %v", name, err)
		}
		rows[name] = d
	}
	upgraded, err := models.UpgradeDashboardPanels(v1)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		row, client string
		wantPanels  string
		stub        bool
	}{
		{"v1", "1", string(v1), false},
		{"v1", "2", string(upgraded), false},
		{"v2", "2", string(v2), false},
		{"v2", "1", "", true},
		{"newer", "1", "", true},
		{"newer", "2", "", true},
	} {
		t.Run(tc.row+"/client"+tc.client, func(t *testing.T) {
			resp, err := BuildDashboardResponse(ctx, db, log, creator, dashboardClient(t, tc.client, ""), rows[tc.row])
			if err != nil {
				t.Fatal(err)
			}
			if tc.stub {
				if string(resp.Panels) != string(models.EmptyDashboardPanelsJSON()) || !resp.PanelsCorrupt || resp.MinSchema != 2 || resp.CanEdit || resp.Corrupt {
					t.Fatalf("response = %+v, want the schema 1 stub", resp)
				}
				return
			}
			if string(resp.Panels) != tc.wantPanels || resp.PanelsCorrupt || resp.MinSchema != 0 || !resp.CanEdit || resp.Corrupt || resp.Revision != 1 {
				t.Fatalf("response = %+v\npanels %s\nwant %s", resp, resp.Panels, tc.wantPanels)
			}
		})
	}
}

// TestUpdateDashboardClientContract covers the write table: a schema 1 body
// may replace a schema 1 row, a schema 2 body may replace a schema 1 row and
// from then on the row is schema 2, a schema 1 body on a schema 2 row is
// refused, and a stored feature the writer did not declare is refused and
// named. A refused save leaves the row byte for byte as it was.
func TestUpdateDashboardClientContract(t *testing.T) {
	db, dbPath := newTestDBWithPath(t)
	log := discardLogger()
	ctx := context.Background()
	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	team, src := seedTeamWithSource(t, db, "team-a", creator)

	dash, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: "d", Panels: panelBlob(team.ID, src.ID)})
	if err != nil {
		t.Fatal(err)
	}
	v1 := panelBlob(team.ID, src.ID)
	v2 := panelBlobV2(team.ID, src.ID)
	save := func(client models.DashboardClient, body json.RawMessage) (*models.Dashboard, error) {
		t.Helper()
		current, err := db.GetDashboard(ctx, dash.ID)
		if err != nil {
			t.Fatal(err)
		}
		return UpdateDashboard(ctx, db, log, dash.ID, creator, client, &models.UpdateDashboardRequest{Name: "d", Panels: body, Revision: &current.Revision})
	}
	stored := func() string {
		t.Helper()
		current, err := db.GetDashboard(ctx, dash.ID)
		if err != nil {
			t.Fatal(err)
		}
		return string(current.PanelsJSON)
	}

	if _, err := save(schema1Client(t), v1); err != nil {
		t.Fatalf("schema 1 body on a schema 1 row: %v", err)
	}
	if _, err := save(schema1Client(t), v2); err != nil {
		t.Fatalf("schema 2 body on a schema 1 row: %v", err)
	}
	if got := stored(); got != string(v2) {
		t.Fatalf("stored blob after a schema 2 save = %s, want the canonical schema 2 body", got)
	}
	_, err = save(schema1Client(t), v1)
	var stale *DashboardClientStaleError
	if !errors.Is(err, ErrDashboardClientStale) || !errors.As(err, &stale) || !strings.Contains(stale.Detail, "schema 2") {
		t.Fatalf("schema 1 body on a schema 2 row: err = %v, want DashboardClientStaleError naming the schema", err)
	}
	if got := stored(); got != string(v2) {
		t.Fatalf("a refused save changed the row: %s", got)
	}
	if _, err := save(schema2Client(t), v2); err != nil {
		t.Fatalf("feature-free schema 2 row with no features header: %v", err)
	}

	// A newer server stored a feature this one does not know.
	newer := strings.Replace(string(v2), `"features":[]`, `"features":["tabs","formats"]`, 1)
	raw := openRawSQLite(t, dbPath)
	if _, err := raw.ExecContext(ctx, "UPDATE dashboards SET panels_json = ? WHERE id = ?", newer, dash.ID); err != nil {
		t.Fatal(err)
	}
	_, err = save(dashboardClient(t, "2", "tabs, formats, made-up"), v2)
	if !errors.As(err, &stale) || !strings.Contains(stale.Detail, "tabs, formats") {
		t.Fatalf("stored unknown features: err = %v, want DashboardClientStaleError naming them", err)
	}
	if got := stored(); got != newer {
		t.Fatalf("a refused save changed the row: %s", got)
	}
	resp, err := BuildDashboardResponse(ctx, db, log, creator, schema2Client(t), mustLoad(t, db, dash.ID))
	if err != nil || !resp.PanelsCorrupt || resp.MinSchema != 2 {
		t.Fatalf("row with an unknown feature is not served as the stub: %+v %v", resp, err)
	}
}

func mustLoad(t *testing.T, db *sqlite.DB, id int) *models.Dashboard {
	t.Helper()
	d, err := db.GetDashboard(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// panelBlobV2 is the canonical schema 2 form of panelBlob.
func panelBlobV2(teamID models.TeamID, sourceID models.SourceID) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}]}],"panels":[{"id":"p1","kind":"query","title":"a","team_id":%d,"source_id":%d,"query":{"mode":"count","language":"logchefql","text":"x","interval":"auto"},"viz":{"type":"stat","reduce":"sum"}}]}`,
		int(teamID), int(sourceID),
	))
}

// TestDashboardTextBounds covers the name and description bounds in code
// points: 200 and 2,000 pass, one more fails with a validation error.
func TestDashboardTextBounds(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	team, src := seedTeamWithSource(t, db, "team-a", creator)

	for _, tc := range []struct {
		name, description string
		wantErr           bool
	}{
		{strings.Repeat("é", models.MaxDashboardNameRunes), strings.Repeat("日", models.MaxDashboardDescriptionRunes), false},
		{strings.Repeat("é", models.MaxDashboardNameRunes+1), "", true},
		{"ok", strings.Repeat("日", models.MaxDashboardDescriptionRunes+1), true},
	} {
		_, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: tc.name, Description: tc.description, Panels: panelBlob(team.ID, src.ID)})
		if tc.wantErr != errors.Is(err, ErrInvalidDashboard) {
			t.Errorf("name of %d and description of %d runes: err = %v", len([]rune(tc.name)), len([]rune(tc.description)), err)
		}
	}
}

// TestListDashboardsCorruptRowIsolated covers B12: a row whose stored blob
// fails validation must not break the whole list. It is listed to its creator
// as corrupt with no counts, kept from a team member, and valid rows still
// come back.
func TestListDashboardsCorruptRowIsolated(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	member := newTestUser(t, db, "member@test.dev", "Member")
	team, src := seedTeamWithSource(t, db, "team-a", creator, member)

	// A healthy dashboard.
	good, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: "good", Panels: panelBlob(team.ID, src.ID)})
	if err != nil {
		t.Fatalf("CreateDashboard(good): %v", err)
	}

	// Corrupt rows inserted straight through the store (bypasses validation):
	// one unparseable, one parseable but invalid.
	corrupt := &models.Dashboard{Name: "corrupt", PanelsJSON: json.RawMessage(`{not valid json`), CreatedBy: &creator.ID}
	if err := db.CreateDashboard(ctx, corrupt); err != nil {
		t.Fatalf("store CreateDashboard(corrupt): %v", err)
	}
	invalid := &models.Dashboard{Name: "invalid", PanelsJSON: json.RawMessage(fmt.Sprintf(`{"version":1,"layout":[],"panels":[{"id":"p1","type":"pie","team_id":%d,"source_id":%d,"query_language":"logchefql"}]}`, team.ID, src.ID)), CreatedBy: &creator.ID}
	if err := db.CreateDashboard(ctx, invalid); err != nil {
		t.Fatalf("store CreateDashboard(invalid): %v", err)
	}

	list, err := ListDashboards(ctx, db, log, creator)
	if err != nil {
		t.Fatalf("ListDashboards: %v", err)
	}
	seen := map[int]*models.DashboardSummary{}
	for _, d := range list {
		seen[d.ID] = d
	}
	if g := seen[good.ID]; g == nil || g.Corrupt || g.PanelCount != 1 {
		t.Errorf("good dashboard = %+v", g)
	}
	for _, bad := range []*models.Dashboard{corrupt, invalid} {
		if c := seen[bad.ID]; c == nil || !c.Corrupt || c.PanelCount != 0 || len(c.PanelTypes) != 0 || !c.CanEdit {
			t.Errorf("corrupt dashboard %s = %+v", bad.Name, c)
		}
	}

	// A team member sees the valid row but neither corrupt one.
	memberList, err := ListDashboards(ctx, db, log, member)
	if err != nil {
		t.Fatalf("ListDashboards(member): %v", err)
	}
	ids := make([]int, 0, len(memberList))
	for _, d := range memberList {
		ids = append(ids, d.ID)
	}
	if !slices.Contains(ids, good.ID) || slices.Contains(ids, corrupt.ID) || slices.Contains(ids, invalid.ID) {
		t.Errorf("member sees %v, want only %d", ids, good.ID)
	}
	for _, bad := range []*models.Dashboard{corrupt, invalid} {
		can, err := UserCanViewDashboard(ctx, db, member, bad)
		if err != nil || can {
			t.Errorf("member can view corrupt row %s: %v %v", bad.Name, can, err)
		}
		resp, err := BuildDashboardResponse(ctx, db, log, creator, schema2Client(t), bad)
		if err != nil || !resp.Corrupt || resp.RawPanelsJSON != string(bad.PanelsJSON) || resp.Panels != nil || !resp.CanEdit {
			t.Errorf("creator's corrupt response for %s = %+v, %v", bad.Name, resp, err)
		}
	}

	// The full response must marshal without error (the B12 failure mode).
	if _, err := json.Marshal(list); err != nil {
		t.Fatalf("list response failed to marshal: %v", err)
	}
}

// TestListDashboardsRepairsStaleSummaries covers the startup backfill and the
// list repair: rows an older writer left stale are repaired before visibility
// applies, a second backfill writes nothing, and the list never needs a blob.
func TestListDashboardsRepairsStaleSummaries(t *testing.T) {
	db, dbPath := newTestDBWithPath(t)
	log := discardLogger()
	ctx := context.Background()
	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	member := newTestUser(t, db, "member@test.dev", "Member")
	team, src := seedTeamWithSource(t, db, "team-a", creator, member)

	var ids []int
	for i := range 60 {
		d, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: fmt.Sprintf("d%02d", i), Panels: panelBlob(team.ID, src.ID)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	raw := openRawSQLite(t, dbPath)
	if _, err := raw.ExecContext(ctx, "UPDATE dashboards SET summary_revision = 0, panel_team_ids = '[]'"); err != nil {
		t.Fatal(err)
	}

	list, err := ListDashboards(ctx, db, log, member)
	if err != nil {
		t.Fatalf("ListDashboards: %v", err)
	}
	if len(list) != 60 {
		t.Fatalf("member sees %d dashboards after repair, want 60", len(list))
	}
	for _, s := range list {
		if s.Stale() || !slices.Equal(s.PanelTeamIDs, []int{int(team.ID)}) || s.Revision != 1 {
			t.Fatalf("summary not repaired: %+v", s)
		}
	}
	n, err := BackfillDashboardSummaries(ctx, db, log)
	if err != nil || n != 0 {
		t.Fatalf("second backfill wrote %d rows, %v; want 0", n, err)
	}
	if !slices.Contains(ids, list[0].ID) {
		t.Fatal("list returned an unknown row")
	}
}

// newTestDBWithPath is newTestDB for tests that also need raw SQL on the file.
func newTestDBWithPath(t *testing.T) (db *sqlite.DB, dbPath string) {
	t.Helper()
	dbPath = sqlitetest.Path(t)
	db, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: discardLogger(),
		Config: config.SQLiteConfig{Path: dbPath},
	})
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dbPath
}

// openRawSQLite opens database/sql on the test store's file for statements
// the store interface does not offer.
func openRawSQLite(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

// TestUpdateDashboardRevisions covers the save contract: a body without a
// revision is refused, the loaded revision wins once and moves the row on, and
// a second save from that same revision is a conflict that writes nothing.
func TestUpdateDashboardRevisions(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	creator := newTestUser(t, db, "creator@test.dev", "Creator")
	team, src := seedTeamWithSource(t, db, "team-a", creator)
	dash, err := CreateDashboard(ctx, db, log, creator, &models.CreateDashboardRequest{Name: "d", Panels: panelBlob(team.ID, src.ID)})
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}
	if dash.Revision != 1 {
		t.Fatalf("new dashboard revision = %d, want 1", dash.Revision)
	}

	_, err = UpdateDashboard(ctx, db, log, dash.ID, creator, schema2Client(t), &models.UpdateDashboardRequest{
		Name: "d2", Panels: panelBlob(team.ID, src.ID),
	})
	if !errors.Is(err, ErrDashboardRevisionRequired) {
		t.Fatalf("update without a revision: err = %v, want ErrDashboardRevisionRequired", err)
	}

	loaded := dash.Revision
	updated, err := UpdateDashboard(ctx, db, log, dash.ID, creator, schema2Client(t), &models.UpdateDashboardRequest{
		Name: "d3", Panels: panelBlob(team.ID, src.ID), Revision: &loaded,
	})
	if err != nil {
		t.Fatalf("update from the loaded revision: %v", err)
	}
	if updated.Name != "d3" || updated.Revision != 2 {
		t.Fatalf("update returned name %q revision %d, want d3 at revision 2", updated.Name, updated.Revision)
	}

	_, err = UpdateDashboard(ctx, db, log, dash.ID, creator, schema2Client(t), &models.UpdateDashboardRequest{
		Name: "d4", Panels: panelBlob(team.ID, src.ID), Revision: &loaded,
	})
	if !errors.Is(err, ErrDashboardConflict) {
		t.Fatalf("second update from the same revision: err = %v, want ErrDashboardConflict", err)
	}
	stored, err := db.GetDashboard(ctx, dash.ID)
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	if stored.Name != "d3" || stored.Revision != 2 {
		t.Fatalf("stored row after the refused update: name %q revision %d, want d3 at revision 2", stored.Name, stored.Revision)
	}
}

type dashboardListRaceStore struct {
	store.StoreOps
	beforeList func()
	lists      int
}

func (s *dashboardListRaceStore) ListDashboardSummaries(ctx context.Context) ([]*models.DashboardSummary, error) {
	s.lists++
	s.beforeList()
	return s.StoreOps.ListDashboardSummaries(ctx)
}
func TestDashboardListConcurrentOldWriter(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		t.Run(fmt.Sprint(continuous), func(t *testing.T) {
			db, path := newTestDBWithPath(t)
			owner := newTestUser(t, db, "race@test.dev", "Owner")
			team, src := seedTeamWithSource(t, db, "team", owner)
			d, err := CreateDashboard(t.Context(), db, discardLogger(), owner, &models.CreateDashboardRequest{Name: "race", Panels: panelBlob(team.ID, src.ID)})
			if err != nil {
				t.Fatal(err)
			}
			raw := openRawSQLite(t, path)
			race := &dashboardListRaceStore{StoreOps: db}
			race.beforeList = func() {
				if continuous || race.lists == 1 {
					if _, err := raw.ExecContext(t.Context(), "UPDATE dashboards SET revision = revision + 1, name = 'old writer' WHERE id = ?", d.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			rows, err := ListDashboards(t.Context(), race, discardLogger(), owner)
			if continuous {
				if !errors.Is(err, ErrDashboardListUnavailable) || race.lists != 3 {
					t.Fatalf("%d passes, %v", race.lists, err)
				}
			} else if err != nil || race.lists != 2 || len(rows) != 1 || rows[0].Stale() || rows[0].Revision != 2 || rows[0].Name != "old writer" {
				t.Fatalf("%d passes, rows %+v, %v", race.lists, rows, err)
			}
		})
	}
}
