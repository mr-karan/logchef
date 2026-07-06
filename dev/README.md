# Development Environment

Local development setup for Logchef.

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
- **Sources**: HTTP Access Logs, Syslog Logs (both linked to Dev Team)

## Services

| Service | Port | Description |
|---------|------|-------------|
| ClickHouse HTTP | 8123 | HTTP interface |
| ClickHouse Native | 9000 | Native protocol |
| Dex | 5556 | OIDC provider |
| Webhook Receiver | 8888 | Test webhook endpoint |
| Mailpit UI | 8025 | Email inbox UI |
| Mailpit SMTP | 1025 | SMTP server |
| Postgres | 5432 | Opt-in metadata backend (default is SQLite) |

## Postgres backend (opt-in)

logchef defaults to per-pod **SQLite**. To exercise the **Postgres** metadata
backend (for multi-replica / HA), the dev stack ships a Postgres 17 instance:

```
DSN: postgres://logchef:logchef@localhost:5432/logchef?sslmode=disable
```

It comes up with the rest of the stack (`just dev-docker`). Point logchef at it
via config or env:

```bash
LOGCHEF_DATABASE__DRIVER=postgres \
LOGCHEF_POSTGRES__DSN='postgres://logchef:logchef@localhost:5432/logchef?sslmode=disable' \
  just run-backend
```

Connect a psql shell for inspection:

```bash
docker exec -it dev-postgres-1 psql -U logchef -d logchef
```

The data lives in the `postgres-data` volume; `just dev-clean` removes it.

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
