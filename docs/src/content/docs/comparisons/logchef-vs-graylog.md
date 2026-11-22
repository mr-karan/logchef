---
title: Logchef vs Graylog
head:
  - tag: title
    content: "Logchef vs Graylog"
description: "Logchef vs Graylog: an open-source Graylog alternative for logs in ClickHouse, VictoriaLogs or Loki. Compare licenses, Graylog Open limits, storage and queries."
---

Graylog is a complete log management system: it receives logs, parses and
routes them, stores them in OpenSearch, and searches them. Logchef is only the
last part. It is a query UI over ClickHouse, VictoriaLogs or Grafana Loki, and
something else has to collect and store the logs. Logchef is a Graylog
alternative if you are happy to run that pipeline yourself and want SQL, SSO,
team access control and an audit log without a commercial license.

## The short version

Pick **Graylog** if you want one product that handles inputs, parsing, routing,
storage and search, or if you need its security tooling. Expect to buy
Graylog Enterprise or Graylog Security once you need OIDC or SAML SSO, teams, archiving or
audit logs.

Pick **Logchef** if your logs are in ClickHouse, VictoriaLogs or Loki (or will be),
you already have a collector such as Vector or the OpenTelemetry Collector, and
you want a light query layer with everything in the open-source release.

## At a glance

