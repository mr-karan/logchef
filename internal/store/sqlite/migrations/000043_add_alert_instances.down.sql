-- Thin down path: drops instances and the new columns. no_data states map to
-- resolved because the old checks reject them.
DROP TABLE IF EXISTS alert_instances;

-- DROP TABLE removes a table's sqlite_sequence row, and the copy only
-- restores the surviving maximum id. Keep the high-water marks so ids of
-- deleted alerts and pruned history rows are never handed out again.
CREATE TABLE alert_sequences_saved AS
SELECT name, seq FROM sqlite_sequence WHERE name IN ('alerts', 'alert_history');

CREATE TABLE alerts_old (
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
    last_state TEXT NOT NULL DEFAULT 'resolved' CHECK (last_state IN ('firing', 'resolved')),
    last_evaluated_at DATETIME,
    last_triggered_at DATETIME,
    recipient_user_ids_json TEXT,
    webhook_urls_json TEXT,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO alerts_old (
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
    CASE WHEN last_state = 'no_data' THEN 'resolved' ELSE last_state END, last_evaluated_at, last_triggered_at,
    recipient_user_ids_json, webhook_urls_json, created_by, created_at, updated_at
FROM alerts;

DROP TABLE alerts;
ALTER TABLE alerts_old RENAME TO alerts;

CREATE INDEX IF NOT EXISTS idx_alerts_source_id ON alerts(source_id);
CREATE INDEX IF NOT EXISTS idx_alerts_is_active ON alerts(is_active);
CREATE INDEX IF NOT EXISTS idx_alerts_created_by ON alerts(created_by);
CREATE INDEX IF NOT EXISTS idx_alerts_query_language ON alerts(query_language);

CREATE TABLE alert_history_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('triggered', 'resolved', 'error')),
    triggered_at DATETIME NOT NULL DEFAULT (datetime('now')),
    resolved_at DATETIME,
    value REAL,
    message TEXT,
    payload_json TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO alert_history_old (id, alert_id, status, triggered_at, resolved_at, value, message, payload_json, created_at)
SELECT id, alert_id, CASE WHEN status = 'no_data' THEN 'resolved' ELSE status END, triggered_at, resolved_at, value, message, payload_json, created_at
FROM alert_history;

DROP TABLE alert_history;
ALTER TABLE alert_history_old RENAME TO alert_history;

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
