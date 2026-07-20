---
title: Logchef vs ClickStack
description: How Logchef's query UI over existing ClickHouse/VictoriaLogs data compares to ClickStack (HyperDX), ClickHouse's full OpenTelemetry observability stack.
---

Both projects put ClickHouse's speed in front of engineers looking at logs, but
they start from different premises and solve different problems. This page
compares them honestly so you can pick the right tool — including picking
ClickStack, if that's the better fit.

## The short version

**Logchef** is a query and control-plane UI you point at ClickHouse tables or
VictoriaLogs instances you already have. It doesn't collect logs, doesn't
require a particular schema, and doesn't try to be an APM. It's a fast, single
binary front end for log search, dashboards, alerting, and team access control
over data your existing pipeline already writes.

**ClickStack** (built by ClickHouse, formerly known as HyperDX) is an
opinionated, end-to-end observability platform: an OpenTelemetry Collector,
ClickHouse, and the HyperDX UI shipped together, covering logs, traces,
metrics, and session replay. It owns the ingestion pipeline and is built
around an OpenTelemetry-shaped schema.

If you already ship logs into ClickHouse or VictoriaLogs with Vector,
Filebeat, Fluent Bit, or your own OTel pipeline, and you want a lightweight
UI on top without adopting a new schema, Logchef fits that gap. If you're
starting from scratch and want logs, traces, metrics, and session replay
correlated in one product with an ingestion pipeline included, ClickStack is
the more complete answer, see [When ClickStack is the better
choice](#when-clickstack-is-the-better-choice) below.

## What each project actually is

### Logchef

Logchef is a specialized query and visualization layer that sits in front of
log storage you control. Per its own [architecture
documentation](/core/architecture/), it intentionally excludes any ingestion
pipeline: collectors like Vector or Filebeat write logs into ClickHouse or
VictoriaLogs, and Logchef only reads them back out. Beyond a timestamp column,
it does not require a specific [table schema](/integration/schema-design/) —
it ships an optional, tuned OpenTelemetry-style schema, but works against any
existing ClickHouse table or VictoriaLogs source.

- **Backends**: ClickHouse and VictoriaLogs, chosen per source
- **Query languages**: LogchefQL (Logchef's own filter syntax), plus each
  backend's native language (ClickHouse SQL or VictoriaLogs LogsQL)
- **Signals**: logs. ClickHouse-backed sources can carry `trace_id`/`span_id`
  columns for correlation, but Logchef has no trace waterfall, APM, or
  session-replay UI
- **Deployment**: single Go binary, SQLite metadata store by default, optional
  Postgres for [multi-replica HA](/operations/database-backends/)
- **Access control**: OIDC/SSO or built-in local auth, [team-scoped source
  access](/core/user-management/)
- **Alerting**: [scheduled query evaluation](/features/alerting/) with
  email/webhook notifications
- **Dashboards**: [multi-panel grid](/features/dashboards/) (time
  series/stat/table) that can mix ClickHouse and VictoriaLogs panels
- **AI**: optional [natural-language-to-SQL assistant](/features/ai-sql-generation/)
  against any OpenAI-compatible API, plus an [MCP
  server](/integration/mcp-server/) for AI coding assistants
- **License**: AGPLv3
- **Latest version referenced here**: v1.7.0 (2026-07-06)

### ClickStack

ClickStack is ClickHouse's own observability stack, built around the HyperDX
project ClickHouse acquired and now ships as "ClickStack." Its documentation
describes three components shipped together: ClickHouse (the datastore), an
OpenTelemetry Collector (ingestion), and HyperDX (the UI for search,
dashboards, alerts, and session replay). It's explicitly OpenTelemetry-native
— the bundled collector is the primary ingestion path — though ClickHouse's
docs note the schema isn't strictly limited to OTel, as long as events carry a
timestamp.

- **Backend**: ClickHouse (the stack doesn't support VictoriaLogs or other
  datastores)
- **Query languages**: a Lucene-style search syntax in the UI (transpiled to
  SQL), plus direct SQL for advanced queries
- **Signals**: logs, traces, metrics, session replay, and exceptions/errors —
  a full observability surface, not logs-only
- **Ingestion**: owns the pipeline via a bundled OpenTelemetry Collector;
  instrumented with OTel SDKs (JS, Python, Java, Go, Ruby, PHP, .NET, Elixir,
  Rust) or HyperDX's own browser/Node/Python SDKs
- **Deployment**: a single all-in-one Docker container for local/small setups,
  Docker Compose for multi-container deployments, or Kubernetes/Helm; the
  self-hosted open-source stack also runs a MongoDB instance to store
  application state (dashboards, alerts, saved searches, user profiles) —
  something Logchef doesn't need, since that metadata lives in Logchef's own
  SQLite/Postgres store
  instead
- **Managed option**: "Managed ClickStack" on ClickHouse Cloud, which takes
  over storage/compute scaling, backups, and (per ClickHouse's docs)
  fine-grained RBAC as part of the managed/enterprise tier — the self-hosted
  OSS docs don't detail an equivalent RBAC model
- **License**: HyperDX UI is MIT-licensed; ClickHouse and the OpenTelemetry
  Collector are Apache 2.0
- **Latest version referenced here**: HyperDX/ClickStack app v2.30.1
  (published 2026-07-13)

## Feature comparison

*Last updated 2026-07-14. Versions compared: Logchef v1.7.0, ClickStack
(HyperDX app) v2.30.1. Both projects move fast — verify current behavior
against each project's own docs before making a decision.*

| | Logchef | ClickStack |
|---|---|---|
| Backend(s) | ClickHouse, VictoriaLogs | ClickHouse only |
| Owns ingestion? | No — reads existing data | Yes — bundled OTel Collector |
| Forced schema? | No (timestamp column only; optional tuned schema available) | OTel-shaped by default; other formats work if a timestamp is present |
| Logs | Yes | Yes |
| Traces | No (ClickHouse sources can store `trace_id`/`span_id` for correlation, no trace UI) | Yes, native waterfall/APM view |
| Metrics | No | Yes |
| Session replay | No | Yes |
| Query languages | LogchefQL, ClickHouse SQL, VictoriaLogs LogsQL | Lucene-style search, SQL |
| Alerting | Scheduled query evaluation, email/webhook | Built into HyperDX UI |
| Dashboards | Multi-panel grid, mixed backends per dashboard | Built into HyperDX UI |
| Saved queries / collections | Yes, team-shared [Collections](/features/collections/) | Saved searches (per HyperDX UI) |
| RBAC | Team-based, OIDC/SSO or local auth | Not detailed for self-hosted OSS; fine-grained RBAC positioned as a managed/enterprise feature |
| HA / multi-replica | Optional Postgres metadata backend for multi-replica ([caveats apply](/operations/database-backends/)) | Managed ClickStack (ClickHouse Cloud) handles HA/scaling; self-hosted HA is on you via ClickHouse's own clustering |
| Deployment footprint | Single binary + your existing ClickHouse/VictoriaLogs | ClickHouse + OTel Collector + HyperDX UI (+ MongoDB for self-hosted app state) |
| AI features | NL-to-SQL assistant, MCP server for coding assistants | Not part of core OSS docs reviewed here |
| License | AGPLv3 | HyperDX UI: MIT. ClickHouse, OTel Collector: Apache 2.0 |

## When Logchef is the better choice

- You already ship logs into ClickHouse or VictoriaLogs (Vector, Fluent Bit,
  Filebeat, a custom pipeline) and don't want to re-architect ingestion or
  adopt a new schema to get a good UI.
- You want a single static binary with no additional services beyond the
  datastore you already run.
- Your problem is genuinely logs-first: search, saved queries shared across a
  team, scheduled alerts, and simple multi-panel dashboards — not full APM,
  traces, or session replay.
- You need VictoriaLogs support specifically, or want to mix ClickHouse and
  VictoriaLogs sources under one access-control model.
- You want team-scoped RBAC and OIDC/SSO out of the box without an enterprise
  tier.

## When ClickStack is the better choice

- You're instrumenting a system from scratch and want logs, traces, metrics,
  and session replay correlated in one product, with the ingestion pipeline
  included rather than assembled separately.
- You need real APM: trace waterfalls, span-level latency breakdowns,
  exception tracking tied to user sessions.
- You'd rather adopt OpenTelemetry as your instrumentation standard than bring
  an existing, possibly non-OTel, log schema.
- You want a managed option (ClickHouse Cloud's Managed ClickStack) that takes
  ingestion, storage scaling, and backups off your plate.

## They can coexist

Because Logchef doesn't own ingestion or a schema, it can point at the same
ClickHouse tables ClickStack's OpenTelemetry Collector writes into — nothing
about the two is mutually exclusive. Some teams use ClickStack's collector for
ingestion and traces/metrics, and use Logchef as a lighter, team-scoped UI for
day-to-day log search and alerting on top of the resulting tables. If you go
this route, start from [Logchef's schema design
guide](/integration/schema-design/) to confirm your OTel table layout works
well with LogchefQL.

## Further reading

- [Logchef architecture](/core/architecture/)
- [Choosing between ClickHouse and VictoriaLogs sources](/integration/schema-design/)
- [Database backends and HA](/operations/database-backends/)
- [Alerting](/features/alerting/)
- [Dashboards](/features/dashboards/)
- ClickStack: [clickhouse.com/clickstack](https://clickhouse.com/clickstack)
- HyperDX on GitHub: [github.com/hyperdxio/hyperdx](https://github.com/hyperdxio/hyperdx)
