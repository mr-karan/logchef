DROP INDEX IF EXISTS idx_saved_queries_created_from_team;

ALTER TABLE saved_queries DROP COLUMN created_from_team_id;
