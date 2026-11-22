---
title: Logchef vs SigNoz
head:
  - tag: title
    content: "Logchef vs SigNoz"
description: "Logchef vs SigNoz: a log-only UI for your own ClickHouse, VictoriaLogs or Loki, compared with SigNoz's OpenTelemetry-native logs, metrics, and traces."
---

SigNoz is an OpenTelemetry-native observability platform that stores logs, metrics, and traces in a ClickHouse it manages. Logchef is a log-only query UI that reads the ClickHouse tables, VictoriaLogs instances, and Grafana Loki tenants you already run. Both use ClickHouse, but Logchef cannot read SigNoz's log table as it is, for a reason this page explains.

Competitor facts on this page come from SigNoz's docs, pricing page, and repositories as of November 2026. Sources are linked below.

## The short version

Pick SigNoz if you want one product for logs, metrics, traces, and exceptions, you instrument with OpenTelemetry, and you are happy for SigNoz to own the ClickHouse schema.

Pick Logchef if you only need logs, your tables already exist in a schema you chose, you use VictoriaLogs or Loki, or you want OIDC and team-scoped access in a single self-hosted binary.

## At a glance

| | Logchef | SigNoz |
|---|---|---|
| What it is | A log query UI and control plane over existing log storage | An "open-source, OpenTelemetry-native observability platform" with logs, metrics, and traces in one tool ([repository](https://github.com/SigNoz/signoz)) |
| License | AGPLv3 | MIT Expat, except the `ee/` and `cmd/enterprise/` directories, which have their own license ([LICENSE](https://github.com/SigNoz/signoz/blob/main/LICENSE)) |
| Storage | Your ClickHouse tables, VictoriaLogs, or [Loki](/tutorials/loki/). Metadata in SQLite or Postgres | ClickHouse, in a schema SigNoz creates and migrates ([architecture](https://signoz.io/docs/architecture/)) |
| Ingestion | None. Your collector writes the logs | The SigNoz OTel Collector, which accepts OTLP, Jaeger, Kafka, and Zipkin among others |
| Query languages | [LogchefQL](/guide/search-syntax/), ClickHouse SQL, [VictoriaLogs LogsQL](/guide/logsql-queries/), [Loki LogQL](/guide/logql-queries/) | [Query Builder](https://signoz.io/docs/userguide/query-builder-v5/) with filter expressions, and [ClickHouse SQL](https://signoz.io/docs/userguide/logs_clickhouse_queries/) |
| Signals | Logs | Logs, metrics, traces, exceptions |
| Deployment | One binary next to your log store | Docker Standalone, Docker Swarm, or Kubernetes with Helm ([install](https://signoz.io/docs/install/self-host/)) |
| Auth and access control | OIDC or local login, [teams that scope source access](/core/user-management/), [service tokens](/features/service-tokens/), an [audit log](/operations/audit-log/) | Google Workspace SSO in Community Edition. SAML and OIDC on Cloud and Enterprise ([SSO](https://signoz.io/docs/manage/administrator-guide/sso/overview/)) |
| Alerting | [Scheduled rules](/features/alerting/) on logs, with email, webhook, Slack, and PagerDuty destinations | Rules on metrics, logs, traces, anomalies, or exceptions, sent to Slack, PagerDuty, Opsgenie, MS Teams, email, or webhooks ([alerts](https://signoz.io/docs/alerts/)) |
| Dashboards | [Built in](/features/dashboards/), log panels only, with [scheduled email reports](/features/scheduled-reports/) | Built in, across all signals |
| AI and MCP | [AI query generation](/features/ai-sql-generation/) and a built-in [MCP endpoint](/integration/mcp-server/) | [MCP server](https://signoz.io/docs/ai/signoz-mcp-server/), hosted on Cloud and self-run otherwise. An AI teammate in SigNoz Cloud |
| Pricing | Free. Self-hosted only | Community Edition is free. Cloud Teams starts at $49 per month ([pricing](https://signoz.io/pricing/)) |

## How they differ

### Logs only versus full observability

SigNoz covers application performance monitoring, distributed tracing, metrics, logs, and exception tracking.

Logchef covers logs. It has no traces UI, no APM, no metrics store, and no session replay. You can store `trace_id` in a log table and filter on it, but Logchef will not draw the trace. If you need traces and metrics in the same tool as logs, SigNoz fits and Logchef does not.

### Who owns the schema

SigNoz creates its own databases and tables in ClickHouse and migrates them between releases. Logs go to `signoz_logs.logs_v2`, and SigNoz's query layer knows that layout. You send telemetry to the SigNoz collector and do not design tables.

Logchef works the other way around. It has [no ingestion pipeline](/core/architecture/), and a ClickHouse source can be any table with a `DateTime` or `DateTime64` timestamp column. Logchef reads the columns from ClickHouse and builds the [field sidebar](/features/field-sidebar/) and query suggestions from them. Its [schema advisor](/features/schema-advisor/) proposes indexes and column changes for that table as DDL that you review and run. See [schema design](/integration/schema-design/).

### What you run

SigNoz's [Docker install](https://signoz.io/docs/install/docker/) asks for at least 4 GB of memory and starts five containers: SigNoz, the collector, ClickHouse, ClickHouse Keeper, and Postgres.

Logchef is one binary with its web UI embedded. Metadata goes to SQLite by default, or to [Postgres](/operations/database-backends/) for more than one replica. You also need the log store itself, so compare Logchef plus your ClickHouse with the whole SigNoz stack, not with the SigNoz binary alone.

### Sign-in and access control

Logchef's open source build includes OIDC sign-in, local accounts, and SSO auto-provisioning. Access is scoped by team: admins attach sources to teams, and users query only the sources of their teams. [Declarative provisioning](/getting-started/provisioning/) keeps that mapping in a TOML file, and an [audit log](/operations/audit-log/) records configuration changes, sign-ins, and data exports.

SigNoz's SSO docs list Google Workspace OAuth as "Available on Community Edition, SigNoz Cloud and Enterprise Self-Hosted", and SAML and OIDC as available on SigNoz Cloud and Enterprise Self-Hosted. The pricing page lists fine-grained RBAC outside Community Edition. If you self-host and your identity provider is Okta, Keycloak, or another OIDC provider, check which SigNoz edition you need.

### Query model

SigNoz centers on the Query Builder, a visual interface shared by logs, traces, and metrics, with filter expressions such as `service.name = 'frontend' AND http.status_code >= 400`. It also documents ClickHouse SQL against its log tables.

Logchef is text first. [LogchefQL](/guide/search-syntax/) is a short filter syntax with an [aggregation pipe](/guide/aggregations/), and native mode accepts ClickHouse `SELECT` queries, LogsQL, or LogQL. [Time macros](/guide/time-macros/) fill in the selected range.

## Using them together

### Can Logchef read SigNoz's ClickHouse tables?

Not directly. SigNoz's [logs schema](https://signoz.io/docs/userguide/logs_clickhouse_queries/) stores `timestamp` as `UInt64` nanoseconds. Logchef validates the timestamp column when you add a source and rejects any type that is not `DateTime` or `DateTime64`.

A ClickHouse view can expose a converted column:

```sql
CREATE VIEW signoz_logs.logs_for_logchef AS
SELECT toDateTime(intDiv(timestamp, 1000000000), 'UTC') AS ts, *
FROM signoz_logs.distributed_logs_v2;
```

Logchef accepts a view as a source, with `ts` as the timestamp field. Treat this as a workaround with limits:

- **Second precision.** Logs within the same second have no defined order.
- **Different time filters.** SigNoz's [documented queries](https://signoz.io/docs/userguide/logs_clickhouse_queries/) filter both `timestamp` and `ts_bucket_start`. Logchef's generated time filter targets the configured `ts` column. Verify query pruning on your own table before adopting this view.
- **Not tested end to end.** This view is a proposed adaptation, not a verified migration from a live SigNoz installation.

### A cleaner split

If you want both tools, send the same logs to two places. An upstream OpenTelemetry Collector can export OTLP to SigNoz and also write a ClickHouse table that Logchef reads, as described in the [OpenTelemetry Collector guide](/integration/otel-collector/). This doubles log storage. It makes sense when a few teams need Logchef's access model or SQL over logs while SigNoz stays the main observability tool.

## When SigNoz is the better choice

- You need traces, APM, metrics, or exception tracking with your logs.
- You are adopting OpenTelemetry and want a backend built around its data model.
- You want built-in notification channels for Opsgenie or MS Teams, or alert rules on metrics, traces, and anomalies. Logchef alerts on logs only and sends to email, webhook, Slack, and PagerDuty.
- You want a managed service. Per the [pricing page](https://signoz.io/pricing/), SigNoz Cloud's Teams plan is $49 per month including $49 of usage, with logs and traces at $0.30 per GB ingested and 15 days of retention.
- You prefer an MIT core license to AGPLv3.

## When Logchef is the better choice

- You need logs only and do not want to run a full observability platform for them.
- Your logs already sit in ClickHouse tables with your own schema, and you do not want to re-ingest them.
- You use VictoriaLogs or Loki, or more than one backend. One [dashboard](/features/dashboards/#multiple-data-sources) can mix panels from each.
- You self-host and need OIDC sign-in with access scoped by team.
- You want log search from a terminal through the [CLI](/integration/cli/), and [saved queries](/features/collections/) shared with your team.

## Migrating from SigNoz

There is no migration guide for SigNoz, and no tool that copies SigNoz dashboards or alerts into Logchef. To move log search to Logchef:

1. Follow the [quickstart](/getting-started/quickstart/) to run Logchef.
2. Create a log table from the [schema design](/integration/schema-design/) page, or let the collector create `otel_logs`.
3. Point your logs pipeline at that table with the [OpenTelemetry Collector](/integration/otel-collector/) or [Vector](/integration/vector/).
4. Recreate alert rules and dashboards. The [dashboard gallery](/features/dashboard-gallery/) has worked examples.

Keep SigNoz, or another tool, for traces and metrics.

## FAQ

### Is Logchef a SigNoz alternative?

For log search, log alerts, and log dashboards, yes. For traces, metrics, or APM, no.

### Can Logchef read SigNoz's ClickHouse tables?

Not as they are. SigNoz stores log timestamps as `UInt64` nanoseconds, and Logchef needs a `DateTime` or `DateTime64` column. A view can convert the column, with the precision and filtering caveats described [above](#can-logchef-read-signozs-clickhouse-tables).

### Does Logchef support OpenTelemetry?

Logchef reads the `otel_logs` table that the OpenTelemetry Collector's ClickHouse exporter writes. It does not receive OTLP itself.

### Is SigNoz open source?

Most of the repository is under the MIT Expat license. The `ee/` and `cmd/enterprise/` directories are under a separate license, and some features are listed only for Cloud and Enterprise plans.

### Can Logchef and SigNoz share one ClickHouse cluster?

For a self-hosted SigNoz, yes in principle. Its ClickHouse is a regular ClickHouse server, and Logchef queries whichever tables you add as sources. Keep your own tables in a database separate from `signoz_logs`. We have not tested this setup.

To try Logchef on your own tables, start with the [quickstart](/getting-started/quickstart/).
