-- Alert silences (RFC 0006 unit S1). A silence mutes notifications for a
-- source, optionally one rule and a set of label matchers, once or weekly.
-- Evaluation and history are not affected.
CREATE TABLE alert_silences (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    alert_id INTEGER REFERENCES alerts(id) ON DELETE CASCADE,
    matchers_json TEXT NOT NULL DEFAULT '{}',
    comment TEXT NOT NULL,
    schedule_kind TEXT NOT NULL CHECK (schedule_kind IN ('once', 'weekly')),
    starts_at DATETIME,
    ends_at DATETIME,
    weekly_json TEXT,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    CHECK (schedule_kind <> 'once' OR (starts_at IS NOT NULL AND ends_at IS NOT NULL)),
    CHECK (schedule_kind <> 'weekly' OR weekly_json IS NOT NULL)
);

CREATE INDEX idx_alert_silences_source ON alert_silences(source_id);
CREATE INDEX idx_alert_silences_alert ON alert_silences(alert_id);
