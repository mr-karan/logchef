package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/storetest"
)

// TestConformance runs the shared store.Store conformance suite against Postgres.
// It is skipped unless LOGCHEF_TEST_POSTGRES_DSN points at a disposable database
// (the suite drops and recreates the public schema for a clean run).
func TestConformance(t *testing.T) {
	dsn := os.Getenv("LOGCHEF_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("LOGCHEF_TEST_POSTGRES_DSN not set; skipping Postgres conformance")
	}
	ctx := context.Background()

	// Reset to a clean schema so New() migrates from scratch and the suite's
	// fixed keys don't collide with a previous run.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect for reset: %v", err)
	}
	if _, err := conn.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("reset schema: %v", err)
	}
	_ = conn.Close(ctx)

	s, err := postgres.New(ctx, postgres.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.PostgresConfig{DSN: dsn},
	})
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	storetest.Run(t, s)
}
