---
title: Scheduled Reports
head:
  - tag: title
    content: "Scheduled Log Reports by Email | Logchef"
description: Email a dashboard, one dashboard tab or a saved query to people, teams and allowed external addresses on an hourly, daily, weekly or monthly schedule.
---

A scheduled report sends a dashboard, one tab of a dashboard, or a saved query
by email on a schedule, for example a 5xx summary every weekday at 09:00. The
email carries the numbers and the first rows, so a reader needs no login, and
it links to the live view.

A report runs with the access of its owner, checked again at every run. Each
Logchef user who receives it sees only the panels that Logchef would show that
user in the UI; the others are locked in their copy. An address outside
Logchef receives a report only when a global admin allows it, and then sees
what the owner sees.

## Create a report

Open **Reports** in the sidebar and click **New report**. You can also start
from a source:

- **Schedule report** in a dashboard's toolbar opens the editor on that
  dashboard.
- **Schedule report** in a saved query's row menu, in a collection, opens the
  editor on that query.

The editor has these parts.

| Part | Fields |
|------|--------|
| Source | A saved query, or a dashboard with one tab or **All tabs**. For a saved query, values for its variables |
| Schedule | Frequency, time and timezone. See [Schedule](#schedule) |
| Time range | The range each email covers, and where it ends. See [Time range](#time-range) |
| Options | Rows per table (5 to 100, default 25), **Skip when there are no rows**, and for a saved query **Attach the rows as a CSV file** |
| Recipients | People, teams and, when a global admin allows them, email addresses. See [Recipients](#recipients) |

After the first save the dialog stays open on the report:

- **Preview** shows the email you would receive, without sending it.
- **Send test to me** sends one email to you now.
- **History** opens the run history.

The editor warns when no email can be sent (SMTP is not configured), when the
email can carry no links (no frontend URL is configured), and when the owner
can no longer see a panel's source.

A report name is 1 to 200 characters with no line break. A report holds at
most 50 recipient rows.

## Schedule

A schedule is one of four frequencies, in the report's timezone. Nothing runs
more often than once an hour.

| Frequency | You pick |
|-----------|----------|
| Hourly | The minute past the hour |
| Daily | The time |
| Weekly | The days of the week and the time |
| Monthly | The day of the month (1 to 28) and the time |

The **Next run** column of the list shows when each report sends next. It is
empty for a paused report and for a report with no owner.

On a daylight-saving change the schedule follows the wall clock. A daily 02:30
report in `Europe/Berlin` sends at 03:00 on the day the clocks skip 02:30, and
once, at the first 02:30, on the day they repeat it.

**Missed sends.** After downtime, a report sends its latest missed edition
only if that edition is less than one hour late. Older editions are skipped.
A daily 09:00 report whose server was down from Monday morning until Tuesday
09:30 sends Tuesday's edition and never Monday's. Back at Tuesday 15:00, it
sends nothing until Wednesday.

## Time range

The report's timezone also sets the time range, so every recipient gets the
same numbers. The email header names the zone.

The range uses the grammar of dashboards: `15m`, `6h`, `24h`, `7d`, `2w`,
`today`, `yesterday` and `today-<n>d`. A saved query needs a range. A
dashboard report can use **Dashboard default**, which is the dashboard's
default range, or `24h` when the dashboard has none.

**Range ends at** picks the "now" of the range:

| Range ends at | A `24h` range at a 09:00 send |
|---------------|-------------------------------|
| Send time | Yesterday 09:00 to today 09:00 |
| Start of that day | Yesterday 00:00 to today 00:00 |

- The send time is the scheduled time, not the moment the run started, so a
  late start covers the same window.
- With **Start of that day**, a range in whole days, or in hours that make
  whole days such as `24h` or `48h`, keeps both ends on midnight across a
  daylight-saving change. Other hour ranges subtract exact hours.
- `today`, `yesterday` and `today-<n>d` are calendar ranges. They take
  **Send time** only.
- A dashboard panel with its own range keeps it. A panel's own calendar range
  resolves at the send time.
- A saved query's `today` or `yesterday` day default resolves at the send
  time in the report's timezone.

## What the email shows

The email is HTML with a plain-text copy. It loads no remote image, script or
font, and every panel is a table, so it reads the same in any mail client.
Panels follow the dashboard's reading order (row, then column, tab by tab) in
one column.

| Panel | In the email |
|-------|--------------|
| Saved query | A table of the first rows |
| Table | A table of the first rows, with the panel's visible columns |
| Stat | A tile with the reduced value, its unit and the row it came from, or "No data" |
| Chart | A bar table: one row per x value, the value and a bar sized to the largest value. One block per series, at most 5 series, then "N more series" with a link |
| Toplist | The same bar table, sorted as the panel sorts it |
| Pie | The same bar table, with each slice's percent of the total |
| Other (LogQL panels, unknown kinds) | The panel title, a note that the panel is not in the email, and a link |

- A table shows the report's rows per table, then "N more rows" with a link.
- A count chart shows at most 24 bars. The report picks the smallest bucket,
  no finer than the panel's own interval, that gives at most 24. A range over
  24 days uses one-day buckets and adds neighbours together, so a 30-day range
  shows 15 bars of two days whose counts add up exactly. The chart says how
  much time one bar covers.
- A truncated result keeps its label: "First N rows", "Cut by size" or "May
  be incomplete".
- A ClickHouse SQL panel without a time macro says it has a fixed window.
- An instant prints in the report's timezone. Numbers use the panel's format.
- The email stays under 100 KiB. Logchef first shows fewer rows per table, down
  to 5, then replaces panels from the end with their title and a link.

The footer names the owner, the sources of the panels you can see, the window
and the timezone. It links to the live dashboard tab or saved query, and to
the report's settings at `/reports/<id>`. The dashboard link opens the tab, not
the report's window. Links need a frontend URL; every link needs a login.

### CSV attachment

A saved-query report can attach its rows as a CSV file named
`<report name>-<date>.csv`. Cells follow the rules of the log export. The file
is at most `reports.max_attachment_bytes` before encoding (default 5 MiB). A
larger result ends at the last whole row that fits, and the email says "The
attached CSV holds the first N of M rows". If the header row alone is over the
limit, the email has no file and says so. A Logchef user gets the file only
when they can see the saved query's source. An
[external address](#external-addresses) gets it whenever the owner can see the
source.

## Recipients

A report goes to people, teams and, when allowed,
[external addresses](#external-addresses). With no recipients it goes to its
owner. A team receives as its members at send time, and a person named twice
gets one email.

Who you may add:

| You are | You may add |
|---------|-------------|
| A global admin | Any user or team |
| Anyone else | Yourself, people who share a team with you, and your own teams |

A row already on the report stays when you save it, even one you could not
add yourself.

**What each recipient sees.** The queries run once, as the owner. Each
recipient then gets their own email, in which every panel they cannot see is
replaced by "You do not have access to this panel.":

- A dashboard panel needs a membership in the panel's team, and that team
  must have the panel's source.
- A saved query needs any team of the recipient's that has its source.

There is no exception for the owner or for admins. A global admin who sends a
test to themselves sees locked panels for teams they are not a member of.

A recipient who can see no panel gets no email. An inactive user, a service
account, or a user with no email address gets no email either. Each run sends
to at most 200 people: users first, by user id, then external addresses; the
rest are recorded as skipped with `recipient_limit`.

**Stop receiving.** A report sent to you shows on the **Sent to me** tab, with
its name, owner, source and schedule. **Stop receiving** takes you off a report
that names you directly, by your user or by your email address. If you get it
through a team, ask the owner.

### External addresses

An address that belongs to no Logchef user takes report data out of Logchef,
and Logchef cannot check what its reader may see. Two settings gate it. Both
are under **Administration → System Settings → Reports**, and only a global
admin changes them, from a browser session; an API token cannot. Each change
is in the [audit log](/operations/audit-log/).

| Setting | Default | Meaning |
|---------|---------|---------|
| `reports.external_recipients` | `false` | When false, no external address receives a report |
| `reports.allowed_email_domains` | empty | Domains separated by commas, such as `example.com, partner.example`. An address's domain must match one exactly; a subdomain does not match. `*` allows every domain. Empty allows none |

With external recipients on and no domain listed, no address passes; the
settings page warns about it. See also
[configuration](/getting-started/configuration/#scheduled-report-recipients).

The editor shows an address field only while external recipients are on. An
address is a plain ASCII address such as `name@example.com`, with no display
name, at most 254 bytes. Its domain is stored in lower case.

**An address that belongs to a user is that user.** Logchef looks the address
up among the users, ignoring letter case, both when the report is saved and at
every send:

- At save, such an address is stored as that user's row. You may add it when
  you may add that user, or when the settings allow the address.
- At send, an address row whose address now belongs to a user, for example a
  user created after the row was added, is that user. The user's own access
  decides what they see, so a new user with no team gets no panel and no
  email.

**What an external address receives.** Every panel the owner can reach,
which is the owner's own view, and the CSV attachment when the report has one.
A panel locked for the owner stays locked, with no data. The footer names the
sources of the panels in that copy.

**Checked at every send.** Logchef checks both settings when a report is
saved, and refuses an address they do not allow with a 400. It checks them
again at every send, so turning the setting off or removing a domain stops
delivery on the next run. The run history then records the address as
`skipped` with `external_disabled` (the setting is off) or
`domain_not_allowed` (the domain is not listed). An address already on a
report stays on it when the settings change, so turning them back on resumes
delivery.

## Owner and access

A report runs as its owner: the user who created it, or the user who claimed
it. A global admin who edits a report does not become its owner.

- Every panel is checked against the owner's team memberships at every run. A
  report owned by a global admin reaches only the teams that admin is a member
  of.
- A panel whose source the owner can no longer reach is locked for everyone,
  and the run is partial. When every panel is locked, the run fails with
  `no_access` and sends nothing.
- When the owner is deactivated, the report pauses with **Paused: owner
  inactive**.
- When the owner is deleted, the report stays with **No owner** and does not
  run.
- Deleting a dashboard or saved query deletes its reports.

**Claim** in the row menu takes over a report with no owner or with an
inactive owner. It makes you the owner, so the report runs with your access,
and it ends an owner-inactive pause. A global admin can claim any such report.
Anyone else must be able to see its source.

| Action | Who |
|--------|-----|
| Create | Anyone who can see the source |
| Edit, pause, resume, delete, preview, send, history | The owner or a global admin |
| Add an external address | Whoever creates or edits the report, while the settings allow the address |
| Claim | A global admin, or anyone who can see the source, when the owner is gone or inactive |
| See it on **Sent to me** | Its recipients |
| See every report (**All** tab) | Global admins |

## Run history

**History** lists a report's runs, newest first, with the trigger (scheduled
or manual), the window, each panel's rows or error, and each delivery. A
report keeps its last 100 runs. Only the owner and global admins read the
history, because a panel error can hold query text.

| Status | Meaning |
|--------|---------|
| Sent | Every recipient the run could send to got the email. A recipient skipped on purpose, for example an inactive user or one who can see no panel, does not change this |
| Partly sent | At least one email went out, but a panel failed or was locked, a delivery failed, or the run ran out of time before some recipients. Also used when no send is confirmed but one may have reached the mail server (`delivery_unknown`) |
| Skipped | Nothing ran or nothing was sent on purpose: the report was paused (`paused`), had no rows with **Skip when there are no rows** (`empty`), had no owner (`no_owner`) or an inactive one (`owner_inactive`), or another run was in flight (`busy`) |
| Failed | No email went out, for example `no_access`, `all_panels_failed`, `smtp_not_configured`, `all_deliveries_failed`, `source_invalid` or `timeout` |
| Interrupted | The server stopped during the run |

A delivery is `sent`, `failed`, `skipped` or `unknown`. A failed send is
retried twice, after 2 and 8 seconds, when the mail server answered with a
temporary error. `unknown` means the connection broke after the message was
handed over, so the mail server may have accepted it. Logchef never retries
it, so a recipient gets an edition at most once.

Three failed runs in a row pause the report with **Paused: repeated
failures**, and the owner gets one email that says why. **Resume** starts it
again.

One run of a report is in flight at a time. A manual run while another runs
answers 409. A scheduled tick that finds a run in flight is skipped with
`busy`. A report allows one manual run and one preview per minute.

## Limits

| Bound | Value |
|-------|-------|
| Schedule | At most once an hour |
| Recipient rows per report | 50, counting user, team and address rows |
| People per run | 200, users first, then external addresses |
| Rows read per panel | The panel's limit, at most 10,000 |
| Bytes per panel result | 4 MiB, or `query.max_response_bytes` when smaller |
| Rows shown per table | 5 to 100, default 25 |
| Series per chart | 5 |
| Bars per count chart | 24 |
| Email body | 100 KiB |
| CSV attachment | `reports.max_attachment_bytes`, default 5 MiB |
| Query timeout | 60 seconds per panel |
| Query budget per run | `reports.query_budget`, default 5 minutes, 1 to 30 minutes |
| Delivery time after the queries | 5 minutes |
| Panel queries at once, per run | 4 |
| Runs at once, per replica | `reports.max_concurrent_runs`, default 2 |
| Manual runs and previews | One of each per report per minute |
| Run history | 100 runs per report |

A panel still running when the query budget ends fails, and the run goes on
with the others. Recipients not reached when the delivery time ends are
skipped with `deadline`.

## Configuration

Reports send through the SMTP settings in **Administration → System Settings
→ Alerts**: host, port and from address are required. Reports send even when
alerting is disabled. `server.frontend_url`, else `server.browser_url` or
`server.public_url`, sets the links in the email. With none of them set, the
email carries no links.

```toml
[reports]
enabled = true               # false: the report routes answer 503 and nothing sends
query_budget = "5m"          # 1m to 30m; outside that range the server refuses to start
max_concurrent_runs = 2      # runs this replica executes at once
max_attachment_bytes = 5242880
```

Scheduled sends run on the background job runner, one job per report, and
need `[jobs] enabled = true` on at least one replica. With several replicas on
Postgres, one replica sends each edition; see
[database backends](/operations/database-backends/#background-jobs-run-on-any-replica).
The scheduler does not run when `reports.enabled` is false or
`demo.read_only` is set. When reports are enabled again, the one-hour grace of
[missed sends](#schedule) applies to each report's latest edition.

[Metrics](/operations/metrics/#report-metrics) count runs, panels and
deliveries.

## CLI

The [CLI](/integration/cli/#reports) lists reports, reads their run history
and sends one now. It does not create or edit a report.

```bash
logchef reports list
logchef reports runs 4
logchef reports run 4 --all
```

## API

All routes sit under `/api/v1/reports` and need the `reports:read` or
`reports:write` [token scope](/features/service-tokens/). An OAuth sign-in,
such as `logchef auth` or an MCP client, cannot reach them.

| Method and path | Scope | Result |
|-----------------|-------|--------|
| `GET /reports?scope=mine\|received\|all` | `reports:read` | A list. `all` needs a global admin |
| `POST /reports` | `reports:write` | 201 with the report |
| `GET /reports/:id` | `reports:read` | The report. A recipient who may not edit it gets the reduced view |
| `PUT /reports/:id` | `reports:write` | The report. Replaces every field |
| `DELETE /reports/:id` | `reports:write` | 204 |
| `POST /reports/:id/pause`, `/resume`, `/claim` | `reports:write` | The report |
| `POST /reports/:id/run` | `reports:write` | 202 with the run. Body `{"recipients": "me"}` (default) or `"all"` |
| `POST /reports/:id/preview` | `reports:read` and `logs:read` | The HTML you would receive |
| `GET /reports/:id/runs?limit=&before=` | `reports:read` | Runs, newest first. `limit` 1 to 100, default 20 |
| `GET /reports/recipient-options` | `reports:read` | The users and teams you may add, and `external`: whether external addresses are on and the allowed domains |
| `DELETE /reports/:id/recipients/me` | `reports:write` | 204. Takes you off a report that names you directly, by user or by address |

A create or update body:

```json
{
  "name": "Daily 5xx summary",
  "source": { "kind": "dashboard", "dashboard_id": 12, "tab_id": "main" },
  "schedule": { "frequency": "weekly", "weekdays": [1, 2, 3, 4, 5], "hour": 9, "minute": 0 },
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
    { "kind": "email", "email": "oncall@partner.example" }
  ]
}
```

- `schedule` holds only the fields its frequency uses: `minute` always,
  `hour` except for hourly, `weekdays` (0 to 6, Sunday is 0) for weekly, and
  `day_of_month` (1 to 28) for monthly. Any other field is a 400.
- `source.kind` is `dashboard` (with `dashboard_id` and an optional `tab_id`)
  or `saved_query` (with `saved_query_id`). `variables` and `attach_csv` apply
  to a saved query only.
- `range_anchor` is `send_time` or `day_start`.
- A recipient is `{"kind": "user", "user_id": ...}`, `{"kind": "team",
  "team_id": ...}` or `{"kind": "email", "email": ...}`. An `email` row needs
  the [external-address settings](#external-addresses), unless the address
  belongs to a user you may add; then it is stored as that user's row.

The response adds `id`, `owner`, `state`, `pause_reason`, `next_run_at`,
`last_run`, `can_edit` and `warnings`. Errors: 400 names the field, 403 when
you may not make the change or the owner cannot see the source, 404 when the
report does not exist or you may not see it, 409 when a run is in flight, 429
within a minute of the last manual run or preview, and 503 when reports are
disabled.
