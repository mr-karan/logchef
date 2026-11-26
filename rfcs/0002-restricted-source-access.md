# RFC 0002: Restricted Access to Shared Sources

Status: Draft
Date: 2026-11-03
Revised: 2026-11-22
Authors: Karan Sharma

## Summary

Several apps often write to one shared log table. Logchef grants access per
source, so a team linked to that source sees every app's rows. Teams want a
restricted view: team `payments` queries the shared table but sees only rows
where `namespace = 'payments'`, by every path Logchef offers.

A restricted view is its own Logchef source, and the log database enforces the
row filter, not Logchef. ClickHouse uses a dedicated read-only user behind a
definer view or a row policy. VictoriaLogs uses the source's scope query. Loki
uses one tenant per source (RFC 0011).

This revision checks that decision against the code merged since the first
draft. It lists every path that reads source rows, names the audit events, and
plans eleven units. Unit R1 makes source links a global-admin action, and unit
L17 of RFC 0011 waits for it.

## Problem

Logchef authorizes a caller for a team and a source, then reads as that
source's stored connection. `requireTeamHasSource` builds an
`access.AuthorizedSource` (`internal/server/middleware.go:431`), and
`datasource.Service` loads the source by ID for every read
(`internal/datasource/service.go:635`). Each ClickHouse client belongs to one
source ID (`internal/clickhouse/manager.go:257`). No layer holds a row filter.

Raw SQL is a deliberate feature. `NewExtendedQueryBuilder` accepts any single
`SELECT` and states that "ClickHouse connection permissions are the real
security boundary" (`internal/clickhouse/query_builder.go:84`). An appended
`WHERE namespace = 'payments'` is bypassed by any of these:

- the table named again in a scalar subquery, CTE, JOIN or `UNION ALL` branch
- an aliased literal, as in `SELECT msg, 'payments' AS namespace FROM t`,
  which defeats an outer wrapper filter
- table functions such as `remote()`, `cluster()`, `merge()` or `url()`, when
  the connection user holds those privileges
- any other object the connection user can read, including system log tables

The database is the one place where a filter covers raw SQL, because every
read reaches it as the connection user. The merged code leaves six gaps
between that decision and a restricted source that is safe to run.

| Gap | Evidence |
|---|---|
| A team admin can link a source that they can read to their own team. A user who is a member of `platform` and an admin of `payments` can link the shared source to `payments`, and every member of `payments` then reads all rows | `internal/core/teams.go:392`, `internal/server/server.go:502` |
| The ClickHouse identity key is `host/database/table` with no user, so a second source on the same table is refused as a duplicate | `pkg/models/source.go:344`, `internal/datasource/service.go:138` |
| ClickHouse answers a read outside the grant with `ACCESS_DENIED`, code 497. That code is not in the list of public query errors, so the caller gets a 500 and the `query.run` event says `internal_error` | `internal/clickhouse/query_error.go:28`, `internal/server/query_errors.go:70`, `internal/server/query_run.go:353` |
| The recent-activity route maps two errors to "unavailable". A denied grant is neither, so the route answers 500 | `internal/server/source_handlers.go:244` |
| Every VictoriaLogs call site adds the scope by calling `applyScopeFilters` itself. The request function does not, so a new call site can leave it out | `internal/victorialogs/querying.go:1171`, `internal/victorialogs/querying.go:1120` |
| No test proves that a restricted source returns none of the other rows, and a source carries no mark that tells a member why rows are missing | `internal/datasource/providertest/providertest.go:68`, `rfcs/0008-scheduled-reports.md:452` |

## Goals and non-goals

Goals:

1. A member of a restricted team reads only the permitted rows through every
   Logchef path, raw SQL included.
2. Only a global admin or the provisioning file decides which team reads
   which source.
3. A path that cannot work on a restricted source answers "unavailable". It
   never fails the source and never answers 500.
4. A test in CI proves the isolation of each backend, path by path.
5. A restriction change and a denial are audit events in the RFC 0009
   envelope.

Non-goals:

- A row filter inside Logchef. "Alternatives rejected" has the reasons.
- One source ID that carries a different scope per team.
- DDL generated from the admin UI. See open question 4.
- A restricted boundary inside one Loki tenant. RFC 0011 owns that decision.
- Control over what a member sends out of the team. An export, an alert
  destination, an external report recipient, a query share and an Ask Chef
  turn carry the rows that the member can read. A restricted source behaves
  as every other source does.
- Proof that an app cannot forge the filter column at ingestion. See open
  question 2.

## Design

### Model and stored data

A restricted view is a normal source with its own ID, name, connection user
and table name. It is linked to the restricted team like any other source.
The shared source stays linked only to teams that may read all rows.

```
shared_apps   (source A, broad user)    -> linked to: platform
payments_apps (source B, payments user) -> linked to: payments
```

Nothing about teams, roles, saved queries, alerts, dashboards, saved views,
reports, exports or shares changes. Each already keys on the source ID, so an
object created on source B stays on source B.

The source gains one stored field.

| Field | Type | Meaning |
|---|---|---|
| `access_note` | text, not null, default `''` | A display-only label such as `namespace = payments`. At most 200 characters, one line, no control characters. It restricts nothing |

Migrations, for both metadata stores, each with the next free number at build
time:

| Migration | SQLite and Postgres |
|---|---|
| `add_source_access_note` | `ALTER TABLE sources ADD COLUMN access_note TEXT NOT NULL DEFAULT ''`. The down migration drops the column |
| `clickhouse_identity_key_username` | Appends `\|user=<username>` to `identity_key` of every ClickHouse source whose key has no such suffix. SQLite reads the user with `json_extract(connection_config, '$.username')`, and Postgres with `connection_config->>'username'`. Both trim it, as the Go function does. The guard on the suffix makes the statement safe to run twice. The down migration cuts the suffix, and it fails while two sources share a table |

The unique index on `identity_key` stays
(`internal/store/sqlite/migrations/000047_allow_loki_source_type.up.sql:46`).

Models in `pkg/models/source.go`:

- `Source.AccessNote string` with `db:"access_note"` and
  `json:"access_note,omitempty"`, and the same field on `SourceResponse`.
- `CreateSourceRequest.AccessNote string` and
  `UpdateSourceRequest.AccessNote *string`.
- `BuildClickHouseIdentityKey` returns
  `clickhouse:<host>/<database>/<table>|user=<username>`. The user name is
  trimmed and keeps its case, because ClickHouse user names are
  case-sensitive.

