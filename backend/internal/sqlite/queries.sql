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
    table_name TEXT NOT NULL UNIQUE,
    schema_type TEXT NOT NULL,
    dsn TEXT NOT NULL,
    description TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- name: CreateSource
-- Create a new source entry
-- $1: id
-- $2: table_name
-- $3: schema_type
-- $4: dsn
-- $5: description
INSERT INTO sources (
    id, table_name, schema_type, dsn, description, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))
RETURNING *;

-- name: GetSource
-- Get a single source by ID
-- $1: id
SELECT * FROM sources WHERE id = ?;

-- name: GetSourceByName
-- Get a single source by table name
-- $1: table_name
SELECT * FROM sources WHERE table_name = ?;

-- name: ListSources
-- Get all sources ordered by creation date
SELECT * FROM sources ORDER BY created_at DESC;

-- name: UpdateSource
-- Update an existing source
-- $1: table_name
-- $2: schema_type
-- $3: dsn
-- $4: description
-- $5: id
UPDATE sources
SET table_name = ?,
    schema_type = ?,
    dsn = ?,
    description = ?,
    updated_at = datetime('now')
WHERE id = ?;

-- name: DeleteSource
-- Delete a source by ID
-- $1: id
DELETE FROM sources WHERE id = ?;