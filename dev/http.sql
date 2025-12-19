CREATE TABLE default.http
(
    `timestamp` DateTime64(3) CODEC(DoubleDelta, LZ4),
    `host` LowCardinality(String) CODEC(ZSTD(1)),
    `method` LowCardinality(String) CODEC(ZSTD(1)),
    `protocol` LowCardinality(String) CODEC(ZSTD(1)),
    `referer` String CODEC(ZSTD(1)),
    `request` LowCardinality(String) CODEC(ZSTD(1)),
    `status` UInt16 CODEC(ZSTD(1)),
    `user-identifier` LowCardinality(String) CODEC(ZSTD(1)),
    `bytes` UInt32 CODEC(ZSTD(1)),
    INDEX idx_method method TYPE set(100) GRANULARITY 4,
    INDEX idx_status status TYPE minmax GRANULARITY 4,
    INDEX idx_referer referer TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (host, timestamp)
TTL toDateTime(toUnixTimestamp(timestamp)) + toIntervalDay(7)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;
