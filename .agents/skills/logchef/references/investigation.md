# Investigation playbook

The discipline: **discover before you query, aggregate before you sample, widen
the window last.** Reuse output you already have instead of re-running. This
keeps scans (and tokens) small and answers stable.

## The loop

1. **Orient** — which source, which backend, which columns.
2. **Quantify** — counts / histogram over a modest window. Don't pull rows yet.
3. **Narrow** — add a field filter; re-quantify.
4. **Sample small** — read ~10 rows.
5. **Pivot** — follow a trace/request id across services.
6. **Widen** — expand the time range only once counts are bounded.

## Step 0 — find the source (when you don't know it)

```bash
logchef find 'payment-api' -s 24h        # which sources mention this? (+ sample values)
logchef find 'db timeout' -s 6h --no-samples   # just the match-count summary
```

`find` scans candidate columns (`service`, `service_name`, `job_name`, `app`,
`host`, `msg`) across every accessible source and ranks by match count. Restrict
with `-t <team>` / `-S <source>` if you already know roughly where to look.

## ClickHouse source — worked example

```bash
# 1. Orient
logchef sources -t platform                     # confirm app-logs is ClickHouse
logchef schema  -t platform -S app-logs         # columns + types
logchef fields  service                          # what service values exist?

# 2. Quantify — when did errors spike? (cheap: buckets, not rows)
logchef histogram 'level="error"' -t platform -S app-logs -s 6h --interval 5m

# 3. Narrow — which service dominates the spike?
logchef sql "SELECT service, count() c FROM logs.app
  WHERE level='error' GROUP BY service ORDER BY c DESC LIMIT 10" \
  -t platform -S app-logs -s 30m

# 4. Sample small
logchef query 'level="error" and service="payment-api"' \
  -t platform -S app-logs -s 30m -l 10

# 5. Pivot on a trace id pulled from a sample row
logchef query 'log_attributes.trace_id="abc123"' -t platform -S app-logs -s 1h

# 6. Widen only now, if counts stayed reasonable
logchef query 'level="error" and service="payment-api"' -s 6h -l 20
```

## VictoriaLogs source — worked example

Same loop; LogchefQL is identical, and raw aggregation uses LogsQL `| stats`.

```bash
# 1. Orient
logchef sources -t platform                     # confirm vl-app is VictoriaLogs
logchef schema  -t platform -S vl-app

# 2. Quantify
logchef histogram 'level="error"' -t platform -S vl-app -s 6h --interval 5m

# 3. Narrow — top apps by error count (LogsQL stats)
logchef sql '_time:30m level:=error | stats by (app) count() c | sort by (c) desc' \
  -t platform -S vl-app

# 4. Sample small
logchef query 'level="error" and app="checkout"' -t platform -S vl-app -s 30m -l 10

# 5. Pivot
logchef query 'trace_id="abc123"' -t platform -S vl-app -s 1h
```

## Histogram: find the spike, not the rows

`histogram` returns counts-over-time buckets — far cheaper than fetching rows,
and it tells you *where* to point steps 3–5.

```bash
logchef histogram 'level="error"' -s 6h --interval 5m
logchef histogram 'status>=500'   -s 1h --interval 1m --group-by service
logchef histogram 'level="error"' --from '2026-07-14 09:00:00' --to '2026-07-14 10:00:00'
```

`--interval` sets bucket width; `--group-by <field>` splits each bucket by a
dimension; the usual `--since` / `--from` / `--to` apply.

## Field discovery

Don't guess field names or values — they cost empty queries and confusion.

```bash
logchef schema -t platform -S app-logs   # all columns + types
logchef fields                            # list this source's fields
logchef fields service                    # observed values for `service` (autocomplete)
```

## Tail (live follow)

```bash
logchef tail 'level="error" and service="payment-api"' -s 1m
logchef tail 'status>=500' --interval 1 --limit 200 --output jsonl | jq -r '.msg'
logchef tail 'level="error"' --max-lines 50        # stop after 50 rows
```

`tail` polls (default every 2s), dedupes across polls, and starts from `-s`
(default `30s`, and unlike other commands it accepts a seconds unit). If a
single poll hits `--limit`, the oldest rows in that poll can be dropped — raise
`--limit` or lower `--interval` if you see the backpressure warning on stderr.
Ctrl-C to stop.

## Saved queries and collections

Reuse curated queries instead of rebuilding them:

```bash
logchef collections -t platform -S app-logs                 # list collections for a source
logchef collections "Error Dashboard" -s 1h                 # run by name, override window
logchef collections "Error Dashboard" --var env=prod        # fill a {{env}} variable

logchef saved-queries                                       # list saved queries
logchef saved-queries 14                                    # run by id
logchef saved-queries 'https://logs.example.com/logs/explore?team=8&source=11&id=14'
logchef saved-queries "Error Dashboard" --show-sql          # print resolved query, don't run
```

Collections/saved queries can be either LogchefQL or native; the `TYPE` column
in the list tells you which. Variables use `{{name}}` placeholders; override with
`--var name=value`.

## Handing off to the UI

When the question is visual (time-series shape, drill-down, sharing), stop and
open the explorer:

```bash
logchef open 'level="error" and service="payment-api"'   # open current source+query in browser
logchef open --print                                     # just print the URL (for pasting)
```
