-- Query stats daily rollup: an authoritative, NON-pruned daily aggregate of
-- executed queries. Unlike query_history (which is capped per user and is only
-- a recent window), this table is never pruned, so all-time analytics
-- (top sources, top users, volume-by-day) stay correct. It is incremented at
-- record time via an UPSERT keyed by (bucket_date,user_id,team_id,source_id,
-- query_language). No FKs — team_id/source_id/user_id are plain columns (mirror
-- query_history) so recording never blocks on referential integrity and rows
-- survive after a team, source, or user is deleted.
CREATE TABLE query_stats_daily (
    bucket_date       DATE   NOT NULL,           -- 'YYYY-MM-DD' (UTC)
    user_id           BIGINT NOT NULL,
    team_id           BIGINT NOT NULL,
    source_id         BIGINT NOT NULL,
    query_language    TEXT   NOT NULL,
    query_count       BIGINT NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_date, user_id, team_id, source_id, query_language)
);

CREATE INDEX idx_query_stats_daily_date ON query_stats_daily(bucket_date);
CREATE INDEX idx_query_stats_daily_source ON query_stats_daily(source_id);
