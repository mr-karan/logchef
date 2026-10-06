package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestOAuthMigrationUpDown checks that 000008 applies on a migrated database,
// that its down file drops only the OAuth tables, and that it re-applies.
func TestOAuthMigrationUpDown(t *testing.T) {
	dsn := resetSchema(t)
	opener(dsn)(t)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	assertOAuthTables(t, ctx, conn, 5)
	if _, err := conn.Exec(ctx, readMigration(t, "000008_oauth.down.sql")); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	assertOAuthTables(t, ctx, conn, 0)
	var users int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'users'`).Scan(&users); err != nil || users != 1 {
		t.Fatalf("users table after down = %d / %v", users, err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, "000008_oauth.up.sql")); err != nil {
		t.Fatalf("up migration again: %v", err)
	}
	assertOAuthTables(t, ctx, conn, 5)
}

func assertOAuthTables(t *testing.T, ctx context.Context, conn *pgx.Conn, want int) {
	t.Helper()
	var n int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name LIKE 'oauth\_%'`).Scan(&n); err != nil {
		t.Fatalf("count oauth tables: %v", err)
	}
	if n != want {
		t.Fatalf("oauth tables = %d, want %d", n, want)
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}
