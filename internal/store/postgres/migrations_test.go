package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/postgres"
)

func TestMigrationRecordingRollback(t *testing.T) {
	dsn := resetSchema(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, `CREATE TABLE schema_migrations (
version BIGINT PRIMARY KEY CHECK (version <> 1),
applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`); err != nil {
		t.Fatal(err)
	}
	s, err := postgres.New(ctx, postgres.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.PostgresConfig{DSN: dsn},
	})
	if err == nil {
		_ = s.Close()
		t.Fatal("migration succeeded despite rejected version record")
	}
	if !strings.Contains(err.Error(), "record migration 1") {
		t.Fatalf("unexpected migration failure: %v", err)
	}
	var users *string
	if err := conn.QueryRow(ctx, "SELECT to_regclass('public.users')::text").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != nil {
		t.Error("users table remains after migration version recording failed")
	}
	var count int
	if err := conn.QueryRow(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("recorded migrations = %d, want 0", count)
	}
	if _, err := conn.Exec(ctx, "ALTER TABLE schema_migrations DROP CONSTRAINT schema_migrations_version_check"); err != nil {
		t.Fatal(err)
	}
	opener(dsn)(t)
}
