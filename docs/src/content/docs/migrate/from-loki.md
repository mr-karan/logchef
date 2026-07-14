---
title: From Loki
description: Migrate Loki and Grafana log analytics to ClickHouse with Logchef — pipeline re-point, label-to-column mapping, LogQL to LogchefQL/SQL examples.
---

This guide covers moving log analytics off Loki/Grafana Explore and onto
ClickHouse (or VictoriaLogs) with Logchef as the query UI. It's scoped to
Loki's role as a log store — it doesn't cover Grafana's role as a general
dashboarding tool for Prometheus/metrics, which Logchef doesn't replace.

## What actually moves

A typical Loki pipeline looks like:

```
Promtail / Alloy  →  Loki  →  Grafana Explore
```

Moving to Logchef changes the last two stages:

```
Alloy / Vector / OTel Collector  →  ClickHouse  →  Logchef
```

Promtail collection agent reached end-of-life on March 2, 2026 in favor of
[Grafana Alloy](https://grafana.com/docs/loki/latest/send-data/promtail/), so
if you're still on Promtail you're likely re-pointing a shipper either way.
Alloy, Vector, Fluent Bit, and the OpenTelemetry Collector can all tail the
same sources Promtail did (files, journald, Kubernetes pod logs); only the
output changes to a ClickHouse sink instead of Loki's push API. See [Shipping
Logs with Vector](/integration/vector) for working source/sink configs.

### Shortcut: keep your shipper, target VictoriaLogs