| | Logchef | Graylog |
|---|---|---|
| What it is | Query UI and control plane over log storage you already run | Log management platform: ingest, process, store, search ([docs](https://go2docs.graylog.org/current/what_is_graylog/what_is_graylog.htm)) |
| License | AGPLv3 | Graylog Open: [SSPL v1](https://github.com/Graylog2/graylog2-server#license). Enterprise and Security: commercial license |
| Storage | None of its own. Reads ClickHouse tables, VictoriaLogs and [Loki](/tutorials/loki/) | Graylog Data Node or self-managed OpenSearch for log data, MongoDB for metadata ([architecture](https://go2docs.graylog.org/current/planning_your_deployment/graylog_system_architecture.htm)) |
| Ingestion | None. Your collectors write to the backend | Owns it: [inputs](https://go2docs.graylog.org/current/getting_in_log_data/inputs.htm) for Syslog, GELF, Beats, OpenTelemetry and more, then streams and pipelines |
| Query languages | [LogchefQL](/guide/search-syntax/), ClickHouse SQL, [VictoriaLogs LogsQL](/guide/logsql-queries/), [Loki LogQL](/guide/logql-queries/) | Graylog search syntax, [based on Lucene query syntax](https://go2docs.graylog.org/current/making_sense_of_your_log_data/search_syntax_reference.htm) |
| Signals | Logs only | Logs and security events |
| Deployment | One Go binary, SQLite or [Postgres](/operations/database-backends/) for metadata, plus your backend | Graylog server, Data Node and MongoDB at minimum |
| Auth and RBAC | [OIDC SSO, local auth, team-scoped source access](/core/user-management/) and an [audit log](/operations/audit-log/) in the open-source release | Open: local users, roles, LDAP and Active Directory. OIDC, SAML, Okta and teams need Enterprise |
| Alerting | [Scheduled alerts](/features/alerting/) with email, webhook, Slack and PagerDuty destinations | Event definitions and notifications. Correlation and chat or paging notifications are Enterprise |
| Dashboards | [Built in](/features/dashboards/): time series, stat, breakdown, table. [Scheduled email reports](/features/scheduled-reports/) | Built in. Scheduled reports and dashboard parameters are Enterprise |
| AI and MCP | [AI query generation](/features/ai-sql-generation/), read-only [MCP server](/integration/mcp-server/) | [MCP tools](https://go2docs.graylog.org/current/setting_up_graylog/model_context_protocol__mcp__tools.htm) in Graylog 7.x, [off by default](https://go2docs.graylog.org/current/setting_up_graylog/configure_mcp.htm), with API token auth |
| Pricing | Free. You pay for the infrastructure you run | Open is free. Enterprise, Security and Cloud are licensed ([pricing](https://graylog.org/pricing/)) |

Graylog facts are from its documentation as of November 2026 (Graylog 7.x).

## How they differ

### Graylog processes logs. Logchef does not.

This is Graylog's main strength and the biggest gap to plan for. Graylog
listens for logs on inputs (Syslog, GELF, Beats, OpenTelemetry over gRPC, raw
text and others, per its [inputs docs](https://go2docs.graylog.org/current/getting_in_log_data/inputs.htm)).
Streams route messages, and pipeline rules parse, transform and enrich them
before they are stored. A network device that speaks only syslog can send
straight to Graylog.

Logchef has no ingestion at all. A collector such as
[Vector](/integration/vector/) or the
[OpenTelemetry Collector](/integration/otel-collector/) receives, parses and
writes logs to ClickHouse, VictoriaLogs or Loki, and Logchef reads the result. If
your team already runs such a collector, nothing is missing. If Graylog's
inputs and pipelines are your collector today, you are replacing two things,
not one.

### Storage

Graylog stores log data in OpenSearch, either through its own Data Node or a
cluster you manage, and keeps configuration and users in MongoDB. Its
[architecture docs](https://go2docs.graylog.org/current/planning_your_deployment/graylog_system_architecture.htm)
describe the smallest deployment as Graylog, Data Node and MongoDB.

Logchef stores only its own metadata, in SQLite by default or
[Postgres](/operations/database-backends/) for more than one replica. Logs stay
in ClickHouse, a columnar database with typed columns and SQL, or in
VictoriaLogs or Loki. One Logchef instance can serve many sources across all
three backends.

### What the free edition includes

Graylog Open is free and its source is public, under the Server Side Public
License. The OSI [does not consider the SSPL an open source
license](https://opensource.org/blog/the-sspl-is-not-an-open-source-license),
which matters to some organizations and not to others.

Several features that teams ask for early are in the paid editions. Per
Graylog's docs:

- **SSO.** "Graylog Open supports Active Directory and LDAP integration."
  Enterprise "adds support for Okta as well as generic OpenID Connect (OIDC)
  and SAML authentication"
  ([security guide](https://go2docs.graylog.org/current/planning_your_deployment/secure_your_graylog_environment.htm)).
- **Audit logs.** The same guide marks user audit logging as an Enterprise
  feature that needs a valid license.
- **Teams, archiving, reporting, correlation, search parameters and search
  filters.** All are listed as Enterprise additions on the
  [Graylog Enterprise overview](https://go2docs.graylog.org/current/what_is_graylog/what_is_graylog_operations.html),
  along with Slack, Discord, Microsoft Teams, PagerDuty and script
  notifications.

Logchef has one edition under AGPLv3. OIDC sign-in, teams with per-source
access, [service tokens](/features/service-tokens/),
[shared collections of saved queries](/features/collections/), an
[audit log](/operations/audit-log/),
[scheduled email reports](/features/scheduled-reports/) and
[alert notifications](/features/alerting/) by email, webhook, Slack and
PagerDuty are all in it. One caveat in Graylog's
favor: Logchef does not bind to LDAP directly. It needs an OIDC provider (which
can itself sit in front of LDAP) or its built-in local accounts.

### Query languages

Graylog's search syntax is based on Lucene query syntax: field matches, boolean
operators, wildcards and ranges. Its
[syntax reference](https://go2docs.graylog.org/current/making_sense_of_your_log_data/search_syntax_reference.htm)
covers filtering. The [widget documentation](https://go2docs.graylog.org/current/interacting_with_your_log_data/widgets.html)
describes UI aggregation widgets that group data, apply metrics, and sort results.

Logchef gives you [LogchefQL](/guide/search-syntax/) for filtering, with
[aggregation pipes](/guide/aggregations/) for counts, sums and percentiles.
For anything larger you write the backend's native language. On ClickHouse
that is full SQL, with `GROUP BY`, joins and window functions, and the same
query can back a [dashboard panel](/features/dashboards/) or an
[alert](/features/alerting/). Logchef can also
[generate the query from a plain-English question](/features/ai-sql-generation/).

### Security use cases

Graylog sells a security product. Graylog Security builds threat detection,
investigation and response on top of Enterprise, and Illuminate content packs
ship parsing rules for common sources. Logchef has none of this. It is a log
search and analytics tool, not a SIEM.

## When Graylog is the better choice

- You need **inputs and pipelines** in the same product, especially for
  syslog-only devices and network gear.
- You want **SIEM features**: correlation, security content and investigations.
- Your organization requires **direct LDAP or Active Directory** login without
  an OIDC provider.
- You want **archiving** and long-term retention managed by the product, and
  you are ready to license Enterprise for it.
- Your team already runs OpenSearch and knows Lucene-style queries.

## When Logchef is the better choice

- Your logs are in **ClickHouse, VictoriaLogs or Loki**, or you want that
  storage for cost or query speed.
- You need **OIDC SSO, team-based access, audit logs, scheduled reports, or
  Slack and PagerDuty notifications** without buying a license.
- You want **SQL** over logs, and joins against other tables.
- You want fewer moving parts in the query layer: one binary and a SQLite
  file, with [TOML provisioning](/getting-started/provisioning/) and
  [Prometheus metrics](/operations/metrics/).
- You want a [CLI](/integration/cli/) and an OAuth-protected, read-only
  [MCP server](/integration/mcp-server/) that follow the same team permissions
  as the web UI.
- You need an OSI-approved license.

## Migrating from Graylog

There is no dedicated Graylog migration guide, and Logchef cannot read
Graylog's OpenSearch indices. Plan the move in two parts.

**Collection and parsing.** Replace Graylog inputs and pipeline rules with a
collector. [Vector](/integration/vector/) handles syslog, files and container
logs and can parse and reshape events. The
[OpenTelemetry Collector](/integration/otel-collector/) covers OTLP sources.
Most shippers can send to two destinations, so you can run Graylog and the new
path side by side.

**Storage and UI.** Create a ClickHouse table with the
[schema design guide](/integration/schema-design/) or stand up
[VictoriaLogs](/tutorials/victorialogs/), then follow the
[quickstart](/getting-started/quickstart/) and add it as a source. Rebuild
saved searches as [saved queries](/features/collections/) and event
definitions as [alerts](/features/alerting/). Old data stays in Graylog until
its retention expires.

## FAQ

### Is Logchef a Graylog alternative?

For searching, dashboarding and alerting on logs, yes, with ClickHouse,
VictoriaLogs or Loki as storage and your own collector. It does not replace Graylog's
inputs, pipelines or security features.

### Is Graylog open source?

Graylog Open's source is public under SSPL v1, which the OSI does not classify
as open source. Logchef is AGPLv3, which is OSI-approved.

### Does Graylog Open support SSO?

Graylog's docs list Active Directory and LDAP for Graylog Open. OIDC, SAML and
Okta are Enterprise features. Logchef includes OIDC SSO in its only edition.

### Can Logchef read Graylog's OpenSearch data?

No. Logchef supports ClickHouse, VictoriaLogs and Grafana Loki sources.

### Does Logchef accept syslog or GELF?

No. Logchef has no inputs. Use a collector such as Vector to receive those
protocols and write to ClickHouse, VictoriaLogs or Loki.

### What databases does each one need?

Graylog needs OpenSearch (usually through Data Node) and MongoDB. Logchef needs
the log backend you choose, plus an embedded SQLite file or an optional
Postgres database for its metadata.

Ready to try it? Start with the [quickstart](/getting-started/quickstart/).
