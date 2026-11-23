---
title: "Grafana Loki UI and Log Analytics in v2.8"
description: "Logchef v2.8.0 adds Grafana Loki as a third backend beside ClickHouse and VictoriaLogs, LogchefQL aggregations, per-group alerts, an audit log of query runs, and a rebuilt Query Activity page."
pubDate: 2026-11-22
tags: ["release", "loki", "clickhouse", "victorialogs", "alerting", "audit"]
author: "Logchef Team"
---

Logchef v2.8.0 is out. It reads Grafana Loki, so one self-hosted UI now covers logs in ClickHouse, VictoriaLogs and Loki. LogchefQL can count and group. Alerts fire per service instead of per rule. The queries people and agents run are recorded, and a rebuilt Query Activity page shows who uses Logchef and which queries cost the most.

By code changed, this is the largest Logchef release so far: 171 commits since v2.7.0, with 18 database migrations on each metadata store. This post walks through what you can do now. The [changelog](https://github.com/mr-karan/logchef/blob/main/CHANGELOG.md) has every detail, and the [upgrade notes](#upgrading) at the end matter: this release needs downtime.

## Grafana Loki as a third backend

If your logs are in Loki, you can now put Logchef in front of it. Logchef treats Loki the way it treats ClickHouse and VictoriaLogs: a source that teams query, with saved queries, dashboards, alerts and team access on top. Logchef reads Loki's HTTP API and never writes to Loki.

A global admin adds a Loki source with a base URL, an optional tenant (sent as `X-Scope-OrgID`), a default stream selector and a parser (none, JSON or logfmt). Then you search it two ways.

LogchefQL compiles to LogQL. A condition on a stream label joins the selector, a condition on `_msg` becomes a line filter, and everything else becomes a label filter after the parser:

```text
level="error"
```

On a source whose selector is `{service_name=~".+"}` with the JSON parser, that becomes:

```text
{service_name=~".+", level="error"} | json | drop __error__, __error_details__
```

The **LogQL** tab runs native LogQL as written. Logchef sends the time range as Loki's `start` and `end`, and reads past Loki's per-request entry limit in pages, up to the row limit you pick. A metric query comes back as rows of `value`, `time` and one column per label.

The rest of the Explorer works too:

- The histogram counts entries per bucket.
- The field sidebar preloads the values of up to 30 stream labels in one request.
- Live tail polls `query_range` every 2 seconds instead of using Loki's `/tail` WebSocket, which is limited to ten tails per tenant.
- **Show Context** lists the lines before and after a row in that row's own stream.

Loki sources also work in saved queries, dashboards, alerts, AI query generation, the CLI and the MCP server.

<figure class="post-clip">
<video controls muted loop playsinline preload="none" poster="/releases/v2.8.0/loki-explorer.png" width="1280" height="720" aria-label="A LogchefQL filter on a Grafana Loki source: the histogram, the field sidebar, the compiled LogQL, log context and live tail.">
<source src="/releases/v2.8.0/loki-explorer.webm" type="video/webm" />
<source src="/releases/v2.8.0/loki-explorer.mp4" type="video/mp4" />
</video>
<figcaption>A LogchefQL filter on a Grafana Loki source: the histogram, the field sidebar, the compiled LogQL, log context and live tail.</figcaption>
</figure>

One rule matters before you link a Loki source to a team: the team gets the whole tenant. Native LogQL can name any stream, so the default selector is a convenience, not a boundary. The [Grafana Loki tutorial](/tutorials/loki/) covers setup and every difference, and [LogQL Queries](/guide/logql-queries/) has the full LogchefQL mapping. Exports, windowed search and LogchefQL aggregations are not available on Loki yet.

## LogchefQL counts and groups

Until now, LogchefQL found rows. To count them you switched to SQL or LogsQL, and wrote it differently for each backend. In v2.8.0 a pipe after the filter aggregates:

```text
level=error | count() by service
```

```text
service=api | p95(duration_ms), avg(duration_ms) by time(5m)
```

The functions are `count`, `count_distinct`, `sum`, `avg`, `min`, `max` and the approximate percentiles `p50` to `p99`, with up to 5 `by` keys and optional `| sort` and `| limit` stages. The same text compiles to ClickHouse SQL on a ClickHouse source and to a LogsQL `stats` pipe on a VictoriaLogs source.

The Explorer shows the result as a table or a chart. **Add to dashboard** opens the panel builder on a dashboard you can edit, with a result panel filled in from the query. The [Aggregations](/guide/aggregations/) page covers the syntax, the limits and the differences between backends.

<figure class="post-clip">
<video controls muted loop playsinline preload="none" poster="/releases/v2.8.0/aggregations.png" width="1280" height="720" aria-label="A pipe after the filter counts and groups, shown as a table, as a chart, then added to a dashboard.">
<source src="/releases/v2.8.0/aggregations.webm" type="video/webm" />
<source src="/releases/v2.8.0/aggregations.mp4" type="video/mp4" />
</video>
<figcaption>A pipe after the filter counts and groups, shown as a table, as a chart, then added to a dashboard.</figcaption>
</figure>

## Alerts that fire per group

A v2.7 alert rule produced one value. To watch checkout and search separately, you wrote two rules. In v2.8.0 a rule can group:

```text
status_code >= 500 | count() as value by service
```

With **Group by** `service`, each service fires, notifies and resolves on its own, and the rule's page lists every group with its state. Native queries group too: a ClickHouse `GROUP BY service`, a LogsQL `stats by (service)`, or a LogQL `sum by (service)`.

The rest of alerting changed with it:

- **No data.** Each rule decides what an empty result means: compare 0 with the threshold, open a no-data episode, or keep the current state.
- **Silences.** A silence mutes notifications for one rule or all of a source's rules, once for up to 30 days or every week for maintenance windows. Evaluation and history continue.
- **Backtest.** **Run backtest** replays an unsaved rule over the last hour, 6 hours, 24 hours or 7 days, at up to 120 instants, and shows the notifications it would have sent. It writes nothing.
- **Destinations.** Email, webhook, Slack and PagerDuty targets are reusable destinations, private, shared with a team, or global. PagerDuty gets one `dedup_key` per group, so a resolve closes the incident its trigger opened.
- **Delivery queue.** Notifications retry after 30 seconds, 2, 10 and 30 minutes, and each history entry lists its deliveries with their state and last error.

Rules also run on a background job runner, so several replicas on Postgres no longer each evaluate every rule. The [Alerting](/features/alerting/) page covers all of it, including the upgrade from 2.7.

<figure class="post-clip">
<video controls muted loop playsinline preload="none" poster="/releases/v2.8.0/alerting-v2.png" width="1280" height="720" aria-label="One rule grouped by region, backtested over 24 hours, with each group firing and silenced on its own.">
<source src="/releases/v2.8.0/alerting-v2.webm" type="video/webm" />
<source src="/releases/v2.8.0/alerting-v2.mp4" type="video/mp4" />
</video>
<figcaption>One rule grouped by region, backtested over 24 hours, with each group firing and silenced on its own.</figcaption>
</figure>

## Queries on the record

Logchef controls who can read which logs. Until now, the record of who read what was each user's query history and a recent sample on Query Activity. The new audit log records configuration changes, sign-ins, OAuth consent, exports and query runs.

Each `query.run` event holds the full query text, the requested time range, the rows returned and scanned, the duration and the outcome. That covers queries from the Explorer, dashboards, the CLI and the API, and also each live tail session, each log context read, and each MCP or Ask Chef tool call that reads logs. An AI agent's queries are recorded under the user it acts for, with the OAuth client that sent them. The histogram and the field sidebar don't record events yet.

Global admins browse events in **Administration → Audit Log**, read them through the API, or filter them from the terminal:

```bash
logchef audit list --action query.run --since 24h
```

Each event is also written as a JSON log line, so you can ship it off the host for longer retention. Credentials are recorded only as "changed". The [Audit Log](/operations/audit-log/) page lists every action and the retention settings.

**Administration → Query Activity** is rebuilt on top of the same data. It answers three questions: who uses Logchef, which sources are busy or slow, and which repeated query patterns cost the most. Every number compares with the window of the same length before it, with latency percentiles, scan cost and failure rates. Click a source or a user to scope the whole page to it.

<figure class="post-clip">
<video controls muted loop playsinline preload="none" poster="/releases/v2.8.0/audit-log.png" width="1280" height="720" aria-label="The audit log with a query an agent ran over MCP, opened to its full text, rows, duration and OAuth client.">
<source src="/releases/v2.8.0/audit-log.webm" type="video/webm" />
<source src="/releases/v2.8.0/audit-log.mp4" type="video/mp4" />
</video>
<figcaption>The audit log with a query an agent ran over MCP, opened to its full text, rows, duration and OAuth client.</figcaption>
</figure>
<figure class="post-clip">
<video controls muted loop playsinline preload="none" poster="/releases/v2.8.0/query-activity.png" width="1280" height="720" aria-label="Query Activity ranks sources by query time and scopes the whole page to one source with a click.">
<source src="/releases/v2.8.0/query-activity.webm" type="video/webm" />
<source src="/releases/v2.8.0/query-activity.mp4" type="video/mp4" />
</video>
<figcaption>Query Activity ranks sources by query time and scopes the whole page to one source with a click.</figcaption>
</figure>

## Reports, views and shared dashboards

- **Scheduled reports.** Email a dashboard, one dashboard tab or a saved query every hour, day, week or month. Charts become bar tables in the email, and a saved query can attach its rows as CSV. Each run checks the owner's access, and each Logchef recipient's copy locks the panels that recipient can't see. See [Scheduled Reports](/features/scheduled-reports/).
- **Saved Explorer views.** Save a source's columns, widths, sort and display mode, with an optional filter and time range, as a personal, team or source view. Each scope can set a default, and a share link carries your layout. See [Saved Views](/features/saved-views/).
- **Shared dashboard editing.** Team owners, admins and editors can now edit a dashboard when they have edit rights on every team its query panels use. Only the creator and global admins can delete it. See [Dashboards](/features/dashboards/).

## A schema advisor for ClickHouse

ClickHouse rewards a table that fits its queries, and it is hard to know which queries you actually run. Logchef records the filter shape of each Explorer query on a ClickHouse source: the columns, Map keys and JSON paths, never the values. A global admin presses **Run analysis** on Source Inspection, and the advisor joins those shapes with the table's metadata.

Findings suggest bloom filter indexes, text indexes on ClickHouse 26.8 or later, materialized columns for frequently queried Map keys or JSON paths, `LowCardinality`, and codecs. Each finding shows its evidence, its expected benefit and cost, and any suggested DDL. Logchef never runs the DDL. You review it and apply it yourself. See [Schema Advisor](/features/schema-advisor/).

## MCP and the CLI

The [MCP server](/integration/mcp-server/) now has 22 read-only tools. Agents can query Loki sources, run aggregations through `query_logchefql`, list saved views, and see alert groups, silences and deliveries. Our [MCP launch post](/blog/ai-agents-query-logs-mcp-logchef/) shows an agent working through an incident.

CLI 0.5.0 adds `logchef advisor`, `logchef audit list`, `logchef reports`, `logchef views list` and `logchef open --view`. It also runs LogQL through `logchef sql` and prints aggregations as tables:

```bash
logchef query 'level=error | count() by service' --since 1h
logchef sql '{service_name="payments-api"} |= "gateway"' --since 1h
```

See the [CLI reference](/integration/cli/).

## Upgrading

Read this before you upgrade:

1. **Back up the metadata database.** Startup runs 18 migrations on each store. The alert migrations move each rule's targets into destinations and cannot be undone, so rolling back means restoring the backup.
2. **Stop every old replica before the first new one starts.** Alerting v2 does not support a rolling upgrade, so plan for downtime.
3. **Update scripts that manage alerts.** `recipient_user_ids` and `webhook_urls` are gone, and a request that contains either answers 400. Create a destination and pass its ID in `destination_ids`.
4. **Remove `alerts.evaluation_interval`.** Each rule now runs at its own frequency.
5. **Reissue API tokens that need the new scopes**, such as `destinations:read`, `saved_views:read` or `reports:read`.

The [upgrade notes](https://github.com/mr-karan/logchef/blob/main/CHANGELOG.md#upgrade-notes) list every migration, config key and API change.

## Try it

- Live demo: [demo.logchef.app](https://demo.logchef.app)
- Install: the [quick start](/getting-started/quickstart/), or the image `ghcr.io/mr-karan/logchef:v2.8.0`
- Loki: the [Grafana Loki tutorial](/tutorials/loki/)
- Source, issues and releases: [github.com/mr-karan/logchef](https://github.com/mr-karan/logchef)

If you're new here, three earlier posts cover the parts this release builds on: [letting an AI agent query your logs over MCP](/blog/ai-agents-query-logs-mcp-logchef/), [log dashboards without Grafana](/blog/log-dashboards-clickhouse-logchef/), and [a team UI for VictoriaLogs](/blog/victorialogs-ui-logchef/).
