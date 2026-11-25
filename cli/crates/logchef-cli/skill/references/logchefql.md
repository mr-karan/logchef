# LogchefQL reference

LogchefQL is Logchef's filter language. You write it once; the server translates
it to ClickHouse SQL (for ClickHouse sources) or LogsQL (for VictoriaLogs
sources). Used by `query`, `tail`, `histogram`, and LogchefQL collections.

Always wrap a query in **single quotes** so the shell leaves `"`, `!`, `|`, `()`
alone.

## Shape

Every clause is `field operator value`. Clauses combine with `and` / `or`
(case-insensitive) and parentheses. **There is no bare full-text search** — a
lone word like `'timeout'` is a syntax error. Search text by filtering a field:

```bash
# WRONG:  logchef query 'timeout'
# RIGHT:
logchef query 'msg~"timeout"' -s 15m
```

An empty query (or just a `| field-list`) matches everything in the window.
Use it for a quick look at a source, with a short `-s` and a small `-l`; add a
field filter for anything wider.

## Operators

| Operator | Meaning | Example |
|---|---|---|
| `=`  | equals | `level="error"` |
| `!=` | not equals | `level!="debug"` |
| `~`  | contains, case-insensitive substring | `msg~"connection refused"` |
| `!~` | does **not** contain | `path!~"/health"` |
| `>` `<` `>=` `<=` | numeric comparison | `status>=500`, `duration_ms>1000` |

### What the operators actually do

- **ClickHouse sources:** `~` → `positionCaseInsensitive(col, val) > 0` and `!~`
  → `... = 0`. Despite the internal name, `~` is a **case-insensitive substring**
  match, **not** a regular expression. `=`/`!=` are exact equality.
- **VictoriaLogs sources:** `=`→`field:=value`, `!=`→`NOT field:=value`,
  `~`→`field:~<regex>` (your substring is regex-escaped, so it stays a literal
  substring match), `!~`→`NOT field:~…`, comparisons→`field:>value` etc.

Because `~` is substring (not regex/tokenized), `msg~"time"` also matches
`"timeout"` and `"lifetime"`. Narrow with a more specific substring or combine
with `=` on another field. In an alert or a count this matters more than in a
search: `msg~"error"` also counts `"no error"`, `"error_count=0"` and
`"errors: []"`. Sample the matches (`-l 20`) before you trust a count, and
prefer `=` on a level field over `~` on the message.

## Values

- Bare values need no quotes: `status=200`, `level=error`.
- **Quote** anything with spaces or punctuation: `msg~"connection refused"`,
  `service="payment-api"`.
- Numbers stay unquoted for comparison operators on numeric fields:
  `status>=500`.
- On ClickHouse sources, a number compared with a **string** column must be
  quoted: `status_text="500"`, not `status_text=500`. Check the type with
  `logchef schema`. VictoriaLogs has no column types and accepts either.
- `null`: on ClickHouse, only `=` and `!=` accept it (`trace_id!=null`); any
  other operator is an error. VictoriaLogs rejects every `null` comparison;
  use `trace_id!=""` to find non-empty values there.
- Quote the *value*, using double quotes inside the single-quoted shell arg.

## Nested / Map / JSON fields

Use dot notation to reach into Map columns, JSON columns, or nested JSON inside
a string column:

```bash
logchef query 'log_attributes.user_id="12345"' -s 15m
logchef query 'log_attributes.request.method="POST" and msg~"error"' -s 1h
```

If a field *name itself* contains a dot, quote that segment:

```bash
logchef query 'log_attributes."user.name"="alice"' -s 15m
```

Discover what's available first with `logchef schema` (columns) and
`logchef fields <field>` (observed values) rather than guessing.

### JSON inside a String column

On ClickHouse, a dotted path on a **String** column (for example a `msg` column
that holds a JSON body) compiles to `JSONExtractString(msg, 'key')`. That
returns the value only when the JSON value is a string. **A JSON number comes
back as an empty string.** So:

- `msg.level="error"` works when `level` is a JSON string.
- `msg.latency_ms>500` is an error (a number compared with text).
- `msg.latency_ms>"500"` compares text, and `p95(msg.latency_ms)`,
  `sum`, `avg`, `min` and `max` skip every numeric value. Both translate, so
  `logchef explain` prints a warning for them.

