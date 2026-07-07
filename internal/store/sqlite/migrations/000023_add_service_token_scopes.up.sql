ALTER TABLE users ADD COLUMN account_type TEXT NOT NULL DEFAULT 'human' CHECK (account_type IN ('human', 'service'));

ALTER TABLE api_tokens ADD COLUMN scopes TEXT NOT NULL DEFAULT '["*"]';

CREATE INDEX IF NOT EXISTS idx_users_account_type ON users(account_type);
