-- Drop the case-insensitive uniqueness guard. The forward lowercasing of
-- existing email values is intentionally not reversed (the original casing is
-- not recoverable, and lowercase emails remain valid).
DROP INDEX IF EXISTS idx_users_email_nocase;
