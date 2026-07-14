---
title: Migrate to Logchef
description: Guides for moving log analytics from Elasticsearch/Kibana or Loki/Grafana to ClickHouse or VictoriaLogs with Logchef as the query UI.
---

Logchef is a query and control-plane UI, not a log storage engine. "Migrating to
Logchef" really means two things: re-pointing your log pipeline at a storage
backend Logchef supports (ClickHouse or VictoriaLogs), and then using Logchef
to explore, dashboard, and alert on what lands there. Your existing shippers
usually need only a sink/output change, not a rewrite.

These guides walk through that move from the two most common log stacks:

<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:1rem;margin:1.5rem 0;">
  <a href="/migrate/from-elasticsearch" style="display:block;padding:1rem 1.25rem;border:1px solid var(--sl-color-hairline);border-radius:0.5rem;text-decoration:none;">
    <strong>From Elasticsearch</strong>
    <p style="margin:0.35rem 0 0;color:var(--sl-color-gray-3);">Move off Elasticsearch/Kibana: re-point Logstash/Beats/Vector at ClickHouse, map ECS fields, translate KQL to LogchefQL and SQL.</p>
  </a>
  <a href="/migrate/from-loki" style="display:block;padding:1rem 1.25rem;border:1px solid var(--sl-color-hairline);border-radius:0.5rem;text-decoration:none;">
    <strong>From Loki</strong>
    <p style="margin:0.35rem 0 0;color:var(--sl-color-gray-3);">Move off Loki/Grafana: re-point Promtail/Alloy at ClickHouse, turn labels into columns, translate LogQL to LogchefQL and SQL.</p>
  </a>
</div>

## What actually changes

- **Ingestion**: your shipper's *output* changes (to a ClickHouse sink, or an
  Elasticsearch-compatible/native endpoint for VictoriaLogs). The *input* side
  — tailing files, scraping Kubernetes pod logs, receiving syslog — usually
  doesn't need to change. See [Shipping Logs with Vector](/integration/vector).
- **Schema**: instead of a dynamic mapping (Elasticsearch) or a label set
  (Loki), you define columns up front, with a `Map` or `JSON` column for
  free-form attributes. See [Schema Design](/integration/schema-design).
- **Query language**: you write [LogchefQL](/guide/search-syntax) for everyday
  filtering (it compiles to SQL or LogsQL depending on the source), and drop
  into native SQL or LogsQL for aggregations the quick-filter syntax doesn't
  cover.
- **The UI**: log explorer, [field sidebar](/features/field-sidebar),
  [dashboards](/features/dashboards), [alerting](/features/alerting), and
  [saved queries/collections](/features/collections) replace Kibana Discover
  or Grafana Explore for logs specifically. Logchef doesn't replace Grafana
  for metrics or Kibana for full-text search products — it's scoped to log
  analytics.

## What doesn't change

Logchef has no ingestion pipeline of its own, so you keep whatever collector
you already trust (Vector, Fluent Bit, the OpenTelemetry Collector, or
Filebeat/Promtail if you'd rather keep them and just add a second output).
Nothing about how logs are produced or collected has to move on day one —
only where they land.

## Before you start

Both guides assume you're moving log storage to **ClickHouse**. If you'd
rather keep a label-oriented, VictoriaMetrics-style backend, Logchef also
supports [VictoriaLogs](/tutorials/victorialogs) as a datasource — closer in
shape to Loki, but queried through LogsQL instead of LogQL.

## Next steps

- [From Elasticsearch](/migrate/from-elasticsearch)
- [From Loki](/migrate/from-loki)
- [Schema Design](/integration/schema-design) for the ClickHouse table shape
- [Shipping Logs with Vector](/integration/vector) for the ingestion side
