---
title: Time Macros
description: Built-in macros that put the request's time range, bucket size and timezone into ClickHouse SQL and LogsQL queries in Logchef.
---

A time macro is a placeholder that Logchef fills from the request, not from a
value you type. `{{__time_filter}}` becomes a filter on the selected time range,
and `{{__interval}}` becomes the bucket size. One query text then works for any
range: in the Explorer's SQL mode, in a dashboard panel, from the CLI and from
the MCP server.

```sql
SELECT toStartOfInterval(timestamp, {{__interval}}, {{__timezone}}) AS t, count() AS n
FROM default.http
WHERE {{__time_filter}}
GROUP BY t
ORDER BY t
```

With a range of the last 7 days, a 24-hour bucket and the timezone
`Asia/Kolkata`, the server runs:

```sql
SELECT toStartOfInterval(timestamp, INTERVAL 1 DAY, 'Asia/Kolkata') AS t, count() AS n
FROM default.http
WHERE `timestamp` >= toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC')
  AND `timestamp` < toDateTime64('2026-11-12 18:30:00.000', 3, 'UTC')
GROUP BY t
ORDER BY t
```

## The macros

Every macro name starts with `__`.

| Macro | ClickHouse SQL | LogsQL |
| :--- | :--- | :--- |
| `{{__from}}`, `{{__to}}` | `toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC')` | `"2026-11-05T18:30:00Z"` |
| `{{__time_filter}}` | `` `ts` >= <from> AND `ts` < <to> `` on the source's timestamp column | Not available. The request's range already applies |
| `{{__interval}}` | `INTERVAL 1 DAY` | `1d` |
| `{{__interval_s}}` | `86400` | `86400` |
| `{{__timezone}}` | `'Asia/Kolkata'` | Not available. Use `{{__tz_offset}}` |
| `{{__tz_offset}}` | Not available. Use `{{__timezone}}` | `19800s` |

LogchefQL accepts no macro. Its compiler always applies the time range.

The range end is exclusive, so two ranges that meet never count a row twice.
`{{__from}}` and `{{__to}}` are instants in UTC. To compare them with a `Date`
column, convert them: `event_date >= toDate({{__from}}, {{__timezone}})`.

On VictoriaLogs, bucket by local days with the offset:

```
level:error | stats by (_time:{{__interval}} offset {{__tz_offset}}) count() as errors
```

Without the offset, `stats by (_time:1d)` buckets are UTC days.

## What a macro renders from

| Input | Request field | Used by |
| :--- | :--- | :--- |
| Time range | `start_time` and `end_time`, RFC 3339 | `{{__from}}`, `{{__to}}`, `{{__time_filter}}`, `{{__tz_offset}}` |
| Timezone | `timezone`, an IANA name such as `Asia/Kolkata`. Absent means UTC | `{{__timezone}}`, `{{__tz_offset}}` |
| Bucket size | `interval` on a query, `window` on a histogram | `{{__interval}}`, `{{__interval_s}}` |

The bucket size is one of `1s`, `5s`, `10s`, `15s`, `30s`, `1m`, `5m`, `10m`,
`15m`, `30m`, `1h`, `2h`, `3h`, `6h`, `12h` and `24h`. No other duration is
accepted.

Each surface sends these inputs for you:

| Surface | Range and timezone | Bucket size |
| :--- | :--- | :--- |
| Explorer, SQL mode | The time picker and your timezone preference | About 100 buckets over the range |
| Dashboard panel | The dashboard range, or the [panel's own range](/features/dashboards/#panel-time) | The panel's **Bucket size** |
| [CLI](/integration/cli/#raw-native-queries) | `--since`, or `--from` and `--to`, and the configured timezone | `--interval` |
| [MCP server](/integration/mcp-server/#querying-logs) | `start_time`, `end_time` and `timezone` | `interval` on `query_logs`, `window` on `get_log_histogram` |

## Rules

- **A macro stands for an expression.** Inside a string, a quoted identifier, a
  heredoc or a raw string it is an error. Inside a comment it stays as written.
- **A missing input is an error.** A time macro with no range in the request
  returns 400, and so does `{{__interval}}` with no bucket size. Logchef never
  fills in a default.
- **The names are reserved.** A request variable named after a macro, such as
  `__from`, returns 400. A new saved-query variable cannot start with `__`. A
  saved query that already has such a variable keeps working, unless the name is
  a macro.
- **A value is never expanded.** A variable whose value is the text
  `{{__to}}` stays that text.
- **A macro counts as provided in an optional block.** `[[ AND {{__time_filter}} ]]`
  is always kept. `[[ AND ts >= {{__from}} AND service = {{service}} ]]` drops
  out when `service` has no value.

See [Query Variables](/user-guide/query-interface/#query-variables) for
variables and optional blocks.

## Whose timezone

A window or a bucket takes its timezone from whatever defines it.

| Defined by | Timezone | Same for every viewer |
| :--- | :--- | :--- |
| SQL functions in the query text, such as `today()`, `now()` and `toDate(ts)` | The ClickHouse server, or the column | Yes |
| The time range, `{{__from}}`, `{{__to}}` and `{{__time_filter}}` | The viewer's | No. "Today" differs for two viewers in different zones |
| `{{__interval}}` used with `{{__timezone}}`, and count buckets | The viewer's | No |

Do not mix the rows in one query. A `toDate(ts)` bucket inside a viewer-zone
range puts the first and last bucket on different day boundaries than the
range.

## Day buckets and daylight saving

ClickHouse aligns `toStartOfInterval(ts, INTERVAL 1 DAY, '<zone>')` to local
midnight on both sides of a daylight-saving change.

VictoriaLogs takes one fixed offset, read at the start of the range.
`{{__tz_offset}}` renders that offset. After a change inside the range, its day
buckets start one hour off local midnight. A dashboard count panel shows a
notice when that happens.
