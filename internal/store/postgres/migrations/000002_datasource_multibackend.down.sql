-- WARNING: rolling back keeps only ClickHouse sources. VictoriaLogs sources
-- cannot be represented in the legacy column layout and are dropped, along
-- with their saved queries and alerts via FK cascade.

-- alerts: restore query_type
ALTER TABLE alerts ADD COLUMN query_type TEXT NOT NULL DEFAULT 'sql'
    CHECK (query_type IN ('sql', 'condition'));
UPDATE alerts
SET query_type = CASE WHEN editor_mode = 'condition' THEN 'condition' ELSE 'sql' END;
DROP INDEX IF EXISTS idx_alerts_query_language;
ALTER TABLE alerts DROP COLUMN query_language, DROP COLUMN editor_mode;

-- saved_queries: restore query_type
ALTER TABLE saved_queries ADD COLUMN query_type TEXT NOT NULL DEFAULT 'sql';
UPDATE saved_queries
SET query_type = CASE WHEN query_language = 'logchefql' THEN 'logchefql' ELSE 'sql' END;
DROP INDEX IF EXISTS idx_saved_queries_query_language;
ALTER TABLE saved_queries DROP COLUMN query_language, DROP COLUMN editor_mode;

-- sources: restore flat ClickHouse columns; drop non-ClickHouse sources.
DELETE FROM sources WHERE source_type <> 'clickhouse';

ALTER TABLE sources
    ADD COLUMN host TEXT,
    ADD COLUMN username TEXT,
    ADD COLUMN password TEXT,
    ADD COLUMN database TEXT,
    ADD COLUMN table_name TEXT,
    ADD COLUMN tls_enable BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE sources
SET host = connection_config->>'host',
    username = connection_config->>'username',
    password = connection_config->>'password',
    database = connection_config->>'database',
    table_name = connection_config->>'table_name',
    tls_enable = COALESCE((connection_config->>'tls_enable')::BOOLEAN, FALSE);

ALTER TABLE sources
    ALTER COLUMN host SET NOT NULL,
    ALTER COLUMN username SET NOT NULL,
    ALTER COLUMN password SET NOT NULL,
    ALTER COLUMN database SET NOT NULL,
    ALTER COLUMN table_name SET NOT NULL;

DROP INDEX IF EXISTS idx_sources_identity_key;
DROP INDEX IF EXISTS idx_sources_source_type;

ALTER TABLE sources
    DROP COLUMN source_type,
    DROP COLUMN connection_config,
    DROP COLUMN identity_key;

ALTER TABLE sources ADD CONSTRAINT sources_database_table_name_key UNIQUE (database, table_name);
CREATE INDEX idx_sources_database_table ON sources(database, table_name);
