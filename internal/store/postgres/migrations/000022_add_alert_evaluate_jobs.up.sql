-- Alert evaluation moves onto the job runner (RFC 0006 unit J2). Each alert
-- gets one alert.evaluate job, due one frequency after its last evaluation, or
-- now when it was never evaluated. The interval has the 10-second floor that
-- every later upsert applies. Idempotent through the unique (kind, key).
INSERT INTO jobs (kind, key, schedule_kind, schedule_expr, timezone, misfire, grace_seconds, enabled, next_run_at)
SELECT
    'alert.evaluate',
    id::text,
    'interval',
    GREATEST(frequency_seconds, 10)::text,
    'UTC',
    'run_once',
    0,
    is_active,
    COALESCE(last_evaluated_at + frequency_seconds * interval '1 second', now())
FROM alerts
ON CONFLICT (kind, key) DO NOTHING;

-- Each rule's job carries its own frequency, so the global interval is gone.
DELETE FROM system_settings WHERE key = 'alerts.evaluation_interval';
