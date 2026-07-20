---
title: "OpenTelemetry Logs in ClickHouse, Made Actually Queryable with Logchef"
description: "Exporting OTel logs to ClickHouse solves storage, not search. Logchef adds a query UI, field explorer, and alerting over your existing otel_logs table."
pubDate: 2026-07-15
tags: ["opentelemetry", "clickhouse", "observability", "logs", "otel"]
author: "Logchef Team"
---

You wired up the OpenTelemetry Collector, pointed the `clickhouseexporter` at your ClickHouse cluster, and logs are flowing. `otel_logs` is filling up, insert throughput is fine, and storage costs dropped compared to whatever SaaS platform you were paying per-GB before. On paper, you're done.

Then someone asks "did the checkout service log anything about `CONNECTION_REFUSED` in the last hour, for this trace ID?" and you're staring at a table with a `LogAttributes` column of type `Map(String, String)`, a `ResourceAttributes` map with another few dozen keys, and a `clickhouse-client` prompt. You can answer the question. It takes a `WHERE ResourceAttributes['service.name'] = 'checkout' AND positionCaseInsensitive(Body, 'CONNECTION_REFUSED') > 0` query you have to hand-write, re-derive the right map key casing from memory, and paste into a terminal, every time, for every on-call engineer, with no history of what anyone queried last time.

That's the actual gap. Getting OTel logs *into* ClickHouse is well-trodden ground. Getting a team to *search* them without everyone becoming a ClickHouse SQL expert is not. [Logchef](https://github.com/mr-karan/logchef) is a single-binary log analytics UI built specifically for this: point it at your ClickHouse table (including the exact `otel_logs` schema the Collector creates), and it gives you a query language, a field sidebar, saved queries, live tail, and alerting on top.

## Getting OTel logs into ClickHouse

Two common paths. If you're running the OpenTelemetry Collector, its `clickhouseexporter` will create the table for you:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    send_batch_size: 1024

exporters:
  clickhouse:
    endpoint: tcp://localhost:9000
    database: otel
    username: default
    password: "${env:CLICKHOUSE_PASSWORD}"
    ttl: 720h # 30 days
    logs_table_name: otel_logs
    create_schema: true
    timeout: 5s
    sending_queue:
      enabled: true
      num_consumers: 10

service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [clickhouse]
```

With `create_schema: true`, the exporter creates a table roughly shaped like this (the exact DDL varies slightly by exporter version, but the column set and casing are stable):

```sql
CREATE TABLE otel.otel_logs
(
    Timestamp          DateTime64(9),
    TraceId             String,
    SpanId              String,
    TraceFlags          UInt32,
    SeverityText        LowCardinality(String),
    SeverityNumber      Int32,
    ServiceName         LowCardinality(String),
    Body                String,
    ResourceAttributes  Map(LowCardinality(String), String),
    ScopeName           String,
    ScopeAttributes     Map(LowCardinality(String), String),
    LogAttributes       Map(LowCardinality(String), String)
)
ENGINE = MergeTree
ORDER BY (toStartOfFiveMinutes(Timestamp), ServiceName, Timestamp);
```

If you'd rather ship via [Vector](https://vector.dev) (say, you're not running the Collector, or you want more control over the transform), Logchef's own bundled schema is a simplified, single-map take on the same OTel data model (`timestamp`, `severity_text`, `severity_number`, `service_name`, `trace_id`, `span_id`, `body`, one flat `log_attributes` map). A minimal Vector sink into that shape:

```toml
[sources.otlp_logs.grpc]
address = "0.0.0.0:4317"

[sources.otlp_logs]
type = "opentelemetry"

[transforms.remap_otel]
inputs = ["otlp_logs.logs"]
type = "remap"
source = '''
  # Vector's opentelemetry source already exposes .message, .resources,
  # .attributes, .severity_text, .severity_number, .trace_id, .span_id
  # as top-level fields — just reshape them into LogChef's schema.
  .service_name = .resources."service.name" ?? "unknown"
  .namespace = .resources."service.namespace" ?? "default"
  .body = .message
  .log_attributes = .attributes

  del(.message)
  del(.resources)
  del(.attributes)
'''

[sinks.clickhouse]
type = "clickhouse"
inputs = ["remap_otel"]
endpoint = "http://localhost:8123"
database = "default"
table = "logs"
```

Either way, the point is the same: **Logchef doesn't require its own schema.** It connects to any ClickHouse table with a timestamp column, auto-detects the columns, and adapts its query interface to them, so the real `otel_logs` table the Collector already created for you works as-is. You can even declare it via [provisioning](/getting-started/provisioning) config with `table_name = "otel_logs"` and skip the UI setup entirely.

## Querying it: LogchefQL over Map columns

Once a source is connected, Logchef's query bar defaults to **LogchefQL** — a small filter language that compiles to SQL for ClickHouse sources (or LogsQL for VictoriaLogs sources). The part that matters for OTel data: dot notation on `Map` columns.

Filter by service and severity:

```
ServiceName="checkout" and SeverityText="ERROR"
```

Find everything tied to a trace, with a focused column set instead of `SELECT *`:

```
TraceId="4bf92f3577b34da6a3ce929d0e0e4736" | Timestamp ServiceName SpanId Body
```

Resource attributes in OTel are keyed with literal dots — `service.name`, `deployment.environment`, `k8s.pod.name` — which is exactly what LogchefQL's nested-field syntax expects. Query a resource attribute directly:

```
ResourceAttributes.deployment.environment="production" and LogAttributes.http.status_code>=500
```

That compiles to real ClickHouse map subscript access:

```sql
SELECT *
FROM otel.otel_logs
WHERE (`ResourceAttributes`['deployment.environment'] = 'production')
  AND (`LogAttributes`['http.status_code'] >= 500)
  AND Timestamp BETWEEN toDateTime('2026-07-14 09:00:00', 'UTC')
                     AND toDateTime('2026-07-14 10:00:00', 'UTC')
