-- Alert evaluation moves onto the job runner (RFC 0006 unit J2). Each alert
-- gets one alert.evaluate job, due one frequency after its last evaluation, or
-- now when it was never evaluated. The interval has the 10-second floor that
-- every later upsert applies. Idempotent through the unique (kind, key).
-- Timestamps use the jobs table's '%Y-%m-%dT%H:%M:%fZ' form.
INSERT INTO jobs (
    kind, key, schedule_kind, schedule_expr, timezone, misfire, grace_seconds, enabled,
    next_run_at, created_at, updated_at
)
SELECT
    'alert.evaluate',
    CAST(id AS TEXT),
    'interval',
    CAST(MAX(frequency_seconds, 10) AS TEXT),
    'UTC',
    'run_once',
    0,
    is_active,
    COALESCE(
        strftime('%Y-%m-%dT%H:%M:%fZ', last_evaluated_at, '+' || frequency_seconds || ' seconds'),
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    ),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM alerts
WHERE true
ON CONFLICT (kind, key) DO NOTHING;

-- Each rule's job carries its own frequency, so the global interval is gone.
DELETE FROM system_settings WHERE key = 'alerts.evaluation_interval';
