package sqlite_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/storetest"
)

// TestConformance runs the shared store.Store conformance suite against a fresh,
// migrated SQLite database in a temp dir.
func TestConformance(t *testing.T) {
	storetest.Run(t, opener(filepath.Join(t.TempDir(), "conformance.db"))(t))
}

// TestOAuthConformance runs the OAuth suite. Each opened store has its own
// read pool and write connection on the same file, like separate processes.
func TestOAuthConformance(t *testing.T) {
	storetest.RunOAuth(t, opener(filepath.Join(t.TempDir(), "oauth.db")))
}

func opener(path string) storetest.Opener {
	return func(t *testing.T) store.Store {
		t.Helper()
		s, err := sqlite.New(context.Background(), sqlite.Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Config: config.SQLiteConfig{Path: path},
		})
		if err != nil {
			t.Fatalf("sqlite.New: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
}
