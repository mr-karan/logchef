package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDropRedundantIndexesMigration(t *testing.T) {
	dsn := resetSchema(t)
	opener(dsn)(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	indexes := []string{"idx_users_email", "idx_teams_name", "idx_team_members_team_id", "idx_team_sources_team_id", "idx_query_stats_daily_date"}
	assertIndexes := func(want bool) {
		t.Helper()
		for _, index := range indexes {
			var present bool
			if err := conn.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", index).Scan(&present); err != nil {
				t.Fatal(err)
			}
			if present != want {
				t.Errorf("index %s present = %t, want %t", index, present, want)
			}
		}
	}
	assertIndexes(false)
	var emailIndex bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('idx_users_email_lower') IS NOT NULL").Scan(&emailIndex); err != nil || !emailIndex {
		t.Fatalf("case-insensitive email index present = %t / %v", emailIndex, err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO users (email, full_name, role, status) VALUES ('owner@example.com', 'Owner', 'member', 'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO users (email, full_name, role, status) VALUES ('OWNER@example.com', 'Duplicate', 'member', 'active')`); err == nil {
		t.Error("case-insensitive duplicate email accepted")
	}
	if _, err := conn.Exec(ctx, readMigration(t, "000011_drop_redundant_indexes.down.sql")); err != nil {
		t.Fatal(err)
	}
	assertIndexes(true)
	if _, err := conn.Exec(ctx, readMigration(t, "000011_drop_redundant_indexes.up.sql")); err != nil {
		t.Fatal(err)
	}
	assertIndexes(false)
	if _, err := conn.Exec(ctx, readMigration(t, "000011_drop_redundant_indexes.up.sql")); err != nil {
		t.Fatalf("repeat up: %v", err)
	}
}
