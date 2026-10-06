package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/storetest"
)

// TestConformance runs the shared store.Store conformance suite against Postgres.
// It is skipped unless LOGCHEF_TEST_POSTGRES_DSN points at a disposable database
// (the suite drops and recreates the public schema for a clean run).
func TestConformance(t *testing.T) {
	dsn := resetSchema(t)
	storetest.Run(t, opener(dsn)(t))
}

// TestOAuthConformance runs the OAuth suite. Each opened store has its own
// connection pool, like a separate replica.
func TestOAuthConformance(t *testing.T) {
	dsn := resetSchema(t)
	storetest.RunOAuth(t, opener(dsn))
}

// resetSchema returns the test DSN after dropping and recreating the public
// schema, so New() migrates from scratch and fixed test keys don't collide
// with a previous run.
func resetSchema(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("LOGCHEF_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("LOGCHEF_TEST_POSTGRES_DSN not set; skipping Postgres conformance")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect for reset: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	return dsn
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
