-- Revert to the original index without updated_at
DROP INDEX IF EXISTS idx_team_queries_bookmarked;
CREATE INDEX IF NOT EXISTS idx_team_queries_bookmarked ON team_queries(team_id, source_id, is_bookmarked);
