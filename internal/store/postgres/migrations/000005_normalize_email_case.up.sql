-- Issue #95: make user emails case-insensitive (storage + uniqueness).
--
-- SAFETY: mirrors the SQLite counterpart (migration 000030). This migration
-- must NOT silently merge two pre-existing accounts that differ only in email
-- case, so it aborts loudly with a clear message when such a collision exists,
-- letting an operator resolve the duplicates by hand before upgrading.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM users GROUP BY lower(email) HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'migration 000005: users whose emails differ only in case exist; resolve these duplicate accounts manually before upgrading (issue #95)';
    END IF;
END $$;

-- No collisions: forward-normalize existing rows to lowercase so stored data
-- matches the application-layer normalization (store.normalizeEmail).
UPDATE users SET email = lower(email) WHERE email <> lower(email);

-- Enforce case-insensitive uniqueness going forward (defense in depth on top of
-- the store-layer normalization). idx_users_email is kept for equality lookups,
-- which pass an already-lowercased email.
CREATE UNIQUE INDEX idx_users_email_lower ON users (lower(email));
