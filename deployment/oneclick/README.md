# One-click deploy assets

Shared images used by the one-click deploy targets:

- `clickhouse/` — ClickHouse server image with the `default.logs` table schema baked in
  (created on first boot) and a data directory compatible with PaaS volume mounts
  at `/data/db`.
- `vector/` — demo log generator that ships synthetic syslog into ClickHouse so a
  freshly deployed stack has data to explore immediately. Remove it once you
  connect a real ingestion pipeline.

Both are referenced by:

- The Render blueprint (`render.yaml` at the repo root) — deployed via the
  "Deploy to Render" button in the README.
- The Railway template — see `../railway/README.md`.

## First login

The stack deploys with Logchef's built-in email + password auth:

- **Email**: the `LOGCHEF_AUTH__LOCAL__ADMIN_EMAIL` you were prompted for.
- **Password**: `LOGCHEF_AUTH__LOCAL__ADMIN_PASSWORD` (generated — visible in the
  service's environment variables in the Render/Railway dashboard).

## Connect the ClickHouse source

Logchef is datasource-first: after logging in, add the ClickHouse source from the
UI (**Sources → Add Source**):

| Field | Value |
| --- | --- |
| Host | the ClickHouse private hostname (see `CLICKHOUSE_HOST` on the vector service, or the dashboard) |
| Port | `9000` (native TCP interface) |
| Username | `logchef` |
| Password | value of `CLICKHOUSE_PASSWORD` |
| Database | `default` |
| Table | `logs` |

The demo generator starts writing rows immediately, so the source shows data as
soon as it is connected.
