-- Drop team_id from alerts, query_shares, and export_jobs. Add nullable
-- created_by to alerts (the other two already have it). Visibility and edit
-- access are now driven by source membership and creator/admin checks at the
-- application layer, mirroring the saved-queries change in 000017.

-- ---------------- alerts ----------------

DROP INDEX IF EXISTS idx_alerts_team_source;

CREATE TABLE alerts_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
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
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO alerts_new (
    id, source_id, name, description, query_type, query, condition_json,
    lookback_seconds, threshold_operator, threshold_value, frequency_seconds,
    severity, labels_json, annotations_json, generator_url, is_active,
    last_state, last_evaluated_at, last_triggered_at,
    recipient_user_ids_json, webhook_urls_json,
    created_at, updated_at
)
SELECT
    id, source_id, name, description, query_type, query, condition_json,
    lookback_seconds, threshold_operator, threshold_value, frequency_seconds,
    severity, labels_json, annotations_json, generator_url, is_active,
    last_state, last_evaluated_at, last_triggered_at,
    recipient_user_ids_json, webhook_urls_json,
    created_at, updated_at
FROM alerts;

DROP TABLE alerts;
ALTER TABLE alerts_new RENAME TO alerts;

CREATE INDEX idx_alerts_source ON alerts(source_id);
CREATE INDEX idx_alerts_active ON alerts(is_active);
CREATE INDEX idx_alerts_last_evaluated ON alerts(last_evaluated_at);
CREATE INDEX idx_alerts_last_state ON alerts(last_state);
CREATE INDEX idx_alerts_created_by ON alerts(created_by);

-- ---------------- query_shares ----------------

DROP INDEX IF EXISTS idx_query_shares_team_source;

CREATE TABLE query_shares_new (
    token TEXT PRIMARY KEY,
    source_id INTEGER NOT NULL,
    created_by INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    last_accessed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);

INSERT INTO query_shares_new (token, source_id, created_by, payload_json, expires_at, last_accessed_at, created_at)
SELECT token, source_id, created_by, payload_json, expires_at, last_accessed_at, created_at
FROM query_shares;

DROP TABLE query_shares;
ALTER TABLE query_shares_new RENAME TO query_shares;

CREATE INDEX IF NOT EXISTS idx_query_shares_source ON query_shares(source_id);
CREATE INDEX IF NOT EXISTS idx_query_shares_created_by ON query_shares(created_by);
CREATE INDEX IF NOT EXISTS idx_query_shares_expires_at ON query_shares(expires_at);

-- ---------------- export_jobs ----------------

DROP INDEX IF EXISTS idx_export_jobs_team_source;

CREATE TABLE export_jobs_new (
    id TEXT PRIMARY KEY,
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
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);

INSERT INTO export_jobs_new (id, source_id, created_by, status, format, request_json, file_name, file_path, error_message, rows_exported, bytes_written, expires_at, completed_at, created_at, updated_at)
SELECT id, source_id, created_by, status, format, request_json, file_name, file_path, error_message, rows_exported, bytes_written, expires_at, completed_at, created_at, updated_at
FROM export_jobs;

DROP TABLE export_jobs;
ALTER TABLE export_jobs_new RENAME TO export_jobs;

CREATE INDEX IF NOT EXISTS idx_export_jobs_source ON export_jobs(source_id);
CREATE INDEX IF NOT EXISTS idx_export_jobs_created_by ON export_jobs(created_by);
CREATE INDEX IF NOT EXISTS idx_export_jobs_expires_at ON export_jobs(expires_at);
