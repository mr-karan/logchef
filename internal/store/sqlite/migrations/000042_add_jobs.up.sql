-- Background jobs for internal/jobs (RFC 0006). Timestamps are UTC text in
-- the '%Y-%m-%dT%H:%M:%fZ' form, so comparisons with strftime('now') in the
-- same form are correct as text.
CREATE TABLE jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    key TEXT NOT NULL,
    schedule_kind TEXT NOT NULL CHECK (schedule_kind IN ('interval', 'cron')),
    schedule_expr TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    misfire TEXT NOT NULL CHECK (misfire IN ('run_once', 'skip')),
    grace_seconds INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    next_run_at DATETIME NOT NULL,
    lease_owner TEXT,
    lease_token INTEGER NOT NULL DEFAULT 0,
    lease_expires_at DATETIME,
    last_started_at DATETIME,
    last_finished_at DATETIME,
    last_status TEXT CHECK (last_status IN ('ok', 'error', 'skipped', 'lost')),
    last_error TEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE (kind, key)
);

CREATE INDEX idx_jobs_enabled_next_run_at ON jobs(enabled, next_run_at);
