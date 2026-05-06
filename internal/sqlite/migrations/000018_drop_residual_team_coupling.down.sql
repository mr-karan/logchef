-- Best-effort revert: restore team_id NOT NULL on alerts, query_shares, and
-- export_jobs by picking the smallest team_id linked to the row's source.
-- alerts.created_by is dropped (v1 schema doesn't carry it).

-- ---------------- alerts ----------------

DROP INDEX IF EXISTS idx_alerts_created_by;
DROP INDEX IF EXISTS idx_alerts_last_state;
DROP INDEX IF EXISTS idx_alerts_last_evaluated;
DROP INDEX IF EXISTS idx_alerts_active;
DROP INDEX IF EXISTS idx_alerts_source;

CREATE TABLE alerts_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    query_type TEXT NOT NULL DEFAULT 'sql' CHECK (query_type IN ('sql', 'condition')),
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
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO alerts_old (
    id, team_id, source_id, name, description, query_type, query, condition_json,
    lookback_seconds, threshold_operator, threshold_value, frequency_seconds,
    severity, labels_json, annotations_json, generator_url, is_active,
    last_state, last_evaluated_at, last_triggered_at,
    recipient_user_ids_json, webhook_urls_json,
    created_at, updated_at
)
SELECT
    a.id,
    COALESCE(
        (SELECT MIN(ts.team_id) FROM team_sources ts WHERE ts.source_id = a.source_id),
        1
    ),
    a.source_id, a.name, a.description, a.query_type, a.query, a.condition_json,
    a.lookback_seconds, a.threshold_operator, a.threshold_value, a.frequency_seconds,
    a.severity, a.labels_json, a.annotations_json, a.generator_url, a.is_active,
    a.last_state, a.last_evaluated_at, a.last_triggered_at,
    a.recipient_user_ids_json, a.webhook_urls_json,
    a.created_at, a.updated_at
FROM alerts a;

DROP TABLE alerts;
ALTER TABLE alerts_old RENAME TO alerts;

CREATE INDEX idx_alerts_team_source ON alerts(team_id, source_id);
CREATE INDEX idx_alerts_active ON alerts(is_active);
CREATE INDEX idx_alerts_last_evaluated ON alerts(last_evaluated_at);
CREATE INDEX idx_alerts_last_state ON alerts(last_state);

-- ---------------- query_shares ----------------

DROP INDEX IF EXISTS idx_query_shares_expires_at;
DROP INDEX IF EXISTS idx_query_shares_created_by;
DROP INDEX IF EXISTS idx_query_shares_source;

CREATE TABLE query_shares_old (
    token TEXT PRIMARY KEY,
    team_id INTEGER NOT NULL,
    source_id INTEGER NOT NULL,
    created_by INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    last_accessed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);

INSERT INTO query_shares_old (token, team_id, source_id, created_by, payload_json, expires_at, last_accessed_at, created_at)
SELECT
    qs.token,
    COALESCE(
        (SELECT MIN(ts.team_id) FROM team_sources ts WHERE ts.source_id = qs.source_id),
        1
    ),
    qs.source_id,
    qs.created_by, qs.payload_json, qs.expires_at, qs.last_accessed_at, qs.created_at
FROM query_shares qs;

DROP TABLE query_shares;
ALTER TABLE query_shares_old RENAME TO query_shares;

CREATE INDEX IF NOT EXISTS idx_query_shares_team_source ON query_shares(team_id, source_id);
CREATE INDEX IF NOT EXISTS idx_query_shares_created_by ON query_shares(created_by);
CREATE INDEX IF NOT EXISTS idx_query_shares_expires_at ON query_shares(expires_at);

-- ---------------- export_jobs ----------------

DROP INDEX IF EXISTS idx_export_jobs_expires_at;
DROP INDEX IF EXISTS idx_export_jobs_created_by;
DROP INDEX IF EXISTS idx_export_jobs_source;

CREATE TABLE export_jobs_old (
    id TEXT PRIMARY KEY,
    team_id INTEGER NOT NULL,
    source_id INTEGER NOT NULL,
    created_by INTEGER NOT NULL,
    status TEXT NOT NULL,
    format TEXT NOT NULL,
    request_json TEXT NOT NULL,
    file_name TEXT,
    file_path TEXT,
    error_message TEXT,
    rows_exported INTEGER NOT NULL DEFAULT 0,
    bytes_written INTEGER NOT NULL DEFAULT 0,
    expires_at DATETIME NOT NULL,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);

INSERT INTO export_jobs_old (id, team_id, source_id, created_by, status, format, request_json, file_name, file_path, error_message, rows_exported, bytes_written, expires_at, completed_at, created_at, updated_at)
SELECT
    ej.id,
    COALESCE(
        (SELECT MIN(ts.team_id) FROM team_sources ts WHERE ts.source_id = ej.source_id),
        1
    ),
    ej.source_id,
    ej.created_by, ej.status, ej.format, ej.request_json, ej.file_name, ej.file_path,
    ej.error_message, ej.rows_exported, ej.bytes_written, ej.expires_at, ej.completed_at,
    ej.created_at, ej.updated_at
FROM export_jobs;

DROP TABLE export_jobs;
ALTER TABLE export_jobs_old RENAME TO export_jobs;

CREATE INDEX IF NOT EXISTS idx_export_jobs_team_source ON export_jobs(team_id, source_id);
CREATE INDEX IF NOT EXISTS idx_export_jobs_created_by ON export_jobs(created_by);
CREATE INDEX IF NOT EXISTS idx_export_jobs_expires_at ON export_jobs(expires_at);
