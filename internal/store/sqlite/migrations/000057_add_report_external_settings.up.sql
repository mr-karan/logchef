-- RFC 0008 unit R9: a reports category for system settings, and the two
-- settings that gate external report recipients, off and with no domain.
--
-- SQLite cannot change a CHECK in place, so system_settings is rebuilt with
-- the same columns. No table references it, and migrations run on a
-- connection with foreign keys off (setupAndRunMigrations in sqlite.go), so
-- the rebuild touches no other table.
CREATE TABLE system_settings_new (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    value_type TEXT NOT NULL CHECK (value_type IN ('string', 'number', 'boolean', 'duration')),
    category TEXT NOT NULL CHECK (category IN ('alerts', 'ai', 'auth', 'server', 'reports')),
    description TEXT,
    is_sensitive INTEGER NOT NULL DEFAULT 0 CHECK (is_sensitive IN (0, 1)),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO system_settings_new (key, value, value_type, category, description, is_sensitive, created_at, updated_at)
SELECT key, value, value_type, category, description, is_sensitive, created_at, updated_at FROM system_settings;
DROP TABLE system_settings;
ALTER TABLE system_settings_new RENAME TO system_settings;
CREATE INDEX IF NOT EXISTS idx_system_settings_category ON system_settings(category);

-- An empty table is seeded at first boot (seedSystemSettings in
-- internal/app), which skips a table that has rows, so the defaults go in
-- only beside rows that exist.
INSERT OR IGNORE INTO system_settings (key, value, value_type, category, description, is_sensitive)
SELECT 'reports.external_recipients', 'false', 'boolean', 'reports',
       'Send reports to email addresses that belong to no Logchef user', 0
WHERE EXISTS (SELECT 1 FROM system_settings);
INSERT OR IGNORE INTO system_settings (key, value, value_type, category, description, is_sensitive)
SELECT 'reports.allowed_email_domains', '', 'string', 'reports',
       'Domains, separated by commas, that external report recipients may use; * allows every domain', 0
WHERE EXISTS (SELECT 1 FROM system_settings);