`config.ProvisionSource` gains `AccessNote` with the koanf key `access_note`
(`internal/config/provisioning.go:71`).

### ClickHouse setup, option 1: a definer view

Option 1 needs no Logchef code. A `SQL SECURITY DEFINER` view gets a distinct
table name. The restricted user gets `SELECT` on the view and nothing else.

```sql
CREATE VIEW logs.payments_apps
DEFINER = logs_view_owner SQL SECURITY DEFINER
AS SELECT * FROM logs.shared_apps WHERE namespace = 'payments';

CREATE USER logchef_payments IDENTIFIED BY '<from secret manager>'
  SETTINGS readonly = 2;
GRANT SELECT ON logs.payments_apps TO logchef_payments;
```

- `logs_view_owner` holds `SELECT` on `logs.shared_apps` only. The definer
  reads the base table, so `logchef_payments` never needs that grant.
- The `DEFINER` clause is required. A view created without it runs with the
  permissions of the caller, and the caller would then need a grant on the
  base table. The clause exists from ClickHouse 24.2.
- `readonly = 2` blocks writes and DDL and still allows the per-query
  settings that Logchef sends, such as `max_execution_time`.
- Keep `access_control_improvements.select_from_system_db_requires_grant` on.
  The restricted user then cannot read `system.query_log` or `system.parts`.
  `system.tables` and `system.columns` stay readable and show only the
  objects that the user holds a grant on. `system.settings` stays readable
  too. Logchef's health check, schema read and capability probes use those
  three tables
  (`internal/clickhouse/health.go:43`, `internal/clickhouse/schema.go:133`,
  `internal/clickhouse/capabilities.go:145`).
- A plain view has no sorting key. The recent-activity panel needs a leading
  timestamp key (`internal/datasource/clickhouse_provider.go:660`), so it is
  unavailable for the view unless the source sets
  `connection.activity.allow_view`
  (`internal/datasource/clickhouse_provider.go:647`). Queries still push the
  time filter into the base table.
- A view can also drop columns. A restricted team then cannot read them.

### ClickHouse setup, option 2: a row policy

A row policy on the base table keeps the original table name and its sorting
key. Option 2 needs unit R3.

```sql
CREATE ROW POLICY payments_only ON logs.shared_apps
  FOR SELECT USING namespace = 'payments' AS RESTRICTIVE TO logchef_payments;
GRANT SELECT ON logs.shared_apps TO logchef_payments;
```

- Users without a policy keep reading all rows, because
  `users_without_row_policies_can_read_rows` defaults to true. Existing
  sources are unaffected.
- `RESTRICTIVE` is ANDed with every other policy, so a later permissive
  policy cannot widen it.
- A `Distributed` table cannot apply the policy itself. ClickHouse 26.8
  rejects the query with `ILLEGAL_PREWHERE` when the policy sits on the
  `Distributed` table. Define the policy on the local tables `ON CLUSTER`.
- The local tables apply the policy to the user that runs the query on the
  shard. That user is the restricted user only when the cluster definition
  sets a `secret`. Without a `secret` the shard runs the query as the user in
  the cluster definition, and the policy does not apply. Unit R10 tests both.
- Pin `serialize_query_plan = 0` as a `CONST` setting of the user, on a
  server that has the setting. A serialized plan skips the row policies of
  the remote server, and `readonly = 2` lets a query set it otherwise.
- Do not grant the user extra roles. `SET ROLE` changes which policies apply.

The statements above were checked against the ClickHouse 26.8 documentation
at tag `v26.8.2.7-lts`, the newest version in the CI matrix
(`.github/workflows/go-tests.yml:149`):

- views and `SQL SECURITY`:
  https://github.com/ClickHouse/ClickHouse/blob/v26.8.2.7-lts/docs/reference/statements/create/view.mdx
- row policies and `Distributed` tables:
  https://github.com/ClickHouse/ClickHouse/blob/v26.8.2.7-lts/docs/reference/statements/create/row-policy.mdx
- `access_control_improvements`:
  https://github.com/ClickHouse/ClickHouse/blob/v26.8.2.7-lts/docs/reference/settings/server-settings/settings/access-control.mdx
- `readonly`:
  https://github.com/ClickHouse/ClickHouse/blob/v26.8.2.7-lts/docs/concepts/features/configuration/settings/permissions-for-queries.mdx
- the cluster `secret`:
  https://github.com/ClickHouse/ClickHouse/blob/v26.8.2.7-lts/docs/reference/engines/table-engines/special/distributed.mdx
- the system tables that need no grant:
  https://github.com/ClickHouse/ClickHouse/blob/v26.8.2.7-lts/src/Access/ContextAccess.cpp

### VictoriaLogs

A source carries tenant IDs and a scope query. Logchef sends the scope as
`extra_filters`, or as `extra_stream_filters` when it is a stream selector
(`internal/victorialogs/querying.go:1171`). A restricted VictoriaLogs view is
a second source with the restriction in its scope. The scope is part of the
identity key (`pkg/models/source.go:367`), so both sources can exist.

The first draft asked for two checks. The VictoriaLogs v1.52.0 documentation,
the version of the dev stack (`dev/docker-compose.yml:34`), answers both:

- "All the HTTP querying APIs" accept both parameters.
- Both are "global constraints" that are "unconditionally propagated into all
  the subqueries", including `join`, `union` and `in(<query>)`.

Sources:
https://github.com/VictoriaMetrics/VictoriaLogs/blob/v1.52.0/docs/victorialogs/querying/README.md
and
https://github.com/VictoriaMetrics/VictoriaLogs/blob/v1.52.0/docs/victorialogs/security-and-lb.md

Two limits remain. VictoriaLogs enforces the filter, but Logchef attaches it,
so the boundary holds only if every request carries it. Unit R5 moves the
call into the request function and tests every endpoint. The filter also does
not bind the credential. A deployment that needs the filter bound to a
credential puts it in the `url_prefix` of a vmauth user, as the second page
above describes, and gives that credential to the restricted source.

Until R5 merges, treat a VictoriaLogs scope as a convenience, not a security
boundary.

### Loki

RFC 0011 decided that the tenant is the one boundary for a Loki source. The
single request function sets the tenant header on every request
(`internal/loki/querying.go:48`, `internal/loki/provider.go:512`). The source
`selector` is not a boundary. Unit L17 of RFC 0011 verifies tenant isolation,
and "Seams" states what it relies on.

### Every path that reads source rows

