-- Alerting v2 (RFC 0006, unit A1): one state row per (rule, group), the
-- columns that later units read, and checks widened for the no_data state.

ALTER TABLE alerts
    ADD COLUMN IF NOT EXISTS group_by_json TEXT,
    ADD COLUMN IF NOT EXISTS no_data_policy TEXT NOT NULL DEFAULT 'zero'
        CONSTRAINT alerts_no_data_policy_check CHECK (no_data_policy IN ('zero', 'no_data', 'keep')),
    ADD COLUMN IF NOT EXISTS eval_seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_last_state_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_last_state_check CHECK (last_state IN ('firing', 'resolved', 'no_data'));

ALTER TABLE alert_history
    ADD COLUMN IF NOT EXISTS group_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS group_json TEXT;
ALTER TABLE alert_history DROP CONSTRAINT IF EXISTS alert_history_status_check;
ALTER TABLE alert_history ADD CONSTRAINT alert_history_status_check CHECK (status IN ('triggered', 'resolved', 'error', 'no_data'));

CREATE TABLE IF NOT EXISTS alert_instances (
    alert_id     BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    group_key    TEXT NOT NULL,
    group_json   TEXT NOT NULL DEFAULT '{}',
    state        TEXT NOT NULL CHECK (state IN ('ok', 'firing', 'no_data')),
    value        DOUBLE PRECISION,
    state_since  TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ,
    history_id   BIGINT,
    notified     BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (alert_id, group_key)
);

-- Backfill: the open episode is the row GetLatestUnresolvedAlertHistory picks
-- (newest triggered_at, then highest id). Older open rows of the same alert
-- are closed.
UPDATE alert_history AS h
SET status = 'resolved',
    resolved_at = now(),
    message = 'superseded at upgrade'
WHERE h.status = 'triggered'
  AND h.id <> (
    SELECT latest.id FROM alert_history AS latest
    WHERE latest.alert_id = h.alert_id AND latest.status = 'triggered'
    ORDER BY latest.triggered_at DESC, latest.id DESC
    LIMIT 1
  );

-- notified = true: every open row has had a delivery attempt, so the first
-- tick after the upgrade sends no second firing message. A retry that was
-- pending or interrupted at upgrade is dropped (see the upgrade note).
INSERT INTO alert_instances (alert_id, group_key, group_json, state, value, state_since, last_seen_at, history_id, notified)
SELECT h.alert_id, '', '{}', 'firing', h.value, h.triggered_at, a.last_evaluated_at, h.id, TRUE
FROM alert_history AS h
JOIN alerts AS a ON a.id = h.alert_id
WHERE h.status = 'triggered'
ON CONFLICT (alert_id, group_key) DO NOTHING;
