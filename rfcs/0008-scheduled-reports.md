# RFC 0008: Scheduled reports

Status: Draft
Date: 2026-11-15
Authors: Karan Sharma

## Summary

A scheduled report sends a dashboard or a saved query to recipients by email
on a schedule, for example a 5xx summary every morning at 09:00. Logchef has
no such feature, and users who move from Metabase expect its subscriptions.

This RFC makes five decisions.

1. The email is HTML built on the server. A stat panel becomes a tile, a table
   panel becomes a table and a chart becomes a table with bars. The server
   renders no chart image and runs no browser.
2. A report runs with the access of its owner, checked again at every run. It
   never uses the admin bypass.
3. A Logchef user who receives a report sees only the panels that the UI would
   show that user. An external address needs a setting that only a global
   admin turns on.
4. A schedule is a typed object (hourly, daily, weekly or monthly) with one
   IANA timezone. The same timezone sets the time range of the report.
5. One report has one run in flight, and a recipient gets an edition at most
   once. Every run and every delivery lands in a history.

## Problem

Logchef sends email for alerts only, and that email cannot carry a report.

- The alert sender writes one fixed `text/plain` message from the fields of an
  alert (`internal/alerts/email_sender.go:129-141`). It has no HTML part and
  no attachment. It also puts the alert name into the `Subject` header as it
  is (`internal/alerts/email_sender.go:130`).
- The only scheduler is the alert manager. It is one ticker that lists due
  alerts and evaluates them in a loop (`internal/alerts/manager.go:112-129`,
  `internal/alerts/manager.go:155`). Nothing else can register work with it.
  The docs tell operators to run exactly one replica of it
  (`docs/src/content/docs/operations/database-backends.md:65`).
- A dashboard has no server-side run path. The comment on the routes says
  that the frontend fetches panel data through the team-scoped log endpoints
  (`internal/server/server.go:521-526`). The server stores the panel blob and
  never runs it.
- The column kinds, the stat reducers and the number formats of RFC 0003 live
  in the frontend (`frontend/src/utils/panelFrame.ts`,
  `frontend/src/utils/panelPresentation.ts`,
  `frontend/src/utils/numberFormat.ts`). The Go side has only the exact number
  parser (`pkg/models/dashboard_presentation.go:18`).
- Charts draw in the browser with unovis (`frontend/package.json:24`).
  `go.mod` holds no chart library, no cron library and no headless browser.
- Export jobs write a file for one user to download
  (`internal/server/export_job_handlers.go:24`). They run only on ClickHouse,
  because they take the ClickHouse connection directly
  (`internal/server/export_job_handlers.go:231`).

A user who wants a daily summary today opens the dashboard by hand, or writes
a cron job around the CLI.

## Goals and non-goals

Goals:

- Send a dashboard, one tab of a dashboard, or a saved query by email on a
  schedule.
- Make the email useful without a click. It carries the numbers and the first
  rows, and it links to the live view.
- Never show a recipient data that Logchef would not show that recipient,
  unless a global admin allowed external recipients.
- Keep running correctly when the owner loses access, is deactivated or is
  deleted. "Correctly" means that the report stops and says why.
- Run once per tick with several replicas on Postgres.

Non-goals:

- Chart images, PDF and screenshots. "Alternatives rejected" gives the reasons.
- A report on an Explorer saved view. RFC 0007 owns that object. "Seams" names
  the dependency.
- The destinations of RFC 0006 as recipients, in the first release. That
  covers Slack, webhook, PagerDuty and email destinations. "Seams" gives the
  rule that a later unit must keep.
- Reports from raw query text. A report always points at a stored dashboard
  or saved query, so there is one place to edit the query.
- A raw cron expression. The typed schedule covers what Metabase offers.
- An exact calendar month as a time range. The range grammar of RFC 0003 has
  no such form, and this RFC does not extend it.
- A per-member opt-out for team recipients. A user who is a direct recipient
  can remove themself. A team member asks the owner.
- Dashboard variables. The schema 2 blob has no variables field yet
  (`pkg/models/dashboards_v2.go:129-136`). Saved-query variables are in scope.
- Comparison deltas on stat tiles (`compare.mode`). The tile shows the value
  and the row date. The delta follows when a user asks for it.
- Provisioning reports from TOML.

## Design

### Data shape

Three tables carry the feature. "Migrations" gives their columns. The models
`Report`, `ReportRecipient` and `ReportRun` in `pkg/models/reports.go` mirror
the columns one to one, with typed ids and typed strings for each enumerated
column.

| Object | Holds |
|---|---|
| `Report` | What to send, when, and as whom |
| `ReportRecipient` | One user, team or address |
| `ReportRun` | One attempt, with its window, its panel outcomes and its deliveries |

The schedule is a value inside `Report`, stored as JSON.

```go
type ReportFrequency string // "hourly" | "daily" | "weekly" | "monthly"

// ReportSchedule is the whole schedule. A field that the frequency does not
// use must be absent, and validation rejects it.
type ReportSchedule struct {
	Frequency  ReportFrequency `json:"frequency"`
	Minute     int             `json:"minute"`                 // 0-59
	Hour       *int            `json:"hour,omitempty"`         // 0-23, not hourly
	Weekdays   []int           `json:"weekdays,omitempty"`     // 0-6, Sunday is 0, weekly only
	DayOfMonth *int            `json:"day_of_month,omitempty"` // 1-28, monthly only
}
```

| Column | Values |
|---|---|
| `reports.source_kind` | `dashboard`, `saved_query` |
| `reports.state` | `active`, `paused` |
| `reports.pause_reason` | empty, `user`, `owner_inactive`, `failures` |
| `reports.range_anchor` | `send_time`, `day_start` |
| `reports.time_range` | The range grammar of RFC 0003. NULL means the dashboard default |
| `reports.variables_json` | An object of saved-query variable values, by name |
| `reports.max_rows` | 5 to 100, default 25. Rows shown per table |
| `report_recipients.kind` | `user`, `team`, `email` |
| `report_runs.trigger` | `schedule`, `manual` |
| `report_runs.scheduled_for` | The tick in UTC. The request time for a manual run |
| `report_runs.status` | `running`, `sent`, `partial`, `skipped`, `failed`, `interrupted` |
| `report_runs.reason` | A fixed code. "Run flow" lists them |

A run stores two JSON arrays.

| Column | Fields of one entry |
|---|---|
| `panels_json` | `panel_id`, `title`, `status` (`ok`, `empty`, `locked`, `failed`, `unsupported`), `rows`, `truncation` (as RFC 0003), `error` |
| `deliveries_json` | `class` (`user` or `external`), `target` (a user id or an address), `via` (`direct`, `team:<id>` or `email`), `status` (`sent`, `failed`, `skipped`, `unknown`), `attempts`, `reason`, `locked_panels` |

Three rules follow from the shape.

- A report has exactly one source. A check constraint holds the rule, so no
  row can name both a dashboard and a saved query.
- The schedule is a typed value. No field expresses an interval below one
  hour, so nobody can write one.
- A run row is the claim. One partial unique index allows one scheduled row
  per tick, and a second allows one `running` row per report. "Run flow"
  gives the statements.

### Migrations

