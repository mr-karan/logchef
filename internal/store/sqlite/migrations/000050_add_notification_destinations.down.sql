-- The targets are not moved back to the rules; restore a backup to recover them.
ALTER TABLE alerts ADD COLUMN recipient_user_ids_json TEXT;
ALTER TABLE alerts ADD COLUMN webhook_urls_json TEXT;
DROP TABLE alert_destinations;
DROP TABLE notification_destinations;
