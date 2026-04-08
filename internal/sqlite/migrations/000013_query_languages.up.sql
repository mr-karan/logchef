ALTER TABLE team_queries ADD COLUMN query_language TEXT NOT NULL DEFAULT 'clickhouse-sql';
ALTER TABLE team_queries ADD COLUMN editor_mode TEXT NOT NULL DEFAULT 'native';

UPDATE team_queries
SET query_language = CASE
        WHEN query_type = 'logchefql' THEN 'logchefql'
        ELSE 'clickhouse-sql'
    END,
    editor_mode = CASE
        WHEN query_type = 'logchefql' THEN 'builder'
        ELSE 'native'
    END;

CREATE INDEX IF NOT EXISTS idx_team_queries_query_language ON team_queries(query_language);
CREATE INDEX IF NOT EXISTS idx_team_queries_editor_mode ON team_queries(editor_mode);

ALTER TABLE alerts ADD COLUMN query_language TEXT NOT NULL DEFAULT 'clickhouse-sql';
ALTER TABLE alerts ADD COLUMN editor_mode TEXT NOT NULL DEFAULT 'native';

UPDATE alerts
SET query_language = 'clickhouse-sql',
    editor_mode = CASE
        WHEN query_type = 'condition' THEN 'condition'
        ELSE 'native'
    END;

CREATE INDEX IF NOT EXISTS idx_alerts_query_language ON alerts(query_language);
CREATE INDEX IF NOT EXISTS idx_alerts_editor_mode ON alerts(editor_mode);
