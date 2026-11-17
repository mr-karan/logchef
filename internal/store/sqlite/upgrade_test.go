package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/storetest"
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

// TestCollectionTeamsUpgradeFromV33 seeds collection data at the last schema
// version before collection_teams, applies the migration, and asserts existing
// collections, members, and items are untouched and the new relation exists.
func TestCollectionTeamsUpgradeFromV33(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := newMigrator(t, db)
	if err := m.Migrate(33); err != nil {
		t.Fatalf("migrate to v33: %v", err)
	}

	mustExec(t, db, `INSERT INTO users (id, email, full_name, role, status) VALUES
			(1, 'owner@test.dev', 'Owner', 'member', 'active'),
			(2, 'member@test.dev', 'Member', 'member', 'active')`)
	mustExec(t, db, `INSERT INTO teams (id, name) VALUES (1, 'team-a')`)
	mustExec(t, db, `INSERT INTO sources (id, name, _meta_is_auto_created, _meta_ts_field, connection_config, identity_key)
			VALUES (1, 'logs.app', 0, 'timestamp', '{}', 'clickhouse:h/logs/app')`)
	mustExec(t, db, `INSERT INTO saved_queries (id, source_id, name, query_language, editor_mode, query_content, created_by)
			VALUES (1, 1, 'errors', 'logchefql', 'builder', '{}', 1)`)
	mustExec(t, db, `INSERT INTO collections (id, name, is_personal, created_by) VALUES (1, 'Shared', 0, 1)`)
	mustExec(t, db, `INSERT INTO collection_members (collection_id, user_id, role, added_by) VALUES (1, 1, 'owner', 1), (1, 2, 'editor', 1)`)
	mustExec(t, db, `INSERT INTO collection_items (collection_id, saved_query_id, added_by) VALUES (1, 1, 1)`)

	if err := m.Up(); err != nil {
		t.Fatalf("migrate up from v33: %v", err)
	}

	assertRow(t, db, `SELECT name || '/' || is_personal || '/' || created_by FROM collections WHERE id = 1`, "Shared/0/1")
	assertRow(t, db, `SELECT group_concat(user_id || ':' || role, ',') FROM (SELECT user_id, role FROM collection_members WHERE collection_id = 1 ORDER BY user_id)`, "1:owner,2:editor")
	assertRow(t, db, `SELECT saved_query_id FROM collection_items WHERE collection_id = 1`, "1")

	// Cascade behavior is covered by the store conformance suite, which runs
	// with foreign_keys enabled; here we only check the new relation exists.
	mustExec(t, db, `INSERT INTO collection_teams (collection_id, team_id, added_by) VALUES (1, 1, 1)`)
	assertRow(t, db, `SELECT collection_id || '/' || team_id FROM collection_teams`, "1/1")
}

// alertInstancesVersion is the migration that adds alert_instances and
// rebuilds alerts and alert_history (RFC 0006 unit A1).
const alertInstancesVersion = 43

