package sqlite_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/internal/store/storetest"
)

// TestConformance runs the shared store.Store conformance suite against a fresh,
// migrated SQLite database in a temp dir.
func TestConformance(t *testing.T) {
	path := sqlitetest.Path(t)
	s := opener(path)(t)
	storetest.Run(t, s)
	t.Run("DashboardSummaries", func(t *testing.T) {
		raw, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("open raw sqlite: %v", err)
		}
		t.Cleanup(func() { _ = raw.Close() })
		exec := func(ctx context.Context, query string, args ...any) error {
			_, err := raw.ExecContext(ctx, query, args...)
			return err
		}
		storetest.RunDashboardSummaries(t, s, storetest.SummaryStaleHooks{
			ResetSummaryRevision: func(ctx context.Context, id int) error {
				return exec(ctx, "UPDATE dashboards SET summary_revision = 0 WHERE id = ?", id)
			},
			SetSummaryVersion: func(ctx context.Context, id, version int) error {
				return exec(ctx, "UPDATE dashboards SET summary_version = ? WHERE id = ?", version, id)
			},
		})
	})
}

// TestOAuthConformance runs the OAuth suite. Each opened store has its own
// read pool and write connection on the same file, like separate processes.
func TestOAuthConformance(t *testing.T) {
	storetest.RunOAuth(t, opener(sqlitetest.Path(t)))
}

// TestSavedViewDefaultRace runs two default swaps from two store instances
// on one file; the second writer waits on SQLite's lock.
func TestSavedViewDefaultRace(t *testing.T) {
	storetest.RunSavedViewDefaultRace(t, opener(sqlitetest.Path(t)))
}

// TestCollectionLinkRaces runs the link races with two store instances on
// one file, so the second writer waits on SQLite's lock, not on Go.
func TestCollectionLinkRaces(t *testing.T) {
	storetest.RunCollectionLinkRaces(t, opener(sqlitetest.Path(t)))
}

// TestJobs runs the job claim and takeover tests with two store instances on
// one file, so a claim waits on SQLite's write lock, not on Go.
func TestJobs(t *testing.T) {
	storetest.RunJobs(t, opener(sqlitetest.Path(t)), false)
	storetest.RunDeliveries(t, opener(sqlitetest.Path(t)))
}

// TestReports runs the report store tests with two store instances on one
// file, so the cooldown race waits on SQLite's write lock, not on Go.
func TestReports(t *testing.T) {
	storetest.RunReports(t, opener(sqlitetest.Path(t)))
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

// TestShareLogRedaction asserts share error logs never carry the full token.
func TestShareLogRedaction(t *testing.T) {
	storetest.RunShareLogRedaction(t, func(t *testing.T, logger *slog.Logger) store.Store {
		t.Helper()
		s, err := sqlite.New(context.Background(), sqlite.Options{
			Logger: logger,
			Config: config.SQLiteConfig{Path: sqlitetest.Path(t)},
		})
		if err != nil {
			t.Fatalf("sqlite.New: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}
