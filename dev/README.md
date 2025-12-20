# Development Environment

Local development setup for LogChef.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [`just`](https://github.com/casey/just)
- [`vector`](https://vector.dev/docs/setup/installation/) (for log ingestion)

## Quick Start

```bash
# 1. Start infrastructure (ClickHouse, Dex, Alertmanager)
just dev-docker

# 2. Run backend (in new terminal)
just run-backend

# 3. Run frontend (in new terminal)
just run-frontend

# 4. Seed dev data - creates team, sources, user (in new terminal)
just dev-seed

# 5. Ingest sample logs (in new terminal)
cd dev && vector -c http.toml
# or
cd dev && vector -c syslog.toml
```

Open http://localhost:5173 and login with `dev@localhost` / `password`.

## What Gets Created

### ClickHouse Tables

The `init-clickhouse.sql` script creates two tables on first startup:

- `default.http` - HTTP access logs (method, status, host, etc.)
- `default.syslogs` - Syslog format logs (lvl, service_name, body, etc.)

### Seed Data

Running `just dev-seed` creates:

- **User**: `dev@localhost` (admin)
- **Team**: "Dev Team"
- **Sources**: HTTP Access Logs, Syslog Logs (both linked to Dev Team)

## Services

| Service | Port | Description |
|---------|------|-------------|
| ClickHouse HTTP | 8123 | HTTP interface |
| ClickHouse Native | 9000 | Native protocol |
| Dex | 5556 | OIDC provider |
| Alertmanager | 9093 | Alert routing |
| Webhook Receiver | 8888 | Test webhook endpoint |

## Useful Commands

```bash
# View Alertmanager logs
just dev-alertmanager-logs

# View webhook receiver logs (for testing alerts)
just dev-webhook-logs

# Test webhook is working
just dev-test-webhook

# Open Alertmanager UI
just dev-alertmanager-ui

# Reset everything (delete volumes)
cd dev && docker compose down -v
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Infrastructure services |
| `init-clickhouse.sql` | Creates ClickHouse tables |
| `seed.sh` | Creates team, sources, user via API |
| `http.toml` | Vector config for HTTP demo logs |
| `syslog.toml` | Vector config for syslog demo logs |
| `dex/config.yaml` | Dex OIDC configuration |
| `alertmanager/alertmanager.yml` | Alertmanager routing config |

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