If you want a lighter lift than re-designing a ClickHouse schema, VictoriaLogs
is closer in shape to Loki: it's a purpose-built log store rather than a
general-purpose analytical database, and it accepts logs through several
[ingestion protocols](https://docs.victoriametrics.com/victorialogs/data-ingestion/vector/)
including one Vector's `loki` sink can be adapted for. That gets you Logchef
as your query UI without redesigning around ClickHouse columns first. See
[Using VictoriaLogs with Logchef](/tutorials/victorialogs).

## The model shift: labels + chunks → columns

Loki's core design choice is to index only a small set of **labels** per
stream (a "table of contents") and store the log lines themselves as
compressed, unindexed chunks — label queries narrow down which streams to
scan, then line filters (`|=`, `!~`, …) brute-force scan the matching chunks
([Loki architecture docs](https://grafana.com/docs/loki/latest/get-started/architecture/)).
That's why Loki's own guidance caps labels at roughly 10-15 low-cardinality
values (service, environment, region) and pushes anything unbounded — request
IDs, user IDs, trace IDs — into "structured metadata" instead of labels
([Loki label cardinality guidance](https://grafana.com/docs/loki/latest/get-started/labels/cardinality/)).

ClickHouse doesn't have a labels-vs-content split — everything is a column,
and Logchef doesn't require you to flatten anything into a fixed set. The
practical mapping:

| Loki concept | ClickHouse/Logchef equivalent |
|---|---|
| Stream labels (`app`, `env`, `namespace`) | `LowCardinality(String)` columns, e.g. `service_name`, `namespace` |
| Structured metadata / high-cardinality fields (`trace_id`, `user_id`) | `log_attributes` `Map(LowCardinality(String), String)` column, or dedicated columns if you filter on them constantly |
| Unstructured log line | `body` (`String`) column |
| `\| json` / `\| logfmt` parsing at query time | Parsing done once at ingestion (in Vector/OTel), writing structured fields directly into columns or `log_attributes` |

That last row is the biggest practical difference: Loki parses `json` or
`logfmt` out of the line on every query. In a ClickHouse+Logchef setup, you
parse once during ingestion and query the already-structured field —
cheaper per query, at the cost of deciding the schema up front. See [Schema
Design](/integration/schema-design) for guidance on which fields to promote
to columns.

## Query translation: LogQL → LogchefQL / SQL

Assume your Vector/Alloy pipeline parses the log line at ingestion, writing
`service_name`, `severity_text` (from a `level` field), and a numeric
`status_code` column, with anything else left in `log_attributes`:

| LogQL | Meaning | LogchefQL | ClickHouse SQL |
|---|---|---|---|
| `{app="payment-api"} \|= "error"` | Lines from `payment-api` containing "error" | `service_name="payment-api" and body~"error"` | `WHERE service_name = 'payment-api' AND positionCaseInsensitive(body, 'error') > 0` |
| `{app="payment-api"} \| json \| level="error"` | Parse JSON, filter on extracted `level` | `service_name="payment-api" and severity_text="error"` | `WHERE service_name = 'payment-api' AND severity_text = 'error'` |
| `{job="nginx"} \| logfmt \| status_code >= 500` | Parse logfmt, filter numeric field | `service_name="nginx" and status_code>=500` | `WHERE service_name = 'nginx' AND status_code >= 500` |
| `rate({app="payment-api"}[5m])` | Requests/sec over time | — (use SQL or a dashboard panel) | `SELECT toStartOfMinute(timestamp) AS t, count() FROM logs GROUP BY t ORDER BY t` |

For fields you chose to keep unflattened in `log_attributes` rather than
promoting to columns, the same filters work with [dot
notation](/guide/search-syntax#nested-field-access):
`log_attributes.status_code>=500`.

There's no LogchefQL equivalent to a bare `rate()` metric query — that's a
job for [SQL mode](/guide/search-syntax#native-mode) or a [dashboard time
series panel](/features/dashboards), both of which replace the role Grafana
Explore's metric-query view played for Loki.

## Live tailing

Grafana Explore's live-tail view for Loki has a direct analog: Logchef's
explorer has its own [Live toggle](/tutorials/victorialogs#live-tail). On
VictoriaLogs sources it proxies the backend's native tail stream; on
ClickHouse it polls on an interval, since ClickHouse has no native log-tail
API.

## What you gain

- **Real indexing on log content.** Loki's line filters (`|=`, `!~`) scan
  decompressed chunks within the streams selected by labels — fast because
  labels narrow scope first, but the content scan itself is unindexed grep.
  ClickHouse can use bloom-filter and token-based skip indexes
  (`tokenbf_v1`, `bloom_filter`) on the body/attribute columns, so line-content
  filtering doesn't have to fall back to a full chunk scan.
- **SQL aggregations without a metric-query DSL.** `GROUP BY`, joins, and
  window functions are ordinary SQL, versus LogQL's separate metric-query
  syntax (`rate()`, `_over_time()`, `unwrap`) for turning log lines into
  numbers.
- **No runtime parsing tax.** If your logs are already structured JSON,
  parsing once at ingestion and querying typed columns avoids re-running
  `| json` or `| logfmt` on every query.

## What you lose or should reconsider

- **Loki's storage economy.** Indexing only labels and compressing the rest
  is a deliberate, cheap design. A ClickHouse table with several indexed
  columns and skip indexes will generally use more storage per log line than
  Loki's label-plus-chunk model — you're trading some storage cost for
  content-query speed.
- **The Prometheus/Grafana ecosystem.** Loki's label model mirrors
  Prometheus's, and Grafana ties metrics, logs, and alerting (via
  Alertmanager) into one UI. Logchef's dashboards mix ClickHouse and
  VictoriaLogs log panels, not general Prometheus metric panels — if you rely
  on a unified metrics+logs view in Grafana, plan to keep Grafana for metrics
  and use Logchef specifically for the log side.
- **Cardinality discipline becomes a schema decision, not a runtime one.**
  Loki lets you bolt on new labels carefully at write time and enforces
  cardinality limits centrally. In ClickHouse, the equivalent decision (which
  fields become `LowCardinality` columns vs. `Map` attributes) is made when
  you design the table, and changing it later means a schema migration rather
  than a label-policy tweak.

## Next steps

- [Schema Design](/integration/schema-design) for the ClickHouse table shape
- [Shipping Logs with Vector](/integration/vector) for the ingestion pipeline
- [Search Syntax](/guide/search-syntax) and [Query Examples](/guide/examples) for the full LogchefQL reference
- [Dashboards](/features/dashboards) for the time-series view that replaces Grafana Explore's metric queries
- [Using VictoriaLogs with Logchef](/tutorials/victorialogs) if you'd rather keep a label-oriented backend
