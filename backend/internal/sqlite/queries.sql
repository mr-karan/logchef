-- sources
-- name: SetPragmas
-- Set SQLite pragmas for optimal performance and reliability
PRAGMA busy_timeout       = 5000;
PRAGMA journal_mode       = WAL;
PRAGMA journal_size_limit = 5000000;
PRAGMA synchronous       = NORMAL;
PRAGMA foreign_keys      = ON;
PRAGMA temp_store        = MEMORY;
PRAGMA cache_size        = -16000;

-- name: CreateSourcesTable
-- Create the sources table if it doesn't exist
CREATE TABLE IF NOT EXISTS sources (
    id TEXT PRIMARY KEY,
    schema_type TEXT NOT NULL CHECK (schema_type IN ('managed', 'unmanaged')),
    host TEXT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    database TEXT NOT NULL,
    table_name TEXT NOT NULL,
    description TEXT,
    ttl_days INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(database, table_name)
);

-- name: CreateSource
-- Create a new source entry
-- $1: id
-- $2: schema_type
-- $3: host
-- $4: username
-- $5: password
-- $6: database
-- $7: table_name
-- $8: description
-- $9: ttl_days
INSERT INTO sources (
    id, schema_type, host, username, password, database, table_name, description, ttl_days, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
RETURNING *;

-- name: GetSource
-- Get a single source by ID
-- $1: id
SELECT * FROM sources WHERE id = ?;

-- name: GetSourceByName
-- Get a single source by table name and database
-- $1: database
-- $2: table_name
SELECT * FROM sources WHERE database = ? AND table_name = ?;

-- name: ListSources
-- Get all sources ordered by creation date
SELECT * FROM sources ORDER BY created_at DESC;

-- name: UpdateSource
-- Update an existing source
-- $1: schema_type
-- $2: host
-- $3: username
-- $4: password
-- $5: database
-- $6: table_name
-- $7: description
-- $8: ttl_days
-- $9: id
UPDATE sources
SET schema_type = ?,
    host = ?,
    username = ?,
    password = ?,
    database = ?,
    table_name = ?,
    description = ?,
    ttl_days = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteSource
-- Delete a source by ID
-- $1: id
DELETE FROM sources WHERE id = ?;