package postgrestest_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
)

func TestSchemaIsolation(t *testing.T) {
	open := func(t *testing.T) *pgx.Conn {
		t.Helper()
		dsn := postgrestest.DSN(t)
		db, err := postgres.New(t.Context(), postgres.Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Config: config.PostgresConfig{DSN: dsn},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		conn, err := pgx.Connect(t.Context(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
		var schema, migrationSchema string
		if err := conn.QueryRow(t.Context(), `SELECT current_schema(), n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.oid = 'schema_migrations'::regclass`).Scan(&schema, &migrationSchema); err != nil {
			t.Fatal(err)
		}
		if schema == "public" || migrationSchema != schema {
			t.Fatalf("schema = %q, migration schema = %q", schema, migrationSchema)
		}
		return conn
	}
	survivor := open(t)
	if _, err := survivor.Exec(t.Context(), "CREATE TABLE isolation_marker (value text); INSERT INTO isolation_marker VALUES ('survives')"); err != nil {
		t.Fatal(err)
	}
	var removedSchema string
	t.Run("independent", func(t *testing.T) {
		conn := open(t)
		if err := conn.QueryRow(t.Context(), "SELECT current_schema()").Scan(&removedSchema); err != nil {
			t.Fatal(err)
		}
		var marker *string
		if err := conn.QueryRow(t.Context(), "SELECT to_regclass('isolation_marker')::text").Scan(&marker); err != nil {
			t.Fatal(err)
		}
		if marker != nil {
			t.Fatal("another test's table is visible")
		}
	})
	var value string
	if err := survivor.QueryRow(t.Context(), "SELECT value FROM isolation_marker").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "survives" {
		t.Fatalf("marker = %q", value)
	}
	var exists bool
	if err := survivor.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", removedSchema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("test cleanup left its schema behind")
	}
}
