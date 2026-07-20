---
title: From Elasticsearch
description: Migrate Elasticsearch and Kibana log analytics to ClickHouse with Logchef — pipeline re-point, ECS schema mapping, KQL to LogchefQL/SQL examples.
---

This guide covers moving log analytics off Elasticsearch/Kibana and onto
ClickHouse (or VictoriaLogs) with Logchef as the query UI. It does not cover
Elasticsearch as a general search engine, APM, or SIEM product — only its use
as a log store queried through Kibana Discover.

## What actually moves

A typical ELK log pipeline looks like:

```
Filebeat / Logstash  →  Elasticsearch  →  Kibana Discover
```

Moving to Logchef changes the last two stages, not necessarily the first:

```
Filebeat / Logstash / Vector / OTel Collector  →  ClickHouse  →  Logchef
```

Your ingestion-side concerns — tailing files, parsing multiline stack traces,
scraping container logs — don't disappear. What changes is the *output*: instead
of writing to an Elasticsearch bulk endpoint, the shipper writes rows to a
ClickHouse table (or, if you'd rather keep a label-oriented backend, to
VictoriaLogs — see [the shortcut below](#shortcut-keep-your-shipper-target-victorialogs)).

### Re-pointing the pipeline

If you're on **Filebeat** or **Logstash**, the least-effort path is to keep
them for collection and only swap the output plugin (Logstash has a
[ClickHouse output plugin](https://www.elastic.co/guide/en/logstash/current/output-plugins.html)
maintained outside Elastic; Filebeat has no first-party ClickHouse output).
In practice, most teams migrating off ELK replace the shipper with
[Vector](https://vector.dev) or the OpenTelemetry Collector, since both ship a
native ClickHouse sink and can read from the same sources (files, journald,
Docker/Kubernetes container logs, syslog) that Filebeat and Logstash do. See
[Shipping Logs with Vector](/integration/vector) for working configs — the
`[sources]` blocks (file, docker_logs, journald) are close analogs to Filebeat
inputs; only the `[sinks.clickhouse]` block is new.

### Shortcut: keep your shipper, target VictoriaLogs

If a full pipeline rewrite isn't practical right now, VictoriaLogs exposes an
[Elasticsearch-compatible bulk ingestion endpoint](https://docs.victoriametrics.com/victorialogs/data-ingestion/),
so a Filebeat or Logstash output aimed at an Elasticsearch bulk API can often
be re-pointed at VictoriaLogs with a URL and auth change, no shipper
replacement required. You get Logchef as your query UI immediately and can
migrate the ingestion side later. See [Using VictoriaLogs with
Logchef](/tutorials/victorialogs).

## Schema mapping: ECS fields → ClickHouse columns

Elastic Common Schema (ECS) puts almost everything under namespaced objects —
only `@timestamp`, `message`, `labels`, and `tags` live at the root; fields
like log level, service name, and HTTP status live under `log.*`,
`service.*`, and `http.*` ([ECS field
reference](https://www.elastic.co/guide/en/ecs/current/ecs-field-reference.html)).
ClickHouse has no equivalent to a dynamic mapping, so you decide up front
which of those nested fields become real columns and which stay as flexible
attributes.

Logchef's [built-in OpenTelemetry schema](/integration/schema-design) is a
reasonable default target:

| ECS field | Logchef/ClickHouse column | Notes |
|---|---|---|
| `@timestamp` | `timestamp` (`DateTime64(3)`) | Required by Logchef |
| `message` | `body` (`String`) | Primary log text |
| `log.level` | `severity_text` (`LowCardinality(String)`) | Small, stable value set |
| `service.name` | `service_name` (`LowCardinality(String)`) | |
| `http.response.status_code`, `trace.id`, etc. | `log_attributes` (`Map(LowCardinality(String), String)`) | Anything you don't want to promote to a column |

You don't have to flatten everything: fields you'd rather keep exactly as ECS
shaped them (`http.response.status_code`, `user.id`, …) can stay inside
`log_attributes` and be queried with [dot
notation](/guide/search-syntax#nested-field-access), including the quoted
form for keys that contain a literal dot, e.g. `log_attributes."http.response.status_code"`.
Only promote a field to its own column when you filter or aggregate on it
constantly — that's what earns it an index.

## Query translation: KQL → LogchefQL / SQL

Kibana's default query language (KQL) is deliberately filter-only — per
[Elastic's own KQL docs](https://www.elastic.co/guide/en/kibana/current/kuery-query.html),
it has no role in aggregating or sorting, that's left to Lens/visualizations.
[LogchefQL](/guide/search-syntax) plays the same narrow role: fast filtering,
with SQL mode for aggregations.

Assuming ECS fields were flattened into `severity_text`, `service_name`, and a
`status_code` column at ingestion:

| KQL | LogchefQL | ClickHouse SQL |
|---|---|---|
| `log.level: "error"` | `severity_text="error"` | `WHERE severity_text = 'error'` |
| `service.name: "payment-api" and log.level: "error"` | `service_name="payment-api" and severity_text="error"` | `WHERE service_name = 'payment-api' AND severity_text = 'error'` |
| `http.response.status_code >= 500` | `status_code>=500` | `WHERE status_code >= 500` |
| `http.response.status_code: 4*` | `status_code>=400 and status_code<500` | `WHERE status_code >= 400 AND status_code < 500` |
| `message: "timeout"` | `body~"timeout"` | `WHERE positionCaseInsensitive(body, 'timeout') > 0` |
| `(service.name: "auth" or service.name: "users") and log.level: "error"` | `(service_name="auth" or service_name="users") and severity_text="error"` | `WHERE (service_name = 'auth' OR service_name = 'users') AND severity_text = 'error'` |

If you instead kept those fields nested in `log_attributes` rather than
flattening them, the same filters work with dot notation:
`log_attributes."log.level"="error"`. See [Search Syntax](/guide/search-syntax)
for the full operator set, and [Query Examples](/guide/examples) for more
worked patterns.

For anything KQL can't express — aggregations, `GROUP BY`, joins — switch to
Logchef's SQL mode, the same way you'd reach for Lens or a scripted metric
aggregation in Kibana.

## What you gain

- **SQL instead of a separate DSL.** Aggregations, `GROUP BY`, window
  functions, and joins are plain ClickHouse SQL — no Painless scripting or a
  separate visualization builder to learn.
- **Columnar compression.** ClickHouse's columnar storage with codecs like
  `ZSTD` and `DoubleDelta` (see [Schema Design](/integration/schema-design))
  is generally more storage-efficient for log-shaped data than Elasticsearch's
  inverted-index-plus-doc-values model, though the exact ratio depends
  heavily on your schema and data.
- **Less operational surface.** No JVM heap sizing, no shard/replica count
  planning, no index lifecycle policies to maintain — Logchef and ClickHouse
  ship as ordinary services with none of a JVM-based search engine's tuning
  knobs.
- **One binary for the query layer.** Logchef itself is a single binary with
  no external dependency beyond the datasource, versus Kibana's tighter
  coupling to a specific Elasticsearch version and cluster topology.

## What you lose or should reconsider

Be honest with yourself about these before migrating a workload that depends
on them:

- **Relevance-ranked full-text search.** Elasticsearch's BM25 scoring ranks
  matches by relevance. ClickHouse log queries in Logchef do substring/token
  matching (`~`, `positionCaseInsensitive`, `tokenbf_v1` skip indexes) — fast
  filtering, not ranked search. If you're running a search product on top of
  your log index, not just log analytics, Elasticsearch is still the better
  fit.
- **Index Lifecycle Management.** ILM's automated hot→warm→cold→frozen tiering
  is a mature, built-in cost lever. ClickHouse gives you TTL-based deletion and
  manual storage-tiering (disks/volumes), but you're assembling it yourself
  rather than configuring a policy.
- **The broader Elastic Stack.** APM, Security/SIEM, ML-based anomaly
  detection, and semantic/vector search are integrated products built on
  Elasticsearch. Logchef is scoped to log analytics — it doesn't replace those
  products, and if you rely on them heavily, plan to keep Elasticsearch
  running alongside for that traffic rather than migrating it wholesale.
- **Dynamic mapping.** Elasticsearch infers a mapping from whatever JSON you
  send it. ClickHouse wants you to decide the schema (or route the unknown
  parts into a `Map`/`JSON` column) up front — see [Schema
  Design](/integration/schema-design) for how to do that without over-committing
  to columns you don't need yet.

## Next steps

- [Schema Design](/integration/schema-design) for the ClickHouse table shape
- [Shipping Logs with Vector](/integration/vector) for the ingestion pipeline
- [Search Syntax](/guide/search-syntax) and [Query Examples](/guide/examples) for the full LogchefQL reference
- [Using VictoriaLogs with Logchef](/tutorials/victorialogs) if you'd rather target VictoriaLogs than ClickHouse