Each path below reads through `datasource.Service` or through the ClickHouse
client of the same source ID. None names another source. The last column says
how the restriction reaches the path, or that the path is unavailable.

Query and exploration:

| Path | Entry and read | On a restricted source |
|---|---|---|
| Native query, buffered | `internal/server/logs_handlers.go:194`, `internal/core/logs.go:13`, `internal/datasource/clickhouse_provider.go:313` | The database filters every table that the text names. A read outside the grant answers 403 after R2 |
| Native query, streamed, and dashboard panels | `internal/server/query_stream.go:288`, `internal/server/dashcache.go:170` | Same. The cache key holds the team, the source and the source revision (`internal/cache/dashcache.go:284`), so no entry crosses sources |
| LogchefQL query and aggregations | `internal/server/logchefql_handlers.go:175`, `internal/core/query.go:328` | The compiled query reads only the source table. The filter applies before `GROUP BY` |
| Histogram | `internal/server/histogram_handlers.go:23`, `internal/core/query.go:509`, `internal/clickhouse/logs.go:101` | The histogram wraps the caller's SQL. The database filters the inner read |
| Log context | `internal/server/log_context_handlers.go:17`, `internal/clickhouse/logs.go:451`, `internal/loki/context.go:28` | Filtered by the database. Neighbouring lines of another app never appear. VictoriaLogs has no log context |
| Live tail | `internal/server/tail_handlers.go:35`, `internal/datasource/clickhouse_tail.go:255`, `internal/victorialogs/tail.go:42`, `internal/loki/tail.go:38` | Each poll or stream is a read as the source |
| Field sidebar and field values | `internal/server/schema_fields_handlers.go:68`, `internal/clickhouse/field_values.go:68`, `internal/victorialogs/querying.go:566` | Values and counts come from the permitted rows only |
| Label preload | `internal/server/schema_fields_handlers.go:157`, `internal/clickhouse/field_values.go:370`, `internal/victorialogs/querying.go:621`, `internal/loki/field_values.go:93` | Same. Loki reads label values of its tenant |
| Windowed search and stream fields | `internal/server/windowed_handlers.go:48`, `internal/victorialogs/windowed.go:578`, `internal/victorialogs/stream_fields.go:16` | VictoriaLogs only. `windowForm` adds the scope to every window |
| Schema | `internal/server/schema_fields_handlers.go:23`, `internal/clickhouse/schema.go:133` | Metadata, no rows. The user sees the columns of the granted object |

Background and derived reads:

| Path | Entry and read | On a restricted source |
|---|---|---|
| Alert evaluation, grouped and no-data | `internal/alerts/evaluate.go:42`, `internal/datasource/service.go:471`, `internal/alerts/samples.go:32` | The scheduler runs the query as the source. Groups and the no-data decision come from that one result |
| Alert test and backtest | `internal/server/alert_handlers.go:305`, `internal/core/visibility.go:82`, `internal/core/alerts.go:651` | Both check that the caller can read the source, then evaluate as the source |
| Triage samples | `internal/notify/webhook_v2.go:77` | Not built. `triage` is always null. Unit P2 of RFC 0006 must read through `datasource.Service` |
| Alert notifications | `internal/alerts/messages.go:46` | A message holds the value, the group values and the query text, and no rows. Group values are permitted values |
| Scheduled reports, HTML and CSV | `internal/core/report_runs.go:498`, `internal/core/report_dashboards.go:161`, `internal/core/report_runs.go:464` | Each run checks that the owner still reaches the source, then reads as the source. A recipient who cannot reach a panel's source gets a locked panel (`internal/core/report_runs.go:613`) |
| Report preview | `internal/server/report_handlers.go:272`, `internal/core/report_runs.go:773` | Same as a run |
| Exports, streamed and as a job | `internal/server/export_handlers.go:76`, `internal/server/export_job_handlers.go:330` | ClickHouse only. Both use the client of the source ID. Only the creator or a global admin downloads a job (`internal/server/export_job_handlers.go:283`) |
| Recent activity | `internal/server/source_handlers.go:218`, `internal/datasource/clickhouse_provider.go:604` | Counts the permitted rows. A view needs `allow_view`. A denied grant answers "unavailable" after R2 |
| Storage stats | `internal/server/source_handlers.go:167`, `internal/clickhouse/stats.go:64` | Unavailable. `system.parts` is denied and the inspection continues without it (`internal/datasource/clickhouse_provider.go:589`) |
| Schema advisor | `internal/server/server.go:420`, `internal/clickhouse/advisor_metadata.go:212` | Global admins only. Unavailable when the system tables are denied (`internal/clickhouse/advisor_metadata.go:747`) |
| Query shapes and Query Activity | `internal/server/query_shapes.go:66`, `internal/server/server.go:428` | Global admins only. Built from Logchef's own query records, never from the log database |

Clients and stored objects:

| Path | Entry and read | On a restricted source |
|---|---|---|
| MCP tools | `internal/mcp/server.go:229`, `internal/mcp/logs.go:87`, `internal/mcp/logchefql.go:72`, `internal/mcp/investigate.go:106`, `internal/mcp/analysis.go:83` | Every tool authorizes the team and source, then calls the same core functions as HTTP |
| Ask Chef | `internal/server/ask_chef_handlers.go:74`, `internal/askchef/tools.go:238` | Runs the MCP tools as the user. The permitted rows go to the configured AI provider |
| AI query generation | `internal/server/ai_sql_handlers.go:21` | Sends the schema and no rows (`internal/server/ai_sql_handlers.go:163`) |
| CLI | `cli/crates/logchef-core/src/api/mod.rs:360` | Calls the HTTP routes above and nothing else |
| Saved views | `internal/core/saved_views.go:193` | Metadata keyed on the source ID. No rows |
| Query shares | `internal/server/query_share_handlers.go:82` | A share stores query text. The reader needs access to the source |
| Query history | `internal/server/user_handlers.go:244` | The caller's own query text. No rows |

### API

