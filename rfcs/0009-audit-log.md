# RFC 0009: Structured audit log

Status: Draft
Date: 2026-11-15
Authors: Karan Sharma

## Summary

Logchef records who ran which query. It does not record who changed
configuration, who signed in, or who took log data out. This RFC adds a
structured audit log for global admins and for operators.

- One envelope, `models.AuditEvent`, describes every event: actor, credential,
  action, target, outcome, request metadata, and a redacted before and after
  diff. Actions are named `<resource>.<verb>`. No credential, query text, or
  SQL enters an event.
- Events go to a new `audit_events` table in the metadata store. Each event is
  also written as one JSON line through `pkg/logger`, so operators can ship it
  to their own pipeline.
- A route declares its action with a Fiber route name. One middleware records
  the event. A test fails when a mutating route has neither an action nor a
  written exemption.
- Exports and API token creation fail closed. Every other event fails open.
- Admins get a viewer at `/admin/audit-log`, a list API, an NDJSON export, and
  `logchef audit list`.

## Problem

Logchef has two records of activity, and both cover queries only.

- `recordQueryHistory` queues one row per executed query
  (`internal/server/query_history.go:11`). The queue drops entries when it is
  full (`internal/server/query_history.go:33`), and the store keeps 200 rows
  per user (`pkg/models/query_history.go:8`).
- `handleAdminQueryActivity` reads a recent window of the same table
  (`internal/server/admin_query_activity.go:55`).

Configuration changes leave unstructured log lines or nothing.

| Change | What exists today |
|---|---|
| Setting updated or deleted | A free-text log line with the key and the email (`internal/server/admin_settings_handlers.go:169`, `:197`) |
| Sign-in | A `user.login` log line on success, nothing on failure (`internal/server/auth_handlers.go:266`, `:411`) |
| Team created | A log line without the actor (`internal/core/teams.go:131`) |
| Source updated | No line at all (`internal/server/source_handlers.go:100`) |
| API token created | No line on success (`internal/server/user_handlers.go:295`) |
| Export started | Not in query history. No export handler calls `recordQueryHistory` (`internal/server/export_handlers.go:32`, `internal/server/export_job_handlers.go:24`) |
| Provisioning run | Info lines per run, no per-object record (`internal/provisioning/reconciler.go:23`) |

Two comments in the settings handlers read "Get current user for audit log"
(`internal/server/admin_settings_handlers.go:414`, `:483`). No audit log
exists. An admin who asks "who changed the password on this source last
week" has no answer. An operator who must prove who exported data has none
either.

`internal/server/server.go` registers 69 routes with a `POST`, `PUT`, or
`DELETE` method. This RFC maps 54 of them to an event and exempts 15 with a
reason. None of the 54 leaves a structured record today. One of the 54,
`POST /oauth/revoke`, is recorded by `internal/oauth`, not by the middleware.

## Goals and non-goals

Goals:

1. Record every change to sources, teams, memberships, users, service
   accounts, API tokens, settings, saved queries, collections, alerts,
   dashboards, query shares, and OAuth grants.
2. Record sign-in, sign-out, failed sign-in, and each export of log data.
3. Keep credentials, query text, and SQL out of events by construction, and
   make a leak a build-time or test-time failure, not a review finding.
4. Make a new mutating route without an audit decision fail a test.
5. Give sibling RFCs one envelope and one recording API.
6. Bound the table by age and by row count.

Non-goals:

- Query execution. Query history and Query Activity own it. This RFC adds no
  event for `logs/query`, histogram, tail, MCP queries, or Ask Chef turns.
- Refused requests. A 401, 403, 404, 409, or validation error changes nothing.
  The request log line and `logchef_authorization_failures_total` already
  record them. One exception exists: a failed sign-in is an event.
- Tamper evidence inside the database (a hash chain or signed rows). A person
  with write access to the metadata database can rewrite any table. The log
  stream, shipped off the host, is the copy that this person cannot edit.
- A viewer for team admins or for a user's own events. Version 1 serves
  global admins only.
- Scheduler run records. Alert history owns alert evaluations. Report runs
  and report sends stay in the run history of RFC 0008. A change to the
  configuration of an alert or a report is an event.
- A general event bus. `internal/audit` records events. Nothing subscribes.

## Design

### The envelope

`models.AuditEvent` in `pkg/models/audit.go` is the stored object and the wire
format. One row of `audit_events` holds one event.

| Field | Type | Meaning |
|---|---|---|
| `id` | string | UUIDv7, set by the server. Time-ordered, so it breaks ties in the sort. |
| `occurred_at` | timestamp, UTC | When the server built the event. |
| `action` | string | `<resource>.<verb>`. See the naming rule. |
| `outcome` | string | `success` or `failure`. |
| `reason` | string | Empty on success. On failure, a short fixed code such as `invalid_credentials` or `internal_error`. Never driver text. |
| `actor_type` | string | `user`, `service_account`, `system`, or `anonymous`. |
| `actor_id` | integer, nullable | `users.id`. No foreign key, so the event outlives the user. |
| `actor_label` | string | Email or service account name when the event happened. For `system`, the component name. For a failed sign-in, the attempted email. |
| `auth_method` | string | How the actor authenticated. See the table below. |
| `credential_id` | string | API token ID, OAuth grant ID, or provisioning run ID. Empty for a session. |
| `client_id` | string | OAuth client ID. Empty otherwise. This is the MCP host or the CLI. |
| `target_type` | string | The resource part of the action, or the object it applies to. |
| `target_id` | string | Primary key as text. A setting uses its key. |
| `target_label` | string | Name when the event happened. |
| `team_id` | integer, nullable | Set when the target belongs to one team. |
| `source_id` | integer, nullable | Set when the target is a source or belongs to one. |
| `ip` | string | `c.IP()`, which honors `server.trusted_proxies`. Empty for `system`. |
| `user_agent` | string | First 256 bytes. |
| `http_method`, `route`, `status` | string, string, integer | The method, the route pattern such as `/api/v1/admin/sources/:sourceID`, and the response status. Never the raw path or the query string. |
| `changes` | JSON object | Field name to `{"from": x, "to": y}`. A create has only `to`. A delete has only `from`. |
| `detail` | JSON object | Facts that are not a diff, such as the export format. |

`auth_method` maps onto `access.AuthMethod`
(`internal/core/access/access.go:20`):

| `auth_method` | Caller | `credential_id` | `client_id` |
|---|---|---|---|
| `session` | Browser session | empty | empty |
| `api_token` | Personal or service account token | token ID | empty |
| `oauth_api` | OAuth access token for the API, such as the CLI | grant ID | client ID |
| `oauth_mcp` | OAuth access token for `/mcp` | grant ID | client ID |
| `assistant` | Ask Chef acting for a user | empty | empty |
| `provisioning` | A `provisioning.Reconcile` run | run ID | empty |
| `startup` | `core.InitAdminUsers` and OIDC auto-provisioning | empty | empty |
| `scheduler` | A background job (RFC 0006) | job ID | empty |
| `none` | A failed sign-in | empty | empty |

The envelope separates who acted from how they authenticated. A user and a
service account are `actor_type` values. An API token and an MCP client are
credentials, so they are `auth_method` with `credential_id` and `client_id`.
A provisioning run is the `system` actor with the run ID in `credential_id`.

