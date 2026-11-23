---
title: "A Team UI for VictoriaLogs"
description: "vmui works well for one engineer running LogsQL. Logchef adds teams, scoped sources, saved queries, dashboards, live tail, and alerting on top of your existing VictoriaLogs."
pubDate: 2026-11-18
tags: ["victorialogs", "logsql", "observability", "logs", "self-hosted"]
author: "Logchef Team"
---

You moved your logs to VictoriaLogs. Ingestion is cheap, the single binary is easy to run, and LogsQL is pleasant once you learn the `field:=value` syntax. For the first few weeks, the built-in vmui is all you need: open the page, type a query, read the rows.

Then more people need the logs. The on-call engineer at 3 a.m. wants the query someone wrote last month to find gateway timeouts, but it lived in that person's browser history. Someone asks for an alert when checkout errors cross a threshold, and the answer is "set up vmalert and write a rules file." vmui has no users, no teams, and no per-source access control. Access is whatever the URL allows.

That is the gap [Logchef](https://github.com/mr-karan/logchef) fills. Logchef is a single-binary log analytics UI that started on ClickHouse. It now treats VictoriaLogs as a first-class datasource: you point it at your VictoriaLogs API, and it adds a query bar, field sidebar, saved queries, dashboards, live tail, alerting, and team-based access control. Your logs stay in VictoriaLogs. Logchef does not ingest or copy them.

## Connecting a source

Logchef talks to the VictoriaLogs HTTP API. As a global admin, open **Sources**, choose **Add Source**, select **VictoriaLogs**, and fill in:

- **Base URL**, for example `http://victorialogs:9428`. It must be reachable from the Logchef process.
- **Auth mode**: `none`, `basic` (username and password), or `bearer` (a token sent as `Authorization: Bearer <token>`).
- **Account ID** and **Project ID**, if you run multi-tenant VictoriaLogs. Both must be set together and both must be numeric. Logchef sends them as `AccountID` and `ProjectID` headers on every request.
- **Immutable Scope Query**, optional, covered below.
- **Timestamp Field** stays `_time` for a normal setup. Set **Severity Field** to whatever your pipeline emits, such as `level`.

When you save, Logchef checks `/health`, runs field discovery, and runs a one-row query with your auth, tenant headers, and scope applied. A wrong base URL, bad credentials, or a proxy that allows metadata calls but blocks queries fails at save time instead of on the first real search.

If you prefer config files, the same source can be declared in a provisioning file and reconciled at startup:

```toml
manage_sources = true
manage_teams = true

[[sources]]
name = "Payments Logs"
source_type = "victorialogs"
meta_ts_field = "_time"
meta_severity_field = "level"
secret_ref = "LOGCHEF_VL_PROD_TOKEN"

[sources.connection]
base_url = "https://logs.example.com"

[sources.connection.auth]
mode = "bearer"

[sources.connection.tenant]
account_id = "12"
project_id = "34"

[sources.connection.scope]
query = "{app=\"payments\"}"

[[teams]]
name = "Payments"
sources = ["Payments Logs"]
members = [
  { email = "alice@example.com", role = "admin" },
  { email = "bob@example.com", role = "member" },
]
```

The token comes from the `LOGCHEF_VL_PROD_TOKEN` environment variable, so it never sits in the file. Point `config.toml` at the file with `[provisioning] file = "provisioning.toml"`, and start with `dry_run = true` to see what Logchef would change before it changes anything.

## Scoping: one cluster, many narrow sources

The scope query is the piece that makes VictoriaLogs workable for several teams. A source can be pinned to a stream filter such as `{app="payments"}` or to a plain filter such as `kubernetes.namespace:="prod"`. Logchef adds it server side to every request for that source: explore queries, histograms, field-value lookups, live tail, and alert evaluation. A stream filter goes out as `extra_stream_filters`, a plain filter as `extra_filters`. Nothing typed in the query editor can remove it.

So one VictoriaLogs cluster can back several Logchef sources, each pinned to one app, namespace, or tenant. You then assign each source to a team under **Access > Teams**. Creating a source does not grant every team access; team membership only exposes the sources assigned to that team. Teams have `admin`, `editor`, and `member` roles, and team admins can manage their own members without being global admins. Users sign in through OIDC or, if enabled, local accounts.

## Querying: LogchefQL or native LogsQL

Each VictoriaLogs source supports two query languages, switchable per query.

**LogchefQL** is Logchef's filter syntax, the same one used on ClickHouse sources. On a VictoriaLogs source it compiles to LogsQL before it runs:

| LogchefQL | Generated LogsQL |
|---|---|
| `level = "error"` | `level:="error"` |
| `level != "error"` | `NOT level:="error"` |
| `status_code >= 500` | `status_code:>=500` |
| `_msg ~ "timeout"` | `_msg:~"(?i)timeout"` |
| `service = "api" and env = "prod"` | `(service:="api") AND (env:="prod")` |

Two details matter here. The `~` operator is a literal, case-insensitive substring match: Logchef escapes regex characters in your text, so `_msg ~ "user[1]"` searches for the literal `user[1]`. And a projection such as `service = "api" | _msg service` becomes a LogsQL `fields` pipe with the timestamp field always included first, so results never lose their time column.

Since v2.8.0, LogchefQL also counts and groups. The [aggregation pipe](/guide/aggregations/) `level=error | count() by service` compiles to `level:="error" | stats by (service) count()` plus a `sort` by the count, and the Explorer shows the result as a table or a chart.

**Native LogsQL** is there when you need VictoriaLogs pipes such as `stats`, `facets`, `unpack_json`, or `fields`. Anything valid in LogsQL runs unchanged:

```text
service:="api" | unpack_json from extra_fields | fields _time, _msg, user_id
```

In native mode, Logchef sends the time picker range as `start` and `end` parameters, so you don't add your own `_time:` filter. A query with no pipes, or only `fields` pipes, gets `| sort by (_time desc)` appended so results come back newest first. A query with `stats`, its own `sort`, or any other pipe runs as written. A row limit always applies, capped at the source's configured maximum, and Logchef shows a banner when it applies one.

## Live tail

The explorer's **Live** toggle proxies VictoriaLogs' native `/select/logsql/tail` endpoint, so new rows are pushed as they arrive instead of polled. If VictoriaLogs restarts or the connection drops, Logchef reports a lost connection rather than a normal stop, so you can tell the two apart. Scope and tenant headers apply to the tail like any other request.

## Large sources: windowed search

On a source with billions of rows, a wide time range can be slow to answer in one request. Since 2.4.0, VictoriaLogs sources have an opt-in **windowed search**. Logchef splits the time range into windows, searches newest first, and shows partial results as each window completes. **Load older** continues the same query, and a slow or failed window can be skipped and retried on its own. Background counts fill in totals and the histogram.

Enable it in the source form or in provisioning:

```toml
[sources.connection.optimizer]
enabled = true
max_window_seconds = 10800
concurrency = 2
schema_lookback_seconds = 300
stream_fields = ["service"]
```

Filter-only queries use windowed search. Queries with pipes keep the standard path so their ordering and aggregations stay correct. With the optimizer on, schema discovery looks back five minutes by default, so raise `schema_lookback_seconds` if rare fields are missing from the field list.

## Saved queries, dashboards, and alerts

Queries in either language can be saved to your library or to a shared [collection](/features/collections/), so the gateway-timeout query is one click away for whoever is on call.

[Dashboards](/features/dashboards/) support time series, stat, breakdown, and table panels on VictoriaLogs. Each panel picks its own team and source, so one dashboard can put a VictoriaLogs error rate next to a ClickHouse-backed request table. A team can have sources of any type, and the same LogchefQL filter works on each.

[Alerts](/features/alerting/) come in two modes. **Condition mode** takes a LogchefQL filter and an aggregate such as `count`. Logchef stores the rule as one LogchefQL aggregation and compiles it to LogsQL at every evaluation:

```text
level = "error" | count() as value by service
```

**Native mode** takes LogsQL that returns a numeric `value`, one row per group:

```text
level:="error" | stats by (service) count() as value
```

Logchef evaluates it through VictoriaLogs' `stats_query` endpoint and applies the alert's lookback window, so the query holds only the filter and the stats. With **Group by** set to `service`, each service fires and resolves on its own. Notifications go to reusable email, webhook, Slack and PagerDuty destinations.

## What this doesn't do

Logchef does not ingest logs. You keep shipping to VictoriaLogs with Vector, Fluent Bit, or whatever you use today. Two features are not available on VictoriaLogs: log context (jumping from a row to the logs around it) and exporting a full result set to CSV or NDJSON. `null` comparisons in LogchefQL are rejected on VictoriaLogs sources rather than guessed. And if you are debugging VictoriaLogs itself (ingestion, storage, retention), vmui is still the right tool.

## Try it

Logchef ships as a single Go binary with SQLite for metadata. The [VictoriaLogs guide](https://logchef.app/tutorials/victorialogs/) walks through running VictoriaLogs locally, ingesting sample logs, and adding the source. The [LogsQL Queries](https://logchef.app/guide/logsql-queries/) page covers native mode and the full LogchefQL mapping. Source and the Docker quick start are at [github.com/mr-karan/logchef](https://github.com/mr-karan/logchef).
