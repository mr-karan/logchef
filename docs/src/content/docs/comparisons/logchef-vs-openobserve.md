---
title: Logchef vs OpenObserve
description: "Logchef vs OpenObserve (O2): an open-source alternative for logs already in ClickHouse, VictoriaLogs or Loki. Storage, SSO, RBAC and query languages."
---

OpenObserve and Logchef are both AGPL-licensed and both self-hostable, but they
are different kinds of software. OpenObserve (O2) is a full observability
backend: it ingests logs, metrics and traces and stores them itself as Parquet
files. Logchef stores application metadata, but no logs. It is a log UI over the
ClickHouse, VictoriaLogs or Grafana Loki that you already run. Choose on that line first, then on the details below.

## The short version

Pick **OpenObserve** if you are starting fresh and want one system that takes
in logs, metrics, traces and frontend monitoring, keeps them on object storage,
and gives you a UI for all of it.

Pick **Logchef** if your logs already land in ClickHouse, VictoriaLogs or Loki,
or you want them there, and you need search, dashboards, alerts, SSO and
per-team access control over that store without adding a second one.

## At a glance

| | Logchef | OpenObserve |
|---|---|---|
| What it is | Query UI and control plane over log storage you already run | Observability platform that ingests, stores and queries telemetry ([README](https://github.com/openobserve/openobserve)) |
| License | AGPLv3 | Open source edition: [AGPLv3](https://github.com/openobserve/openobserve/blob/main/LICENSE). Enterprise edition: commercial EULA ([editions](https://openobserve.ai/downloads/)) |
| Storage | None of its own. Reads ClickHouse tables, VictoriaLogs and [Loki](/tutorials/loki/) | Its own Parquet files on local disk or object storage such as S3, GCS, MinIO or Azure Blob ([architecture](https://openobserve.ai/docs/architecture/)) |
| Ingestion | None. Your collectors write to the backend | Owns it: `_json`, `_multi` and `_bulk` HTTP APIs, OpenTelemetry Collector, Vector, Fluent Bit, Filebeat, syslog and more ([ingestion docs](https://openobserve.ai/docs/ingestion/)) |
| Query languages | [LogchefQL](/guide/search-syntax/), native ClickHouse SQL, native [VictoriaLogs LogsQL](/guide/logsql-queries/), native [Loki LogQL](/guide/logql-queries/) | SQL for logs and traces, SQL or PromQL for metrics, VRL in pipelines |
| Signals | Logs only | Logs, metrics, traces, real user monitoring, session replay |
| Deployment | One Go binary, SQLite or [Postgres](/operations/database-backends/) for metadata, plus your backend | One binary with SQLite and local disk. HA mode needs Kubernetes, object storage, PostgreSQL and NATS |
| Auth and RBAC | [OIDC SSO, local auth, team-scoped source access](/core/user-management/) and an [audit log](/operations/audit-log/) in the open-source release | Open source: no SSO, and all users have full access. SSO and RBAC are Enterprise features |
| Alerting | [Scheduled alerts](/features/alerting/) with email, webhook, Slack and PagerDuty destinations | Scheduled, real-time and composite alerts, email and webhook destinations ([alerts docs](https://openobserve.ai/docs/user-guide/alerts/alerts/)) |
| Dashboards | [Built in](/features/dashboards/): time series, stat, breakdown, table. [Scheduled email reports](/features/scheduled-reports/) | Built in, [19 prebuilt chart types plus custom charts](https://openobserve.ai/downloads/) |
| AI and MCP | [AI query generation](/features/ai-sql-generation/), [MCP server](/integration/mcp-server/) with OAuth sign-in | AI assistant, [MCP server](https://openobserve.ai/docs/integration/ai/mcp/) in all editions |
| Pricing | Free. You pay for the infrastructure you run | Open source is free. Enterprise is free up to 50 GB of ingestion per day. Cloud lists per-GB ingestion pricing ([pricing](https://openobserve.ai/pricing/)) |

OpenObserve facts are from its docs and repository as of November 2026.

## How they differ

### A backend versus a UI over your database

OpenObserve owns the whole path. Collectors send data to its ingestion APIs,
ingesters convert it to Parquet, and queriers read those files back. Per its
[architecture docs](https://openobserve.ai/docs/architecture/), the default is
a single node with SQLite and local disk. The HA mode stores all Parquet files
in object storage, keeps metadata in PostgreSQL, uses NATS as the cluster
coordinator, and does not support local disk storage.

Logchef owns none of that. It connects to a ClickHouse table, a VictoriaLogs
instance or a Loki tenant as a [source](/core/user-management/#sources),
discovers the fields, and queries it. Beyond a timestamp column it needs no particular
[schema](/integration/schema-design/). Your data stays in a database that other
tools can read at the same time: `clickhouse-client`, Grafana, a BI tool, or a
batch job.

The practical result: OpenObserve is less to assemble when you have nothing.
Logchef is less to change when you already have ClickHouse, VictoriaLogs or
Loki, because nothing gets re-ingested.

### Signals covered

OpenObserve covers logs, metrics, traces, real user monitoring and session
replay in one UI, per its README. If you want to jump from a slow trace to its
logs, it does that.

Logchef does logs. It has no traces UI, no APM, no metrics store and no session
replay, and it does not ingest anything. A ClickHouse table can carry `trace_id`
and `span_id` columns and you can filter on them, but that is filtering, not
tracing.

### Access control in the open-source edition

This is the sharpest difference between the two free editions.

OpenObserve's [edition comparison](https://openobserve.ai/downloads/) lists
single sign-on as not available in the open source edition. Its
[RBAC docs](https://openobserve.ai/docs/user-guide/account-administration/identity-and-access-management/role-based-access-control/)
say: "RBAC is not supported. All users have unrestricted access to all
features." Both are in the Enterprise edition, which is free up to 50 GB of
ingestion per day under a commercial license. The comparison page marks SSO
and RBAC in Enterprise as requiring HA mode. The RBAC docs also list
OpenObserve Cloud.

Logchef's full access model is in the AGPL release. Users sign in with
[OIDC or local accounts](/core/user-management/). Sources are assigned to
teams, and a user can query only the sources of their teams. Automation gets
[scoped service tokens](/features/service-tokens/), and teams, sources and
memberships can be declared in a [TOML file](/getting-started/provisioning/)
kept in Git. An [audit log](/operations/audit-log/) records configuration
changes, sign-ins and data exports for global admins.

### Query languages

Both tools speak SQL, which sets them apart from most log products.

OpenObserve uses SQL for logs and traces, and SQL or PromQL for metrics. Its
README makes a point of having no proprietary query language. VRL functions
transform data in pipelines.

On a ClickHouse source, Logchef accepts a single `SELECT` statement, including
functions and joins against other tables. It parses the SQL, expands
[time macros](/guide/time-macros/), and applies result limits. VictoriaLogs
sources take native LogsQL, and Loki sources take native LogQL. On top of all
three sits [LogchefQL](/guide/search-syntax/), a short filter syntax that
compiles to each backend's language. Its
[aggregations](/guide/aggregations/) work on ClickHouse and VictoriaLogs.

### Storage cost model

OpenObserve is built around Parquet on object storage, and its README credits
that design for its storage cost. If cheap, long retention on S3 is your main
requirement and you have no existing log database, that is a strong reason to
choose it.

With Logchef, storage cost is whatever your backend costs. You size and
operate that backend yourself. Logchef adds no storage of its
own beyond a small metadata database.

## When OpenObserve is the better choice

- You want **logs, metrics and traces in one product**, with correlation
  between them.
- You need **real user monitoring or session replay**.
- You have **no log database yet** and want ingestion, storage and UI from one
  binary.
- You want **object storage as the primary store** from day one.
- You want **ingest-time pipelines** that parse, enrich or route data with VRL.
- You want **real-time alerts** evaluated at ingestion. Logchef alerts are
  scheduled queries.

## When Logchef is the better choice

- Your logs are **already in ClickHouse, VictoriaLogs or Loki** and you do not
  want to copy them into a second system.
- You need **SSO, per-team access control and an audit log** in a fully
  open-source deployment.
- You want **native ClickHouse SQL** over your own tables, including joins with
  non-log data.
- You run several backends and want **one UI across them**, with
  [dashboards](/features/dashboards/) that mix ClickHouse, VictoriaLogs and
  Loki panels.
- You want a terminal workflow: the [CLI](/integration/cli/) runs the same
  queries with the same permissions, and the
  [command palette](/features/command-palette/) runs a search in any source.

## Migrating from OpenObserve

There is no dedicated migration guide, and Logchef has no importer for
OpenObserve's Parquet files. The practical path is to re-point ingestion and
let old data age out:

1. Create a log table using the [schema design guide](/integration/schema-design/),
   or stand up [VictoriaLogs](/tutorials/victorialogs/).
2. Send the same stream there. [Vector](/integration/vector/) and the
   [OpenTelemetry Collector](/integration/otel-collector/) are both collectors
   that OpenObserve also accepts, so a second sink is usually a small config
   change.
3. Follow the [quickstart](/getting-started/quickstart/), add the source, and
   run both side by side until you trust the new path.

Metrics, traces and RUM need a home of their own. Logchef will not take them.

## FAQ

### Is Logchef an OpenObserve alternative?

For logs, yes, if you run ClickHouse, VictoriaLogs or Loki. For metrics,
traces or RUM, no.

### Can Logchef read OpenObserve data?

No. OpenObserve stores data in its own Parquet layout. Logchef reads
ClickHouse, VictoriaLogs and Loki.

### Are both really open source?

Both publish their core under AGPLv3. OpenObserve also ships an Enterprise
edition under a commercial license that adds SSO, RBAC, audit trail and
federated search. Logchef has one edition, and its SSO, team access and audit
log are part of it.

### Does OpenObserve open source include SSO or RBAC?

Not per its own docs as of November 2026. They are Enterprise features.
Logchef includes OIDC SSO and team-scoped access in its open-source
release.

### Which one is a single binary?

Both, with a difference. OpenObserve's binary includes storage. Logchef's
binary is the query layer only, and it needs a ClickHouse, VictoriaLogs or
Loki backend to read from.

### Do both have an MCP server?

Yes. OpenObserve documents an MCP server in all editions. Logchef's
[MCP server](/integration/mcp-server/) is built into the main binary, signs in
with OAuth, is read-only, and sees only the teams of the user who approved it.

Ready to try it? Start with the [quickstart](/getting-started/quickstart/).