| Route | Change | Errors |
|---|---|---|
| `POST /api/v1/teams/:teamID/sources` | Only a global admin passes. Scope `teams:write` as today. An OAuth token never passes, whatever the user's role (`internal/core/access/access.go:149`) | 403 with `models.AuthorizationErrorType` and the message "Global admin access is required to link a source to a team" |
| `DELETE /api/v1/teams/:teamID/sources/:sourceID` | None. A team admin can still unlink | |
| `POST /api/v1/admin/sources`, `PUT /api/v1/admin/sources/:sourceID` | Accept `access_note`. Scope `sources:write` as today | 400 when the note is longer than 200 characters or holds a control character |
| Every route that returns a source | The response includes `access_note` | |
| Every query route, the MCP query tools and exports | A read that the database refuses for lack of a grant answers 403, not 500 | 403 with `models.AuthorizationErrorType` and the message "The source's database user is not allowed to read an object that this query names" |
| `GET /api/v1/teams/:teamID/sources/:sourceID/activity` | A denied grant gives the existing "unavailable" answer | |

### Backend logic

**Source links.** `core.AddTeamSource` returns `access.ErrGlobalAdminRequired`
before it opens the transaction, unless the principal is trusted system work
or `p.RequireGlobalAdmin()` passes. The `requireSourceVisible` branch at
`internal/core/teams.go:392` goes away, because a caller who reaches the
transaction can link any source. The handler parses the body first, so the
denied audit event names the source. The route drops
`requireTeamAdminOrGlobalAdmin`, and the handler maps the error to the 403.
Provisioning links sources inside its own transaction
(`internal/provisioning/reconciler.go:506`) and is unchanged.

**Denied grants.** `classifyQueryError` gains one case, before the default
case. An error for which `clickhouse.IsAccessDeniedError` is true
(`internal/clickhouse/query.go:366`) maps to the 403 above.
`queryFailureReason` maps status 403 to the reason `access_denied`. Both
functions already serve every run mode, so buffered, streamed, windowed,
tail, context, MCP, Ask Chef and export runs get the same answer.
`classifyActivityError` maps the same error to
`ErrSourceActivityUnavailable`. The 403 is not logged as an error.

**Identity key.** `BuildClickHouseIdentityKey` adds the user. The duplicate
check on create and on update compares the new key
(`internal/datasource/service.go:138`,
`internal/datasource/source_management.go:119`).

**VictoriaLogs scope.** `doFormRequest` calls `applyScopeFilters` on the form
that it sends, and so does `probeQueryEndpoint`
(`internal/victorialogs/provider.go:611`). The thirteen calls at the call sites
go away. A new call site then cannot send a request without the scope.

**Isolation suite.** `internal/datasource/providertest` gains
`RunIsolation(t, IsolationHarness)` beside `Run`.

```go
// IsolationHarness describes one restricted source and the rows that it
// must never return.
type IsolationHarness struct {
	Provider datasource.Provider
	// Restricted is the source under test.
	Restricted *models.Source
	Language   models.QueryLanguage
	// Ingest writes both sets. A row of forbidden lies outside the
	// restriction of Restricted.
	Ingest  func(t *testing.T, allowed, forbidden []Row)
	Queries Queries
	// Bypass lists native queries that try to read a forbidden row.
	Bypass []string
}
```

The suite writes a marker that is unique to the run into the message and the
level of every forbidden row. It then calls each read method that the
provider implements and fails when any result holds the marker. The methods
are `QueryLogs`, `QueryLogsStream`, a compiled LogchefQL query, `Histogram`,
`GetFieldValues`, `GetAllFieldValues`, `EvaluateAlert`, `GetLogContext`
around an allowed row and one `TailLogs` batch. The histogram sum and the
alert count must equal the number of allowed rows. Each `Bypass` query must
return an error or a result without the marker.

### Client surfaces

- **Web UI.** `TeamSettings.vue` shows the **Add source** button only to a
  global admin (`frontend/src/views/access/teams/TeamSettings.vue:533`). The
  source form in `AddSource.vue` gains an **Access note** field. The source
  picker in Explore
  (`frontend/src/views/explore/components/TeamSourceSelector.vue`) and the
  team's source table show a lock icon and the note next to a source that has
  one. The Query Activity failure chart gets a label for `access_denied`
  (`frontend/src/i18n/locales/en.json:2266`).
- **CLI.** `logchef sources` prints the note
  (`cli/crates/logchef-cli/src/commands/sources.rs:127`). The CLI has no
  command that links a source.
- **MCP.** `get_sources` and `get_team_sources` return `access_note`
  (`internal/mcp/sources.go:32`), so an agent knows that its results are a
  restricted view.
- **Ask Chef.** No change of its own. It reads sources through the MCP tool
  server.
- **Reports.** The email footer prints the note after the source name
  (`internal/reports/render.go:253`), as RFC 0008 planned.
- **Provisioning TOML.** `access_note` under `[[sources]]`. The export writes
  it (`internal/provisioning/export.go:126`).
- **Docs.** A new guide, `guide/restricted-sources.md`. Edits to
  `core/user-management.md`, `tutorials/loki.mdx`,
  `getting-started/provisioning.md` and `operations/audit-log.md`.
- **Metrics.** No new metric. A refused link counts in
  `logchef_authorization_failures_total` with the reason `insufficient_role`,
  as `requireAdmin` does (`internal/server/middleware.go:210`).

### Verification checklist for each restricted source

Run these as the restricted ClickHouse user, outside Logchef. Every query
must fail with a permission error or return only allowed rows. Logchef passes
SQL through, so a pass here covers raw SQL mode.

- `SELECT count() FROM logs.shared_apps`. Option 1 denies it.
- `SELECT DISTINCT namespace FROM <restricted table>` returns only the
  allowed value.
- A scalar subquery, a CTE, a JOIN and a `UNION ALL` that read the base table.
- `remote()`, `cluster()`, `merge()`, `url()` and `file()`.
- `SELECT * FROM system.query_log` and `SELECT * FROM system.parts`.
- `SELECT 1 SETTINGS serialize_query_plan = 1`. Option 2 rejects it.
- `SET ROLE ALL`, `CREATE VIEW`, any `INSERT` and any `ALTER`.
- With a `Distributed` table, `SELECT DISTINCT namespace` through the
  `Distributed` table.

Then, in Logchef, as a member of the restricted team, check that Explore in
both modes, the histogram, field values, log context, tail, an export, an
alert test, Ask Chef and an MCP query show no other namespace. Unit R4 runs
the same checks in CI for the provider methods.

## Access and security

| Action | Global admin | Team admin | Member | Provisioning |
|---|---|---|---|---|
| Create, edit or delete a source, and set `access_note` | Yes | No | No | Yes |
| Link a source to a team | Yes | No, after R1 | No | Yes |
| Unlink a source from a team | Yes | Yes | No | Yes |
| Add or remove team members | Yes | Yes | No | Yes |
| Read rows of a linked source | As a member | Yes | Yes | Not applicable |