One migration per store, named `add_reports`, with the next free number at
build time. The SQLite form follows. The Postgres form uses `BIGSERIAL`,
`TIMESTAMPTZ` and `BOOLEAN`, as the existing Postgres migrations do.

```sql
CREATE TABLE reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('dashboard', 'saved_query')),
    dashboard_id INTEGER REFERENCES dashboards(id) ON DELETE CASCADE,
    tab_id TEXT,
    saved_query_id INTEGER REFERENCES saved_queries(id) ON DELETE CASCADE,
    schedule_json TEXT NOT NULL,
    timezone TEXT NOT NULL,
    time_range TEXT,
    range_anchor TEXT NOT NULL DEFAULT 'send_time',
    variables_json TEXT NOT NULL DEFAULT '{}',
    max_rows INTEGER NOT NULL DEFAULT 25,
    skip_if_empty BOOLEAN NOT NULL DEFAULT 0,
    attach_csv BOOLEAN NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'active',
    pause_reason TEXT NOT NULL DEFAULT '',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_manual_run_at DATETIME,
    last_preview_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    CHECK ((source_kind = 'dashboard' AND dashboard_id IS NOT NULL AND saved_query_id IS NULL)
        OR (source_kind = 'saved_query' AND saved_query_id IS NOT NULL AND dashboard_id IS NULL))
);
CREATE INDEX idx_reports_owner ON reports(owner_id);
CREATE INDEX idx_reports_dashboard ON reports(dashboard_id);
CREATE INDEX idx_reports_saved_query ON reports(saved_query_id);

CREATE TABLE report_recipients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('user', 'team', 'email')),
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    team_id INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    email TEXT NOT NULL DEFAULT '',
    added_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX idx_report_recipients_target
    ON report_recipients(report_id, kind, COALESCE(user_id, 0), COALESCE(team_id, 0),
                         email);

CREATE TABLE report_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
    trigger TEXT NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    scheduled_for DATETIME NOT NULL,
    actor_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    window_start DATETIME,
    window_end DATETIME,
    panels_json TEXT NOT NULL DEFAULT '[]',
    deliveries_json TEXT NOT NULL DEFAULT '[]',
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    deadline_at DATETIME,
    finished_at DATETIME
);
CREATE UNIQUE INDEX idx_report_runs_tick
    ON report_runs(report_id, scheduled_for) WHERE trigger = 'schedule';
CREATE UNIQUE INDEX idx_report_runs_running
    ON report_runs(report_id) WHERE status = 'running';
CREATE INDEX idx_report_runs_report ON report_runs(report_id, id);
```

- A deleted user or team takes its recipient rows with it. A deleted
  dashboard or saved query takes its reports with it. The delete dialogs of
  both say how many reports go.
- A deleted owner leaves the report in place with `owner_id` NULL, as
  dashboards do (`internal/store/sqlite/migrations/000029_add_dashboards.up.sql:11`).
- The migration creates tables only and rebuilds none, so it does not depend
  on how SQLite treats foreign keys during a migration.
- The down migration drops the three tables.
- The saved-query table is `saved_queries`
  (`internal/store/sqlite/migrations/000027_remove_legacy_query_type.up.sql:31`).

Queries go into `queries.sql` of both stores, and the store interface gains
the report methods. Conformance tests go into `internal/store/storetest/`.

### API

All routes sit in a new group `api.Group("/reports", s.requireAuth)`, next to
the dashboard group (`internal/server/server.go:526`). Two token scopes are
new: `reports:read` and `reports:write`. Both join `validTokenScopes`
(`internal/core/api_tokens.go:60`). `reports:read` joins the read-only preset
(`internal/core/api_tokens.go:87`).

| Method and path | Scope | Who | Result |
|---|---|---|---|
| `GET /reports?scope=mine\|received\|all` | `reports:read` | Any user. `all` needs a global admin | A list |
| `POST /reports` | `reports:write` | Any user who can view the source | 201 with the report |
| `GET /reports/:id` | `reports:read` | Owner, global admin, or a recipient | The report |
| `PUT /reports/:id` | `reports:write` | Owner or global admin | The report |
| `DELETE /reports/:id` | `reports:write` | Owner or global admin | 204 |
| `POST /reports/:id/pause` and `/resume` | `reports:write` | Owner or global admin | The report |
| `POST /reports/:id/claim` | `reports:write` | A global admin, or any user who can view the source, when the report has no owner | The report, owned by the caller |
| `POST /reports/:id/run` | `reports:write` | Owner or global admin | 202 with the run |
| `POST /reports/:id/preview` | `reports:read` and `logs:read` | Owner or global admin | The HTML that the caller would receive |
| `GET /reports/:id/runs?limit=&before=` | `reports:read` | Owner or global admin | Runs, newest first |
| `DELETE /reports/:id/recipients/me` | `reports:write` | A direct user recipient | 204 |

A recipient who is not the owner gets a reduced `GET`: name, owner, source
name, schedule and timezone. The recipient list, the variables and the runs
are left out.

The request body of `POST` and `PUT`:

```json
{
  "name": "Daily 5xx summary",
  "source": { "kind": "dashboard", "dashboard_id": 12, "tab_id": "main" },
  "schedule": { "frequency": "daily", "hour": 9, "minute": 0 },
  "timezone": "Asia/Kolkata",
  "time_range": "24h",
  "range_anchor": "day_start",
  "variables": {},
  "max_rows": 25,
  "skip_if_empty": false,
  "attach_csv": false,
  "recipients": [
    { "kind": "user", "user_id": 7 },
    { "kind": "team", "team_id": 3 },
    { "kind": "email", "email": "oncall@example.com" }
  ]
}
```

The response adds `id`, `owner` (`id`, `name`, `email`, or null), `state`,
`pause_reason`, `next_run_at`, `last_run` (status, reason and time),
`can_edit`, and `warnings`. A warning is a fixed code with a panel id, for
example `panel_locked_for_owner`.

`POST /reports/:id/run` takes `{"recipients": "me" | "all"}`. The default is
`me`, which sends one email to the caller. `all` sends to every recipient.

Errors:

| Status | Case |
|---|---|
| 400 | A field fails validation. The message names the field |
| 400 | `kind: "email"` while external recipients are off, or the domain is not allowed |
| 403 | The caller may not edit, or the owner cannot view the new source |
| 404 | The report does not exist, or the caller may not see it |
| 409 | A run of this report is in flight. The `running` index decides |
| 429 | A manual run within 60 seconds of the last manual run of this report, or a preview within 60 seconds of the last preview. One conditional update decides |
| 503 | Reports are disabled on this server |

Validation at save, in `internal/core/reports.go`:

- `name` is 1 to 200 code points and holds no CR or LF.
- The owner can view the dashboard (`UserCanViewDashboard`,
  `internal/core/dashboards.go:402`) or can reach the source of the saved
  query (`ListTeamsWithAccessToSource`, `internal/core/teams.go:491`). The
  check uses the owner, also when a global admin edits the report.
- `tab_id` names a tab of the dashboard. The source has at most 30 query
  panels. A larger dashboard needs a `tab_id`.
- `timezone` passes `core.ValidateTimezone` (`internal/core/query.go:73`).
- `time_range` matches the dashboard range patterns
  (`pkg/models/dashboards.go:186`, `pkg/models/dashboards.go:190`). A saved
  query requires it. `range_anchor: "day_start"` takes a rolling range only.
