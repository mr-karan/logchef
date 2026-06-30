package sqlite_test

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/storetest"
)

// TestConformance runs the shared store.Store conformance suite against a fresh,
// migrated SQLite database in a temp dir.
func TestConformance(t *testing.T) {
	s, err := sqlite.New(sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "conformance.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	storetest.Run(t, s)
}
