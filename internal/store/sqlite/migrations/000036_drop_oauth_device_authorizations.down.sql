CREATE TABLE oauth_device_authorizations (
    device_code_hash TEXT PRIMARY KEY,
    user_code_hash TEXT NOT NULL UNIQUE,
    client_id TEXT NOT NULL,
    resource TEXT NOT NULL,
    scopes TEXT NOT NULL,
    offline_access INTEGER NOT NULL DEFAULT 0,
    interval_secs INTEGER NOT NULL,
    last_polled_at INTEGER,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    grant_id INTEGER REFERENCES oauth_grants(id) ON DELETE CASCADE,
    approved_at INTEGER,
    denied_at INTEGER,
    consumed_at INTEGER,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_oauth_device_authorizations_expires ON oauth_device_authorizations(expires_at);
