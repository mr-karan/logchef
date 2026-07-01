-- Widen collection_members.role to allow an 'editor' role. Editors can edit the
-- saved queries curated in a shared collection (and curate items) but cannot
-- manage members or delete the collection. SQLite can't ALTER a CHECK
-- constraint, so we rebuild the table — same pattern as migration 000020.
-- Disable FK enforcement during the swap (collection_members has outbound FKs
-- with ON DELETE CASCADE) to avoid surprises while the old table is dropped.
PRAGMA foreign_keys = OFF;

CREATE TABLE collection_members_new (
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'member')),
    added_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (collection_id, user_id)
);

INSERT INTO collection_members_new (collection_id, user_id, role, added_by, created_at)
SELECT collection_id, user_id, role, added_by, created_at FROM collection_members;

DROP TABLE collection_members;
ALTER TABLE collection_members_new RENAME TO collection_members;

CREATE INDEX idx_collection_members_user ON collection_members(user_id);

PRAGMA foreign_keys = ON;
PRAGMA foreign_key_check;
