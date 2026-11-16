# RFC 0005: LogchefQL aggregations

Status: Draft
Date: 2026-11-15
Authors: Karan Sharma

## Summary

LogchefQL gains an aggregation pipe. `level=error | count() by service` counts
error rows per service, and the same text compiles to ClickHouse SQL and to
LogsQL. A user who wants a count, a sum, an average, a percentile or a
group-by no longer needs to write the native language of one backend.

The pipe has eleven functions, `by` keys with optional time buckets, and two
optional stages, `sort` and `limit`. The result is the frame of RFC 0003,
typed columns and rows from `POST /logchefql/query`. No new endpoint and no
second result path exist.

The Explorer, dashboard panels in `result` mode, the CLI, MCP and Ask Chef
all get the feature. Paths that need log rows (live tail, windowed search,
field values, the histogram) reject an aggregation by construction. Alerting
consumes the compile API in RFC 0006.

## Problem

LogchefQL is a filter language. The grammar is a boolean filter plus one pipe
that only lists fields (`internal/logchefql/grammar.go:52`). A pipe item is a
field path and nothing else (`internal/logchefql/grammar.go:106`). The AST has
a `Where` and a `Select` (`internal/logchefql/types.go:102`).

The ClickHouse generator turns the pipe into a column list
(`internal/logchefql/sql_generator.go:47`) and the builder always emits
`ORDER BY <timestamp> DESC` with no `GROUP BY`
(`internal/logchefql/logchefql.go:442`). The LogsQL generator turns the pipe
into a `fields` pipe (`internal/logchefql/logsql_generator.go:201`).

So every count, sum or group-by needs native SQL or LogsQL, and four features
are tied to one backend each.

- A dashboard panel that sums or groups must use `result` mode with
  `clickhouse-sql` or `logsql`. RFC 0003 gives "LogchefQL has no aggregation
  syntax" as the first reason that `count` mode stays
  (`rfcs/0003-dashboards-v2.md:447`). A LogchefQL `result` panel returns log
  rows only (`rfcs/0003-dashboards-v2.md:492`).
- An alert in condition mode stores a native query next to the LogchefQL
  condition (`internal/core/alerts.go:161`). The alert form builds that query
  in the browser from a translate call
  (`frontend/src/composables/useAlertForm.ts:240`). Both providers refuse any
  alert language other than their own
  (`internal/datasource/clickhouse_provider.go:668`,
  `internal/victorialogs/querying.go:703`).
- The MCP query tool describes LogchefQL as "a simple filter syntax"
  (`internal/mcp/logchefql.go:162`), and the CLI `query` command prints log
  rows (`cli/crates/logchef-cli/src/commands/query.rs:241`). An agent or a
  script that needs a count writes SQL for one backend.

## Goals and non-goals

Goals:

- One aggregation syntax that compiles to ClickHouse SQL and to LogsQL.
- The result is an RFC 0003 frame from the existing query endpoint.
- No valid query of today changes meaning.
- Group cardinality, bucket count and row count have bounds, and the user
  sees which bound was hit.
- The places where the two backends cannot agree are written down, and each
  has one defined behavior per backend.
- A path that cannot show an aggregated result rejects it with a clear
  message. No path ignores the pipe.

Non-goals:

- **Expressions.** No arithmetic between aggregates (`sum(a) / count()`), no
  `if` inside a function. A ratio stays a native query.
- **A filter after the aggregation** (`| where count > 100`). An alert
  compares the value with its own threshold, so the first consumer does not
  need the stage. The grammar leaves room for it.
- **Top N series over time with an "Other" series.** It needs a subquery on
  ClickHouse and has no LogsQL form. `count` mode already does it
  (`internal/clickhouse/logs.go:74`), and RFC 0003 rules that a result frame
  never gets an automatic "Other".
- **Arbitrary percentiles.** Five fixed percentiles cover dashboards and
  alerts.
- **Aggregations in live tail, windowed search, exports and the histogram.**
- **A change to alert evaluation.** RFC 0006 owns it.
- **A partial-aggregate compile mode.** No output exists for the number of
  inputs that `avg` consumed. "Seams" has the consequence for backtests.
- **More than one aggregation pipe, joins and subqueries.**

## Design

### Data shape

This feature stores nothing. It adds no table, no migration for either
metadata store, and no stored model. The data shape is the AST and the
description of the result that the compiler returns.

AST, in `internal/logchefql/types.go`:

```go
type AggFunc string // count, count_distinct, sum, avg, min, max, p50, p75, p90, p95, p99

type Aggregate struct {
	Func  AggFunc
	Field any    // string or NestedField. nil for count()
	Name  string // output column name, from "as" or the default rule
}

type GroupKey struct {
	Time     bool   // the time bucket key
	Interval string // "5m" for time(5m). "" means the request interval
	Field    any    // string or NestedField. nil when Time is true
	Name     string // output column name
}

type SortKey struct {
	Name string // an output column name
	Desc bool
}

type AggregationNode struct {
	Aggregates []Aggregate // 1 to 10
	By         []GroupKey  // 0 to 5, at most one with Time
	Sort       *SortKey
	Limit      int // 0 means the query has no limit of its own
}
```

`QueryNode` gains `Aggregate *AggregationNode`. The parser sets `Select` or
`Aggregate`, never both.

Result description, returned by the compiler and by the API:

```go
type ResultRole string // "time", "group", "value"

type ResultColumn struct {
	Name string
	Role ResultRole
	// Type is the column type to report when the backend reports none.
	// "DateTime" for time, "String" for group, "UInt64" for count and
	// count_distinct, "Float64" for every other value.
	Type string
	// Source is the field that the backend returns the column under. It
	// differs from Name on VictoriaLogs only.
	Source string
}

type AggregationShape string // "scalar", "grouped", "timeseries", "grouped_timeseries"

type AggregationInfo struct {
	Shape    AggregationShape
	Columns  []ResultColumn // time first, then group keys, then values, each in written order
	Interval string         // the bucket window in use, such as "5m". "" without a time key
	Limit    int            // the query's own limit, or 0
}
```

| Shape | `by` | Rows | Example |
|---|---|---|---|
| `scalar` | none | Exactly 1 | `level=error \| count()` |
| `grouped` | fields only | One per group | `\| count() by service` |
| `timeseries` | `time` only | One per bucket that has rows | `\| count() by time` |
| `grouped_timeseries` | `time` and fields | One per bucket and group that has rows | `\| p95(duration_ms) by time(5m), service` |

### Syntax

```
query     = [ filter ] [ "|" ( select | aggregate ) ]
select    = field { field }                          (unchanged)
aggregate = item { "," item } [ "by" key { "," key } ]
            [ "|" "sort" name [ "asc" | "desc" ] ]
            [ "|" "limit" integer ]
item      = func "(" [ field ] ")" [ "as" name ]
key       = ( "time" [ "(" window ")" ] | field ) [ "as" name ]
```

