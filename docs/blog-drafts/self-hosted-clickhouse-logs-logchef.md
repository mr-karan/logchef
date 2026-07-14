---
title: "Self-Hosted Log Analytics: Pairing ClickHouse with LogChef Instead of a SaaS Log Bill"
description: "ClickHouse wins on log cost and speed. LogChef adds the query UI, RBAC, and alerting to run it self-hosted. An honest tradeoff breakdown."
pubDate: 2026-07-15
tags: ["clickhouse", "observability", "self-hosted", "cost", "log-management"]
author: "LogChef Team"
---

Every team that ships logs to a SaaS observability platform eventually has the same conversation. Someone opens the monthly invoice, notices it has grown faster than the business it's monitoring, and asks why. The usual answer is ingest volume: logs are verbose, retention windows are generous by default, and per-GB or per-host pricing scales with exactly the thing you can't easily control — how much your systems talk.

The reflexive fixes are all bad. Sample less and lose the log line you need during the next incident. Cut retention and lose the ability to investigate anything older than a week. Push back on developers to log less, which works until the next outage makes everyone log more "just in case." None of these fix the underlying issue: you're paying a managed-service premium for a workload — indexed text storage — that has gotten dramatically cheaper to run yourself.

This is the context behind a trend that's hard to miss if you follow the observability space: **ClickHouse has become the default answer for log storage at scale.** It wasn't built for logs specifically, but its columnar layout, aggressive compression, and cheap full scans turn out to be a very good fit for "store a lot of semi-structured text, filter it fast, throw most of it away after N days." Companies running serious log volume (and several observability vendors themselves) have converged on ClickHouse or ClickHouse-like engines under the hood.

