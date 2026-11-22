---
title: Logchef vs Elasticsearch and Kibana
head:
  - tag: title
    content: "Logchef vs Elasticsearch and Kibana"
description: "Logchef vs Elasticsearch and Kibana (ELK): an open-source Kibana alternative for logs in ClickHouse, VictoriaLogs or Loki. Licenses, paid tiers and trade-offs."
---

Logchef is not a drop-in replacement for Elasticsearch. It is a log UI that
reads ClickHouse, VictoriaLogs or Grafana Loki, so using it as an ELK
alternative means moving log storage as well as swapping Kibana. If you use
Elasticsearch only to store logs and Kibana Discover to search them, that move
gets you SQL, single sign-on, an audit log and alert notifications with no paid
subscription. If you rely on Elastic for
APM, SIEM, machine learning or ranked search, stay on Elastic.

## The short version

Pick **Logchef** if your logs are in ClickHouse, VictoriaLogs or Loki (or you
are willing to put them there) and you want search, dashboards, alerts and team
access control from one self-hosted binary.

Pick **Elasticsearch and Kibana** if you need one platform for logs, metrics,
traces, security analytics and application search, or if relevance-ranked
full-text search matters to you.

**OpenSearch and OpenSearch Dashboards** sit on the Elasticsearch side of this
comparison. They started as a fork of Elasticsearch and Kibana and use the same
index-based storage model, so the architectural points below apply to them too.
The licensing and paid-tier points do not.

## At a glance

