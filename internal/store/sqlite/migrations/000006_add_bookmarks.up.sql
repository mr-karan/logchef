-- Add is_bookmarked column to team_queries table
ALTER TABLE team_queries ADD COLUMN is_bookmarked BOOLEAN NOT NULL DEFAULT FALSE;

-- Create index for efficient bookmark filtering and sorting
CREATE INDEX IF NOT EXISTS idx_team_queries_bookmarked ON team_queries(team_id, source_id, is_bookmarked);