- **Trust.** A global admin, a person who edits the provisioning file and a
  person with write access to the metadata database can each read every
  source. A team admin of a restricted team decides who joins the team and
  cannot widen the rows that the team reads.
- **Rollout rule.** A restricted team gets no team admin until R1 is merged.
  Until then a team admin who also reads the shared source can link it. R2
  should merge before the team starts work, because until then a denied read
  looks like a server error.
- **Token scopes.** No new scope. A link needs `teams:write`, a source edit
  needs `sources:write`, and a read needs `logs:read`.
- **A source edit can move the boundary.** The team links, saved queries,
  alerts and reports of a ClickHouse or VictoriaLogs source survive a change
  of its connection user, table or scope. Only a global admin can make that
  change, and R7 marks it in the audit log. A Loki source refuses the change
  (`internal/provisioning/reconciler.go:583`).
- **Metadata that a member sees.** The source details include the `CREATE`
  statement of the view (`internal/datasource/clickhouse_provider.go:306`),
  so a member learns the base table's name and the filter expression. The
  guide says so.
- **Secrets.** The restricted user's password is stored and redacted like
  every source password (`internal/audit/views.go:133`). `access_note` is
  shown to members and must hold no secret.
- **Query text.** Query history and `query.run` events hold query text, never
  rows. A collection still shows a saved query's text to a participant who
  cannot read its source (`internal/core/collections.go:774`). That finding
  is separate from this RFC.
- **Outbound requests.** This RFC adds none.
- **`/metrics`.** The route has no authentication
  (`internal/server/server.go:315`) and exposes per-source row histograms.
  Keep it on an internal network. That finding is also separate.

## Limits and failure modes

| Bound or failure | Value | What the user sees |
|---|---|---|
| `access_note` length | 200 characters, one line | 400 on save |
| A link by a caller who is not a global admin | Always refused | 403. The UI does not show the button |
| Denied link events | 60 a minute for each process. Events above the bound go to the log line only, through `RecordLimited`, as failed sign-ins do (`internal/server/audit.go:248`) | Nothing |
| A read that the database refuses | Each one | 403 with the message under "API" and a `query.run` event with the reason `access_denied` |
| Recent activity or storage stats without the grant | Each request | "Unavailable" in the source page. The source stays healthy |
| A second source on the same table and user | Refused by the unique identity key | 409, as today |
| Cached dashboard panels after a change inside the database | Up to the cache TTL, at most one hour by default (`internal/config/config.go:563`) | Results from before the change. Saving the source changes its revision and drops the entries |
| Connections | One client and one health check for each source | A cluster with 30 restricted views holds 30 Logchef clients |

Several replicas on Postgres:

- `AddTeamSource` runs under `LockMembershipWrites`
  (`internal/core/teams.go:380`), so two replicas cannot interleave a link.
- Two replicas that create the same source race on the unique index, and one
  gets the conflict (`internal/store/postgres/sources.go:58`).
- During a rolling deploy of R3, an old replica that saves a ClickHouse
  source writes the old key format. The next save by a new replica rewrites
  it. Until then the duplicate check misses that one source. No access
  changes.
- The denied-link bound is per process, like the sign-in bound.

## Backend differences

| | ClickHouse | VictoriaLogs | Loki |
|---|---|---|---|
| Boundary | The connection user, behind a definer view or a row policy | The scope query, or the tenant | The tenant |
| Who enforces it | ClickHouse | VictoriaLogs, on parameters that Logchef attaches to every request | Loki, on the header that Logchef sets |
| Raw native queries | Any `SELECT`. A denied object answers 403 | Any LogsQL. The filter reaches every subquery | Any LogQL inside the tenant |
| A second source on the same data | A new view name, or the user in the identity key (R3) | The scope is in the identity key | The tenant, selector and principal are in the identity key |
| Unavailable on a restricted source | Storage stats, the schema advisor, and recent activity on a view without `allow_view` | Nothing | Nothing |
| Exports | Yes | Not supported | Not supported |
| Log context | Yes | Not supported | LogchefQL rows only |
| Proven by | R4 and R10 | R5 | L17 of RFC 0011 |

No capability flag gates anything in this RFC. A restricted source has the
capabilities of its provider.

## Seams

**RFC 0011, unit L17.** L17 depends on "RFC 0002's change 1"
(`rfcs/0011-loki-datasource.md:1312`). Change 1 is unit R1. After R1 merges,
L17 can rely on exactly this:

- `POST /api/v1/teams/:teamID/sources` answers 403 with
  `models.AuthorizationErrorType` to every caller who is not a global admin.
  That includes an admin of the target team who can already read the source,
  and an OAuth token of a global admin.
- `core.AddTeamSource` returns `access.ErrGlobalAdminRequired` for such a
  caller and writes no link. Provisioning still links sources.
- A team admin can still unlink a source and manage members.

L17 needs nothing else from this RFC. `providertest.RunIsolation` (R4) covers
the assertions that L17 lists. L17 may call it with a second tenant as the
forbidden side, and it does not have to wait for it. The `access_note` chip
(R8) applies to a Loki source with no Loki code.

**RFC 0009.** Requires nothing new in the envelope. A handler can already
stage an outcome and a reason for a 4xx (`internal/server/audit.go:313`).
RFC 0009 lists refused requests as a non-goal with one exception, the failed
sign-in. The refused source link is the second exception. Open question 6
asks Karan to confirm it. `access_note` joins `SourceFields`, as RFC 0009
expected (`rfcs/0009-audit-log.md:746`).

**RFC 0008.** The report footer prints `access_note` (R9). External
recipients stay under the settings of RFC 0008. `access_note` remains
display-only, so no report behavior keys on it.

**RFC 0006.** Evaluation, test and backtest need no change. Unit P2 (triage)
is not built. When it is, it must read samples through `datasource.Service`
for the alert's own source, and its sample rows then honor the restriction.

**RFC 0010.** The advisor reports `unavailable` on a restricted source. R2
reuses `IsAccessDeniedError`, as RFC 0010 suggested.

**RFC 0005, RFC 0007, RFC 0003.** Not affected. Compiled aggregations read
one table. Saved views and dashboards key on the source ID.

**Conflicts.** None in design. RFC 0005 and RFC 0006 cite lines 32, 50 and
128 of the first draft of this file
(`rfcs/0005-logchefql-aggregations.md:676`,
`rfcs/0006-alerting-v2.md:890`). Those lines moved. The bypass list is now
under "Problem", the alert paths are in the read-path tables, and the
VictoriaLogs status is under "VictoriaLogs". This revision does not edit
those files.

