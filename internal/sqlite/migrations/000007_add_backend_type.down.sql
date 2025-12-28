DROP INDEX IF EXISTS idx_sources_backend_type;

ALTER TABLE sources DROP COLUMN victorialogs_connection;
ALTER TABLE sources DROP COLUMN backend_type;
