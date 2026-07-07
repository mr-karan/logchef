package sqlite

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// preDatasourceVersion is the last schema version before the multi-datasource
// migrations (000025–000027) reshaped sources, saved_queries and alerts.
const preDatasourceVersion = 24

// TestDatasourceUpgradeFromV24 replays the pre-datasource schema, seeds rows in
// the legacy shape (flat ClickHouse columns, query_type discriminators), then
// applies the remaining migrations and asserts the data was carried over into
// the new shape (connection_config JSON + identity_key, query_language +
// editor_mode).
func TestDatasourceUpgradeFromV24(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := newMigrator(t, db)

	// 1. Bring the schema to the last pre-datasource version.
	if err := m.Migrate(preDatasourceVersion); err != nil {
		t.Fatalf("migrate to v%d: %v", preDatasourceVersion, err)
	}

	// 2. Seed legacy-shaped rows.
	mustExec(t, db, `INSERT INTO users (id, email, full_name, role, status) VALUES (1, 'admin@test.dev', 'Admin', 'admin', 'active')`)
	mustExec(t, db, `INSERT INTO teams (id, name) VALUES (1, 'team-a')`)
	mustExec(t, db, `INSERT INTO sources (
			id, name, _meta_is_auto_created, _meta_ts_field, _meta_severity_field,
			host, username, password, database, table_name, description, ttl_days, managed, tls_enable
		) VALUES
			(1, 'logs.app', 0, 'timestamp', 'severity_text', 'CH1.local:9000', 'default', 'sekret', 'Logs', 'App', 'app logs', 30, 0, 1),
			(2, 'logs.nginx', 0, 'ts', NULL, 'ch2.local:9000', 'reader', '', 'logs', 'nginx', NULL, 0, 1, 0)`)
	mustExec(t, db, `INSERT INTO team_sources (team_id, source_id) VALUES (1, 1), (1, 2)`)
	mustExec(t, db, `INSERT INTO saved_queries (id, source_id, name, description, query_type, query_content, created_by, created_from_team_id) VALUES
			(1, 1, 'errors', '5xx', 'logchefql', '{"content":"status>=500"}', 1, 1),
			(2, 1, 'raw scan', '', 'sql', '{"content":"SELECT 1"}', NULL, NULL)`)
	mustExec(t, db, `INSERT INTO alerts (
			id, source_id, name, query_type, query, condition_json, threshold_operator, threshold_value, severity, created_by
		) VALUES
			(1, 1, 'native alert', 'sql', 'SELECT count(*) FROM t', NULL, 'gt', 10, 'warning', 1),
			(2, 2, 'builder alert', 'condition', 'SELECT count(*) FROM t WHERE x', '{"field":"x"}', 'gte', 1, 'critical', NULL)`)

	// 3. Apply the datasource migrations.
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up from v%d: %v", preDatasourceVersion, err)
	}

	// 4. Sources: flat columns became connection_config + identity_key.
	var (
		sourceType, connConfig, identityKey string
		password, database, tableName       string
		tlsEnable                           bool
	)
	row := db.QueryRow(`SELECT source_type, connection_config, identity_key,
			json_extract(connection_config, '$.password'),
			json_extract(connection_config, '$.database'),
			json_extract(connection_config, '$.table_name'),
			json_extract(connection_config, '$.tls_enable')
		FROM sources WHERE id = 1`)
	if err := row.Scan(&sourceType, &connConfig, &identityKey, &password, &database, &tableName, &tlsEnable); err != nil {
		t.Fatalf("scan migrated source: %v", err)
	}
	if sourceType != "clickhouse" {
		t.Errorf("source_type = %q, want clickhouse", sourceType)
	}
	if password != "sekret" || database != "Logs" || tableName != "App" {
		t.Errorf("connection_config carried %q/%q/%q, want sekret/Logs/App", password, database, tableName)
	}
	if !tlsEnable {
		t.Errorf("tls_enable not carried into connection_config: %s", connConfig)
	}
	// Identity key lowercases and trims host/database/table.
	if want := "clickhouse:ch1.local:9000/logs/app"; identityKey != want {
		t.Errorf("identity_key = %q, want %q", identityKey, want)
	}

	if got := countRows(t, db, "sources"); got != 2 {
		t.Errorf("sources count = %d, want 2", got)
	}

	// 5. Saved queries: query_type became query_language + editor_mode.
	assertRow(t, db, `SELECT query_language || '/' || editor_mode FROM saved_queries WHERE id = 1`, "logchefql/builder")
	assertRow(t, db, `SELECT query_language || '/' || editor_mode FROM saved_queries WHERE id = 2`, "clickhouse-sql/native")
	assertRow(t, db, `SELECT query_content FROM saved_queries WHERE id = 1`, `{"content":"status>=500"}`)
	// created_from_team_id and created_by survive the table rebuild.
	assertRow(t, db, `SELECT COALESCE(created_from_team_id, -1) || '/' || COALESCE(created_by, -1) FROM saved_queries WHERE id = 1`, "1/1")

	// 6. Alerts: sql → native, condition → condition; language always clickhouse-sql.
	assertRow(t, db, `SELECT query_language || '/' || editor_mode FROM alerts WHERE id = 1`, "clickhouse-sql/native")
	assertRow(t, db, `SELECT query_language || '/' || editor_mode FROM alerts WHERE id = 2`, "clickhouse-sql/condition")

	// 7. Legacy columns are gone.
	for _, q := range []string{
		`SELECT host FROM sources LIMIT 1`,
		`SELECT query_type FROM saved_queries LIMIT 1`,
		`SELECT query_type FROM alerts LIMIT 1`,
	} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("legacy column still present: %s", q)
		}
	}

	// 8. No FK breakage from the table rebuilds.
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Error("foreign_key_check reported violations after upgrade")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check rows: %v", err)
	}
}

func newMigrator(t *testing.T, db *sql.DB) *migrate.Migrate {
	t.Helper()
	migrationFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	sourceDriver, err := iofs.New(migrationFS, ".")
	if err != nil {
		t.Fatalf("iofs: %v", err)
	}
	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{MigrationsTable: "schema_migrations"})
	if err != nil {
		t.Fatalf("migrate driver: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", driver)
	if err != nil {
		t.Fatalf("migrate instance: %v", err)
	}
	return m
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func assertRow(t *testing.T, db *sql.DB, query, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query %s: %v", query, err)
	}
	if got != want {
		t.Errorf("%s = %q, want %q", query, got, want)
	}
}