```
level=error | count() by service
level=error | count() as errors, count_distinct(user_id) as users by service | sort errors desc | limit 10
service=api | p95(duration_ms), avg(duration_ms) by time(5m)
```

Rules:

- A pipe whose first item is `name(` is an aggregation. Any other pipe is a
  select. `name(` after a pipe is a parse error today, so no valid query
  changes meaning. `| count` with no parenthesis still selects the field
  `count`, and `x=1 | limit` still selects the field `limit`.
- Keywords and function names are case-insensitive, as `and` and `or` are
  (`internal/logchefql/grammar.go:113`).
- `time` as a key means the bucket. A field named `time` is written `"time"`,
  with the quoted segment that field paths already accept
  (`internal/logchefql/grammar.go:94`).
- `window` is one of the histogram windows, the table that
  `template.ParseInterval` reads (`internal/template/builtins.go:45`). `time`
  with no window uses the `interval` of the request.

| Function | Argument | Meaning |
|---|---|---|
| `count()` | none | Rows that match the filter |
| `count_distinct(f)` | any field | Distinct non-empty values of `f`. Exact |
| `sum(f)`, `avg(f)` | number field | Sum and mean of the numeric values of `f` |
| `min(f)`, `max(f)` | number field | Smallest and largest numeric value of `f` |
| `p50(f)`, `p75(f)`, `p90(f)`, `p95(f)`, `p99(f)` | number field | Percentile of `f`. Approximate on both backends |

| Item | Default name | With `as` |
|---|---|---|
| `count()` | `count` | The alias |
| `func(f)` | `func_f`, where every character of the path outside `[A-Za-z0-9_]` becomes `_`. `p95(log.duration)` is `p95_log_duration` | The alias |
| Field key | The path as written, joined with `.`. `log_attributes.host` stays `log_attributes.host` | The alias |
| `time` key | `time` | The alias |

A group column keeps the dotted path, so it has the same name on both
backends. The select pipe names a nested field `base_path` on ClickHouse
(`internal/logchefql/sql_generator.go:485`), and this RFC does not change it.

### Grammar

The lexer gains one rule, `{Name: "Comma", Pattern: ","}`. A comma is a lexer
error today, so the rule changes no valid query. The parser keeps a lookahead
of 2 (`internal/logchefql/grammar.go:115`). The choice between the two pipes
sits after the `Pipe` token, where `Ident LParen` selects the aggregation.

```go
type PQuery struct {
	Where *POrExpr `parser:"@@?"`
	Pipe  *PPipe   `parser:"( Pipe @@ )?"`
}

type PPipe struct {
	Aggregate *PAggregate    `parser:"( @@"`
	Select    []*PSelectItem `parser:"| @@+ )"`
}

type PAggItem struct {
	Func  string      `parser:"@Ident LParen"`
	Field *PFieldPath `parser:"@@? RParen"`
	Alias *string     `parser:"( 'as':Ident @Ident )?"`
}
```

`PAggregate` holds the items, the optional `by` keys, and the optional
`Pipe 'sort'` and `Pipe 'limit'` stages. `PGroupKey` and `PSort` follow the syntax above. A window such as `5m` lexes
as `Number` then `Ident`, and the converter joins them. The struct layout is
a guide. The shared fixture in "Web UI" decides what parses.

`expectedDescriptions` gains a word for each new node
(`internal/logchefql/logchefql.go:132`), so an error reads "expected a
function such as count() after `|`" and not a grammar node name.

`ConvertToAST` builds the `AggregationNode`, and a new `validateAggregation`
in `internal/logchefql/aggregation.go` checks it. Each failure is a
`ParseError` with the new code `INVALID_AGGREGATION` and the position of the
item.

| Rule | Message |
|---|---|
| The function is one of the eleven | `unknown function "median". Use count, count_distinct, sum, avg, min, max, p50, p75, p90, p95 or p99` |
| `count` takes no field. Every other function takes one | `sum needs a field, as in sum(bytes)` |
| 1 to 10 items, 0 to 5 keys, at most one `time` key | `a query can have at most 10 aggregations` |
| An alias matches `^[A-Za-z_][A-Za-z0-9_]{0,63}$` | `invalid name "2xx"` |
| Output names are unique | `two columns are named "count". Name one with "as"` |
| A field appears once in `by`, and no field or alias starts with `__lc_` | `"service" is grouped twice` |
| `sort` names an output column | `cannot sort by "foo". Columns are service, count` |
| `limit` is 1 to 10,000 | `limit must be between 1 and 10000` |
| No `limit` when the keys are `time` plus a field | `limit cannot be used with time and a field, because it would cut buckets and not series. Add a filter to reduce the series` |

### Default deny in the compile path

Many call sites compile LogchefQL today. Most of them need a filter or log
rows: live tail (`internal/server/tail_handlers.go:253`), windowed search
(`internal/server/windowed_handlers.go:62`), field values
(`internal/clickhouse/field_values.go:55`) and the MCP investigation check
(`internal/mcp/investigate.go:75`). A new caller that forgets the aggregation
case would run it wrong, and tail would ignore the pipe without a word.

So the compile request carries an opt-in, and the default rejects.

`LogchefQLCompileRequest` gains `Interval string` and `AllowAggregation bool`
(`internal/datasource/service.go:360`). `CompiledLogchefQL` gains
`Aggregation *logchefql.AggregationInfo`, which is nil for a query that
returns log rows (`internal/datasource/service.go:370`).

The run path carries the same description. `PrepareLogchefQLQuery` passes
only the compiled text to the provider today (`internal/core/query.go:258`),
and `datasource.QueryRequest` has no field for it
(`internal/datasource/query.go:12`). `QueryRequest` gains
`Aggregation *logchefql.AggregationInfo`, and `PrepareLogchefQLQuery` copies
it from the compiled result. A provider reads it for column names, order,
types and the bounds in "Limits and failure modes". The buffered path and the
streaming path both receive it, because both take a `QueryRequest`.

- `logchefql.Translate` and `logchefql.TranslateToLogsQL` return the error
  `AGGREGATION_NOT_ALLOWED` for an `AggregationNode`, unless the caller passes
  the aggregation options. The message is "this feature needs log rows, and
  the query aggregates. Remove the pipe after the filter".
- Three callers set `AllowAggregation`: `PrepareLogchefQLQuery` when the HTTP
  query handler, the CLI or MCP calls it (`internal/core/query.go:217`),
  `TranslateLogchefQL` (`internal/core/query.go:350`), and the alert evaluator
  of RFC 0006. `LogchefQLQueryRequest` gains a field for it
  (`internal/core/query.go:194`), and windowed search leaves it false.
- `FilterOnly` stays the filter of the query, with or without an aggregation.
  The field sidebar and the conditions list therefore keep working on an
  aggregated query.
- `logchefql.Validate` is syntax plus `validateAggregation`. It has no source
  and applies no deny rule.

A new capability says that a provider compiles the pipe.

```go
CapabilityLogchefQLAggregation Capability = "logchefql_aggregation"
```