### The naming rule

An action is `<resource>.<verb>`, lowercase, with exactly one dot.

- `<resource>` is a singular noun in snake case. It names the stored object,
  not the route. A relation between two objects is its own resource:
  `team_member`, `team_source`, `collection_item`.
- `<verb>` is an imperative in snake case from this list: `create`, `update`,
  `delete`, `add`, `remove`, `link`, `unlink`, `approve`, `deny`, `revoke`,
  `resolve`, `test`, `login`, `logout`, `download`. A sibling RFC adds a verb
  only when none of these fits.
- The verb never encodes the outcome. A failed sign-in is `session.login`
  with `outcome` `failure`.

Every action is a constant in `internal/audit/actions.go`, registered with
its target type. `audit.Lookup(name)` rejects an unknown name. Sibling RFCs
add their constants to the same file.

### Where events go

| Option | Admin viewer | Survives a metadata store outage | Leaves the host | Cost |
|---|---|---|---|---|
| Table in the metadata store | Yes | No | Only by export | One insert per event |
| Log stream through `pkg/logger` | No. Logchef would need a source that points at its own logs | Yes | Yes, with the operator's shipper | One log line per event |
| Both | Yes | Yes | Yes | Both |

This RFC recommends both. The table is the record that Logchef reads, and
the log line is a copy for the operator. The table alone has no fallback
when an insert fails, and it keeps audit data inside the application. The
log stream alone has no viewer. The copy costs about ten lines of code,
because `pkg/logger` already writes JSON to stdout
(`pkg/logger/logger.go:13`).

The log line has `msg` `audit`, level `info`, every envelope field as a
top-level attribute, and `stored`, which says whether the insert succeeded.
`audit.log_events` turns the line off.

### Migrations

Both stores get one migration named `add_audit_events`. Use the next free
number at build time. The table has no foreign key, because an event must
outlive its actor and its target. `query_history` set that precedent for
`team_id` and `source_id`.

The columns are the envelope fields, in the order of the envelope table.

- `id` is `TEXT PRIMARY KEY`.
- `occurred_at`, `action`, `outcome`, `actor_type`, and `auth_method` are
  `NOT NULL` without a default.
- `actor_id`, `team_id`, and `source_id` are nullable integers.
- `status` is `INTEGER NOT NULL DEFAULT 0`.
- `changes` and `detail` are `NOT NULL DEFAULT '{}'`.
- Every other column is `TEXT NOT NULL DEFAULT ''`.

SQLite uses `DATETIME` for `occurred_at` and `TEXT` for the two JSON columns.
Postgres uses `TIMESTAMPTZ`, `BIGINT` for the three IDs, and `JSONB`. Write
`occurred_at` in the timestamp format that `query_history` uses in each
store. The down migration drops the table. Both stores get four indexes:

```sql
CREATE INDEX idx_audit_events_time ON audit_events(occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_action ON audit_events(action, occurred_at DESC);
CREATE INDEX idx_audit_events_actor ON audit_events(actor_id, occurred_at DESC);
CREATE INDEX idx_audit_events_target ON audit_events(target_type, target_id, occurred_at DESC);
```

Filters on `team_id` and `source_id` have no index. They scan the time range
that the request gives. Each extra index adds a write to every event, and
these two filters are rare.

### Store interface

Add `AuditStore` to `internal/store/store.go` and embed it in `StoreOps`, so
that a `WithTx` handle can insert an event (`internal/store/store.go:36`).

```go
type AuditStore interface {
	InsertAuditEvent(ctx context.Context, e *models.AuditEvent) error
	ListAuditEvents(ctx context.Context, f models.AuditFilter) ([]models.AuditEvent, error)
	CountAuditEvents(ctx context.Context, f models.AuditFilter, max int) (int, error)
	PruneAuditEvents(ctx context.Context, before time.Time, keepRows, batch int) (int, error)
}
```

The interface has no update and no delete by ID. `models.AuditFilter` holds
`From`, `To`, `Action`, `ActionPrefix`, `ActorID`, `AuthMethod`,
`CredentialID`, `TargetType`, `TargetID`, `TeamID`, `SourceID`, `Outcome`,
`Ascending`, `Limit`, and a keyset cursor of `occurred_at` and `id`.

### The recording API

A new package, `internal/audit`, owns the event type, the action registry,
redaction, and the recorder. It imports `pkg/models` and `internal/store`. It
does not import `internal/server` or `internal/core`.

```go
// Fields is a redacted snapshot of one object. Only this package builds one.
type Fields struct{ /* unexported */ }

func NewFields() *Fields
func (f *Fields) Set(key string, value any) *Fields      // string, bool, integer, float, []string, time.Time
func (f *Fields) Secret(key, value string) *Fields       // records presence and change, never the value
func (f *Fields) Digest(key, text string) *Fields        // query text and SQL: SHA-256 and byte length only
func (f *Fields) URL(key, raw string) *Fields            // scheme and host only

type Target struct {
	Type, ID, Label string
	TeamID          *models.TeamID
	SourceID        *models.SourceID
}

type Recorder struct{ /* store, logger, limiter, config */ }

// Record inserts on the root store, then writes the log line. It never
// returns an error. A failed insert is logged and counted.
func (r *Recorder) Record(ctx context.Context, e Event)
// RecordOrFail is Record that returns the insert error and writes no log line
// on failure.
func (r *Recorder) RecordOrFail(ctx context.Context, e Event) error
// Batch inserts through a WithTx handle and holds the log lines back.
func (r *Recorder) Batch(tx store.AuditStore) *Batch
func (b *Batch) Add(ctx context.Context, e Event) error // return the error to abort the transaction
func (b *Batch) Publish()                               // call only after WithTx returned nil
```

An insert inside a transaction proves nothing until the commit. `Batch`
therefore writes no log line in `Add`. The caller calls `Publish` after
`WithTx` returns nil, and `Publish` writes the lines with `stored` `true`. A
rollback, a later callback error, or a failed commit drops the batch, so
neither the table nor the log stream holds the events.

`audit.Event` carries the actor, the action, the target, the outcome, the
request metadata, and two `*Fields` values, `Before` and `After`, plus a
`Detail` `*Fields`. It has no field of type `map[string]any` or
`json.RawMessage`. The recorder computes `changes` from `Before` and `After`
and keeps only the keys whose values differ.

The guarantee is narrow and exact. No credential field, no query text, and
no SQL enters an event. Names and descriptions that a user typed are
recorded as typed. Redaction cannot be skipped, for five reasons that the
code enforces.

1. `changes` and `detail` have one way in. A caller passes `*Fields`. A raw
   map, a struct, or a JSON blob does not compile.
2. `Fields` is an allowlist. A model field that no view names is not in the
   event. Each audited object has one view function in
   `internal/audit/views.go`, such as `SourceFields(*models.Source) *Fields`.
3. `Secret` stores a SHA-256 digest in memory for the equality check and
   serializes only `{"secret": true, "changed": <bool>}`.
