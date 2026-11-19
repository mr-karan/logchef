-- Alerting v2 (RFC 0006, unit A3): whether a rule sends its resolved
-- notification. Existing rules keep sending it.
ALTER TABLE alerts ADD COLUMN notify_on_resolve INTEGER NOT NULL DEFAULT 1 CHECK (notify_on_resolve IN (0, 1));
