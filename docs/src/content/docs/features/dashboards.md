---
title: Dashboards
description: Group saved visualizations into a shared grid with one time range and auto-refresh.
---

A dashboard is a grid of panels, each one a query rendered as a chart, sharing one
time range and refreshing together. Use them for the views you keep coming back to:
an error overview, a service's request rate, the count of 5xx responses right now.
There's no configuration or external tooling to set up; dashboards live entirely
inside Logchef.

Open **Dashboards** from the sidebar to see every dashboard, then open one to view
its panels.

## Concept

A dashboard holds a set of **panels** laid out on a 12-column grid. Each panel is a
self-contained visualization: it points at a team and source, carries its own query
(LogchefQL or a source-native query), and renders as one of the panel types below.
Panels do not share a query, only the dashboard's time range and refresh interval.

Panels run through the same team-scoped query endpoints the explorer uses, so a
dashboard adds no new way to reach log data: whatever a viewer can query in the
explorer, they can see in a panel.

## Panel types

- **Time series**: a stacked bar chart over time, the same histogram the explorer
  draws. Optionally group by a field to break the series out (for example, 5xx
  responses split by service).
- **Stat**: a single number: the total match count for the query over the current
  time range. Good for "how many errors in the last 15 minutes".
- **Table**: the matching rows, like the explorer results grid. Set a row limit and
  an optional column subset.

### Multiple data sources

Each panel picks its own team and source, so a single dashboard can mix panels from
different backends: a ClickHouse time series next to a VictoriaLogs stat. LogchefQL
compiles to whatever the panel's source speaks, so the same filter works across
backends. Native-query panels (SQL or LogsQL) target the specific source you chose.

If a viewer lacks access to a panel's source, that panel renders a locked state
while the rest of the dashboard loads normally, the same pattern used for shared
collection items.

## Time range and refresh

Every panel on a dashboard shares one **time range**, set from the toolbar (the same
quick and absolute ranges as the explorer). Changing it re-runs all panels. New
dashboards default to the last 15 minutes.

Set an **auto-refresh** interval (off, 30 seconds, 1 minute, or 5 minutes) to keep
a wall-board style view current. There is also a manual refresh button.

Panels render correctly for viewers in any timezone: histogram buckets (time series
panels) align to your local timezone, while table and stat panels use a UTC-anchored
query internally so their time window doesn't shift for non-UTC viewers.

## Chart styles

Time series panels can render as **bars** (default), **line**, or **area**. Set the
style per panel in the panel builder. The histogram data is shared with the
explorer, including zero-filled gaps, so a sparse or grouped series (e.g. 5xx errors
by host) renders as a continuous chart instead of isolated bars floating over dead
space.

## Editing

Open a dashboard and choose **Edit** to enter a direct-manipulation canvas:

- **Move a panel**: drag it by its header to a new grid position.
- **Resize a panel**: drag the handle on its bottom-right corner.
- **Add a panel**: click an empty grid slot, or the "+ Add panel" tile. This opens
  the **panel builder**, a full-height drawer where you pick the team and source,
  write the query in the Monaco editor (LogchefQL or the source's native language)
  with a live preview, choose the panel type, and set type-specific options: group-by
  and chart style for time series, a row limit and optional column subset for tables.
- **Edit or remove** an existing panel via the pencil / trash icons on its header,
  which reopens the same panel builder drawer.

Changes stay local until you **Save**; **Cancel** discards them. Leaving with unsaved
edits prompts first.

## Who can edit

- **Anyone signed in** can list and view dashboards.
- **The creator** and **global admins** can edit and delete a dashboard.

Per-dashboard sharing roles are not part of this release. As always, viewing a panel
never grants access to its underlying source; source access is enforced when the
panel's query runs.

## No configuration needed

Dashboards ship with Logchef. You don't need to enable anything in config or run an
extra service: dashboard definitions are saved in Logchef's metadata store, right
alongside your teams, sources, and saved queries.
