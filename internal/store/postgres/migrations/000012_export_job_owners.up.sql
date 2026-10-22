ALTER TABLE export_jobs ADD COLUMN owner_instance_id TEXT NOT NULL DEFAULT '';
ALTER TABLE export_jobs ADD COLUMN owner_process_id TEXT NOT NULL DEFAULT '';
