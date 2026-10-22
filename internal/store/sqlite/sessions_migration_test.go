package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestRevokeNonUTCSessionsUpgradeFromV34(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	m := newMigrator(t, legacy)
	if err := m.Migrate(34); err != nil {
		t.Fatal(err)
	}
	mustExec(t, legacy, `INSERT INTO users (id, email, full_name, role, status)
		VALUES (1, 'sessions@example.com', 'Sessions', 'member', 'active')`)
	mustExec(t, legacy, `INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES
		('east-live', 1, '2026-10-21 19:30:00 +0530 IST m=+7200.000000001', datetime('now')),
		('east-expired', 1, '2026-10-21 15:30:00 +0530 IST', datetime('now')),
		('west-live', 1, '2026-10-21 09:00:00 -0500 CDT m=+7200.000000001', datetime('now')),
		('west-expired', 1, '2026-10-21 05:00:00 -0500 CDT', datetime('now')),
		('utc-live', 1, '2026-10-21 14:00:00 +0000 UTC m=+7200.000000001', datetime('now')),
		('utc-expired', 1, '2026-10-21 10:00:00 +0000 UTC', datetime('now'))`)
	if _, err := m.Close(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	db, err := New(ctx, Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, id := range []models.SessionID{"east-live", "east-expired", "west-live", "west-expired"} {
		if _, err := db.GetSession(ctx, id); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("legacy session %q survived migration, got %v", id, err)
		}
	}
	for _, id := range []models.SessionID{"utc-live", "utc-expired"} {
		if _, err := db.GetSession(ctx, id); err != nil {
			t.Errorf("UTC session %q lost during migration: %v", id, err)
		}
	}
	migration, err := migrationsFS.ReadFile("migrations/000035_revoke_non_utc_sessions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.writeDB.ExecContext(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, db.readDB, "sessions"); got != 2 {
		t.Errorf("sessions after repeated migration = %d, want 2", got)
	}
}