- `variables` holds only names that the saved query declares
  (`pkg/models/query.go:272-291`). Every variable that the query needs has a
  value, from the report or from the stored default.
- `attach_csv` needs `source.kind` to be `saved_query`.
- At most 50 recipient rows. A team expands at send time.
- The schedule passes the field rules in "Data shape".

### Run identity

A report runs as its owner. The owner is the user who created it, or the user
who claimed it.

A new principal carries the rule. It follows `AssistantPrincipal`
(`internal/core/access/access.go:98`).

```go
// ReportPrincipal builds the principal a scheduled report uses for its owner.
// It holds logs:read, dashboards:read and saved_queries:read, and it never
// passes RequireGlobalAdmin.
func ReportPrincipal(owner *models.User) Principal
```

- `RequireGlobalAdmin` refuses the new method, as it refuses the assistant
  (`internal/core/access/access.go:131`). A report owned by a global admin
  reaches only the teams that the admin is a member of. An unattended job
  with instance-wide reach is a larger risk than a locked panel.
- Every panel passes `access.AuthorizeTeamSource`
  (`internal/core/access/access.go:166`) at every run. Nothing is cached, and
  no grant is stored on the report.
- The queries then take the paths that HTTP and MCP share: `core.RunQuery`
  (`internal/core/query.go:180`), `core.RunLogchefQL`
  (`internal/core/query.go:312`) and `core.RunHistogram`
  (`internal/core/query.go:482`). A report adds no new read path.

What happens when the owner changes:

| Event | Effect on the next run |
|---|---|
| The owner leaves a team, or a source is unlinked | The panels on that source are locked for everyone. The email says "The report owner no longer has access to this panel" |
| Every query panel is locked | The run fails with `no_access`. Nothing is sent |
| The owner can no longer view the dashboard | The run fails with `no_access` |
| The owner is deactivated | The run is skipped with `owner_inactive`, and the report pauses with the same reason. The check is the one the session middleware applies (`internal/server/middleware.go:189`) |
| The owner is deleted | `owner_id` becomes NULL. The report does not run. The list shows "No owner" |
| The dashboard or saved query is deleted | The report is deleted with it |

A report with no owner stays until someone claims it or deletes it. A claim
makes the caller the owner, so the caller lends their own access and nobody
else's. The claim clears a pause with reason `owner_inactive`.

### Recipients

A recipient row is one of three kinds. Every row resolves to individual
people at send time, and each person gets their own message.

| Kind | Resolves to |
|---|---|
| `user` | One user |
| `team` | The members of the team at send time |
| `email` | One address. At send time the server looks the address up among the users, without regard to case |

**Resolution.** The run turns the rows into a list of individual recipients
and removes duplicates by address. Each one is then either a Logchef user or
an external address. The class comes from the users table at send time, never
from the row kind. An address that was external when the owner added it, and
that belongs to a user today, is a Logchef user for this run.

| Class | Must pass | Receives |
|---|---|---|
| Logchef user | Active, a human account, has an email address | The panels that this user can reach. Others are locked |
| External address | Both external-recipient settings | Every panel that the owner can reach |

A user who fails the first test gets a `skipped` delivery with reason
`inactive`.

**What a Logchef user can reach.** The test depends on the source of the
report.

- A dashboard panel names a team and a source. The user must be a member of
  that team, and the team must have the source. That is the test the
  dashboard applies to a viewer (`internal/core/dashboards.go:312`), without
  its exemption for the creator and for admins.
- A saved query names a source and no team. It is visible through any team
  that has the source (`pkg/models/query.go:293-296`), and
  `CreatedFromTeamID` is optional. The user passes when at least one of the
  user's own teams has the source, which is what
  `ListTeamsWithAccessToSource` returns (`internal/core/teams.go:491`). The
  team that the owner's run used plays no part. Two users who reach one
  source through different teams both receive the report.

The queries run once, as the owner. The server then builds one message per
recipient from the rendered panels and puts a locked placeholder where the
test fails. The same result decides the text part and the attachment. A CSV
goes only to a recipient who passes the test for the saved query. A report
therefore moves no data to a Logchef user that the UI would not show.

A recipient with every panel locked gets no email. The delivery is recorded
as `skipped` with reason `no_access`.

**External addresses.** An address outside Logchef takes data out of the
system, and Logchef cannot check what that reader may see. Two settings gate
it. Both live in the settings table, next to the SMTP settings
(`internal/server/admin_settings_handlers.go:280-293`), so only a global
admin changes them.

| Setting | Default | Meaning |
|---|---|---|
| `reports.external_recipients` | `false` | When false, no external address receives a report |
| `reports.allowed_email_domains` | empty | A comma-separated list of domains. An external address must match one. `*` allows every domain |

- With the first setting on and the list empty, no address passes. The admin
  page says so.
- At save, an `email` row whose address belongs to a user is stored as a
  `user` row. That is a convenience. The rule that protects the data is the
  lookup at send time.
- The server checks both settings at save and again at every send. Turning
  the setting off stops delivery to external addresses on the next run. The
  delivery is recorded as `skipped` with reason `external_disabled`.
- Once the setting is on, any report owner may add an allowed address. The
  admin decides the policy, and the owner decides the use. Each added address
  is an audit event.

**Restricted sources (RFC 0002).** A restricted view is its own source with
its own connection user. A report on it runs through that source and reads
only the rows that the database allows. The per-user test keeps the report
inside the teams linked to the restricted source. RFC 0002 gives a source no
machine-readable "restricted" mark, so the external-recipient settings are
the only control over mail that leaves the instance. When the display-only
`access_note` of RFC 0002 exists, the email footer prints it next to the
source name.

Each recipient gets its own SMTP transaction, as alert email does
(`internal/alerts/email_sender.go:197`). No recipient sees another's address.

### Schedule and time

**Schedule.** The UI offers the four frequencies as presets, for example
"Monday to Friday at 09:00". Each preset maps to one `ReportSchedule`. The
schedule runs in the report's `timezone`. `next_run_at` in the response comes
from the job runner.

**One timezone.** RFC 0003 takes the timezone of a range from the viewer. A
report has no viewer, so the report's `timezone` takes that role. It sets the
schedule, the range, `{{__timezone}}`, the buckets of count mode and the way
the email prints an instant. Every recipient gets the same numbers, and the
email header names the zone.

**The range.** `T` is the effective time `At` that the runner hands to the
handler. It is the due occurrence that the runner selected, not the time the
job started. A report that starts 40 seconds late still covers the same
window.

| `range_anchor` | "Now" for the range | `24h` at a 09:00 tick |
|---|---|---|
| `send_time` | `T` | Yesterday 09:00 to today 09:00 |
| `day_start` | The start of the day of `T` in the report's timezone | Yesterday 00:00 to today 00:00 |

- Calendar forms (`today`, `yesterday`, `today-<n>d`) resolve at `T` in the
  report's timezone, as RFC 0003 defines them for a viewer.
- With `day_start`, a range in days or weeks subtracts calendar days, so a
  daylight-saving change keeps the window on midnight.
- `time_range` null uses the dashboard's `default_time_range`, and `24h` when
  the dashboard has none.
- A panel with its own `query.range` keeps it, resolved at the same anchor.
  The panel header prints that range.

