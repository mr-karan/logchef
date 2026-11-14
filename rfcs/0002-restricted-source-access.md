# RFC 0002: Restricted Access to Shared Sources

Status: Draft
Date: 2026-11-03
Authors: Karan Sharma

## Summary

Several apps often write to one shared log table. Logchef grants access per
source, so a team linked to that source sees every app's rows. Teams want a
restricted view: team `payments` queries the shared table but sees only rows
where `namespace = 'payments'`, by every path Logchef offers.

This RFC proposes that a restricted view is its own Logchef source, and that
the log database enforces the row filter, not Logchef. For ClickHouse that is
a dedicated read-only user that can read only a filtered view (or a table
guarded by a row policy). Logchef needs a small set of changes to make this
safe and pleasant, listed under "Logchef changes".

An app-level filter on the team-source link is rejected for v1. It is
described under "Alternatives" with the reasons.

## Why the filter cannot live in Logchef today

Raw SQL is a deliberate feature. ClickHouse SQL goes through
`NewExtendedQueryBuilder`, which accepts any single `SELECT` and states that
"ClickHouse connection permissions are the security boundary"
(`internal/clickhouse/query_builder.go:219`). Tests expect other tables, JOINs,
CTEs and subqueries to pass (`internal/clickhouse/query_builder_test.go:10`).
An appended `WHERE namespace = 'payments'` is bypassed by:

- naming the table again in a scalar subquery, CTE, JOIN or `UNION ALL` branch;
- aliasing a literal: `SELECT msg, 'payments' AS namespace FROM t` defeats an
  outer wrapper filter;
- table functions such as `remote()`, `cluster()`, `merge()` or `url()`, when
  the connection user holds those privileges;
- querying any other object the connection user can read, including system
  log tables.

The row filter would also need to reach about 20 independent read paths, each
of which builds its own query from a source ID alone:

| Path | Builds its own query at |
|---|---|
| Log context (surrounding lines) | `internal/clickhouse/logs.go:499` |
| Field values and distinct counts | `internal/clickhouse/field_values.go:96`, `:244` |
| Histogram over caller SQL | `internal/clickhouse/logs.go:129` |
| Streaming export (bypasses the datasource service) | `internal/server/export_handlers.go:83` |
| Export jobs | `internal/server/export_job_handlers.go:236` |
| Alert test and scheduler | `internal/core/alerts.go:487`, `internal/alerts/manager.go:193` |
| ClickHouse tail polls | `internal/datasource/clickhouse_tail.go:256` |
| Table stats from `system.parts` | `internal/clickhouse/stats.go:60` |

Core receives an `AuthorizedSource` but passes only `SourceID()` to the
datasource layer (`internal/core/logs.go:12`), so no single choke point exists.
The database already is one: every read reaches it as the connection user.

## Proposal

### Model

A restricted view is a normal source with its own ID, name, connection user
and table name. It is linked to the restricted team like any other source.
The shared source stays linked only to teams that may read all rows.

```
shared_apps (source A, broad user)      -> linked to: platform
payments_apps (source B, payments user) -> linked to: payments
```

Nothing about teams, roles, saved queries, alerts, dashboards, exports or
shares changes. Each already keys on source ID, so artifacts created on source
B stay scoped to source B.

### ClickHouse setup (option 1, no Logchef code)

A `SQL SECURITY DEFINER` view gets a distinct table name. The restricted user
gets `SELECT` on the view and nothing else.

```sql
CREATE VIEW logs.payments_apps
DEFINER = logs_view_owner SQL SECURITY DEFINER
AS SELECT * FROM logs.shared_apps WHERE namespace = 'payments';

CREATE USER logchef_payments IDENTIFIED BY '<from secret manager>'
  SETTINGS readonly = 2;
GRANT SELECT ON logs.payments_apps TO logchef_payments;
```

- `logs_view_owner` holds `SELECT` on `logs.shared_apps` only. The definer
  user reads the base table, so `logchef_payments` never needs that grant.
- `readonly = 2` blocks writes and DDL but still allows Logchef's per-query
  settings such as `max_execution_time`.
- Turn on `access_control_improvements.select_from_system_db_requires_grant`
  so the restricted user cannot read `system.query_log` or `system.parts`.
- A plain view has no sorting key. Logchef's recent-activity panel needs a
  leading timestamp key (`internal/datasource/clickhouse_provider.go:586`), so
  it will be unavailable for the view. Queries still push the time filter into
  the base table.

### ClickHouse setup (option 2, row policy, needs one Logchef change)

A row policy on the base table keeps the original table name and its sorting
key:

```sql
CREATE ROW POLICY payments_only ON logs.shared_apps
  FOR SELECT USING namespace = 'payments' AS RESTRICTIVE TO logchef_payments;
GRANT SELECT ON logs.shared_apps TO logchef_payments;
```

- Users without a policy keep reading all rows, because
  `users_without_row_policies_can_read_rows` defaults to true. Existing
  sources are unaffected.
- `RESTRICTIVE` is ANDed with every other policy, so a later permissive policy
  cannot widen it.
- A `Distributed` table cannot apply the policy itself. Define the policy on
  the local tables `ON CLUSTER`, and pin `serialize_query_plan = 0` as a
  constant setting for the user, because a serialized plan skips remote row
  policies.
