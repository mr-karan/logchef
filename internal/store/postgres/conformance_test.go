package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/internal/store/storetest"
)

// TestConformance runs the shared store.Store conformance suite against Postgres.
// Each run uses its own schema in LOGCHEF_TEST_POSTGRES_DSN.
func TestConformance(t *testing.T) {
	dsn := postgrestest.DSN(t)
	s := opener(dsn)(t)
	storetest.Run(t, s)
	t.Run("MembershipWritesSerialized", func(t *testing.T) { storetest.RunMembershipWritesSerialized(t, s) })
	t.Run("ForUpdateSerialized", func(t *testing.T) { storetest.RunForUpdateSerialized(t, s) })
	t.Run("TeamForUpdateSerialized", func(t *testing.T) { storetest.RunTeamForUpdateSerialized(t, s) })
	t.Run("DashboardSummaries", func(t *testing.T) {
		conn, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			t.Fatalf("connect for hooks: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
		storetest.RunDashboardSummaries(t, s, storetest.SummaryStaleHooks{
			ResetSummaryRevision: func(ctx context.Context, id int) error {
				_, err := conn.Exec(ctx, "UPDATE dashboards SET summary_revision = 0 WHERE id = $1", id)
				return err
			},
			SetSummaryVersion: func(ctx context.Context, id, version int) error {
				_, err := conn.Exec(ctx, "UPDATE dashboards SET summary_version = $1 WHERE id = $2", version, id)
				return err
			},
		})
	})
}

// TestOAuthConformance runs the OAuth suite. Each opened store has its own
// connection pool, like a separate replica.
func TestOAuthConformance(t *testing.T) {
	dsn := postgrestest.DSN(t)
	storetest.RunOAuth(t, opener(dsn))
}

// TestSavedViewDefaultRace runs two default swaps from two pools; the second
// waits on the first's row or index lock.
func TestSavedViewDefaultRace(t *testing.T) {
	storetest.RunSavedViewDefaultRace(t, opener(postgrestest.DSN(t)))
}

// TestCollectionLinkRaces runs the link races with two pools, so the second
// writer waits on the collection row lock.
func TestCollectionLinkRaces(t *testing.T) {
	dsn := postgrestest.DSN(t)
	storetest.RunCollectionLinkRaces(t, opener(dsn))
}

// TestJobs runs the job claim and takeover tests with separate pools, like
// separate replicas.
func TestJobs(t *testing.T) {
	storetest.RunJobs(t, opener(postgrestest.DSN(t)), true)
}

// TestReports runs the report store tests with separate pools, like
// separate replicas.
func TestReports(t *testing.T) {
	storetest.RunReports(t, opener(postgrestest.DSN(t)))
}

func opener(dsn string) storetest.Opener {
	return func(t *testing.T) store.Store {
		t.Helper()
		s, err := postgres.New(context.Background(), postgres.Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Config: config.PostgresConfig{DSN: dsn},
		})
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
}

// TestShareLogRedaction asserts share error logs never carry the full token.
func TestShareLogRedaction(t *testing.T) {
	dsn := postgrestest.DSN(t)
	storetest.RunShareLogRedaction(t, func(t *testing.T, logger *slog.Logger) store.Store {
		t.Helper()
		s, err := postgres.New(context.Background(), postgres.Options{
			Logger: logger,
			Config: config.PostgresConfig{DSN: dsn},
		})
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}