The catch: ClickHouse gives you a fast table, not a log platform. There's no login page, no query builder, no saved searches, no alerting, no access control between teams. You get a `clickhouse-client` prompt and a schema you have to design yourself. That gap, between "we have a fast place to put logs" and "our on-call engineer can search them in 10 seconds at 3am," is where most self-hosted ClickHouse-for-logs projects stall. It's also the gap [LogChef](https://github.com/mr-karan/logchef) is built to close.

## Why ClickHouse for logs

The case for ClickHouse as a log backend rests on a few properties that are easy to verify yourself rather than take on faith:

- **Compression and columnar storage.** Log fields are repetitive: the same `service_name`, the same `severity_text`, the same handful of status codes, which is exactly what columnar storage with per-column codecs is good at collapsing.
- **Cheap full scans.** Unlike inverted-index systems that pay an indexing cost on write, ClickHouse's `MergeTree` engine leans on partition pruning, sparse primary key indexes, and (optionally) skip indexes like bloom filters, and just scans fast. For log workloads where queries are unpredictable and ad hoc, that's a reasonable trade against a heavier, slower-to-write inverted index.
- **Native TTL and partition-based retention.** `TTL ... DELETE` combined with `PARTITION BY toDate(timestamp)` means old data drops out cheaply: ClickHouse can discard whole partitions instead of rewriting files, which is what keeps a 30- or 90-day retention window affordable.
- **You already run it.** A lot of teams adopting this pattern already operate ClickHouse for product analytics or metrics. Logs become one more table on infrastructure you're already staffed to run, rather than a new managed product with its own bill.

None of this is unique to logs. It's the same reasoning that makes ClickHouse attractive for any high-volume, append-mostly, time-partitioned dataset. Logs just happen to fit the pattern unusually well.

## What you actually give up going self-hosted

This is the part vendors don't spend much time on, so it's worth being direct: self-hosting isn't a free upgrade. You are trading a subscription for an operational obligation.

- **You own schema design.** ClickHouse doesn't know what a "log" is. Deciding which fields get their own typed columns versus a catch-all `Map`, what to index, and how to partition is your job: get it wrong and queries get slow or the table becomes hard to change later. (There's a documented [OpenTelemetry-shaped schema](https://logchef.app/integration/schema-design/) that most teams can start from, but it's a starting point, not a substitute for understanding your own log shapes.)
- **You own retention and disk.** TTLs don't manage themselves. Someone has to size disks, watch for a TTL misconfiguration silently filling a volume, and plan for growth. There's no "unlimited retention" tier to fall back on; the tier is however much disk you provisioned.
- **You own the ingest pipeline.** Something has to parse, transform, and ship logs into ClickHouse — typically [Vector](https://vector.dev) or an OpenTelemetry Collector. That's another moving piece to configure, monitor, and keep compatible with your table schema.
- **You own upgrades, backups, and cluster health.** Replication, sharding once you outgrow a single node, coordination, backup/restore drills: all of it lands on whoever runs the cluster. SaaS abstracts this; self-hosting doesn't.
- **You own the on-call burden for the logging system itself.** If ClickHouse goes down, so does your ability to debug why anything else went down.

If your team doesn't already run ClickHouse and doesn't want a new stateful system to operate, the SaaS bill might be the correct trade: you're buying out exactly this list. Self-hosting makes sense once you already have the operational muscle (or are willing to build it), and a managed platform's recurring cost has grown large enough to justify the fixed cost of owning infrastructure instead.

**An illustrative (not a quote) way to think about the trade:** SaaS log platforms commonly price by ingested or indexed GB per day, so the bill scales roughly linearly with volume regardless of how much you actually query. Self-hosted ClickHouse cost is dominated by disk and compute for your retention window (storage that, thanks to compression, is often a small fraction of raw ingested bytes) plus the fixed engineering time to run it. Whether that nets out cheaper depends entirely on your volume, retention needs, existing ClickHouse footprint, and how you value engineering time. There's no universal multiplier here, and anyone quoting one without your numbers is guessing.

## What LogChef adds on top

Assume you've made the call: logs live in ClickHouse (or [VictoriaLogs](https://victorialogs.io), which LogChef also supports as a backend). You still need the layer between "raw table" and "usable system." That's the actual product surface of LogChef: a single Go binary that runs as a query and control plane on top of your existing datasource, without taking ownership of ingestion or storage.

**Query-first exploration with LogchefQL.** Rather than writing SQL for every search, LogchefQL gives you a compact filter syntax that compiles to native SQL (or LogsQL for VictoriaLogs sources):

```
severity_text="ERROR" and service_name="payment-api" and log_attributes.request.method="POST"
```

That expression resolves nested `Map`/JSON fields with dot notation, supports `and`/`or`/parentheses, and a pipe operator to select specific columns (`level="error" | timestamp body log_attributes.request_id`) instead of pulling `SELECT *`. When you need more (aggregations, joins, window functions), you switch to native SQL mode and get the full expressiveness of ClickHouse directly, without any abstraction layer in the way.

**Saved queries and collections.** Queries can be saved to a personal library or shared "collections" (think on-call runbooks or a team's go-to incident queries) with Member/Editor/Owner roles controlling who can run, edit, or manage membership. Collection access is layered on top of, not instead of, source access: being in a collection never grants access to logs you couldn't already query.

**Team-based RBAC.** Sources (a ClickHouse table, or a VictoriaLogs connection) are assigned to teams, and users only see and query sources their teams can reach. Auth is OIDC/SSO or built-in email+password, so you're not required to stand up an identity provider just to try it.

**Real-time alerting without a separate tool.** Alerts run on a schedule against a LogchefQL condition or native SQL/LogsQL, with configurable thresholds, lookback windows, and evaluation frequency. Notifications go out over SMTP email or generic webhooks, which means they work with Slack incoming webhooks or PagerDuty without LogChef needing bespoke integrations for either.

**Scoped service tokens for automation.** Log shippers, dashboards, or CI checks can authenticate with API tokens scoped to specific permissions (read-only, alerts-manager, source-admin, etc.) rather than a shared admin credential.

**Single binary, boring by default.** LogChef ships as one executable with no runtime dependencies, backed by SQLite for its own metadata (users, teams, saved queries, alert configs, not your logs) with zero configuration. If you need more than one replica for availability, Postgres is an opt-in metadata backend, though it's worth reading the fine print: alert evaluation still needs to run on exactly one replica until leader election ships, since Postgres coordinates shared state but not alert scheduling.

## A minimal architecture

The shape of a self-hosted setup looks like this:

```
   applications / services
            │
            ▼
   Vector or OTel Collector   (parse, transform, batch)
            │
            ▼
        ClickHouse            (MergeTree table, TTL retention,
                                partitioned by date, your schema)
            │
            ▼
         LogChef               (query UI, LogchefQL, RBAC,
                                 saved queries, alerting, CLI/API)
            │
            ▼
   engineers / on-call / CI
```

ClickHouse never sees LogChef as anything other than a SQL client with opinions. That matters for the "no lock-in" argument: if LogChef disappeared tomorrow, your logs are still sitting in a ClickHouse table you fully own, queryable with any client you like.

## The pragmatic middle path

This isn't a pitch that self-hosting is strictly better, or that LogChef makes the operational cost disappear. It's the opposite: self-hosting means real ownership of schema, retention, and uptime, and that's worth going in with eyes open. What ClickHouse changes is the economics of the storage layer: cheap, fast, and something many teams already run. What LogChef adds is the part that turns a fast table into something a team can actually use under pressure: a query language people will use instead of hand-writing SQL, saved runbooks, access control between teams, and alerts that don't require standing up a separate system.

If your logging bill is the thing prompting this conversation, or you're already committed to ClickHouse and tired of `clickhouse-client` as your incident-response tool, it's worth fifteen minutes to look at what a query-and-control-plane layer actually buys you. Try the [live demo](https://demo.logchef.app), read the [docs](https://logchef.app), or go straight to the [source on GitHub](https://github.com/mr-karan/logchef): it's AGPLv3, single binary, and `docker compose up -d` gets a full stack (LogChef, ClickHouse, and sample data) running locally in a few minutes.