For a numeric JSON value, use native SQL with `JSONExtractFloat` or
`JSONExtractInt`:

```bash
logchef sql "SELECT quantile(0.95)(JSONExtractFloat(msg, 'latency_ms')) AS p95
  FROM logs.app WHERE job_name = 'api'" -s 1h
```

Map and native JSON columns keep their types and are not affected.

## Selecting columns — the `|` pipe

End a query with `| field1 field2 …` to return only those columns (space-
separated). A leading `|` with no filter selects columns across all rows:

```bash
logchef query 'level="error" | _timestamp service msg' -s 15m
logchef query '| service_name msg' -s 5m          # no filter, just these columns
```

The timestamp column is included by the server as needed for ordering; list it
explicitly if you want it shown.

## Aggregating — the `| func()` pipe

A pipe whose first item is a function call aggregates instead of selecting
columns. The same text runs on ClickHouse and VictoriaLogs.

```bash
logchef query 'level="error" | count() by service' -s 1h
logchef query 'level="error" | count() as errors, count_distinct(user_id) as users by service | sort errors desc | limit 10' -s 1h
logchef query 'service="api" | p95(duration_ms), avg(duration_ms) by time(5m)' -s 1h
logchef query '| count() by time' -s 1h --interval 5m
```

- Functions: `count()`, `count_distinct(f)`, `sum(f)`, `avg(f)`, `min(f)`,
  `max(f)`, `p50(f)`, `p75(f)`, `p90(f)`, `p95(f)`, `p99(f)`. Up to 10, comma
  separated. Percentiles are approximate.
- `by` takes up to 5 keys: fields, and at most one `time` bucket. `time(5m)`
  names its window; bare `time` takes the window from `--interval` and fails
  without it. A field literally named `time` is written `"time"`.
- Column names: `count`, `func_field` (`p95(log.duration)` is
  `p95_log_duration`), the key path as written, `time`. Rename with `as`.
- `| sort <column> [asc|desc]` and `| limit N` (1 to 10000) follow the
  aggregation. `limit` is rejected with `time` and a field key together.
- **At most 1,000 time buckets** over the range: `by time(5m)` over `-s 7d` is
  2,016 buckets and fails with an error that names the smallest window that
  fits (`15m` for 7 days, `5m` for 3 days, `1m` for 16 hours).
- **Rows are cut at the row limit**, the smallest of the query's `| limit`,
  `-l` and the server maximum. `query` defaults to `-l 100`, so a grouped time
  series (`by time(15m), service`: buckets times groups rows) is cut silently
  unless you raise it: `-l 1000` or more. Check `stats.truncation` in
  `--output json`. The MCP `query_logchefql` tool caps the limit at 500, so
  use a longer bucket there.
- Default order: grouped results by the first value, descending; time series
  by `time`, ascending.
- Output: `text` and `table` print the result columns; `json`/`jsonl` print
  the rows as they are, and `json` adds an `aggregation` object (shape and
  columns).
- `tail` and `histogram` need log rows and reject an aggregation.

## Combining conditions

```bash
logchef query 'level="error" and service="api"' -s 15m
logchef query '(service="auth" or service="users") and level="error"' -s 1h
logchef query 'status>=500 and path!~"/health" and msg~"timeout"' -s 30m
```

`and` binds tighter than `or`; parenthesize when mixing them.

## Previewing the translation

Before running something expensive, see exactly what the server will execute:

```bash
logchef explain 'level="error" and status>=500'    # print generated SQL/LogsQL, no scan
logchef query   'level="error"' --explain -s 15m    # trace SQL to stderr AND run
logchef query   'level="error"' --dry-run           # print generated SQL, then exit
```

`explain` / `--dry-run` are the fastest way to debug "why did this match
nothing?" — you see whether your nested-field path or substring landed where you
expected.

## When LogchefQL isn't enough

LogchefQL filters, selects columns and aggregates; it does not join, compute
expressions (`sum(a) / count()`), or filter after an aggregation. For those,
subqueries, or window functions, drop to `logchef sql` (see
`clickhouse-sql.md`) or, on VictoriaLogs, LogsQL `| stats` (see
`logsql-victorialogs.md`).
