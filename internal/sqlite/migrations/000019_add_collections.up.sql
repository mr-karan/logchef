-- Cross-team Collections. Each user has a "personal" collection auto-created
-- on first /api/v1/collections fetch (handled in app code). Other collections
-- are invite-only with two roles: owner (full control) and member (read +
-- bookmark). Items are saved-query references; an item visible to a member
-- who lacks source access still surfaces with a runnable: false flag from the
-- application layer.

CREATE TABLE collections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    is_personal INTEGER NOT NULL DEFAULT 0 CHECK (is_personal IN (0, 1)),
    created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Each user gets at most one personal collection. The unique partial index
-- only applies when is_personal = 1, so users can still own multiple shared
-- collections.
CREATE UNIQUE INDEX idx_collections_one_personal_per_user
    ON collections(created_by) WHERE is_personal = 1;
CREATE INDEX idx_collections_created_by ON collections(created_by);

CREATE TABLE collection_members (
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    added_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (collection_id, user_id)
);

CREATE INDEX idx_collection_members_user ON collection_members(user_id);

CREATE TABLE collection_items (
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    saved_query_id INTEGER NOT NULL REFERENCES saved_queries(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    added_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (collection_id, saved_query_id)
);

CREATE INDEX idx_collection_items_query ON collection_items(saved_query_id);
CREATE INDEX idx_collection_items_collection_order
    ON collection_items(collection_id, sort_order, saved_query_id);
