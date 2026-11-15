-- Audit events: configuration changes and security events (RFC 0009). No
-- foreign keys, so an event outlives its actor and its target. occurred_at is
-- written by the store as '%Y-%m-%dT%H:%M:%fZ' UTC text, so text comparison
-- orders it.
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    occurred_at DATETIME NOT NULL,
    action TEXT NOT NULL,
    outcome TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    actor_type TEXT NOT NULL,
    actor_id INTEGER,
    actor_label TEXT NOT NULL DEFAULT '',
    auth_method TEXT NOT NULL,
    credential_id TEXT NOT NULL DEFAULT '',
    client_id TEXT NOT NULL DEFAULT '',
    target_type TEXT NOT NULL DEFAULT '',
    target_id TEXT NOT NULL DEFAULT '',
    target_label TEXT NOT NULL DEFAULT '',
    team_id INTEGER,
    source_id INTEGER,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    http_method TEXT NOT NULL DEFAULT '',
    route TEXT NOT NULL DEFAULT '',
    status INTEGER NOT NULL DEFAULT 0,
    changes TEXT NOT NULL DEFAULT '{}',
    detail TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_audit_events_time ON audit_events(occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_action ON audit_events(action, occurred_at DESC);
CREATE INDEX idx_audit_events_actor ON audit_events(actor_id, occurred_at DESC);
CREATE INDEX idx_audit_events_target ON audit_events(target_type, target_id, occurred_at DESC);