**Macros.** The run fills the macro inputs that RFC 0003 defines and that
`template.Builtins` already takes (`internal/template/builtins.go:66`):
`From` and `To` from the window, `Location` from the report's timezone, and
`Interval`.

- A ClickHouse SQL panel that uses no time macro is a fixed-window panel. Its
  SQL sets the window. The email prints "fixed window" on it, as the
  dashboard chip does.
- `query.interval: "auto"` in result mode resolves with the client's rule of
  about 100 buckets, ported to Go.
- A count chart shows at most 24 bars, because 24 bars fit an email. The run
  asks for the smallest window of `template.IntervalWindows`
  (`internal/template/builtins.go:55`) that gives at most 24 buckets. The
  largest window is 24 hours (`internal/template/builtins.go:41`), so a range
  above 24 days has no such window. The run then asks for 24 hours and adds
  adjacent buckets into groups of equal size until at most 24 remain. A count
  adds exactly. A 30-day range gives 30 buckets and 15 bars of two days.
- A saved query takes `variables` from the report, then the stored defaults.
  A `day` default of `today` or `yesterday` resolves at `T` in the report's
  timezone.

### Rendering

`internal/reports` turns a source into rendered panels. Each rendered panel
keeps its source, and its team when it comes from a dashboard, so the
per-recipient test can lock it later.
The package has no HTTP and no SMTP code.

Each visualization has one email form.

| Viz | Email form |
|---|---|
| `stat` | A tile with the reduced value, the unit and the row date. No sparkline and no comparison |
| `table` | An HTML table of the first `max_rows` rows, with `visible_columns` applied. A last line says "N more rows" and links to the dashboard |
| `chart` | A table of the x value, the y value and a bar. The bar is a cell whose width is the share of the largest value. One block per series, at most 5 series |
| `toplist` | The same bar table, sorted as the panel sorts it |
| `pie` | The same bar table, with the percent of the total |
| Unknown viz or panel kind | A line with the title and a link. Status `unsupported` |

- A saved query renders as one `table` panel.
- Panels follow the reading order of the layout (row, then column), tab by
  tab. The email is one column. It does not copy the grid.
- A stat tile needs the reducers of RFC 0003 in Go: `sum`, `avg`, `min`,
  `max`, `first`, `last`, with `order_by`, the NULL rules and the "No data"
  cases. They use the exact parser that exists
  (`pkg/models/dashboard_presentation.go:18`), so wide integers and decimals
  keep every digit.
- Number formats (`decimals`, `unit`, `notation`) get a Go formatter. One
  fixture file, `pkg/models/testdata/report_number_cases.json`, feeds both the
  Go test and a vitest test of `frontend/src/utils/numberFormat.ts`. A change
  on one side fails the other side's test.
- An instant prints in the report's timezone as `2026-11-15 09:00 IST`. A
  ClickHouse `Date` prints as the date. Every other cell prints as the query
  returned it.
- A truncated panel prints the label of RFC 0003: "First N rows", "Cut by
  size" or "May be incomplete". A sum over a cut result is wrong without it.
- `html/template` escapes every value. The repo already uses the package
  (`internal/oauth/boundary.go:5`). Styles are inline, the layout uses tables,
  and the email loads no remote image, script or font.
- The message is `multipart/alternative` with a text part and an HTML part.
  With a CSV it is `multipart/mixed` around that.
- The subject is `[Logchef] <report name>, <date in the report timezone>`,
  encoded as an RFC 2047 word.
- The footer names the owner, the sources, the window and the timezone. It
  links to the dashboard or saved query, with the window in the URL, and to
  the report. Links use `uiURL` (`internal/server/meta_handlers.go:59`). With
  no URL configured, the email has no links and the report response carries
  the warning `no_frontend_url`.

**CSV.** `attach_csv` attaches the rows of a saved query as one file. The run
builds it from the `QueryResult` it already holds, so it works on every
backend. The export job path cannot serve here, because it is bound to
ClickHouse (`internal/server/export_job_handlers.go:231`). The cell rule is
`csvValue` (`internal/server/export_handlers.go:359`), which moves to
`internal/util` so that both callers share it.

### Run flow

A scheduled run and a manual run take the same path. A manual run starts in
a background task and the route returns 202.

1. Claim the report. "The claim" below gives the statements.
2. Load the report, the owner and the source. Apply the owner rules of "Run
   identity".
3. Resolve the window from `T`.
4. Run the panels through a pool of 4, the size the dashboard view uses
   (`frontend/src/stores/dashboards.ts:220-221`). Each query has a 60 second
   timeout (`pkg/models/query.go:11`). All queries share the query budget.
5. Render. Apply the size rules of "Limits and failure modes".
6. Decide the outcome from the table below. Stop here when nothing is sent.
7. Resolve the recipients. Build and send one message per recipient.
8. Write the final status, the panels and the deliveries in one update. Prune
   the runs of this report beyond the newest 100.

**The claim.** One report has at most one run in flight, whatever started
it. Two partial unique indexes on `report_runs` hold the rule in the
database, so it also holds across replicas.

- `idx_report_runs_running` allows one row with status `running` per report.
- `idx_report_runs_tick` allows one scheduled row per report and tick.

A scheduled run inserts its row with status `running`. On a conflict it
reads the tick. If a row for the tick exists, another replica or an earlier
attempt holds it, and the job ends without work. Otherwise another run of
the report is in flight, and the job inserts the tick with status `skipped`
and reason `busy`.

A manual run first takes the cooldown with one statement, then inserts its
`running` row.

```sql
UPDATE reports SET last_manual_run_at = :now
WHERE id = :id
  AND (last_manual_run_at IS NULL OR last_manual_run_at <= :now_minus_60s);
```

No row updated is a 429. A conflict on the insert is a 409. `:now` is the
database clock. A preview takes the same statement on `last_preview_at` and
inserts no run row.

**The time budget.** A run has two deadlines. The query deadline is the
start plus the query budget. The handler deadline is the query deadline plus
5 minutes. The claim writes the handler deadline into `deadline_at` on the
run row, from the database clock. A run with a status other than `running`
has no `deadline_at`. RFC 0006 ends the handler's context at the handler
deadline, so the run divides it.

| Phase | Ends at | When it ends early |
|---|---|---|
| Queries | The query deadline. `reports.query_budget` is 5 minutes by default and 30 minutes at most | Panels still running fail with `timeout`. The run goes on with the others |
| Render and delivery | 15 seconds before the handler deadline | Recipients not yet reached get a `skipped` delivery with reason `deadline` |
| Final update | 10 seconds, on a context that the handler deadline does not cancel | The row stays `running` and becomes `interrupted` |

**Delivery.** Report delivery owns its retries and its records. `Mailer.Send`
returns a classified error, and the run acts on the class.

| Class | Cases | Action |
|---|---|---|
| Transient | Dial, TLS or timeout before the end of `DATA`. A 4xx reply at any step | Retry. At most 3 attempts, after 2 and 8 seconds, inside the delivery phase |
| Permanent | A 5xx reply, an authentication failure, SMTP not configured | `failed`, no retry |
| Unknown | The connection drops or times out after the end of `DATA` was written and before the reply arrived | `unknown`, no retry |