// TestAlertInstancesUpgrade seeds alert history at the version before
// alertInstancesVersion, then opens the database through New, which runs the
// production migration path (a dedicated connection on the bare file path,
// foreign keys off). The alerts and alert_history rebuild must keep every
// history row; PRAGMA foreign_key_check alone would not show a cascade delete.
func TestAlertInstancesUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	m := newMigrator(t, raw)
	if err := m.Migrate(alertInstancesVersion - 1); err != nil {
		t.Fatalf("migrate to v%d: %v", alertInstancesVersion-1, err)
	}
	for _, query := range storetest.AlertInstancesUpgradeSeed {
		mustExec(t, raw, query)
	}
	// Rows above the surviving ids were deleted; their ids must stay retired.
	mustExec(t, raw, `UPDATE sqlite_sequence SET seq = 1000 WHERE name IN ('alerts', 'alert_history')`)
	if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v, %v", sourceErr, dbErr)
	}

	db, err := New(context.Background(), Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer db.Close()
	storetest.CheckAlertInstancesUpgrade(t, db)

	// The first tick opened an episode for the quiet rule after the retired ids.
	history, err := db.ListAlertHistory(context.Background(), 5, 1)
	if err != nil || len(history) != 1 || history[0].ID <= 1000 {
		t.Fatalf("newest history of alert 5 = %+v, %v; want an id above 1000", history, err)
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	assertRow(t, check, `SELECT seq FROM sqlite_sequence WHERE name = 'alerts'`, "1000")
	assertRow(t, check, `SELECT count(*) FROM sqlite_master WHERE name = 'alert_sequences_saved'`, "0")
}

// allowLokiSourceTypeVersion is the migration that rebuilds sources to accept
// source_type 'loki' (RFC 0011).
const allowLokiSourceTypeVersion = 47

// TestAllowLokiSourceTypeUpgrade seeds a source with every kind of child row
// before the sources rebuild, applies it, and asserts that every child row
// survives and still joins to its source, that the new CHECK accepts loki, and
// that the child foreign keys point at the rebuilt table.
func TestAllowLokiSourceTypeUpgrade(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := newMigrator(t, db)
	before := previousMigrationVersion(t, allowLokiSourceTypeVersion)
	if err := m.Migrate(before); err != nil {
		t.Fatalf("migrate to v%d: %v", before, err)
	}

	mustExec(t, db, `INSERT INTO users (id, email, full_name, role, status) VALUES (1, 'admin@test.dev', 'Admin', 'admin', 'active')`)
	mustExec(t, db, `INSERT INTO teams (id, name) VALUES (1, 'team-a'), (2, 'team-b')`)
	mustExec(t, db, `INSERT INTO sources (id, name, _meta_is_auto_created, source_type, _meta_ts_field, _meta_severity_field, connection_config, identity_key, description, ttl_days, managed, secret_ref) VALUES
			(1, 'logs.app', 0, 'clickhouse', 'timestamp', 'severity_text', '{"host":"ch:9000","database":"logs","table_name":"app"}', 'clickhouse:ch:9000/logs/app', 'app logs', 30, 1, 'LOGCHEF_SOURCE_APP'),
			(2, 'vl', 0, 'victorialogs', '_time', NULL, '{"base_url":"http://vl:9428"}', 'victorialogs:http://vl:9428|acct=|proj=|scope=', NULL, 0, 0, NULL)`)
	if _, err := db.Exec(`INSERT INTO sources (name, _meta_is_auto_created, source_type, connection_config, identity_key) VALUES ('loki', 0, 'loki', '{}', 'loki:early')`); err == nil {
		t.Fatalf("v%d accepted source_type loki before the rebuild", before)
	}
	// Source 9 was the newest and is gone; its ID must not be handed out again.
	mustExec(t, db, `INSERT INTO sources (id, name, _meta_is_auto_created, connection_config, identity_key) VALUES (9, 'deleted', 0, '{}', 'clickhouse:gone')`)
	mustExec(t, db, `DELETE FROM sources WHERE id = 9`)
	mustExec(t, db, `INSERT INTO team_sources (team_id, source_id) VALUES (1, 1), (2, 1), (1, 2)`)
	mustExec(t, db, `INSERT INTO saved_queries (id, source_id, name, query_language, editor_mode, query_content, created_by, created_from_team_id) VALUES
			(1, 1, 'errors', 'logchefql', 'builder', '{"content":"status>=500"}', 1, 1),
			(2, 2, 'vl errors', 'logsql', 'native', '{"content":"error"}', 1, NULL)`)
	mustExec(t, db, `INSERT INTO alerts (id, source_id, name, query_language, editor_mode, query, threshold_operator, threshold_value, severity, created_by) VALUES
			(1, 1, 'too many errors', 'clickhouse-sql', 'native', 'SELECT count(*) FROM logs.app', 'gt', 10, 'warning', 1)`)
	mustExec(t, db, `INSERT INTO query_shares (token, source_id, created_by, payload_json, expires_at, team_id) VALUES ('share-1', 1, 1, '{}', '2999-01-01 00:00:00', 1)`)
	mustExec(t, db, `INSERT INTO export_jobs (id, source_id, created_by, status, format, request_json, expires_at) VALUES ('job-1', 1, 1, 'complete', 'csv', '{}', '2999-01-01 00:00:00')`)
	mustExec(t, db, `INSERT INTO dashboards (id, name, panels_json, created_by) VALUES (1, 'ops', '{"version":1,"panels":[{"source_id":1}]}', 1)`)

	if err := m.Migrate(allowLokiSourceTypeVersion); err != nil {
		t.Fatalf("migrate to v%d: %v", allowLokiSourceTypeVersion, err)
	}

	assertRow(t, db, `SELECT group_concat(id || ':' || source_type || ':' || name || ':' || identity_key || ':' || ttl_days || ':' || managed || ':' || COALESCE(secret_ref, '-'), ',') FROM (SELECT * FROM sources ORDER BY id)`,
		"1:clickhouse:logs.app:clickhouse:ch:9000/logs/app:30:1:LOGCHEF_SOURCE_APP,2:victorialogs:vl:victorialogs:http://vl:9428|acct=|proj=|scope=:0:0:-")
	assertRow(t, db, `SELECT json_extract(connection_config, '$.table_name') FROM sources WHERE id = 1`, "app")
	assertRow(t, db, `SELECT group_concat(ts.team_id || '>' || s.name, ',') FROM (SELECT * FROM team_sources ORDER BY team_id, source_id) ts JOIN sources s ON s.id = ts.source_id`, "1>logs.app,1>vl,2>logs.app")
	assertRow(t, db, `SELECT group_concat(q.name || '>' || s.name, ',') FROM (SELECT * FROM saved_queries ORDER BY id) q JOIN sources s ON s.id = q.source_id`, "errors>logs.app,vl errors>vl")
	assertRow(t, db, `SELECT a.name || '>' || s.name FROM alerts a JOIN sources s ON s.id = a.source_id`, "too many errors>logs.app")
	assertRow(t, db, `SELECT q.token || '>' || s.name FROM query_shares q JOIN sources s ON s.id = q.source_id`, "share-1>logs.app")
	assertRow(t, db, `SELECT j.id || '>' || s.name FROM export_jobs j JOIN sources s ON s.id = j.source_id`, "job-1>logs.app")
	assertRow(t, db, `SELECT panels_json FROM dashboards WHERE id = 1`, `{"version":1,"panels":[{"source_id":1}]}`)
	assertRow(t, db, `SELECT group_concat(name, ',') FROM (SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'sources' AND sql IS NOT NULL ORDER BY name)`,
		"idx_sources_created_at,idx_sources_identity_key,idx_sources_source_type")

	mustExec(t, db, `INSERT INTO sources (name, _meta_is_auto_created, source_type, connection_config, identity_key) VALUES ('loki', 0, 'loki', '{"base_url":"http://loki:3100","selector":"{app=\"api\"}"}', 'loki:http://loki:3100|tenant=|selector={app="api"}|principal=')`)
	assertRow(t, db, `SELECT id FROM sources WHERE name = 'loki'`, "10")
	if _, err := db.Exec(`INSERT INTO sources (name, _meta_is_auto_created, source_type, connection_config, identity_key) VALUES ('other', 0, 'elastic', '{}', 'elastic:x')`); err == nil {
		t.Fatal("the rebuilt CHECK accepted source_type elastic")
	}

	// With foreign keys on, deleting a source must cascade into the children:
	// that proves their references resolve to the rebuilt table.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=on`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM sources WHERE id = 1`); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	for _, table := range []string{"team_sources", "saved_queries", "alerts", "query_shares", "export_jobs"} {
		var n int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE source_id = 1`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s kept %d rows of the deleted source", table, n)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}

	// The down migration refuses to drop the Loki source silently.
	if err := m.Migrate(before); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("down migration with a loki source: err = %v, want a CHECK failure", err)
	}
}

// TestAllowLokiSourceTypeDowngrade runs the down migration without Loki sources
// and asserts that it keeps the rows and the AUTOINCREMENT high-water mark.
func TestAllowLokiSourceTypeDowngrade(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "downgrade.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := newMigrator(t, db)
	if err := m.Migrate(allowLokiSourceTypeVersion); err != nil {
		t.Fatalf("migrate to v%d: %v", allowLokiSourceTypeVersion, err)
	}
	mustExec(t, db, `INSERT INTO teams (id, name) VALUES (1, 'team-a')`)
	mustExec(t, db, `INSERT INTO sources (id, name, _meta_is_auto_created, connection_config, identity_key) VALUES (1, 'app', 0, '{}', 'clickhouse:app'), (5, 'gone', 0, '{}', 'clickhouse:gone')`)
	mustExec(t, db, `DELETE FROM sources WHERE id = 5`)
	mustExec(t, db, `INSERT INTO team_sources (team_id, source_id) VALUES (1, 1)`)

	if err := m.Migrate(previousMigrationVersion(t, allowLokiSourceTypeVersion)); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	assertRow(t, db, `SELECT ts.team_id || '>' || s.name FROM team_sources ts JOIN sources s ON s.id = ts.source_id`, "1>app")
	mustExec(t, db, `INSERT INTO sources (name, _meta_is_auto_created, connection_config, identity_key) VALUES ('next', 0, '{}', 'clickhouse:next')`)
	assertRow(t, db, `SELECT id FROM sources WHERE name = 'next'`, "6")
}

// previousMigrationVersion returns the migration before version, so a test
// keeps working when its migration is renumbered at merge time.
func previousMigrationVersion(t *testing.T, version uint) uint {
	t.Helper()
	migrationFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	sourceDriver, err := iofs.New(migrationFS, ".")
	if err != nil {
		t.Fatalf("iofs: %v", err)
	}
	prev, err := sourceDriver.Prev(version)
	if err != nil {
		t.Fatalf("migration before v%d: %v", version, err)
	}
	return prev
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

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
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

// alertJobsVersion is the migration that gives every alert its alert.evaluate
// job (RFC 0006 unit J2).
const alertJobsVersion = 48

// TestAlertJobsUpgrade seeds alerts at the last version before
// alertJobsVersion, then opens the database through New, which runs the
// remaining migrations. A legacy "YYYY-MM-DD HH:MM:SS" evaluation time, which
// older binaries wrote, schedules the same as the RFC 3339 form.
func TestAlertJobsUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	m := newMigrator(t, raw)
	before := lastVersionBefore(t, alertJobsVersion)
	if err := m.Migrate(before); err != nil {
		t.Fatalf("migrate to v%d: %v", before, err)
	}
	for _, query := range storetest.AlertJobsUpgradeSeed {
		mustExec(t, raw, query)
	}
	mustExec(t, raw, `INSERT INTO alerts (id, source_id, name, query_language, editor_mode, query, lookback_seconds, threshold_operator,
			threshold_value, frequency_seconds, severity, is_active, last_state, last_evaluated_at)
		VALUES (5, 1, 'legacy time', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', 1, 'resolved', '2026-01-01 00:10:00')`)
	if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v, %v", sourceErr, dbErr)
	}

	db, err := New(context.Background(), Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer db.Close()
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	assertRow(t, check, `SELECT CAST(next_run_at AS TEXT) FROM jobs WHERE kind = 'alert.evaluate' AND key = '5'`, "2026-01-01T00:11:00.000Z")
	mustExec(t, check, `DELETE FROM jobs WHERE key = '5'`)
	storetest.CheckAlertJobsUpgrade(t, db)
}

// lastVersionBefore returns the newest migration version below v. Other
// branches may hold the numbers in between.
func lastVersionBefore(t *testing.T, v uint) uint {
	t.Helper()
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var last uint
	for _, e := range entries {
		var version uint
		if _, err := fmt.Sscanf(e.Name(), "%d_", &version); err != nil {
			t.Fatalf("parse version of %s: %v", e.Name(), err)
		}
		if version < v && version > last {
			last = version
		}
	}
	return last
}

// alertDestinationsVersion is the migration that moves rule targets into
// notification destinations (RFC 0006 unit D1).
const alertDestinationsVersion = 50

// TestAlertDestinationsUpgrade seeds rule targets at the newest version below
// alertDestinationsVersion, then opens the database through New, which runs
// the remaining migrations.
func TestAlertDestinationsUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	before := lastVersionBefore(t, alertDestinationsVersion)
	m := newMigrator(t, raw)
	if err := m.Migrate(before); err != nil {
		t.Fatalf("migrate to v%d: %v", before, err)
	}
	for _, query := range storetest.AlertDestinationsUpgradeSeed {
		mustExec(t, raw, query)
	}
	if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v, %v", sourceErr, dbErr)
	}

	db, err := New(context.Background(), Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer db.Close()
	storetest.CheckAlertDestinationsUpgrade(t, db)
}

// alertDeliveriesVersion adds the notification delivery queue (RFC 0006 unit
// D2).
const alertDeliveriesVersion = 55

// TestAlertDeliveriesUpgrade seeds open episodes with delivery bookkeeping at
// the newest version below alertDeliveriesVersion, then opens the database
// through New, which runs the remaining migrations.
func TestAlertDeliveriesUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	before := lastVersionBefore(t, alertDeliveriesVersion)
	m := newMigrator(t, raw)
	if err := m.Migrate(before); err != nil {
		t.Fatalf("migrate to v%d: %v", before, err)
	}
	for _, query := range storetest.AlertDeliveriesUpgradeSeed {
		mustExec(t, raw, query)
	}
	if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
		t.Fatalf("close migrator: %v, %v", sourceErr, dbErr)
	}

	db, err := New(context.Background(), Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer db.Close()
	storetest.CheckAlertDeliveriesUpgrade(t, db)
}
