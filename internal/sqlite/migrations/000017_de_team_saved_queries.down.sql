-- Best-effort revert: restore team_queries with team_id derived from team_sources.
-- created_by is dropped — v1 schema does not carry it.

DROP INDEX IF EXISTS idx_saved_queries_created_by;
DROP INDEX IF EXISTS idx_saved_queries_source_bookmark;

CREATE TABLE team_queries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL,
    source_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    query_type TEXT NOT NULL,
    query_content TEXT NOT NULL,
    is_bookmarked BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE
);

-- For each saved query, pick the lowest team_id that links to its source.
-- If no team links to that source, fall back to team_id = 1 so the NOT NULL
-- constraint still holds (admin will need to fix this manually).
INSERT INTO team_queries (id, team_id, source_id, name, description, query_type, query_content, is_bookmarked, created_at, updated_at)
SELECT
    sq.id,
    COALESCE(
        (SELECT MIN(ts.team_id) FROM team_sources ts WHERE ts.source_id = sq.source_id),
        (SELECT MIN(id) FROM teams)
    ),
    sq.source_id,
    sq.name,
    sq.description,
    sq.query_type,
    sq.query_content,
    sq.is_bookmarked,
    sq.created_at,
    sq.updated_at
FROM saved_queries sq;

DROP TABLE saved_queries;

CREATE INDEX IF NOT EXISTS idx_team_queries_bookmarked
    ON team_queries(team_id, source_id, is_bookmarked, updated_at);
