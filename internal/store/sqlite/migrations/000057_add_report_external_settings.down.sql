DELETE FROM system_settings WHERE category = 'reports';

CREATE TABLE system_settings_old (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    value_type TEXT NOT NULL CHECK (value_type IN ('string', 'number', 'boolean', 'duration')),
    category TEXT NOT NULL CHECK (category IN ('alerts', 'ai', 'auth', 'server')),
    description TEXT,
    is_sensitive INTEGER NOT NULL DEFAULT 0 CHECK (is_sensitive IN (0, 1)),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO system_settings_old (key, value, value_type, category, description, is_sensitive, created_at, updated_at)
SELECT key, value, value_type, category, description, is_sensitive, created_at, updated_at FROM system_settings;
DROP TABLE system_settings;
ALTER TABLE system_settings_old RENAME TO system_settings;
CREATE INDEX IF NOT EXISTS idx_system_settings_category ON system_settings(category);
