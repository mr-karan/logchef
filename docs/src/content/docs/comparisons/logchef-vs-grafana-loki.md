---
title: Logchef vs Grafana Loki
description: How Logchef's query UI over ClickHouse or VictoriaLogs compares to Grafana Loki's label-indexed, object-storage logging — architecture, query languages, high-cardinality, and when each is the right fit.
---

Both let you search logs without paying for a hosted SaaS, but they take
opposite approaches to storage and indexing, and they slot into very different
stacks. This page compares them honestly so you can pick the right tool —
including picking Loki, if that's the better fit.

## The short version

**Logchef** is a query and control-plane UI you point at ClickHouse tables or
VictoriaLogs instances you already run. It doesn't collect logs and doesn't
force a schema — collectors write logs into your backend, and Logchef reads
them back out with fast full-text search, SQL, dashboards, alerting, and
team access control.

**Grafana Loki** is a full logging backend: it ingests logs through agents
(Promtail / Grafana Alloy), indexes only a small set of *labels* (not the log
content), stores compressed chunks in object storage (S3/GCS/local), and is
queried with LogQL — almost always through Grafana's UI. It's the "L" in
Grafana's LGTM stack.

If you already run — or want to run — ClickHouse or VictoriaLogs and need fast
search over high-cardinality fields plus SQL analytics, Logchef fits that gap.
If you want the cheapest possible object-storage logging at large scale, with
low-cardinality labels, inside a Grafana-centric stack, Loki is the more
natural answer — see [When Grafana Loki is the better
choice](#when-grafana-loki-is-the-better-choice).

## What each project actually is

### Logchef

Logchef is a query and visualization layer that sits in front of log storage
you control. Per its [architecture documentation](/core/architecture/), it
intentionally excludes any ingestion pipeline: collectors like Vector, Fluent
Bit, or an OpenTelemetry pipeline write logs into ClickHouse or VictoriaLogs,
and Logchef only reads them back. Beyond a timestamp column it doesn't require
a specific [table schema](/integration/schema-design/).

- **Backends**: ClickHouse and VictoriaLogs, chosen per source
- **Indexing**: whatever the backend does — ClickHouse indexes columns
  (including high-cardinality ones) and supports full-text via tokenized
  indexes; VictoriaLogs indexes all fields
- **Query languages**: LogchefQL (Logchef's own filter syntax), plus each
  backend's native language (ClickHouse SQL or VictoriaLogs LogsQL)
- **Signals**: logs (ClickHouse sources can carry `trace_id`/`span_id` columns
  for correlation; there is no trace/APM UI)
- **Deployment**: single Go binary, SQLite metadata by default, optional
  Postgres for [multi-replica HA](/operations/database-backends/)
- **UI**: its own [explorer](/), [dashboards](/features/dashboards/),
  [alerting](/features/alerting/), and [access control](/core/user-management/)
- **License**: AGPLv3

### Grafana Loki

Loki is a horizontally scalable log aggregation system inspired by Prometheus.
Its defining design choice is that it indexes only labels — a small set of
key/value pairs attached to each stream — and stores the raw log lines as
compressed chunks in object storage. That makes ingestion and storage cheap,
at the cost of pushing content search into a scan of the matched chunks.

- **Storage**: object storage (S3/GCS/Azure/filesystem), label index only
- **Ingestion**: owns it — agents such as Promtail or Grafana Alloy push
  streams into Loki
- **Query language**: LogQL (label selectors plus filter expressions and
  metric queries)
- **Signals**: logs; correlates with metrics (Prometheus/Mimir) and traces
  (Tempo) inside the Grafana LGTM stack
- **UI**: Grafana (Explore, dashboards, alerting) — Loki itself has no UI
- **License**: AGPLv3 (Grafana relicensed Loki in 2024)

## Feature comparison

| | Logchef | Grafana Loki |
|---|---|---|
| Role | Query UI over existing storage | Full log backend (ingest + store + query) |
| Backend(s) | ClickHouse, VictoriaLogs | Loki's own object-storage engine |
| Owns ingestion? | No — reads existing data | Yes — via Promtail / Alloy agents |
| Indexing model | Backend columns/fields (high-cardinality OK) | Labels only; content is scanned, not indexed |
| High-cardinality fields | Handled well (columnar / per-field) | A known weak spot — too many label values hurt performance |
| Full-text search | Yes (tokenized indexes on ClickHouse, native on VictoriaLogs) | Filter expressions scan matched chunks; no content index |
| Query languages | LogchefQL, ClickHouse SQL, VictoriaLogs LogsQL | LogQL |
| SQL / ad-hoc analytics | Yes (native ClickHouse SQL) | No SQL; LogQL metric queries only |
| Built-in UI | Yes (explorer, dashboards, alerting, RBAC) | No — uses Grafana |
| Signals | Logs | Logs (metrics/traces via the wider LGTM stack) |
| Alerting | Scheduled query evaluation, email/webhook | Loki ruler + Grafana alerting |
| RBAC | Team-based, OIDC/SSO or local auth | Via Grafana / tenant setup |
| Deployment footprint | Single binary + your ClickHouse/VictoriaLogs | Loki (single- or micro-service mode) + agents + object store + Grafana |
| Cheapest-storage story | Depends on the backend you run | Strong — compressed chunks on cheap object storage |

## When Logchef is the better choice

- You already store logs in **ClickHouse or VictoriaLogs**, or want to, and
  need a UI without adopting a new backend.
- You query on **high-cardinality fields** (request IDs, user IDs, trace IDs)
  where Loki's label model struggles.
- You want **SQL** and real analytical queries over logs, not just filtering.
- You want a **dedicated log UI** — with dashboards, alerting, saved queries,
  and team access control — without standing up Grafana and its stack.
- You'd rather run a **single binary** against storage you already operate.

Notably, **VictoriaLogs is itself frequently adopted as a Loki alternative** —
so "Logchef in front of VictoriaLogs" is a direct, self-hosted alternative to a
Loki + Grafana setup, with stronger full-text and high-cardinality behavior.

## When Grafana Loki is the better choice

- You're **all-in on Grafana** and want logs correlated with Prometheus
  metrics and Tempo traces in one UI.
- Your logs are **high-volume but low-cardinality**, and object-storage cost
  is the primary concern.
- You want a **turnkey ingest-to-store pipeline** (agents + backend) rather
  than running ClickHouse or VictoriaLogs yourself.
- Your team already knows **LogQL** and lives in Grafana Explore.

## They can coexist

These aren't mutually exclusive. A common split: keep Grafana + Loki (or
Prometheus/Tempo) for metrics, traces, and cheap high-volume logs, and point
Logchef at a ClickHouse or VictoriaLogs source for the log workloads that need
high-cardinality search and SQL analysis. Logchef reads your existing storage,
so adding it doesn't disturb an existing Grafana setup.

## Further reading

- [Logchef architecture](/core/architecture/)
- [Connecting VictoriaLogs](/tutorials/victorialogs/)
- [Schema design](/integration/schema-design/)
- [Dashboards](/features/dashboards/) and [alerting](/features/alerting/)
