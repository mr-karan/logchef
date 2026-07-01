ALTER TABLE saved_queries
ADD COLUMN created_from_team_id INTEGER REFERENCES teams(id) ON DELETE SET NULL;

-- Best-effort backfill for existing source-scoped saved queries. The original
-- team_id was intentionally dropped in 000017, so for legacy rows we pick the
-- lowest team currently linked to the query's source.
UPDATE saved_queries
SET created_from_team_id = (
    SELECT MIN(ts.team_id)
    FROM team_sources ts
    WHERE ts.source_id = saved_queries.source_id
)
WHERE created_from_team_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_saved_queries_created_from_team
    ON saved_queries(created_from_team_id);