After the server replies 250 to the end of `DATA`, the delivery is `sent`,
whatever `QUIT` returns. The sender of today returns the `QUIT` error as a
failure (`internal/alerts/email_sender.go:208`), and a retry on it would send
a second copy. A recipient therefore gets a given edition at most once. An
`unknown` delivery may or may not have arrived, and the history says so.

Run outcomes:

| Status | Reason codes | Meaning |
|---|---|---|
| `sent` | none | Every panel rendered and every delivery succeeded |
| `partial` | `panel_failed`, `panel_locked`, `delivery_failed`, `delivery_unknown`, `deadline` | The email went out with a failed or locked panel, or a delivery did not succeed |
| `skipped` | `empty`, `owner_inactive`, `no_owner`, `paused`, `disabled`, `busy` | Nothing was sent, by rule |
| `failed` | `no_access`, `all_panels_failed`, `smtp_not_configured`, `all_deliveries_failed`, `source_invalid`, `timeout` | Nothing useful was sent |
| `interrupted` | `process_stopped` | The process ended during the run |

- "Empty" means that every query panel returned no rows. A count panel with
  no matching rows counts as empty here. `skip_if_empty` turns an empty run
  into `skipped`. Without it, each panel prints "No rows".
- A failed panel prints "This panel failed to load" in the email. The error
  text goes into the run history only, where the owner and admins read it.
  Query errors can quote SQL and field names.
- A run does not retry a failed query. The next tick is the retry.
- Three `failed` runs in a row pause the report with reason `failures`. The
  owner gets one plain email that names the report and the last reason. A
  `sent` or `partial` run resets the count.
- A run with status `running` becomes `interrupted` when its `deadline_at`
  plus 2 minutes has passed on the database clock. The cleanup loop that
  reaps export jobs does this (`internal/server/export_job_handlers.go:366`).
  The loop uses no fixed age. A live run ends its final update at most 10
  seconds after `deadline_at`, so the loop never reaps a live run, and the
  `running` index never admits a second run while the first still delivers.
  The run is not resumed. A recipient who was not
  reached misses that edition.

### Client surfaces

**Web UI.**

- A **Reports** page at `/reports`, registered next to `/dashboards`
  (`frontend/src/router/index.ts:124`). It uses the shared list pieces:
  `PageHeader`, `SearchInput` with `useTableSearchSort` (name A to Z,
  `persistKey` `reports-list`), `SortHeader`, `RowActionsMenu` (Duplicate,
  Delete), `ConfirmDialog`, `EmptyState` and `LoadingState`. Columns: name,
  source, schedule, next run, last run status, owner. Tabs: **Mine** and
  **Sent to me**. Admins also get **All**.
- A report editor dialog with four parts: source and tab, schedule and
  timezone, range and options, recipients. The recipient picker lists users
  and teams, and shows the address field only when external recipients are
  on. **Preview** opens the HTML of `/preview` in a sandboxed iframe.
- A **Schedule report** action in `DashboardToolbar.vue` and in the row menu
  of a saved query. A history drawer per report.
- **Administration > Settings** gains the two external-recipient settings.
  The delete dialogs of dashboards and saved queries print the report count.

**CLI.** `logchef reports list`, `logchef reports runs <id>` and `logchef
reports run <id> [--all]`. The CLI does not create or edit a report, because
the editor needs pickers that a terminal does not have.

**MCP and Ask Chef.** Not affected. A report is configuration, not
investigation, and a tool that schedules outbound email would give an agent a
way to move data out. The assistant scopes
(`internal/core/access/access.go:33`) gain no report scope.

**Provisioning TOML.** Not affected. Provisioning covers users, teams and
sources. A report points at a dashboard id, which provisioning does not set.

**Config.** A `[reports]` section: `enabled` (default `true`),
`max_concurrent_runs` (default 2), `query_budget` (default 5 minutes, from
1 to 30 minutes, and the server refuses to start outside that range) and
`max_attachment_bytes` (default 5 MiB). The scheduler does not start when
`demo.read_only` is set (`internal/config/config.go:46`).

**Docs.** A new page `docs/src/content/docs/features/scheduled-reports.md`.
Edits to `features/service-tokens.mdx` for the scopes, to
`operations/metrics.mdx` for the metrics, and to
`operations/database-backends.md` for the replica rule.

**Metrics.** Four metrics in `internal/metrics/metrics.go`, none with a
report id label: `logchef_report_runs_total` (`trigger`, `status`),
`logchef_report_run_duration_seconds` (`trigger`),
`logchef_report_panels_total` (`status`) and
`logchef_report_deliveries_total` (`kind`, `status`).

## Access and security

The table under "API" says who may call each route. These rules add to it.

- A global admin who edits a report does not become its owner. The report
  keeps running as the owner, and every source check uses the owner.
- A global admin who runs **Send test to me** is a `user` recipient for that
  run. The per-user test applies, so the admin sees locked panels for teams
  the admin is not a member of.
- A service account can own a report, with the team memberships it has.
- The email holds no token and no signed link. Every link needs a login.
- No secret enters a report row, a run row or an email. SMTP settings stay in
  the settings table.
- The only outbound request is SMTP to the configured host. A report takes no
  URL from a user.
- The report name goes into the subject as an encoded word, and validation
  rejects CR and LF, so a name cannot add a header.
- An error text in the run history comes from the query path and can hold
  query text. Only the owner and admins read it. A recipient never does.
- `POST /reports/:id/preview` runs queries. It is registered through
  `registerLimited` with the query limiter, like the query routes.

## Limits and failure modes

| Bound | Value | When it is hit |
|---|---|---|
| Recipient rows per report | 50 | 400 at save |
| Resolved recipients per run | 200 | The run sends to the first 200 by user id and records the rest as `skipped`, reason `recipient_limit` |
| Query panels per report | 30 | 400 at save. The message says to pick a tab |
| Rows per panel query | The panel's `query.limit`, at most 10,000 (`pkg/models/dashboards_v2.go:30`) | The panel prints "First N rows" |
| Bytes per panel result | The smaller of 4 MiB and `max_response_bytes` (`internal/config/config.go:101-102`) | The panel prints "Cut by size" |
| Rows shown per table | `max_rows`, 5 to 100 | "N more rows" with a link |
| Series per chart | 5 | "N more series" with a link |
| Bars per count chart | 24 | The interval grows, then adjacent buckets are added together |
| HTML body | 100 KiB | See below |
| CSV attachment | `reports.max_attachment_bytes`, default 5 MiB before encoding. Base64 makes it about 6.9 MiB in the message | The file is cut at a row boundary. The email says how many rows it holds |
| Query timeout | 60 seconds per panel | The panel fails |
| Query budget | `reports.query_budget`, default 5 minutes, at most 30 minutes | Panels still running fail. The run goes on with the rest |
| Handler deadline | The query budget plus 5 minutes, so at most 35 minutes. Stored per run as `deadline_at` | Recipients not yet reached are `skipped`, reason `deadline` |
| Delivery attempts | 3 per recipient, after 2 and 8 seconds | `failed` |
| Concurrent runs | `reports.max_concurrent_runs` per replica, default 2 | Later runs wait in the runner |
| Runs in flight per report | 1, over both triggers and all replicas | 409 for a manual run. `skipped`, reason `busy`, for a tick |
| Panel queries in flight per run | 4 | The rest wait |
| Manual run, and preview | One of each per report per 60 seconds | 429 |
| Run history | 100 runs per report | Older rows are deleted |
| Schedule | At most once an hour | Not expressible |

