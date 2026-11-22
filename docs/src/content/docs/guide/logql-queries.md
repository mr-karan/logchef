---
title: LogQL Queries
head:
  - tag: title
    content: "Grafana Loki LogQL Queries | Logchef"
description: Use native LogQL in Logchef against Grafana Loki, with common query patterns, how LogchefQL compiles to LogQL, and what Loki cannot express.
---

Loki sources in Logchef take two query languages: **LogchefQL**, the filter
syntax shared with ClickHouse and VictoriaLogs, and native **LogQL**, Loki's
own language. This page covers how native LogQL runs in Logchef, common
patterns, and how LogchefQL compiles to LogQL.

For connecting a Loki source, see [Using Grafana Loki with
Logchef](/tutorials/loki). For the LogchefQL reference, see [Search
Syntax](/guide/search-syntax).

The examples run on the dev stack's "Loki Demo" source (`dev/ingest-loki.sh`):
streams labelled `service_name`, `env` and `level`, JSON lines with `message`,
`status`, `request_id` and `duration_ms`, and `trace_id` as structured
metadata.

## Native LogQL mode

Open the **LogQL** tab in the query editor. Native mode differs from
LogchefQL mode in a few ways:

- **The query runs as written.** Logchef adds no stream selector, parser or
  limit. Every LogQL query needs its own selector, and Loki rejects one that
  matches the empty string, such as `{level=""}`.
- **The time range is sent separately**, as Loki's `start` and `end`. LogQL
  has no time literal, so there is nothing to add to the query.
- **A log query returns rows newest first**, with `_time`, `_msg` and one
  column per label of the entry. Loki puts stream labels, structured metadata
  and parsed fields into one label set, so all three appear as columns. Every
  value is a string.
- **The row limit applies.** Logchef reads up to the row limit you pick,
  capped by the server's maximum, in pages of the source's
  `max_entries_per_query`. When more entries exist, the result shows the
  **rows** truncation chip. When more entries share one nanosecond than fit in
  a page, the result stops there with "More than N entries share one
  timestamp. Narrow the query."
- **A metric query returns samples as rows**: `value` first, then `time`, then
  one column per series label.
- **An empty editor runs the source selector.**

Logchef never parses LogQL itself. Loki's answer decides whether a query is a
log query or a metric query, and Loki's parse errors come back as 400s with
Loki's message.

### Step

Logchef sends `query_range` a `step` with every query, because Loki checks it
before it knows the query kind and rejects a range of more than 11,000 steps.
The step is the dashboard bucket size when there is one, raised to at least
`range / 11000` and rounded up to a whole second. For a log query the step has
no effect. When a metric query's step is raised, the result says "Step raised
to N seconds to stay within Loki's limit of 11,000 points".

### Variables and macros

Request variables work in LogQL outside quoted strings. Inside a string,
`{{...}}` belongs to LogQL's own templates, such as
`| line_format "{{.message}}"`, and Logchef leaves it alone.

| Macro | Value |
|---|---|
| `{{__interval}}` | The bucket size as a LogQL duration, such as `5m` or `1d` |
| `{{__interval_s}}` | The bucket size in whole seconds |
| `{{__from}}`, `{{__to}}`, `{{__time_filter}}`, `{{__timezone}}`, `{{__tz_offset}}` | Rejected: the request range always applies |

## Common LogQL patterns

