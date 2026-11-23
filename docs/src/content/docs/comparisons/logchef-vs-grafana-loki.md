---
title: Logchef vs Grafana Loki
head:
  - tag: title
    content: "Loki vs ClickHouse and VictoriaLogs: Logchef vs Grafana Loki"
description: "Loki vs ClickHouse for logs: label-indexed chunks vs columnar search. Run Logchef as a UI over your Loki, or move logs to ClickHouse or VictoriaLogs."
---

Grafana Loki is a log storage backend. It indexes labels, keeps log lines in object storage, and you query it with LogQL, usually from Grafana. Logchef is a query UI that stores no logs. It reads ClickHouse, VictoriaLogs, or Loki, so there are two ways to use it if you run Loki:

1. **Keep Loki.** Add it to Logchef as a source. Logchef is then a log UI over the Loki you run, next to Grafana or in place of its log views. No data moves.
2. **Replace Loki.** Send logs to ClickHouse or VictoriaLogs and put Logchef on top.

Competitor facts on this page come from Grafana's docs and the Loki repository as of November 2026. Sources are linked below.

## The short version

Stay with Loki and Grafana alone if Grafana holds your metrics and traces, your team writes LogQL, and Grafana's log views do what you need.

Keep Loki and add Logchef if you want a log workspace over it: a short filter syntax that compiles to LogQL, sign-in and team access, alerts, a CLI, and an MCP endpoint. Ingestion, retention, and storage stay as they are.

Move logs to ClickHouse or VictoriaLogs, with Logchef on top, if you filter on high-cardinality fields such as request IDs, or you want SQL over logs.

Logchef does not change how Loki searches. A query on a Loki source still selects streams by label and scans the matching chunks. If that is too slow for your queries, only the second path fixes it.

## At a glance

