---
title: Logchef vs ClickStack
description: "Logchef vs ClickStack (formerly HyperDX): a log UI for the ClickHouse, VictoriaLogs or Loki you already run, compared with ClickHouse's observability stack."
---

Logchef and ClickStack (formerly HyperDX) both put a search UI on logs stored in ClickHouse. ClickStack is a full observability stack: its own OpenTelemetry collector ingests logs, traces, metrics, and session replays. Logchef is a log-only query UI that reads logs you already store in ClickHouse, VictoriaLogs, or Grafana Loki, and it can read the log table ClickStack writes.

Competitor facts on this page come from ClickStack's docs and repository as of November 2026. Sources are linked below.

## The short version

Pick ClickStack if you want logs, traces, metrics, and session replay in one product with the ingestion pipeline included, or if you want ClickHouse to run it for you.

Pick Logchef if your logs already land in ClickHouse, VictoriaLogs, or Loki and you want a log UI with OIDC, team-scoped access, alerts, and dashboards from one binary, without adding a collector or MongoDB.

You do not have to choose. Logchef can read ClickStack's `otel_logs` table, so the two can share one ClickHouse.

## At a glance

| | Logchef | ClickStack |
|---|---|---|
| What it is | A log query UI and control plane over existing log storage | An "open-source observability platform for SREs, developers, and their AI agents, built on ClickHouse" ([overview](https://clickhouse.com/docs/clickstack/overview)) |
| License | AGPLv3 | MIT ([repository](https://github.com/hyperdxio/hyperdx)) |
| Storage | Your ClickHouse tables, VictoriaLogs, or [Loki](/tutorials/loki/). Metadata in SQLite or Postgres | ClickHouse for telemetry. MongoDB for dashboards, alerts, and users ([architecture](https://clickhouse.com/docs/clickstack/architecture)) |
| Ingestion | None. Your collector writes the logs | Bundled OpenTelemetry collector, OTLP on ports 4317 and 4318 |
| Query languages | [LogchefQL](/guide/search-syntax/), ClickHouse SQL, [VictoriaLogs LogsQL](/guide/logsql-queries/), [Loki LogQL](/guide/logql-queries/) | Lucene-style search such as `level:err`, and SQL |
| Signals | Logs | Logs, traces, metrics, session replays |
| Deployment | One binary. Docker Compose and a Helm chart are in the repository | All-in-one image, Docker Compose, Helm, UI only, browser-only local mode, or embedded in ClickHouse ([options](https://clickhouse.com/docs/clickstack/deployment/oss)) |
| Auth and access control | OIDC or local login, [teams that scope source access](/core/user-management/), [service tokens](/features/service-tokens/), an [audit log](/operations/audit-log/) | Local accounts in open source. Roles are documented for the ClickHouse Cloud service ([RBAC](https://clickhouse.com/docs/clickstack/observability/managing/rbac)) |
| Alerting | [Scheduled rules](/features/alerting/) with email, webhook, Slack, and PagerDuty destinations | [Alerts with webhook destinations](https://clickhouse.com/docs/clickstack/features/alerts). Not available in local mode or the embedded build |
| Dashboards | [Time series, stat, breakdown, and table panels](/features/dashboards/), with [scheduled email reports](/features/scheduled-reports/) | Dashboards and a visual query builder |
| AI and MCP | [AI query generation](/features/ai-sql-generation/) and a built-in [MCP endpoint](/integration/mcp-server/) with OAuth | MCP server at `/api/mcp` with a personal API key ([MCP](https://clickhouse.com/docs/clickstack/mcp)) |
| Pricing | Free. Self-hosted only | Open source is free. The managed service bills per GB ([pricing](https://clickhouse.com/pricing/observability)) |

## How they differ

### Logs only versus four signals

ClickStack's overview lists what its UI covers: "Search and correlate logs, metrics, session replays, and traces". Logchef covers logs. A ClickHouse table can carry `trace_id` and `span_id` columns and you can filter on them, but Logchef has no trace waterfall, no metrics store, and no session replay.

This is the difference that should decide most evaluations. If you need to jump from a slow trace to its logs inside one product, ClickStack does that and Logchef does not.

### Ingestion and schema

ClickStack ships a preconfigured collector with what its docs call "an opinionated schema for logs, traces, and metrics". The docs also say the UI is schema agnostic and "works on top of your existing ClickHouse schema", and the [UI-only deployment](https://clickhouse.com/docs/clickstack/deployment/hyperdx-only) leaves ingestion to you.

Logchef has [no ingestion pipeline](/core/architecture/) at all. Vector, Fluent Bit, or an OpenTelemetry Collector writes the logs, and Logchef reads them. A ClickHouse source needs one thing: a `DateTime` or `DateTime64` timestamp column. See [schema design](/integration/schema-design/) for the optional built-in schema. For a table you designed yourself, the [schema advisor](/features/schema-advisor/) reads the filters your queries use and the table metadata, and proposes indexes and column changes as DDL that you review and run.

Both tools can sit on a table you designed yourself. ClickStack's collector is the faster start when you have no pipeline yet.

### What you run

Open source ClickStack keeps application state in MongoDB, apart from the telemetry in ClickHouse. The all-in-one image bundles ClickHouse, the UI, the collector, and MongoDB, and the [all-in-one deployment docs](https://clickhouse.com/docs/clickstack/deployment/all-in-one) advise against using that image in production. The UI-only deployment still needs a MongoDB instance.

Logchef is a single binary that embeds its web UI. It stores users, teams, saved queries, and alert rules in SQLite by default, or in [Postgres](/operations/database-backends/) when you run more than one replica. It exposes [Prometheus metrics](/operations/metrics/) at `/metrics`.

### Access control

Logchef's access model is part of the open source build. Users sign in with OIDC or a local email and password. Admins add sources to teams, and a user can query only the sources of their teams. Service tokens carry their own scopes for automation. An [audit log](/operations/audit-log/) records configuration changes, sign-ins, and data exports for global admins.

In open source ClickStack you create an account on first visit, per the [getting started guide](https://clickhouse.com/docs/clickstack/getting-started/oss). ClickStack's RBAC docs describe Admin, Member, and ReadOnly roles plus custom roles for a service that "authenticates through ClickHouse Cloud". We found no equivalent role documentation for the open source deployment.

### Storage backends

ClickStack is built on ClickHouse, and its docs list no other log store. Logchef reads ClickHouse, VictoriaLogs, and [Grafana Loki](/tutorials/loki/) as sources. LogchefQL compiles to SQL, LogsQL, or LogQL, so one [dashboard](/features/dashboards/#multiple-data-sources) can mix panels from all three.

## Using them together

ClickStack's collector writes logs to an `otel_logs` table. Per the [schema docs](https://clickhouse.com/docs/clickstack/ingesting-data/schemas), its `Timestamp` column is `DateTime64(9)`, next to `Body`, `ServiceName`, `LogAttributes`, and `ResourceAttributes`. That meets Logchef's timestamp requirement, and the same columns appear in the upstream exporter table that Logchef's [OpenTelemetry Collector guide](/integration/otel-collector/) covers.

A common split:

1. ClickStack's collector ingests everything, and its UI serves traces, metrics, and session replay.
2. Logchef adds the `otel_logs` table as a source with `Timestamp` as the timestamp field.
3. Teams that only need logs get Logchef accounts scoped to their sources, with [saved queries and collections](/features/collections/), [saved views](/features/saved-views/), alerts, and the Logchef [CLI](/integration/cli/).

Logchef reads the table in place. It does not copy the data or change the schema.

## When ClickStack is the better choice

- You need traces, metrics, or session replay next to logs.
- You are starting without a pipeline and want a collector, schema, and UI that already fit together.
- You want a managed service. ClickHouse's [observability pricing page](https://clickhouse.com/pricing/observability) lists a fully managed service, in beta at the time of writing, at $0.10 per uncompressed GB for logs, traces, and session replay on its Scale plan (list price $0.20). The page says terms may change before general availability.
- You prefer an MIT license to AGPLv3.
- You want an agent to create dashboards and alerts through MCP. ClickStack's MCP server exposes tools for both.

## When Logchef is the better choice

- Your logs are already in ClickHouse, VictoriaLogs, or Loki and you do not want another collector in the path.
- You use VictoriaLogs or Loki, or more than one backend.
- You want OIDC, team-scoped source access, and service tokens without a cloud plan.
- You want to run one binary with SQLite, not a UI plus MongoDB.
- You want teams, sources, and access declared in a TOML file. See [declarative provisioning](/getting-started/provisioning/).
- You want one filter syntax across your backends. On ClickHouse and VictoriaLogs it includes counts and percentiles. See [LogchefQL aggregations](/guide/aggregations/).

## Migrating from ClickStack

There is no log data to move. Keep the collector, then add `otel_logs` as a ClickHouse source in Logchef. Start with the [quickstart](/getting-started/quickstart/), then read the [OpenTelemetry Collector guide](/integration/otel-collector/) for the table layout and [schema design](/integration/schema-design/) if you plan a new table. Dashboards, alerts, and saved searches do not transfer. Recreate them in Logchef.

If you drop ClickStack's collector, [Vector](/integration/vector/) or an upstream OpenTelemetry Collector can write the same table.

## FAQ

### Is Logchef a ClickStack alternative?

For log search, alerting, and dashboards, yes. For traces, metrics, or session replay, no. Logchef does not cover those signals.

### Is ClickStack the same as HyperDX?

ClickStack's repository describes it as "ClickStack (formerly HyperDX)". ClickHouse [announced the HyperDX acquisition](https://clickhouse.com/blog/clickhouse-acquires-hyperdx-the-future-of-open-source-observability) on March 13, 2025.

### Can Logchef read ClickStack's ClickHouse tables?

Yes, for logs. The `otel_logs` table has a `DateTime64(9)` timestamp column, which is the one thing a Logchef ClickHouse source requires. Logchef has no trace, metric, or session replay views.

### Does Logchef need MongoDB?

No. Logchef stores its metadata in SQLite, or in Postgres for multi-replica deployments.

### Does Logchef ingest logs or accept OTLP?

No. A collector writes to ClickHouse, VictoriaLogs, or Loki, and Logchef queries the result.

### Can ClickStack query VictoriaLogs?

Its docs describe ClickHouse as the storage and query engine and list no other log store. Logchef supports ClickHouse, [VictoriaLogs](/tutorials/victorialogs/), and [Loki](/tutorials/loki/).

To try Logchef on your own tables, start with the [quickstart](/getting-started/quickstart/).
