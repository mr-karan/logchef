CREATE DATABASE IF NOT EXISTS default;

CREATE TABLE IF NOT EXISTS default.http
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

CREATE TABLE IF NOT EXISTS default.syslogs
(
    timestamp DateTime64(3) CODEC(DoubleDelta, LZ4),
    lvl LowCardinality(String) CODEC(ZSTD(1)),
    service_name LowCardinality(String) CODEC(ZSTD(1)),
    namespace LowCardinality(String) CODEC(ZSTD(1)),
    body String CODEC(ZSTD(1)),
    log_attributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    INDEX idx_lvl lvl TYPE set(100) GRANULARITY 4,
    INDEX idx_log_attributes_keys mapKeys(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_log_attributes_values mapValues(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_body body TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (namespace, service_name, timestamp)
TTL toDateTime(timestamp) + INTERVAL 7 DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;
