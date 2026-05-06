-- Retire the saved-query bookmark flag. Personal collections take its place:
-- every existing user gets a personal collection back-filled with the queries
-- they had bookmarked (best-effort, only for queries with a known creator).

-- 1. Create a personal collection for every user that does not have one yet.
--    The default name is "My Collection" — personal collections are never
--    shared, so the label is purely owner-facing. Users can rename via the UI.
--    Existing personal collections are left untouched.
INSERT INTO collections (name, description, is_personal, created_by, created_at, updated_at)
SELECT 'My Collection', '', 1, u.id, datetime('now'), datetime('now')
FROM users u
WHERE NOT EXISTS (
    SELECT 1 FROM collections c WHERE c.created_by = u.id AND c.is_personal = 1
);

-- 2. Ensure the owner-membership row exists for every personal collection
--    (safe to re-run; fixes any half-broken state from the v1.6.0-dev preview
--    where SQL parsing dropped the membership write).
INSERT OR IGNORE INTO collection_members (collection_id, user_id, role, added_by)
SELECT c.id, c.created_by, 'owner', c.created_by
FROM collections c
WHERE c.is_personal = 1;

-- 3. Migrate bookmarked queries into their creator's personal collection.
--    Bookmarks were per-query, not per-user, so the creator is the only
--    honest signal we have. NOTE: Legacy queries with NULL created_by are
--    silently skipped — there's no user to attribute the bookmark to. If the
--    deployment has a non-trivial number of NULL-creator bookmarked queries,
--    snapshot saved_queries before running this migration.
INSERT OR IGNORE INTO collection_items (collection_id, saved_query_id, sort_order, added_by)
SELECT
    pc.id,
    sq.id,
    0,
    sq.created_by
FROM saved_queries sq
JOIN collections pc ON pc.created_by = sq.created_by AND pc.is_personal = 1
WHERE sq.is_bookmarked = 1 AND sq.created_by IS NOT NULL;

-- 4. Drop the is_bookmarked column from saved_queries. SQLite can't DROP
--    COLUMN cleanly when an index references it, so we rebuild the table.
DROP INDEX IF EXISTS idx_saved_queries_source_bookmark;

CREATE TABLE saved_queries_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    query_type TEXT NOT NULL,
    query_content TEXT NOT NULL,
    created_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
);

INSERT INTO saved_queries_new (id, source_id, name, description, query_type, query_content, created_by, created_at, updated_at)
SELECT id, source_id, name, description, query_type, query_content, created_by, created_at, updated_at
FROM saved_queries;

DROP TABLE saved_queries;
ALTER TABLE saved_queries_new RENAME TO saved_queries;

CREATE INDEX IF NOT EXISTS idx_saved_queries_source ON saved_queries(source_id);
CREATE INDEX IF NOT EXISTS idx_saved_queries_created_by ON saved_queries(created_by);
