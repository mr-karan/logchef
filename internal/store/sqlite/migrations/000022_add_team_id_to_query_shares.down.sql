CREATE TABLE query_shares_no_team (
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

INSERT INTO query_shares_no_team (token, source_id, created_by, payload_json, expires_at, last_accessed_at, created_at)
SELECT token, source_id, created_by, payload_json, expires_at, last_accessed_at, created_at
FROM query_shares;

DROP TABLE query_shares;
ALTER TABLE query_shares_no_team RENAME TO query_shares;

CREATE INDEX IF NOT EXISTS idx_query_shares_source ON query_shares(source_id);
CREATE INDEX IF NOT EXISTS idx_query_shares_created_by ON query_shares(created_by);
CREATE INDEX IF NOT EXISTS idx_query_shares_expires_at ON query_shares(expires_at);