ORDER BY Timestamp DESC
LIMIT 100
```

Substring search on the log body uses `~` (case-insensitive contains, `positionCaseInsensitive` under the hood):

```
Body~"connection refused" and ServiceName="checkout"
```

If a map key itself contains a dot as a single literal key (rather than being a multi-segment path you want joined), quote the segment: `LogAttributes."user.name"="alice"`. And when you need aggregation (`GROUP BY ServiceName, SeverityText`, error-rate-over-time, joins against a traces table), LogchefQL steps aside for **native SQL mode**, where the query runs exactly as written, with no automatic time-range or `LIMIT` injected.

## Actually finding the field names

The honest failure mode of hand-writing these queries isn't syntax, it's not remembering what's in the map. Did that service tag its span with `http.status_code` or `http.response.status_code`? Is it `k8s.pod.name` or `k8s.pod_name`? Logchef's **field sidebar** exists for this: it lists queryable columns from your source and their top distinct values for the current time range, with click-to-filter.

Two honest caveats here, because it's easy to oversell this feature for OTel data specifically. First, `LowCardinality` and `Enum` columns (`ServiceName`, `SeverityText` in the schema above) load their top values automatically when you open the sidebar; plain `String` columns require a click, so you don't accidentally fire an expensive distinct-values scan on something like `Body`. Second, and more importantly: **`Map`, `Array`, `Tuple`, and JSON columns are excluded from the sidebar's value list entirely**, since ClickHouse can't cheaply enumerate distinct values for them. That means `ResourceAttributes` and `LogAttributes` themselves won't show up as expandable fields with a value list; you still need to know the key you're looking for and type `ResourceAttributes.service.name=` yourself. The sidebar helps you discover `ServiceName` and `SeverityText` values instantly; it doesn't turn the attribute maps into a browsable tree.

## Alerting without a separate stack

Once the query works, turning it into an alert is the same LogchefQL condition plus a threshold — no separate alerting pipeline to stand up:

```
SeverityText = "ERROR" and ServiceName = "checkout"
```

Pick an aggregate (`count`, `avg`, etc.), a threshold, a lookback window, and evaluation frequency; Logchef generates the executable ClickHouse query and runs it on schedule, notifying over email or webhook (Slack incoming webhooks, PagerDuty, or anything else that takes a POST). For anything the condition builder can't express, native mode takes a raw SQL query that returns one numeric value:

```sql
SELECT count() AS value
FROM otel.otel_logs
WHERE ResourceAttributes['deployment.environment'] = 'production'
  AND SeverityText = 'ERROR'
  AND Timestamp >= now() - toIntervalMinute(5)
```

## Live tail and saved queries

For the "watch it happen right now" case, **Live** streams matching rows as they arrive instead of running one-shot queries. Worth knowing that on ClickHouse sources this polls rather than subscribes, so it's not a true push stream, and it can occasionally re-show a row near the poll boundary. It's still faster than re-running the same query on a loop by hand.

And once a query is worth keeping (the one that finds checkout errors by trace, the one that surfaces 5xx spikes by environment), **Save** it into your personal library or a shared collection (an on-call runbook, say), so the next person paging doesn't have to reconstruct the map-key syntax from scratch.

## What this doesn't do

To be direct about the boundaries: Logchef doesn't parse or reshape OTLP itself — it's a query and access layer over a ClickHouse (or VictoriaLogs) table that something else (the Collector, Vector, Fluent Bit, whatever) already populated. There's no OTLP receiver built into Logchef, no schema-transform step, and no special-cased "OTel mode" beyond the fact that its bundled default schema happens to be modeled on the OTel log data model and its query language happens to handle dotted map keys well. If your `LogAttributes` map is inconsistent across services (one team using `http.status_code`, another `httpStatusCode`), Logchef will surface that inconsistency exactly as-is; it won't normalize it for you.

## Try it

The fastest way to see this against real OTel-shaped data is the [hosted demo](https://demo.logchef.app) — no signup, pre-loaded sources, LogchefQL query bar included. For self-hosting, Logchef ships as a single Go binary plus SQLite for metadata; your logs stay in your ClickHouse. Source, docs, and the Docker quick-start are at [github.com/mr-karan/logchef](https://github.com/mr-karan/logchef).
