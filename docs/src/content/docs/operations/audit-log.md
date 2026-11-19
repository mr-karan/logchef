---
title: Audit Log
head:
  - tag: title
    content: "Audit Log for Log Access and Changes | Logchef"
description: "Record who changed Logchef configuration, who signed in and who exported logs. Event format, actions, export, retention and failure handling."
---

Logchef records configuration changes, sign-ins, OAuth decisions and data
exports as audit events. Global admins read them in **Administration → Audit
Log**, through the API, or with `logchef audit list`. Each event is also
written as one JSON log line, so a log shipper can keep a copy off the host.

The audit log records changes and security events. It does not record queries:
query history and **Administration → Query Activity** own those. Each of the
two pages links to the other.

## What an event holds

One row of the `audit_events` table is one event. The API returns the same
object.

| Field | Meaning |
|-------|---------|
| `id` | UUIDv7. Time-ordered, so it breaks ties in the sort |
| `occurred_at` | When the server built the event, UTC, millisecond precision |
| `action` | `<resource>.<verb>`, such as `source.update`. See [Actions](#actions) |
| `outcome` | `success` or `failure` |
| `reason` | Empty on success. On failure, a fixed code such as `invalid_credentials` or `internal_error`, never driver text |
| `actor_type` | `user`, `service_account`, `system` or `anonymous` |
| `actor_id` | The user ID. No foreign key, so the event outlives the user |
| `actor_label` | The email or service account name at the time. For `system`, the component. For a failed sign-in, the attempted email |
| `auth_method` | How the actor authenticated. See the next table |
| `credential_id` | API token ID, OAuth grant ID, provisioning run ID or report run. Empty for a session |
| `client_id` | OAuth client ID, such as the CLI or an MCP host. Empty otherwise |
| `target_type`, `target_id`, `target_label` | The object the action applies to. A setting uses its key as the ID. A query share uses its redacted token |
| `team_id`, `source_id` | Set when the target belongs to one team or one source |
| `ip` | The client address. It honors `server.trusted_proxies`. Empty for `system` |
| `user_agent` | The first 256 bytes |
| `http_method`, `route`, `status` | The method, the route pattern (such as `/api/v1/admin/sources/:sourceID`) and the response status. Never the raw path or query string |
| `changes` | Field name to `{"from": x, "to": y}`. A create has only `to`, a delete only `from`. Only changed fields appear |
| `detail` | Facts that are not a diff, such as an export's format or a revoke's cause |

| `auth_method` | Caller | `credential_id` |
|---------------|--------|-----------------|
| `session` | A browser session | empty |
| `api_token` | A personal or service account API token | token ID |
| `oauth_api` | An OAuth access token for the API, such as the CLI | grant ID |
| `oauth_mcp` | An OAuth access token for `/mcp` | grant ID |
| `assistant` | Ask Chef acting for a user | empty |
| `provisioning` | A provisioning run | run ID |
| `startup` | Admin setup from `auth.admin_emails` and `[auth.local]`, and OIDC auto-provisioning | empty |
| `scheduler` | A background job, such as a report run | `report_run:<id>` |
| `none` | A failed sign-in | empty |

### What never enters an event

- **Credentials.** Passwords, API tokens, OAuth secrets, webhook URLs,
  connection headers and sensitive settings are recorded as
  `{"secret": true, "changed": true}`: the event says a secret changed, never
  its value. A field whose name contains `password`, `secret`, `token`,
  `api_key`, `authorization`, `cookie`, `hash`, `headers`, `credential` or
  `dsn` is replaced with `[redacted]`.
- **Query text.** Saved query content, alert queries and conditions, export
  queries, share payloads and saved view filters are recorded as a SHA-256
  digest and a byte length. Alert labels and annotations are a digest plus
  the label names.
- **URLs.** Any other absolute URL is reduced to its scheme and host.
- **Large objects.** A dashboard is recorded by name, revision, panel count
  and tab count, never the panel definitions.

One string value holds at most 1,024 bytes and ends with `...` when cut.
`changes` and `detail` together hold at most 16 KiB; later keys are dropped and
`detail.truncated` is `true`.

## Actions

Each action is a resource and a verb. A filter on `source.` matches every
action of that resource. A request that changes nothing, such as removing a
member who is not on the team, records nothing. A request refused with a 4xx
before the change records nothing, except a failed sign-in.

| Resource | Actions | Recorded when |
|----------|---------|---------------|
| Alert | `alert.create`, `alert.update`, `alert.delete`, `alert.resolve` | An alert rule changes or is resolved by hand |
| Alert silence | `alert_silence.create`, `alert_silence.update`, `alert_silence.delete` | A silence changes |
| API token | `api_token.create`, `api_token.delete` | A personal or service account token is issued or deleted. Creation [fails closed](#when-an-audit-write-fails) |
| Audit log | `audit_event.export` | An admin downloads events. `detail` holds the filters and the row count |
| Collection | `collection.create`, `collection.update`, `collection.delete` | A collection changes |
| Collection item | `collection_item.add`, `collection_item.update`, `collection_item.remove` | A saved query is added to, converted in, or removed from a collection |
| Collection member | `collection_member.add`, `collection_member.remove` | A user is given or loses access to a collection |
| Collection team | `collection_team.add`, `collection_team.remove` | A collection is shared with or unshared from a team |
| Dashboard | `dashboard.create`, `dashboard.update`, `dashboard.delete` | A dashboard changes. A delete also removes the dashboard's scheduled reports, with no `report.delete` of their own: `detail.reports_removed` counts them |
| Log export | `log_export.create`, `log_export.download` | Logs are exported, streamed (`detail.mode` `stream`) or as a job (`job`), and a job's file is downloaded. Creation fails closed |
| Notification destination | `notification_destination.create`, `notification_destination.update`, `notification_destination.delete`, `notification_destination.test` | A destination changes or is tested |
| OAuth consent | `oauth_consent.approve`, `oauth_consent.deny` | A user decides an OAuth authorization request. The event holds the client, the resource and the scopes |
| OAuth grant | `oauth_grant.revoke` | A grant is revoked: by the user in Connected apps, by the client at `/oauth/revoke`, or by the server on a replayed code or refresh token |
| Query share | `query_share.create`, `query_share.delete` | A share link is created or deleted |
| Report | `report.create`, `report.update`, `report.delete`, `report.pause`, `report.resume`, `report.claim` | A scheduled report's configuration changes, or it is paused (by a user or by the scheduler), resumed, or claimed by a new owner. Report runs and sends are in the report's own run history |
| Report recipient | `report_recipient.remove` | A direct recipient takes themself off a report. Recipients that the owner adds or removes are in the `recipients` field of `report.create` and `report.update` |
| Saved query | `saved_query.create`, `saved_query.update`, `saved_query.delete` | A saved query changes. A delete also removes the query's scheduled reports, with no `report.delete` of their own: `detail.reports_removed` counts them |
| Saved view | `saved_view.create`, `saved_view.update`, `saved_view.delete`, `saved_view.set_default`, `saved_view.clear_default` | An Explorer view changes or becomes or stops being a default |
| Schema advisor dismissal | `schema_advisor_dismissal.create`, `schema_advisor_dismissal.delete` | An admin dismisses or restores a schema advisor finding |
| Schema advisor schedule | `schema_advisor_schedule.update` | An admin turns a source's weekly schema analysis on or off |
| Service account | `service_account.create`, `service_account.delete` | A service account is created or deleted |
| Session | `session.login`, `session.logout` | A local or OIDC sign-in (`detail.method`), including a failed one, and a sign-out |
| Setting | `setting.update`, `setting.delete`, `setting.test` | A system setting changes, or the test email or webhook is sent |
| Source | `source.create`, `source.update`, `source.delete` | A source changes |
| Team | `team.create`, `team.update`, `team.delete` | A team changes |
| Team member | `team_member.add`, `team_member.remove` | A user joins or leaves a team. A role change is a `team_member.add` with a `role` diff |
| Team source | `team_source.link`, `team_source.unlink` | A source is linked to or unlinked from a team |
| User | `user.create`, `user.update`, `user.delete` | A user changes. Role and status are in `changes` |

### Events from outside a request

Some changes happen outside the request handlers. Except for a client revoke
at `/oauth/revoke`, they have `actor_type` `system` and no IP address.

| Writer | Actions | Actor |
|--------|---------|-------|
| [Provisioning](/getting-started/provisioning/) | `source.*`, `team.*`, `team_member.*`, `team_source.*`, `user.create`, and `user.update` when a run marks an existing user as managed | `provisioning`, with one run ID per run in `credential_id`. Adopting an existing source or team is an `update` with `managed` going from `false` to `true` |
| Admins from `auth.admin_emails` at startup | `user.create`, `user.update` | `startup`, label `auth.admin_emails` |
| The local admin from `[auth.local]` at startup | `user.create` | `startup`, label `auth.local` |
| OIDC auto-provisioning on first sign-in | `user.create`, `team_member.add` | `startup`, label `auth.auto_provision`, with the matched domain in `detail.domain` |
| The OAuth server | `oauth_grant.revoke` | On a replayed authorization code or refresh token, with `detail.cause` `code_replay` or `refresh_replay`. A client revoke at `/oauth/revoke` has the grant's user as actor |
| The report scheduler | `report.pause` | `scheduler`, with the run in `credential_id` and the cause in `detail.reason` |

A provisioning run records its events only after its transaction commits. A
dry run, or a run that fails, records nothing.

Two gaps are known:

- The local admin that `ensureLocalAdmin` creates at startup, and the password
  it sets, record no event.
- A replayed OAuth token records no client IP, because a `system` event has
  none. The grant ID and the cause identify the replay.

## Reading events

Only global admins read the audit log. Team admins and members have no access.
Nobody can change or delete an event through Logchef.

### In the UI

**Administration → Audit Log** (`/admin/audit-log`) lists events newest first.
Filter by time range, action (an exact action or a `resource.` prefix), actor,
outcome, and target type and ID. The filters live in the URL, so
`/admin/audit-log?target_type=source&target_id=7` is the history of one
source, and each expanded row links to its target's history. A row expands to
show the changed fields, the detail, the route, the status, the auth method,
the credential and client IDs and the user agent. **Load more** fetches the
next page, and **Download** exports the events that match the filters.

### Through the API

Both routes need a global admin: a session, or an API token with the
`audit:read` scope (or `*`). OAuth tokens, including a `logchef auth` sign-in
and MCP clients, are always refused. `audit:read` is not part of the
Read-only token preset, because events hold emails and IP addresses.

```
GET /api/v1/admin/audit-events
```

| Parameter | Meaning |
|-----------|---------|
| `from`, `to` | RFC 3339. Default: the 7 days before `to`, which defaults to now |
| `action` | An exact action, or a resource with a trailing dot such as `source.` |
| `actor_id`, `auth_method`, `credential_id` | Exact match |
| `target_type`, `target_id` | Exact match. `target_id` needs `target_type` |
| `team_id`, `source_id`, `outcome` | Exact match |
| `order` | `desc` (default) or `asc` |
| `limit` | Default 100, at most 500 |
| `cursor` | The `next_cursor` of the previous page |

The response is the standard envelope,
`{"status": "success", "data": {"events": [...], "next_cursor": "..."}}`. Pass
`data.next_cursor` as `cursor` with the same filters to get the next page; it
is empty on the last page. A bad parameter, or an unknown action or resource, is
a 400.

```bash
curl -H "Authorization: Bearer $LOGCHEF_TOKEN" \
  "https://logchef.example.com/api/v1/admin/audit-events?action=source.&limit=50"
```

```
GET /api/v1/admin/audit-events/export
```

The export takes the same filters and streams `application/x-ndjson`, one
event per line. `to` is capped at now. Above 100,000 matching events it answers
400 and asks for a narrower time range. Each export records an
`audit_event.export` event with the filters and the row count.

### From the CLI

```bash
# Every source change in the last 24 hours
logchef audit list --action source. --since 24h

# One source's history as JSON lines
logchef audit list --target source:7 --output jsonl

# Failed sign-ins over the last week
logchef audit list --action session.login --outcome failure --since 7d
```

`--since` and `--until` take a lookback such as `24h` or an RFC 3339 time.
`--actor` takes a user ID, `--target` a type or `type:id`, and `--limit`
(default 100) is the total; the CLI follows the cursor for you. The CLI needs
an API token with `audit:read`, passed with `--token` or
`LOGCHEF_AUTH_TOKEN`.

## The log line

Each event is also written to the server log as one JSON line with `msg`
`audit`, level `info`, every field above as a top-level attribute, and
`stored`, which says whether the database insert succeeded. A line with
`"stored": false` is the only copy of an event whose insert failed.

```json
{"time":"2026-11-16T09:12:03.512Z","level":"INFO","msg":"audit","id":"019a8c3e-...","occurred_at":"2026-11-16T09:12:03.511Z","action":"source.update","outcome":"success","reason":"","actor_type":"user","actor_id":1,"actor_label":"admin@example.com","auth_method":"session","credential_id":"","client_id":"","target_type":"source","target_id":"7","target_label":"nginx","team_id":null,"source_id":7,"ip":"203.0.113.10","user_agent":"Mozilla/5.0 ...","http_method":"PUT","route":"/api/v1/admin/sources/:sourceID","status":200,"changes":{"ttl_days":{"from":7,"to":30}},"detail":null,"stored":true}
```

To keep a copy off the host, have your shipper select lines whose `msg` is
`audit`. For example, with [Vector](/integration/vector/):

```toml
[transforms.logchef_audit]
type = "filter"
inputs = ["logchef_logs"]
condition = '.msg == "audit"'
```

Set `audit.log_events = false` to turn the line off.

## Configuration

```toml
[audit]
retention_days = 365
max_rows = 1000000
log_events = true
```

| Key | Default | Meaning |
|-----|---------|---------|
| `audit.retention_days` | `365` | Delete events older than this many days. `0` keeps every event |
| `audit.max_rows` | `1000000` | Delete the oldest events above this count. `0` removes the bound |
| `audit.log_events` | `true` | Write the `audit` log line |

A negative value falls back to the default. The keys live in the config file,
not in system settings, so an admin session cannot shorten retention from the
UI. Like every key, they can be set with environment variables, such as
`LOGCHEF_AUDIT__RETENTION_DAYS`.

Every five minutes each server deletes at most 5,000 events, oldest first, by
both bounds. A larger backlog drains over the following runs. Pruning is
idempotent, so replicas can run it at once. It logs the count and records no
audit event. On a busy instance `max_rows` can end retention before
`retention_days`.

With `demo.read_only` set, events go to the log line only.

## When an audit write fails

Most events are recorded after the change commits, and **fail open**. If the
insert fails, the change stands, the request succeeds, the log line says
`"stored": false`, and `logchef_audit_events_total{result="failed"}` counts it.
A provisioning run also fails open: a failed insert does not stop startup.
Sign-in fails open too, so a broken audit table cannot lock out the admins who
need to repair it.

Three actions **fail closed**: no credential and no log data leaves the server
without a stored event.

| Action | On a failed insert |
|--------|--------------------|
| `api_token.create`, personal and service account | The token is rolled back. The request answers 503 and no token exists |
| `log_export.create`, streamed | The request answers 503 before the query runs. No log data is sent |
| `log_export.create`, export job | The job is rolled back. The request answers 503 and no job exists |

`log_export.download` fails open, because `log_export.create` already
recorded who made the file.

## Limits

| Bound | Value | What you see |
|-------|-------|--------------|
| `changes` plus `detail` | 16 KiB | Later keys dropped, `detail.truncated` is `true` |
| One string value | 1,024 bytes | The value ends with `...` |
| List page | 500 events | `limit` is clamped |
| Export | 100,000 events | 400 asking for a narrower time range |
| Insert timeout | 2 seconds | Fails open or closed by action |
| Pruning | 5,000 events per 5 minutes per server | A backlog drains over several runs |
| Failed sign-in events | 600 per minute per server | Extra events go to the log line only, counted as `limited` |

The failed sign-in bound exists because that is the one event an
unauthenticated caller can cause. Without it, a credential-stuffing run could
fill the table and push real events past `audit.max_rows`. With several
replicas the bound is per process, and events from different replicas that are
milliseconds apart can sort out of order under clock skew.

## Metrics

| Metric | Meaning |
|--------|---------|
| `logchef_audit_events_total{result}` | Events by result: `stored`, `failed` (the insert failed; the log line has the event) or `limited` (the failed sign-in bound kept it out of the table) |
| `logchef_audit_redactions_total` | Values the serializer replaced with `[redacted]` |
| `logchef_audit_unstaged_total` | Events whose handler staged no target or fields. Above zero means a code path is missing its audit fields |

See [Metrics Reference](/operations/metrics/#audit-metrics).
