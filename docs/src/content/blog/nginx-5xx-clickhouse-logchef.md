---
title: "Debugging a 5xx Spike in Nginx Access Logs with ClickHouse and Logchef"
description: "Query nginx access logs in ClickHouse when 5xx errors spike: live-tail the burst, narrow with LogchefQL, then pivot to SQL to find the bad host and endpoint."
pubDate: 2026-07-15
tags: ["clickhouse", "nginx", "observability", "logs", "incident-response"]
author: "Logchef Team"
---

Your pager goes off: `5xx rate > 2%`, evaluated over the last 5 minutes. No deploy just went out. No infra change is in flight. The dashboard shows a jagged spike in the error-rate panel and not much else. Grafana can tell you *that* something broke, not *what* is breaking or *for whom*.

This is the part where a lot of teams still `ssh` into a box and `tail -f access.log | grep " 5"`. If your nginx logs already live in ClickHouse, you don't have to. This post walks through that exact failure mode: alert fires, you open the log explorer, live-tail to confirm it's still happening, narrow the noise with a query language, then drop into SQL to find the one endpoint (and the one backend) actually causing it.

We'll use [Logchef](https://github.com/mr-karan/logchef), an open-source query and alerting layer on top of ClickHouse (and VictoriaLogs), and a schema you can copy directly.

## The logs are already in ClickHouse

Assume Vector is already tailing your nginx access log and shipping combined-format entries into a ClickHouse table. Here's a schema for that — it mirrors the one Logchef ships in its own local dev environment for testing HTTP access logs:

```sql
CREATE TABLE IF NOT EXISTS default.http
(
    `timestamp`        DateTime64(3) CODEC(DoubleDelta, LZ4),
    `host`             LowCardinality(String) CODEC(ZSTD(1)),
    `method`           LowCardinality(String) CODEC(ZSTD(1)),
    `protocol`         LowCardinality(String) CODEC(ZSTD(1)),
    `referer`          String CODEC(ZSTD(1)),
    `request`          LowCardinality(String) CODEC(ZSTD(1)),
    `status`           UInt16 CODEC(ZSTD(1)),
    `user-identifier`  LowCardinality(String) CODEC(ZSTD(1)),
    `bytes`            UInt32 CODEC(ZSTD(1)),
    INDEX idx_method method TYPE set(100) GRANULARITY 4,
    INDEX idx_status status TYPE minmax GRANULARITY 4,
    INDEX idx_referer referer TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (host, timestamp)
TTL toDateTime(toUnixTimestamp(timestamp)) + toIntervalDay(7)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;
```

`host` here is whichever edge/app node emitted the log line, useful once you have more than one nginx instance behind a load balancer, which is exactly the case where "one bad host" is a plausible root cause. `ORDER BY (host, timestamp)` means queries that filter or group by `host` first are cheap; the `minmax` index on `status` and the token bloom filter on `referer` keep filtered scans fast without a full column scan.

A Vector pipeline that gets nginx's combined log format into this shape:

```toml
[sources.nginx_access]
type = "file"
include = ["/var/log/nginx/access.log"]
read_from = "beginning"

[transforms.parse_nginx]
type = "remap"
inputs = ["nginx_access"]
source = '''
parsed, err = parse_regex(.message, r'^(?P<host>\S+) \S+ (?P<user>\S+) \[(?P<ts>[^\]]+)\] "(?P<method>\S+) (?P<request>\S+) (?P<protocol>\S+)" (?P<status>\d+) (?P<bytes>\d+) "(?P<referer>[^"]*)" "(?P<agent>[^"]*)"')
if err != null {
  log("unparsed nginx line: " + err, level: "warn")
  abort
}

.timestamp = parse_timestamp!(parsed.ts, format: "%d/%b/%Y:%H:%M:%S %z")
.host = parsed.host
."user-identifier" = parsed.user
.method = parsed.method
.request = parsed.request
.protocol = parsed.protocol
.status = to_int!(parsed.status)
.bytes = to_int!(parsed.bytes)
.referer = parsed.referer
del(.message)
'''

[sinks.clickhouse]
type = "clickhouse"
inputs = ["parse_nginx"]
endpoint = "http://localhost:8123"
database = "default"
table = "http"
compression = "gzip"
skip_unknown_fields = true
```

Point Logchef at this table (Sources → Add Source, host/port/database/table), assign it to a team, and you're ready to query. Worth saying plainly: Logchef doesn't collect or parse logs itself. Vector (or whatever ships your logs) does that job; Logchef is the query and alerting surface on top of what's already in ClickHouse.

