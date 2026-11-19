-- Daily rollup of the filter shape of each recorded query (RFC 0010): the
-- columns and match kinds a query filters on, never values, query text or a
-- user. shape_hash is the first 16 hex characters of SHA-256 of the shape JSON,
-- or 'unanalyzed' or 'overflow'. source_id has no foreign key, as in
-- query_stats_daily, so recording never blocks on referential integrity.
CREATE TABLE query_shape_stats_daily (
    bucket_date       TEXT    NOT NULL, -- 'YYYY-MM-DD' (UTC)
    source_id         INTEGER NOT NULL,
    query_language    TEXT    NOT NULL,
    shape_hash        TEXT    NOT NULL,
    shape             TEXT    NOT NULL,
    query_count       INTEGER NOT NULL DEFAULT 0,
    total_duration_ms INTEGER NOT NULL DEFAULT 0,
    total_rows        INTEGER NOT NULL DEFAULT 0,
    max_lookback_s    INTEGER,
    PRIMARY KEY (bucket_date, source_id, query_language, shape_hash)
);

CREATE INDEX idx_query_shape_stats_daily_source ON query_shape_stats_daily(source_id, bucket_date);
