-- Query history: a persistent, per-user log of queries executed on the preview
-- paths (/logs/query and /logchefql/query). Replaces the old localStorage-only
-- history so it survives across machines. Rows are pruned to the newest N per
-- user on insert (see PruneQueryHistoryForUser). user_id cascades on delete so a
-- removed user's history is cleaned up automatically; team_id/source_id are kept
-- as plain columns (no FK) so history is retained even after a team or source is
-- deleted.
CREATE TABLE query_history (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id BIGINT NOT NULL,
    source_id BIGINT NOT NULL,
    query_text TEXT NOT NULL,
    query_language TEXT NOT NULL,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    row_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_query_history_user_created ON query_history(user_id, created_at DESC);
