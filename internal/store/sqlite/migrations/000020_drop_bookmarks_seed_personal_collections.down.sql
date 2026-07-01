-- Rebuild saved_queries with is_bookmarked. Restore the bookmark flag
-- best-effort: a query is marked bookmarked if it exists in its creator's
-- personal collection. This is approximate (the original pre-1.6 signal was
-- lost) but matches the intent of the up migration.

DROP INDEX IF EXISTS idx_saved_queries_created_by;
DROP INDEX IF EXISTS idx_saved_queries_source;

CREATE TABLE saved_queries_old (
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

INSERT INTO saved_queries_old (id, source_id, name, description, query_type, query_content, is_bookmarked, created_by, created_at, updated_at)
SELECT
    sq.id,
    sq.source_id,
    sq.name,
    sq.description,
    sq.query_type,
    sq.query_content,
    CASE
        WHEN EXISTS (
            SELECT 1
            FROM collection_items ci
            JOIN collections pc ON pc.id = ci.collection_id
            WHERE ci.saved_query_id = sq.id
              AND pc.is_personal = 1
              AND pc.created_by = sq.created_by
        ) THEN 1
        ELSE 0
    END,
    sq.created_by,
    sq.created_at,
    sq.updated_at
FROM saved_queries sq;

DROP TABLE saved_queries;
ALTER TABLE saved_queries_old RENAME TO saved_queries;

CREATE INDEX IF NOT EXISTS idx_saved_queries_source_bookmark
    ON saved_queries(source_id, is_bookmarked, updated_at);
CREATE INDEX IF NOT EXISTS idx_saved_queries_created_by ON saved_queries(created_by);

-- Personal collections created by 000020.up are intentionally not removed —
-- collections are user-visible state, and dropping them on revert would lose
-- user-curated items that survived the bookmark migration.
