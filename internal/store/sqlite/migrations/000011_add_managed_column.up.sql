-- Add managed flag and secret_ref for declarative provisioning.
-- managed: 0 = UI-managed (default), 1 = config-managed.
-- secret_ref: stores the env var name that provided the password (for export round-trip).
ALTER TABLE sources ADD COLUMN managed INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1));
ALTER TABLE sources ADD COLUMN secret_ref TEXT;
ALTER TABLE teams ADD COLUMN managed INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1));
ALTER TABLE users ADD COLUMN managed INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1));
