---
title: Database Backends
head:
  - tag: title
    content: "SQLite and Postgres Metadata Stores | Logchef"
description: "Choose SQLite or Postgres for Logchef metadata. Configure the default local store or use Postgres to share metadata across multiple replicas."
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

### Background jobs run on any replica

Alert evaluation runs on the background job runner. Each rule is a job in the
metadata database with its own frequency. Every replica polls for due jobs
(`[jobs] poll_interval`, default `2s`) and claims them with a lease, so one
replica evaluates a given tick and the load spreads across replicas. A replica
whose claim was taken over, for example after it stalled past its lease, cannot
commit alert state or history.

Set `[jobs] enabled = false` (`LOGCHEF_JOBS__ENABLED=false`) on a replica that
should serve HTTP only. `[alerts] max_concurrent_evaluations` (default `4`)
bounds the rules one replica evaluates at once.

An evaluation sends nothing itself. It writes its notifications as delivery
rows in the same transaction as the rule's state, and the `notify.deliver` job
sends them. Each row is reserved by one replica at a time, and rows for the
same group go out in order. A replica that stalls or dies after a webhook,
Slack or PagerDuty request was accepted and before its result was written
sends that message again once the reservation ends; an email is never sent
twice and is marked `unknown` instead. See
[Delivery and retries](/features/alerting/#delivery-and-retries).

This release cannot be rolled out one replica at a time when upgrading from
2.7: stop every old replica before the first new one starts. See
[Upgrading from 2.7](/features/alerting/#upgrading-from-27).

[Scheduled reports](/features/scheduled-reports/) use the same runner, one job
per report. The report's run row is its claim: one row per scheduled tick and
one running run per report, so two replicas never send the same edition, and a
manual run answers 409 while any replica runs that report. A replica whose claim
was taken over writes nothing; the cleanup marks its run `interrupted` two
minutes after the run's deadline. `[reports] max_concurrent_runs` (default `2`)
bounds the runs one replica executes at once.

### Give each export replica a stable identity

Set `export.instance_id` separately for each replica, for example with
`LOGCHEF_EXPORT__INSTANCE_ID` from a stable per-replica identity. Never set it in a
config file shared by replicas. Duplicate IDs make each replica fail the other's
live exports at startup. Leave it empty when no stable identity exists, so cleanup
uses job age only.

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
[development setup](/contributing/setup/) and run against it with:

```bash
LOGCHEF_DATABASE__DRIVER=postgres \
LOGCHEF_POSTGRES__DSN='postgres://logchef:logchef@localhost:5432/logchef?sslmode=disable' \
  just run-backend
```
