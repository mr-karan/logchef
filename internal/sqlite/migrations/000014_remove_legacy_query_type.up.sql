PRAGMA foreign_keys=off;

DROP INDEX IF EXISTS idx_team_queries_query_language;
DROP INDEX IF EXISTS idx_team_queries_editor_mode;
DROP INDEX IF EXISTS idx_team_queries_query_type;

CREATE TABLE team_queries_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL,
    source_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    query_language TEXT NOT NULL DEFAULT 'clickhouse-sql',
    editor_mode TEXT NOT NULL DEFAULT 'native',
    query_content TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    is_bookmarked BOOLEAN NOT NULL DEFAULT FALSE,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE
);

INSERT INTO team_queries_new (
    id,
    team_id,
    source_id,
    name,
    description,
    query_language,
    editor_mode,
    query_content,
    created_at,
    updated_at,
    is_bookmarked
)
SELECT
    id,
    team_id,
    source_id,
    name,
    description,
    query_language,
    editor_mode,
    query_content,
    created_at,
    updated_at,
    is_bookmarked
FROM team_queries;

DROP TABLE team_queries;
ALTER TABLE team_queries_new RENAME TO team_queries;

CREATE INDEX IF NOT EXISTS idx_team_queries_team_id ON team_queries(team_id);
CREATE INDEX IF NOT EXISTS idx_team_queries_source_id ON team_queries(source_id);
CREATE INDEX IF NOT EXISTS idx_team_queries_name ON team_queries(name);
CREATE INDEX IF NOT EXISTS idx_team_queries_created_at ON team_queries(created_at);
CREATE INDEX IF NOT EXISTS idx_team_queries_source_team ON team_queries(source_id, team_id);
CREATE INDEX IF NOT EXISTS idx_team_queries_bookmarked ON team_queries(team_id, source_id, is_bookmarked, updated_at);
CREATE INDEX IF NOT EXISTS idx_team_queries_query_language ON team_queries(query_language);
CREATE INDEX IF NOT EXISTS idx_team_queries_editor_mode ON team_queries(editor_mode);

DROP INDEX IF EXISTS idx_alerts_query_language;
DROP INDEX IF EXISTS idx_alerts_editor_mode;

CREATE TABLE alerts_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    query_language TEXT NOT NULL DEFAULT 'clickhouse-sql',
    editor_mode TEXT NOT NULL DEFAULT 'native',
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
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    recipient_user_ids_json TEXT,
    webhook_urls_json TEXT
);

INSERT INTO alerts_new (
    id,
    team_id,
    source_id,
    name,
    description,
    query_language,
    editor_mode,
    query,
    condition_json,
    lookback_seconds,
    threshold_operator,
    threshold_value,
    frequency_seconds,
    severity,
    labels_json,
    annotations_json,
    generator_url,
    is_active,
    last_state,
    last_evaluated_at,
    last_triggered_at,
    created_at,
    updated_at,
    recipient_user_ids_json,
    webhook_urls_json
)
SELECT
    id,
    team_id,
    source_id,
    name,
    description,
    query_language,
    editor_mode,
    query,
    condition_json,
    lookback_seconds,
    threshold_operator,
    threshold_value,
    frequency_seconds,
    severity,
    labels_json,
    annotations_json,
    generator_url,
    is_active,
    last_state,
    last_evaluated_at,
    last_triggered_at,
    created_at,
    updated_at,
    recipient_user_ids_json,
    webhook_urls_json
FROM alerts;

DROP TABLE alerts;
ALTER TABLE alerts_new RENAME TO alerts;

CREATE INDEX idx_alerts_team_source ON alerts(team_id, source_id);
CREATE INDEX idx_alerts_active ON alerts(is_active);
CREATE INDEX idx_alerts_last_evaluated ON alerts(last_evaluated_at);
CREATE INDEX idx_alerts_last_state ON alerts(last_state);
CREATE INDEX IF NOT EXISTS idx_alerts_query_language ON alerts(query_language);
CREATE INDEX IF NOT EXISTS idx_alerts_editor_mode ON alerts(editor_mode);

PRAGMA foreign_keys=on;
