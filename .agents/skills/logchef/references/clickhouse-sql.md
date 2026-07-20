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

- Parameterize / never build SQL from untrusted input by string-concatenation.
- The table lives at `database.table` — get the exact name from
  `logchef sources` (the `TARGET` column) or `logchef schema`.
- `sql` does not accept LogchefQL. Filtering-only tasks belong in `logchef query`.
