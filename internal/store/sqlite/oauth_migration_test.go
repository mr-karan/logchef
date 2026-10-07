package sqlite

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

const oauthMigrationVersion = 33

var oauthTables = []string{
	"oauth_grants", "oauth_auth_requests", "oauth_device_authorizations",
	"oauth_access_tokens", "oauth_refresh_tokens",
}

// TestOAuthMigrationUpDown applies and reverts 000033 on an empty database.
func TestOAuthMigrationUpDown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "oauth-migration.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	assertOAuthMigrationRoundTrip(t, db)
}

// TestOAuthMigrationOnDevDBCopy applies and reverts 000033 on a copy of a
// populated local database. Set LOGCHEF_TEST_SQLITE_DB to its path; the
// original file is only read.
func TestOAuthMigrationOnDevDBCopy(t *testing.T) {
	src := os.Getenv("LOGCHEF_TEST_SQLITE_DB")
	if src == "" {
		t.Skip("LOGCHEF_TEST_SQLITE_DB not set; skipping populated-DB migration check")
	}
	dst := filepath.Join(t.TempDir(), "dev-copy.db")
	copyFile(t, src, dst)
	if _, err := os.Stat(src + "-wal"); err == nil {
		copyFile(t, src+"-wal", dst+"-wal")
	}

	db, err := sql.Open("sqlite", dst)
	if err != nil {
		t.Fatalf("open copy: %v", err)
	}
	defer db.Close()
	users, sources := countRows(t, db, "users"), countRows(t, db, "sources")
	assertOAuthMigrationRoundTrip(t, db)
	if got := countRows(t, db, "users"); got != users {
		t.Errorf("users = %d after round trip, want %d", got, users)
	}
	if got := countRows(t, db, "sources"); got != sources {
		t.Errorf("sources = %d after round trip, want %d", got, sources)
	}
}

func assertOAuthMigrationRoundTrip(t *testing.T, db *sql.DB) {
	t.Helper()
	m := newMigrator(t, db)
	if err := m.Migrate(oauthMigrationVersion - 1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate to v%d: %v", oauthMigrationVersion-1, err)
	}
	assertOAuthTables(t, db, false)

	if err := m.Migrate(oauthMigrationVersion); err != nil {
		t.Fatalf("migrate up to v%d: %v", oauthMigrationVersion, err)
	}
	assertOAuthTables(t, db, true)
	mustExec(t, db, `INSERT INTO users (id, email, full_name, role, status) VALUES (900001, 'oauth-migration@test.dev', 'm', 'member', 'active')`)
	mustExec(t, db, `INSERT INTO oauth_grants (user_id, client_id, resource, scopes, created_at) VALUES (900001, 'c', 'r', '[]', 0)`)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("migrate down from v%d: %v", oauthMigrationVersion, err)
	}
	assertOAuthTables(t, db, false)
	if v, dirty, err := m.Version(); err != nil || dirty || v != oauthMigrationVersion-1 {
		t.Fatalf("version after down = %d dirty=%v err=%v", v, dirty, err)
	}
	mustExec(t, db, `DELETE FROM users WHERE id = 900001`)

	if err := m.Up(); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	assertOAuthTables(t, db, true)
}

func assertOAuthTables(t *testing.T, db *sql.DB, want bool) {
	t.Helper()
	for _, table := range oauthTables {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
			t.Fatalf("look up %s: %v", table, err)
		}
		if (n == 1) != want {
			t.Fatalf("table %s present = %v, want %v", table, n == 1, want)
		}
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
}
