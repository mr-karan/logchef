package provisioning

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newReconcileTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.New(sqlite.Options{
		Logger: quietLogger(),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "prov.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestReconcile_TeamsMembersLinks characterizes the team-reconciliation path:
// from an empty DB (with a pre-existing source so the ClickHouse-validation path
// is skipped), one Reconcile must create the team (managed), create the member
// user (managed) with the configured role, and link the source to the team.
//
// This is a behavioral snapshot to guard the upcoming WithTx/store port.
func TestReconcile_TeamsMembersLinks(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()

	// Pre-create the source so ManageSources stays off and Reconcile never
	// touches ClickHouse. Reconcile links teams to it by name.
	src := &models.Source{
		Name:        "src1",
		Connection:  models.ConnectionInfo{Host: "localhost:9000", Database: "default", TableName: "logs"},
		MetaTSField: "timestamp",
	}
	if err := db.CreateSource(ctx, src); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	cfg := &config.ProvisioningConfig{
		ManageSources: false,
		ManageTeams:   true,
		Teams: []config.ProvisionTeam{{
			Name:        "team1",
			Description: "Team One",
			Sources:     []string{"src1"},
			Members:     []config.ProvisionMember{{Email: "alice@example.com", Role: "admin"}},
		}},
	}

	if err := Reconcile(ctx, cfg, db, clickhouse.NewManager(quietLogger()), quietLogger(), []string{"admin@example.com"}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Team created and marked managed.
	team, err := db.GetTeamByName(ctx, "team1")
	if err != nil {
		t.Fatalf("team1 should exist: %v", err)
	}
	if managed, err := db.IsTeamManaged(ctx, team.ID); err != nil || !managed {
		t.Fatalf("team1 should be managed (managed=%v err=%v)", managed, err)
	}

	// Member user created, managed, with the configured team role.
	user, err := db.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("member user should exist: %v", err)
	}
	if managed, err := db.IsUserManaged(ctx, user.ID); err != nil || !managed {
		t.Fatalf("member user should be managed (managed=%v err=%v)", managed, err)
	}
	member, err := db.GetTeamMember(ctx, team.ID, user.ID)
	if err != nil {
		t.Fatalf("membership should exist: %v", err)
	}
	if member.Role != models.TeamRoleAdmin {
		t.Errorf("member role = %q, want admin", member.Role)
	}

	// Source linked to the team.
	sources, err := db.ListTeamSources(ctx, team.ID)
	if err != nil {
		t.Fatalf("ListTeamSources: %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "src1" {
		t.Fatalf("team should link src1, got %+v", sources)
	}
}

// TestReconcile_DryRunRollsBack verifies dry-run commits nothing.
func TestReconcile_DryRunRollsBack(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()

	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		DryRun:      true,
		Teams:       []config.ProvisionTeam{{Name: "ghost", Description: "should not persist"}},
	}

	if err := Reconcile(ctx, cfg, db, clickhouse.NewManager(quietLogger()), quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile dry-run: %v", err)
	}

	if _, err := db.GetTeamByName(ctx, "ghost"); err == nil {
		t.Fatal("dry-run should not have persisted team 'ghost'")
	}
}

// findSource returns the source with the given name, or nil.
func findSource(t *testing.T, db *sqlite.DB, name string) *models.Source {
	t.Helper()
	sources, err := db.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	for _, s := range sources {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// TestReconcile_SourceCreate covers ManageSources creating a brand-new source.
// The ClickHouse connection check is best-effort; an unreachable host (port 1)
// fails fast and the source is still created and marked managed.
func TestReconcile_SourceCreate(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()

	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{{
			Name:        "newsrc",
			Host:        "127.0.0.1:1",
			Username:    "u",
			Database:    "default",
			TableName:   "logs",
			MetaTSField: "timestamp",
		}},
	}

	if err := Reconcile(ctx, cfg, db, clickhouse.NewManager(quietLogger()), quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	src := findSource(t, db, "newsrc")
	if src == nil {
		t.Fatal("newsrc should have been created")
	}
	if managed, err := db.IsSourceManaged(ctx, src.ID); err != nil || !managed {
		t.Fatalf("newsrc should be managed (managed=%v err=%v)", managed, err)
	}
}

// TestReconcile_SourceAdopt covers adopting an existing unmanaged source: its
// fields are updated from config and it is marked managed (no ClickHouse path).
func TestReconcile_SourceAdopt(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()

	seed := &models.Source{
		Name:        "adopt",
		Connection:  models.ConnectionInfo{Host: "old:9000", Database: "default", TableName: "t1"},
		MetaTSField: "timestamp",
	}
	if err := db.CreateSource(ctx, seed); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if managed, _ := db.IsSourceManaged(ctx, seed.ID); managed {
		t.Fatal("seed source should start unmanaged")
	}

	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{{
			Name:        "adopt",
			Host:        "new:9000",
			Database:    "default",
			TableName:   "t1",
			MetaTSField: "timestamp",
		}},
	}
	if err := Reconcile(ctx, cfg, db, clickhouse.NewManager(quietLogger()), quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	src := findSource(t, db, "adopt")
	if src == nil {
		t.Fatal("adopt source missing")
	}
	if managed, err := db.IsSourceManaged(ctx, src.ID); err != nil || !managed {
		t.Fatalf("adopt source should be managed (managed=%v err=%v)", managed, err)
	}
	if src.Connection.Host != "new:9000" {
		t.Errorf("adopt host = %q, want new:9000 (fields should update)", src.Connection.Host)
	}
}

// TestReconcile_SourcePrune covers Prune deleting a managed source absent from
// config. The source is created managed in a first pass, then pruned in a
// second pass with an empty source list.
func TestReconcile_SourcePrune(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	chMgr := clickhouse.NewManager(quietLogger())

	create := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{{
			Name: "doomed", Host: "127.0.0.1:1", Database: "default", TableName: "logs", MetaTSField: "timestamp",
		}},
	}
	if err := Reconcile(ctx, create, db, chMgr, quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile create: %v", err)
	}
	if findSource(t, db, "doomed") == nil {
		t.Fatal("doomed should exist after create pass")
	}

	prune := &config.ProvisioningConfig{ManageSources: true, Prune: true}
	if err := Reconcile(ctx, prune, db, chMgr, quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile prune: %v", err)
	}
	if findSource(t, db, "doomed") != nil {
		t.Fatal("doomed should have been pruned")
	}
}

// TestReconcile_TeamAdopt covers adopting an existing unmanaged team.
func TestReconcile_TeamAdopt(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()

	seed := &models.Team{Name: "adopted", Description: "old desc"}
	if err := db.CreateTeam(ctx, seed); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	if managed, _ := db.IsTeamManaged(ctx, seed.ID); managed {
		t.Fatal("seed team should start unmanaged")
	}

	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams:       []config.ProvisionTeam{{Name: "adopted", Description: "new desc"}},
	}
	if err := Reconcile(ctx, cfg, db, clickhouse.NewManager(quietLogger()), quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	team, err := db.GetTeamByName(ctx, "adopted")
	if err != nil {
		t.Fatalf("adopted team missing: %v", err)
	}
	if managed, err := db.IsTeamManaged(ctx, team.ID); err != nil || !managed {
		t.Fatalf("adopted team should be managed (managed=%v err=%v)", managed, err)
	}
	if team.Description != "new desc" {
		t.Errorf("team description = %q, want updated 'new desc'", team.Description)
	}
}

// TestReconcile_MemberRoleUpdateAndPrune runs two passes: the second changes a
// member's role, removes another member, and unlinks a source — exercising the
// role-update, member-prune, and source-unlink-prune paths.
func TestReconcile_MemberRoleUpdateAndPrune(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	chMgr := clickhouse.NewManager(quietLogger())

	src := &models.Source{
		Name:        "s",
		Connection:  models.ConnectionInfo{Host: "h:9000", Database: "default", TableName: "logs"},
		MetaTSField: "timestamp",
	}
	if err := db.CreateSource(ctx, src); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	pass1 := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{{
			Name:    "t",
			Sources: []string{"s"},
			Members: []config.ProvisionMember{
				{Email: "alice@example.com", Role: "member"},
				{Email: "bob@example.com", Role: "admin"},
			},
		}},
	}
	if err := Reconcile(ctx, pass1, db, chMgr, quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile pass1: %v", err)
	}

	team, err := db.GetTeamByName(ctx, "t")
	if err != nil {
		t.Fatalf("team t missing: %v", err)
	}

	// Pass 2: alice promoted to admin, bob removed, source unlinked.
	pass2 := &config.ProvisioningConfig{
		ManageTeams: true,
		Prune:       true,
		Teams: []config.ProvisionTeam{{
			Name:    "t",
			Members: []config.ProvisionMember{{Email: "alice@example.com", Role: "admin"}},
		}},
	}
	if err := Reconcile(ctx, pass2, db, chMgr, quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile pass2: %v", err)
	}

	alice, _ := db.GetUserByEmail(ctx, "alice@example.com")
	m, err := db.GetTeamMember(ctx, team.ID, alice.ID)
	if err != nil {
		t.Fatalf("alice membership missing: %v", err)
	}
	if m.Role != models.TeamRoleAdmin {
		t.Errorf("alice role = %q, want admin after update", m.Role)
	}

	// GetTeamMember returns (nil, nil) for a non-member, so check the member.
	bob, _ := db.GetUserByEmail(ctx, "bob@example.com")
	if bobMember, err := db.GetTeamMember(ctx, team.ID, bob.ID); err != nil || bobMember != nil {
		t.Errorf("bob should have been pruned from the team (member=%v err=%v)", bobMember, err)
	}

	sources, err := db.ListTeamSources(ctx, team.ID)
	if err != nil {
		t.Fatalf("ListTeamSources: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("source should have been unlinked, got %d", len(sources))
	}
}
