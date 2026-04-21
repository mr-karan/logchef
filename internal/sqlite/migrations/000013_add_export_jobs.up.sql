CREATE TABLE IF NOT EXISTS export_jobs (
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

CREATE INDEX IF NOT EXISTS idx_export_jobs_team_source ON export_jobs(team_id, source_id);
CREATE INDEX IF NOT EXISTS idx_export_jobs_created_by ON export_jobs(created_by);
CREATE INDEX IF NOT EXISTS idx_export_jobs_expires_at ON export_jobs(expires_at);
