# Development Environment

Local development setup for Logchef.

## VictoriaLogs Reference

The local VictoriaLogs setup here is aligned with VictoriaMetrics' official Docker deployment examples:

- Official deployment guide: [VictoriaLogs Docker deployment](https://github.com/VictoriaMetrics/VictoriaLogs/tree/master/deployment/docker)
- Official single-node compose: [compose-vl-single.yml](https://github.com/VictoriaMetrics/VictoriaLogs/blob/master/deployment/docker/compose-vl-single.yml)
- Official Vector example: [vector-vl-single.yml](https://github.com/VictoriaMetrics/VictoriaLogs/blob/master/deployment/docker/vector-vl-single.yml)

The upstream single-node example exposes VictoriaLogs on `http://localhost:9428`, routes VMUI through `vmauth` on `http://localhost:8427/select/vmui/`, and uses Vector to push logs over HTTP into VictoriaLogs.

Logchef's local dev environment intentionally keeps this lighter:

- it runs the VictoriaLogs server itself on `:9428`
- it does not run the full upstream `vmauth`, Grafana, `vmalert`, and VictoriaMetrics sidecar stack
- it seeds a LogChef datasource pointing at the local VictoriaLogs instance
- it ships a tiny ingestion helper for sample data instead of mirroring the full upstream Vector topology

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [`just`](https://github.com/casey/just)
- [`vector`](https://vector.dev/docs/setup/installation/) (for log ingestion)

## Quick Start

```bash
# One-time setup (starts docker, creates tables, seeds data)
just dev-setup

# Run backend (terminal 1)
just run-backend

# Run frontend (terminal 2)
just run-frontend

# Ingest sample logs (terminal 3)
just dev-ingest-logs
```

Open http://localhost:5173 and login with `admin@logchef.internal` / `password`.
Mailpit UI is available at http://localhost:8025.
VictoriaLogs health should be available at http://localhost:9428/health.
This local setup does not expose the upstream VMUI path on `:8427`, because `vmauth` is not part of Logchef's dev compose.

To test email delivery locally, configure SMTP settings to:
- Host: `mailpit`
- Port: `1025`
- From: `alerts@logchef.local`

## What Gets Created

### ClickHouse Tables

The `init-clickhouse.sql` script creates two tables on first startup:

- `default.http` - HTTP access logs (method, status, host, etc.)
- `default.syslogs` - Syslog format logs (lvl, service_name, body, etc.)

### Seed Data

Running `just dev-seed` creates:

- **User**: `dev@localhost` (admin)
- **Team**: "Dev Team"
- **Sources**: HTTP Access Logs, Syslog Logs, VictoriaLogs Demo (all linked to Dev Team)
- **VictoriaLogs sample data**: `just dev-ingest-logs` now inserts a small set of demo logs into the local VictoriaLogs instance in addition to the ClickHouse demo streams

## Services

| Service | Port | Description |
|---------|------|-------------|
| ClickHouse HTTP | 8123 | HTTP interface |
| ClickHouse Native | 9000 | Native protocol |
| VictoriaLogs | 9428 | Local VictoriaLogs API |
| Dex | 5556 | OIDC provider |
| Webhook Receiver | 8888 | Test webhook endpoint |
| Mailpit UI | 8025 | Email inbox UI |
| Mailpit SMTP | 1025 | SMTP server |

## Useful Commands

```bash
# Quick reset (truncate logs, recreate SQLite data)
just dev-reset

# Full clean (stop docker, remove volumes, delete DB)
just dev-clean

# Re-seed without full reset
just dev-seed

# View webhook receiver logs (for testing alerts)
just dev-webhook-logs

# Test webhook is working
just dev-test-webhook
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Infrastructure services |
| `init-clickhouse.sql` | Creates ClickHouse tables |
| `seed.sh` | Creates team, sources, user (idempotent) |
| `provisioning.toml` | Example datasource-aware provisioning config for dev |
| `ingest-victorialogs.sh` | Sends sample JSONL logs to the local VictoriaLogs instance |
| `http.toml` | Vector config for HTTP demo logs |
| `syslog.toml` | Vector config for syslog demo logs |
| `dex/config.yaml` | Dex OIDC configuration |

## Troubleshooting

**Tables not created?**
```bash
# Manually create tables
just dev-init-tables
```

**Seed script fails?**
- Ensure backend is running (`just run-backend`)
- Check the database path matches your config
- Run with: `LOGCHEF_DB_PATH=./logchef.db ./dev/seed.sh`

**Vector can't connect?**
- Ensure ClickHouse is healthy: `curl http://localhost:8123/ping`
- Check table exists: `curl "http://localhost:8123/?query=SHOW+TABLES"`

**VictoriaLogs source is empty?**
- Run `just dev-ingest-logs` to send sample JSONL data into VictoriaLogs and demo streams into ClickHouse.
- Validate the service is up with: `curl http://localhost:9428/health`
- Re-send only the VictoriaLogs sample payload with: `cd dev && ./ingest-victorialogs.sh`
- If you want the full upstream VictoriaLogs demo stack with VMUI, Grafana, `vmauth`, and `vmalert`, use the official compose files linked above instead of Logchef's trimmed local dev compose.
