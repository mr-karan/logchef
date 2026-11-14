---
title: Dashboards
description: Build multi-panel Logchef dashboards that group log-query visualizations into a shared grid with one time range and auto-refresh.
---

A dashboard is a grid of panels, each one a query rendered as a visualization, sharing one
time range and refreshing together. Use them for the views you keep coming back to:
an error overview, a service's request rate, the count of 5xx responses right now.
There's no configuration or external tooling to set up; dashboards live entirely
inside Logchef.

Open **Dashboards** from the sidebar to see every dashboard, then open one to view
its panels.

## Concept

A dashboard holds a set of **panels** laid out on a 12-column grid. Each panel is a
self-contained visualization: it points at a team and source, carries its own query
(LogchefQL, ClickHouse SQL, or native LogsQL for a VictoriaLogs source), and renders as one of
the panel types below.
Panels do not share a query, only the dashboard's time range and refresh interval.
A panel can also [pin its own range and bucket size](#panel-time).

Panels run through the same team-scoped query endpoints the explorer uses, so a
dashboard adds no new way to reach log data: whatever a viewer can query in the
explorer, they can see in a panel.

## Panel types

- **Chart**: a line, area, or bar chart over a time or category column.
  Result charts store the X column, ordered Y columns, and an optional series
  column. Bars can be stacked or grouped and can display values. Count charts
  use the histogram endpoint and can group by a field.
- **Stat**: a match count or a computed value from a result column. Run SQL or
  a LogsQL stats query, select the value column, and reduce it with `last`,
  `first`, `sum`, `avg`, `min`, or `max`.
- **Breakdown**: a toplist or pie chart. Result panels store a label column
  and a numeric value column. Count panels group matching logs by a field.
  Toplists keep query order. Pies fold small slices into **Other** only when
  you set **Fold slices below %**. The remainder retains the sum of those
  slices, even when it is very small.
- **Table**: the matching rows, like the explorer results grid. Set a row limit and
  an optional column subset. A `LIMIT` in the query itself also applies, and the
  smaller of the two wins. A panel whose rows were cut shows a chip: **First N
  rows**, **Cut by size** or **May be incomplete**.

Charts, breakdowns, and stats support both **count** and **result** mode.
Count mode uses the histogram endpoint. Tables use result mode. A result
query runs as written, including SQL aggregates and LogsQL stats pipes. The default result limit is 1,000 rows, with a maximum of 10,000.
VictoriaLogs' histogram API counts the filter portion and ignores pipes.

Grouped histograms return the top 10 series and combine any remaining series
into an **other** bucket. Prefer fields such as service, environment, host, or
severity over high-cardinality request and trace IDs.

### Multiple data sources

Each panel picks its own team and source, so a single dashboard can mix panels from
different backends: a ClickHouse time series next to a VictoriaLogs stat. LogchefQL
compiles to whatever the panel's source speaks, so the same filter works across
backends. The panel builder offers **ClickHouse SQL** for ClickHouse sources
and **LogsQL** for VictoriaLogs sources. Selecting ClickHouse SQL starts a
result query. Existing SQL count panels continue to work.

If a viewer lacks access to a panel's source, that panel renders a locked state
while the rest of the dashboard loads normally, the same pattern used for shared
collection items.

## Chart axes and colors

**X scale** defaults to **Auto**, which uses a time axis for `time` and `date`
columns. A time axis draws oldest to newest regardless of query order.
For example, `ORDER BY date DESC` still draws dates in ascending order with
`x_scale: "auto"` or `x_scale: "time"`. Choose **Category, query order**
(`x_scale: "category"`) to keep descending dates or any other query order.
Time tick labels adapt to the interval and range. Tooltips retain full timestamps.

Series and pie slices use categorical theme colors in light and dark mode.
These colors are separate from the named palette model for threshold rules.

## Computed stats and exact values

For a total in crores, run a result query such as
`SELECT sum(amount) AS total FROM payments`. Choose **Stat**, select `total`
as the value column, then set Scale to `1e-7`, Decimals to `2`, and Suffix to
` Cr`. A total of 124,000,000 displays as **12.40 Cr**.

Set **Order by** to the date or timestamp column for `first` and `last`.
Rows sort chronologically, with result order breaking ties. The tile shows
the selected row's date. A selected NULL remains NULL, and ties or invalid
order cells produce a notice. Other reducers ignore NULLs. An empty result
shows **No data**; an empty count is zero.

Number formats support `auto`, `standard`, and `compact` notation, thousands
separators, 0 to 10 decimals, scale, units, prefix, and suffix. Auto keeps the
full number while it fits. The tooltip keeps the exact value. Decimal rounding
uses half away from zero. Wide integers and decimal strings keep their digits
through table cells, tooltips, sorting, and stat arithmetic. Averages retain
10 fraction digits before display formatting.

ClickHouse column types determine each column's kind. VictoriaLogs provides
no declared types, so its result columns remain text until you confirm their
kinds under **Column kinds**. Guessed kinds appear only in the builder. A bad
number, date, time, or boolean cell becomes NULL with a notice; other cells
keep their values. Date-only columns keep their calendar date in every
viewer timezone. Timestamp columns use your timezone preference.

Changing visualization, roles, kinds, reducer, or format reuses the fetched
frame. It does not rerun the query. Dashboards that use these settings store
`result_frames` or `formats` features. An older client cannot save over them
and asks you to reload before editing.

### Thresholds and table formatting

Stat panels can color the value or background using up to ten ordered threshold
steps. The first step is the base color. Charts draw labeled reference lines
for each numeric step. Thresholds compare exact decimal values, including
integers above 2^53. The six palette colors adapt to the viewer’s theme.

Tables support up to twenty rules on selected columns. Rules can highlight
a cell or its entire row using numeric comparisons, text containment, or empty
value checks. The first matching rule for each scope wins. Row rules can use
columns hidden from the table. Select visible columns and individual column
formats in the panel builder. Number formats preserve exact values. Time formats
choose ISO or localized dates and minutes, seconds, or milliseconds. Timestamps
use the viewer’s timezone; calendar dates never shift.

These settings require the `thresholds`, `table_rules`, and `column_formats`
client features. Older clients can read dashboards with these settings but
cannot overwrite them.


## Build a VictoriaLogs overview

After connecting a VictoriaLogs source, a compact service-health dashboard is a
good way to see the four panel types together:

1. Open **Dashboards**, choose **New dashboard**, name it, and open it.
2. Choose **Edit**, then **Add panel**.
3. Add a **Stat** panel for `level="error"` to show the current error count.
4. Add a **Time series** panel for `env="prod"`, grouped by `service`, to show
   which service is driving volume.
5. Add a **Breakdown** panel for `env="prod"`, grouped by `level`, and choose
   horizontal bars or a donut.
6. Add a **Table** panel for `level="error"`, keep its row limit small, and pick
   the columns responders need first.
7. Resize and arrange the panels, then choose **Save**.

All four panels follow the dashboard time picker. You can switch an individual
VictoriaLogs panel to native LogsQL without changing the other panels or the
dashboard's backend mix.

## Time range and refresh

Panels on a dashboard share one **time range**, set from the toolbar (the same
quick and absolute ranges as the explorer). Changing it re-runs panels on the open tab. New dashboards default to the last 15 minutes. In edit mode, the
dashboard settings can set the range a dashboard opens with: a quick range, or
a calendar range such as **Today** or **Last 7 calendar days**.

Set an **auto-refresh** interval (off, 30 seconds, 1 minute, or 5 minutes) to keep
a wall-board style view current. There is also a manual refresh button.

Panels render correctly for viewers in any timezone: histogram buckets (time series
panels) align to your local timezone, while table and stat panels use a UTC-anchored
query internally so their time window doesn't shift for non-UTC viewers.

## Tabs

A dashboard can split its panels over up to 10 **tabs**, for example "Current day"
and "Last 7 days". Each tab has its own grid, and a panel sits on exactly one tab.
A dashboard with a single tab shows no tab bar.

The open tab is part of the URL as `tab=<id>`, so a reload or a shared link opens
on the same tab, and the browser's Back button returns to the previous one. A link
that names a tab the dashboard no longer has opens the first tab.

Only the tab on screen runs its queries. A tab runs when you first open it, and a
refresh, a time range change or an auto-refresh tick runs the open tab alone. A tab
you return to runs again only if a refresh happened while it was hidden. At most
four panel requests are in flight at a time across tab switches.

The tab bar works from the keyboard: Left and Right move between tabs, Home and End
jump to the first and last, and Enter or Space opens the focused tab.

## Panel time

A panel follows the dashboard range unless its query or its settings say
otherwise. Chips in the panel header show what applies.

| Chip | Meaning |
| :--- | :--- |
| **Fixed window** | The query sets its own window, so the time picker does not change it |
| A range, such as **Last 7 calendar days** | The panel has its own range. Hover for the range and timezone it ran over |
| A bucket size, such as **24h** | The panel has a fixed bucket size. An amber chip shows the size that ran when the chosen one was raised |

### Which panels follow the time picker

| Query | Follows the time picker |
| :--- | :--- |
| LogchefQL | Always |
| LogsQL | Always |
| ClickHouse SQL | Only when the text uses `{{__time_filter}}`, or both `{{__from}}` and `{{__to}}` |

A ClickHouse SQL panel without those [time macros](/guide/time-macros) keeps the
window its SQL sets, for example `WHERE date(ts) >= today() - 10`, and shows
**Fixed window**. To make it follow the time picker, filter with the macro:

```sql
SELECT count() AS n FROM default.http WHERE {{__time_filter}}
```

### A range for one panel

In the panel builder, **Time range** is **Dashboard range** by default. Pick a
range to make the panel query that range in place of the dashboard's. A "last
7 days" chart can then sit on a dashboard that opens on today.

A range is rolling or calendar.

| Kind | Example | Window |
| :--- | :--- | :--- |
| Rolling | `7d` | Exactly 168 hours that end now. At noon the window starts at noon seven days ago, so the first day is half missing |
| Calendar | `today`, `yesterday`, `today-6d` | Whole days in the viewer's timezone. `today-6d` runs from the start of the day six days before today until now. It covers today and the 6 dates before it |

**Custom range** accepts `<n>m`, `<n>h`, `<n>d`, `<n>w`, `today`, `yesterday`
and `today-<n>d`. Setting the range back to **Dashboard range** removes it from
the panel.

### Bucket size

**Bucket size** is **Auto** by default, which gives about 100 buckets over the
range. A fixed size is one of `1s`, `5s`, `10s`, `15s`, `30s`, `1m`, `5m`,
`10m`, `15m`, `30m`, `1h`, `2h`, `3h`, `6h`, `12h` and `24h`.

- In count mode the bucket size is the histogram window. A count chart with
  `24h` buckets on a 7-day range draws one bar per day.
- In result mode it feeds `{{__interval}}` in the query.
- A fixed size that would give more than 1,000 buckets over the range is raised
  to the next size that fits, and the chip shows the size that ran.

Day buckets need a calendar range. A rolling range starts in the middle of a
day, so its first bucket is partial. The builder says so when a panel combines
`24h` with a rolling range.

### Day buckets on VictoriaLogs

A VictoriaLogs count panel buckets with one fixed UTC offset, read at the start
of the range. When the range crosses a daylight-saving change in the viewer's
timezone and the bucket is longer than one hour, the panel shows a notice that
names the change: buckets after it start one hour off local midnight.
ClickHouse buckets follow the change, so ClickHouse panels show no notice.

## Result caching

Because every panel re-runs on each refresh, a busy dashboard can hammer the
backend: *N* panels open by *M* viewers is up to *N×M* queries per refresh cycle.
Each dashboard has a **cache TTL** in its settings (default 10 minutes) that turns
on a shared, server-side result cache. The first viewer's query fills the cache and
everyone else within that window is served the stored result, so a panel hits the
backend at most once per TTL. Set the TTL to `0` to disable caching for that
dashboard and always query live.

The cache covers dashboard panels only — explorer and other ad-hoc queries are never
cached. Server-wide limits (total size, per-entry cap, entry count) are set with the
[`[dashboard_cache]` config](/getting-started/configuration/#dashboard-result-cache).

## Chart styles

Time series panels can render as **line** (default), **area**, or **bars**. Set the
style per panel in the panel builder. When the style is bars, you can choose
**stacked** (default) or **grouped** bar mode. The histogram data is shared with the
explorer, including zero-filled gaps, so a sparse or grouped series (e.g. 5xx errors
by host) renders as a continuous chart instead of isolated bars floating over dead
space.

## Editing

Open a dashboard and choose **Edit** to enter a direct-manipulation canvas:

- **Move a panel**: drag it by its header to a new grid position.
- **Resize a panel**: drag the handle on its bottom-right corner. A panel can be
  any width from 2 to 12 columns and from 1 to 12 rows tall.
- **Add a panel**: click an empty grid slot, or the "+ Add panel" tile. This opens
  the **panel builder**, a full-height drawer where you pick the team and source,
  write the query in the Monaco editor (LogchefQL, or LogsQL for VictoriaLogs)
  with a live preview, choose the panel type, and set type-specific options: group-by
  and chart style for time series, a required group-by and horizontal-bar or donut view for breakdowns, and a row limit and optional column subset for tables.
- **Edit or remove** an existing panel via the pencil / trash icons on its header,
  which reopens the same panel builder drawer.
- **Add a tab** with **Add tab** next to the tab bar. The menu beside the tab bar
  renames the open tab, moves it left or right, or deletes it. Deleting a tab
  deletes its panels and asks first.
- **Move a panel to another tab** with the arrows icon on its header. The panel
  keeps its size and lands after the panels that tab already has.

Each tab packs its panels from the top left, and editing one tab never rearranges
another.

Changes stay local until you **Save**; **Cancel** discards them. Leaving with unsaved
edits prompts first.

If someone else saves the dashboard while you edit it, **Save** keeps your changes
and asks what to do: **Save as copy** creates a new dashboard from your version,
**Overwrite with mine** shows when the newer version was saved and replaces it after
you confirm, and **Discard and load latest** drops your changes and shows theirs.

A dashboard can contain up to 24 panels on up to 10 tabs. Dashboard names are
limited to 200 characters, descriptions to 2,000 and tab names to 60.

## Who can edit

- **Anyone signed in** can list and view dashboards.
- **The creator** and **global admins** can edit and delete a dashboard.

Per-dashboard sharing roles are not part of this release. As always, viewing a panel
never grants access to its underlying source; source access is enforced when the
panel's query runs.

## API clients

Scripts that use the dashboards API send two headers on every dashboard
request.

- `X-Logchef-Dashboard-Schema` names the newest panel schema the script can
  read: `1` or `2`. Without it the server treats the script as a schema 1
  client. The frontend saves dashboards as schema 2, and a schema 1 client
  reading one gets an empty blob with `panels_corrupt: true` and
  `min_schema: 2` instead of panels it cannot read.
- `X-Logchef-Dashboard-Features` lists, separated by commas, the features the
  script's code supports. A save to a dashboard that uses a feature the script
  did not declare is refused with 409, so a script can never drop state it
  does not know. This version stores `result_frames`, `formats`,
  `result_charts`, `stat_compare`, `thresholds`, `table_rules`,
  `column_formats`, `tabs` (more than one tab) and `free_grid` (a width outside
  2, 3, 4, 6, 8, 9 and 12, or a height above 6), and `panel_time` (a fixed panel interval, a panel range,
  or a calendar default range). An empty value is correct for
  a dashboard that uses none of them. Declare what your code supports; never
  copy a dashboard's stored `features` list into the header.

`GET /dashboards` returns one summary per dashboard (panel count, panel
kinds, tab count, revision, edit rights and a corrupt flag) and no panel
blobs. Read a dashboard's panels with `GET /dashboards/:id`. Every update
must carry the `revision` from that read, and the panel blob may be up to
512 KiB.

A dashboard whose stored panels fail validation shows a notice instead of
panels. Its creator or an admin gets the stored blob back as
`raw_panels_json` from `GET /dashboards/:id` and can repair it with an
update.

## No configuration needed

Dashboards ship with Logchef. You don't need to enable anything in config or run an
extra service: dashboard definitions are saved in Logchef's metadata store, right
alongside your teams, sources, and saved queries.

## Next steps

- Save the queries behind your panels to a [Collection](/features/collections) so teammates can reuse them
- Review [Search Syntax](/guide/search-syntax) to write the LogchefQL filters panels use
- See [Alerting](/features/alerting) to get notified instead of watching a dashboard
