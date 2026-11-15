-- Alerting v2 (RFC 0006, unit A1): one state row per (rule, group), the
-- columns that later units read, and checks widened for the no_data state.
--
-- SQLite cannot alter a CHECK, so alerts and alert_history are rebuilt. The
-- rebuild keeps every alert_history row because migrations run on a dedicated
-- connection opened with the bare file path (setupAndRunMigrations in
-- sqlite.go), where foreign_keys is off, so DROP TABLE alerts does not cascade.
-- TestAlertInstancesUpgrade seeds history and asserts that it survives.

-- DROP TABLE removes a table's sqlite_sequence row, and the copy only
-- restores the surviving maximum id. Keep the high-water marks so ids of
-- deleted alerts and pruned history rows are never handed out again.
CREATE TABLE alert_sequences_saved AS
SELECT name, seq FROM sqlite_sequence WHERE name IN ('alerts', 'alert_history');

CREATE TABLE alerts_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    query_language TEXT NOT NULL DEFAULT 'clickhouse-sql',
    editor_mode TEXT NOT NULL DEFAULT 'native' CHECK (editor_mode IN ('native', 'condition')),
    query TEXT,
    condition_json TEXT,
    lookback_seconds INTEGER NOT NULL DEFAULT 300,
    threshold_operator TEXT NOT NULL CHECK (threshold_operator IN ('gt', 'gte', 'lt', 'lte', 'eq', 'neq')),
    threshold_value REAL NOT NULL,
    frequency_seconds INTEGER NOT NULL DEFAULT 300,
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    labels_json TEXT,
    annotations_json TEXT,
    generator_url TEXT,
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    last_state TEXT NOT NULL DEFAULT 'resolved' CHECK (last_state IN ('firing', 'resolved', 'no_data')),
    last_evaluated_at DATETIME,
    last_triggered_at DATETIME,
    recipient_user_ids_json TEXT,
    webhook_urls_json TEXT,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    group_by_json TEXT,
    no_data_policy TEXT NOT NULL DEFAULT 'zero' CHECK (no_data_policy IN ('zero', 'no_data', 'keep')),
    eval_seq INTEGER NOT NULL DEFAULT 0
);

INSERT INTO alerts_new (
    id, source_id, name, description, query_language, editor_mode, query, condition_json,
    lookback_seconds, threshold_operator, threshold_value, frequency_seconds,
    severity, labels_json, annotations_json, generator_url, is_active,
    last_state, last_evaluated_at, last_triggered_at,
    recipient_user_ids_json, webhook_urls_json, created_by, created_at, updated_at
)
SELECT
    id, source_id, name, description, query_language, editor_mode, query, condition_json,
    lookback_seconds, threshold_operator, threshold_value, frequency_seconds,
    severity, labels_json, annotations_json, generator_url, is_active,
    last_state, last_evaluated_at, last_triggered_at,
    recipient_user_ids_json, webhook_urls_json, created_by, created_at, updated_at
FROM alerts;

DROP TABLE alerts;
ALTER TABLE alerts_new RENAME TO alerts;

CREATE INDEX IF NOT EXISTS idx_alerts_source_id ON alerts(source_id);
CREATE INDEX IF NOT EXISTS idx_alerts_is_active ON alerts(is_active);
CREATE INDEX IF NOT EXISTS idx_alerts_created_by ON alerts(created_by);
CREATE INDEX IF NOT EXISTS idx_alerts_query_language ON alerts(query_language);

CREATE TABLE alert_history_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('triggered', 'resolved', 'error', 'no_data')),
    triggered_at DATETIME NOT NULL DEFAULT (datetime('now')),
    resolved_at DATETIME,
    value REAL,
    message TEXT,
    payload_json TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    group_key TEXT NOT NULL DEFAULT '',
    group_json TEXT
);

INSERT INTO alert_history_new (id, alert_id, status, triggered_at, resolved_at, value, message, payload_json, created_at)
SELECT id, alert_id, status, triggered_at, resolved_at, value, message, payload_json, created_at
FROM alert_history;

DROP TABLE alert_history;
ALTER TABLE alert_history_new RENAME TO alert_history;

CREATE INDEX IF NOT EXISTS idx_alert_history_alert_id ON alert_history(alert_id);
CREATE INDEX IF NOT EXISTS idx_alert_history_triggered_at ON alert_history(alert_id, triggered_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_history_alert_status ON alert_history(alert_id, status);

UPDATE sqlite_sequence
SET seq = (SELECT saved.seq FROM alert_sequences_saved AS saved WHERE saved.name = sqlite_sequence.name)
WHERE name IN (SELECT saved.name FROM alert_sequences_saved AS saved WHERE saved.seq > sqlite_sequence.seq);
INSERT INTO sqlite_sequence (name, seq)
SELECT saved.name, saved.seq FROM alert_sequences_saved AS saved
WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence AS current WHERE current.name = saved.name);
DROP TABLE alert_sequences_saved;

CREATE TABLE IF NOT EXISTS alert_instances (
    alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    group_key TEXT NOT NULL,
    group_json TEXT NOT NULL DEFAULT '{}',
    state TEXT NOT NULL CHECK (state IN ('ok', 'firing', 'no_data')),
    value REAL,
    state_since DATETIME NOT NULL,
    last_seen_at DATETIME,
    history_id INTEGER,
    notified INTEGER NOT NULL DEFAULT 0 CHECK (notified IN (0, 1)),
    PRIMARY KEY (alert_id, group_key)
);

-- Backfill: the open episode is the row GetLatestUnresolvedAlertHistory picks
-- (newest triggered_at, then highest id). Older open rows of the same alert
-- are closed. The newest row is never updated, so the correlated subquery
-- returns the same row for every candidate.
UPDATE alert_history
SET status = 'resolved',
    resolved_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    message = 'superseded at upgrade'
WHERE status = 'triggered'
  AND id <> (
    SELECT latest.id FROM alert_history AS latest
    WHERE latest.alert_id = alert_history.alert_id AND latest.status = 'triggered'
    ORDER BY latest.triggered_at DESC, latest.id DESC
    LIMIT 1
  );

-- notified = 1: every open row has had a delivery attempt, so the first tick
-- after the upgrade sends no second firing message. A retry that was pending
-- or interrupted at upgrade is dropped (see the upgrade note).
INSERT INTO alert_instances (alert_id, group_key, group_json, state, value, state_since, last_seen_at, history_id, notified)
SELECT h.alert_id, '', '{}', 'firing', h.value, h.triggered_at, a.last_evaluated_at, h.id, 1
FROM alert_history AS h
JOIN alerts AS a ON a.id = h.alert_id
WHERE h.status = 'triggered'
ON CONFLICT (alert_id, group_key) DO NOTHING;
