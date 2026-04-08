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
    secret_ref
)
SELECT
    id,
    name,
    _meta_is_auto_created,
    _meta_ts_field,
    _meta_severity_field,
    COALESCE(json_extract(connection_config, '$.host'), ''),
    COALESCE(json_extract(connection_config, '$.username'), ''),
    COALESCE(json_extract(connection_config, '$.password'), ''),
    COALESCE(json_extract(connection_config, '$.database'), ''),
    COALESCE(json_extract(connection_config, '$.table_name'), ''),
    description,
    ttl_days,
    created_at,
    updated_at,
    managed,
    secret_ref
FROM sources
WHERE source_type = 'clickhouse';

DROP INDEX IF EXISTS idx_sources_identity_key;
DROP INDEX IF EXISTS idx_sources_created_at;
DROP INDEX IF EXISTS idx_sources_name;
DROP INDEX IF EXISTS idx_sources_source_type;
DROP TABLE sources;

ALTER TABLE sources_old RENAME TO sources;

CREATE INDEX idx_sources_created_at ON sources(created_at);
CREATE INDEX idx_sources_database_table ON sources(database, table_name);
CREATE INDEX idx_sources_name ON sources(name);

PRAGMA foreign_keys=on;
