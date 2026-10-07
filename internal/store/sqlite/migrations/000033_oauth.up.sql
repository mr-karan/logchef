-- Logchef OAuth authorization server storage. Codes and tokens are never
-- stored in plaintext: every *_hash column holds an HMAC-SHA256 hex digest.
-- Timestamps are INTEGER Unix milliseconds, not DATETIME, because the
-- expiry and poll-interval predicates compare them in SQL and text dates
-- written by the driver do not sort reliably.

-- A grant is one completed consent ("connected app") and one refresh family.
CREATE TABLE oauth_grants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id TEXT NOT NULL,
    resource TEXT NOT NULL,
    scopes TEXT NOT NULL,
    offline_access INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER,
    revoked_at INTEGER,
    revoke_reason TEXT
);
CREATE INDEX idx_oauth_grants_user ON oauth_grants(user_id);

CREATE TABLE oauth_auth_requests (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    resource TEXT NOT NULL,
    scopes TEXT NOT NULL,
    offline_access INTEGER NOT NULL DEFAULT 0,
    code_challenge TEXT NOT NULL,
    state TEXT NOT NULL,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    grant_id INTEGER REFERENCES oauth_grants(id) ON DELETE CASCADE,
    code_hash TEXT UNIQUE,
    code_expires_at INTEGER,
    code_consumed_at INTEGER,
    decided_at INTEGER,
    denied INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_oauth_auth_requests_expires ON oauth_auth_requests(expires_at);

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

CREATE TABLE oauth_access_tokens (
    id_hash TEXT PRIMARY KEY,
    grant_id INTEGER NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_oauth_access_tokens_grant ON oauth_access_tokens(grant_id);
CREATE INDEX idx_oauth_access_tokens_expires ON oauth_access_tokens(expires_at);

CREATE TABLE oauth_refresh_tokens (
    token_hash TEXT PRIMARY KEY,
    grant_id INTEGER NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER,
    replaced_by_hash TEXT,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_oauth_refresh_tokens_grant ON oauth_refresh_tokens(grant_id, expires_at);