These are standard [LogQL](https://grafana.com/docs/loki/latest/query/) and run
unchanged through Logchef.

**Stream selectors**

```logql
{service_name="payments-api"}
```

```logql
{service_name="payments-api", level="error"}
```

```logql
{service_name=~"payments-.*|billing-.*", level!="info"}
```

**Line filters.** `|=` and `!=` match a substring. `|~` and `!~` match a
regular expression, which is not anchored.

```logql
{service_name="payments-api"} |= "gateway"
```

```logql
{service_name="payments-api"} |~ "(?i)GATEWAY" != "retrying"
```

**Structured metadata** needs no parser:

```logql
{service_name=~".+"} | trace_id="trace-003"
```

**Parsed fields and numeric comparisons.** A line whose value fails to
convert to a number keeps the line and sets `__error__`. Filter those out with
`__error__=""`.

```logql
{service_name="payments-api"} | json | status >= 500 | __error__=""
```

```logql
{service_name=~".+"} | json | duration_ms > 100 and request_id != "req-002"
```

**Reshaping the line**

```logql
{service_name="payments-api"} | json | line_format "{{.message}} ({{.status}})"
```

**Metric queries** return `value`, `time` and the series labels:

```logql
sum by (level) (count_over_time({service_name=~".+"} [5m]))
```

```logql
sum by (service_name) (rate({service_name=~".+"} |= "gateway" [5m]))
```

```logql
quantile_over_time(0.95, {service_name="payments-api"} | json | unwrap duration_ms | __error__="" [1h]) by (service_name)
```

**A dashboard result panel** uses the bucket size:

```logql
sum by (level) (count_over_time({service_name=~".+"} [{{__interval}}]))
```

**An alert query** is a metric query whose range is the window it counts:

```logql
sum(count_over_time({service_name="payments-api", level="error"} [5m]))
```

```logql
max(max_over_time({service_name="payments-api"} | json | unwrap duration_ms | __error__="" [5m]))
```

### Which features need which kind

| Feature | Query kind |
|---|---|
| Search | Log or metric |
| Histogram, dashboard count panels | Log query. Logchef wraps it in `count_over_time` |
| Live tail | Log query |
| Explorer log context | LogchefQL query. The context lookup uses the row's complete stream labels |
| Field value counts | Log query |
| Alerts | Metric query |

The [field sidebar](/features/field-sidebar#loki) preloads stream label values
from the index using the query's leading selector. If there is no leading
selector, as with a metric query, it uses the source's default selector.

## How LogchefQL compiles to LogQL

A compiled query has up to six parts in a fixed order, and only the selector
is always there:

```text
{<source selector>, <hoisted matchers>} <line filters> | <parser> | drop __error__, __error_details__ | <label filter> | __error__="" | keep <fields>
```

- A top-level `and` condition on a **stream label** with `=`, `!=`, `~` or
  `!~` joins the stream selector.
- A top-level condition on **`_msg`** becomes a line filter.
- Everything else goes into one **label filter** after the parser, keeping the
  query's `and`, `or` and parentheses.
- The source's parser stage (`json` or `logfmt`) is added when the source has
  one, with `drop __error__, __error_details__` after it.
- A numeric comparison adds `| __error__=""`, so lines whose value is not a
  number drop out instead of matching.

These are the queries Logchef generates on the dev source, whose selector is
`{service_name=~".+"}` and whose parser is JSON:

| LogchefQL | Generated LogQL |
|---|---|
| `level="error"` | `{service_name=~".+", level="error"} \| json \| drop __error__, __error_details__` |
| `level!="info"` | `{service_name=~".+", level!="info"} \| json \| drop __error__, __error_details__` |
| `service_name~"pay"` | `{service_name=~".+", service_name=~"(?i).*pay.*"} \| json \| drop __error__, __error_details__` |
| `_msg~"gateway"` | `{service_name=~".+"} \|~ "(?i)gateway" \| json \| drop __error__, __error_details__` |
| `request_id="req-003"` | `{service_name=~".+"} \| json \| drop __error__, __error_details__ \| request_id="req-003"` |
| `trace_id="trace-003"` | `{service_name=~".+"} \| json \| drop __error__, __error_details__ \| trace_id="trace-003"` |
| `service_name="payments-api" and status>=500` | `{service_name=~".+", service_name="payments-api"} \| json \| drop __error__, __error_details__ \| status>=500 \| __error__=""` |
| `status>=500 or level="warn"` | `{service_name=~".+"} \| json \| drop __error__, __error_details__ \| level="warn" or status>=500 \| __error__=""` |
| `level="error" \| _time _msg status` | `{service_name=~".+", level="error"} \| json \| drop __error__, __error_details__ \| keep status, env, level, service_name` |

Details worth knowing:

- **`~` and `!~` are case-insensitive substring matches.** Logchef escapes
  the text and writes `(?i)`. On a stream label it adds `.*` on both sides,
  because label regexes are anchored. Line filters are not.
- **`_msg="exact line"`** becomes `|~ "^exact line$"`, and an `or` of positive
  `_msg` matches becomes one line filter: `_msg~"a" or _msg~"b"` is
  `|~ "(?i)(a|b)"`.
- **Numeric comparisons move to the end of an `or`.** Loki's `or` stops at its
  first true operand, and a comparison on a value that is not a number counts
  as true with `__error__` set. Written first, `status>=500` would hide a line
  with `status=unknown` and `level="warn"`. Logchef therefore writes
  `level="warn" or status>=500`. A quoted number such as `status<"600"`
  becomes `status<600`.
- **Dotted names join with `_`.** `k8s.pod.name="p"` becomes
  `k8s_pod_name="p"`, which is how Loki's `json` parser and its OTLP ingestion
  name nested keys.
- **A select pipe becomes `keep`.** `_time` and `_msg` are always returned,
  and every stream label is always kept, because log context needs the row's
  stream.
- **A bare word is a string and a number stays a number.** `x=true` becomes
  `x="true"`, and `x=42` stays `x=42`.

### What Loki cannot express

Each of these returns an error that names the construct:

- `>`, `>=`, `<` or `<=` on `_msg`.
- An `or` that joins a `_msg` condition to another field, or that negates
  `_msg`. LogQL cannot join a line filter and a label filter with `or`. Move
  the `_msg` condition out of the `or`.
- An `or` with numeric comparisons on two different fields, such as
  `status>=500 or duration_ms>1000`. Logchef cannot order it so that a
  non-numeric value does not hide a match. Comparisons on one field, such as
  `status>=500 or status<100`, are allowed.
- A comparison with `null`.
- A field name that is not a valid Loki label name after dots become `_`, or
  that is a LogQL keyword such as `by`, `json`, `keep`, `drop`, `offset` or
  `pattern`.
- A [LogchefQL aggregation](/guide/aggregations) pipe. Write a LogQL metric
  query instead.

## Tenant and selector

Every request to Loki carries the source's tenant as `X-Scope-OrgID`, its
credential and its custom headers. No query text can change them.

The source's default stream selector seeds LogchefQL queries, the empty
editor, the schema and the field sidebar. It does not restrict native LogQL,
which can read any stream in the tenant. Treat a Loki source as access to the
whole tenant.

## Next steps

- [Using Grafana Loki with Logchef](/tutorials/loki) to connect a source
- [Search Syntax](/guide/search-syntax) for the full LogchefQL reference
- [Alerting](/features/alerting) to evaluate a LogQL metric query on a schedule
- [Dashboards](/features/dashboards) for LogQL panels

Use the [LogchefQL Playground](/playground/) to see how a LogchefQL filter becomes a Loki stream selector and pipeline.
