DROP INDEX IF EXISTS idx_saved_queries_query_language;
DROP INDEX IF EXISTS idx_alerts_query_language;

ALTER TABLE saved_queries DROP COLUMN query_language;
ALTER TABLE saved_queries DROP COLUMN editor_mode;
ALTER TABLE alerts DROP COLUMN query_language;
ALTER TABLE alerts DROP COLUMN editor_mode;
