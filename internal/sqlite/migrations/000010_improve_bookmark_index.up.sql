-- Drop the old index that doesn't cover the updated_at sort
DROP INDEX IF EXISTS idx_team_queries_bookmarked;

-- Create a covering index for the common query pattern:
-- ORDER BY is_bookmarked DESC, updated_at DESC WHERE team_id = ? AND source_id = ?
CREATE INDEX IF NOT EXISTS idx_team_queries_bookmarked ON team_queries(team_id, source_id, is_bookmarked, updated_at);