4. The serializer in `Recorder` runs for every event. It replaces the value
   of any key that contains `password`, `passwd`, `secret`, `token`,
   `api_key`, `authorization`, `cookie`, `hash`, `headers`, `credential`, or
   `dsn` with `"[redacted]"`, unless `Secret` set it. It also truncates each
   string to 1,024 bytes and the whole `changes` plus `detail` to 16 KiB. It
   counts each replacement in `logchef_audit_redactions_total`.
5. Query text and SQL go through `Digest`, which serializes
   `{"sha256": "<hex>", "bytes": <n>}`. A literal inside a query, such as a
   session ID in a `WHERE` clause, never reaches the event. A URL goes
   through `URL`, which drops the user info, the path, and the query string.
   The serializer applies the same rule to any other value that parses as an
   absolute URL.

A test in `internal/audit` fills every model that has a view with a sentinel
string in each string field, by reflection. It records a create, an update,
and a delete, and it asserts that the sentinel from each field in the
secret list never appears in the stored row or the log line. The secret list
names `ConnectionInfo.Password` (`pkg/models/source.go:43`), the
VictoriaLogs auth password and token (`pkg/models/source.go:205`), the connection
headers (`pkg/models/source.go:223`), `User.PasswordHash`
(`pkg/models/auth.go:105`), `APIToken.TokenHash` (`pkg/models/auth.go:162`),
the value of a sensitive setting (`pkg/models/settings.go:20`), and every
alert webhook URL. The same test puts a second sentinel inside allowed
values: a literal in a saved query, an alert query, and export SQL, and the
user info and the query string of a URL. It asserts that neither appears.

Views record a summary for large objects. `DashboardFields` records the name,
the description, the revision, and the panel count, not the panel blob.
`SavedQueryFields` and `AlertFields` record the name, the source, the query
language, and a `Digest` of the query. `log_export.create` records the
language, the format, the row limit, and a `Digest` of the SQL.

### Where the call sits

The call sits in `internal/server`, in one middleware plus one line per
handler. It does not sit in `internal/core`.

Core functions do not know the caller. `core.UpdateSource` takes a context, a
datasource service, an ID, and a request (`internal/core/source.go:81`), and
`core.CreateTeam` takes no principal (`internal/core/teams.go:96`). The
actor, the credential, the IP, and the route exist only on the Fiber
context, where `principalFromLocals` reads them
(`internal/server/middleware.go:236`). A call in core would add a principal
and request metadata to about 40 function signatures. Provisioning, startup
admin setup, and OIDC auto-provisioning do not pass through a handler. They
call `Recorder` directly, as "Events outside HTTP" describes.

The server side has three parts.

**A route declares its action.** A mutating route gets a Fiber route name
that is the action:

```go
admin.Put("/sources/:sourceID", s.requireTokenScope(models.TokenScopeSourcesWrite),
	s.requireSourceNotManaged, s.handleUpdateSource).Name(string(audit.SourceUpdate))
```

No route in `internal/` has a name today, so the names are free to use.
`registerLimited` returns the router that `router.Add` gives it, so its
routes take a name the same way (`internal/server/server.go:300`).

**One middleware records.** `s.auditMiddleware` is registered with `app.Use`
next to the request logger. After `c.Next()` returns, it reads
`c.Route().Name`. The metrics middleware already reads `c.Route()` after
`c.Next()` (`internal/metrics/middleware.go:56`). When the name is a
registered action, the middleware builds the event:

- The actor and the credential come from `principalFromLocals` and from the
  API token or the OAuth token on the request, unless the handler staged an
  actor.
- The request metadata comes from `c.IP()`, the `User-Agent` header,
  `c.Method()`, `c.Route().Path`, and the response status.
- The status is not `c.Response().StatusCode()` alone. Fiber calls the error
  handler after the middleware chain unwinds, so a handler that returns
  `fiber.NewError(503, ...)` still shows 200 here. The middleware derives the
  status from the error that `c.Next()` returned, as `requestLogger` does
  (`internal/server/logging_middleware.go:31`). Both middlewares call one
  shared function for it.
- A panic never reaches the code after `c.Next()`. `recoverMiddleware` is
  registered before this middleware (`internal/server/server.go:169`), so the
  panic unwinds through it first. The middleware therefore records from a
  deferred function. The function calls `recover()`, records one `failure`
  event with status 500, and panics again with the same value, so
  `recoverMiddleware` still logs the stack and answers. The normal path
  records in the same deferred function, so each request records at most
  once.
- The outcome is `success` for a status below 400 and `failure` for 500 and
  above. For a status from 400 to 499 the middleware records nothing. A
  staged outcome replaces this rule.
- The target and the fields are the values that the handler staged.

**A handler stages the target in one line.** The line goes after the core
call succeeds:

```go
auditSet(c, audit.SourceTarget(updated), audit.SourceFields(before), audit.SourceFields(updated))
```

`auditSet(c, target, before, after)` stores the three values in `c.Locals`.
A `nil` `before` means create. A `nil` `after` means delete. Four more
helpers exist.

- `auditTarget(c, target)` stages a target without fields.
- `auditDetail(c, key, value)` adds a detail field.
- `auditAction(c, action)` replaces the action with another action of the
  same resource. The consent handler chooses `oauth_consent.approve` or
  `oauth_consent.deny` with it.
- `auditAuth(c, actor, outcome, reason)` stages the actor, the outcome, and a
  fixed reason code. Only the session handlers call it.

### Snapshots come from the mutation

A handler must not read the object before it calls core. That read is
outside the lock of the mutation, so a second admin can change the row in
between, and the diff would blame this actor for the other change.
`core.UpdateUser` already locks and reads the row inside its transaction
(`internal/core/users.go:305`), and the handler reads the result again after
the commit.

The rule is that `before` and `after` come from the transaction that writes.
Each update function in `internal/core` returns both models. The function
reads `before` inside its `WithTx`, under the lock that the write already
takes or a new locking read, and returns the row that it wrote as `after`.
Core stays free of `internal/audit`. The handler turns the two models into
`Fields`.

| Update | Lock that covers the read |
|---|---|
| `UpdateUser` | `GetUserForUpdate`, already there (`internal/core/users.go:305`) |
| `UpdateDashboard` | The revision compare-and-swap inside its `WithTx` (`internal/core/dashboards.go:481`) |
| `UpdateCollection`, collection members, teams, and items | `LockCollection` (`internal/store/store.go:127`) |
| Team members and team sources | `LockMembershipWrites` (`internal/store/store.go:73`) |
| `UpdateTeam`, `UpdateSavedQuery`, `UpdateAlert`, source update, setting update | None today. Each unit adds a `WithTx` and a locking read. |

A create returns the created model. A delete returns the model that it
deleted, read in the same transaction. Until a unit lands, its updates
record the target only, never a diff.

### What makes a missing event hard to write

Three checks, in the order a developer meets them.

1. **A mutating route without a decision fails a test.**
   `TestEveryMutatingRouteHasAnAuditDecision` walks `s.app.GetRoutes(true)`,
   as `internal/server/oauth_test.go:1130` already does. Each route with a
   `POST`, `PUT`, `PATCH`, or `DELETE` method must have a name that
   `audit.Lookup` accepts, or an entry in `auditExemptRoutes` in the same test
   file. An entry is a method, a path, and a reason. The test also fails on
   an exemption that matches no route.
