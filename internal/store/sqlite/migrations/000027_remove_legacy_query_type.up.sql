-- Drop the legacy query_type discriminator now that query_language and
-- editor_mode are authoritative (backfilled in 000026).
PRAGMA foreign_keys=off;

CREATE TABLE saved_queries_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    query_language TEXT NOT NULL DEFAULT 'clickhouse-sql',
    editor_mode TEXT NOT NULL DEFAULT 'native',
    query_content TEXT NOT NULL,
    created_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    created_from_team_id INTEGER REFERENCES teams(id) ON DELETE SET NULL,
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
);

INSERT INTO saved_queries_new (
    id, source_id, name, description, query_language, editor_mode,
    query_content, created_by, created_at, updated_at, created_from_team_id
)
SELECT
    id, source_id, name, description, query_language, editor_mode,
    query_content, created_by, created_at, updated_at, created_from_team_id
FROM saved_queries;

DROP TABLE saved_queries;
ALTER TABLE saved_queries_new RENAME TO saved_queries;

CREATE INDEX IF NOT EXISTS idx_saved_queries_source ON saved_queries(source_id);
CREATE INDEX IF NOT EXISTS idx_saved_queries_created_by ON saved_queries(created_by);
CREATE INDEX IF NOT EXISTS idx_saved_queries_created_from_team ON saved_queries(created_from_team_id);
CREATE INDEX IF NOT EXISTS idx_saved_queries_query_language ON saved_queries(query_language);

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
    last_state TEXT NOT NULL DEFAULT 'resolved' CHECK (last_state IN ('firing', 'resolved')),
    last_evaluated_at DATETIME,
    last_triggered_at DATETIME,
    recipient_user_ids_json TEXT,
    webhook_urls_json TEXT,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
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

PRAGMA foreign_keys=on;
PRAGMA foreign_key_check;
