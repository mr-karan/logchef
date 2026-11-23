---
title: Aggregations
head:
  - tag: title
    content: "LogchefQL Log Aggregations | Logchef"
description: Count, sum, average and take percentiles of matching logs with a LogchefQL aggregation pipe. The same query runs on ClickHouse and VictoriaLogs.
---

An aggregation pipe turns the logs that a LogchefQL filter matches into a
table of numbers. Write the filter, a pipe, and one or more functions:

```text
level=error | count() by service
```

This counts the error logs of each service. The same text compiles to
ClickHouse SQL on a ClickHouse source and to LogsQL on a VictoriaLogs source,
so you do not need the native language of either backend for a count, a sum,
an average, a percentile or a group-by.

An aggregation runs in the [Explorer](#in-the-explorer), in dashboard panels
in [result mode](/features/dashboards/#chart-a-logchefql-aggregation), in the
[CLI](/integration/cli/#aggregate-with-logchefql) and through
[MCP](/integration/mcp-server/#aggregations).

## Syntax

```text
<filter> | <item>, <item>, ... [by <key>, <key>, ...] [| sort <column> [asc|desc]] [| limit <n>]
```

- An item is a function call with an optional name: `count()`,
  `p95(duration_ms) as p95`.
- The filter is optional. `| count()` counts every log in the time range.
- Function names and the words `as`, `by`, `sort`, `limit`, `asc` and `desc`
  are case-insensitive, as `and` and `or` are.
- A pipe is an aggregation only when its first item is a function call. A pipe
  of plain field names still [selects fields](/guide/search-syntax/#pipe-operator-custom-select).
  `| count` selects the field named `count`, and `x=1 | limit` selects the
  field named `limit`.

### Functions

| Function | Argument | Result |
| :--- | :--- | :--- |
| `count()` | None | The number of matching logs |
| `count_distinct(f)` | Any field | The number of distinct non-empty values of `f`. Exact on both backends |
| `sum(f)`, `avg(f)` | A number field | The sum and the mean of the numeric values of `f` |
| `min(f)`, `max(f)` | A number field | The smallest and the largest numeric value of `f` |
| `p50(f)`, `p75(f)`, `p90(f)`, `p95(f)`, `p99(f)` | A number field | A percentile of `f`. Approximate |

A query can have up to 10 items. `count` takes no field, and every other
function takes exactly one. Fields use the same paths as filters, including
nested fields such as `log_attributes.host` and quoted names such as
`"bytes sent"`.

The number functions skip a log whose field is missing or empty. When no log
has a numeric value, the result is null. Text that is not a number is skipped
by every function on ClickHouse and by `sum` and `avg` on VictoriaLogs, but
`min`, `max` and the percentiles on VictoriaLogs compare it as text and can
return null. Keep number functions to fields that hold numbers. The
[backend differences](#backend-differences) section has the details.

### Group keys

`by` splits the result into groups, one row per group. A query can have up to
5 keys.

- A field key groups by the value of that field: `by service`, `by
  log_attributes.host`.
- `time` groups by time bucket. `time(5m)` sets the bucket size in the query.
  The sizes are `1s`, `5s`, `10s`, `15s`, `30s`, `1m`, `5m`, `10m`, `15m`,
  `30m`, `1h`, `2h`, `3h`, `6h`, `12h` and `24h`.
- A bare `time` key takes the bucket size from the request. The Explorer sends
  the bucket size of its histogram, and a dashboard panel sends its
  [bucket size](/features/dashboards/#bucket-size). The CLI and MCP send none
  unless you pass `--interval` or `interval`, and the query then fails with
  "this query buckets by time, so the request needs an interval".
- A query can have one time key. Its position in `by` does not matter: the
  `time` column always comes first.
- A field named `time` is written with quotes, `"time"`. `time.zone` is a field.
- The time range divided by the bucket size must give at most 1,000 buckets.
  A longer range fails and names the smallest size that fits, for example "1h
  over this range gives 2160 buckets. The maximum is 1000. Use 3h or longer".

### Column names

Each item and each key becomes a result column.

| Item | Default name |
| :--- | :--- |
| `count()` | `count` |
| `func(f)` | `func_f`. Each character of the path other than a letter, a digit or `_` becomes `_`, so `p95(log.duration)` is `p95_log_duration` |
| A field key | The path as written: `log_attributes.host` stays `log_attributes.host` |
| The `time` key | `time` |

`as` sets another name: `count() as errors`, `by service as svc`. A name
starts with a letter or `_`, holds letters, digits and `_`, and is at most 64
characters. Two columns cannot have the same name, so `| count(), count()`
fails with "two columns are named "count". Name one with "as"". A field cannot
be grouped twice, and names and fields that start with `__lc_` are reserved.

On ClickHouse, a name cannot also be a column of the source, other than the
field that a key groups by. ClickHouse reads a column alias inside `WHERE`, so
`count() as level` would change the meaning of a `level` filter. On a table
that has a column named `time`, write `by time as bucket`.

### Sort and limit

`| sort <column>` orders the result by one output column, ascending unless you
add `desc`. `| limit <n>` keeps the first `n` rows, from 1 to 10,000. When
both appear, `sort` comes first.

```text
level=error | count() as errors, count_distinct(user_id) as users by service | sort errors desc | limit 10
```

A query that groups by `time` and a field cannot have `limit`, because the
limit would cut buckets and not series. Add a filter to reduce the series
instead.

Without `sort`, the order depends on the shape of the result:

| Shape | Default order |
| :--- | :--- |
| No keys | One row |
| Field keys only | The first value, largest first, then the keys A to Z |
| A `time` key | `time` ascending, then the keys A to Z |

The order is total, so a capped grouped result holds the top groups by the
first value, and two runs return the same rows.

## Result shapes

| Shape | `by` | Rows | Example |
| :--- | :--- | :--- | :--- |
| `scalar` | None | Exactly one | `level=error \| count()` |
| `grouped` | Fields only | One per group | `status>=500 \| count() by host` |
| `timeseries` | `time` only | One per bucket that has logs | `\| count() by time` |
| `grouped_timeseries` | `time` and fields | One per bucket and group that has logs | `level=error \| count() by time(1h), service` |

The server returns only groups that have at least one matching log. It adds no
zero rows, no empty buckets and no "Other" row. Charts in the Explorer and on
dashboards fill the empty buckets themselves.

## Examples

```text
# Error logs per service
level=error | count() by service

# Error logs in the time range
level=error | count()

# Server errors per host
status>=500 | count() by host

# Log volume over time, with the bucket size of the request
| count() by time

# p95 and mean latency of the api service in 5-minute buckets
service=api | p95(duration_ms), avg(duration_ms) by time(5m)

# p95 latency per service over time, one series per service
service=api | p95(duration_ms) by time, service

# Hourly error counts per service
level=error | count() by time(1h), service

# Top 10 services by errors, with distinct users
level=error | count() as errors, count_distinct(user_id) as users by service | sort errors desc | limit 10

# Bytes per service, the 20 largest
| sum(bytes) as total_bytes, avg(bytes) by service | sort total_bytes desc | limit 20

# Every percentile of one field
| min(a), max(a), p50(a), p75(a), p90(a), p99(a)

# Group by a nested field
| count() by log_attributes.host

# Group by a field named time
| count() by "time"
```

## In the Explorer

When a LogchefQL query aggregates, the Explorer shows the result in place of
the log rows, with a **Chart** and **Table** toggle.

- A result with a `time` column and at most one field key opens as a line
  chart, one series per group. Any other result opens as a table.
- A grouped result with one key charts as a top list, and a single value
  charts as a stat.
- A result with two or more field keys has no chart, because one row per
  bucket and series would not be unique. The toggle is off, with "A chart
  needs one group field". A scalar result with several values has no chart
  either ("A chart needs one value").
- The chart fills empty buckets: 0 for `count` and `count_distinct`, a gap for
  the other functions. It does not fill a result that the row limit cut or
  that has its own `limit`, since a missing row there may hold data.
- Click a group cell in the table to add `field = "value"` to the filter. A
  null cell adds nothing.
- The histogram, export, the log view modes and live tail are off for an
  aggregated result. The field sidebar still works on the filter.
- The Explorer's row limit applies to the result rows. The **First N rows**
  chip shows only when a limit cut rows. A `limit` in the query is your own
  cut, and shows no chip.
- **Add to dashboard** asks for a dashboard you can edit and opens its panel
  builder on a new result panel with the query and the current view.

The generated query popover shows the native query. On VictoriaLogs the
values there are named `__lc_v0`, `__lc_v1` and so on: Logchef writes the
result under the output names. A copy of that LogsQL in a native LogsQL panel
returns the internal names.

## Where an aggregation is rejected

Some features need log rows. They refuse an aggregation with "this feature
needs log rows, and the query aggregates. Remove the pipe after the filter",
and never ignore the pipe:

- Live tail, in the Explorer and with `logchef tail`.
- `logchef histogram`.
- The MCP `compare_windows` tool and the field value tools.
- A dashboard panel in count mode. Save fails with "an aggregation needs
  result mode", and the panel builder switches to result mode when you type an
  aggregation.

A Loki source does not run aggregations and answers "aggregations are not
supported for this source type".

## Limits

| Bound | Value | When it is reached |
| :--- | :--- | :--- |
| Items | 10 | The query does not parse |
| Group keys | 5, with at most one `time` | The query does not parse |
| `limit` | 1 to 10,000 | The query does not parse |
| Buckets | 1,000 over the time range | An error that names the smallest bucket size that fits |
| Rows returned | The smallest of the query's `limit`, the request's row limit and the server maximum (`query.max_preview_limit`) | The extra rows are cut, and `stats.truncation` says so |
| Groups, ClickHouse | 100,000 | The query fails: "the query produced more than 100000 groups. Add a filter or group by fewer fields" |
| Memory, ClickHouse | `query.aggregation_max_memory_bytes`, default 2 GiB, or the lower limit of the source or the ClickHouse user | The query fails: "the query needs more memory than the limit allows. Add a filter or shorten the range" |

A ClickHouse query that passes a bound fails and returns no rows, since a
partial aggregate would be wrong in every row. VictoriaLogs has no per-query
bound on groups or memory. The row limit, the query timeout and the
VictoriaLogs server's own settings are the only bounds there. The memory
setting is in the [configuration reference](/getting-started/configuration/#query-settings).

## Backend differences

`count()` in every shape, `sum` and `avg` over fields that hold only numbers,
group keys on fields that every log has, `sort` and `limit` give the same
result on both backends. The rest can differ:

| Topic | ClickHouse | VictoriaLogs |
| :--- | :--- | :--- |
| Number fields | The column type decides. Text is parsed as a number, and text that is not a number is skipped. Bool, date, Array, Map, Tuple and JSON columns fail with an error such as `sum needs a number field. "tags" is Array(String)` | Every value is text. `sum` and `avg` skip text that is not a number. `min`, `max` and the percentiles compare it as text, and a result that is not a number comes back null |
| A missing field in a number function | Skipped, except on a numeric column that is not `Nullable`, where the column default, such as 0, takes part | Skipped |
| No numeric value at all | null | null |
| A missing field as a group key | `""` for a String, Map or JSON text field, null for a `Nullable` column: two different groups | `""` |
| `count_distinct` | Exact. Empty values are not counted | Exact. Empty values are not counted |
| Percentiles | Approximate, from a sample on large inputs, and interpolated between values. Two runs can differ in the last digits | A value from the data, with no interpolation. The median of the values 101 to 112 is 107, where ClickHouse gives 106.5 |
| Time buckets | In the request's timezone, with daylight-saving changes | One fixed UTC offset, read at the start of the range. After a daylight-saving change inside the range, buckets start one hour off |
| Large integers | `sum` of an integer column is exact | `sum` is a float, and loses digits above 2^53 |
| Result types | Typed by ClickHouse. `count` is `UInt64`, and `sum` keeps the integer type of its column | `count` and `count_distinct` are exact digit strings typed `UInt64`. Every other value is a JSON number typed `Float64` |
| Group and memory bounds | 100,000 groups and a memory limit | None per query |
| Nested fields | Map key, native JSON path, or a JSON string column | One flat dotted field name. VictoriaLogs flattens nested JSON at ingest |
| Output names | Cannot be a column of the source, see [column names](#column-names) | No restriction. `_time` cannot be a field key next to a `time` key |

## Under the hood

On ClickHouse, `level=error | count() by service` compiles to one `GROUP BY`
over the source table:

```sql
SELECT `service` AS `service`, count() AS `count`
FROM logs.app
WHERE `timestamp` BETWEEN toDateTime('2026-11-15 00:00:00', 'UTC') AND toDateTime('2026-11-16 00:00:00', 'UTC')
  AND (`level` = 'error')
GROUP BY `service`
ORDER BY `count` DESC, `service` ASC
```

A number function reads text through `toFloat64OrNull`, and its result uses
the `-OrNull` form, such as `sumOrNull` or `quantileOrNull(0.95)`.
`count_distinct` is `uniqExact`, which skips `""` on a text field. A time key
is `toStartOfInterval` in the request's timezone.

On VictoriaLogs the same query compiles to a `stats` pipe:

```text
level:="error" | stats by (service) count() as __lc_v0 | sort by (__lc_v0 desc, service)
```

A number function skips logs without the field, as in
`sum(bytes) if (NOT bytes:"")`. `count_distinct` is `count_uniq`, and a time
key is `_time:5m offset <offset>`.

Logchef applies the row limit when it runs the query, on both backends.

## API

The routes do not change. `POST /api/v1/teams/{team_id}/sources/{source_id}/logchefql/query`
and `.../logchefql/translate` accept an optional `interval`, the bucket size
of a bare `time` key. When the query aggregates, the response has an
`aggregation` object next to `generated_query`:

```json
{
  "shape": "grouped",
  "interval": "",
  "limit": 0,
  "columns": [
    { "name": "service", "role": "group", "type": "String" },
    { "name": "count", "role": "value", "type": "UInt64" }
  ]
}
```

`logs` holds the result rows and `columns` their names and types, in the
order of `aggregation.columns`: time first, then the keys, then the values.
`interval` is the bucket size in use, and `limit` is the query's own limit,
or 0.

In a translate response, `generated_query` holds the whole aggregated query
on VictoriaLogs. On ClickHouse, `sql` stays the filter, and `full_sql` and
`generated_query` hold the aggregated query only when the request has
`start_time`, `end_time` and `timezone`. Without them, `generated_query` is the
filter. `/logchefql/validate`
checks the syntax and the rules that do not depend on the source, and needs
no `interval`. The ClickHouse name and number-field rules and the bucket
limit are checked when the query compiles.

## Next steps

- [Search Syntax](/guide/search-syntax/): the filter part of a query
- [Dashboards](/features/dashboards/#chart-a-logchefql-aggregation): chart an aggregation in a panel
- [Logchef CLI](/integration/cli/#aggregate-with-logchefql): aggregate from the terminal

[Try this syntax in the LogchefQL Playground](/playground/) to compare the output for all three backends.
