-- Remove obsolete alertmanager settings and add SMTP settings for direct email notifications.
-- Alertmanager integration was replaced with direct SMTP email and webhook notifications.

-- Remove obsolete alertmanager setting
DELETE FROM system_settings WHERE key = 'alerts.alertmanager_url';

-- Add SMTP settings for email notifications (if they don't exist)
INSERT OR IGNORE INTO system_settings (key, value, value_type, category, description, is_sensitive)
VALUES
    ('alerts.smtp_host', '', 'string', 'alerts', 'SMTP server hostname for sending alert emails', 0),
    ('alerts.smtp_port', '587', 'number', 'alerts', 'SMTP server port (typically 587 for STARTTLS, 465 for TLS, 25 for plain)', 0),
    ('alerts.smtp_username', '', 'string', 'alerts', 'SMTP authentication username', 0),
    ('alerts.smtp_password', '', 'string', 'alerts', 'SMTP authentication password', 1),
    ('alerts.smtp_from', '', 'string', 'alerts', 'Email address to send alerts from', 0),
    ('alerts.smtp_reply_to', '', 'string', 'alerts', 'Reply-to email address for alerts', 0),
    ('alerts.smtp_security', 'starttls', 'string', 'alerts', 'SMTP connection security: none, starttls, or tls', 0);
