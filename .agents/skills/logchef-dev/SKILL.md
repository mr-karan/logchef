---
name: logchef-dev
description: "Set up and manage the LogChef local development environment. Use this skill whenever the user mentions dev setup, dev environment, running logchef locally, starting the backend/frontend, docker compose, seeding data, resetting the dev database, or wants to get logchef running for development. Also trigger when the user asks about login credentials, Dex OIDC setup, ClickHouse dev tables, or sample log ingestion."
---

# LogChef Dev Environment

Set up and run the LogChef development environment. LogChef is a Go (Fiber) backend + Vue 3 (TypeScript/Pinia) frontend for log analytics backed by ClickHouse.

## Prerequisites

- Docker (for ClickHouse, Dex OIDC, Mailpit, webhook receiver)
- `just` task runner
- Go 1.24+, bun, sqlite3, and build tools on PATH
- `vector` on PATH for log ingestion

## Quick Start (Full Setup)

Run these in order. Use tmux to keep services running.

### 1. Start infrastructure

```bash
cd /home/karan/Code/Personal/logchef
just dev-docker-detach
```

This starts: ClickHouse (:8123/:9000), Dex OIDC (:5556), Mailpit (:8025/:1025), webhook receiver (:8888).

Wait for ClickHouse to be healthy:
```bash
docker exec dev-clickhouse-local-1 clickhouse-client --query "SELECT 1"
```

### 2. Create ClickHouse tables

```bash
just dev-init-tables
```

### 3. Build the app

```bash
just build
```

### 4. Start backend (creates SQLite DB on first run)

Use tmux for persistent sessions:
```bash
tmux new-session -d -s logchef-dev -n backend
tmux send-keys -t logchef-dev:backend "cd /home/karan/Code/Personal/logchef && just run-backend" Enter
```

Wait for the backend to be ready:
```bash
curl -s http://localhost:8125/api/v1/health
```

### 5. Start frontend

```bash
tmux new-window -t logchef-dev -n frontend
tmux send-keys -t logchef-dev:frontend "cd /home/karan/Code/Personal/logchef/frontend && bun run dev" Enter
```

Frontend available at http://localhost:5173

### 6. Seed data (if needed)

The dev config uses provisioning (`dev/provisioning.toml`) to automatically create teams, sources, and link OIDC users on backend startup. If you need to seed the SQLite database with the `dev@localhost` API user:

```bash
cd /home/karan/Code/Personal/logchef
LOGCHEF_DB_PATH=./local.db ./dev/seed.sh
```

### 7. Ingest sample logs

```bash
cd /home/karan/Code/Personal/logchef/dev
vector -c http.toml & vector -c syslog.toml & sleep 60; kill %1 %2 2>/dev/null; wait
```

Or for continuous ingestion in background:
```bash
cd /home/karan/Code/Personal/logchef/dev
vector -c http.toml &
vector -c syslog.toml &
```

Alternatively, insert sample data directly:
```bash
docker exec dev-clickhouse-local-1 clickhouse-client --query "
INSERT INTO default.http SELECT
  now() - toIntervalSecond(number * 2 + rand() % 3),
  arrayElement(['api.logchef.dev','web.logchef.dev','cdn.logchef.dev'], (rand()%3)+1),
  arrayElement(['GET','POST','PUT','DELETE','GET','GET'], (rand()%6)+1),
  'HTTP/1.1', '-',
  arrayElement(['/api/v1/health','/api/v1/logs/query','/api/v1/teams','/api/v1/sources'], (rand()%4)+1),
  arrayElement([200,200,201,400,404,500], (rand()%6)+1),
  arrayElement(['admin','demo','-'], (rand()%3)+1),
  toUInt32(rand()%50000)
FROM numbers(5000)"
```

## Login Credentials

Authentication uses Dex OIDC. Static passwords configured in `dev/dex/config.yaml`:

| Email | Password | Role |
|-------|----------|------|
| `admin@logchef.internal` | `password` | admin |
| `demo@logchef.internal` | `password` | member |

## Key Config Files

| File | Purpose |
|------|---------|
| `config.toml` | Main app config (OIDC, server, SQLite path, provisioning) |
| `dev/provisioning.toml` | Declarative teams, sources, and user-team memberships |
| `dev/dex/config.yaml` | Dex OIDC provider config (static users, clients) |
| `dev/docker-compose.yml` | Infrastructure services |
| `dev/init-clickhouse.sql` | ClickHouse table schemas |
| `dev/seed.sh` | SQLite seed script (API tokens, dev user) |
| `dev/http.toml` | Vector config for HTTP demo logs |
| `dev/syslog.toml` | Vector config for syslog demo logs |

## Dev Config Notes

- `config.toml` has `secure_cookie = false` under `[server]` for HTTP dev
- Provisioning auto-links `admin@logchef.internal` and `demo@logchef.internal` to "Dev Team"
- ClickHouse tables: `default.http` (HTTP access logs) and `default.syslogs` (syslog format)
- SQLite database: `local.db` in project root

## Services & Ports

| Service | Port | Description |
|---------|------|-------------|
| Backend API | 8125 | Go/Fiber HTTP server |
| Frontend (Vite) | 5173 | Vue 3 dev server |
| ClickHouse HTTP | 8123 | HTTP interface |
| ClickHouse Native | 9000 | Native protocol |
| Dex OIDC | 5556 | OpenID Connect provider (issuer: `http://localhost:5556/dex`) |
| Mailpit UI | 8025 | Email inbox for testing |
| Mailpit SMTP | 1025 | SMTP server |
| Webhook Receiver | 8888 | Test webhook endpoint |

## Common Operations

### Reset everything
```bash
just dev-clean    # Stops docker, removes volumes, deletes local.db
just dev-setup    # Fresh start (docker + tables + seed)
```

### Rebuild after code changes
```bash
# Backend only
just run-backend

# Frontend only (hot-reload, usually automatic)
cd frontend && bun run dev

# Full rebuild
just build
```

### Check ClickHouse data
```bash
docker exec dev-clickhouse-local-1 clickhouse-client --query "SELECT count() FROM default.http"
docker exec dev-clickhouse-local-1 clickhouse-client --query "SELECT count() FROM default.syslogs"
```

### Run checks before committing
```bash
just check
```

### Frontend type check / build
```bash
cd frontend && bun run typecheck
cd frontend && bun run build
```

## Troubleshooting

### "address already in use" on port 8125
```bash
ss -tlnp | grep 8125 | grep -oP 'pid=\K[0-9]+' | xargs -r kill -9
```

### Can't login / auth_error redirect
- Check Dex is running: `curl http://localhost:5556/dex/.well-known/openid-configuration`
- Verify `config.toml` has `secure_cookie = false` under `[server]`
- Verify OIDC URLs in `config.toml` match Dex config

### "You don't have access to any teams"
- Check `dev/provisioning.toml` has the user's email in a team's members list
- Restart backend to trigger provisioning reconciliation

### sqlite3 not found (during seed.sh)
- Install `sqlite` via your package manager (e.g. `yay -S sqlite`)
- Or set DB path: `LOGCHEF_DB_PATH=./local.db ./dev/seed.sh`

### No logs in explorer
- Check ClickHouse has data: `docker exec dev-clickhouse-local-1 clickhouse-client --query "SELECT count() FROM default.http"`
- Ingest logs with vector or direct INSERT (see step 7 above)
- Check the time range in the UI covers when data was inserted