**Body size.** Mail clients cut a large HTML body. The renderer keeps the
body under 100 KiB in three steps. It halves the rows per table until the
body fits or each table shows 5 rows. It then replaces panels from the end
with a title and a link. If the body still does not fit, the run fails with
`all_panels_failed`. The email says which panels were cut.

**SMTP.** With no SMTP host, the run fails with `smtp_not_configured`. The
editor shows a warning when the test-email precondition fails
(`internal/server/admin_settings_handlers.go:442`).

**Several replicas on Postgres.** The job runner of RFC 0006 gives a tick to
one replica. `idx_report_runs_tick` refuses a second run of the same tick,
also when a stalled process lets the runner deliver a tick twice.
`idx_report_runs_running` and the conditional update keep manual runs to one
at a time, whichever replica takes the request.

**Missed ticks.** After downtime the runner selects the latest due
occurrence and drops the older ones. That occurrence runs only when it is
less than one hour old. A daily 09:00 report whose server was down from
Monday morning until Tuesday 09:30 sends Tuesday's edition and never
Monday's. The same server back at Tuesday 15:00 sends nothing until
Wednesday.

**Query activity.** Report queries do not register with the query tracker in
the first release, because the tracker lives in package `server`
(`internal/server/logs_handlers.go:38`). The concurrency cap and the timeouts
bound them. The admin Query activity page does not list them.

**Lost updates.** A report has one owner, and admins edit it rarely. Updates
are last write wins. The report has no revision column.

## Backend differences

| Topic | ClickHouse | VictoriaLogs |
|---|---|---|
| Count panels | Histogram path | Hits API. Pipes are ignored, as on the dashboard |
| Result panels | Any single SELECT | LogsQL with stats pipes. Values arrive as strings |
| Stat reducers | Typed numbers | The exact parser reads the strings |
| Buckets | Follow the report's timezone | One fixed offset. A range that crosses a daylight-saving change sits one hour off, and the panel says so |
| Time macros | All | `{{__time_filter}}` and `{{__timezone}}` are rejected, as RFC 0003 says |
| CSV attachment | Yes | Yes. The file comes from the query result |
| Restricted source | A view or a row policy on its own connection user | A scope query on its own source. RFC 0002 treats that scope as a convenience until it is verified |

The report code calls no provider directly. It needs two capabilities from
the datasource layer: a histogram for count panels, and a native query that
returns columns and rows. A source that lacks one renders the affected panel
as `unsupported`. When RFC 0011 lands, a Loki source follows the same rule
through its capability flags.

## Seams

The cross-RFC seam decisions of 2026-11-15 settle the runner and delivery
seams. This section states what reports use.

**Requires from RFC 0006, the job runner (units J1 and J3):**

- `Register` for a job kind `report`, with `Timeout` and `Concurrency`.
- `Upsert` and `Delete` of a job by report id, inside the transaction of the
  report write. A paused report has a disabled job.
- A `cron` schedule with five fields and an IANA timezone. The report code
  derives the fields from `ReportSchedule`.
- The handler receives the report id and the effective time `At`. `T` in this
  RFC is that `At`.
- The policy `skip` with `grace_seconds = 3600`. After downtime the runner
  selects the latest due occurrence, then applies the grace to it. Older
  occurrences are skipped.
- One replica runs a given tick. A stalled process can break that guarantee.
  `idx_report_runs_tick` covers the case.
- `Schedule.Next(after)` for `next_run_at`.

**Requires from RFC 0006, delivery (unit D1):**

- `notify.Mailer.Send(ctx, to, MailMessage)`, which sends one message to one
  address through the instance SMTP settings. The message has a subject, a
  text part, an HTML part and attachments with a name and a content type.
- An error from `Send` that carries one of the three classes of "Run flow".
  The class `unknown` means that the server may have accepted the message.
- Nothing else. Report delivery owns retries, backoff, deadlines, sanitized
  results and records. Reports do not use the delivery queue.

The first units do not wait for D1. `internal/reports` defines `Mailer` with
the same method. Unit R3 implements it over the existing
`alerts.EmailSender`, and unit R14 moves it to `notify.Mailer`. One test
suite runs against both adapters.

**Destinations.** Recipients that point at a destination of RFC 0006 are
deferred from the first release, as unit R12. Slack, webhook and PagerDuty
destinations finish in the delivery queue after the run ends, so they need a
pending delivery state and a way to reconcile it, which this RFC does not
design. When email destinations are added, each one expands at send time
into its users and addresses, and every one of them goes through
"Resolution" under "Recipients". No destination receives the owner's view as
a whole. The link to a destination uses `ON DELETE RESTRICT`, as RFC 0006
says.

**Requires from other RFCs:**

| RFC | Requirement |
|---|---|
| 0003 | The range grammar, the macro contract, the truncation labels and the stat rules, as written. When the blob gains dashboard variables, a report passes `variables` to dashboard panels as it does for a saved query |
| 0005 | A LogchefQL aggregation returns columns and rows through `core.RunLogchefQL`. The report renders it as any result panel |
| 0007 | A saved view with a stable id and a server-side way to run it. A third `source_kind` then adds it. This RFC designs neither |
| 0009 | Units P2 and P3: the action registry, field views, handler staging, and the recorder for calls outside HTTP |
| 0011 | Capability flags for histogram and native query |

**Conflicts.** None open. One difference from RFC 0003 is deliberate. A
report takes the timezone from the report, where a dashboard takes it from
the viewer.

## Audit events

The audit log records changes to the configuration of a report. Runs and
sends stay in the run history of this RFC and produce no audit event.

| Event | Actor | Target | Detail |
|---|---|---|---|
| `report.create` | User | Report | Source kind and id, schedule, timezone, recipient counts by kind |
| `report.update` | User | Report | Names of the changed fields |
| `report.delete` | User | Report | None |
| `report.pause` | User, or system | Report | Reason |
| `report.resume` | User | Report | None |
| `report.claim` | User | Report | Previous owner id, if any |
| `report.recipient_add` | User | Report | Kind and target. An external address is written in full |
| `report.recipient_remove` | User | Report | Kind and target |

- Every HTTP route integrates through the contract of RFC 0009: a route
  name, a registered action, a field view and handler staging. The field view
  of a report holds no query text and no variable values. It records the
  source kind and id, and the names of the variables.
- A pause by the system happens inside the job, outside HTTP. It calls the
  recorder directly, as RFC 0009 allows for jobs.
- A report that goes away with its dashboard or saved query produces no
  event of its own. The delete event of the dashboard or saved query carries
  the number of reports removed.
- A change to `reports.external_recipients` or
  `reports.allowed_email_domains` is a settings event. RFC 0009 owns it.

## Alternatives rejected

