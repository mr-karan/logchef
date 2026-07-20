-- Local (email+password) authentication: bcrypt hash storage for users.
-- NULL means the user has no local password (OIDC-only).
ALTER TABLE users ADD COLUMN password_hash TEXT;