2. **A named route records an event without any handler code.** When the
   handler staged nothing, the middleware builds the target from the action's
   target type and the route parameters, and adds `detail.unstaged` `true`.
   The event says who did what. It has a target ID only when the route
   carries one. A create route and a relation whose ID is in the body do not,
   so every audited handler that exists today stages its target in P3. The
   session, token, export, and OAuth consent handlers are included. P5, P7,
   and P8 add their fields and their transactional guarantees later.
3. **A handler that forgot to stage fails the handler tests.** The server
   test helper installs a recorder that fails the test on any event with
   `detail.unstaged`. Each action also has a `Staged` flag, which a unit sets
   when its handlers stage fields. The same recorder fails the test when a
   `Staged` action arrives without fields. Production counts both cases in
   `logchef_audit_unstaged_total`.

### Every mutating route and its event

The table lists all 69 routes in `internal/server/server.go` with a `POST`,
`PUT`, or `DELETE` method, `POST /mcp`, and two `GET` routes that are events.
A row that names several methods covers one route per method. Paths are
relative to `/api/v1` unless they start with `/oauth` or `/mcp`.

| Method and path | Line | Action | Target |
|---|---|---|---|
| `POST /auth/local/login` | 320 | `session.login` | The user. `detail.method` is `local`. |
| `GET /auth/callback` | 321 | `session.login` | The user. `detail.method` is `oidc`. |
| `POST /auth/logout` | 322 | `session.logout` | The user |
| `PUT /me/preferences` | 328 | exempt | UI preferences of the caller |
| `DELETE /query-shares/:token` | 334 | `query_share.delete` | Share, ID redacted with `models.RedactShareToken` |
| `POST /me/tokens` | 338 | `api_token.create` | Token. Fails closed. |
| `DELETE /me/tokens/:tokenID` | 339 | `api_token.delete` | Token |
| `POST`, `PUT`, `DELETE` on `/admin/users` | 351, 353, 354 | `user.create`, `.update`, `.delete` | User. Role and status are in `changes`. |
| `POST`, `DELETE` on `/admin/service-accounts` | 356, 357 | `service_account.create`, `.delete` | Service account |
| `POST /admin/service-accounts/:userID/tokens` | 359 | `api_token.create` | Token. `detail.owner_id` is the service account. Fails closed. |
| `DELETE /admin/service-accounts/:userID/tokens/:tokenID` | 360 | `api_token.delete` | Token |
| `POST`, `DELETE` on `/admin/service-accounts/:userID/teams` | 362, 363 | `team_member.add`, `.remove` | Member, with `team_id` |
| `POST`, `DELETE` on `/admin/teams` | 377, 378 | `team.create`, `.delete` | Team |
| `POST`, `PUT`, `DELETE` on `/admin/sources` | 382, 384, 385 | `source.create`, `.update`, `.delete` | Source |
| `POST /admin/sources/validate` | 383 | exempt | Tests a connection. Stores nothing. |
| `PUT`, `DELETE` on `/admin/settings/:key` | 402, 403 | `setting.update`, `.delete` | Setting, ID is the key |
| `POST /admin/settings/test-email`, `/test-webhook` | 404, 405 | `setting.test` | `detail.kind` is `email` or `webhook`. The webhook URL is a `Secret`. |
| `POST`, `DELETE` on `/teams/:teamID/members` | 417, 418 | `team_member.add`, `.remove` | Member, with `team_id` and the role |
| `PUT /teams/:teamID` | 421 | `team.update` | Team |
| `POST`, `PUT`, `DELETE` on `/collections` and `/collections/:collectionID` | 437 to 439 | `collection.create`, `.update`, `.delete` | Collection |
| `POST`, `DELETE` on `/collections/:collectionID/members` | 440, 441 | `collection_member.add`, `.remove` | Member, with the role |
| `POST`, `DELETE` on `/collections/:collectionID/teams` | 442, 443 | `collection_team.add`, `.remove` | Collection and team |
| `POST`, `PUT`, `DELETE` on `/collections/:collectionID/items` | 444 to 446 | `collection_item.add`, `.update`, `.remove` | Collection and saved query, with the kind |
| `POST`, `PUT`, `DELETE` on `/saved-queries` | 453, 455, 456 | `saved_query.create`, `.update`, `.delete` | Saved query, with `source_id` |
| `POST`, `DELETE` on `/teams/:teamID/sources` | 464, 465 | `team_source.link`, `.unlink` | Team and source |
| `POST .../logs/query`, `/logs/windowed`, `/logs/histogram`, `/logs/context` | 478, 479, 488, 489 | exempt | Query execution. Query history owns it. |
| `POST .../logs/query/:queryID/cancel` | 483 | exempt | Cancels the caller's own running query |
| `POST .../generate-sql` | 490 | exempt | Returns SQL text. Stores nothing. |
| `POST .../logchefql/translate`, `/validate`, `/query` | 494 to 496 | exempt | Query execution |
| `POST .../logs/export` | 482 | `log_export.create` | Source. `detail.mode` is `stream`. Fails closed. |
| `POST .../exports` | 484 | `log_export.create` | Export job. `detail.mode` is `job`. Fails closed. |
| `GET .../exports/:exportID/download` | 486 | `log_export.download` | Export job |
| `POST .../query-shares` | 491 | `query_share.create` | Share, ID redacted, with `source_id` and the expiry |
| `POST /ask-chef/turns` | 506 | exempt | Read-only assistant |
| `POST`, `PUT`, `DELETE` on `/alerts` | 513, 516, 517 | `alert.create`, `.update`, `.delete` | Alert, with `source_id` |
| `POST /alerts/test` | 514 | exempt | Runs a query. Stores nothing. |
| `POST /alerts/:alertID/resolve` | 519 | `alert.resolve` | Alert |
| `POST`, `PUT`, `DELETE` on `/dashboards` | 528, 530, 531 | `dashboard.create`, `.update`, `.delete` | Dashboard, with the revision |
| `POST /oauth/authorize` | 537 | exempt | Creates a pending request. The decision is the event. |
| `POST /oauth/token` | 538 | exempt | Code exchange and refresh. The grant is the event. |
| `POST /oauth/revoke` | 539 | exempt from the middleware | `internal/oauth` is the only recorder of `oauth_grant.revoke` for this route, see below. |
| `POST /oauth/requests/:requestID/decision` | 548 | `oauth_consent.approve` or `oauth_consent.deny` | Grant or request, with `client_id` and the scopes |
| `DELETE /me/connected-apps/:grantID` | 550 | `oauth_grant.revoke` | Grant |
| `POST /mcp` | 552 | exempt | Every MCP tool reads. None writes (`internal/mcp/logs.go:254`). |

The three session routes run without `requireAuth`
(`internal/server/server.go:319`), so no principal is on the request. Each
handler calls `auditAuth`.

- A successful sign-in stages the user that the handler loaded.
- A failed local sign-in stages `anonymous`, the attempted email, and
  `invalid_credentials`.
- A failed OIDC callback redirects with the same 307 as a success
  (`internal/server/auth_handlers.go:178`). `redirectToFrontend` stages
  `failure` with a reason code when it receives an error.
- Sign-out loads the session and its user before it revokes the session. A
  sign-out without a valid session records nothing.

