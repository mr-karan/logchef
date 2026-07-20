-- Recreate the legacy query_type column from query_language/editor_mode.
ALTER TABLE saved_queries ADD COLUMN query_type TEXT NOT NULL DEFAULT 'sql';
UPDATE saved_queries
SET query_type = CASE
        WHEN query_language = 'logchefql' THEN 'logchefql'
        ELSE 'sql'
    END;

ALTER TABLE alerts ADD COLUMN query_type TEXT NOT NULL DEFAULT 'sql';
UPDATE alerts
SET query_type = CASE
        WHEN editor_mode = 'condition' THEN 'condition'
        ELSE 'sql'
    END;

DROP INDEX IF EXISTS idx_saved_queries_query_language;
DROP INDEX IF EXISTS idx_alerts_query_language;

ALTER TABLE saved_queries DROP COLUMN query_language;
ALTER TABLE saved_queries DROP COLUMN editor_mode;
ALTER TABLE alerts DROP COLUMN query_language;
ALTER TABLE alerts DROP COLUMN editor_mode;
