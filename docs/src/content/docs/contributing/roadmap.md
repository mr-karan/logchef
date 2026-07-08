---
title: Roadmap
description: What Logchef has shipped recently, what's being built now, and where the project is headed.
---

This is a snapshot of where Logchef is and where it's going. Near-term items are a
committed direction, not dated promises — timelines shift as we learn.

## Recently shipped

- **Dashboards** — grids of saved visualizations (time series, stat, and table panels) with a shared time range and auto-refresh; panels can mix ClickHouse and VictoriaLogs sources (v2.0). See [dashboards](/features/dashboards).
- **Pluggable metadata store** — SQLite by default, with Postgres for multi-replica HA deployments (v1.7). See [database backends](/operations/database-backends/).
- **Redesigned Library** — collections with per-collection roles (v1.7). See [collections](/features/collections).
- **Scoped API tokens + service accounts** — non-interactive access with fine-grained scopes (v1.6.1). See [service tokens](/features/service-tokens).
- **Collections & editor team role** — shared saved queries and a dedicated editor role (v1.6).
- **Alerting** — SQL-based conditions with email and webhook delivery (v1.0+). See [alerting](/features/alerting).
- **Rust CLI** — `logchef` runs query, sql, tail, and find from the terminal. See [the CLI docs](/integration/cli).
- **MCP server** — exposes Logchef to AI assistants over the Model Context Protocol. See [the MCP server docs](/integration/mcp-server).

## Now: multi-datasource Logchef

The current release makes log sources pluggable. ClickHouse and
**VictoriaLogs** become first-class backends behind a single query experience:

- LogchefQL compiles to ClickHouse SQL or LogsQL depending on the source.
- The UI is capability-driven, adapting to what each backend supports.
- Native-language editors per backend, so you can drop down to raw SQL or LogsQL when you need to.
- **Built-in local authentication** — run Logchef without an external OIDC provider.
- **Live tail** — follow matching logs as they arrive, on both backends.

## Next

Near-term direction we're committed to:

- **Explore performance** — virtualized rendering for very large result sets.
- **Alert-scheduler leader election** — so alerting runs correctly across multiple replicas.

## Later / exploring

Directions we're interested in but haven't committed to:

- Server-side persistent query history.
- Streaming responses for very large result sets.
- Additional datasource backends — the provider interface is designed to accommodate them.

## Get involved

Feedback and contributions are welcome. Open an issue or start a discussion on
[GitHub](https://github.com/mr-karan/logchef).
