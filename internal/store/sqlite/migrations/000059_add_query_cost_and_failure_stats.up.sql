-- Cost of each successful query run in the daily rollups: what it scanned,
-- the rows it returned, and a latency histogram with one column per bucket
-- of models.LatencyBoundsMs, so percentiles sum across rows. Rows written
-- before this migration keep zeros.
ALTER TABLE query_stats_daily ADD COLUMN rows_read INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN bytes_read INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN rows_returned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_100ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_250ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_500ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_1s INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_2500ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_5s INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_10s INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_30s INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_le_60s INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_stats_daily ADD COLUMN latency_gt_60s INTEGER NOT NULL DEFAULT 0;

ALTER TABLE query_shape_stats_daily ADD COLUMN total_rows_read INTEGER NOT NULL DEFAULT 0;
ALTER TABLE query_shape_stats_daily ADD COLUMN total_bytes_read INTEGER NOT NULL DEFAULT 0;

-- Failed query runs per day, by query.run failure reason. Like
-- query_stats_daily it is never pruned and has no foreign keys.
CREATE TABLE query_failure_stats_daily (
    bucket_date    TEXT    NOT NULL, -- 'YYYY-MM-DD' (UTC)
    user_id        INTEGER NOT NULL,
    team_id        INTEGER NOT NULL,
    source_id      INTEGER NOT NULL,
    query_language TEXT    NOT NULL,
    reason         TEXT    NOT NULL,
    query_count    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_date, user_id, team_id, source_id, query_language, reason)
);