- Do not grant the user extra roles. `SET ROLE` changes which policies apply.

This needs a Logchef change: the ClickHouse identity key is
`host/database/table` without the username (`pkg/models/source.go:264`), so a
second source on the same table is rejected as a duplicate
(`internal/datasource/service.go:132`).

### VictoriaLogs

Sources already carry tenant IDs and an immutable scope query, sent as
`extra_filters` or `extra_stream_filters` (`internal/victorialogs/querying.go:1095`).
A restricted VictoriaLogs view is a second source with the restriction in its
scope. This still needs verification against the VictoriaLogs docs and a real
backend:

- that extra filters apply to every nested read and to all endpoints Logchef
  uses (`/query`, `/hits`, `/facets`, `/tail`, stream fields);
- that no user-supplied LogsQL can widen them.

Until both are verified, treat a VictoriaLogs scope as a convenience, not a
security boundary.

## Logchef changes

1. **Only global admins link sources to teams.** Today a team admin can link
   any source in the instance by ID: `handleLinkSourceToTeam` accepts a bare
   `source_id` and `AddTeamSource` checks only that it exists
   (`internal/server/team_handlers.go:310`, `internal/core/teams.go:355`).
   This is an existing privilege escalation, and it defeats any restricted
   view: a team admin could link the shared source. Team admins keep managing
   members. Update `TeamSettings.vue` to hide the link control for them.
2. **Degrade cleanly when the connection user lacks system grants.** Table
   stats, inspection and schema extras must report "unavailable" instead of
   failing the source when `system.parts` or `system.tables` is denied.
3. **(Option 2 only) Include the username in the ClickHouse identity key.**
   Add a migration that recomputes `identity_key` in both metadata stores.
4. **Mark restricted sources in the UI.** Add an optional, display-only
   `access_note` on the source (for example `namespace = payments`). Members
   see it as a locked chip in Explore. It is informational. The database
   enforces the restriction.
5. **Docs.** A guide with the DDL above, the verification checklist below,
   and a warning that a view queried with a broad user isolates nothing.

Items 1 and 2 are required before a restricted team gets a team admin.
Items 3 to 5 are independent.

## Verification checklist (per restricted source)

Run these as the restricted ClickHouse user, outside Logchef. Every query must
fail with a permission error or return only allowed rows. Logchef passes SQL
through, so passing here covers raw SQL mode.

- `SELECT count() FROM logs.shared_apps` (option 1: denied)
- `SELECT DISTINCT namespace FROM <restricted table>` returns only the allowed value
- Scalar subquery, CTE, JOIN and `UNION ALL` reading the base table
- `remote()`, `cluster()`, `merge()`, `url()`, `file()`
- `SELECT * FROM system.query_log`, `system.parts`, `system.tables`
- `SET serialize_query_plan = 1` (option 2: rejected)
- `SET ROLE ALL`, `CREATE TEMPORARY VIEW`, any `INSERT` or `ALTER`

Then, in Logchef, as a member of the restricted team: Explore in both modes,
histogram, field values, log context, tail, export, an alert test, Ask Chef
and an MCP query must show no other namespace.

## Alternatives

### Mandatory filter on the team-source link (rejected for v1)

Store a typed filter such as `[{field: namespace, equals: payments}]` on
`team_sources` and apply it in one execution boundary. This keeps a single
source ID. It is secure only if Logchef also:

- blocks raw ClickHouse SQL for every principal that holds any restricted
  grant, because another source's broad user can read the same table;
- moves export, alerts, context, tail, field values and stats behind a
  grant-aware execution handle;
- binds alerts, alert history, export artifacts, VictoriaLogs window cursors
  and caches to the grant and a policy revision;
- requires structured histogram, alert and export requests instead of SQL
  that the client sent back.

That is a large change that touches every read path, and it removes SQL mode
for exactly the teams that need it. Revisit it only if one source ID with
different per-team scopes becomes a hard requirement.

### Rewrite arbitrary SQL

Rewriting the AST recursively over CTEs, subqueries, set operations, table
functions and dictionaries is fragile and tracks the ClickHouse dialect
forever. Rejected.

### Parameterized views

Source table names must be plain identifiers
(`internal/datasource/validation.go:44`), and a parameter the member supplies
restricts nothing. Rejected.

## Related findings (separate fixes)

- Collection participants receive the full saved-query text for sources they
  cannot access. Only `Runnable` is false (`internal/core/collections.go:423`).
- `/metrics` is served without authentication and includes per-source
  returned-row histograms (`internal/server/server.go:280`). Low severity, but
  it should be internal-only.

## Open questions

1. Is a separate source per restricted team acceptable, or must one source ID
   carry per-team scopes? A "yes" to the second makes the rejected
   alternative necessary.
2. Can apps forge `namespace` at ingestion? If so, the row filter does not
   prove ownership.
3. Which ClickHouse version and engine back the shared table? Option 1 needs
   `SQL SECURITY DEFINER` support. Option 2 changes if the table is
   `Distributed`.
4. Should Logchef generate the DDL from the admin UI later? Wait until more
   than a few teams need restricted views.