It sits with the others (`internal/datasource/service.go:51`). A source
response already lists capabilities (`pkg/models/source.go:307`), so clients
read it from there. The flag is one bit and has no per-shape or per-function
form. One aggregation compiles to one backend query, and no compile returns
several queries. A provider that has the flag validates each aggregation when
it compiles it, and rejects a function or a shape that it cannot run with the
`ParseError` code `AGGREGATION_NOT_SUPPORTED` and a message that names the
construct. A provider without the flag returns the same code with
"aggregations are not supported for this source type". ClickHouse and
VictoriaLogs run every construct in this RFC.

### API

No new route. Token scopes do not change. All three routes keep
`logs:read` (`internal/server/server.go:494`).

| Route | Request change | Response change |
|---|---|---|
| `POST .../logchefql/validate` | None | An aggregation that breaks a rule returns `valid: false` with `INVALID_AGGREGATION` |
| `POST .../logchefql/translate` | New optional `interval` | New `aggregation` object. `sql` and `conditions` stay the filter. `full_sql` and `generated_query` hold the whole aggregated query |
| `POST .../logchefql/query` | New optional `interval` | New `aggregation` object next to `generated_query`. `logs` holds the result rows and `columns` their names and types |

The `aggregation` object is the JSON form of `AggregationInfo`, with the keys
`shape`, `interval`, `limit` and `columns`, and `name`, `role` and `type` in
each column. It is absent for a query that returns log rows. The handler
writes it from the compiled result, on a cache hit too, because the compile
runs before the cache lookup (`internal/server/logchefql_handlers.go:188`).
The dashboard cache key gains the output column names, since two queries
that differ only in an alias compile to one LogsQL text
(`internal/cache/dashcache.go:291`). The response stays one
`QueryResult` (`pkg/models/schema.go:9`) with the same `stats`, including
`limit_source` and `truncation` (`pkg/models/common.go:53`).

Errors, all HTTP 400 with the validation error type:

| Case | Message |
|---|---|
| `by time` with no window and no `interval` in the request | `this query buckets by time, so the request needs an interval` |
| `interval` or a `time(...)` window outside the table | `invalid interval "7m": use one of 1s, 5s, ...`, the text that `requestInterval` already returns (`internal/core/query.go:90`) |
| The range divided by the window gives more than 1,000 buckets | `1h over this range gives 2160 buckets. The maximum is 1000. Use 3h or longer` |
| The provider cannot run the aggregation | `AGGREGATION_NOT_SUPPORTED`, with the message from the provider |
| ClickHouse group limit reached | `the query produced more than 100000 groups. Add a filter or group by fewer fields` |
| ClickHouse memory limit reached | `the query needs more memory than the limit allows. Add a filter or shorten the range` |

The server never raises a window by itself. A client that wants RFC 0003's
"raise to the next window" rule applies it before the request, as the
dashboard runtime does (`frontend/src/utils/dashboardRuntime.ts:134`).

LogchefQL still rejects every `{{__...}}` macro, as RFC 0003 decided
(`rfcs/0003-dashboards-v2.md:1073`). The compiler applies the range, and
`by time` reads the `interval` request field that `{{__interval}}` reads in
the native languages.

### Result shape

