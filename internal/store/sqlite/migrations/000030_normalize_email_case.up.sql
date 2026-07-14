-- Issue #95: make user emails case-insensitive (storage + uniqueness).
--
-- SAFETY: this migration must NOT silently merge two pre-existing accounts that
-- differ only in email case (doing so would delete a real account's data). So
-- before touching any row we abort loudly when such a collision exists, letting
-- an operator resolve the duplicates by hand first.
--
-- The abort is implemented with a guard table whose CHECK constraint only
-- permits a zero collision count: 0 collision groups inserts cleanly and the
-- table is dropped; any collision makes the INSERT fail with a CHECK constraint
-- error that names this table, so the failure is loud and self-describing.
DROP TABLE IF EXISTS _migration_000030_email_case_collisions_must_be_resolved_manually;
CREATE TABLE _migration_000030_email_case_collisions_must_be_resolved_manually (
    collision_group_count INTEGER NOT NULL CHECK (collision_group_count = 0)
);
INSERT INTO _migration_000030_email_case_collisions_must_be_resolved_manually (collision_group_count)
SELECT COUNT(*) FROM (
    SELECT lower(email)
    FROM users
    GROUP BY lower(email)
    HAVING COUNT(*) > 1
);
DROP TABLE _migration_000030_email_case_collisions_must_be_resolved_manually;

-- No collisions: forward-normalize existing rows to lowercase so stored data
-- matches the application-layer normalization (store.normalizeEmail).
UPDATE users SET email = lower(email) WHERE email <> lower(email);

-- Enforce case-insensitive uniqueness going forward. This is defense in depth
-- on top of the store-layer normalization; the existing idx_users_email (binary
-- collation) is kept because equality lookups pass an already-lowercased email.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nocase ON users (email COLLATE NOCASE);