## The alert that started this

Logchef's alerting evaluates a query on a schedule and fires when a threshold trips — no separate alerting stack needed. For this table, a condition-mode alert (Logchef generates the executable SQL from the filter) looks like:

- **Query**: `status >= 500`
- **Aggregate**: `count`
- **Threshold**: `> 100`
- **Lookback**: `5m`
- **Frequency**: `60s`
- **Recipients**: your on-call rotation, plus a webhook into whatever chat tool you use

That's the alert that just paged you. It tells you the count crossed a threshold, nothing about which host or endpoint. That's what the rest of this is for.

## Confirm it's still happening: live tail

Before digging into history, check if the spike is ongoing. Open the source in Logchef, type `status>=500` in LogchefQL mode, and click **Live**. Instead of running one query, Logchef streams matching rows as they land.

Two things worth knowing before you rely on it: on ClickHouse sources, live tail polls rather than subscribes. It re-scans a short trailing window each cycle to catch rows that finish ingesting slightly late, so there's a small delay, not a true push stream. And it's only available in LogchefQL mode; if you're on the SQL tab, there's no **Live** button, since raw ClickHouse SQL isn't supported for tailing (VictoriaLogs sources can tail in native LogsQL too, but ClickHouse can't).

If rows are still streaming in with `status>=500`, you're mid-incident, not looking at a blip that already resolved.

## Narrow it with LogchefQL

Switch back to a normal query and start cutting the noise. LogchefQL is a flat key-value filter language — `field=value`, comparisons, `and`/`or`, no aggregation:

```
status>=500
```

Too broad — that's every 500-599 in the window. Narrow by method, since a GET spike and a POST spike usually mean different things:

```
status>=500 and method="POST"
```

Or check whether it's concentrated on one host:

```
status>=500 and host="edge-03"
```

Once you're not drowning in irrelevant columns, use the pipe operator to select exactly what you want to look at instead of full rows:

```
status>=500 | timestamp host method request status bytes
```

This is still one-shot filtering, though — LogchefQL has no `GROUP BY`. To find out *which* host and *which* endpoint are actually responsible, rather than eyeballing a scrolling table, you need aggregation. That means switching to the SQL tab.

## Pivot to SQL for the breakdown

Logchef's native SQL mode runs your query against ClickHouse exactly as written — full access to `GROUP BY`, aggregates, and everything else LogchefQL intentionally leaves out. Note the tradeoff: in SQL mode the time-range picker becomes informational only, so put your own time filter in the `WHERE` clause.

```sql
SELECT
    host,
    request,
    status,
    count() AS n
FROM default.http
WHERE status >= 500
  AND timestamp >= now() - INTERVAL 15 MINUTE
GROUP BY host, request, status
ORDER BY n DESC
LIMIT 20
```

Illustrative output — the shape of what you're looking for, not a claim about real traffic:

| host | request | status | n |
|---|---|---|---|
| edge-03 | `POST /api/checkout HTTP/1.1` | 502 | ~800 |
| edge-01 | `POST /api/checkout HTTP/1.1` | 502 | ~40 |
| edge-02 | `GET /api/catalog HTTP/1.1` | 500 | ~12 |

One host, one endpoint, one status code dominates the count by an order of magnitude. That's the signal an aggregate count-over-time alert can't give you on its own: it tells you *something* crossed a threshold, not that it's concentrated on `edge-03` serving `/api/checkout`.

From here, the result view's **Histogram** tab (with **Group By** set to `host`) turns the same query into a time-series bar chart, so you can see exactly when the spike on `edge-03` started relative to the others. That's useful for correlating against a deploy timestamp or a config push on that one node.

## What this doesn't do for you

Worth being direct about the limits, since none of this replaces judgment:

- Logchef found *where* the errors are concentrated. It didn't tell you *why* `edge-03` is failing; that's still a `nginx -T` / upstream health check / systemd journal problem on that host.
- The alert is a threshold on a count, not an anomaly detector. Set the threshold too low and you get paged on normal traffic variance; too high and small but real problems don't page at all.
- Live tail on ClickHouse is a polling window, not a real subscription, and rows that are byte-identical across every column (including timestamp) can't be told apart from a re-fetch. That's a real constraint of ClickHouse tables having no row ID, not a bug to chase.

None of that makes the workflow useless. It moves the incident from "somewhere in prod, no idea where" to "this one host, this one endpoint" in the time it takes to type three queries.

Try this against your own logs at the [live demo](https://demo.logchef.app), or self-host from [github.com/mr-karan/logchef](https://github.com/mr-karan/logchef).