## Audit events

No new action and no new verb. Every event uses an action that
`internal/audit/actions.go:14` already registers.

| Event | When | Actor | Target | Outcome and detail |
|---|---|---|---|---|
| `team_source.link` | A global admin or provisioning links a source | The user, or `system` with `provisioning` | `team_source` with the team and source | `success`. Exists today |
| `team_source.link` | A caller who is not a global admin tries to link | The user | `team_source` with the team and the source from the body | `failure`, reason `global_admin_required`, status 403. New in R1 |
| `team_source.unlink` | A link is removed | The user or `system` | `team_source` | `success`. Exists today |
| `source.create`, `source.update`, `source.delete` | A source changes | The user or `system` | `source` | `success`. `access_note` joins the fields in R6 |
| `source.update` | The update changes what the source can read | The user or `system` | `source` | `success` with `detail.restriction_changed = true`. New in R7 |
| `query.run` | The database refuses a read for lack of a grant | The caller | `source` with the team | `failure`, reason `access_denied`. The status is 403 for an HTTP run. New reason in R2 |
| `log_export.complete` | The query of an export is refused the same way | The caller | The export, as today | `failure`, reason `access_denied`. New reason in R2 |

`restriction_changed` is true when the identity key of the source changed.
After R3 that key holds the host, database, table and user of a ClickHouse
source, and it already holds the URL, tenant and scope of a VictoriaLogs
source. For a VictoriaLogs source it is also true when the auth mode, the
auth user, the token or a header changed, because a proxy can bind a filter
to the credential. A password change of a ClickHouse source does not set it.

The denied link carries no `changes`. A reason is a fixed code and never
holds driver text, as RFC 0009 requires. A caller who is not a member of the
team is refused earlier, by `requireTeamMember`
(`internal/server/middleware.go:358`), and records no event, like every other
refused request.

## Alternatives rejected

### A mandatory filter on the team-source link

Store a typed filter such as `[{field: namespace, equals: payments}]` on
`team_sources` and apply it in one execution boundary. This keeps a single
source ID. It is secure only if Logchef also does all of this:

- blocks raw ClickHouse SQL for every principal that holds any restricted
  grant, because another source's broad user can read the same table
- binds alerts, alert history, export files, VictoriaLogs window cursors and
  caches to the grant and a policy revision
- requires structured histogram, alert and export requests instead of SQL
  that the client sent back

`datasource.Service` is now a single place to add such a filter, which the
first draft did not have. The other costs stand. The design removes SQL mode
for exactly the teams that need it. Revisit it only if one source ID with a
different scope per team becomes a hard requirement.

### Rewriting arbitrary SQL

A recursive rewrite of the AST over CTEs, subqueries, set operations, table
functions and dictionaries is fragile and follows the ClickHouse dialect
forever. Rejected.

### Parameterized views

Source table names must be plain identifiers
(`internal/datasource/validation.go:176`), and a parameter that the member
supplies restricts nothing. Rejected.

### Keeping the merged link rule

The merged code lets a team admin link a source that they can already read
(`internal/core/teams.go:392`). That rule stops a team admin from reaching
an unknown source. It does not stop a person in two teams from widening the
second team. The first draft chose global admins only, and this revision
keeps that choice.

### A `restricted` flag that turns features off

A boolean on the source could disable exports, external recipients or Ask
Chef. The first draft decided that nothing about those features changes, and
a flag that an admin can forget to set protects nothing. `access_note` stays
a label.

### New audit actions for restrictions

An action such as `source_restriction.update` would repeat `source.update`,
and a verb such as `deny` would encode the outcome, which RFC 0009 forbids.
An outcome, a reason and one detail key say the same with no new name.

## Implementation plan

R1, R2, R3, R4 and R6 start at once and are independent of each other. R5
and R10 follow R4. R7 follows R3. R8 and R9 follow R6 and run in parallel.
R11 is last. The first slice a user can use is R1. With R1 merged, a definer
view (option 1) needs no other Logchef change, and R4 proves it on query,
LogchefQL and histogram before the other paths.

Every unit that touches a store adds the migration and the queries to both
stores, runs `just sqlc-generate`, and adds a conformance test in
`internal/store/storetest`. Every unit ends with `just check`. A frontend
unit also runs `bun run typecheck` and `bun run test`. Each unit names only
its extra check below. The old numbering maps as follows: change 1 is R1,
change 2 is R2, change 3 is R3, change 4 is R6, R8 and R9, and change 5 is
R11.

**R1. Source links are a global-admin action.** Depends on nothing. This is
"change 1", the unit that L17 of RFC 0011 waits for.
Edit `internal/core/teams.go`, `internal/core/teams_role_test.go`,
`internal/server/team_handlers.go`, `internal/server/server.go`,
`internal/server/audit.go`, `internal/server/authz_test.go`,
`frontend/src/views/access/teams/TeamSettings.vue`,
`docs/src/content/docs/core/user-management.md`,
`docs/src/content/docs/tutorials/loki.mdx` and `CHANGELOG.md`.
Change: `AddTeamSource` returns `access.ErrGlobalAdminRequired` unless the
principal is system work or a global admin, and the visibility branch is
deleted. The route drops `requireTeamAdminOrGlobalAdmin`. The handler parses
the body, stages the `team_source` target, and on that error stages the
outcome `failure` with the reason `global_admin_required` through a new
helper `auditDenied`, then answers 403. A second `auth.WindowLimiter` bounds
the denied events at 60 a minute. The UI shows **Add source** to a global
admin only.
Check: a core test asserts the error for a team admin who reads the source
through another team, for a member, and for an OAuth principal of a global
admin, and asserts a link for a global admin and for system work. A server
test asserts the 403, one stored `team_source.link` event with outcome
`failure`, reason `global_admin_required` and the source ID, and that the
61st denial in a minute stores no row. UI scenario: a team admin opens the
team's **Sources** tab and sees the table and no **Add source** button.

