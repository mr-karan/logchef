// Package sqlitetest gives tests a fresh, already-migrated SQLite database file.
package sqlitetest

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
)

var (
	templateOnce  sync.Once
	templateBytes []byte
	templateErr   error
)

// Path returns a database path in t.TempDir() that already holds the migrated
// schema, so sqlite.New on it skips replaying every migration.
func Path(t testing.TB) string {
	t.Helper()
	templateOnce.Do(func() { templateBytes, templateErr = buildTemplate() })
	if templateErr != nil {
		t.Fatalf("building migrated sqlite template: %v", templateErr)
	}
	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, templateBytes, 0o600); err != nil {
		t.Fatalf("writing sqlite template: %v", err)
	}
	return path
}

func buildTemplate() ([]byte, error) {
	dir, err := os.MkdirTemp("", "logchef-sqlite-template-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "template.db")
	db, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		return nil, err
	}
	// Closing the last connection checkpoints the WAL into the main file.
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}