The three OAuth paths in lines 537 to 539 are the constants
`oauth.AuthorizePath`, `oauth.TokenPath`, and `oauth.RevokePath`. Use the
constants in the exemption table, not the literal paths above.

### Events outside HTTP

| Writer | Where | Events | Actor |
|---|---|---|---|
| Provisioning | `provisioning.Reconcile` (`internal/provisioning/reconciler.go:23`) | `source.create`, `source.update`, `source.delete`, `team.create`, `team.update`, `team.delete`, `team_member.add`, `team_member.remove`, `team_source.link`, `team_source.unlink`, `user.create` | `system`, `provisioning`, run ID |
| Startup admins | `core.InitAdminUsers`, called at `internal/app/app.go:101` | `user.create`, `user.update` | `system`, `startup` |
| OIDC auto-provisioning | `autoProvisionUser`, called at `internal/auth/oidc.go:338` | `user.create`, `team_member.add` | `system`, `startup`, with the matched domain in `detail` |
| OAuth revoke by the client | `storage.RevokeToken` (`internal/oauth/storage.go:291`) | `oauth_grant.revoke` | The grant's user, `oauth_api` or `oauth_mcp` |
| OAuth refresh-token replay | `CheckRefreshToken` and `RotateRefreshToken` revoke the grant (`internal/store/sqlite/oauth.go:357`) | `oauth_grant.revoke`, `detail.cause` `refresh_replay` | `system` |
| OAuth authorization-code replay | The code exchange revokes the grant (`internal/store/sqlite/oauth.go:151`) | `oauth_grant.revoke`, `detail.cause` `code_replay` | `system` |

Provisioning fails open, like every action outside the fail-closed class. A
failed audit insert must not roll back a reconcile, because a reconcile
error stops startup (`internal/app/app.go:171`). `Reconcile` takes a
`*audit.Recorder` and generates one run ID per call. It runs inside
`db.WithTx` (`internal/provisioning/reconciler.go:48`). The callback appends
each event to a slice and inserts nothing. After `WithTx` returns nil,
`Reconcile` calls `Record` for each event. A rollback and a dry run discard
the slice, so they leave neither rows nor log lines. Do not call `Record`
inside the callback. On SQLite a call on the root store waits for the single
write connection, which the transaction holds
(`internal/store/sqlite/tx.go:49`). `Batch` is for the fail-closed actions
only.

The store does not say which grant it revoked. `RevokeGrantByToken` returns
only an error (`internal/store/store.go:373`). A replay returns
`RefreshReplay` without a grant, because `RefreshOutcome.Grant` is set only
for `RefreshOK` (`pkg/models/oauth.go:128`), and a code replay returns only
`ErrOAuthCodeReplay`. The OAuth unit changes all four paths
(`RevokeGrantByToken`, `CheckRefreshToken`, `RotateRefreshToken`, and the
code exchange) in both stores to return the affected grant and a `Revoked`
flag. The flag is true only for the call that changed the row.
`internal/oauth` records one event per true flag, so a second replay of the
same token records nothing.

`POST /oauth/revoke` answers 200 for an unknown token and for a grant that
is already revoked (`internal/store/store.go:370`). A status-based event
would report a revocation that did not happen. The route therefore has an
entry in `auditExemptRoutes`, with the reason that `internal/oauth` records
it. `storage.RevokeToken` records `oauth_grant.revoke` when `Revoked` is
true and records nothing when it is false. Until P7 lands, a revoke through
this route records nothing.

### Cost on the write path

Recording is synchronous. `Record` runs after the handler and before Fiber
writes the response. An event costs one in-memory diff bounded by the 16 KiB
cap, one insert with four index writes, and one `slog` call. An update adds
one primary-key read of the object before the change. The insert runs on
`context.WithoutCancel(c.Context())` with a 2 second timeout, so a client
disconnect or a shutdown does not lose the event.

A queue was rejected. The query history queue holds 128 entries and drops
the rest (`internal/server/query_history.go:33`). That is correct for
history and wrong for an audit log. Mutations are also rare. A 50-person
instance produces a few hundred events a day, against thousands of queries.

### When an audit write fails

Events fall into two classes.

**Recorded after the change (fails open).** The change is committed. A failed
insert cannot undo it, and a 500 would tell the client that the change did
not happen. `Record` writes the log line with `stored` `false`, logs an
error, and increments `logchef_audit_events_total{result="failed"}`. The
request succeeds. The change and the event use the same database, so an
insert that fails after the change committed is a transient lock or a
timeout, not an outage.

**Recorded with the action (fails closed).** The rule is that no credential
and no log data leaves the server without a stored event. Three actions are
in this class.

| Action | How | On failure |
|---|---|---|
| `api_token.create` | The handler runs `core.CreateAPIToken` and `Batch.Add` inside one `s.store.WithTx`, then `Publish`. `CreateAPIToken` already takes `store.StoreOps` (`internal/core/api_tokens.go:165`). | The transaction rolls back. 503. No token exists. |
| `log_export.create`, stream | The handler calls `RecordOrFail` after validation and before the first byte. | 503. No query runs. |
| `log_export.create`, job | The handler runs `CreateExportJob` and `Batch.Add` inside one `WithTx`, then `Publish`. | 503. No job exists. |

The middleware skips an event that the handler already recorded.
`log_export.download` fails open, because `log_export.create` already
recorded who made the artifact.

Sign-in fails open. A broken audit table must not lock every admin out of
the instance that they need to repair.

### Retention and pruning

A new `[audit]` section in the TOML config:

| Key | Default | Meaning |
|---|---|---|
| `audit.retention_days` | `365` | Delete events older than this. `0` keeps every event. |
| `audit.max_rows` | `1000000` | Delete the oldest events above this count. `0` removes the bound. |
| `audit.log_events` | `true` | Write the `audit` log line. |

The keys are in the config file, not in `system_settings`. An admin with a
session cannot shorten the retention from the UI and then wait.

`cleanupExpiredBackgroundState` calls `PruneAuditEvents` every five minutes
(`internal/server/export_job_handlers.go:385`). The call deletes at most
5,000 rows per run, oldest first. It is idempotent, so several replicas can
run it at once. It logs one line with the count when it deletes rows. It
writes no audit event.

The demo skips the table. A read-only demo shares one login, and
`enforceDemoReadOnly` already refuses changes
(`internal/server/demo_mode.go:33`). When `demo.read_only` is set, `Record`
writes the log line only, as query history does for its rows.

### API

Two admin routes, both behind `requireAuth` and `requireAdmin`. `requireAdmin`
refuses every OAuth token (`internal/server/middleware.go:210`), so an MCP
host or an OAuth CLI session cannot read the audit log.

A new token scope, `audit:read`, joins `pkg/models/auth.go`. A session holds
every scope. An API token needs `audit:read` or `*`. The scope is not part of
`ReadOnlyTokenScopes` (`internal/core/api_tokens.go:103`), because events
hold emails and IP addresses.

`GET /api/v1/admin/audit-events`