**R2. A denied grant answers 403 or "unavailable".** Depends on nothing.
Parallel with R1.
Edit `internal/server/query_errors.go`, `internal/server/query_run.go`,
`internal/datasource/clickhouse_provider.go`,
`frontend/src/i18n/locales/en.json`, `frontend/src/i18n/locales/zh-CN.json`
and `docs/src/content/docs/operations/audit-log.md`. Add the test to the
list in `.github/workflows/go-tests.yml`.
Change: the two mappings under "Denied grants", the `classifyActivityError`
case, the failure-reason label, and the reason in the docs list
(`docs/src/content/docs/operations/audit-log.md:135`).
Check: a server test in the `clickhouse-integration` job creates a user with
a grant on one table. A raw query that names a second table gets 403 and the
message, the `query.run` event has the reason `access_denied`, and no error
line is logged. The same user's activity request answers "unavailable". A
unit test covers `queryFailureReason` for the status and for the error.

**R3. The ClickHouse identity key includes the user.** Depends on nothing.
Parallel with R1.
Edit `pkg/models/source.go` and its tests. Create the migration
`clickhouse_identity_key_username` in both stores and
`internal/store/storetest/clickhouse_identity_upgrade.go`.
Change: the key format under "Model and stored data".
Check: the upgrade test seeds two ClickHouse sources and one VictoriaLogs
source, runs the migration twice, and asserts that each ClickHouse key
equals `BuildClickHouseIdentityKey` of its connection and that the
VictoriaLogs key is unchanged. A datasource test creates two sources on one
table with two users, and gets the conflict for a third with a repeated
user.

**R4. Isolation suite with the ClickHouse cases.** Depends on nothing.
Parallel with R1.
Edit `internal/datasource/providertest/providertest.go`. Create
`internal/datasource/clickhouse_isolation_integration_test.go`.
Change: `RunIsolation` as designed. The ClickHouse test creates a base table
with a `namespace` column and two harnesses. One is a definer view with its
user. The other is a row policy with its user. `Bypass` holds the `SELECT`
statements of the verification checklist. The test runs the other statements
of the checklist on a direct connection as the restricted user and expects an
error. It also runs `ValidateConnection`, `PrepareSource`,
`InitializeSource`, `CheckSourceConnectionStatus`, `GetSourceSchema` and
`InspectSource` as each restricted user and asserts that none fails. The view harness skips on a server that rejects the
`DEFINER` clause. The test skips without `LOGCHEF_TEST_CLICKHOUSE_ADDR`, like
the other integration tests (`internal/datasource/completeness_test.go:21`).
Check: the `clickhouse-integration` job runs the test on its three versions
and passes. Before the last commit, the builder grants the view user `SELECT`
on the base table, confirms that the suite fails, and removes the grant. The
PR description records that run.

**R5. VictoriaLogs scope in the request function.** Depends on R4.
Edit `internal/victorialogs/querying.go`, `provider.go`, `windowed.go`,
`tail.go`, `stream_fields.go` and `integration_test.go` in
`internal/victorialogs`.
Change: `doFormRequest` and `probeQueryEndpoint` apply the scope. The call
sites stop calling `applyScopeFilters`. The integration test calls
`RunIsolation` twice, once with a stream scope and once with a field scope.
`Bypass` holds a `join`, a `union`, an `in(<query>)` subquery and a query
with an `options(...)` prefix. The test also calls `StreamFields` and
`InspectSourceActivity`.
Check: the `victorialogs-integration` job passes. A unit test sends one
request through each provider method to an `httptest` server and asserts
that every request carries the scope parameter exactly once.

**R6. `access_note` on the source.** Depends on nothing. Parallel with R1.
Edit `pkg/models/source.go`, `internal/datasource/validation.go`,
`internal/datasource/source_updates.go`, the `PrepareSource` of the three
providers, `internal/config/provisioning.go`,
`internal/provisioning/reconciler.go`, `internal/provisioning/export.go`,
`internal/audit/views.go`, `queries.sql` and `sources.go` of both stores,
`internal/store/sqlite/utility.go`, and
`docs/src/content/docs/getting-started/provisioning.md`. Create the migration
`add_source_access_note` in both stores.
Change: the field, its validation next to `ValidateCommonSourceFields`
(`internal/datasource/clickhouse_provider.go:60`) and in the update path
(`internal/datasource/source_updates.go:39`), the provisioning key, the
export, and `access_note` in `SourceFields`.
Check: a store conformance test round-trips the note. A server test asserts
the 400 for 201 characters and for a newline, and a `source.update` event
whose `changes` hold `access_note`. A provisioning test sets, changes and
exports the note.

**R7. `source.update` marks a restriction change.** Depends on R3.
Edit `internal/audit/views.go`, `internal/server/source_handlers.go` and
`internal/provisioning/reconciler.go`.
Change: `audit.RestrictionChanged(before, after *models.Source) bool` as
"Audit events" defines it. The update handler and the reconciler add the
detail when it is true.
Check: a table test covers a ClickHouse user change, a table change, a
password change, a VictoriaLogs scope change, a token change and a
description change. A server test asserts the detail on the stored event.

**R8. `access_note` in the web UI.** Depends on R6. Parallel with R9.
Edit `frontend/src/api/sources.ts`, `frontend/src/views/sources/AddSource.vue`,
`frontend/src/views/explore/components/TeamSourceSelector.vue`,
`frontend/src/views/access/teams/TeamSettings.vue` and both locale files.
Change: the form field, and the lock icon with the note in the Explore
source picker and the team's source table.
Check: UI scenario. A global admin saves the note `namespace = payments`. A
member of the team opens Explore and sees the lock icon and the note next to
the source name. A source without a note shows neither.

**R9. `access_note` in the CLI, MCP and report footer.** Depends on R6.
Parallel with R8.
Edit `cli/crates/logchef-core/src/api/models.rs`,
`cli/crates/logchef-cli/src/commands/sources.rs`, `internal/mcp/sources.go`,
`internal/core/report_runs.go` and `internal/reports/render.go`.
Change: the CLI column, the MCP field, and the footer text
`<source name> (<note>)`.
Check: `just check-cli`. An MCP test asserts the field in `get_sources`. A
report render test asserts the footer with and without a note.

**R10. Distributed tables under a row policy.** Depends on R4.
Edit `.github/workflows/go-tests.yml` and
`internal/datasource/clickhouse_isolation_integration_test.go`. Create
`internal/clickhouse/testdata/isolation_secret_cluster.xml`.
Change: a second test cluster whose definition sets a `secret`, installed
like the two-shard cluster (`.github/workflows/go-tests.yml:185`). The test
puts the policy on the local table and reads through a `Distributed` table
of each cluster.
Check: through the cluster with the `secret`, `RunIsolation` passes. Through
the cluster without it, the test asserts that forbidden rows come back, and
its comment tells the reader that the guide depends on this result. If
ClickHouse behaves differently, the unit corrects the guide text of R11.