The result is the frame of RFC 0003 ("A panel is a query plus a
visualization"). The server sends `columns` and `logs`, and the client builds
the frame with `resultFrame` (`frontend/src/utils/panelFrame.ts:40`).

- **Column order.** The `time` column, then keys, then values, each in
  written order.
- **Rows.** One row per group that has at least one matching row. The server
  adds no zero rows and no "Other" row. A `scalar` query always returns one
  row.
- **Gaps.** A bucket with no rows is absent. The client fills gaps for a
  chart from `aggregation.interval`, as it does for a count frame.
- **Types on ClickHouse.** ClickHouse reports the type of every column, and
  the frame reads it with `declaredColumn`
  (`frontend/src/utils/panelFrame.ts:25`).
- **Types on VictoriaLogs.** VictoriaLogs reports no types, and the provider
  guesses from the field name today
  (`internal/victorialogs/querying.go:1377`). For an aggregated query the
  provider builds `columns` from `QueryRequest.Aggregation`: the names, the
  order and the `type` of each column. It does so for a result with no rows
  too, so an empty grouped result still has its columns.
- **Declared types reach the frame.** The dashboard store drops every
  declared type when the generated language is LogsQL
  (`frontend/src/stores/dashboards.ts:559`). It keeps them when the response
  has `aggregation`, and the Explorer follows the same rule. The frame then
  marks the columns `declared`, and the author confirms nothing. RFC 0003
  says that a VictoriaLogs frame has a null `backendType`. That rule stays
  for native LogsQL and gains this one exception. "Seams" records it.
- **Value cells on ClickHouse.** As the driver returns them.
- **Value cells on VictoriaLogs.** Every value arrives as text, and the frame
  reads a declared float only from a JSON number
  (`frontend/src/utils/panelFrame.ts:96`). So the provider converts each
  `Float64` cell with `strconv.ParseFloat` and writes a JSON number. `NaN`,
  an infinity, `""` and text that does not parse become JSON null. A `UInt64`
  cell stays the digit string that VictoriaLogs returned, which the frame
  parses as an exact integer.
- **Group cells.** The value as the backend returned it. `""` and null stay
  distinct.

Default order, when the query has no `sort`:

| Shape | Order |
|---|---|
| `scalar` | None |
| `grouped` | The first value descending, then the keys ascending |
| `timeseries`, `grouped_timeseries` | `time` ascending, then the keys ascending |

The order is total, so a capped `grouped` result is the top groups by the
first value, and two runs return the same rows.

### ClickHouse generator

`SQLGenerator` gains `GenerateAggregation`, and
`buildFullQueryFromTranslation` gains a second branch
(`internal/logchefql/logchefql.go:442`). The time range and the filter are
built as today. The provider passes the bucket expression and the timezone
as rendered text, so `internal/logchefql` imports nothing new.
`template.Interval` gains two exported methods, `ClickHouse()` and `LogsQL()`,
over its two private fields (`internal/template/builtins.go:18`).

```sql
SELECT toStartOfInterval(`timestamp`, INTERVAL 5 MINUTE, 'Asia/Kolkata') AS `time`,
       `service` AS `service`,
       count() AS `count`,
       quantileOrNull(0.95)(`duration_ms`) AS `p95_duration_ms`
FROM default.logs
WHERE `timestamp` BETWEEN toDateTime(...) AND toDateTime(...)
  AND (`level` = 'error')
GROUP BY `time`, `service`
ORDER BY `time` ASC, `service` ASC
```

The bounds are not in the SQL text. The provider adds them to the settings
of the query when `QueryRequest.Aggregation` is set, through the map that
already carries `max_execution_time` (`internal/clickhouse/result_limits.go:105`).
"Limits and failure modes" lists them.

A field resolves through `resolveField`, the function that comparisons use
(`internal/logchefql/sql_generator.go:262`). Map keys, native JSON paths and
`JSONExtractString` on a String column therefore work in an aggregation with
no new code. A bare name that is not a column is not looked up in the default
Map column. Only the select pipe does that, and an aggregation follows the
comparison rule.

A number function needs a number. The generator picks the expression from
the column type that `resolveField` returns.

| Resolved type | Expression for `sum`, `avg`, `min`, `max`, `pNN` |
|---|---|
| `Int*`, `UInt*`, `Float*`, `Decimal*`, with or without `Nullable` | The field |
| `String`, `FixedString`, a Map with String values, `JSONExtractString`, an unknown type | `toFloat64OrNull(<field>)` |
| `Dynamic` (a native JSON path with no declared type) | `toFloat64OrNull(toString(<field>))` |
| `Bool`, `Date*`, `DateTime*`, `Array`, `Map`, `Tuple`, `JSON` | Compile error: `sum needs a number field. "tags" is Array(String)` |

Non-numeric text becomes NULL, and every aggregate skips NULL. The sum of an
integer column stays an integer, so RFC 0003's exact numbers hold.

An output name that equals a source column, other than the group field that
it names, is a compile error on ClickHouse. ClickHouse resolves an alias
inside `WHERE`, so `count() as level` would replace the column `level` in the
filter. The same rule covers the `time` bucket on a table that has a column
named `time`, and the message asks for `as`.

### LogsQL generator

`LogsQLGenerator.visitQuery` gains a branch next to the `fields` pipe
(`internal/logchefql/logsql_generator.go:177`).

```
level:="error"
  | stats by (_time:5m offset 19800s, service)
      count() as __lc_v0,
      quantile(0.95, duration_ms) if (NOT duration_ms:"") as __lc_v1
  | sort by (_time, service)
```

- The query never renames. Each value gets an internal name, `__lc_v<i>` in
  written order, and each key keeps its field name, with `_time` for the
  bucket. `ResultColumn.Source` records that name, and the provider writes
  each row under the output names. The reviewer of this RFC read in the
  v1.52.0 parser (`lib/logstorage/pipe_stats.go`) that VictoriaLogs rejects a
  value name that equals a `by` field, so `| count() as service by service as
  svc` would fail if the alias reached the query. With internal names no alias, swap or reserved word can collide,
  and the validation rule on `__lc_` keeps a field from taking the prefix.
- `sort` names the `Source` of its column.
- Field names go through `formatFieldName`
  (`internal/logchefql/logsql_generator.go:358`), which quotes a name that is
  a LogsQL keyword (`internal/logchefql/logsql_generator.go:57`).
- The generated query that the Explorer shows therefore has `__lc_v0` where
  the result has `count`. A user who copies it into a native LogsQL panel
  gets the internal names.
- VictoriaLogs flattens nested JSON into dotted field names at ingest, so
  `log.level` is one field name. No JSON or Map handling exists.
- The offset is one fixed offset, read at the start of the range, the rule
  that `{{__tz_offset}}` uses (`internal/template/builtins.go:131`).
- The provider's default sort leaves a query with a `stats` pipe untouched
  (`internal/victorialogs/querying.go:1273`).
- The LogsQL forms below come from the LogsQL reference at
  https://docs.victoriametrics.com/victorialogs/logsql/ , read on 2026-11-15.
  The dev stack pins `victoria-logs:v1.52.0` (`dev/docker-compose.yml:34`).

### Construct table

`f` is the resolved field and `n(f)` is the number expression from the table
above.

| Construct | ClickHouse SQL | LogsQL |
|---|---|---|
| `count()` | `count()` | `count()` |
| `count_distinct(f)`, String field | `uniqExactIf(f, f != '')` | `count_uniq(f)` |
| `count_distinct(f)`, other type | `uniqExact(f)` | `count_uniq(f)` |
| `sum(f)` | `sumOrNull(n(f))` | `sum(f) if (NOT f:"")` |
| `avg(f)` | `avgOrNull(n(f))` | `avg(f) if (NOT f:"")` |
| `min(f)`, `max(f)` | `minOrNull(n(f))`, `maxOrNull(n(f))` | `min(f) if (NOT f:"")`, `max(f) if (NOT f:"")` |
| `p95(f)` | `quantileOrNull(0.95)(n(f))` | `quantile(0.95, f) if (NOT f:"")` |
| A value and its name | `` <expr> AS `<name>` `` | `<expr> as __lc_v<i>`. The provider writes the name |
| `by f` | `` f AS `<name>` `` and `` GROUP BY `<name>` `` | `by (f)`. The provider writes the name |
| `by time(5m)` | `` toStartOfInterval(`ts`, INTERVAL 5 MINUTE, '<tz>') AS `time` `` | `by (_time:5m offset <offset>)`. The provider writes the name |
| `by time` | The same, with the request `interval` | The same, with the request `interval` |
| Default order | `ORDER BY` per "Result shape" | `sort by (...)` per "Result shape" |
| `\| sort x desc` | `` ORDER BY `x` DESC ``, then the keys ascending | `sort by (<source of x> desc, <keys>)` |
| `\| limit N` | `LIMIT N` | `limit N` |
| Group count bound | Query settings `max_rows_to_group_by = 100000` with `throw` | None exists |
| Memory bound | Query setting `max_memory_usage` | None exists |

The LogsQL filter is `NOT f:""`, the complement of the empty value filter.
The any value filter `f:*` means the same in the LogsQL reference, but on
VictoriaLogs v1.52.0 `if (f:*)` inside `stats` matches no row of a block
that stores `f` as an integer column, while a top-level `f:*` does. With it
every number function over an integer field returned null.

The `-OrNull` and `-If` combinators and their combination are in the
ClickHouse combinator reference at
https://clickhouse.com/docs/sql-reference/aggregate-functions/combinators .

### Web UI

**Language support.** LogchefQL has no full parser in the browser. Four
helpers read the text, and each one changes.

| File | Change |
|---|---|
| `frontend/src/utils/logchefql/aggregation.ts` (new) | `splitStages(query)`, `isAggregation(query)` and `aggregationOutline(query)`, which returns the functions, keys, aliases and whether a `time` key has a window. It reads tokens and does not validate |
| `frontend/src/utils/logchefql/filters.ts` | `splitPipe` keeps every stage after the first pipe as the tail (`frontend/src/utils/logchefql/filters.ts:67`). `addSelectField` returns the query unchanged for an aggregation (`frontend/src/utils/logchefql/filters.ts:92`). `appendCondition` needs no change |
| `frontend/src/utils/logchefql/diagnostics.ts` | `getLogchefQLFields` skips function names, keywords, aliases and windows, so the editor does not mark `count` or `errors` as an unknown field (`frontend/src/utils/logchefql/diagnostics.ts:22`). A number function on a non-number column gets a warning marker |
| `frontend/src/utils/logchefql-autocomplete.ts` | `parseLogchefQLContext` gains four contexts (`frontend/src/utils/logchefql-autocomplete.ts:166`). After a pipe it offers functions and fields. Inside `sum(` it offers number fields. After an item it offers `as`, `by` and `,`. After `by` it offers fields and `time` |
| `frontend/src/utils/monaco.ts` | The tokenizer gains function and keyword tokens (`frontend/src/utils/monaco.ts:350`) |

Validation stays on the server, through `/logchefql/validate` as today.

A shared fixture keeps the browser helpers and the Go parser in step.
`internal/logchefql/testdata/aggregation_cases.json` lists queries, each with
`valid`, `is_aggregation`, the function names, the key names and the output
column names. The Go test and the vitest suite both read the file, so a
syntax change that reaches one side fails the other.

**Explorer.** `LogExplorer.vue` renders the log table or the compact list
(`frontend/src/views/explore/LogExplorer.vue:1104`). When the response
carries `aggregation`, the Explorer builds a frame and renders a result view
in that place.

- A **Chart** and **Table** toggle. The table is `PanelTable`. The chart
  depends on the shape. `timeseries` renders `PanelChart` with `x = time` and
  `y` the value columns. `grouped_timeseries` with one field key adds that
  key as `series`. `grouped` with one key renders `PanelTopList` on the first
  value. `scalar` renders `PanelStat` for one value.
- A result with two or more field keys has no chart. `buildPanelChart`
  identifies a row by `x` and one series column, and rejects a duplicate in
  result mode (`frontend/src/utils/panelCharts.ts:151`). `| count() by time,
  service, host` gives two rows for one `(time, service)` when a service has
  two hosts. The toggle is disabled, with the hint "A chart needs one group
  field". A `scalar` result with several values has no chart either.
- The default is the chart for a shape that has `time` and at most one field
  key, and the table otherwise.
- The histogram above the result is hidden, by the rule that hides it in SQL
  mode (`frontend/src/views/explore/LogExplorer.vue:618`). The result is
  already an aggregation, and the histogram wrapper cannot count grouped SQL.
- The Explorer sends `interval` with every LogchefQL query, from the
  granularity that the histogram already computes.
- Row expansion, log context, column selection and live tail are off for an
  aggregated result. The field sidebar stays.
- A click on a group cell adds `field = "value"` to the filter. A click on a
  null group cell adds nothing, because LogsQL has no null test.
- The truncation chip of RFC 0003 shows above the result.
- **Add to dashboard** creates a `result` panel with `language: logchefql`
  and the query text.
- The generated-query popover shows `generated_query`, as it does today.

**Dashboards.** A panel in `result` mode with `language: logchefql` already
posts to `/logchefql/query`. Three changes make it chart an aggregation.

- The runtime sends `interval` for a LogchefQL `result` panel, resolved by
  the function that resolves it for `{{__interval}}`
  (`frontend/src/utils/dashboardRuntime.ts:134`).
- The builder suggests roles from `aggregation.columns` when the response has
  it. `time` is `x`, each `value` column is in `y`, and the one `group`
  column is `series`. With two or more `group` columns the builder suggests
  the table. The author can still change the suggestion.
- The store keeps declared types for an aggregated result, per "Result
  shape".
- `ValidateDashboardPanels` rejects a `count` panel whose LogchefQL text is
  an aggregation, next to the `translate_limit` rule
  (`pkg/models/dashboards_v2.go:798`). The message is "an aggregation needs
  result mode". The builder switches the mode when the author types a pipe
  with a function.

The stored blob needs no new field and no new feature name. An older client
sends the text to the server and renders the frame that returns.

**Alerts.** Not changed here. "Seams" states what RFC 0006 receives.

### CLI

- `QueryRequest` gains `interval`
  (`cli/crates/logchef-core/src/api/models.rs:169`), and `logchef query`
  gains `--interval`. Without the flag, a `by time` query with no window
  fails with the server's message.
- When the response has `aggregation`, the table output prints the result
  columns in order with no timestamp column in front. `--output json` and
  `jsonl` print the rows as they are.
- `logchef explain` prints the aggregated `generated_query`, and `logchef
  tail` prints the server's `AGGREGATION_NOT_ALLOWED` message.
- The bundled skill text (`cli/crates/logchef-cli/src/commands/skills.rs`)
  and `docs/src/content/docs/integration/cli.mdx` gain the syntax.

### MCP

- `QueryLogchefQLParams` and `TranslateLogchefQLParams` gain `interval`
  (`internal/mcp/logchefql.go:17`, `internal/mcp/logchefql.go:28`). The query
  handler sets `AllowAggregation`.
- The `query_logchefql` description gains the pipe, with two examples and the
  eleven function names (`internal/mcp/logchefql.go:162`). The result gains
  the `aggregation` object.
- `get_log_histogram` takes native text (`internal/mcp/logs.go:40`). Its
  description says to pass the `sql` filter of a translation and not an
  aggregated `full_sql`.
- The window comparison tool takes a filter
  (`internal/mcp/analysis.go:24`). It keeps the default deny, and its
  description says "filter only".
- The LogchefQL reference in `internal/mcp/prompts.go` and
  `internal/mcp/resources.go` gains the syntax.

### Ask Chef

The LogchefQL reference in `internal/askchef/prompt.go` gains the pipe, so
the assistant can propose `level=error | count() by service`. Its LogchefQL
tool in `internal/askchef/tools.go` sets `AllowAggregation` and passes
`interval`, and the evidence card (`internal/askchef/evidence.go`) shows the
result as a table. The AI SQL generator writes native SQL and is not affected.

### Other surfaces

| Surface | Change |
|---|---|
| Provisioning TOML | Not affected |
| Metrics | Not affected. No new metric |
| Config | New key `query.aggregation_max_memory_bytes`, default 2 GiB, next to the preview limits (`internal/config/config.go:98`). Settable as `LOGCHEF_QUERY__AGGREGATION_MAX_MEMORY_BYTES` |
| Saved queries, collections and query history | Not affected. They store the LogchefQL text, and the text may hold an aggregation |
| Docs | New `guide/aggregations.md`. Edits to `guide/search-syntax.md`, `features/dashboards.md`, `integration/cli.mdx`, `integration/mcp-server.mdx` and the changelog |

## Access and security

- **Roles and scopes.** No change. An aggregation needs what a LogchefQL
  query needs: team access to the source, and `logs:read` for a token.
- **Restricted sources (RFC 0002).** The generated SQL reads only the source
  table. It has no subquery, CTE, JOIN or set operation, the constructs that
  RFC 0002 lists as ways around a restriction
  (`rfcs/0002-restricted-source-access.md:32`). A row filter applies before
  `GROUP BY`, so an aggregate sees only the rows that the caller may read.
  The generated LogsQL uses `stats`, `sort` and `limit` only. It
  never emits `union` or `join`. RFC 0002 marks the VictoriaLogs scope as
  unverified (`rfcs/0002-restricted-source-access.md:128`), and this RFC adds
  no new way around it.
- **Injection.** A field goes through `queryescape.ClickHouseIdentifier` or
  `formatFieldName`. An alias matches a fixed pattern, a window comes from a
  closed table, and the compiler writes the limit and the percentile as
  numbers. No text from the request reaches the query unescaped.
- **Secrets, redaction and outbound requests.** Not affected.

## Limits and failure modes

| Bound | Value | What the user sees |
|---|---|---|
| Query length | 64 KiB, unchanged (`internal/logchefql/grammar.go:16`) | `QUERY_TOO_LONG` |
| Aggregates per query | 10 | `INVALID_AGGREGATION` at parse |
| Group keys | 5, with one `time` at most | `INVALID_AGGREGATION` at parse |
| `limit` stage | 1 to 10,000 | `INVALID_AGGREGATION` at parse |
| Buckets | 1,000 over the request range | A 400 that names the smallest window that fits |
| Groups on ClickHouse | 100,000, as `max_rows_to_group_by` with `throw`. A count of groups, not a memory bound | A 400 that says to add a filter or group by fewer fields |
| Memory on ClickHouse | `max_memory_usage` of 2 GiB per aggregation, or the source's own setting when that is lower | A 400 that says to add a filter or shorten the range. No rows return |
| Rows returned | The LIMIT rule of RFC 0003. The smallest of the `limit` stage, the request limit and the configured maximum. The default is 1,000 and the maximum is 100,000 (`internal/config/config.go:468`). A dashboard panel sends 1 to 10,000 | The "First N rows" chip when a cap cut rows. Nothing when the `limit` stage won |
| Response bytes | `max_response_bytes`, unchanged | The "Cut by size" chip |
| Time | The query timeout, unchanged | The timeout error of today |
| Concurrency | The query admission of today | Unchanged |

Group cardinality at the bound:

- **`grouped`, more groups than the row limit.** The default order is the
  first value descending, so the result is the top N groups, with the "First
  N rows" chip. A `limit` stage makes the cut the author's own, and no chip
  shows.
- **A shape with `time`, more rows than the row limit.** The newest buckets
  are cut. The chip shows, with the notice "The result has more than N rows.
  Use a longer interval or add a filter".
- **More than 100,000 groups on ClickHouse.** The query fails. A partial
  aggregation is wrong in every row, so none returns.
- **Memory on ClickHouse.** The group count does not bound memory. A scalar
  `count_distinct(request_id)` has one group, and its `uniqExact` state holds
  every distinct value. The query timeout bounds time and not peak memory. So
  every aggregation runs with `max_memory_usage`. The value is 2 GiB, or the
  source's `max_memory_usage` when that is lower, the rule that
  `queryTimeout` applies to `max_execution_time`
  (`internal/clickhouse/result_limits.go:116`). ClickHouse ends a query that
  passes the limit with an error, and the server maps it to the 400 above. A
  ClickHouse constraint on the user still wins over the query setting.
- **VictoriaLogs.** The LogsQL reference documents no memory limit for
  `stats`, and `count_uniq` holds every distinct value. Logchef cannot set a
  limit per query. The row limit, the timeout and the memory settings of the
  VictoriaLogs server are the only bounds.

The row probe of RFC 0003 applies, so `truncation` is proven. An alert must
not read a cut result, and RFC 0006 decides that rule for grouped alerts.

**Several replicas on Postgres.** The feature holds no state. Each request
compiles and runs on the replica that receives it. The dashboard result cache
keys on the compiled query, so an aggregated query caches as any other.

## Backend differences

Four things are portable and give the same result on both backends: `count()`
in every shape, `sum` and `avg` over fields whose non-empty values are all
numbers, group keys on fields that every row has, and `sort` and `limit`.

Places where the backends cannot agree. Each row is a decided behavior and
goes into the docs page.

| Topic | ClickHouse | VictoriaLogs |
|---|---|---|
| Number typing | The column type decides. Text is parsed with `toFloat64OrNull`, and text that is not a number is skipped | Every value is text. `sum` and `avg` skip non-numbers. `min`, `max` and `quantile` may compare a non-numeric string as text |
| Missing field in a number function | NULL, or the type default for a non-Nullable column, such as 0 for `UInt32`. The default takes part | Skipped, by the `if (NOT f:"")` filter |
| No numeric input at all | null, by `-OrNull` | null. The provider maps `NaN`, `""` and unparsed text to null |
| Missing field as a group key | `""` for a String, Map or JSON-string field. null for a `Nullable` column. Two different groups | `""`. No null group exists |
| `count_distinct` on empty values | `""` is not counted for a String field | Empty values are not counted |
| Percentiles | `quantile` samples. Two runs can differ in the last digits | `quantile` over the values in memory. Not equal to ClickHouse to the digit |
| Bucket alignment | The named timezone, with daylight-saving changes | One fixed offset for the whole range. Buckets shift by an hour after a daylight-saving change inside the range. RFC 0003's "UTC buckets" chip rule applies |
| Large integers | `sum` of a `UInt64` column is exact | `sum` is a float. Above 2^53 it loses digits |
| Result types | `count` is `UInt64`. `sum` keeps the column's integer type | `count` and `count_distinct` are exact digit strings declared `UInt64`. Every other value is a JSON number declared `Float64` |
| Group count and memory bounds | 100,000 groups and `max_memory_usage`, then an error | None per query |
| Nested fields | Map key, native JSON path, or `JSONExtractString` on a String column | A flat dotted field name |

ClickHouse and VictoriaLogs declare `logchefql_aggregation`
(`internal/datasource/clickhouse_provider.go:35`,
`internal/victorialogs/provider.go:158`) and run every construct. A Loki
source (RFC 0011) does not declare it in its first version, and its compiler
rejects an `AggregationNode` with `AGGREGATION_NOT_SUPPORTED`.

## Seams

**RFC 0003 (dashboards).** Consumed as a constraint. The result is its
frame, the mode is its `result` mode, and `by time` reads its `interval`
request field. Two statements of RFC 0003 change in scope, and neither
changes a decision.

- "LogchefQL returns log rows. Useful for tables"
  (`rfcs/0003-dashboards-v2.md:492`) becomes "log rows, or an aggregated
  frame".
- A VictoriaLogs frame has a null `backendType`. An aggregated LogchefQL
  result on VictoriaLogs has declared types, because the compiler wrote the
  query.

**Provides to RFC 0006 (alerting).**

- `core.CompileLogchefQL` with `AllowAggregation` returns the native query
  and an `AggregationInfo`.
- Two shapes suit an alert. `scalar` with one value gives one number.
  `grouped` gives one number per group, with the group keys as labels.
- A `time` key is legal in the language, and RFC 0006 decides whether an
  alert accepts it.
- No partial-aggregate mode. `count()` counts rows, and `avg` skips NULL and
  text that is not a number, so `sum` over `count()` is not the average. For
  the values 10 and NULL it gives 5 and not 10. A bucketed backtest can
  therefore combine `count` and `sum` buckets only, and RFC 0006 replays
  every other rule. This follows cross-RFC decision 2.

**Requires from RFC 0006.** The evaluator compiles the LogchefQL text at each
evaluation, with the lookback as the range, and both providers accept the
compiled query in `EvaluateAlert`. RFC 0006 states which shapes a condition
needs and the rule for a truncated grouped result. This RFC designs neither.

**Provides to RFC 0011 (Loki).** The `AggregationNode`, the single
capability flag and the `AGGREGATION_NOT_SUPPORTED` code. This RFC provides
no per-shape capability and no multi-query compile, per cross-RFC decision 1.
LogchefQL aggregations on Loki are deferred, and RFC 0011 rejects the node.

**RFC 0007 (saved views).** The Explorer's Chart and Table toggle is view
state, kept in component state here. RFC 0007 may store it.

**RFC 0008 (reports).** A report may run an aggregated query through
`core.RunLogchefQL` with `AllowAggregation` (`internal/core/query.go:312`).

**RFC 0002 (restricted sources).** No conflict. "Access and security" has the
reasoning. No conflict with another RFC is known.

## Audit events

None. The feature adds no stored object and no admin action. An aggregated
query is a query run with LogchefQL text, and it needs no event of its own.

## Alternatives rejected

| Alternative | Why not |
|---|---|
| A stats section in the request, outside the query text | It splits one question across two fields. A saved query, an alert, a CLI argument and an MCP call would each need the second field. One text is what makes the feature portable across surfaces |
| SQL-style clauses, `count() by service order by count desc limit 10` | A later `where` stage would not fit. Pipe stages extend with no grammar break |
| LogsQL spelling, `\| stats by (service) count() as n` | It ties the language to one backend, and it reads worse than the example Karan gave |
| `uniq` on ClickHouse for `count_distinct` | It is approximate and VictoriaLogs is exact, so one query would give two numbers on the same data. `max_memory_usage` bounds the memory of `uniqExact`, and the query fails with no rows at the bound |
| A general `percentile(f, n)` | It adds a numeric argument to the grammar for a need that five names cover |
| Zero rows for empty buckets from the server | It needs `WITH FILL` on ClickHouse and has no LogsQL form. The client already fills gaps for count frames |
| A reject check in each caller, in place of default deny | One forgotten check would run an aggregation where log rows are expected |
| A full LogchefQL parser in TypeScript | The server validates. The browser only splits stages and names tokens, and a shared fixture keeps the two in step at far lower cost |
| `rename` pipes in the LogsQL, in place of names written by the provider | A value alias that equals a `by` field fails in VictoriaLogs, and a swap of two names needs a temporary name. Internal names have no collision case |
| A composite series from every field key in a chart | It needs a new series identity in `buildPanelChart`. The table shows the same rows, and the author can drop a key |
| A new endpoint for aggregated results | RFC 0003 rules out a second result path. The cache, history, limits and streaming of the existing endpoint apply as they are |

## Implementation plan

P1 comes first. P2, P3 and P4 run in parallel after it. P5, P6, P7 and P8
run in parallel after P2. P9 is last. Each unit includes its tests.

### P1. Language core, default deny and result plumbing

- Depends on: nothing.
- Creates: `internal/logchefql/aggregation.go`, `aggregation_test.go`,
  `testdata/aggregation_cases.json`.
- Edits: `internal/logchefql/grammar.go`, `types.go`, `logchefql.go`, both
  generators, `internal/datasource/service.go`, `query.go`,
  `internal/core/query.go`, `internal/server/logchefql_handlers.go`,
  `query_stream.go`, `internal/cache/dashcache.go`.
- Change: the `Comma` token, the `PPipe` grammar, `AggregationNode`,
  `validateAggregation`, `AggregationInfo`, the three error codes, the
  capability constant and the `interval` request field. The compile request
  and result fields, `QueryRequest.Aggregation`, the `aggregation` object in
  the translate and query responses (streamed, buffered and cached), and the
  cache key. Both generators answer `AGGREGATION_NOT_SUPPORTED` when allowed
  and `AGGREGATION_NOT_ALLOWED` when not, so no backend runs one yet.
- Check: the fixture test passes, and every existing test in
  `internal/logchefql` passes unchanged. A test sends `x=1 | count()` to the
  tail, windowed and field-values paths and reads `AGGREGATION_NOT_ALLOWED`.
  `/logchefql/validate` returns `valid: true` for the same text. A handler
  test with a stub provider that compiles an aggregation reads `aggregation`
  from a streamed response, a buffered response and a cache hit, and reads
  two cache keys for two aliases.

### P2. ClickHouse compile and run

- Depends on: P1.
- Creates: `internal/logchefql/sql_aggregation_test.go`.
- Edits: `internal/logchefql/sql_generator.go`, `logchefql.go`,
  `internal/datasource/clickhouse_provider.go`,
  `internal/clickhouse/result_limits.go`, `query.go`,
  `internal/template/builtins.go`, `internal/core/query.go`,
  `internal/config/config.go`, `internal/server/query_errors.go`.
- Change: `GenerateAggregation`, the aggregation branch of
  `buildFullQueryFromTranslation`, the number-expression table, the alias
  rule, the bucket count check, the two query settings and the errors for the
  group limit and the memory limit. The provider declares the capability.
- Check: claims 1 to 7 first. A golden test per row of the construct table.
  An integration test in the style of `sql_generator_integration_test.go`
  runs the four shapes. `curl` of `/logchefql/query` with
  `level=error | count() by service` returns typed columns `service` and
  `count`. This is the first slice that a user can use, through the API, the
  CLI's JSON output and MCP.

### P3. VictoriaLogs compile and run

- Depends on: P1. Parallel with P2. P1 holds every shared file that P3 needs.
- Creates: `internal/logchefql/logsql_aggregation_test.go`.
- Edits: `internal/logchefql/logsql_generator.go`,
  `internal/victorialogs/querying.go`, `internal/victorialogs/provider.go`.
- Change: the `stats`, `sort` and `limit` emission with internal names. The
  provider builds `columns` from `QueryRequest.Aggregation`, writes rows
  under the output names, converts `Float64` cells to JSON numbers or null,
  and declares the capability.
- Check: claims 8 to 15 first. A golden test per row of the construct table,
  with `| count() as service by service as svc` and a swap of two key names
  in the fixture. A provider test against VictoriaLogs v1.52.0 runs the four
  shapes, an empty grouped result that still has columns, and `sum`, `avg`
  and `p95` with numeric, missing and non-numeric input. A claim that fails
  becomes a change to the construct table in this PR.

### P4. Editor language support

- Depends on: P1, for the fixture. Parallel with P2 and P3.
- Creates: `frontend/src/utils/logchefql/aggregation.ts` and its test.
- Edits: the four files in the table under "Web UI", and their tests.
- Check: `bun run test` reads the shared fixture and passes, and `bun run
  typecheck` passes. In the editor, `level=error | ` offers functions,
  `| sum(` offers number fields only, and `| count() by service` shows no
  unknown-field marker.

### P5. Explorer result view

- Depends on: P2, P4.
- Creates: `frontend/src/views/explore/components/AggregationResult.vue` and
  its test.
- Edits: `frontend/src/views/explore/LogExplorer.vue`,
  `frontend/src/stores/explore.ts`, `frontend/src/api/explore.ts`.
- Check: a component test per shape. One test has two hosts in one service
  and bucket, and reads the table with the chart toggle disabled. In the
  browser, `| count() by time` draws a chart with no histogram above it, a
  click on a group cell adds a condition, and **Add to dashboard** opens the
  builder with a `result` panel.

### P6. Dashboard panels

- Depends on: P2, P4. Parallel with P5. The VictoriaLogs check needs P3.
- Edits: `frontend/src/utils/dashboardRuntime.ts`,
  `frontend/src/stores/dashboards.ts`,
  `frontend/src/views/dashboards/components/PanelBuilderDrawer.vue`,
  `pkg/models/dashboards_v2.go`, and their tests.
- Check: a Go test that a `count` panel with an aggregation fails validation.
  A vitest that a LogchefQL `result` panel sends `interval`. A store test
  with a VictoriaLogs response for `sum`, `avg` and `p95`, one of them null,
  reads number columns marked `declared` and a null cell. A builder test
  with two `group` columns reads the table suggestion. In the browser,
  a panel with `| p95(duration_ms) by time, service` draws one line per
  service and follows the time picker.

### P7. CLI

- Depends on: P2. Parallel with P5, P6 and P8.
- Edits: `cli/crates/logchef-core/src/api/models.rs`,
  `cli/crates/logchef-cli/src/cli.rs`, `commands/query.rs`, `explain.rs`,
  `skills.rs`, and the CLI tests.
- Check: `just check-cli` passes. `logchef query 'level=error | count() by
  service'` prints a two-column table. `logchef query '| count() by time'`
  with no `--interval` prints the server's message and exits non-zero.

### P8. MCP and Ask Chef

- Depends on: P2. Parallel with P5, P6 and P7.
- Edits: `internal/mcp/logchefql.go`, `logs.go`, `analysis.go`, `prompts.go`,
  `resources.go`, `internal/askchef/prompt.go`, `tools.go`, `evidence.go`,
  and their tests.
- Check: an MCP test calls `query_logchefql` with an aggregation and reads
  `aggregation.shape`. A second test calls the window comparison tool with
  an aggregation and reads the deny message. `just test-short` passes.

### P9. Docs

- Depends on: P2, P3, P5, P6, P7, P8.
- Creates: `docs/src/content/docs/guide/aggregations.md`.
- Edits: `guide/search-syntax.md`, `features/dashboards.md`,
  `integration/cli.mdx`, `integration/mcp-server.mdx`, `CHANGELOG.md`.
- Check: the docs site builds. Every example on the new page is a case in
  the shared fixture.

## Open questions for Karan

The build proceeds on the defaults. The reviewer agreed with all eight
defaults of the first draft. Rows 5 and 8 carry its conditions, and row 9 is
new.

| # | Question | Default | Other choice |
|---|---|---|---|
| 1 | Exact or approximate distinct count | Exact, `uniqExact`, so both backends agree. `max_memory_usage` fails a query that needs too much memory | `uniq`, which uses less memory on ClickHouse and differs from VictoriaLogs |
| 2 | The function name | `count_distinct` | `uniq` or `dc` |
| 3 | Percentiles | `p50`, `p75`, `p90`, `p95`, `p99` | `percentile(f, n)` |
| 4 | `sort` and `limit` | Pipe stages | Clauses inside the aggregation |
| 5 | The Explorer's first view | The chart when the result has a `time` column and at most one field key, the table otherwise | Always the table |
| 6 | The bucket key word | `time`. A field named `time` then needs quotes | `bucket` |
| 7 | `limit` with `time` and a field | A parse error in the first version | "Top N series", which needs a subquery on ClickHouse and has no LogsQL form |
| 8 | The ClickHouse group count | 100,000, as a constant. It is not a memory bound | A config key under `[query]` |
| 9 | The ClickHouse memory limit for an aggregation | 2 GiB, as the config key `query.aggregation_max_memory_bytes` | No limit from Logchef, so only the ClickHouse profile applies |

## Unverified claims

No experiment ran, because the dev stack was off limits. P2 settles claims 1
to 7 and P3 settles claims 8 to 15, before any other work in the unit.
Claims 3, 8 and 14 are gates. If one fails, execution or truncation is wrong,
and the unit stops for a design change in this RFC.

| # | Claim | Experiment |
|---|---|---|
| 1 | ClickHouse accepts a quoted alias with a dot, such as `` AS `log_attributes.host` ``, and returns the column under that name | Run the generated `grouped` query on the dev table and read the column name |
| 2 | The query setting `max_rows_to_group_by = N` with the forced `throw` mode fails the query when the groups pass N, with an error that the server can map | Group by a high-cardinality field with N = 10 |
| 3 | Gate. The SQL parser behind the LIMIT rule accepts `GROUP BY`, `quantileOrNull(0.95)(x)` and `uniqExactIf`, and adds the probe limit (`internal/clickhouse/query_builder.go:288`) | Add the generated SQL of each shape as a case in `internal/clickhouse/query_builder_test.go` |
| 4 | ClickHouse resolves an alias inside `WHERE`, which is why the alias rule exists | Run `SELECT count() AS level FROM t WHERE level = 'error'`. If it runs correctly, drop the rule |
| 5 | `toFloat64OrNull(toString(x))` reads a number from a `Dynamic` JSON path | Run it on a table with a native JSON column |
| 6 | `toStartOfInterval(ts, INTERVAL 1 DAY, 'Asia/Kolkata')` returns local midnight | Read one bucket |
| 7 | A scalar `count_distinct` over many distinct values fails with the memory error and no rows when `max_memory_usage` is small, and a query setting above a lower ClickHouse constraint does not raise the limit | Run it with `max_memory_usage` of 50 MiB, then again as a user that has a constraint |
| 8 | Gate. VictoriaLogs v1.52.0 accepts `stats by (_time:5m offset 19800s, f)`, returns the bucket under `_time`, and aligns it to the offset. RFC 0003 marks the same form as inferred | Run it through `/select/logsql/query` and read two bucket starts |
| 9 | `func(f) if (f:*) as name` is the accepted order of filter and name. The order holds, but `f:*` fails on an integer column inside `if`, so the filter is `NOT f:""` (see the construct table) | Run one query |
| 10 | The `sort` pipe orders a numeric result column by number and not by text | Sort counts of 9, 10 and 100 |
| 11 | A `by` field that a row lacks groups under `""` | Group by a field that half the rows have |
| 12 | `min`, `max` and `quantile` compare a non-numeric string as text | Run them on a field with mixed values. If they skip it, the "Number typing" row gets simpler |
| 13 | `sum` with no numeric input returns `NaN`, and `min` and `max` return `""` | Run each on a filter that matches no row |
| 14 | Gate. `/select/logsql/query` applies its `limit` argument after the pipes, so the row probe counts result rows | Send `limit=2` with a group-by that has five groups |
| 15 | `count()` and `count_uniq` return a plain digit string, with no exponent and no decimal point | Read a count above 10 million |