| Parameter | Meaning |
|---|---|
| `from`, `to` | RFC 3339. Default is the last 7 days. |
| `action` | Exact action, or a resource with a trailing dot such as `source.` |
| `actor_id`, `auth_method`, `credential_id` | Exact match |
| `target_type`, `target_id` | Exact match. `target_id` needs `target_type`. |
| `team_id`, `source_id`, `outcome` | Exact match |
| `order` | `desc` (default) or `asc` |
| `limit` | Default 100, maximum 500 |
| `cursor` | The `next_cursor` of the previous page |

The response is `{"events": [AuditEvent], "next_cursor": "..."}` in the
standard envelope. `next_cursor` is empty on the last page. The route returns
400 for a bad parameter and 403 without the admin role or the scope.

`GET /api/v1/admin/audit-events/export`

It takes the same filters and returns `application/x-ndjson`, one event per
line. The handler counts the matching rows first. Above 100,000 rows it
returns 400 and asks for a narrower time range. It records
`audit_event.export` with the filters in `detail`.

### Viewer

`frontend/src/views/admin/AuditLog.vue` at `/admin/audit-log`, with
`requiresAdmin`, next to Query Activity in the router
(`frontend/src/router/index.ts:213`) and in the admin navigation.

- `PageHeader` holds the title and a **Download** button that calls the
  export route with the current filters.
- A filter row holds the time range, the action (a select grouped by
  resource), the actor (the user list that `GET /admin/users` returns), the
  outcome, and a target type with an ID. The filters are server-side and live
  in the URL query. `/admin/audit-log?target_type=source&target_id=7` is the
  history of one source.
- `SearchInput` with `useTableSearchSort` filters the loaded rows by actor,
  action, and target label. The `persistKey` is `admin-audit-log`. Events are
  not named entities, so the default order is newest first, not name A-Z.
- `SortHeader` sits on the **Time** column only. It sets `order` and reloads,
  because a client-side sort of one page would mislead.
- Columns: Time, Actor (label, with a badge for `api_token`, `oauth_mcp`,
  `provisioning`, and other non-session methods), Action, Target (a link when
  the object still exists), Outcome, IP.
- A row expands to show `changes` as a from and to list, `detail`, the route,
  the status, the user agent, and the client ID.
- **Load more** fetches the next cursor. `EmptyState` covers "no events yet"
  and "no events match". `LoadingState` covers the first load.
- The page has no destructive action, so it uses no `RowActionsMenu` and no
  `ConfirmDialog`.

The API client goes in `frontend/src/api/admin.ts`. The page keeps its state
locally, as `QueryActivity.vue` does
(`frontend/src/views/admin/QueryActivity.vue:14`). It needs no Pinia store.

### Other client surfaces

| Surface | Change |
|---|---|
| CLI | `logchef audit list` with `--since`, `--until`, `--action`, `--actor`, `--target`, `--outcome`, `--limit`, and `--output table|json|jsonl`. It pages with the cursor. It needs an API token with `audit:read`, because OAuth sessions cannot reach admin routes. The CLI already sends `logchef-cli/<version>` as its user agent (`cli/crates/logchef-core/src/api/mod.rs:81`), so CLI actions are visible in events. |
| MCP | Not affected. No tool reads or writes audit events. |
| Ask Chef | Not affected. |
| Provisioning TOML | No new keys. Provisioning runs emit events. |
| Docs | A new page, `docs/src/content/docs/operations/audit-log.md`: the envelope, the action list, the config keys, the log line with a shipping example, and the fail-closed rules. Add `audit:read` to `docs/src/content/docs/features/service-tokens.mdx`. Add the metrics to `docs/src/content/docs/operations/metrics.mdx`. |
| Metrics | `logchef_audit_events_total{result}` with `stored`, `failed`, and `limited`. `logchef_audit_redactions_total`. `logchef_audit_unstaged_total`. |
| Swagger | Annotations on the two new handlers. |

### Relation to query history and Query Activity

| Record | Holds | Retention | Reader |
|---|---|---|---|
| `query_history` | Query text, duration, rows, per executed query | 200 rows per user | The user, and admins through Query Activity |
| `query_stats_daily` | Counts per day, user, team, source, language | Not pruned | Admins through Query Activity |
| `audit_events` | Changes, sign-ins, exports | `audit.retention_days` and `audit.max_rows` | Admins through the audit log |

The audit log adds no query event and reads neither query table. Exports are
the one place where the two meet, and today exports are in neither. They go
to the audit log, because an export is a transfer of data, and its rate is
low. The audit viewer and Query Activity are separate pages. A link in each
page header points to the other.

## Access and security

- Only a global admin reads events, by session or by an API token with
  `audit:read`. Team admins and members have no access in version 1.
- Nobody writes or deletes an event through the API. The store interface has
  no update.
- Events hold emails, IP addresses, user agents, and object names. Treat
  the table and the log line as personal data. Retention bounds
  both the table and, through the operator's pipeline, the log.
- No credential, query text, or SQL enters an event. "The recording API"
  lists the five mechanisms and the test.
- A share token is a bearer credential. Events store
  `models.RedactShareToken(token)` as the target ID.
- Restricted sources (RFC 0002). A member of a restricted team cannot read
  the audit log, so events about the shared source do not leak to them. RFC
  0002 moves source linking to global admins. `team_source.link` records
  whoever the route allows. The `access_note` field joins `SourceFields` when
  it lands.
- The audit feature makes no outbound request.

## Limits and failure modes

| Bound | Value | What the user sees |
|---|---|---|
| Size of `changes` plus `detail` | 16 KiB | Later keys are dropped and `detail.truncated` is `true` |
| One string value | 1,024 bytes | The value ends with `...` |
| List page | 500 rows | `limit` is clamped |
| Export | 100,000 rows | 400 with "narrow the time range" |
| Insert timeout | 2 seconds | Fails open or closed by class |
| Prune batch | 5,000 rows per 5 minutes per replica | None |
| Failed sign-in events | 600 per minute per process | Extra events go to the log line only, counted as `limited` |

The failed sign-in limit exists because that event is the only one that an
unauthenticated caller can cause. The auth rate limiter is optional
(`internal/server/server.go:292`). Without the bound, a credential-stuffing
run would fill the table and push real events past `audit.max_rows`. The
bound is one `auth.NewGlobalWindowLimiter(time.Minute, 600)`
(`internal/auth/window_limiter.go:38`).

With several replicas on Postgres, each replica inserts on its own. UUIDv7
IDs do not collide. The order is `occurred_at` then `id`, so clock skew can
reorder events that are milliseconds apart. Pruning is idempotent. The
sign-in bound is per process, so N replicas allow N times the rate.

A crash between a committed change and its insert loses that event. Only the
three fail-closed actions close that gap. On SQLite, the insert waits for
the single write connection like every other write. A wait past
`busy_timeout` returns `models.ErrDatabaseBusy`, and the event fails open or
closed by class.

## Backend differences

None. The audit log lives in the metadata store. ClickHouse, VictoriaLogs,
and Loki sources produce the same events, with `source_type` in the source
fields. No capability flag gates anything.

## Seams

