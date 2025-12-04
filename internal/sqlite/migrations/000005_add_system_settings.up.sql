-- Create system_settings table for storing runtime configuration
--
-- Migration Strategy:
-- 1. This migration creates the table and inserts default values as fallbacks
-- 2. On first boot, app.seedSystemSettings() checks if table is empty
-- 3. If empty, values from config.toml are seeded (overriding these migration defaults)
-- 4. If config.toml doesn't specify values, these migration defaults are used
-- 5. After first boot, settings are managed via Admin Settings UI
-- 6. Future deployments can omit [alerts], [ai], and auth session fields from config.toml
--
-- This allows users to gradually migrate from config.toml to database-backed configuration.
CREATE TABLE IF NOT EXISTS system_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    value_type TEXT NOT NULL CHECK (value_type IN ('string', 'number', 'boolean', 'duration')),
    category TEXT NOT NULL CHECK (category IN ('alerts', 'ai', 'auth', 'server')),
    description TEXT,
    is_sensitive INTEGER NOT NULL DEFAULT 0 CHECK (is_sensitive IN (0, 1)),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Create index on category for efficient filtering
CREATE INDEX IF NOT EXISTS idx_system_settings_category ON system_settings(category);

-- No default values are inserted here. On first boot, app.seedSystemSettings() will:
-- 1. Check if the table is empty
-- 2. If empty, seed values from config.toml (if provided)
-- 3. If config.toml doesn't specify values, use built-in defaults
-- 4. After first boot, settings are managed via Admin Settings UI
