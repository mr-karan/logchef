CREATE TABLE oauth_device_authorizations (
    device_code_hash TEXT PRIMARY KEY,
    user_code_hash   TEXT NOT NULL UNIQUE,
    client_id        TEXT NOT NULL,
    resource         TEXT NOT NULL,
    scopes           TEXT NOT NULL,
    offline_access   BOOLEAN NOT NULL DEFAULT FALSE,
    interval_secs    INTEGER NOT NULL,
    last_polled_at   TIMESTAMPTZ,
    user_id          BIGINT REFERENCES users(id) ON DELETE CASCADE,
    grant_id         BIGINT REFERENCES oauth_grants(id) ON DELETE CASCADE,
    approved_at      TIMESTAMPTZ,
    denied_at        TIMESTAMPTZ,
    consumed_at      TIMESTAMPTZ,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_oauth_device_authorizations_expires ON oauth_device_authorizations(expires_at);