**R11. Restricted sources guide.** Depends on R1 to R5 and R10.
Create `docs/src/content/docs/guide/restricted-sources.md`. Edit the sidebar
in `docs/astro.config.mjs` and `CHANGELOG.md`.
Change: the model, both ClickHouse setups with their DDL, the VictoriaLogs
scope and the vmauth variant, a pointer to the Loki guide of L17, the
verification checklist, the list of unavailable panels, and three warnings.
A view queried with a broad user isolates nothing. A view without `DEFINER`
isolates nothing. A restricted team gets no team admin on a Logchef older
than R1.
Check: the docs site builds. Every statement of the guide's DDL appears in
the R4 or R10 test.

## Changes in revision

The first draft is the merged file at commit `7f693c82`. This revision keeps
its decisions and changes the following.

- **Structure.** The file now has the 14 sections of the feature-program
  RFCs. The proposal, the setups and the checklist moved under "Design".
- **The read-path table is new.** The first draft listed eight paths that
  each built a query from a source ID. Its claim that streaming export
  "bypasses the datasource service" no longer matters, because the export
  uses the client of the same source ID.
- **Change 1 is half built.** Commit `9effab52` made `AddTeamSource` require
  that a team admin can already read the source. R1 replaces that rule with
  the first draft's decision. Open question 5 records the choice.
- **Change 2 is partly built.** The inspection skips storage stats for a
  view, activity has an opt-in for views, and the advisor reports
  `unavailable`. R2 covers what is left, and adds the 403 for a denied read,
  which the first draft did not mention.
- **VictoriaLogs.** The documentation of v1.52.0 answers both open checks.
  The scope stays a convenience until R5, then it is a boundary.
- **`Distributed` tables.** The first draft said to define the policy on the
  local tables. That is not enough without a cluster `secret`. R10 tests it.
- **`DEFINER` is required and needs ClickHouse 24.2.** The first draft's DDL
  had the clause. The text now says why.
- **Audit events, the isolation suite, `access_note` data shape, limits and
  the unit plan are new.**
- **The checklist dropped `CREATE TEMPORARY VIEW` and `system.tables`.**
  `readonly = 2` allows temporary tables, and `system.tables` stays readable
  and shows only granted objects.

## Open questions for Karan

1. **Is a separate source for each restricted team acceptable?** Default:
   yes. A "no" makes the rejected link filter necessary.
2. **Can apps forge the filter column at ingestion?** Default: assume they
   cannot, and have the guide tell operators to set the column in the
   collector, not in the app. If they can, a row filter proves nothing.
3. **Which ClickHouse version and engine back the shared table?** Default:
   the guide recommends option 1 on 24.2 or newer, and option 2 for older
   servers and for a source that needs the sorting key. R10 is built,
   because a `Distributed` shared table is common.
4. **Should Logchef generate the DDL from the admin UI later?** Default: no.
   Wait until more than a few teams need restricted views.
5. **Should a team admin keep linking sources that they can already read?**
   Default: no. Only global admins link, as the first draft decided. The
   cost is that a team admin must ask a global admin for each source.
6. **Should a refused source link be an audit event, although RFC 0009
   excludes refused requests?** Default: yes, for this one route. An attempt
   to widen a restricted team is what an investigator looks for.
7. **Should a team admin keep unlinking sources?** Default: yes. An unlink
   only removes access from the admin's own team.
8. **Should members see `access_note`?** Default: yes. It tells a member why
   rows are missing. Operators who consider the filter itself sensitive
   leave the note empty.
9. **Should the docs call a VictoriaLogs scope a security boundary after
   R5?** Default: yes, with the statement that Logchef attaches the filter
   and that vmauth can bind it to the credential.

## Unverified claims

No ClickHouse, VictoriaLogs or Loki instance was started for this revision.
Each claim below comes from documentation or from reading code.

| Claim | Experiment |
|---|---|
| A restricted ClickHouse user can run Logchef's health check, schema read and capability probes, because `system.tables`, `system.columns` and `system.settings` need no grant | R4 runs `InitializeSource`, `CheckSourceConnectionStatus`, `GetSourceSchema` and `InspectSource` as the restricted user on 23.7, 24.1 and 26.8 |
| `ValidateConnection` and `PrepareSource` succeed for a restricted user on a view | R4 calls both on the provider with the restricted user |
| `readonly = 2` accepts every setting that Logchef sends with a query | R4 runs each provider method as a user created with `SETTINGS readonly = 2` |
| A policy that is only `RESTRICTIVE` shows the allowed rows while `users_without_row_policies_can_read_rows` is true | R4, row-policy harness. Count the allowed rows |
| ClickHouse returns code 497 through `clickhouse-go` for a base table, a table function and a system table outside the grant | R2 runs one query of each kind and asserts `IsAccessDeniedError` |
| Servers before 24.2 reject the `DEFINER` clause | R4 on 23.7.4.5 and 24.1.2.5. Record the error that the skip matches |
| A query through a `Distributed` table runs on the shard as the cluster's user unless the cluster has a `secret`, so a policy for the restricted user applies only with the `secret` | R10 |
| A user with `serialize_query_plan = 0 CONST` cannot set it to 1 in a `SETTINGS` clause | In R10, run `SELECT 1 SETTINGS serialize_query_plan = 1` as that user and expect an error |
| The `CREATE` statement of a definer view is visible to the restricted user in `system.tables` | As the restricted user, select `create_table_query` for the view |
| VictoriaLogs v1.52.0 applies `extra_filters` and `extra_stream_filters` on `query`, `hits`, `facets`, `field_names`, `field_values`, `stream_field_names`, `stats_query` and `tail` | R5 calls every provider method under both scope kinds |
| No LogsQL text widens the extra filters, including `join`, `union`, `in(<query>)` and `options(...)` | The `Bypass` list of R5 |
| The SQLite and Postgres migrations produce the same key as `BuildClickHouseIdentityKey`, including for a user name with upper-case letters or spaces around it | The upgrade test of R3 with those two user names |
| A dashboard panel can serve a cached result for up to the TTL after a change inside the database | Load a cached panel, change the view definition, reload within the TTL, then save the source and reload |
| A histogram and a log context query over a view push the time filter into the base table | Run both with `EXPLAIN indexes = 1` on the view and compare the parts read with the same query on the base table |
