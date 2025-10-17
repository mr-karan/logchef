DROP TABLE IF EXISTS alert_history;
DROP TABLE IF EXISTS alert_rule_channels;
DROP TABLE IF EXISTS notification_channels;
DROP TABLE IF EXISTS alert_rooms;
DROP TABLE IF EXISTS room_channels;
DROP TABLE IF EXISTS room_members;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS alerts;

CREATE TABLE alerts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
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
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE alert_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('triggered', 'resolved', 'error')),
    triggered_at DATETIME NOT NULL DEFAULT (datetime('now')),
    resolved_at DATETIME,
    value REAL,
    message TEXT,
    payload_json TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_alerts_team_source ON alerts(team_id, source_id);
CREATE INDEX idx_alerts_active ON alerts(is_active);
CREATE INDEX idx_alerts_last_evaluated ON alerts(last_evaluated_at);
CREATE INDEX idx_alerts_last_state ON alerts(last_state);
CREATE INDEX idx_alert_history_alert_id ON alert_history(alert_id);
CREATE INDEX idx_alert_history_triggered_at ON alert_history(alert_id, triggered_at DESC);
CREATE INDEX idx_alert_history_alert_status ON alert_history(alert_id, status);
