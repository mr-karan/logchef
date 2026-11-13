// Package postgrestest gives tests an isolated, empty Postgres schema.
package postgrestest

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DSN creates a schema and returns a connection string scoped to it. Call it
// once per test and reuse the result for connections that must share data.
// Tests skip when LOGCHEF_TEST_POSTGRES_DSN is unset. Callers must close their
// connections before the test's cleanup drops the schema.
func DSN(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv("LOGCHEF_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("LOGCHEF_TEST_POSTGRES_DSN not set")
	}
	schema := "logchef_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schema}.Sanitize()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect for test schema: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Errorf("connect for test schema cleanup: %v", err)
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		if _, err := conn.Exec(ctx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("parse test DSN failed")
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		query.Set("application_name", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return dsn + " search_path=" + schema + " application_name=" + schema
}
