# Raw ClickHouse SQL (`sql`) reference

`logchef sql '<query>'` (alias `logchef native`) sends your text to the backend
verbatim: **ClickHouse SQL** on ClickHouse sources, LogsQL on VictoriaLogs
sources (see `logsql-victorialogs.md`). This page covers the ClickHouse case.

Use `sql` only when LogchefQL can't express the query: aggregation (`count()`,
`GROUP BY`), `DISTINCT`, joins, subqueries, window functions, `argMax`, etc. For
plain filtering use `logchef query`; for counts-over-time use `logchef histogram`.

## Always bound it

Raw SQL is unbounded and backend-specific. Every query should have a time filter
and a `LIMIT`. You can supply time two ways:

1. **Write it in the SQL** — full control:

   ```bash
   logchef sql "SELECT service, count() AS c FROM logs.app
     WHERE _timestamp > now() - INTERVAL 1 HOUR AND level='error'
     GROUP BY service ORDER BY c DESC LIMIT 10"
   ```

   Relative bounds (`now() - INTERVAL …`) are safe. A literal such as
   `_timestamp > '2026-07-14 09:00:00'` is read in the column's (or server's)
   timezone, not your Logchef timezone; write
   `toDateTime('2026-07-14 09:00:00', 'Asia/Kolkata')`, or use option 2.

2. **Let Logchef inject it** — pass `--since` / `--from`+`--to` and Logchef adds
   a `<ts_field> BETWEEN toDateTime('…', tz) AND toDateTime('…', tz)` condition,
   spliced into your top-level `WHERE` (or added as one) ahead of
   `GROUP/ORDER/LIMIT`. It uses the source's configured timestamp field.

   ```bash
   logchef sql "SELECT service, count() c FROM logs.app WHERE level='error'
     GROUP BY service ORDER BY c DESC LIMIT 10" -s 1h
   ```

   The injector is literal-aware: `WHERE`/`LIMIT` inside string literals,
   backtick identifiers, comments, and subqueries are not mistaken for clauses.

### `__START__` / `__END__` placeholders

For full control over where the bounds land, put both placeholders in your SQL
and pass a time range; Logchef substitutes `toDateTime('…', tz)` expressions:

```bash
logchef sql "SELECT count() FROM logs.app
  WHERE _timestamp BETWEEN __START__ AND __END__ AND level='error'" -s 30m
```

You must include **both** placeholders or neither.

## Time macros and variables (queries copied from a dashboard panel)

A panel's SQL uses server-side macros instead of literal time bounds. Run it
unchanged: the server fills the macros from the request.

| Macro | Renders | Comes from |
|---|---|---|
| `{{__time_filter}}` | `` `ts` >= <from> AND `ts` < <to> `` on the source's timestamp column | `--since` / `--from`+`--to` |
| `{{__from}}`, `{{__to}}` | `toDateTime64('…', 3, 'UTC')` | `--since` / `--from`+`--to` |
| `{{__interval}}` | `INTERVAL 1 DAY` | `--interval` |
| `{{__interval_s}}` | `86400` | `--interval` |
| `{{__timezone}}` | `'Asia/Kolkata'` | the configured timezone |

```bash
logchef sql 'SELECT toStartOfInterval(_timestamp, {{__interval}}, {{__timezone}}) AS t, count() AS n
  FROM logs.app WHERE {{__time_filter}} AND service = {{service}} GROUP BY t ORDER BY t' \
  -s 7d --interval 24h --var service=api
```

- `--interval` is one of `1s 5s 10s 15s 30s 1m 5m 10m 15m 30m 1h 2h 3h 6h 12h 24h`.
- `--var NAME=VALUE` fills `{{NAME}}` as a quoted text literal. `NAME:number=500`
  and `NAME:date=2026-11-06` send a number or a date. Repeat the flag per variable.
- With a time macro in the query the text is not rewritten, and the window rides
  in the request. With no time flag the window is the configured default range.
- A macro inside a string or a quoted identifier is an error. A missing input
  fails with the flag to add, for example `macro {{__interval}} needs an interval`.
- `{{__tz_offset}}` is LogsQL only (see `logsql-victorialogs.md`).

## Preview without executing

```bash
logchef sql "SELECT …" -s 1h --dry-run     # print the resolved SQL to stdout, no run, no server call
logchef sql "SELECT …" -s 1h --explain     # print resolved SQL to stderr, then run it
```

`--dry-run` is the clean, pipe-friendly way to inspect exactly what will run
(including injected time bounds).

## Common patterns

```bash
# Top error messages
logchef sql "SELECT msg, count() c FROM logs.app
  WHERE _timestamp > now() - INTERVAL 1 HOUR AND level='error'
  GROUP BY msg ORDER BY c DESC LIMIT 10"

# Error rate per service
logchef sql "SELECT service,
    countIf(level='error') errors, count() total,
    round(100*errors/total, 2) pct
  FROM logs.app WHERE _timestamp > now() - INTERVAL 1 HOUR
  GROUP BY service ORDER BY pct DESC LIMIT 20"

# Distinct values of a column
logchef sql "SELECT DISTINCT service FROM logs.app
  WHERE _timestamp > now() - INTERVAL 6 HOUR LIMIT 50"
```

## Stdin

Pass `-` to read the query from stdin — handy for long or generated SQL:

```bash
cat query.sql | logchef sql -
```

## Streaming and CSV export (large result sets)

Buffered output builds the whole response in memory. For large pulls:

- `--stream --output jsonl` streams rows straight from the server (only `jsonl`
  is valid with `--stream`).
- `--output csv` runs a server-side export job and streams the finished CSV to
  stdout. `csv` is available on `sql` only.

Both raise the effective query timeout floor to 120s. Example:

```bash
logchef sql "SELECT * FROM logs.app WHERE level='error'" -s 24h --output csv > errors.csv
logchef sql "SELECT * FROM logs.app" -s 1h --stream --output jsonl | jq -r '.msg'
```

## Timeout

`--timeout <secs>` (default 30) bounds the query; the HTTP transport gets extra
headroom automatically so a slow query fails with a clean server error rather
than a client disconnect.

## Reminders

- Outside `{{name}}` placeholders your text runs verbatim. Never splice
  untrusted text (values copied from log lines, user input) into a query: pass
  it with `--var`, which the server substitutes as a literal.
- The table lives at `database.table` — get the exact name from
  `logchef sources` (the `TARGET` column) or `logchef schema`.
- `sql` does not accept LogchefQL. Filtering-only tasks belong in `logchef query`.
