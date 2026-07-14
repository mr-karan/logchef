---
title: LogsQL Queries
description: Use native LogsQL in Logchef against VictoriaLogs — common query patterns, how LogchefQL compiles to LogsQL, and tenancy/scope behavior.
---

VictoriaLogs sources in Logchef support two query languages: **LogchefQL** (Logchef's shared quick-filter syntax, also used on ClickHouse sources) and native **LogsQL** (VictoriaLogs' own query language). This page focuses on LogsQL: how native mode behaves in Logchef, useful query patterns, and exactly how LogchefQL is compiled down to LogsQL.

For the full LogchefQL syntax reference, see [Search Syntax](/guide/search-syntax). For a wider tour of what Logchef adds on top of VictoriaLogs, see [VictoriaLogs Explorer](/tutorials/victorialogs-explorer).

## Native LogsQL mode

Switch a VictoriaLogs source's query editor to native mode to write LogsQL directly. A few things behave differently from LogchefQL mode:

- **Time range is applied separately.** The selected time range is sent as `start`/`end` parameters alongside your query text, not baked into the query string — so you don't need (and shouldn't add) your own `_time:` filter unless you want to override the picker.
- **Results are sorted newest-first automatically**, matching LogchefQL mode: Logchef appends `| sort by (_time desc)` to a query that has no pipes, or only `fields` pipes. A query using `stats`, its own `sort`, `limit`, or any other pipe stage is left untouched — at that point you're in full control of ordering.
- **A row limit is always applied.** An unspecified limit defaults to the source's configured default (1000, unless your admin changed it), and any limit you do set is capped at the source's configured maximum. Logchef surfaces a warning banner when either happens.

```text
service:="api" level:="error"
```

runs as-is against `/select/logsql/query` with your picked time range, and comes back sorted by `_time` descending.

```text
service:="api" level:="error" | stats count() as errors
```

is left exactly as written (it has a `stats` pipe), and returns one row.

## Common LogsQL patterns

These are standard [LogsQL](https://docs.victoriametrics.com/victorialogs/logsql/) syntax, usable in Logchef's native mode exactly as you'd use them against VictoriaLogs directly.

**Exact match and negation**

```text
level:="error"
NOT level:="error"
```

**Substring / regex match**

```text
_msg:~"(?i)timeout"
service:!~"^test-"
```

**Numeric comparisons**

```text
status_code:>=500
response_time_ms:>1000
```

**Combining filters**

```text
(service:="api" OR service:="gateway") AND level:="error"
```

**Field projection**

```text
service:="api" | fields _time, _msg, service, status_code
```

**Aggregation (for alert queries or dashboards)**

```text
service:="payments" level:="error" | stats count() as value
service:="api" | stats avg(response_time_ms) as value
```

**Unpacking a JSON field**

```text
service:="api" | unpack_json from extra_fields | fields _time, _msg, user_id
```

See the [official LogsQL examples](https://docs.victoriametrics.com/victorialogs/logsql-examples/) for more pipe types (`facets`, `top`, `uniq`, and others) — anything valid LogsQL runs unchanged through Logchef's native mode.

## How LogchefQL compiles to LogsQL

For a VictoriaLogs source, every LogchefQL query is translated into LogsQL before it's sent to VictoriaLogs — the translated LogsQL is what actually executes. The mapping:

| LogchefQL | Generated LogsQL |
|---|---|
| `level = "error"` | `level:="error"` |
| `level != "error"` | `NOT level:="error"` |
| `status_code >= 500` | `status_code:>=500` |
| `_msg ~ "timeout"` | `_msg:~"(?i)timeout"` |
| `path !~ "internal"` | `NOT path:~"(?i)internal"` |
| `service = "api" and env = "prod"` | `(service:="api") AND (env:="prod")` |
| `service = "api" and status_code >= 500 \| _msg service` | `(service:="api") AND (status_code:>=500) \| fields _time, _msg, service` |

A few details worth knowing:

- **`~` and `!~` are literal, case-insensitive substring matches, not arbitrary regex.** LogchefQL escapes any regex-special characters in your search text and wraps it in `(?i)`, so `body ~ "user[1]"` looks for the literal text `user[1]`, not a regex character class. If you need a real regex, write it directly in native LogsQL.
- **The pipe operator (`field1 field2 ...`) becomes a LogsQL `fields` pipe**, and the source's configured timestamp field is always included first even if you didn't ask for it, so timestamps never silently disappear from projected results.
- **`null` comparisons aren't supported** for VictoriaLogs — LogchefQL's translator rejects them rather than guessing at LogsQL's handling of missing fields.

## Tenancy and scope behavior

Every request Logchef makes to VictoriaLogs for a given source — explore queries, histograms, field-values lookups, facets, live tail, and alert evaluation — carries the same tenancy and scope configuration:

- **Tenant headers**: if a source has `AccountID` / `ProjectID` configured, both are sent as `AccountID` / `ProjectID` HTTP headers on every request.
- **Immutable scope filter**: if a source has a scope query configured, it's added to every request as an additional filter — either as a stream filter (when the scope is written as `{...}`, e.g. `{app="payments"}`) or as a plain filter expression (e.g. `kubernetes.namespace:="prod"`) appended alongside your query.

This scope can't be bypassed from the query editor — it's applied server-side regardless of query language or mode, so a source pinned to one tenant or namespace stays pinned to it.

**Schema and field discovery ignore pipes.** Field-name discovery, field-values lookups, and facets requests run with `ignore_pipes` set, meaning only the filter portion of your query is used — any `stats`, `sort`, or `fields` pipe in the query text is ignored for those lookups. This keeps field discovery reflecting "what fields exist given these filters," not "what the pipe output looks like."

## Next steps

- [VictoriaLogs Explorer](/tutorials/victorialogs-explorer) — what Logchef adds on top of VictoriaLogs' vmui
- [Using VictoriaLogs with Logchef](/tutorials/victorialogs) — connect a source and configure ingestion
- [Search Syntax](/guide/search-syntax) — the full LogchefQL reference
- [Alerting](/features/alerting) — evaluate a LogsQL query on a schedule
