---
title: Logchef vs Grafana for ClickHouse and VictoriaLogs logs
description: "Logchef vs Grafana with the ClickHouse and VictoriaLogs data source plugins: what a log-first UI adds, where Grafana fits better, and how to run both."
---

"Why not use Grafana with the ClickHouse data source plugin?" is a fair
question, and for many teams Grafana is the right answer. Grafana reads logs
from ClickHouse and VictoriaLogs through two maintained plugins, next to your
metrics and traces. Logchef is a narrower tool: a log workspace over the same
tables, with a short search language, a field sidebar, and per-team access to
sources in the open source release.

Logs in Loki get the same answer. Grafana includes a
[built-in Loki data source](https://grafana.com/docs/grafana/latest/datasources/loki/),
and Logchef reads the same Loki as a [source](/tutorials/loki/). See
[Logchef vs Grafana Loki](/comparisons/logchef-vs-grafana-loki/).

## The short version

Pick Grafana if it already holds your metrics and traces, and the people who
read logs are comfortable in SQL or LogsQL. You get logs beside every other
signal, a larger panel library, and more alert contact point types.

Pick Logchef if people who do not write SQL search logs every day, if
different teams must see different log sources, or if you want the same
queries in a browser, a terminal, and an AI assistant. Logchef covers logs
only. It has no metrics store, traces UI, or APM, and it does not ingest logs.

Neither tool stores logs. Both read the ClickHouse tables, VictoriaLogs
instance, or Loki you already run, so you can run both at once.

This page was checked on 2026-11-18 against the linked Grafana and plugin
documentation and the Logchef repository.

## At a glance

| | Logchef | Grafana with the two plugins |
|---|---|---|
| What it is | A log workspace for ClickHouse, VictoriaLogs, and Loki: explorer, dashboards, alerts, access control | A general observability and visualization platform. ClickHouse and VictoriaLogs are two of its data sources |
| License | AGPLv3 | [Grafana](https://github.com/grafana/grafana) is AGPLv3. The [ClickHouse plugin](https://github.com/grafana/clickhouse-datasource) (Grafana Labs) and the [VictoriaLogs plugin](https://github.com/VictoriaMetrics/victorialogs-datasource) (VictoriaMetrics) are Apache 2.0 |
| Storage | None of its own. It reads your ClickHouse tables, VictoriaLogs instances, and Loki tenants | None of its own. It reads the same backends |
| Ingestion | None. Your collectors write to the backend | None. Your collectors write to the backend |
| Query languages | [LogchefQL](/guide/search-syntax/), ClickHouse SQL, [LogsQL](/guide/logsql-queries/), [LogQL](/guide/logql-queries/) | ClickHouse SQL in a SQL editor or a visual query builder. LogsQL in a code editor or a builder |
| Signals | Logs | Logs, metrics, and traces from the ClickHouse plugin, plus every other Grafana data source |
| Deployment | One binary with SQLite, or [Postgres](/operations/database-backends/) for several replicas | A Grafana server plus the two plugins |
| Access control | OIDC or local login. [Teams scope which sources a user can query](/core/user-management/). An [audit log](/operations/audit-log/) for admins | Any user in an org can query that org's data sources. Per-data-source permissions are in Grafana Enterprise and Grafana Cloud |
| Alerting | [Scheduled rules](/features/alerting/) with email, webhook, Slack, and PagerDuty destinations | Grafana Alerting: alert rules and recording rules over data source queries, with contact points such as Slack, PagerDuty, Opsgenie, email, and webhook |
| Dashboards | [Time series, stat, breakdown, and table panels](/features/dashboards/) over log sources, with [scheduled email reports](/features/scheduled-reports/) | Grafana's full panel library, template variables, and annotations, across all data sources |
| Live tail | [All three backends](/user-guide/query-interface/#live-tail). It polls ClickHouse and Loki and streams VictoriaLogs | The VictoriaLogs plugin shows live-streaming logs. The ClickHouse plugin's manifest does not declare streaming |
| AI and MCP | A built-in [AI query assistant](/features/ai-sql-generation/) and a built-in read-only [MCP endpoint](/integration/mcp-server/) | A separate [MCP server](https://github.com/grafana/mcp-grafana) (Apache 2.0). Its SQL tools, which cover ClickHouse, are off by default |
| Pricing | Free and self-hosted | Grafana OSS is free and self-hosted. Grafana Enterprise and Grafana Cloud are commercial |

Sources for the Grafana column: the
[ClickHouse data source docs](https://grafana.com/docs/plugins/grafana-clickhouse-datasource/latest/),
the [VictoriaLogs data source docs](https://docs.victoriametrics.com/victorialogs/integrations/grafana/),
[data source management](https://grafana.com/docs/grafana/latest/administration/data-source-management/),
[contact points](https://grafana.com/docs/grafana/latest/alerting/configure-notifications/manage-contact-points/),
and each plugin's `plugin.json`
([ClickHouse](https://github.com/grafana/clickhouse-datasource/blob/main/src/plugin.json),
[VictoriaLogs](https://github.com/VictoriaMetrics/victorialogs-datasource/blob/main/src/plugin.json)).
The table does not cover Grafana's Loki data source.

## How they differ

### A search language instead of SQL

In Grafana you query ClickHouse with a visual builder or a SQL editor. The
[query editor docs](https://grafana.com/docs/plugins/grafana-clickhouse-datasource/latest/query-editor/)
say a SQL query for the Logs panel must return a time column and one or more
string columns, and they recommend the aliases `timestamp`, `body`, and
`level`. Time ranges come from macros such as `$__timeFilter`.

In Logchef the everyday query is a [LogchefQL](/guide/search-syntax/) filter:

```text
level="error" and service="payments" and message~"timeout"
```

The same text compiles to SQL on a ClickHouse source, to LogsQL on a
VictoriaLogs source, and to LogQL on a Loki source. An
[aggregation pipe](/guide/aggregations/) such as `| count() by service` covers
counts, sums, and percentiles on ClickHouse and VictoriaLogs. For joins, CTEs,
LogsQL pipes, and LogQL metric queries you switch the editor to
[native mode](/user-guide/query-interface/). The same LogchefQL filter also
works in an alert condition, a dashboard panel, the CLI, and the MCP endpoint.

If your team writes ClickHouse SQL without friction, this difference is small.
If people must search logs without learning your table schema, it is the main
reason to add Logchef.

### Field discovery

Logchef's [field sidebar](/features/field-sidebar/) lists the fields of the
current source with their top values and counts. A click adds a filter and
Alt-click excludes a value. The values follow the selected time range and the
active query.

Grafana is closer on VictoriaLogs than on ClickHouse. The VictoriaLogs plugin
has a Drilldown view (since v0.33.0) that breaks logs down by field values,
stream fields, and message patterns without LogsQL. The ClickHouse plugin
offers column filters in the query builder and ad hoc filters.

### Who can query which source

Per Grafana's
[data source management docs](https://grafana.com/docs/grafana/latest/administration/data-source-management/),
any user in an organization can query that organization's data sources. A
Viewer can run any query against a data source, not only the queries on
dashboards they can open. Restricting a data source to named users or teams
needs data source permissions, which are available in Grafana Enterprise and
Grafana Cloud. The ClickHouse plugin's
[configuration docs](https://grafana.com/docs/plugins/grafana-clickhouse-datasource/latest/configure/)
add that Grafana "does not validate or restrict SQL" and recommend a
read-only ClickHouse user.

In Logchef, [teams own access](/core/user-management/). Each source is
assigned to teams, and users see only the sources of their teams.
This model is part of the open source release. A VictoriaLogs source can also
carry an [immutable scope filter](/tutorials/victorialogs-explorer/#multi-tenant-scoping)
that Logchef applies to every request. Logchef parses raw SQL and rejects
anything that is not a single `SELECT` statement. Use a read-only database
user with both tools.

### Long time ranges on VictoriaLogs

Logchef's opt-in [windowed search](/guide/windowed-search/) splits the
selected range into windows and searches the newest first. Rows arrive as each
window completes, **Load older** continues the same search, and a slow window
can be skipped and retried.

The VictoriaLogs plugin loads the histogram incrementally, with pause, resume,
and stop controls, and limits each query to 1,000 lines by default and 10,000
at most.

### Saved queries, the terminal, and AI assistants

Logchef's [collections](/features/collections/) are shared lists of saved
queries with owner, editor, and member roles. A collection never grants
access to the logs behind a query.
[Saved views](/features/saved-views/) keep a named column layout, filter, and
time range for a source. Grafana has saved queries too. The
[v13.2 release notes](https://grafana.com/docs/grafana/latest/whatsnew/whats-new-in-v13/whats-new-in-v13-2/)
list them as generally available in Grafana Enterprise and Grafana Cloud.

The [Logchef CLI](/integration/cli/) runs LogchefQL, SQL, LogsQL, and LogQL
from a terminal. The
[MCP endpoint](/integration/mcp-server/) is part of the server. An assistant
signs in with OAuth, acts as you, and can only read the sources of your teams.

## Using them together

Both tools read the same backend, so adding one does not disturb the other.
One way to split the work:

- Grafana holds metrics, traces, and the wall dashboards that mix signals.
- Logchef handles log search during an incident, saved queries, and log alerts.
- Grafana monitors Logchef. Logchef exposes
  [Prometheus metrics](/operations/metrics/), and the repository ships a
  Grafana dashboard for them in
  [`dashboards/`](https://github.com/mr-karan/logchef/tree/main/dashboards).

## When Grafana is the better choice

- You already run Grafana for metrics and traces and want logs beside them.
  The VictoriaLogs plugin supports trace-to-logs links from Tempo, Jaeger, and
  Zipkin, and the ClickHouse plugin has a Traces query type. Logchef has no
  traces UI.
- You need dashboards that mix logs with Prometheus metrics or other data
  sources. Logchef panels read only ClickHouse, VictoriaLogs, and Loki log
  sources.
- You need built-in contact points such as Opsgenie. Logchef has email,
  webhook, Slack, and PagerDuty destinations.
- You want a hosted service. Grafana Cloud runs Grafana for you. Logchef is
  self-hosted only.
- Your team is fluent in SQL and LogsQL.

## When Logchef is the better choice

- People outside the platform team search logs daily, and you do not want
  them to learn the table schema first.
- Teams must see different sources, and you run open source software only.
- You use more than one of ClickHouse, VictoriaLogs, and Loki and want one
  filter syntax for all of them.
- You want log queries from a terminal or an AI assistant under the same
  access rules as the web UI.
- You want one binary to deploy, with teams and sources managed from a
  [TOML file](/getting-started/provisioning/).

## Migrating from Grafana

No data moves, because both tools read the same tables. Follow the
[quickstart](/getting-started/quickstart/) and add your ClickHouse table,
VictoriaLogs URL, or Loki URL as a source. A ClickHouse source needs a
timestamp column and no other [schema](/integration/schema-design/).

To reuse a Grafana SQL panel, paste the query into SQL mode and replace
`$__timeFilter(column)` with Logchef's `{{__time_filter}}`
[time macro](/guide/time-macros/). LogQL from a Grafana panel runs in the
LogQL tab once Grafana variables such as `$__interval` become
[Logchef's macros](/guide/logql-queries/#variables-and-macros). If you are
leaving Loki, read [Migrate from Loki](/migrate/from-loki/).

## FAQ

### Is Logchef a Grafana alternative?

Only for logs stored in ClickHouse, VictoriaLogs, or Loki. Logchef does not
replace Grafana for metrics, traces, or dashboards over other data sources.

### Can Grafana and Logchef use the same ClickHouse tables?

Yes. Both only read. Give each tool its own read-only database user.

### Does the Grafana ClickHouse plugin support logs?

Yes. It has a Logs query type, a query builder with an OpenTelemetry preset,
a log volume histogram in Explore, and alerting on ClickHouse queries.

### Can Grafana OSS limit which teams query a data source?

Not per data source. Grafana's docs list data source permissions as a Grafana
Enterprise and Grafana Cloud feature.

### Does Logchef query Loki or Prometheus?

Loki, yes: add Loki 3.0 or later as a source and query it with LogQL. See
[Logchef vs Grafana Loki](/comparisons/logchef-vs-grafana-loki/). Prometheus,
no.

To try Logchef next to your Grafana, start with the
[quickstart](/getting-started/quickstart/).
