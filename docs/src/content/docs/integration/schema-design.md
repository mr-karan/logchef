---
title: Schema Design
description: Understanding LogChef's log schema design and optimization
---

LogChef provides optimized schemas for different types of logs while maintaining the flexibility to work with any Clickhouse table structure.

## OpenTelemetry Schema

Our default schema is optimized for OpenTelemetry logs, providing a balance between query performance and storage efficiency:

```sql
CREATE TABLE logs (
    timestamp DateTime64(3) CODEC(DoubleDelta, LZ4),
    trace_id String CODEC(ZSTD(1)),
    span_id String CODEC(ZSTD(1)),
    trace_flags UInt32 CODEC(ZSTD(1)),
    severity_text LowCardinality(String) CODEC(ZSTD(1)),
    severity_number Int32 CODEC(ZSTD(1)),
    service_name LowCardinality(String) CODEC(ZSTD(1)),
    namespace LowCardinality(String) CODEC(ZSTD(1)),
    body String CODEC(ZSTD(1)),
    log_attributes Map(LowCardinality(String), String) CODEC(ZSTD(1))
)
```

### Key Design Decisions

1. **Optimized Data Types**

   - `DateTime64(3)` for microsecond precision timestamps
   - `LowCardinality(String)` for fields with limited unique values
   - `Map` type for flexible key-value attributes

2. **Efficient Compression**

   - `DoubleDelta, LZ4` for timestamp compression
   - `ZSTD(1)` for optimal compression of string and map fields

3. **Strategic Indexing**

```sql
INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1
INDEX idx_severity_text severity_text TYPE set(100) GRANULARITY 4
INDEX idx_log_attributes_keys mapKeys(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1
INDEX idx_log_attributes_values mapValues(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1
INDEX idx_body body TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
```

4. **Partitioning and Ordering**

```sql
PARTITION BY toDate(timestamp)
ORDER BY (namespace, service_name, timestamp)
```

## HTTP Access Logs Schema

For HTTP access logs, we provide a specialized schema that captures common web server metrics:

```sql
CREATE TABLE http_logs (
    timestamp DateTime,
    remote_addr String,
    request_method String,
    request_uri String,
    status UInt16,
    body_bytes_sent UInt64,
    request_time Float64,
    upstream_response_time Float64,
    request_id String,
    -- Additional fields available
)
```

### Key Features

- Optimized for common HTTP metrics
- Efficient storage of numeric status codes and timings
- Support for standard web server log fields

## Using Custom Schemas

LogChef works with any Clickhouse table structure. When connecting to an existing table:

1. LogChef automatically detects the schema
2. Adapts its query interface to your fields
3. Provides appropriate operators based on field types

## Best Practices

1. **Field Types**

   - Use `LowCardinality(String)` for enum-like fields
   - Use appropriate numeric types (UInt16, Float64) for metrics
   - Consider `Nullable` types if fields might be missing

2. **Compression**

   - Use `ZSTD` for general string compression
   - Use `DoubleDelta` for monotonic numeric sequences
   - Adjust compression levels based on your needs

3. **Indexing**

   - Add bloom filters for high-cardinality string fields
   - Use set indices for low-cardinality fields
   - Consider skip indices for frequently filtered columns

4. **Partitioning**
   - Partition by date for easy data management
   - Choose partition size based on data volume
   - Consider TTL policies for data retention