| | Logchef | Grafana Loki |
|---|---|---|
| What it is | A log query UI and control plane over existing log storage | "A horizontally-scalable, highly-available, multi-tenant log aggregation system" ([overview](https://grafana.com/docs/loki/latest/get-started/overview/)) |
| License | AGPLv3 | AGPLv3, with Apache 2.0 for some directories ([licensing](https://github.com/grafana/loki/blob/main/LICENSING.md)) |
| Storage | None for logs. It reads your ClickHouse tables, VictoriaLogs, or Loki tenants. Metadata in SQLite or Postgres | Compressed chunks in object storage such as S3 or GCS, plus a label index |
| Ingestion | None. Your collector writes the logs | Agents such as Grafana Alloy push streams to Loki |
| Query languages | [LogchefQL](/guide/search-syntax/), ClickHouse SQL, [VictoriaLogs LogsQL](/guide/logsql-queries/), [Loki LogQL](/guide/logql-queries/) | LogQL |
| Signals | Logs | Logs. Metrics and traces come from Mimir and Tempo |
| Deployment | One binary next to your log store | Monolithic or microservices mode ([deployment modes](https://grafana.com/docs/loki/latest/get-started/deployment-modes/)), plus object storage, an agent, and Grafana |
| Auth and access control | OIDC or local login, [teams that scope source access](/core/user-management/), [service tokens](/features/service-tokens/), an [audit log](/operations/audit-log/) | No built-in authentication. Tenants are separated by the `X-Scope-OrgID` header ([authentication](https://grafana.com/docs/loki/latest/operations/authentication/)) |
| Alerting | [Scheduled rules](/features/alerting/) with email, webhook, Slack, and PagerDuty destinations | The ruler evaluates LogQL and sends to Alertmanager, or Grafana alerting |
| Dashboards | [Built in](/features/dashboards/), with [scheduled email reports](/features/scheduled-reports/) | In Grafana |
| AI and MCP | [AI query generation](/features/ai-sql-generation/), LogQL included, and a built-in [MCP endpoint](/integration/mcp-server/) | Grafana's [MCP server](https://github.com/grafana/mcp-grafana) runs LogQL against Loki data sources |
| Pricing | Free. Self-hosted only | Open source is free. Grafana Cloud has a free tier and usage-based plans ([pricing](https://grafana.com/pricing/)) |

## Path 1: Logchef over the Loki you run

Add a [Loki source](/tutorials/loki/) with the base URL, a tenant, and a default stream selector. Logchef reads Loki's HTTP API. It does not write to Loki and does not use Loki's ruler. Loki 3.0 is the minimum version.

On that source your team gets:

- **Search** in [LogchefQL](/guide/search-syntax/), which Logchef compiles to LogQL, or in native [LogQL](/guide/logql-queries/), which runs as written. `service_name="payments-api" and status>=500` replaces `{service_name="payments-api"} | json | status >= 500 | __error__=""`.
- The histogram, the [field sidebar](/features/field-sidebar/), live tail, and log context within a row's stream.
- [Saved queries](/features/collections/), [dashboards](/features/dashboards/), and [alerts](/features/alerting/), which run a LogQL metric query on a schedule.
- [AI query generation](/features/ai-sql-generation/) for LogQL, the [CLI](/integration/cli/), and the [MCP endpoint](/integration/mcp-server/).

What stays the same, and what is missing:

- **Search is still Loki's label-plus-scan model.** Logchef sends LogQL to Loki, so the high-cardinality and SQL points below still apply.
- **A Loki source gives a team the whole tenant.** The tenant is fixed on the source and users cannot change it, but native LogQL can name any stream in that tenant. The default stream selector is not a boundary.
- **Some features do not work on Loki:** exports, [windowed search](/guide/windowed-search/), the [schema advisor](/features/schema-advisor/), and LogchefQL aggregations. For counts and rates you write a LogQL metric query. The [Loki tutorial](/tutorials/loki/) lists every difference.

## Path 2: ClickHouse or VictoriaLogs in place of Loki

Move the data when you need indexed content search, high-cardinality fields as columns, SQL, or exports. On those stores LogchefQL also has an [aggregation pipe](/guide/aggregations/), and native mode takes ClickHouse SQL or LogsQL. The differences below are between Loki and those stores.

### Labels versus columns

Loki's overview states the design directly: "Loki does not index the contents of the logs, but only indexes metadata about your logs". That metadata is a set of labels per stream. Queries select streams by label, then scan the matching chunks for line content.

The cost shows up with high-cardinality values. Loki's [cardinality guide](https://grafana.com/docs/loki/latest/get-started/labels/cardinality/) says "the fewer labels you use, the better", sets a default limit of 15 index labels, and warns that high cardinality makes Loki "build a huge index, and to flush thousands of tiny chunks to the object store". Trace IDs and user IDs belong in [structured metadata](https://grafana.com/docs/loki/latest/get-started/labels/structured-metadata/), which Loki stores but does not index.

ClickHouse has no split between labels and content. Every field is a column, and a column can have a bloom filter or token index. Logchef's [built-in schema](/integration/schema-design/) indexes `trace_id`, the attribute map, and the log body. VictoriaLogs discovers fields from the events it receives.

### Parsing at query time versus at ingest

LogQL parsers such as `json` and `logfmt` extract fields from each line while the query runs. That keeps ingestion simple, and you can change how you parse without rewriting data. With ClickHouse, your collector parses once and writes typed columns, and a schema change means a migration.

### What you operate

A Loki deployment includes Loki, object storage, an agent, and Grafana for the UI. Monolithic mode suits "up to approximately 20GB per day" per the deployment docs. The simple scalable mode "is being deprecated and will be removed with the Loki 4.0 release", which leaves monolithic and microservices. Promtail [reached end of life on March 2, 2026](https://grafana.com/docs/loki/latest/send-data/promtail/), and Grafana directs users to Alloy.

On path 1 you add one Logchef binary to that deployment. On path 2 you run ClickHouse or VictoriaLogs in place of Loki. ClickHouse is a database you have to operate, so this is a different kind of work, not less work in every case.

### Access control

Loki "does not come with any included authentication layer". Its docs tell you to run an authenticating reverse proxy in front of it. User accounts live in that proxy or in Grafana.

Logchef handles sign-in itself through OIDC or local accounts, and a team grants access to specific sources. A Loki source keeps its credential and tenant on the server, so users never send `X-Scope-OrgID` themselves. Loki's own API still needs that proxy, because Logchef is only one client of it.

## Using them together

One Logchef can hold Loki, ClickHouse, and VictoriaLogs sources at once, and a [dashboard](/features/dashboards/#multiple-data-sources) can mix panels from them. That allows a split: keep cheap, high-volume logs in Loki, send the logs that need high-cardinality search or SQL to ClickHouse, and search both from Logchef.

Grafana keeps working on the same Loki, because Logchef only reads.

## When Grafana Loki is the better choice

Keep Loki as the store when:

- Your queries start from a small set of labels such as service, environment, and namespace.
- You want log data in object storage and do not want to run a database for logs.
- You want a managed service. Per [Grafana's pricing page](https://grafana.com/pricing/), the Grafana Cloud free tier is limited to 50 GB of logs ingested per month with 14 days of retention, and paid plans bill by usage.

Keep Grafana as the only log UI when:

- Grafana is your main observability UI, and you want logs next to Prometheus metrics and Tempo traces. Logchef shows logs only. See [Logchef vs Grafana](/comparisons/logchef-vs-grafana/).
- Your alert rules run in Loki's ruler and your dashboards are in Grafana. Logchef does not import either.

## When Logchef is the better choice

Over the Loki you run:

- People who do not write LogQL search logs every day.
- You want OIDC sign-in and team-scoped access in front of Loki tenants.
- You want log search in a terminal ([CLI](/integration/cli/)), from the [command palette](/features/command-palette/), or from an AI assistant through MCP.
- You manage teams and sources as code with [declarative provisioning](/getting-started/provisioning/).

With ClickHouse or VictoriaLogs in place of Loki:

- You search by request ID, user ID, or trace ID every day.
- You want SQL for ad hoc analysis of logs.
- Your logs are already in ClickHouse or VictoriaLogs.

## Migrating from Loki

You do not have to migrate to use Logchef. Start with a [Loki source](/tutorials/loki/) and decide later.

To move the data, follow the [Loki migration guide](/migrate/from-loki/). In outline:

1. Point [Vector](/integration/vector/) or the [OpenTelemetry Collector](/integration/otel-collector/) at ClickHouse or VictoriaLogs.
2. Turn stream labels into `LowCardinality` columns, and move high-cardinality fields into a map column or columns of their own.
3. Translate queries. `{app="payment-api"} |= "error"` becomes `service_name="payment-api" and body~"error"` in LogchefQL.
4. Rebuild dashboards and alert rules in Logchef.

[VictoriaLogs](/tutorials/victorialogs/) is the shorter path if you want to keep a stream-oriented store.

## FAQ

### Is Logchef a Grafana Loki alternative?

Logchef is a UI and stores no logs. Over Loki, it is an alternative to Grafana's log views. Together with ClickHouse or VictoriaLogs, it replaces Loki and those views.

### Can Logchef query Loki or run LogQL?

Yes. Add Loki 3.0 or later as a source. Native LogQL runs as written, and LogchefQL compiles to LogQL.

### Does Logchef make Loki queries faster?

No. Logchef sends LogQL to Loki, which searches as before. Indexed search on log content or high-cardinality fields needs ClickHouse or VictoriaLogs.

### Is Loki still open source?

Yes. Grafana Labs [relicensed Loki](https://grafana.com/blog/2021/04/20/grafana-loki-tempo-relicensing-to-agplv3/) from Apache 2.0 to AGPLv3 in April 2021. Logchef uses the same license.

To try Logchef on your own logs, start with the [quickstart](/getting-started/quickstart/).
