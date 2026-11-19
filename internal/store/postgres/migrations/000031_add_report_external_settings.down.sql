DELETE FROM system_settings WHERE category = 'reports';
ALTER TABLE system_settings DROP CONSTRAINT IF EXISTS system_settings_category_check;
ALTER TABLE system_settings ADD CONSTRAINT system_settings_category_check
    CHECK (category IN ('alerts', 'ai', 'auth', 'server'));
