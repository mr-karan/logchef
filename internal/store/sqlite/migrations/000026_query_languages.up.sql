-- Split the legacy query_type discriminator into an explicit executable
-- language (query_language) and authoring surface (editor_mode), so saved
-- queries and alerts can carry datasource-native languages (clickhouse-sql,
-- logsql) uniformly.
ALTER TABLE saved_queries ADD COLUMN query_language TEXT NOT NULL DEFAULT 'clickhouse-sql';
ALTER TABLE saved_queries ADD COLUMN editor_mode TEXT NOT NULL DEFAULT 'native';

UPDATE saved_queries
SET query_language = CASE
        WHEN query_type = 'logchefql' THEN 'logchefql'
        ELSE 'clickhouse-sql'
    END,
    editor_mode = CASE
        WHEN query_type = 'logchefql' THEN 'builder'
        ELSE 'native'
    END;

CREATE INDEX IF NOT EXISTS idx_saved_queries_query_language ON saved_queries(query_language);

ALTER TABLE alerts ADD COLUMN query_language TEXT NOT NULL DEFAULT 'clickhouse-sql';
ALTER TABLE alerts ADD COLUMN editor_mode TEXT NOT NULL DEFAULT 'native';

UPDATE alerts
SET query_language = 'clickhouse-sql',
    editor_mode = CASE
        WHEN query_type = 'condition' THEN 'condition'
        ELSE 'native'
    END;

CREATE INDEX IF NOT EXISTS idx_alerts_query_language ON alerts(query_language);
