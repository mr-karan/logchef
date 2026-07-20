-- WARNING: rolling back keeps only ClickHouse sources. VictoriaLogs sources
-- cannot be represented in the legacy column layout and are dropped, along
-- with their saved queries and alerts via FK cascade.
PRAGMA foreign_keys=off;

CREATE TABLE sources_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    _meta_is_auto_created INTEGER NOT NULL CHECK (_meta_is_auto_created IN (0, 1)),
    _meta_ts_field TEXT NOT NULL DEFAULT '_timestamp',
    _meta_severity_field TEXT DEFAULT 'severity_text',
    host TEXT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    database TEXT NOT NULL,
    table_name TEXT NOT NULL,
    description TEXT,
    ttl_days INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    managed INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1)),
    secret_ref TEXT,
    tls_enable INTEGER NOT NULL DEFAULT 0 CHECK (tls_enable IN (0, 1)),
    UNIQUE(database, table_name)
);

INSERT INTO sources_old (
    id,
    name,
    _meta_is_auto_created,
    _meta_ts_field,
    _meta_severity_field,
    host,
    username,
    password,
    database,
    table_name,
    description,
    ttl_days,
    created_at,
    updated_at,
    managed,
    secret_ref,
    tls_enable
)
SELECT
    id,
    name,
    _meta_is_auto_created,
    _meta_ts_field,
    _meta_severity_field,
    json_extract(connection_config, '$.host'),
    json_extract(connection_config, '$.username'),
    json_extract(connection_config, '$.password'),
    json_extract(connection_config, '$.database'),
    json_extract(connection_config, '$.table_name'),
    description,
    ttl_days,
    created_at,
    updated_at,
    managed,
    secret_ref,
    CASE WHEN json_extract(connection_config, '$.tls_enable') IN (1, 'true') THEN 1 ELSE 0 END
FROM sources
WHERE source_type = 'clickhouse';

-- Remove children of non-ClickHouse sources before the swap so FK checks pass.
DELETE FROM team_sources WHERE source_id NOT IN (SELECT id FROM sources_old);
DELETE FROM saved_queries WHERE source_id NOT IN (SELECT id FROM sources_old);
DELETE FROM alerts WHERE source_id NOT IN (SELECT id FROM sources_old);

DROP INDEX IF EXISTS idx_sources_identity_key;
DROP INDEX IF EXISTS idx_sources_created_at;
DROP INDEX IF EXISTS idx_sources_source_type;

DROP TABLE sources;
ALTER TABLE sources_old RENAME TO sources;

CREATE INDEX idx_sources_created_at ON sources(created_at);

PRAGMA foreign_keys=on;
PRAGMA foreign_key_check;
