-- Drop the case-insensitive uniqueness guard. The forward lowercasing of
-- existing email values is intentionally not reversed.
DROP INDEX IF EXISTS idx_users_email_lower;