| Alternative | Reason |
|---|---|
| A headless browser that screenshots the dashboard | It adds a Chromium binary of several hundred megabytes to a product that ships as one Go binary, and a browser session that holds the owner's access. A screenshot cannot lock panels per recipient |
| Chart images drawn in Go | A second chart implementation next to unovis would drift in scales, colors and formats. Many mail clients hide images until the reader allows them |
| A PDF attachment | It needs a layout engine and has the same drift problem. Nobody asked for print |
| A link with no data | It fails the goal. A reader on a phone wants the number, not a login page |
| Run as a system identity | A report could then read any source, and the feature would become a way around team access |
| Store the owner's access at save time | A stored grant keeps working after the owner leaves the team. The check at every run costs a few metadata reads |
| Send the owner's view to every Logchef user, as Metabase does | The dashboard already locks panels per viewer. Email that showed more than the UI would be a second, weaker access model |
| Skip a recipient who cannot reach every source | It is simpler than locking per panel, and it makes a mixed-team dashboard report useless for most of its readers |
| Raw cron expressions | Cron allows a report every minute, which is an alert with worse semantics. It also needs a parser and a preview in the web UI and the CLI |
| Reuse export jobs for the attachment | Export jobs run only on ClickHouse and write files to local disk. A report needs at most 5 MiB, which the query result already holds |
| Resume an interrupted run | It needs one stored row per delivery and a rule for a message whose result is unknown. A missed edition costs less than a duplicate |
| A separate reports scheduler | RFC 0006 owns the job runner. A second ticker would repeat the one-replica problem of the alert manager |

## Implementation plan

Each heading names what the unit depends on. R1 and R2 start together. R4,
R5, R6 and R10 run in parallel after R3. The first usable slice is R1 to R4,
a saved-query report that a user sends to themself on demand. R5 adds the
schedule. R12 is deferred from the first release.

**R1. Store, models and migrations.** Depends on nothing.

- Creates `pkg/models/reports.go` and its test, the `add_reports` migration
  pair in both stores, and `internal/store/storetest/reports.go`.
- Edits `queries.sql` of both stores, the store interface and both
  implementations, `pkg/models/auth.go` and `internal/core/api_tokens.go`
  (two scopes). Commits the regenerated sqlc code.
- Change: the types of "Data shape", `ReportSchedule.Validate`, and store
  methods for reports, recipients and runs. `CreateReportRun` returns one
  typed error for a tick conflict and another for a run in flight.
  `TakeReportCooldown` is the conditional update. `FinishReportRun` writes
  the result and prunes to 100 rows.
- Check: `just check` passes. The conformance test runs on both stores. It
  asserts that a second scheduled run for one tick conflicts, that a second
  `running` row for one report conflicts for every pair of triggers, that
  two cooldown updates at one instant update one row, that a row with two
  sources is refused, that a deleted user nulls `owner_id` and removes
  that user's recipient rows, and that a deleted dashboard or saved query
  removes its reports. A table test covers every invalid schedule.

**R2. Render a saved query to HTML and text.** Depends on nothing.

- Creates `internal/reports/` with `render.go`, `table.go`, `message.go`,
  `window.go`, their tests and golden files in `testdata/`.
- Change: `ResolveWindow`, `RenderTable`, the body size steps, and
  `BuildMessage`, which writes the MIME message with an encoded subject.
- Check: golden tests for a table with a time column, a NULL, a 30-digit
  integer and a cell that holds `<script>`. A window test covers `24h` and
  `7d` with both anchors across a daylight-saving change in
  `America/New_York`, and `today-7d`. A size test feeds 2,000 wide rows and
  asserts a body under 100 KiB with a "more rows" line. A test parses the
  message with `net/mail` and `mime/multipart` and asserts two parts and one
  `Subject` header for a name with non-ASCII text.

**R3. API, run identity and manual run.** Depends on R1 and R2.

- Creates `internal/core/reports.go`, `internal/reports/run.go`,
  `internal/reports/mailer.go`, `internal/server/report_handlers.go` and
  tests for each.
- Edits `internal/core/access/access.go` (`AuthReport`, `ReportPrincipal`),
  `internal/server/server.go` (routes), `internal/alerts/email_sender.go`
  (one exported method that sends a prepared message to one address and
  returns the error class),
  `internal/config/config.go` (`[reports]`), and the cleanup loop in
  `internal/server/export_job_handlers.go` (mark runs past `deadline_at`
  plus 2 minutes `interrupted`).
- Change: every route of "API" without `next_run_at`, the save validation,
  the owner rules, the claim, the time budget, the delivery classes and
  retries, the run flow for a saved query with the owner as the only
  recipient, and the run history.
- Check: handler tests assert each row of the error table. A core test with
  the fake datasource (`internal/core/fake_datasource_test.go:1`) asserts
  that a lost team membership fails the run with `no_access`, that an
  inactive owner pauses the report, that a global-admin owner outside the
  team gets `no_access`, and that three failed runs pause the report. A
  Postgres test sends two manual runs at once, and a manual run against a
  scheduled run, and asserts one `running` row each time. A test with a
  fake SMTP server covers each delivery class. It drops the connection after
  the end of `DATA` and asserts one attempt and status `unknown`, and it
  fails `QUIT` after a 250 and asserts `sent`. A test with one slow panel
  and one fast panel asserts that the slow panel fails with `timeout` at the
  query deadline, that the fast panel is sent, and that a final update still
  lands when the handler deadline has passed. A test sets the query budget
  to 20 minutes, claims a run and moves the clock 16 minutes on. It asserts
  that cleanup leaves the run `running` and that a manual run gets a 409.
  It then moves the clock past `deadline_at` plus 2 minutes and asserts that
  cleanup marks the run `interrupted` and that a new run is admitted. A
  config test refuses a query budget of 31 minutes. The mailer tests take
  a `Mailer`, so R14 reuses them. `internal/server/security_hygiene_test.go`
  gains the new routes.

**R4. Web UI: list, editor, history.** Depends on R3.

- Creates `frontend/src/api/reports.ts`, `frontend/src/stores/reports.ts`,
  and `frontend/src/views/reports/` with `ReportsList.vue`,
  `ReportEditorDialog.vue`, `ReportScheduleFields.vue`,
  `ReportHistoryDrawer.vue` and vitest files.
- Edits `frontend/src/router/index.ts`, the main navigation and the row menu
  of saved queries.
- Change: the Reports page and the editor for a saved-query source, with
  **Send test to me**, **Preview** and the history drawer.
- Check: `bun run typecheck` and `bun run test` pass. UI scenario with SMTP
  pointed at a local catcher: create a report on a saved query, click **Send
  test to me**, see the table in the caught message, then see one `sent` run
  in the history drawer.

**R5. Schedule through the job runner.** Depends on R3 and on RFC 0006 units
J1 and J3.

- Creates `internal/reports/schedule.go` and its test. Edits
  `internal/core/reports.go`, the server wiring in `cmd/server` and the
  report response.
- Change: register the `report` job kind with the handler deadline, keep
  the job in step with every report write, pause and resume, set `skip` with
  a grace of one hour, and return `next_run_at`.
- Check: tests through the real runner with a fake clock. A daily 02:30
  report in `Europe/Berlin` fires once on the day that has no 02:30 and once
  on the day that has two. A paused report does not fire. Downtime that
  spans two occurrences and ends 30 minutes after the second runs the second
  only, with `T` equal to it. Downtime that ends 2 hours after it runs
  nothing. A slow panel next to a fast panel ends in a `partial` run. A
  Postgres test starts two runners on one database and asserts one run row
  per tick.

**R6. Dashboard reports: tables and stat tiles.** Depends on R3.

