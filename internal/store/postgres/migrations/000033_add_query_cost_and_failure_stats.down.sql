DROP TABLE query_failure_stats_daily;

ALTER TABLE query_shape_stats_daily DROP COLUMN total_bytes_read;
ALTER TABLE query_shape_stats_daily DROP COLUMN total_rows_read;

ALTER TABLE query_stats_daily DROP COLUMN latency_gt_60s;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_60s;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_30s;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_10s;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_5s;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_2500ms;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_1s;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_500ms;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_250ms;
ALTER TABLE query_stats_daily DROP COLUMN latency_le_100ms;
ALTER TABLE query_stats_daily DROP COLUMN rows_returned;
ALTER TABLE query_stats_daily DROP COLUMN bytes_read;
ALTER TABLE query_stats_daily DROP COLUMN rows_read;
