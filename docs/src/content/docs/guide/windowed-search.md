---
title: VictoriaLogs windowed search
description: Settings, progress, pagination, and limits for windowed searches on large VictoriaLogs sources.
---

Windowed search is an opt-in Explorer mode for VictoriaLogs sources. The source
form has an **Enable windowed search** option. ClickHouse sources and VictoriaLogs
sources with this option disabled keep the standard query path.

## Search behavior

Logchef divides the selected time range into windows and searches newest first.
Partial results arrive as each window completes. The page stops at the selected
row limit, and **Load older** continues the same query and fixed time range.
Parallel windows still appear in newest-first order.

Filter-only LogchefQL and native LogsQL queries use this mode. Queries with
pipes use the standard path so their ordering and aggregations retain their meaning.
Live Tail uses its existing streaming path.

The stream picker sends exact stream filters separately from the query text.
**Select all shown** selects the displayed values. **Clear** removes the picker
restriction. The source's immutable scope and tenant apply to every request.

## Settings

The source form and provisioning use the same settings:

| Setting | Default | Allowed values | Effect |
|---|---|---|---|
| `enabled` | `false` | `true`, `false` | Enables windowed search. |
| `max_window_seconds` | `10800` | `60` to `86400` | Maximum duration of one window, in seconds. |
| `concurrency` | `2` | `1` to `8` | Shared per-source limit for window search, count, and picker requests. |
| `schema_lookback_seconds` | `300` | `60` to `86400` | Recent interval used to discover field names, in seconds. |
| `sidebar_lookback_seconds` | `900` | `60` to `10800` | Recent interval used to discover stream fields and sidebar values, in seconds. |
| `sidebar_values_cap` | `10000` | `100` to `100000` | Bounds sidebar value candidates. The reported distinct count remains exact. |
| `histogram_enabled` | `true` | `true`, `false` | Controls chart visibility. Disabling it retains background counts and totals. |
| `stream_fields` | `[]` | Nonempty field names | Configures picker fields. An empty list enables automatic discovery. |

Missing settings use their defaults. Zero-valued numeric settings also use the
defaults. Sources without the optimizer keep the standard 24-hour schema lookback.
Fields absent from the recent schema interval can be missing from the field list.

This block belongs under a VictoriaLogs source's connection in
[provisioning](/getting-started/provisioning):

```toml
[sources.connection.optimizer]
enabled = true
max_window_seconds = 10800
concurrency = 2
schema_lookback_seconds = 300
sidebar_lookback_seconds = 900
sidebar_values_cap = 10000
histogram_enabled = true
stream_fields = ["service"]
```

## Search and count coverage

**Stop** cancels the active search and counts. **Skip to next window** leaves
a visible gap and continues to older windows. Failed and skipped windows have
individual retry controls. A row larger than the response byte limit fails its
window with a size-limit message.

When the first page contains every matching row, Logchef builds the histogram
from those rows. Otherwise, background counts cover the entire selected range.
The count coverage strip distinguishes counted, pending, and failed windows.
Retrying a failed count replaces that window's contribution.
**Retry failed counts** retries the failed windows in sequence and keeps completed
counts. **Stop** cancels the current retry and leaves the remaining windows
available for retry. Expand **Show failed count windows** to retry one window.

Results can be available while counts are still running. A partial total covers
only the counted windows. Disabling the chart does not disable those counts.
Histogram buckets use the selected timezone offset at the range start, matching
the standard histogram. A range across a daylight-saving transition uses that
same offset throughout.

## Pagination and limits

Each page first finds its timestamp boundary with a time-only query. Logchef
then sorts complete rows within that shorter range. Cursors record the last
timestamp and the number of rows already returned at that timestamp.
For unchanged data, timestamp ties and identical duplicate rows remain distinct
and appear exactly once.

- A search can contain at most 4,096 windows. Shorten the selected range or
  increase `max_window_seconds` if it exceeds this limit.
- Very large groups with one timestamp remain expensive to sort.
- Some VictoriaLogs versions can return a boundary that is too recent.
  A short, untruncated page then triggers one full-window reread, which can be expensive.
- VictoriaLogs has no snapshot isolation across requests. Late ingestion and
  retention changes can affect later pages.
- Source, filter, and time-range changes require a new search. Cursor formats
  can also change on upgrade, so existing searches must be rerun.

For a large source, smaller windows bound each request's time range. Lower
concurrency reduces simultaneous work. Full-range background counts can still
take substantially longer than the first page.