| | Logchef | Elasticsearch and Kibana |
|---|---|---|
| What it is | Query UI and control plane over log storage you already run | Search and analytics engine (Elasticsearch) with its own UI (Kibana) |
| License | AGPLv3 | Source available under AGPLv3, SSPL 1.0 or Elastic License 2.0, your choice. Files licensed solely under Elastic License 2.0 are found in `x-pack`; check each file header ([LICENSE.txt](https://github.com/elastic/elasticsearch/blob/main/LICENSE.txt)) |
| Storage | None of its own. Reads ClickHouse tables, VictoriaLogs and [Loki](/tutorials/loki/) | Its own indices, on nodes you size and shard |
| Ingestion | None. Your collectors write to the backend | Owns it: Elastic Agent, Beats, Logstash, OpenTelemetry intake |
| Query languages | [LogchefQL](/guide/search-syntax/), ClickHouse SQL, [VictoriaLogs LogsQL](/guide/logsql-queries/), [Loki LogQL](/guide/logql-queries/) | [KQL](https://www.elastic.co/docs/explore-analyze/query-filter/languages/kql), Lucene syntax, [ES\|QL](https://www.elastic.co/docs/explore-analyze/query-filter/languages/esql), Query DSL |
| Signals | Logs only | Logs, metrics, traces (APM), security events, search |
| Deployment | One Go binary, SQLite or [Postgres](/operations/database-backends/) for metadata, plus your backend | Elasticsearch cluster, Kibana, and shippers. Or Elastic Cloud |
| Auth and RBAC | [OIDC SSO, local auth, team-scoped source access](/core/user-management/) and an [audit log](/operations/audit-log/), all in the open-source release | Native users, roles and Spaces are free. SSO, LDAP and audit logging need a paid subscription |
| Alerting | [Scheduled alerts](/features/alerting/) with email, webhook, Slack and PagerDuty destinations | Basic includes alerting rule types. Email, Slack, webhook and PagerDuty connectors need a paid subscription |
| Dashboards | [Built in](/features/dashboards/): time series, stat, breakdown, table. [Scheduled email reports](/features/scheduled-reports/) | Kibana dashboards and Lens |
| AI and MCP | [AI query generation](/features/ai-sql-generation/) with your own model key, [built-in MCP server](/integration/mcp-server/) | Elastic AI Assistant and Agent Builder (with MCP) are Enterprise tier |
| Pricing | Free. You pay for the infrastructure you run | Free Basic tier, paid self-managed subscriptions, or usage-based [Elastic Cloud](https://www.elastic.co/pricing) |

Tier facts in this table come from Elastic's
[subscriptions page](https://www.elastic.co/subscriptions) as of November 2026.

## How they differ

### A search index versus an analytical database

Elasticsearch builds an inverted index for analyzed text and can rank matches
by relevance, as its [full-text search documentation](https://www.elastic.co/docs/solutions/search/full-text/how-full-text-works) explains.

Logchef leaves storage to ClickHouse, VictoriaLogs or Loki. ClickHouse is a columnar
database: logs are rows, fields are typed columns, and aggregations are plain
SQL. Text search in Logchef is substring and token matching, with no relevance
score. For "show me errors from the payment service in the last hour, grouped
by host", that is enough. For "find the documents most similar to this
phrase", it is not.

Elastic has narrowed the storage gap for logs with its logsdb index mode, which
is in the free Basic tier. One of its main space savings, synthetic `_source`,
is an Enterprise feature from 8.17 onward, per the
[subscriptions page](https://www.elastic.co/subscriptions).

### What you get without a subscription

This is the difference most self-hosting teams hit first. In the free Basic
tier, Elastic includes role-based access control, native and file-based users,
Kibana Spaces, Discover, dashboards and alerting rules. It does not include:

- **Single sign-on.** Elastic's SAML docs say it directly: "Elastic Stack SSO
  is a subscription feature"
  ([source](https://www.elastic.co/docs/deploy-manage/users-roles/cluster-or-deployment-auth/saml)).
  LDAP and Active Directory are paid too.
- **Alert notifications.** Basic includes the Server log and Index
  connectors. Email, Slack, webhook and PagerDuty connectors are in the paid
  tiers.
- **Audit logging** and **field- and document-level security.**

Logchef has no paid tier. [OIDC single sign-on](/core/user-management/),
team-scoped access to sources, [service tokens](/features/service-tokens/), an
[audit log](/operations/audit-log/) and
[email, webhook, Slack and PagerDuty alert destinations](/features/alerting/)
are in the open-source binary. Logchef grants access per source and has no
field- or document-level security.

### The license history

Elasticsearch and Kibana offered Apache 2.0 distributions until 2021. From version 7.11 the
source moved to a dual SSPL and Elastic License model, and Elastic stopped
shipping an Apache 2.0 distribution
([licensing FAQ](https://www.elastic.co/pricing/faq/licensing)). In August 2024
Elastic [announced](https://www.elastic.co/blog/elasticsearch-is-open-source-again)
AGPLv3 as a third option, so the source is open source again by the OSI
definition. The paid features stay under the Elastic License.

OpenSearch took the other path. It was
[announced in January 2021](https://opensearch.org/about/) as a fork, is
licensed under Apache 2.0, and is now a
[Linux Foundation project](https://opensearch.org/foundation/). Its security
plugin supports [SAML, OpenID Connect and LDAP](https://docs.opensearch.org/latest/security/authentication-backends/authc-index/)
without a subscription, so the paid-tier gap above is an Elastic point, not an
OpenSearch one.

### Query languages

Kibana's default language, KQL, is a filter language. Aggregations happen in
Lens, or in ES|QL, Elastic's newer piped language.

Logchef has the same split with different tools.
[LogchefQL](/guide/search-syntax/) is a short filter syntax
(`level="error" and status>=500`) that also handles
[counts, sums and percentiles](/guide/aggregations/). When you need more, you
switch to the backend's native language: ClickHouse SQL with joins and window
functions, LogsQL on VictoriaLogs, or LogQL on Loki. Logchef accepts a single ClickHouse `SELECT`, expands its
[time macros](/guide/time-macros/), and applies result limits. Queries that use
Logchef macros need those placeholders expanded before use in `clickhouse-client`.

### Scope

Elastic is a platform: Observability (logs, metrics, APM), Security (SIEM,
endpoint) and Search are all built on the same cluster. Logchef does logs.
It has no traces UI, no APM, no metrics store and no SIEM rules. It also does
not ingest anything.

## Using them together

Logchef cannot query Elasticsearch or OpenSearch. The two run side by side, not
stacked. A common split is to keep Elasticsearch for product search, APM or
SIEM, and move high-volume application logs to ClickHouse or VictoriaLogs with
Logchef on top.

If you want to keep Filebeat or Logstash, VictoriaLogs accepts the
[Elasticsearch bulk API](https://docs.victoriametrics.com/victorialogs/data-ingestion/),
so an existing Elasticsearch output can often be pointed at it with a URL
change. See [Using VictoriaLogs with Logchef](/tutorials/victorialogs/).

## When Elasticsearch and Kibana is the better choice

- You need **relevance-ranked search** or vector search, not only log filtering.
- You use **Elastic APM, Elastic Security or ML anomaly detection** and want
  logs correlated with them in one UI.
- You want [dynamic mapping](https://www.elastic.co/docs/manage-data/data-store/mapping/dynamic-mapping)
  to add fields automatically when you index documents.
- You want [index lifecycle management](https://www.elastic.co/docs/manage-data/lifecycle/index-lifecycle-management)
  with hot, warm, cold, frozen, and delete phases. ILM is available in the
  versioned Elastic Stack, not Elasticsearch Serverless.
- You already pay for an Elastic subscription or Elastic Cloud, and your team
  knows Kibana.

## When Logchef is the better choice

- Your logs are already in **ClickHouse, VictoriaLogs or Loki**, or cluster
  cost is the reason you are leaving ELK.
- You want **SSO, team-scoped access, an audit log and alert notifications**
  without a subscription.
- You want **SQL** over logs, including joins against other ClickHouse tables.
- You want a small query layer: one binary, [TOML provisioning](/getting-started/provisioning/)
  for teams and sources, and [Prometheus metrics](/operations/metrics/).
- You want engineers and AI assistants to use the same access rules, through
  the [CLI](/integration/cli/) and the [MCP server](/integration/mcp-server/).

## Migrating from Elasticsearch

The [Elasticsearch migration guide](/migrate/from-elasticsearch/) covers the
whole path: re-pointing Filebeat, Logstash, Vector or the OpenTelemetry
Collector, mapping ECS fields to ClickHouse columns, and a KQL to LogchefQL and
SQL translation table. It also lists what you lose, so read that section before
you commit.

## FAQ

### Is Logchef an Elasticsearch alternative?

For log analytics, yes, together with ClickHouse, VictoriaLogs or Loki as the
store.
For application search, APM or SIEM, no.

### Is Logchef a Kibana alternative?

It replaces the parts of Kibana used for logs: Discover, the
[field sidebar](/features/field-sidebar/), dashboards and alerting. It does not
read Elasticsearch indices, so it is not a different front end for an existing
Elastic cluster.

### Can Logchef query Elasticsearch or OpenSearch?

No. Logchef supports ClickHouse, VictoriaLogs and Grafana Loki sources.

### Is Elasticsearch open source again?

The free portions of the source have offered AGPLv3 as an option since 2024,
alongside SSPL and the Elastic License. The default distribution uses Elastic License 2.0. Features above the Basic tier still need a paid
subscription.

### Does Logchef support KQL?

No. LogchefQL covers the same filtering role with a similar feel. The
[migration guide](/migrate/from-elasticsearch/#query-translation-kql--logchefql--sql)
has side-by-side examples.

### Can I keep Filebeat or Logstash?

Yes. Point them at VictoriaLogs through its Elasticsearch-compatible endpoint,
or replace them with [Vector](/integration/vector/) or the
[OpenTelemetry Collector](/integration/otel-collector/) to write to ClickHouse.

Ready to try it? Start with the [quickstart](/getting-started/quickstart/).
