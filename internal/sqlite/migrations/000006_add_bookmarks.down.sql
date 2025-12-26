-- Remove bookmark index
DROP INDEX IF EXISTS idx_team_queries_bookmarked;

-- Remove is_bookmarked column from team_queries table
ALTER TABLE team_queries DROP COLUMN is_bookmarked;
