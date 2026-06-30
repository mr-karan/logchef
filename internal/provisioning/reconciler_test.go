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