- **Requires from every sibling RFC (0005, 0006, 0007, 0008, 0010, 0011).**
  For each stored object that the RFC adds: one action constant per mutating
  route in `internal/audit/actions.go`, one view function in
  `internal/audit/views.go` that marks every secret with `Secret`, a route
  name on each mutating route, and one `auditSet` line per handler. A
  read-only `POST` route gets an entry in `auditExemptRoutes` with a reason.
  The route test enforces the last two. The webhook URLs and tokens of
  notification destinations (RFC 0006) and report targets (RFC 0008) are
  secrets.
- **Scope agreed across RFCs.** The audit log records configuration changes
  and security events. Report runs and sends stay in the run history of RFC
  0008, which emits no `report.run` and no `report.send` action. No sibling
  records raw query text or SQL. A sibling records the query language, a
  revision or an ID, and a `Digest`. A sibling calls `Recorder` directly only
  outside HTTP, in provisioning and in jobs.
- **What siblings depend on.** P1, P2, and P3 for any audited route. P2 alone
  for a direct `Recorder` call from a job.
- **Requires from RFC 0006.** The job runner gives each job a
  `context.Context` and a stable job ID. A job that changes configuration
  records with `actor_type` `system`, `auth_method` `scheduler`, and the job
  ID in `credential_id`. A job's run history stays in RFC 0006.
- **Offers to RFC 0006.** Pruning uses the existing five-minute cleanup loop,
  so this RFC does not wait for the job runner. Pruning can move to the
  runner later without a schema change.
- **RFC 0003.** Dashboards v2 routes that land after this RFC follow the same
  rule. `DashboardFields` records the revision, not the panel blob. No
  conflict.
- **RFC 0002.** No conflict. See "Access and security".
- **Merge hot spots.** `internal/audit/actions.go`, `internal/audit/views.go`,
  and `auditExemptRoutes` take one line or one function per feature. Keep
  each list sorted by resource to keep conflicts small.

## Audit events

This RFC emits one event about itself.

| Action | Actor | Target |
|---|---|---|
| `audit_event.export` | The admin | None. `detail` holds the filters and the row count. |

Reading the list emits nothing. "Every mutating route and its event" and
"Events outside HTTP" hold the list for the routes that exist today.

## Alternatives rejected

- **Record in `internal/core`.** Core functions have no principal and no
  request metadata. See "Where the call sits".
- **A database trigger per table.** A trigger does not know the actor, needs
  one definition per table and per store, and would record secrets unless
  each trigger named its columns.
- **Derive the diff from JSON tags.** `ConnectionInfo.Password` serializes
  as `password` (`pkg/models/source.go:43`). A default-allow diff leaks the
  first field that someone forgets to tag. The allowlist in `Fields` fails
  safe.
- **An async queue, as query history uses.** It drops events under load and
  cannot fail closed.
- **One transaction for every change and its event.** It is the strongest
  guarantee, and it needs every core function to accept a transaction handle.
  Source changes also go through the datasource service. The three
  fail-closed actions get this guarantee where it matters.
- **Record refused requests.** A group-level middleware such as `requireAdmin`
  rejects a request before Fiber matches the final route, so the middleware
  cannot see the action. Recording only the refusals that reach a handler
  would give an inconsistent record.
- **Log stream only, with a Logchef source over Logchef's own logs.** It
  needs a log backend to administer Logchef, and it makes the viewer depend
  on the operator's pipeline.

## Implementation plan

Each unit is one PR with its tests. P4 to P10 run in parallel after P3. P11
follows P2. P12 and P13 follow P10.

**P1. Audit store.** Depends on nothing.
- Create `pkg/models/audit.go`, the `add_audit_events` migration pair in both
  stores, and `audit.go` in `internal/store/sqlite/`,
  `internal/store/postgres/`, and `internal/store/storetest/`. Edit both
  `queries.sql` files and `internal/store/store.go`. Run `just sqlc-generate`.
- Check: conformance tests on both stores. Each filter narrows the list.
  Keyset pages return every row once in both orders. Prune stops at the batch
  size. An insert inside a rolled-back `WithTx` leaves no row.

**P2. Audit package.** Depends on P1.
- Create `internal/audit/` with `event.go`, `actions.go` (every action in
  this RFC, `Staged` false), `fields.go`, `views.go` (`SourceFields` and
  `SettingFields`), and `recorder.go`. Edit `internal/config/config.go`
  (`AuditConfig`), `config.toml`, `internal/metrics/metrics.go`, and
  `internal/app/app.go` (build one `Recorder`).
- Check: a diff keeps only changed keys. `Secret` serializes no value. A
  denylisted key is replaced. `Digest` and `URL` drop the sentinel inside a
  query and a URL. The 16 KiB cap sets `detail.truncated`. `Record` with a
  failing store writes the log line with `stored` `false`. `RecordOrFail`
  returns the error and writes no line. A `Batch` writes no line after a
  rollback, a callback error, or a failed commit, and one line per event
  after `Publish`.

**P3. Route coverage with staged targets.** Depends on P2. This is the
first slice that an operator can use. Every change produces a log line and a
row with the actor, the action, and the target.
- Create `internal/server/audit.go` (the middleware, the five staging
  helpers, the actor builder, the shared status function) and
  `internal/server/audit_routes_test.go`. Edit `internal/server/server.go`:
  add the `Recorder` to `ServerOptions`, register the middleware, name the
  routes in the route table, and make `registerLimited` return the router.
  Add one `auditTarget` line to every audited handler, including the token,
  export, OAuth consent, and connected-app handlers. Add `auditAuth` to the
  three session handlers and to `redirectToFrontend`. Add the
  `/oauth/revoke` exemption. Token creation and exports record through the
  middleware here and fail open until P5 and P8. Edit
  `internal/server/logging_middleware.go` to use the shared status function.
- Check: `TestEveryMutatingRouteHasAnAuditDecision` passes, and fails when a
  name is removed. A source update, a source create, and a team member add
  each write one row with the caller as actor and the right target ID. A
  handler that returns `fiber.NewError(503, ...)`, one that returns a plain
  error, and one that panics each record exactly one `failure` event with
  status 503 or 500, and the panic still reaches `recoverMiddleware`. A
  request that `requireAdmin` rejects writes no row. A local and an OIDC
  sign-in each record `session.login` with the user as actor. A wrong
  password and a failed OIDC callback, which answers 307, each record
  `failure`. Sign-out with a session records the user, and sign-out without
  one records nothing. The existing HTTP tests pass with the strict test
  recorder, including the token creation test at
  `internal/server/user_handlers_test.go:40`.

**P4. Sources and settings.** Depends on P3.
- Edit `internal/server/source_handlers.go`,
  `internal/server/admin_settings_handlers.go`, and the source and setting
  update paths so that they return `before` and `after` from one transaction.
  Set `Staged` for `source.*` and `setting.*`.
- Check: a password change yields `connection.password` with `secret` and
  `changed`, and no password text in the row or the log line. A sensitive
  setting update records no value.

**P5. Users, service accounts, tokens, teams, and memberships.** Depends on
P3.
- Edit `internal/server/user_handlers.go`, `internal/server/team_handlers.go`,
  `internal/core/users.go`, `internal/core/teams.go`, the views, and the
  actions. `UpdateUser` and `UpdateTeam` return `before` and `after`. Both
  token handlers run `core.CreateAPIToken` and `Batch.Add` in one `WithTx`
  (`internal/server/user_handlers.go:295`, `:450`).
