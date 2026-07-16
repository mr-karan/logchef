---
title: Roadmap
description: What Logchef has shipped recently, what's being built now, and where the log analytics project is headed next.
---

This is a snapshot of where Logchef is and where it's going. Near-term items are a
committed direction, not dated promises. Timelines shift as we learn.

## Recently shipped

- **Dashboards**: grids of saved visualizations (time series, stat, and table panels) with a shared time range and auto-refresh; panels can mix ClickHouse and VictoriaLogs sources (v2.0). See [dashboards](/features/dashboards).
- **VictoriaLogs as a first-class datasource** (v2.0): LogchefQL compiles to LogsQL as well as ClickHouse SQL, with a native LogsQL editor and a capability-driven UI that adapts to what each backend supports.
- **Built-in local authentication** (v2.0): run Logchef with email+password auth, with or without an external OIDC provider.
- **Live tail** (v2.0): follow matching logs as they arrive, on both ClickHouse and VictoriaLogs sources.
- **Streaming query responses** (v2.0): ClickHouse preview queries stream row-by-row instead of buffering the full result in memory, removing a memory-spike path on large result sets.
- **Pluggable metadata store**: SQLite by default, with Postgres for multi-replica HA deployments (v1.7). See [database backends](/operations/database-backends/).
- **Redesigned Library**: collections with per-collection roles (v1.7). See [collections](/features/collections).
- **Scoped API tokens + service accounts**: non-interactive access with fine-grained scopes (v1.6.1). See [service tokens](/features/service-tokens).
- **Collections & editor team role**: shared saved queries and a dedicated editor role (v1.6).
- **Alerting**: SQL-based conditions with email and webhook delivery (v1.0+). See [alerting](/features/alerting).
- **Rust CLI**: `logchef` covers auth, teams/sources/schema discovery, query/sql/explain, fields/histogram, native SSE tail, find, open, collections/saved-queries, doctor, and completions — plus VictoriaLogs parity and a `--quiet` scripting mode. See [the CLI docs](/integration/cli).
- **MCP server**: exposes Logchef to AI assistants over the Model Context Protocol. See [the MCP server docs](/integration/mcp-server).

## Next

Near-term direction we're committed to:

- **Explore performance**: virtualized rendering for very large result sets.
- **Alert-scheduler leader election**: so alerting runs correctly across multiple replicas.

## Later / exploring

Directions we're interested in but haven't committed to:

- Server-side persistent query history.
- Additional datasource backends: the provider interface is designed to accommodate them.

## Get involved

Feedback and contributions are welcome. Open an issue or start a discussion on
[GitHub](https://github.com/mr-karan/logchef).
