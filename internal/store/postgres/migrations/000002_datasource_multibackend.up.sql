-- Multi-datasource support: generalize sources into datasource records and
-- split the legacy query_type discriminator into query_language + editor_mode.
-- Mirrors SQLite migrations 000025–000027.

-- sources: provider-specific connection details move into connection_config
-- JSONB, discriminated by source_type, with identity_key as the uniqueness
-- anchor (replacing UNIQUE(database, table_name)).
ALTER TABLE sources
    ADD COLUMN source_type TEXT NOT NULL DEFAULT 'clickhouse'
        CHECK (source_type IN ('clickhouse', 'victorialogs')),
    ADD COLUMN connection_config JSONB,
    ADD COLUMN identity_key TEXT;

UPDATE sources
SET connection_config = jsonb_build_object(
        'host', host,
        'username', username,
        'password', password,
        'database', database,
        'table_name', table_name,
        'tls_enable', tls_enable
    ),
    identity_key = 'clickhouse:' || LOWER(TRIM(host)) || '/' || LOWER(TRIM(database)) || '/' || LOWER(TRIM(table_name));

ALTER TABLE sources
    ALTER COLUMN connection_config SET NOT NULL,
    ALTER COLUMN identity_key SET NOT NULL;

DROP INDEX IF EXISTS idx_sources_database_table;
ALTER TABLE sources DROP CONSTRAINT IF EXISTS sources_database_table_name_key;

ALTER TABLE sources
    DROP COLUMN host,
    DROP COLUMN username,
    DROP COLUMN password,
    DROP COLUMN database,
    DROP COLUMN table_name,
    DROP COLUMN tls_enable;

CREATE UNIQUE INDEX idx_sources_identity_key ON sources(identity_key);
CREATE INDEX idx_sources_source_type ON sources(source_type);

-- saved_queries: query_type -> query_language + editor_mode
ALTER TABLE saved_queries
    ADD COLUMN query_language TEXT NOT NULL DEFAULT 'clickhouse-sql',
    ADD COLUMN editor_mode TEXT NOT NULL DEFAULT 'native';

UPDATE saved_queries
SET query_language = CASE WHEN query_type = 'logchefql' THEN 'logchefql' ELSE 'clickhouse-sql' END,
    editor_mode = CASE WHEN query_type = 'logchefql' THEN 'builder' ELSE 'native' END;

ALTER TABLE saved_queries DROP COLUMN query_type;
CREATE INDEX idx_saved_queries_query_language ON saved_queries(query_language);

-- alerts: query_type -> query_language + editor_mode
ALTER TABLE alerts
    ADD COLUMN query_language TEXT NOT NULL DEFAULT 'clickhouse-sql',
    ADD COLUMN editor_mode TEXT NOT NULL DEFAULT 'native'
        CHECK (editor_mode IN ('native', 'condition'));

UPDATE alerts
SET editor_mode = CASE WHEN query_type = 'condition' THEN 'condition' ELSE 'native' END;

ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_query_type_check;
ALTER TABLE alerts DROP COLUMN query_type;
CREATE INDEX idx_alerts_query_language ON alerts(query_language);
