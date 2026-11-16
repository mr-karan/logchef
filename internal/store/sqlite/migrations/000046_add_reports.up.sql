-- Scheduled reports (RFC 0008). Timestamps are UTC text in the
-- '%Y-%m-%dT%H:%M:%fZ' form, so the cooldown, deadline and tick comparisons
-- with strftime('now') in the same form are correct as text.
CREATE TABLE reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('dashboard', 'saved_query')),
    dashboard_id INTEGER REFERENCES dashboards(id) ON DELETE CASCADE,
    tab_id TEXT,
    saved_query_id INTEGER REFERENCES saved_queries(id) ON DELETE CASCADE,
    schedule_json TEXT NOT NULL,
    timezone TEXT NOT NULL,
    time_range TEXT,
    range_anchor TEXT NOT NULL DEFAULT 'send_time',
    variables_json TEXT NOT NULL DEFAULT '{}',
    max_rows INTEGER NOT NULL DEFAULT 25,
    skip_if_empty BOOLEAN NOT NULL DEFAULT 0,
    attach_csv BOOLEAN NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'active',
    pause_reason TEXT NOT NULL DEFAULT '',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_manual_run_at DATETIME,
    last_preview_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK ((source_kind = 'dashboard' AND dashboard_id IS NOT NULL AND saved_query_id IS NULL)
        OR (source_kind = 'saved_query' AND saved_query_id IS NOT NULL AND dashboard_id IS NULL))
);
CREATE INDEX idx_reports_owner ON reports(owner_id);
CREATE INDEX idx_reports_dashboard ON reports(dashboard_id);
CREATE INDEX idx_reports_saved_query ON reports(saved_query_id);

CREATE TABLE report_recipients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('user', 'team', 'email')),
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    team_id INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    email TEXT NOT NULL DEFAULT '',
    added_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK ((kind = 'user' AND user_id IS NOT NULL AND team_id IS NULL AND email = '')
        OR (kind = 'team' AND team_id IS NOT NULL AND user_id IS NULL AND email = '')
        OR (kind = 'email' AND email <> '' AND user_id IS NULL AND team_id IS NULL))
);
CREATE UNIQUE INDEX idx_report_recipients_target
    ON report_recipients(report_id, kind, COALESCE(user_id, 0), COALESCE(team_id, 0), email);
CREATE INDEX idx_report_recipients_user ON report_recipients(user_id);
CREATE INDEX idx_report_recipients_team ON report_recipients(team_id);

CREATE TABLE report_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    trigger TEXT NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    scheduled_for DATETIME NOT NULL,
    actor_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    window_start DATETIME,
    window_end DATETIME,
    panels_json TEXT NOT NULL DEFAULT '[]',
    deliveries_json TEXT NOT NULL DEFAULT '[]',
    started_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deadline_at DATETIME,
    finished_at DATETIME
);
CREATE UNIQUE INDEX idx_report_runs_tick
    ON report_runs(report_id, scheduled_for) WHERE trigger = 'schedule';
CREATE UNIQUE INDEX idx_report_runs_running
    ON report_runs(report_id) WHERE status = 'running';
CREATE INDEX idx_report_runs_report ON report_runs(report_id, id);
