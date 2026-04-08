PRAGMA foreign_keys=off;

CREATE TABLE sources_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    _meta_is_auto_created INTEGER NOT NULL CHECK (_meta_is_auto_created IN (0, 1)),
    source_type TEXT NOT NULL DEFAULT 'clickhouse' CHECK (source_type IN ('clickhouse', 'victorialogs')),
    _meta_ts_field TEXT NOT NULL DEFAULT '_timestamp',
    _meta_severity_field TEXT DEFAULT 'severity_text',
    connection_config TEXT NOT NULL,
    identity_key TEXT NOT NULL,
    description TEXT,
    ttl_days INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    managed INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1)),
    secret_ref TEXT
);

INSERT INTO sources_new (
    id,
    name,
    _meta_is_auto_created,
    source_type,
    _meta_ts_field,
    _meta_severity_field,
    connection_config,
    identity_key,
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
    'clickhouse',
    _meta_ts_field,
    _meta_severity_field,
    json_object(
        'host', host,
        'username', username,
        'password', password,
        'database', database,
        'table_name', table_name
    ),
    'clickhouse:' || LOWER(TRIM(host)) || '/' || LOWER(TRIM(database)) || '/' || LOWER(TRIM(table_name)),
    description,
    ttl_days,
    created_at,
    updated_at,
    managed,
    secret_ref
FROM sources;

DROP INDEX IF EXISTS idx_sources_database_table;
DROP INDEX IF EXISTS idx_sources_created_at;
DROP INDEX IF EXISTS idx_sources_name;
DROP TABLE sources;

ALTER TABLE sources_new RENAME TO sources;

CREATE UNIQUE INDEX idx_sources_identity_key ON sources(identity_key);
CREATE INDEX idx_sources_created_at ON sources(created_at);
CREATE INDEX idx_sources_name ON sources(name);
CREATE INDEX idx_sources_source_type ON sources(source_type);

PRAGMA foreign_keys=on;
