-- The notification delivery queue (RFC 0006 unit D2). Timestamps are UTC text
-- in the '%Y-%m-%dT%H:%M:%fZ' form, as in jobs, so they compare as text with
-- strftime('now').
CREATE TABLE notification_deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    destination_id INTEGER NOT NULL REFERENCES notification_destinations(id) ON DELETE CASCADE,
    producer TEXT NOT NULL,
    producer_ref TEXT NOT NULL,
    recipient TEXT NOT NULL DEFAULT '',
    sequence_key TEXT NOT NULL,
    event TEXT NOT NULL CHECK (event IN ('firing', 'resolved', 'no_data', 'test')),
    dedup_key TEXT NOT NULL UNIQUE,
    message_json TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'attempting', 'sent', 'failed', 'unknown')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL,
    reserved_by TEXT,
    reserved_token INTEGER NOT NULL DEFAULT 0,
    reserved_until DATETIME,
    last_error TEXT,
    created_at DATETIME NOT NULL,
    sent_at DATETIME
);

CREATE INDEX idx_notification_deliveries_status_next ON notification_deliveries(status, next_attempt_at);
CREATE INDEX idx_notification_deliveries_sequence ON notification_deliveries(destination_id, recipient, sequence_key, id);
CREATE INDEX idx_notification_deliveries_producer ON notification_deliveries(producer, producer_ref);

-- The instance's notified flag now says whether an episode owes its
-- notification. An open episode that a silence did not hold back has had its
-- delivery attempt, so it owes nothing; a retry pending at upgrade is dropped.
UPDATE alert_instances SET notified = 1
WHERE history_id IN (
    SELECT id FROM alert_history
    WHERE status IN ('triggered', 'no_data')
      AND (payload_json IS NULL OR NOT json_valid(payload_json)
           OR COALESCE(json_extract(payload_json, '$.delivery_silenced'), 0) != 1)
);

UPDATE alert_history
SET payload_json = json_remove(payload_json,
    '$.delivery_failed', '$.delivery_in_progress', '$.delivery_deadline_at',
    '$.delivery_attempted', '$.delivery_attempts', '$.delivery_pending',
    '$.delivery_retries_exhausted', '$.delivery_error', '$.delivery_silenced')
WHERE status IN ('triggered', 'no_data') AND payload_json IS NOT NULL AND json_valid(payload_json);
