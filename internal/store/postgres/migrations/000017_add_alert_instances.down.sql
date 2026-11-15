-- Thin down path: drops instances and the new columns. no_data states map to
-- resolved because the old checks reject them.
DROP TABLE IF EXISTS alert_instances;

UPDATE alert_history SET status = 'resolved' WHERE status = 'no_data';
ALTER TABLE alert_history DROP CONSTRAINT IF EXISTS alert_history_status_check;
ALTER TABLE alert_history ADD CONSTRAINT alert_history_status_check CHECK (status IN ('triggered', 'resolved', 'error'));
ALTER TABLE alert_history DROP COLUMN IF EXISTS group_json, DROP COLUMN IF EXISTS group_key;

UPDATE alerts SET last_state = 'resolved' WHERE last_state = 'no_data';
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_last_state_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_last_state_check CHECK (last_state IN ('firing', 'resolved'));
ALTER TABLE alerts DROP COLUMN IF EXISTS eval_seq, DROP COLUMN IF EXISTS no_data_policy, DROP COLUMN IF EXISTS group_by_json;
