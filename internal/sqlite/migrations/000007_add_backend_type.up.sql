-- Add backend_type column with default 'clickhouse' for existing sources
ALTER TABLE sources ADD COLUMN backend_type TEXT NOT NULL DEFAULT 'clickhouse';

-- Add victorialogs_connection column for VictoriaLogs sources (stores JSON)
ALTER TABLE sources ADD COLUMN victorialogs_connection TEXT;

-- Create index for backend type queries
CREATE INDEX IF NOT EXISTS idx_sources_backend_type ON sources(backend_type);
