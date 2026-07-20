---
title: Database Backends
description: Choose between the default SQLite metadata store and the opt-in Postgres backend for multi-replica deployments
---

Logchef stores its **application metadata** (users, teams, sources, sessions,
saved queries, collections, alerts, API tokens, settings, export jobs, and query
shares) in a relational database. (Your **logs** always live in ClickHouse and
are unaffected by this choice.)

Two backends are supported:

| Backend | Use when | Notes |
|---------|----------|-------|
| **SQLite** (default) | Single instance | Zero-config, single binary, embedded file. The default. |
| **Postgres** (opt-in) | Multiple replicas / HA | Shared metadata so any replica serves any request. |

SQLite remains the default. A single-binary, zero-config start is preserved. You
only need Postgres if you run **more than one Logchef replica** against shared
state (e.g. behind a load balancer for availability).

## Configuring the backend

Select the backend with `database.driver` in `config.toml`:

```toml
[database]
driver = "sqlite"   # "sqlite" (default) | "postgres"

# Used when driver = "sqlite"
[sqlite]
path = "local.db"

# Used when driver = "postgres"
[postgres]
dsn = "postgres://user:password@host:5432/logchef?sslmode=require"
max_open_conns    = 25
max_idle_conns    = 5
conn_max_lifetime = "30m"
```

Or via environment variables:

```bash
LOGCHEF_DATABASE__DRIVER=postgres
LOGCHEF_POSTGRES__DSN='postgres://user:password@host:5432/logchef?sslmode=require'
```

Logchef validates the selection on startup and exits with a clear error if
`driver = "postgres"` is set without a DSN.

### Migrations

On startup Logchef applies any pending schema migrations automatically. The
Postgres backend acquires a **PostgreSQL advisory lock** before migrating, so
multiple replicas starting at once will not race: only one migrates while the
others wait, then all proceed.

## High-availability caveats

Shared metadata in Postgres is necessary for multi-replica operation, but it is
**not sufficient on its own**. Before running more than one replica, understand
these constraints:

### Run exactly one alert-manager replica

Alert evaluation runs on a per-replica timer. Postgres does **not** coordinate
this: if N replicas each run the alert manager, every alert is evaluated N times,
producing duplicate notifications. Until leader election is added, run the alert
evaluation loop on **exactly one** replica (e.g. a dedicated instance, or disable
alerts on all but one). See [Alerting](/features/alerting) for the `alerts.enabled`
switch used to disable evaluation on the other replicas.

### Source-cache is per-replica

Each replica maintains its own in-memory cache of ClickHouse source connections.
A source added or edited on one replica is not instantly reflected on the others;
they pick up the change on their next cache refresh. Plan source changes
accordingly.

### Sessions and writes are shared

Authentication sessions and all metadata writes live in Postgres, so a user's
session and data are visible across replicas: this is exactly the property that
makes multi-replica serving work.

## Development

The dev stack ships a Postgres 17 service. See the
[development setup](/contributing/setup) and run against it with:

```bash
LOGCHEF_DATABASE__DRIVER=postgres \
LOGCHEF_POSTGRES__DSN='postgres://logchef:logchef@localhost:5432/logchef?sslmode=disable' \
  just run-backend
```