- Check: a role change records `role` from and to. Two concurrent updates of
  one user record two diffs, and each diff holds only its own change. With a
  store whose `InsertAuditEvent` fails, `POST /me/tokens` returns 503, the
  user has no token, and no log line says `stored` `true`.

**P6. Saved queries, collections, dashboards, alerts, and query shares.**
Depends on P3.
- Edit the five matching handler files in `internal/server/`, the matching
  update functions in `internal/core/`, the views, and the actions.
- Check: a 200-panel dashboard update records the revision and the panel
  count in a row under 16 KiB. A saved query whose text holds a sentinel
  literal records a digest and no sentinel. A share event stores the redacted
  token. The alert view marks its webhook URL as a secret.

**P7. Sessions and OAuth.** Depends on P3.
- Edit `internal/server/auth_handlers.go` (reason codes, the failed sign-in
  bound, session fields), `internal/server/oauth_handlers.go`,
  `internal/oauth/storage.go`, `internal/oauth/oauth.go` (take a `Recorder`),
  `internal/store/store.go`, `pkg/models/oauth.go`, and both store `oauth.go`
  files (the four revoking paths return the grant and the `Revoked` flag).
- Check: a wrong password records `invalid_credentials` and the attempted
  email. The 601st failure in one minute writes a log line and no row. A
  refresh replay through the check, through the rotation, and a code replay
  each record one `oauth_grant.revoke` with the grant as target. A second
  replay records nothing. `POST /oauth/revoke` with a live token records
  exactly one event. The same call again, and a call with an unknown token,
  each answer 200 and record nothing.

**P8. Exports.** Depends on P3.
- Edit `internal/server/export_handlers.go` and
  `internal/server/export_job_handlers.go`.
- Check: with a failing audit store, `POST .../logs/export` returns 503 before
  any query starts, and `POST .../exports` returns 503 and creates no job. An
  export whose SQL holds a sentinel literal records a digest and no sentinel.

**P9. Provisioning, startup admins, and auto-provisioning.** Depends on P3.
- Edit `internal/provisioning/reconciler.go`, `internal/core/users.go`,
  `internal/auth/auto_provision.go`, `internal/auth/oidc.go`, and
  `internal/app/app.go`.
- Check: a reconcile that creates one team and links one source writes
  `team.create` and `team_source.link` with one run ID. A dry run, a run whose
  callback fails after the first event, and a second run with the same config
  each leave no row and no `audit` log line. With a store whose
  `InsertAuditEvent` fails, the reconcile still commits and startup
  continues.

**P10. List and export API.** Depends on P3, because the export route
records `audit_event.export`.
- Create `internal/server/audit_handlers.go`. Edit `internal/server/server.go`
  (two routes), `pkg/models/auth.go` (`audit:read`), and
  `internal/core/api_tokens.go` (accept the scope, keep it out of the
  read-only set).
- Check: a non-admin and a token without `audit:read` both get 403. A cursor
  walk returns every seeded row once. An export over 100,000 matching rows
  returns 400.

**P11. Pruning.** Depends on P2, which adds the `[audit]` config.
- Edit `cleanupExpiredBackgroundState` in
  `internal/server/export_job_handlers.go`.
- Check: with `retention_days` 1, one run deletes rows from two days ago and
  keeps today's rows. With `max_rows` 10 and 25 rows, 10 remain. With both
  keys at 0, nothing is deleted.

**P12. Admin viewer.** Depends on P10.
- Create `frontend/src/views/admin/AuditLog.vue` and its vitest file. Edit
  `frontend/src/api/admin.ts`, `frontend/src/router/index.ts`,
  `frontend/src/layouts/InnerApp.vue`, the command palette icons, and the
  locale files.
- Check: `bun run typecheck` and `bun run test`. In a running app, change a
  source, open `/admin/audit-log`, expand the row, and see the changed field.
  Filter by action and reload. The filter survives in the URL.

**P13. CLI.** Depends on P10.
- Create `cli/crates/logchef-cli/src/commands/audit.rs`. Edit
  `commands/mod.rs` and the API client in `logchef-core`.
- Check: `just check-cli`. Against a dev server,
  `logchef audit list --action source. --output jsonl` prints one JSON object
  per line.

**P14. Docs.** Depends on P4 to P12.
- Create `docs/src/content/docs/operations/audit-log.md`. Edit
  `service-tokens.mdx`, `metrics.mdx`, and `CHANGELOG.md`.
- Check: the docs site builds. A test in `internal/audit` fails when an
  action in `actions.go` is missing from the page.

## Open questions for Karan

1. **Should the table store IP addresses and user agents?** The default is
   yes. An investigator asks for them first, and retention bounds them.
2. **Should API token creation fail closed?** The default is yes, with
   exports. The cost is one transaction in two handlers. The other option is
   exports only.
3. **What are the retention defaults?** The default is 365 days and 1,000,000
   rows. The row cap can end retention before 365 days on a busy instance.
4. **Should team admins see events for their own team?** The default is no in
   version 1. `team_id` is on the envelope, so a later route can filter by it.
5. **Should `audit:read` be part of the read-only token preset?** The default
   is no. Events hold emails and IP addresses.
6. **Should the log line be on by default?** The default is yes. It is the
   only copy that survives a failed insert.
7. **Should refused requests (403) become events later?** The default is no.
   Moving the auth checks from group middleware to route middleware is one
   way to do it later.

## Unverified claims

1. After `c.Next()` in an `app.Use` middleware, `c.Route().Name` is the name
   of the final matched route when a handler of that route wrote the
   response. It is empty when a group-level middleware rejected the request.
   The code reads `c.Route().Path` this way today, not `Name`. The two P3
   tests on Fiber v3.5.0 settle both cases.
2. `Group.Put(...).Name(x)` names the route just added and every other
   registration with the same method and path. Read from `group.go` and
   `app.go` in Fiber v3.5.0
   (https://pkg.go.dev/github.com/gofiber/fiber/v3@v3.5.0#App.Name), not run.
   No two routes in `server.go` share a method and a path. The P3 route test
   settles it, because it reads each route's name.
3. A handler that returns `fiber.NewError(503, ...)` leaves the response
   status at 200 until the middleware chain unwinds. Read from `router.go` in
   Fiber v3.5.0, not run. The three error cases in the P3 check settle it.
4. The synchronous insert adds under 2 ms to a mutation on SQLite and on
   Postgres. Not measured. To settle it, time 1,000 `InsertAuditEvent` calls
   in the conformance suite on each store and report the median and the p99.
5. A year of events for a 50-person instance is under 100 MB. Estimated from
   about 600 bytes per row and a few hundred events a day. To settle it, seed
   100,000 realistic events and read the table and index sizes on each store.
6. `uuid.NewV7` in `github.com/google/uuid` v1.6.0 gives IDs that sort by
   creation time within one process
   (https://pkg.go.dev/github.com/google/uuid@v1.6.0#NewV7). The P1 keyset
   test settles it with 1,000 inserts in a loop.
7. Ask Chef turns and MCP tools write no metadata other than query history.
   Read from the tool list in `internal/mcp`, not traced end to end. To
   settle it, run each MCP tool against a dev server with SQLite statement
   logging on.