- Creates `internal/reports/dashboard.go`, `stat.go`, `numberformat.go`,
  tests, `pkg/models/testdata/report_number_cases.json` and a vitest file
  that reads the fixture.
- Edits `pkg/models/dashboards_v2.go` (an exported read-only view of the
  panel fields a report needs), `internal/core/reports.go`, the editor dialog
  and `DashboardToolbar.vue`.
- Change: load a dashboard, upgrade a schema 1 blob, select the tab,
  authorize and run each panel, and render `stat` and `table`. Other viz
  types render as `unsupported` until R7.
- Check: a table test covers each reducer with `order_by`, NULL values, no
  rows in both modes, and a 30-digit integer. The shared fixture passes in Go
  and in vitest. A test asserts that a ClickHouse SQL panel with no time
  macro is marked "fixed window". UI scenario: send a test of a dashboard
  with a stat and a table, and compare both values with the dashboard at the
  same range.

**R7. Bar tables for chart, toplist and pie.** Depends on R6.

- Creates `internal/reports/bars.go` and golden files.
- Change: the bar table, the 24-bar rule for count mode with its merge of
  adjacent buckets, the ported `auto` rule for result mode, and the 5-series
  cap.
- Check: golden tests for a count chart with a series, a result chart with
  two y columns, a toplist and a pie. A test asserts that a 7-day range in
  count mode asks for the 12-hour window, and that a 30-day range asks for
  24 hours and shows 15 bars whose values sum to the total of the 30
  buckets.

**R8. User and team recipients with per-recipient locking.** Depends on R3
and R6.

- Creates `internal/reports/recipients.go` and its test. Edits
  `internal/reports/run.go`, `internal/core/reports.go`, the handlers, the
  list page and the editor dialog.
- Change: `user` and `team` rows, resolution at send time, the per-user
  test for dashboard panels and for a saved query, the locked placeholder,
  the 200 cap, the reduced `GET` for a
  recipient, the **Sent to me** tab and `DELETE /reports/:id/recipients/me`.
- Check: a test builds a dashboard with panels on two teams and three
  recipients. It asserts that each message holds exactly the panels that its
  recipient can reach, that a recipient with none gets a `skipped` delivery
  and no message, and that a deactivated team member gets none. A second
  test sends a saved-query report to two users who reach the source through
  different teams, and to one who does not. It asserts two messages.

**R9. External addresses and the admin settings.** Depends on R8.

- Edits `internal/server/admin_settings_handlers.go` (two settings with
  validators), `internal/core/reports.go`, the settings page and the editor
  dialog.
- Change: the `email` kind, both settings, the check at save and at send,
  and the lookup of every address among the users at send time.
- Check: tests assert a 400 with the setting off, a 400 for a domain outside
  the list, a `skipped` delivery after the setting is turned off, and the
  conversion of a known address at save. A test adds an external address,
  then creates a user with that address and no team, and asserts that the
  next run sends that user no panel. A `PUT` of either setting by a non-admin is
  refused.

**R10. CSV attachment.** Depends on R3.

- Creates `internal/util/csv.go` and `internal/reports/csv.go`. Edits
  `internal/server/export_handlers.go` to call the moved function.
- Change: `attach_csv` for a saved query, with the byte cap. Only a
  recipient who passes the saved-query test gets the file.
- Check: the existing export tests pass unchanged. A test parses the
  attachment with `encoding/csv` and compares it with the query result. A
  test above the cap asserts a file that ends on a full row and an email
  line that states the row count.

**R11. CLI, metrics and docs.** Depends on R5.

- Creates `cli/crates/logchef-cli/src/commands/reports.rs`, an API module in
  `cli/crates/logchef-core/src/api/`, and
  `docs/src/content/docs/features/scheduled-reports.md`.
- Edits `internal/metrics/metrics.go`, `internal/reports/run.go` and the
  three docs pages named under "Client surfaces".
- Check: `just check-cli` passes. `logchef reports list` prints the reports
  of the dev user. After one manual run, `/metrics` shows
  `logchef_report_runs_total` with `status="sent"`. The docs site builds.

**R12. Destinations of RFC 0006 as recipients.** Deferred from the first
release. Depends on R9 and on RFC 0006 units D1 and D2.

- Not buildable from this RFC. It needs a design for a pending delivery
  state and its reconciliation with the delivery queue. "Seams" gives the
  rule for email destinations that the design must keep.

**R13. Audit events.** Depends on R9 and on RFC 0009 units P2 and P3.

- Edits `internal/server/report_handlers.go`, `internal/reports/run.go` and
  the action registry and field views of RFC 0009.
- Change: the eight events of "Audit events", through the route contract of
  RFC 0009, and the direct recorder call for a pause by the system.
- Check: the route coverage test of RFC 0009 passes with the report routes.
  A test asserts one event per route with the detail of the table, no event
  for a run, and no variable value in any event.

**R14. Move the mailer to `notify.Mailer`.** Depends on R3 and on RFC 0006
unit D1.

- Edits `internal/reports/mailer.go`. Deletes the exported method that R3
  added to `internal/alerts/email_sender.go`, or to the file that D1 moved
  it to.
- Check: the mailer tests of R3 pass against the new adapter with no change
  to the tests.

## Open questions for Karan

1. Should a report owned by a global admin reach teams that the admin is not
   a member of? Default: no. The report runs with memberships only.
2. Should external recipients be off by default? Default: yes, with an empty
   domain list.
3. Once external recipients are on, may every user add an allowed address,
   or only global admins? Default: every report owner.
4. Should a team recipient be able to opt out alone? Default: no. Only a
   direct recipient removes themself.
5. Is one hour the right lower bound for a schedule? Default: yes.
6. Should deleting a dashboard delete its reports, or refuse while reports
   exist? Default: delete them, and state the count in the dialog.

## Unverified claims

- **Mail clients render a table cell with a percent width as a bar.** Send
  the golden HTML of R7 to Gmail on the web, Apple Mail and Outlook for
  Windows, and compare the bars.
- **Gmail clips an HTML body above about 102 KB.** Send messages of 90 KiB,
  100 KiB and 110 KiB to a Gmail account and note which one shows "Message
  clipped". The 100 KiB bound moves if the limit differs.
- **Both partial unique indexes behave the same in SQLite and Postgres.** The
  R1 conformance test inserts two scheduled runs for one tick, and two
  `running` rows for one report, on each store.
- **A VictoriaLogs result panel returns numbers that the exact parser
  accepts.** Run `* | stats count() as n` against the dev VictoriaLogs and
  pass the cell to `dashboardExact`.
- **The SMTP path and common relays accept a message of about 7 MiB.** A
  5 MiB file is about 6.9 MiB after base64. Send the largest R10 attachment
  through the dev SMTP catcher and through one real relay, and compare the
  encoded size with the limit of the relay.
- **The cron parser of RFC 0006 fires once for a local time that a
  daylight-saving change removes, and once for one that it repeats.** RFC
  0006 leaves this unverified. The R5 test in `Europe/Berlin` settles it. If
  the parser skips or doubles a tick, this RFC states that behavior before
  R5 merges.
- **A relay can deliver a message whose reply to the end of `DATA` never
  reached the client.** The R3 test with the fake server fixes what the
  client does. How often `unknown` means "arrived" depends on the relay.
