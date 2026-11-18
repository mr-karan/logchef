---
title: Logchef vs Datadog Log Management
description: "Logchef as a self-hosted, open source Datadog alternative for logs: what it replaces in Datadog Log Management, what it does not, and how billing works."
---

Logchef is a self-hosted, open source alternative to one part of Datadog: log
search, log dashboards, and log alerting. It is not a Datadog replacement.
Datadog is a SaaS observability platform that collects, processes, and stores
your logs and ties them to traces, metrics, and security products. Logchef is
a query UI over a ClickHouse, VictoriaLogs, or Grafana Loki backend that you
run yourself.

## The short version

Pick Datadog if you want one vendor to collect, parse, store, and correlate
logs with APM and infrastructure monitoring, and you do not want to operate a
log database. That is a real product advantage, and Logchef does not match it.

Pick Logchef if you already run ClickHouse, VictoriaLogs, or Loki (or are
willing to), you want every stored log to be searchable, and you want logs, queries,
and access rules to stay on your own infrastructure. You take on the work
Datadog does for you: shipping logs, sizing the database, and setting
retention.

You can keep Datadog for APM and infrastructure and move only logs. See
[Using them together](#using-them-together).

This page was checked on 2026-11-18 against Datadog's public docs and pricing
page, and the Logchef repository.

## At a glance

| | Logchef | Datadog Log Management |
|---|---|---|
| What it is | A log search, dashboard, and alerting UI over your own ClickHouse, VictoriaLogs, or Loki | The log product of a SaaS observability platform |
| License | AGPLv3 | Proprietary service. The [Datadog Agent](https://github.com/DataDog/datadog-agent) is Apache 2.0 |
| Storage | Your ClickHouse tables, VictoriaLogs instances, or [Loki](/tutorials/loki/) tenants | Datadog-hosted Standard and [Flex](https://docs.datadoghq.com/logs/log_configuration/flex_logs/) tiers, plus [archives](https://docs.datadoghq.com/logs/log_configuration/archives/) in your own S3, Azure Storage, or GCS bucket |
| Ingestion | None. You run a collector such as [Vector](/integration/vector/) or the [OpenTelemetry Collector](/integration/otel-collector/) | Included: collection from hosts, containers, and cloud providers, then pipelines and processors |
| Query languages | [LogchefQL](/guide/search-syntax/), ClickHouse SQL, [LogsQL](/guide/logsql-queries/), [LogQL](/guide/logql-queries/) | Datadog's [log search syntax](https://docs.datadoghq.com/logs/explorer/search_syntax/) |
| Signals | Logs | Logs connected to traces and metrics, and Cloud SIEM on the same logs |
| Deployment | One binary plus your log backend | SaaS, with Agents on your hosts |
| Access control | OIDC or local login. [Teams scope which sources a user can query](/core/user-management/). An [audit log](/operations/audit-log/) for admins | Roles and permissions. A [restriction query](https://docs.datadoghq.com/logs/guide/logs-rbac/) limits which logs a role can read |
| Alerting | [Scheduled rules](/features/alerting/) in LogchefQL, SQL, LogsQL, or LogQL, with email, webhook, Slack, and PagerDuty destinations | [Log monitors](https://docs.datadoghq.com/monitors/types/log/) over indexed logs, with notifications to Slack, Jira, webhooks, Microsoft Teams, PagerDuty, and email |
| Dashboards | [Log panels](/features/dashboards/) over your log sources, with [scheduled email reports](/features/scheduled-reports/) | Datadog dashboards across the platform's products |
| AI and MCP | An [AI query assistant](/features/ai-sql-generation/) that uses your own model provider, and a built-in read-only [MCP endpoint](/integration/mcp-server/) | A remote [Datadog MCP Server](https://docs.datadoghq.com/bits_ai/mcp_server/) with a `search_datadog_logs` tool |
| Pricing | No license fee. You pay for the infrastructure you run | Per GB ingested, plus per million events indexed by retention. See [Pricing](#pricing) |

## How they differ

### You run it, or Datadog runs it

Datadog Log Management covers the whole path. Per its
[docs](https://docs.datadoghq.com/logs/), it ingests logs from hosts,
containers, and cloud providers, enriches them with pipelines and processors,
stores them, and connects them to traces and metrics.

Logchef covers the last step only. It [does not ingest logs](/core/architecture/).
A collector writes logs into ClickHouse, VictoriaLogs, or Loki, and Logchef
reads them. You own the collector configuration, the
[table schema](/integration/schema-design/), the retention setting, and the
database upgrades. You can keep the logs, saved queries, and UI inside your network. Optional
external AI providers and notification destinations receive the data you send to them.

### Pricing

Datadog lists ingestion, Standard indexing, and Flex pricing on its
[pricing page](https://www.datadoghq.com/pricing/?product=log-management) on
2026-11-18:

- Ingest starts at $0.10 per GB of uncompressed data per month, billed
  annually or on demand. This covers processing, live tail, and forwarding to
  your own archive bucket.
- Standard indexing costs $1.70 per million log events per month at 15-day
  retention when billed annually, or $2.55 on demand.
- Flex Logs is metered per million events stored, with each million billed
  for at least 30 days at a minimum of $0.05. Flex compute is sized
  separately.
- Rehydrating archived logs costs $0.10 per compressed GB scanned, plus
  indexing for the events it brings back.
- Forwarding logs to other destinations, such as an external SIEM, starts at
  $0.25 per uncompressed GB per destination.

Check the pricing page for current numbers before you compare.

Logchef has no license fee, no per-GB fee, and no per-user fee. Your cost is
the backend infrastructure and the engineering time to run it. This page makes no claim about which is cheaper for you. That depends on
your volume, your retention, and the cost of that time. The structural
difference is the unit: Datadog charges per event and per GB, and a
self-hosted backend costs what its disks and CPUs cost.

### Which logs you can search and alert on

Datadog separates ingestion from indexing and calls this
[Logging without Limits](https://docs.datadoghq.com/logs/guide/getting-started-lwl/).
You ingest everything and index a subset. Logs that an exclusion filter drops
from an index remain available in Live Tail and can feed log-based metrics or configured
archives. They are excluded from indexed search. Three limits follow:

- Log monitors evaluate indexed logs only, and their rolling window is at
  most 2 days, per the
  [log monitor docs](https://docs.datadoghq.com/monitors/types/log/).
- [Monitors cannot query the Flex tier](https://docs.datadoghq.com/logs/log_configuration/flex_logs/).
- Archived logs need Rehydration or Archive Search before you can query
  them, per the
  [archive docs](https://docs.datadoghq.com/logs/log_configuration/archives/).

With Logchef there are no tiers in the UI. Searches and [alert rules](/features/alerting/) query the configured source,
subject to access controls, time ranges, and query limits.
Retention is the TTL of your ClickHouse table or the retention setting of
VictoriaLogs or Loki. The trade-off is direct. Datadog lets you cut cost by indexing less.
With your own backend you cut cost by storing less or by adding cheaper disks.

### Search syntax or SQL

Datadog's search syntax uses `@attribute:value` terms, tags, and boolean
operators. [Numeric range queries](https://docs.datadoghq.com/logs/explorer/search_syntax/#numerical-values) need the attribute to be a facet first.

Logchef has two levels. [LogchefQL](/guide/search-syntax/) is a short filter
syntax, such as `status>=500 and service="checkout"`, with
[aggregation pipes](/guide/aggregations/) for counts and percentiles. Native
mode accepts a single ClickHouse `SELECT`, including joins, CTEs, and window
functions. Logchef parses the SQL, expands time macros, and applies result limits.

### One slice of a platform

Datadog's log product is tied to the rest of the platform: traces, metrics,
and Cloud SIEM, which runs on the same ingested logs. Logchef has no traces
UI, no APM, no metrics store, and no session replay. A ClickHouse table can
keep a `trace_id` column and you can filter on it, but Logchef will not open
the trace.

## Using them together

You can keep Datadog for APM and infrastructure and send logs to both places.

- Vector's
  [`datadog_agent` source](https://vector.dev/docs/reference/configuration/sources/datadog_agent/)
  receives logs from a Datadog Agent over HTTP. Add a ClickHouse or
  VictoriaLogs sink as described in
  [Shipping Logs with Vector](/integration/vector/), and your hosts keep the
  Agent they already run.
- Index a small, high-value subset in Datadog for correlation with traces.
  Keep the full stream in ClickHouse and search it with Logchef.
- Datadog archives are compressed JSON files in a bucket you own, so your
  log history is not locked in.

## When Datadog is the better choice

- You want logs, traces, and infrastructure metrics correlated in one
  product. Logchef does logs only.
- You do not want to operate a log backend or a collector fleet.
- You rely on Datadog's integrations and processing pipelines to parse logs
  from many sources.
- You need Cloud SIEM or other security products on the same log stream.
- You need alert notifications to Jira or Microsoft Teams as built-in
  targets. Logchef has email, webhook, Slack, and PagerDuty destinations.
- You need log data to stay in your cloud account but want Datadog's UI.
  [BYOC Logs](https://docs.datadoghq.com/cloudprem/introduction/) stores logs
  in your object storage and serves queries from the Datadog UI.

## When Logchef is the better choice

- You already run ClickHouse, VictoriaLogs, or Loki, or your team can.
- You want to search and alert on every stored log, without index and
  exclusion decisions.
- The whole stack must be self-hosted, UI and metadata included. Logchef is
  one binary with a SQLite or
  [Postgres](/operations/database-backends/) metadata store.
- You want SQL over your logs.
- You want open source software, with
  [team-scoped access](/core/user-management/), a
  [CLI](/integration/cli/), and an [MCP endpoint](/integration/mcp-server/)
  in the same release.

## Migrating from Datadog

There is no Datadog-specific migration guide yet. The path has three steps:

1. Pick a backend and create a table. Start from the
   [schema design guide](/integration/schema-design/). Datadog's reserved
   attributes, such as `service` and `status`, map to columns.
2. Ship logs to the backend with [Vector](/integration/vector/) or the
   [OpenTelemetry Collector](/integration/otel-collector/).
3. Install Logchef with the [quickstart](/getting-started/quickstart/) and add
   the table as a source.

Simple filters translate directly. Datadog's `service:checkout status:error` becomes
`service="checkout" and status="error"` in LogchefQL. A range such as
`@http.status_code:[500 TO 599]` becomes
`http.status_code>=500 and http.status_code<=599`, because LogchefQL reads
nested keys with [dot notation](/guide/search-syntax/#nested-field-access).

## FAQ

### Is Logchef an open source Datadog alternative?

For log search, log dashboards, and log alerting, yes. For APM, infrastructure
monitoring, or security products, no. Logchef also needs a ClickHouse,
VictoriaLogs, or Loki backend that you run.

### Can I self-host Datadog Log Management?

Not the whole product. Datadog's BYOC Logs runs log storage in your own
infrastructure, and the UI stays in Datadog's SaaS. Logchef and its backends
run entirely on your side.

### Does Logchef replace the Datadog Agent?

No. Logchef does not collect logs. Use Vector, the OpenTelemetry Collector,
or keep the Datadog Agent and route its logs through Vector.

### Can I keep Datadog APM and move only logs?

Yes. You lose the in-app jump between a trace and its logs. Keep the trace ID
as a column so you can still filter logs by it in Logchef.

### How is Datadog log pricing structured?

In two parts: a per-GB ingest fee and a per-million-events indexing fee that
depends on retention. Flex Logs adds storage and compute fees for longer
retention. Logchef has no usage-based fee.

### Does Logchef have log tiers like Flex Logs or archives?

No. Logchef reads what the backend holds. Retention and storage tiering are
settings of that backend, not of Logchef.

To try it, start with the [quickstart](/getting-started/quickstart/).
