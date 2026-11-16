-- Alerting v2 (RFC 0006, unit A3): whether a rule sends its resolved
-- notification. Existing rules keep sending it.
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS notify_on_resolve BOOLEAN NOT NULL DEFAULT TRUE;
