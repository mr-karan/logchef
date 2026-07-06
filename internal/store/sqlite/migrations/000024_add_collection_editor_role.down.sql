-- Revert the 'editor' collection role. Any existing 'editor' rows are demoted
-- to 'member' (no data loss beyond the elevated capability). Table rebuild
-- mirrors the up migration.
PRAGMA foreign_keys = OFF;

CREATE TABLE collection_members_new (
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    added_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (collection_id, user_id)
);

INSERT INTO collection_members_new (collection_id, user_id, role, added_by, created_at)
SELECT collection_id, user_id,
       CASE WHEN role = 'editor' THEN 'member' ELSE role END,
       added_by, created_at
FROM collection_members;

DROP TABLE collection_members;
ALTER TABLE collection_members_new RENAME TO collection_members;

CREATE INDEX idx_collection_members_user ON collection_members(user_id);

PRAGMA foreign_keys = ON;
PRAGMA foreign_key_check;
