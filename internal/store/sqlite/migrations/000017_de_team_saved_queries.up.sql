-- Drop the old team-scoped index before reshaping the table
DROP INDEX IF EXISTS idx_team_queries_bookmarked;

-- Rebuild the table without team_id and with a nullable created_by (FK to users)
CREATE TABLE saved_queries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    query_type TEXT NOT NULL,
    query_content TEXT NOT NULL,
    is_bookmarked BOOLEAN NOT NULL DEFAULT FALSE,
    created_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
);

-- Carry existing rows over. created_by stays NULL for legacy queries — on the
-- application side those are treated as "edit by global admin only".
INSERT INTO saved_queries (id, source_id, name, description, query_type, query_content, is_bookmarked, created_at, updated_at)
SELECT id, source_id, name, description, query_type, query_content, is_bookmarked, created_at, updated_at
FROM team_queries;

DROP TABLE team_queries;

CREATE INDEX IF NOT EXISTS idx_saved_queries_source_bookmark
    ON saved_queries(source_id, is_bookmarked, updated_at);
CREATE INDEX IF NOT EXISTS idx_saved_queries_created_by
    ON saved_queries(created_by);
