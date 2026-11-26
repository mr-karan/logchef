---
title: Schema Design
head:
  - tag: title
    content: "ClickHouse Log Schema Design | Logchef"
description: "Design ClickHouse tables for log analytics with Logchef. Choose columns, ordering keys, and retention settings for efficient storage and queries."
---

The built-in schemas below index the log body with a `text` index, which needs ClickHouse 26.8 or later. On an older server, create your own table with a different body index, or none, and point Logchef at it.

Logchef works with any ClickHouse table that has a timestamp field. If you don't have an existing schema, or want to start ingesting logs quickly without worrying about the details, you can use the optimized built-in schemas Logchef provides.

## OpenTelemetry Schema

Our default schema is optimized for OpenTelemetry logs, providing a balance between query performance and storage efficiency:

```sql
CREATE TABLE "{{database_name}}"."{{table_name}}" (
    timestamp DateTime64(3) CODEC(DoubleDelta, LZ4),
    trace_id String CODEC(ZSTD(1)),
    span_id String CODEC(ZSTD(1)),
    trace_flags UInt32 CODEC(ZSTD(1)),
    severity_text LowCardinality(String) CODEC(ZSTD(1)),
    severity_number Int32 CODEC(ZSTD(1)),
    service_name LowCardinality(String) CODEC(ZSTD(1)),
    namespace LowCardinality(String) CODEC(ZSTD(1)),
    body String CODEC(ZSTD(1)),
    log_attributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),

    INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_severity_text severity_text TYPE set(100) GRANULARITY 4,
    INDEX idx_log_attributes_keys mapKeys(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_log_attributes_values mapValues(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_body body TYPE text(tokenizer = splitByNonAlpha)
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (namespace, service_name, timestamp)
TTL toDateTime(timestamp) + INTERVAL {{ttl_day}} DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;
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
INDEX idx_body body TYPE text(tokenizer = splitByNonAlpha)
```

ClickHouse deprecates `tokenbf_v1` and `ngrambf_v1` in favor of the `text` index. `hasToken`, `hasAnyTokens` and `hasAllTokens` read the text index. Filters with `=`, `IN`, `startsWith` and many `LIKE` patterns use it to skip data. No index serves the LogchefQL `~` operator, which compiles to `positionCaseInsensitive`. To check a query, run `EXPLAIN indexes = 1` on it.

4. **Partitioning and Ordering**

```sql
PARTITION BY toDate(timestamp)
ORDER BY (namespace, service_name, timestamp)
```

### Field Descriptions

- **`timestamp`**: The time the log event occurred. Must be `DateTime` or `DateTime64`. This is the **only strictly required field** for Logchef to function.
- **`severity_text` / `severity_number`**: Represents the log level (e.g., 'INFO', 'ERROR'). Using `LowCardinality(String)` for `severity_text` is highly recommended for performance.
- **`service_name` / `namespace`**: Identifies the origin of the log. `LowCardinality(String)` improves query speed.
- **`body`**: The primary content of the log message.
- **`trace_id` / `span_id`**: Link logs to distributed traces if you are using OpenTelemetry tracing.
- **`log_attributes`**: A `Map` type for storing arbitrary key-value pairs associated with the log. This allows for flexible and structured logging without needing to alter the table schema frequently.

## Using Custom Schemas

Logchef works with any ClickHouse table structure. When connecting to an existing table:

1. Logchef automatically detects the schema
2. Adapts its query interface to your fields
3. Provides appropriate operators based on field types

Remember, you are **not required** to use Logchef's schemas. Logchef can connect to any existing ClickHouse table as long as it has a `timestamp` column (`DateTime` or `DateTime64`). For optimal query performance with custom schemas, consider using `LowCardinality(String)` for frequently repeated text fields and a `Map` type for flexible attributes.

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

## Next steps

- See [Shipping Logs with Vector](/integration/vector/) to ship logs into a table with this schema
- Follow [Shipping NGINX Logs to ClickHouse](/tutorials/nginx-logs/) for a purpose-built schema example
- Review [Search Syntax](/guide/search-syntax/) for how LogchefQL maps to Map and JSON columns
