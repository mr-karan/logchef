---
title: VictoriaLogs vs ClickHouse for Logs
description: "VictoriaLogs vs ClickHouse for log storage: schema, ingestion, query language, full-text search, retention and clustering compared, with sources linked."
---

VictoriaLogs and ClickHouse both store logs as compressed columns and both handle high-cardinality fields such as trace IDs. The difference is scope. VictoriaLogs is a database built only for logs: it has no schema, it accepts log shipper protocols directly, and you search it with LogsQL. ClickHouse is a general SQL analytics database: you design the table and the ingestion pipeline, and you get full SQL in return.

Facts on this page come from the official VictoriaLogs and ClickHouse docs and license files as of November 2026. Sources are linked inline.

## The short version

Choose VictoriaLogs if you want a log store that runs as one binary, takes logs from your existing shipper, and needs no schema design. It suits teams whose log fields change often and whose questions are mostly search and counts.

Choose ClickHouse if you want SQL over logs, joins with other data, built-in replication, or object storage for old data. It suits teams that already run ClickHouse or have someone to own a schema.

You do not have to pick one for every workload. A query UI such as Logchef can read both at once. See [Using either with Logchef](#using-either-with-logchef).

## At a glance

| | VictoriaLogs | ClickHouse |
|---|---|---|
| What it is | An "open source user-friendly database for logs" ([docs](https://docs.victoriametrics.com/victorialogs/)) | "A high-performance, column-oriented SQL database management system (DBMS) for online analytical processing (OLAP)" ([intro](https://clickhouse.com/docs/get-started/about/intro)) |
| License | Apache 2.0 ([LICENSE](https://github.com/VictoriaMetrics/VictoriaLogs/blob/master/LICENSE)). A separate enterprise build adds features such as mTLS ([flags](https://docs.victoriametrics.com/victorialogs/#enterprise-flags)) | Apache 2.0 ([LICENSE](https://github.com/ClickHouse/ClickHouse/blob/master/LICENSE)) |
| Data model and schema | No schema. An entry is a flat set of string fields, with special `_msg`, `_time` and `_stream` fields ([key concepts](https://docs.victoriametrics.com/victorialogs/keyconcepts/#data-model)) | Typed tables that you define. ClickHouse recommends writing your own log schema ([schema design](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/schema-design)) |
| Ingestion protocols | Elasticsearch bulk, JSON lines, Loki push, OpenTelemetry, journald and Splunk HTTP APIs, plus syslog ([data ingestion](https://docs.victoriametrics.com/victorialogs/data-ingestion/)) | SQL inserts over the HTTP or native TCP interface ([interfaces](https://clickhouse.com/docs/concepts/features/connect-to-clickhouse)). ClickHouse recommends the OpenTelemetry Collector with its ClickHouse exporter ([OTel guide](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/integrating-opentelemetry)) |
| Query language | [LogsQL](https://docs.victoriametrics.com/victorialogs/logsql/): filters followed by pipes | SQL, with joins, subqueries and window functions ([intro](https://clickhouse.com/docs/get-started/about/intro)) |
| Full-text search | Built in. Word and phrase filters, with bloom filters that skip blocks ([how it works](https://docs.victoriametrics.com/victorialogs/faq/#how-does-victorialogs-work)) | Optional. [Text indexes](https://clickhouse.com/docs/reference/engines/table-engines/mergetree-family/textindexes) are generally available from version 26.2. Without one, ClickHouse relies on parallel linear scans ([observability intro](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/introduction)) |
| High-cardinality fields | Fine as ordinary fields. They must not be stream fields ([high cardinality](https://docs.victoriametrics.com/victorialogs/keyconcepts/#high-cardinality)) | Stored as columns. A bloom filter [skip index](https://clickhouse.com/docs/concepts/features/performance/skip-indexes/skipping-indexes) speeds up lookups |
| Aggregations and analytics | The [`stats` pipe](https://docs.victoriametrics.com/victorialogs/logsql/#stats-pipe), a [`join` pipe](https://docs.victoriametrics.com/victorialogs/logsql/#join-pipe) and [subquery filters](https://docs.victoriametrics.com/victorialogs/logsql/#subquery-filter) | Analytical SQL: `GROUP BY`, joins, window functions and approximate aggregates ([intro](https://clickhouse.com/docs/get-started/about/intro)) |
| Storage and compression | Per-day partitions in one local directory ([storage](https://docs.victoriametrics.com/victorialogs/#storage)). The docs say it "usually compresses logs by 10x or more times" ([retention](https://docs.victoriametrics.com/victorialogs/#retention-by-disk-space-usage)) | Column storage with per-column codecs ([compression](https://clickhouse.com/docs/guides/clickhouse/data-modelling/compression/compression-in-clickhouse)). The docs say it "compresses logs and traces on average up to 14x" ([observability intro](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/introduction)) |
| Retention | The `-retentionPeriod` flag, 7 days by default, plus optional disk usage limits ([retention](https://docs.victoriametrics.com/victorialogs/#retention)) | `TTL` on a table or a column, and dropping partitions ([managing data](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/managing-data)) |
| Clustering and HA | Cluster mode shards logs across storage nodes and does not replicate them ([cluster](https://docs.victoriametrics.com/victorialogs/cluster/#replication)) | Replicated tables coordinated by ClickHouse Keeper ([replication](https://clickhouse.com/docs/reference/engines/table-engines/mergetree-family/replication)), and the [Distributed engine](https://clickhouse.com/docs/reference/engines/table-engines/special/distributed) for queries across servers |
| Operational footprint | "A single zero-config executable" with a built-in web UI ([docs](https://docs.victoriametrics.com/victorialogs/)) | A database server. For observability, "both a user interface and data collection framework are required" ([observability intro](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/introduction)) |
| Managed offerings | [VictoriaMetrics Cloud](https://docs.victoriametrics.com/victoriametrics-cloud/) supports VictoriaLogs deployments | [ClickHouse Cloud](https://clickhouse.com/docs/products/cloud/getting-started/intro), "a fully managed cloud service" |

The two compression figures are each vendor's own, measured on different data. They are not a comparison.

## How they differ

### No schema versus a schema you design

VictoriaLogs accepts any set of fields, and the set can change from one log source to the next. Its FAQ puts the trade-off this way: ClickHouse "is good for logs if you know the set of log fields and the expected query types beforehand" ([FAQ](https://docs.victoriametrics.com/victorialogs/faq/#what-is-the-difference-between-victorialogs-and-clickhouse)).

The ClickHouse docs say much the same from the other side: "We recommend users always create their own schema for logs and traces", starting with an ordering key that fits your queries ([schema design](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/schema-design)). For fields you cannot predict, ClickHouse has the `Map` type, which is slower to read than a normal column, and a [JSON type](https://clickhouse.com/docs/reference/data-types/newjson) that is production ready from version 25.3.

If you choose ClickHouse and have no schema yet, Logchef's [schema design guide](/integration/schema-design/) has an OpenTelemetry-style table to start from.

### Ingestion

VictoriaLogs speaks the protocols that log shippers already use, so Vector, Fluent Bit, Filebeat or the OpenTelemetry Collector can write to it directly ([data ingestion](https://docs.victoriametrics.com/victorialogs/data-ingestion/)).

ClickHouse takes SQL inserts, so a collector has to map each log to your table's columns. ClickHouse recommends the OpenTelemetry Collector for this. [ClickStack](https://clickhouse.com/docs/clickstack/overview), its open source observability stack, bundles a preconfigured collector and a UI. Logchef has guides for [Vector](/integration/vector/) and the [OpenTelemetry Collector](/integration/otel-collector/) writing to ClickHouse.

### LogsQL versus SQL

A LogsQL query is a set of filters followed by optional pipes. The query `error` finds that word in log messages. VictoriaLogs says LogsQL "is usually easier to use than SQL for typical log analysis tasks" and publishes a [SQL to LogsQL tutorial](https://docs.victoriametrics.com/victorialogs/sql-to-logsql/).

ClickHouse asks you to "embrace SQL-based observability" ([observability intro](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/introduction)). In return you get arbitrary analytics and joins between logs and any other table.

Full-text search is automatic in VictoriaLogs. In ClickHouse you add a text index to the columns you search. ClickHouse now marks the older `tokenbf_v1` and `ngrambf_v1` indexes as deprecated in favor of the text index ([skip indexes](https://clickhouse.com/docs/concepts/features/performance/skip-indexes/skipping-indexes)). Logchef's built-in schema still uses `tokenbf_v1` on the log body.

### Streams versus ordering keys

VictoriaLogs groups logs by stream, a set of fields that identify one application instance. Logs of a stream are stored together, which improves compression and lets a stream filter skip other data ([stream fields](https://docs.victoriametrics.com/victorialogs/keyconcepts/#stream-fields)). Stream fields must be stable. The docs say fields such as `ip`, `user_id` and `trace_id` "must never be associated with log streams".

In ClickHouse the table's `ORDER BY` key does this job. Logchef's built-in schema orders by `(namespace, service_name, timestamp)`. Both stores reward the same habit: put stable identity fields first, and keep per-request values out of them.

### Scaling and availability

VictoriaLogs prefers a bigger single node over a cluster while one host is enough. Cluster mode adds `vlinsert`, `vlselect` and `vlstorage` roles, all from the same executable. Storage nodes are not replicated, and by default a query fails with a 502 error while a storage node is down. For high availability the docs tell you to send copies of the logs to independent instances or clusters ([cluster](https://docs.victoriametrics.com/victorialogs/cluster/#high-availability)). Object storage is on the [roadmap](https://docs.victoriametrics.com/victorialogs/roadmap/), not shipped.

ClickHouse has asynchronous multi-master replication ([intro](https://clickhouse.com/docs/get-started/about/intro)), coordinated through ClickHouse Keeper or ZooKeeper. Storage tiers can keep recent data on SSD and older data on S3 ([managing data](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/managing-data)).

## When VictoriaLogs is the better choice

- You want logs searchable soon after install, with no schema or pipeline mapping.
- Your log fields vary between services or change often.
- Most queries are searches, filters and counts over time.
- One node, or a few shards, covers your volume.
- Nobody on the team wants to own a database schema.

## When ClickHouse is the better choice

- You want SQL, or you need to join logs with other tables.
- You already run ClickHouse and know how to operate it.
- You need replication inside the cluster, or old data on object storage.
- You know your fields and queries well enough to design an ordering key.
- You want different retention for different tables or columns.

## Using either with Logchef

Logchef is an AGPLv3 log UI that stores no logs. It reads ClickHouse, VictoriaLogs and Grafana Loki, so the choice above does not lock in your UI.

- **ClickHouse sources** work with any table that has a `DateTime` or `DateTime64` timestamp column. You query in [LogchefQL](/guide/search-syntax/) or ClickHouse SQL.
- **VictoriaLogs sources** are queried in LogchefQL, which Logchef compiles to LogsQL, or in [native LogsQL](/guide/logsql-queries/). The [VictoriaLogs tutorial](/tutorials/victorialogs/) covers setup.
- **Both at once.** A [dashboard](/features/dashboards/#multiple-data-sources) can mix panels from ClickHouse and VictoriaLogs sources, and the same LogchefQL filter runs on either.

A few features depend on the store. Log context, exports and the [schema advisor](/features/schema-advisor/) work on ClickHouse sources only. [Windowed search](/guide/windowed-search/) is for VictoriaLogs sources only. Live tail streams from VictoriaLogs and polls ClickHouse.

## FAQ

### Is VictoriaLogs faster than ClickHouse for logs?

We found no head-to-head log benchmark against the other store in either project's official docs. The VictoriaLogs [benchmarks section](https://docs.victoriametrics.com/victorialogs/#benchmarks) compares it with Elasticsearch, Loki and several other databases, and recommends running it next to your current system to compare. Speed depends on your schema in ClickHouse and your stream fields in VictoriaLogs. Test both on your own logs.

### Can VictoriaLogs replace ClickHouse?

For log search and log analytics, often yes. For general analytics, no. VictoriaLogs describes itself as "designed solely for logs" ([FAQ](https://docs.victoriametrics.com/victorialogs/faq/#what-is-the-difference-between-victorialogs-and-clickhouse)), and its query language is LogsQL, not SQL.

### Does VictoriaLogs use ClickHouse?

No. VictoriaLogs is a separate database with its own storage. Its FAQ says the architecture "is inspired by ClickHouse architecture" ([how it works](https://docs.victoriametrics.com/victorialogs/faq/#how-does-victorialogs-work)).

### Does VictoriaLogs support SQL?

Its query language is LogsQL. The [SQL to LogsQL tutorial](https://docs.victoriametrics.com/victorialogs/sql-to-logsql/) maps SQL clauses to LogsQL pipes.

### Is ClickHouse good for logs?

Yes, if you design for it. The ClickHouse docs say it "alone isn't an out-of-the-box solution for Observability" but can be "a highly efficient storage engine" for that data ([observability intro](https://clickhouse.com/docs/guides/use-cases/observability/build-your-own/introduction)).

### Can I run VictoriaLogs and ClickHouse together?

Yes. One split keeps general application logs in VictoriaLogs and logs that need SQL in ClickHouse. Logchef can search both from one UI.

To try Logchef with either store, start with the [quickstart](/getting-started/quickstart/).
