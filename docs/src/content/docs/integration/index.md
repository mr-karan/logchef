---
title: Integrations
description: How to get logs into ClickHouse or VictoriaLogs so Logchef can query them — shippers, schemas, and the CLI/MCP tooling.
---

Logchef is a query and control plane for logs that already live in ClickHouse or VictoriaLogs — it doesn't collect or ship logs itself. Getting logs in front of Logchef is a three-step pipeline:

1. **Collect** — an agent (Vector, the OpenTelemetry Collector) reads logs from your services, files, containers, or cluster
2. **Store** — the agent writes rows into a ClickHouse table (or a VictoriaLogs stream)
3. **Explore** — you point a Logchef source at that table and query it with LogchefQL, SQL, or LogsQL

The guides below cover each collection path, how to shape the ClickHouse schema, and the tools (CLI, MCP server) for working with the data once it's there.

## Shipping Logs

- [Shipping Logs with Vector](/integration/vector) — general-purpose log pipeline: syslog, files, Docker, journald
- [OpenTelemetry Collector](/integration/otel-collector) — the `clickhouseexporter` and the `otel_logs` schema
- [Kubernetes Logs](/integration/kubernetes) — DaemonSet collection with the Collector or Vector
- [Docker Logs](/integration/docker) — Vector's `docker_logs` source against the Docker socket
- [Shipping NGINX Logs to ClickHouse](/tutorials/nginx-logs) — a worked example with a purpose-built schema
- [Using VictoriaLogs with Logchef](/tutorials/victorialogs) — connect a VictoriaLogs datasource instead of ClickHouse

## Schema

- [Schema Design](/integration/schema-design) — field types, codecs, and indexing for fast, storage-efficient queries

## Querying the Data

- [Logchef CLI](/integration/cli) — query logs from the terminal, built for scripting and quick investigations
- [MCP Server](/integration/mcp-server) — connect AI coding assistants to Logchef over the Model Context Protocol

## Next steps

- New to Logchef? Start with [Quick Start](/getting-started/quickstart)
- Already have logs in ClickHouse? Jump to [Query Interface](/user-guide/query-interface) and [Search Syntax](/guide/search-syntax)
