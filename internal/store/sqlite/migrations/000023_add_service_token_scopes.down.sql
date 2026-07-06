DROP INDEX IF EXISTS idx_users_account_type;

ALTER TABLE api_tokens DROP COLUMN scopes;

ALTER TABLE users DROP COLUMN account_type;
