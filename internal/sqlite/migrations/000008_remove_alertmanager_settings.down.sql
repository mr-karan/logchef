-- Rollback: restore alertmanager setting and remove SMTP settings
-- Note: The alertmanager integration code was removed, so this setting has no effect.

-- Remove SMTP settings
DELETE FROM system_settings WHERE key IN (
    'alerts.smtp_host',
    'alerts.smtp_port',
    'alerts.smtp_username',
    'alerts.smtp_password',
    'alerts.smtp_from',
    'alerts.smtp_reply_to',
    'alerts.smtp_security'
);

-- Restore alertmanager setting
INSERT OR IGNORE INTO system_settings (key, value, value_type, category, description, is_sensitive)
VALUES ('alerts.alertmanager_url', '', 'string', 'alerts', 'Alertmanager endpoint URL for sending notifications', 0);
