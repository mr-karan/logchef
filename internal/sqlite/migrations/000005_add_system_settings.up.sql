-- Create system_settings table for storing runtime configuration
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

-- Insert default settings for alerts (migrating from config.toml)
INSERT INTO system_settings (key, value, value_type, category, description) VALUES
    ('alerts.enabled', 'true', 'boolean', 'alerts', 'Enable or disable alert evaluation'),
    ('alerts.evaluation_interval', '1m', 'duration', 'alerts', 'How often to evaluate alert rules'),
    ('alerts.default_lookback', '5m', 'duration', 'alerts', 'Default lookback window for alert queries'),
    ('alerts.history_limit', '50', 'number', 'alerts', 'Maximum number of alert history entries to keep per alert'),
    ('alerts.alertmanager_url', '', 'string', 'alerts', 'Alertmanager endpoint URL for sending notifications'),
    ('alerts.external_url', '', 'string', 'alerts', 'External URL for backend API access'),
    ('alerts.frontend_url', '', 'string', 'alerts', 'Frontend URL for generating alert links in notifications'),
    ('alerts.request_timeout', '5s', 'duration', 'alerts', 'Timeout for Alertmanager HTTP requests'),
    ('alerts.tls_insecure_skip_verify', 'false', 'boolean', 'alerts', 'Skip TLS certificate verification for Alertmanager');

-- Insert default settings for AI
INSERT INTO system_settings (key, value, value_type, category, description, is_sensitive) VALUES
    ('ai.enabled', 'false', 'boolean', 'ai', 'Enable or disable AI-assisted SQL generation', 0),
    ('ai.api_key', '', 'string', 'ai', 'OpenAI API key or compatible provider key', 1),
    ('ai.base_url', '', 'string', 'ai', 'Base URL for OpenAI-compatible API (empty for default OpenAI)', 0),
    ('ai.model', 'gpt-4o', 'string', 'ai', 'AI model to use for SQL generation', 0),
    ('ai.max_tokens', '1024', 'number', 'ai', 'Maximum tokens to generate in AI responses', 0),
    ('ai.temperature', '0.1', 'number', 'ai', 'Temperature for generation (0.0-1.0, lower is more deterministic)', 0);

-- Insert default settings for auth session management
INSERT INTO system_settings (key, value, value_type, category, description) VALUES
    ('auth.session_duration', '8h', 'duration', 'auth', 'Duration of user sessions before expiration'),
    ('auth.max_concurrent_sessions', '1', 'number', 'auth', 'Maximum number of concurrent sessions per user'),
    ('auth.default_token_expiry', '2160h', 'duration', 'auth', 'Default expiration for API tokens (90 days)');

-- Insert default settings for server
INSERT INTO system_settings (key, value, value_type, category, description) VALUES
    ('server.frontend_url', '', 'string', 'server', 'URL of the frontend application for CORS configuration');
