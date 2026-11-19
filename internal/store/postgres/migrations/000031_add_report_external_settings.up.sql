-- RFC 0008 unit R9: a reports category for system settings, and the two
-- settings that gate external report recipients, off and with no domain.
ALTER TABLE system_settings DROP CONSTRAINT IF EXISTS system_settings_category_check;
ALTER TABLE system_settings ADD CONSTRAINT system_settings_category_check
    CHECK (category IN ('alerts', 'ai', 'auth', 'server', 'reports'));

-- An empty table is seeded at first boot (seedSystemSettings in
-- internal/app), which skips a table that has rows, so the defaults go in
-- only beside rows that exist.
INSERT INTO system_settings (key, value, value_type, category, description, is_sensitive)
SELECT v.key, v.value, v.value_type, v.category, v.description, v.is_sensitive
FROM (VALUES
    ('reports.external_recipients', 'false', 'boolean', 'reports',
     'Send reports to email addresses that belong to no Logchef user', FALSE),
    ('reports.allowed_email_domains', '', 'string', 'reports',
     'Domains, separated by commas, that external report recipients may use; * allows every domain', FALSE)
) AS v(key, value, value_type, category, description, is_sensitive)
WHERE EXISTS (SELECT 1 FROM system_settings)
ON CONFLICT (key) DO NOTHING;
