package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDropRedundantIndexesMigration(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "indexes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := newMigrator(t, db)
	if err := m.Migrate(36); err != nil {
		t.Fatal(err)
	}
	indexes := []string{"idx_users_email", "idx_teams_name", "idx_team_members_team_id", "idx_team_sources_team_id", "idx_query_stats_daily_date"}
	for _, index := range indexes {
		assertIndexPresent(t, db, index, true)
	}
	if err := m.Steps(1); err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		assertIndexPresent(t, db, index, false)
	}
	assertIndexPresent(t, db, "idx_users_email_nocase", true)
	mustExec(t, db, `INSERT INTO users (email, full_name, role, status) VALUES ('owner@example.com', 'Owner', 'member', 'active')`)
	if _, err := db.Exec(`INSERT INTO users (email, full_name, role, status) VALUES ('OWNER@example.com', 'Duplicate', 'member', 'active')`); err == nil {
		t.Error("case-insensitive duplicate email accepted")
	}
	if err := m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		assertIndexPresent(t, db, index, true)
	}
	if err := m.Steps(1); err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		assertIndexPresent(t, db, index, false)
	}
}
