# RFC 0004: Log patterns and window compare in Explorer

Status: Proposed
Date: 2026-10-23
Revised: 2026-11-22
Authors: Karan Sharma

## Summary

Explorer shows rows. During an incident the first two questions are "what kinds of messages are there?" and "what changed?". Today the user answers both by scrolling rows and guessing.

This RFC adds two server-side summaries of the query and window that Explorer shows:

- **Patterns** groups the messages of the window into templates such as `connection to <IP> timed out after <*>`. Each template has a count over the whole window, a sparkline and one sample line. A click on a template filters Explorer to it.
- **Compare** compares the window with a baseline of equal length: the previous period, the same window yesterday, or the same window last week. It ranks field values and templates by how much their share of matching logs moved, and it shows the change in total volume.

Both run as aggregate queries in the log backend, on ClickHouse, VictoriaLogs and Loki. Neither reads rows into Logchef. A source names its message field. A source without one gets Compare without Patterns.

Three places own the decisions. `internal/logpattern` owns message extraction, the mask rules and baseline arithmetic, with no I/O. `datasource.Service` owns limits, ranking and the click filter, and calls one optional provider method, `CountGroups`. The Explorer URL owns the selected tab.

RFC 0006 builds new-pattern alert rules and triage on these operations. "Seams" names the unit that delivers each piece it needs.

## Problem

A user opens Explorer on `service="checkout" and level="error"` for the last hour and gets 800,000 rows. Explorer shows the newest 100. The histogram shows that the error rate went up at 10:20. The user still does not know which errors went up.

What the code offers today:

- The provider interface has search, histogram, field values and alert evaluation. It has no operation that groups rows by a derived key (`internal/datasource/service.go:19`).
- Field values count one field over one window (`internal/datasource/discovery.go:22`). On ClickHouse the query is one `GROUP BY value` over a `BETWEEN` window (`internal/clickhouse/field_values.go:120`). It has no second window and no share.
- The only comparison is the MCP tool `compare_windows`. It runs one filter in two windows with at most 500 rows each and subtracts the row counts (`internal/mcp/analysis.go:83`). Its schema says that `row_count` is the "Number of returned rows, not total matching logs" (`internal/mcp/analysis.go:55`). The percentage is null as soon as a sample reaches its limit (`internal/mcp/analysis.go:132`).
- A source records its timestamp field and its severity field, and nothing about its message (`pkg/models/source.go:290`). Explorer guesses the message column in the browser from a list of names (`frontend/src/lib/sourceFields.ts:27`). The server cannot answer "which field is the message".
- A LogchefQL aggregation (RFC 0005) counts by the value of a field. It cannot mask the value first, so it cannot group messages into templates.

Two answers help an on-call engineer first. A list of templates with counts turns 800,000 rows from 30 log statements into 30 lines. A diff against a normal period says "`service=payments` went from 2% to 41% of errors" and "`failed to connect to db host=<IP>` is new". Ask Chef, the MCP server and alerts need the same answers as cheap, bounded calls, so this RFC builds the operations once and every caller uses them.

## Goals and non-goals

Goals:

- Counts cover every matching row in the window, on all three backends, including VictoriaLogs windowed-search sources.
- The same message gives the same template on ClickHouse, VictoriaLogs and Loki.
- Common source shapes produce a short list of templates: a message column, JSON or logfmt in a string column, access-log lines, and messages that carry mixed letter-and-digit IDs.
- A source with no message field still gets Compare.
- A user goes from a template to its logs in one click.
- Compare ranks by change in share, so a small value with a large jump ranks above a large value that stayed flat.
- Every request has a timeout, an admission slot, a group cap and a read cap. A request that is too large fails with a message that tells the user to narrow it. It never returns partial counts as if they were complete.
- One implementation serves the HTTP API, the MCP tools, Ask Chef, the CLI and the alert rules of RFC 0006.

Non-goals for this RFC:

- Learned clustering such as Drain, custom mask rules per source, and a stored catalog of templates.
- Message keys other than `msg` and `message` inside JSON or logfmt.
- Patterns or Compare on ClickHouse native SQL. The user's SQL fixes its own time range and projection, so Compare cannot add a baseline to it. Native LogsQL without pipes and a native LogQL log query are supported.
- Sparklines in Compare. Compare rows show both counts and both shares.
- An exact pattern filter. A click filters by the literal text of the template. See "How a click filters".
- The new-pattern alert rule and triage on fire. RFC 0006 owns both, in its units P0 to P3.
- Template labels from a classification model. "Later: template labels" keeps the direction. No unit in this plan builds it.
- The tab as part of a saved view (RFC 0007) or of a share payload. The tab is a URL parameter only.
- Sample lines on Loki. A Loki metric query returns counts and no log line.

## Design

### User experience

Explorer gains a tab bar above the results: **Logs**, **Patterns** and **Compare**. **Logs** is today's view, with its table, compact and JSON modes unchanged. The tab bar is hidden during live tail and on a source without the `analysis` capability.

**Patterns.** The header reads "37 patterns, top 50 shown, 1,210 other (0.1%), 4,002 without a message". The `other` share is always shown, so a long tail is visible. Each row has a sparkline, the count, the share of the window and the template. Placeholders (`<*>`, `<IP>`, `<UUID>`) render as muted chips. A row expands to show one raw sample line. A click on a row adds a filter for that template and switches to **Logs**.

When a source has no message field, **Patterns** says why it is empty. A global admin sees a button such as "Use `body`", and other users see "Ask an admin to set the message field". On a provisioned source the button is replaced by "Set `meta_message_field` in the provisioning file", because the update route refuses a managed source (`internal/server/server.go:414`). **Compare** works on these sources.

**Compare.** A baseline select offers **Previous period**, **Same time yesterday** and **Same time last week**. Both windows appear as concrete timestamps. The report has three parts:

1. Volume: "812,345 vs 401,002 matching logs (+103%)".
2. Fields that shifted, such as "`service = payments`, 2.0% to 41.0%". A click adds `service="payments"` to the query. The user can add a field from the field sidebar with "Compare by this field", including a JSON path such as `attrs.method` and the path of a URL field.
3. Patterns that shifted, with a status of **new**, **grew**, **shrank** or **gone**. A click filters to the template. This part is absent when the source has no message field.

The URL records the tab as `...&tab=patterns` or `...&tab=compare&vs=day`. Back and Forward move between tabs and run no query. A copied link opens on the same tab.

**Large windows.** When the backend rejects a window as too large, the tab says "Add a filter or narrow the window to see patterns".

### Data shape: source message settings

Sources have `_meta_ts_field` and `_meta_severity_field` today (`pkg/models/source.go:290`). This RFC adds two settings next to them:

| Column | Type | Meaning |
|---|---|---|
| `_meta_message_field` | `TEXT NOT NULL DEFAULT ''` | The field that holds the message. Empty means the source has no message field. |
| `_meta_message_format` | `TEXT NOT NULL DEFAULT 'auto'`, with a check for `raw` and `auto` | How to read that field. See "Message extraction". |

Storage and wire:

- One migration per store, `add_source_message_settings`, with the next free number at build time. Each adds the two columns, then sets `_meta_message_field = '_msg'` for sources whose `source_type` is `victorialogs` or `loki` and whose field is empty. The `UPDATE` changes nothing on a second run. The down file drops both columns.
- `queries.sql` of both stores names the two columns in `CreateSource` and `UpdateSource` (`internal/store/sqlite/queries.sql:6`, `internal/store/postgres/queries.sql:6`). `sqlc.yaml` reads the migrations directory, so it needs no edit (`sqlc.yaml:5`).
- `models.Source` gains `MetaMessageField string` and `MetaMessageFormat logpattern.MessageFormat`, with the JSON keys `_meta_message_field` and `_meta_message_format`. `SourceResponse` carries both (`pkg/models/source.go:595`). The store parses the format once when it reads the row, and an unknown value is an error.
- `CreateSourceRequest` and `UpdateSourceRequest` gain `meta_message_field` and `meta_message_format` (`pkg/models/source.go:658`, `pkg/models/source.go:680`). `ApplyCommonSourceUpdates` applies them with `validateOptionalColumnName` (`internal/datasource/source_updates.go:15`).
- Provider rules. ClickHouse checks that the column exists and has a string type, in the check that `validateColumnTypes` runs for the timestamp and severity fields (`internal/datasource/clickhouse_provider.go:838`). VictoriaLogs and Loki default an empty field to `_msg` on create (`internal/victorialogs/provider.go:165`, `internal/loki/provider.go:138`). On Loki the field is `_msg` or a label name. An update with an empty string clears the field on every backend.
- Provisioning TOML accepts `meta_message_field` and `meta_message_format` on a source (`internal/config/provisioning.go:74`). Validation, the reconciler's change check and export carry them (`internal/provisioning/validate.go:140`, `internal/provisioning/reconciler.go:613`, `internal/provisioning/export.go:129`).
- The audit view `SourceFields` records both (`internal/audit/views.go:115`).
- The source form prefills the field from `MESSAGE_FIELD_ALIASES`, the list Explorer already uses to pick a message column (`frontend/src/lib/sourceFields.ts:27`). Logchef keeps one alias list. The server never guesses.

A source setting gives one answer to every caller: the UI, MCP, Ask Chef, the CLI and alerts. A request parameter would make each caller guess, and a server-side guess would pick the wrong column without saying so. Existing ClickHouse sources need an admin to set the field once.

| Source | Patterns | Compare fields | Compare patterns |
|---|---|---|---|
| Message field set | Yes | Yes | Yes |
| No message field, for example an access or event table | No, with an explanation | Yes | No |

### Data shape: `internal/logpattern`

The package is pure Go with no I/O. It holds the only copy of the rules, and it renders them for each backend.

**Message extraction.** Patterns needs the free text of each row. In many sources that text is the value of `msg` or `message` inside a JSON or logfmt string, next to timestamps, request IDs and user IDs. Masking the whole string produces a template per ID. Masking only the inner message produces one template per log statement.

```go
package logpattern

// MessageFormat says how to read the message field of a source.
type MessageFormat string

const (
	FormatRaw  MessageFormat = "raw"  // the field is the message
	FormatAuto MessageFormat = "auto" // JSON, then logfmt, then raw text
)

// MessageKeys are the keys read inside JSON and logfmt bodies, in order.
var MessageKeys = []string{"msg", "message"}

func ParseMessageFormat(s string) (MessageFormat, error)
```

For `auto`, each row resolves in this order:

1. If the field is valid JSON with a non-empty top-level `msg` or `message`, that value is the message.
2. Else, if the field has a logfmt `msg=` or `message=` pair, quoted or not, its value is the message.
3. Else the whole field is the message.

**The mask rules.**

```go
// Rule replaces every match of Pattern with Placeholder. Placeholders contain
// no digits, so a later digit rule never rewrites an earlier placeholder.
type Rule struct {
	Name        string
	Pattern     string // RE2. ClickHouse (re2), VictoriaLogs and Loki (Go regexp) read it the same way.
	Placeholder string
}

// Rules is ordered: cleanup first, named shapes next, generic tokens last.
var Rules = []Rule{
	{"ansi", `\x1b\[[0-9;]*m`, ""},
	{"uuid", `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`, "<UUID>"},
	{"ipv4", `\b[0-9]{1,3}(?:\.[0-9]{1,3}){3}\b`, "<IP>"},
	{"word_with_digit", `[A-Za-z0-9_]*[0-9][A-Za-z0-9_]*`, "<*>"},
	{"placeholder_run", `<\*>(?:[^A-Za-z<]{1,3}<\*>)+`, "<*>"},
}
```

The word rule is the important one. IDs in real logs mix letters and digits: `AB1234`, `user107`, `FQ12_holdings_8c9f`, `v0.24.5`, `2026-10-23T13:10:44`. A rule that masks only digits leaves the letters, so each ID still makes its own template. The word rule replaces the whole word. The run rule then folds sequences such as `<*>.<*>.<*>` and `<*>-<*>-<*>T<*>` into one `<*>`, so dates, versions and IPv6 addresses become one placeholder. Hyphens and colons are not word characters, so `api-gateway-<*>` keeps its readable prefix.

A field longer than 4,096 bytes and a row whose extracted message is empty are counted and not masked. The response reports them as `oversized` and `missing`, separately from `other`. The length guard bounds the regex cost per row.

**Functions.**

```go
// Mask is the Go reference for one key. Tests compare every backend with it.
func Mask(value string, t Transform, format MessageFormat) (key string, kind Kind) // kind: message, missing, oversized

func ClickHouseKeyExpr(fieldExpr string, t Transform, format MessageFormat) string // kind and key expressions
func LogsQLKeyPipes(quotedField string, t Transform, format MessageFormat) string  // sets _lc_kind and _lc_key
func LogQLKeyStages(field string, t Transform, format MessageFormat) string        // sets _lc_kind and _lc_key

// Clause returns the LogchefQL click filter of a template, or "" when the
// template has no usable literal run.
func Clause(field, template string) string

type BaselineKind string // "previous", "day", "week"
func ResolveBaseline(current TimeRange, kind BaselineKind) (TimeRange, error)
func Shift(cur, curTotal, base, baseTotal int64) *float64 // nil when a total is 0
func StatusOf(cur, base, baseTotal int64, shift *float64) Status
```

`Transform` is `value`, `message` or `path`. `value` uses the field as the key. `message` extracts with the source's format and applies `Rules`. `path` cuts the value at `?` and applies `Rules`, which turns `/orders/8812/items?page=2` into `/orders/<*>/items`. `TimeRange` is declared in `logpattern` and aliased in `datasource`, so the pure package does not import the provider package.

The renderers differ per backend. ClickHouse gets nested `replaceRegexpAll` calls around a `multiIf` of `isValidJSON`, `JSONExtractString` and `extractGroups`. VictoriaLogs gets `unpack_json` and `unpack_logfmt` with a `result_prefix`, conditional `format` pipes that pick the first non-empty value, and one `replace_regexp` pipe per rule. Loki gets `| json` and `| logfmt` with named extractions, `| drop __error__, __error_details__`, and `label_format` templates that call `regexReplaceAllLiteral` once per rule. JSON and logfmt escapes differ between the three parsers, so the fixture corpus includes escaped quotes, unquoted logfmt values, nested JSON and invalid JSON.

We use our own list instead of each backend's normalizer for three reasons. The template text is the same on all backends, and a conformance test proves it against `Mask`. The word rule masks IDs that `collapse_nums` and digit-only rules leave in place. When a mask is wrong, one file changes.

`internal/logchefql` owns LogchefQL syntax, so two additions land there. `Condition(field string, op Operator, value string) string` formats one comparison with the quoting rules of the grammar, as `buildCondition` does in the browser (`frontend/src/utils/logchefql/filters.ts:51`). `Clause` uses it, Compare uses it for field rows, and RFC 0006 uses it for triage. `FieldSQL`, `FieldLogsQL` and `FieldLogQL` export the field compilers that the generators already have (`internal/logchefql/sql_generator.go:265`, `internal/logchefql/logsql_generator.go:406`, `internal/logchefql/logql_generator.go:545`), so a key such as `attrs.method` reads a JSON path the same way the query editor does.

### Data shape: the provider contract

Patterns and Compare run one query shape. Each groups the matching rows by a key, counts the rows of the current window and of the baseline, keeps a sample, and returns the top groups. Providers get one optional interface with two methods. The types live in a new `internal/datasource/analysis.go`.

```go
package datasource

// TimeRange is one window. ClickHouse and VictoriaLogs count Start <= t < End.
// Loki counts Start < t <= End. Adjacent ranges never share a row.
type TimeRange = logpattern.TimeRange // struct{ Start, End time.Time }

// AnalysisFilter is the run's filter with template variables already substituted.
type AnalysisFilter struct {
	Query              string
	Language           models.QueryLanguage // logchefql, logsql without pipes, or a logql log query
	ExtraStreamFilters []string             // VictoriaLogs only, as in QueryRequest
}

type GroupKey struct {
	Field     string // a column or label, or column.path for a JSON key
	Transform logpattern.Transform
}

type GroupCountRequest struct {
	Filter   AnalysisFilter
	Key      GroupKey
	Current  TimeRange
	Baseline *TimeRange // set: count both windows and order by share shift
	Buckets  int        // > 0: per-bucket counts of Current for the returned groups
	MinCount int64      // > 0: drop a group below it in both windows, before Limit
	Limit    int
}

type GroupCounts struct {
	Groups                      []GroupCount
	CurrentTotal, BaselineTotal int64 // every matching row of the window, excluded rows included
	CurrentExcluded             Excluded
	Distinct                    int64 // message groups before MinCount and Limit
}

type Excluded struct{ Missing, Oversized int64 }

type GroupCount struct {
	Key               string
	Sample            string // "" on Loki
	Current, Baseline int64
	Series            []int64 // len == Buckets, or nil
}

// GroupCounter is optional. Service returns ErrOperationNotSupported for a
// provider that does not implement it, as it does for LogContextProvider.
type GroupCounter interface {
	CountGroups(ctx context.Context, source *models.Source, req GroupCountRequest) (*GroupCounts, error)
	DimensionFields(ctx context.Context, source *models.Source, window TimeRange, max int) ([]string, error)
}
```

The provider orders groups before it applies `Limit`. With a baseline the order is the absolute share shift, then the current count, then the key. Without one it is the current count, then the key. A provider returns only groups that pass `MinCount`. Totals always come back when any row matched, even when no group passes.

A provider that implements `GroupCounter` adds `CapabilityAnalysis Capability = "analysis"` to its descriptor (`internal/datasource/service.go:55`). The capability says that the provider can count groups. Patterns availability is a property of the source, so the UI reads `_meta_message_field` from the source response.

Two errors join `ErrOperationNotSupported` in `internal/datasource/query.go`: `ErrMessageFieldNotSet` and `ErrAnalysisTooLarge`. A filter that a backend cannot analyse is a `ValidationError` on the field `query`.

### Service methods and policy

```go
type PatternsRequest struct {
	Filter  AnalysisFilter
	Window  TimeRange
	Limit   int // 0 means 50, at most 50
	Buckets int // 0 means no sparkline, at most 30
}

type CompareRequest struct {
	Filter   AnalysisFilter
	Window   TimeRange
	Baseline logpattern.BaselineKind
	Fields   []GroupKey // added after the default fields
	Limit    int        // rows per list, 0 means 20, at most 20
}

func (s *Service) Patterns(ctx context.Context, id models.SourceID, req PatternsRequest) (*PatternsResult, error)
func (s *Service) ComparePeriods(ctx context.Context, id models.SourceID, req CompareRequest) (*CompareResult, error)
```

`PatternsResult` and `CompareResult` hold the fields of the two responses under "HTTP API". `Service` owns the policy: the top 50 templates, 30 sparkline buckets, the top 20 compare rows, up to 8 compared fields, the minimum count of 5, baseline resolution, statuses and the click filter. If each provider owned `Patterns()` and `Compare()`, three providers would contain the same rules, and the copies would drift. Both methods take no user. RFC 0006 calls them from a job, as `EvaluateAlert` is called today (`internal/datasource/service.go:471`).

`Patterns` returns `ErrMessageFieldNotSet` for a source without a message field, calls `CountGroups` once with the `message` key, and computes `other` as the total minus the returned counts and the excluded rows.

`ComparePeriods` runs these steps in order, one backend query at a time:

1. Resolve the baseline and reject an invalid one. See "Compare semantics".
2. When the source has a message field, count the `message` key over both windows. A failure here fails the request.
3. Pick the fields: the result of `DimensionFields`, then the caller's fields, without duplicates, at most 8.
4. Count each field over both windows. Each field query gets at most 8 seconds. A field whose query times out, hits the group cap, or cannot start before the deadline goes into `SkippedFields` with a reason: `timeout`, `too_many_values` or `deadline`.

The volume totals come from the first query that returns. All queries count the same matching rows, so their totals agree. When no query returns, the request fails with the first error. When the source has no message field and no field to compare, the request is a 400 that asks the user to add a field.

`core` adds `GetPatterns` and `ComparePeriods` next to `GetAllFieldValues` (`internal/core/logs.go:87`). Each takes an `access.AuthorizedSource`, prepares the filter and maps a missing source to `ErrSourceNotFound`. `PrepareAnalysisFilter` does the preparation for every caller. A LogchefQL query loses its aggregation pipe through `logchefql.WithoutAggregation`, as field values do (`internal/core/logs.go:57`), and gets its variables from `template.SubstituteLogchefQLVariables` (`internal/template/logchefql.go:13`). A native query goes through the dialect substitution that the histogram uses (`internal/core/query.go:450`). `core.AnalysisTimeout` is 30 seconds, next to `HistogramTimeout` (`internal/core/query.go:27`).

### Compare semantics

`logpattern.ResolveBaseline` turns the current window `[s, e)` and a baseline kind into a baseline window:

| Kind | URL `vs` | Baseline |
|---|---|---|
| `previous` | omitted | `[s - d, s)`, where `d = e - s` |
| `day` | `day` | `[s - 24h, e - 24h)` |
| `week` | `week` | `[s - 7d, e - 7d)` |

The offsets are exact UTC durations. Across a daylight-saving change, "same time yesterday" is 24 hours earlier, not the same wall-clock hour. `ResolveBaseline` rejects a baseline that overlaps the current window, for example a 2-day window compared with `day`. `Service` also rejects a baseline that starts before the retention cutoff of a source with `ttl_days > 0`. Both windows are truncated to milliseconds.

For each group, `shift = current / currentTotal - baseline / baselineTotal`. Shift is the change in share, from -1 to 1. The UI shows it in percentage points: 2% to 41% is `+39 pp`. The backend applies the order before the limit, so a small value with a large jump is never cut by a top-N on volume. Share, not raw count, makes a rise in one status code visible when total traffic falls.

Two rules keep the report honest:

- A group with fewer than 5 rows in both windows is dropped. A single new log line does not rank as the biggest change.
- If the baseline window has no matching rows, baseline shares and shifts are null and the status is `baseline_empty`, not `new`. The UI says "No matching logs in the baseline window". This covers expired data and sources that started recently.

Statuses are `new` (baseline count 0, baseline total above 0), `gone` (current count 0), `grew`, `shrank` and `baseline_empty`.

`DimensionFields` picks the default fields, because during an incident the user does not yet know which field matters:

- ClickHouse reads the cached schema: the severity field first, then `LowCardinality(String)`, `Enum`, `Bool` and integer columns of 16 bits or fewer, such as a status code, without the timestamp and message columns.
- VictoriaLogs calls `/select/logsql/facets` with `limit=1`, which skips high-cardinality fields. The provider calls that endpoint for the sidebar today (`internal/victorialogs/querying.go:647`).
- Loki lists the stream labels under the source's selector, with the severity field first (`internal/loki/field_values.go:249`).

### HTTP API

```
POST /api/v1/teams/:teamID/sources/:sourceID/logs/patterns
POST /api/v1/teams/:teamID/sources/:sourceID/logs/compare
```

Both routes sit next to `/logs/histogram` (`internal/server/server.go:526`). Each uses `registerLimited` with `queryLimiter` and `requireTeamHasSource(models.TokenScopeLogsRead)`. No new token scope exists. The handlers live in a new `internal/server/analysis_handlers.go`.

The request names its fields as the other log routes do:

```json
{
	"query": "service=\"checkout\" and level=\"error\"",
	"query_language": "logchefql",
	"variables": [],
	"extra_stream_filters": [],
	"start_time": "2026-10-22T10:00:00Z",
	"end_time": "2026-10-22T11:00:00Z",
	"baseline": "previous",
	"fields": [{ "field": "attrs.method", "transform": "value" }, { "field": "url", "transform": "path" }]
}
```

`start_time` and `end_time` are RFC 3339 and required. `query` may be empty, which means every log of the source. `baseline` and `fields` apply to `/logs/compare` only. `baseline` defaults to `previous`. `fields` is optional, holds at most 8 entries and adds to the defaults.

The Patterns response:

```json
{
	"status": "success",
	"data": {
		"message_field": "body",
		"message_format": "auto",
		"start_time": "2026-10-22T10:00:00Z",
		"end_time": "2026-10-22T11:00:00Z",
		"bucket_seconds": 120,
		"total": 812345,
		"missing": 4002,
		"oversized": 17,
		"other": 1210,
		"distinct": 37,
		"patterns": [
			{ "template": "connection to <IP> timed out after <*>", "count": 402113, "sample": "connection to 10.0.4.7 timed out after 3000ms", "series": [13021, 13400], "filter": "body~\"connection to \" and body~\" timed out after \"" }
		]
	}
}
```

The Compare response:

```json
{
	"status": "success",
	"data": {
		"current": { "start_time": "2026-10-22T10:00:00Z", "end_time": "2026-10-22T11:00:00Z", "total": 812345 },
		"baseline": { "kind": "previous", "start_time": "2026-10-22T09:00:00Z", "end_time": "2026-10-22T10:00:00Z", "total": 401002 },
		"patterns": {
			"distinct": 37,
			"rows": [{ "key": "failed to connect to db host=<IP>", "sample": "failed to connect to db host=10.0.4.7", "current": 9120, "baseline": 0, "current_share": 0.0112, "baseline_share": 0, "shift": 0.0112, "status": "new", "filter": "body~\"failed to connect to db host=\"" }]
		},
		"fields": [
			{ "field": "service", "transform": "value", "distinct": 14, "rows": [{ "key": "payments", "current": 333061, "baseline": 8020, "current_share": 0.41, "baseline_share": 0.02, "shift": 0.39, "status": "grew", "filter": "service=\"payments\"" }] }
		],
		"skipped_fields": [{ "field": "trace_id", "reason": "too_many_values" }]
	}
}
```

`patterns` is absent when the source has no message field. A share or a shift is `null` when its window has no matching rows. A field row of the `path` transform has no `filter`. Counts are JSON numbers. Log counts per window stay far below 2^53, so the frontend needs no `bigint`. Templates and samples are cut to 2,000 characters.

Errors go through `classifyQueryError` (`internal/server/query_errors.go:35`). It gains one case for each new error. A `ValidationError` already maps to 400.

| Error | Status | Message |
|---|---|---|
| `ErrMessageFieldNotSet` | 400 | Set a message field for this source to see patterns. |
| `ErrAnalysisTooLarge` | 422 | Too many logs or patterns in this window. Narrow the time range or add a filter. |
| `ValidationError` for ClickHouse SQL | 400 | Patterns and Compare need a LogchefQL query on this source. |
| `ValidationError` for LogsQL with a pipe | 400 | Patterns and Compare need a LogsQL filter without pipes. |
| `ValidationError` for a LogQL metric query | 400 | Patterns and Compare need a LogQL log query. |
| `ValidationError` for the baseline | 400 | The baseline overlaps the current window, or starts before the source's retention. |

Timeouts, cancellations, admission limits, upstream errors and a missing source use the existing mappings. A 500 never contains backend error text.

### ClickHouse queries

The SQL is built and run in a new `internal/clickhouse/analysis.go`, next to the field-values queries. A thin `CountGroups` in `internal/datasource/clickhouse_analysis.go` maps the request, as `GetFieldValues` does (`internal/datasource/clickhouse_provider.go:478`). Identifiers go through `ValidateIdentifier` and `queryescape.ClickHouseIdentifier`. The regex literals are constants from `logpattern`. The only user text in the SQL is the `WHERE` fragment that `logchefql.Translate` compiles (`internal/clickhouse/field_values.go:50`). Windows use `>= start AND < end`, not the `BETWEEN` of the field-values query, so adjacent windows never count a boundary row twice.

Patterns and Compare share one statement. `m` is `ifNull(<message field>, '')`, `text` is the extracted message, and `logpattern.ClickHouseKeyExpr` renders `kind` and `k`:

```sql
SELECT kind, k, sample_line, cur, base, series,
	sum(cur) OVER () AS cur_total,
	sum(base) OVER () AS base_total,
	sumIf(cur, kind = 'missing') OVER () AS cur_missing,
	sumIf(cur, kind = 'oversized') OVER () AS cur_oversized,
	countIf(kind = 'message') OVER () AS distinct_keys,
	kind = 'message' AND (cur >= <min> OR base >= <min>) AS ok
FROM (
	SELECT
		multiIf(length(m) > 4096, 'oversized', length(text) = 0, 'missing', 'message') AS kind,
		if(kind = 'message', <masked text>, '') AS k,
		anyIf(substring(m, 1, 1024), <ts in current>) AS sample_line,
		countIf(<ts in current>) AS cur,
		countIf(<ts in baseline>) AS base,
		countResample(0, <buckets>, 1)(intDiv(toUnixTimestamp64Milli(toDateTime64(<ts>, 3)) - <startMs>, <stepMs>)) AS series
	FROM <db>.<table>
	WHERE (<ts in current> OR <ts in baseline>) AND (<compiled LogchefQL>)
	GROUP BY kind, k
)
ORDER BY ok DESC, <shift or cur> DESC, cur DESC, k
LIMIT <limit>
```

The `ok` test is a sort key and not a `WHERE`, so the statement returns a row whenever any row matched, and that row carries the totals. The provider drops rows whose `ok` is 0. `countResample` returns one array per group, so the group count does not grow with the number of buckets. The `series` column is present only when `Buckets > 0`, which the service sets only without a baseline. A field key uses `ifNull(toString(<field expression>), '')` as `k` and has one kind. Without a baseline, `base` is 0 and the order is `cur`. With one, the order is `abs(ifNull(cur / nullIf(cur_total, 0) - base / nullIf(base_total, 0), 0))`.

The statement runs through `queryOwnWithSettings` (`internal/clickhouse/client.go:247`), which sends every overflow mode as `throw` (`internal/clickhouse/result_limits.go:27`). The extra settings are `max_rows_to_group_by = 50000`, `max_rows_to_read = 20000000`, `max_bytes_to_read = 1073741824` and `max_memory_usage = 536870912`. Each is the lower of this value and the source's own setting, as `boundAggregation` computes for an aggregation (`internal/clickhouse/result_limits.go:94`). `max_execution_time` is the remaining deadline. ClickHouse codes 158, 307 and 241 map to `ErrAnalysisTooLarge` through a new `IsResourceLimitError`, next to `IsReadLimitExceededError` (`internal/clickhouse/query.go:377`).

### VictoriaLogs queries

The LogsQL is built in a new `internal/victorialogs/analysis.go`. Every request goes through `applyScopeFilters`, which carries the source's scope (`internal/victorialogs/querying.go:1171`), and through `readQueryRows` with a byte cap and a row cap (`internal/victorialogs/querying.go:241`). A cut response is `ErrAnalysisTooLarge`. On an optimizer source each request takes a slot from the source's window limiter (`internal/victorialogs/windowed.go:47`), and a window wider than `max_window_seconds` is rejected. A native LogsQL filter that contains a pipe is rejected, with `splitTopLevelPipes` as the test (`internal/victorialogs/querying.go:1430`). Logchef does not drop the pipes, because the counts would no longer match the table.

The first request returns the groups and the totals. Each window is written into the query as `_time:[start, end)`:

```
<filter> (_time:[cs, ce) or _time:[bs, be))
| len(<field>) as _lc_len | copy <field> _lc_sample
| <logpattern.LogsQLKeyPipes(field, transform, format)>   # sets _lc_kind and _lc_key
| stats by (_lc_kind, _lc_key)
	count() if (_time:[cs, ce)) cur,
	count() if (_time:[bs, be)) base,
	any(_lc_sample) sample
| total_stats sum(cur) cur_total, sum(base) base_total, count() groups
| math abs((cur / cur_total default 0) - (base / base_total default 0)) as shift,
	min(1, floor(max(cur, base) / <min>)) as ok
| sort by (_lc_kind desc, ok desc, shift desc, cur desc, _lc_key) | limit <limit + 2>
```

`LogsQLKeyPipes` blanks the text of an oversized row before the mask pipes run, so the regex never reads more than 4,096 bytes. The `missing` and `oversized` groups have one row each. They sort first, because their kind names sort after `message`, and `limit + 2` leaves room for them. Without a baseline the `base` terms and the `shift` are left out. With a `MinCount` of 0 the `ok` term is left out.

When `Buckets > 0`, a second request counts the returned templates per bucket with `stats by (_time:<step>, _lc_key) count()`, filtered to those templates. Its result has at most `limit × 30` rows. The two requests can see different late-arriving rows, so a sparkline's sum can differ from the template's count. The count comes from the first request.

VictoriaLogs limits the memory of a `stats` pipe on the server side. Logchef cannot set a per-query memory cap there. Operators size the VictoriaLogs memory limit for this workload.

### Loki queries

The LogQL is built in a new `internal/loki/analysis.go`. The log query comes from `filterQuery`, which compiles LogchefQL from the source's selector and parser, or takes a native log query as written (`internal/loki/field_values.go:163`). A native metric query fails Loki's parser inside `count_over_time`, and the provider returns a `ValidationError`, as field values do (`internal/loki/field_values.go:288`).

Every count is a metric query of this shape, which the provider already sends for field values (`internal/loki/field_values.go:58`):

```
sum by (<labels>) (count_over_time(<log query> <key stages> [<width>ms]))
```

For a `value` key, `<labels>` is the label name and there are no key stages. For a `message` or `path` key, `<labels>` is `_lc_kind, _lc_key`, and `logpattern.LogQLKeyStages` renders the stages:

```
| json _lc_j1="msg", _lc_j2="message" | logfmt _lc_l1="msg", _lc_l2="message"
| drop __error__, __error_details__
| label_format _lc_m=`{{ if gt (len __line__) 4096 }}{{ else }}{{ default (default (default (default __line__ ._lc_l2) ._lc_l1) ._lc_j2) ._lc_j1 }}{{ end }}`
| label_format _lc_kind=`...`, _lc_key=`{{ regexReplaceAllLiteral "<rule 5>" (... (regexReplaceAllLiteral "<rule 1>" ._lc_m "") ...) "<*>" }}`
```

The provider does not use `topk`. It needs every group to compute totals, the distinct count and the order, so it reads all groups and ranks them in Go:

- **Patterns** runs the histogram plan over the window, grouped by the two labels, with a width of the window divided by `Buckets`. `histogramRun.count` already returns counts per bucket and group (`internal/loki/histogram.go:257`). The count of a template is the sum of its buckets. One plan is at most three Loki requests.
- **Compare** runs one instant query per window, at `Current.End` and at `Baseline.End`, and merges the two results by key.

A Loki window is open at its start and closed at its end, as in the histogram (`internal/loki/histogram.go:84`). A metric query returns no log line, so `Sample` is empty. For a message field other than `_msg`, the stages read that label in place of `__line__`.

Loki fails a metric query that returns more series than the tenant's `max_query_series`, which is 500 by default (`rfcs/0011-loki-datasource.md:976`). That limit is the group cap on Loki. The provider maps that 400 to `ErrAnalysisTooLarge`. It recognises the error by the text "reached for a single query" in Loki's message.

### How a click filters

The server returns each row's `filter` as LogchefQL. All three backends compile LogchefQL, and Explorer already appends LogchefQL when the user adds a filter from the field sidebar (`frontend/src/views/explore/LogExplorer.vue:723`). A field row of the `value` transform gets `logchefql.Condition(field, "=", value)`.

`logpattern.Clause` builds a template's filter from the literal text between placeholders. Each literal run of three or more characters becomes `field~"text"` on the source's message field, and the six longest runs are kept in template order. A run that contains a quote or a backslash is dropped, because JSON and logfmt escape those characters in the stored field. A template with no usable run, such as `<*> <*>`, has no click.

LogchefQL `~` is a case-insensitive substring match on all three backends (`internal/logchefql/sql_generator.go:439`, `internal/logchefql/logsql_generator.go:345`, `internal/logchefql/logql_generator.go:337`). Every message that masks to a template contains each literal run of that template, and the stored field contains the message, so the filter matches every row of the pattern. It can also match a few similar rows. The Logs view after a click can therefore show more rows than the template's count. The row says so: "Filters by the template's text. Similar lines can match."

The click also freezes the window. Explorer replaces a relative window such as "last 1h" with the run's resolved absolute window, so the count after the click is from the same period as the template.

An exact filter needs a regex operator in LogchefQL, such as `=~`. With that operator, only `logpattern.Clause` changes. The API and the frontend stay the same.

### Clients, docs and metrics

| Where | Change |
|---|---|
| Web UI, Explorer | The tab bar, the Patterns list, the Compare report, "Compare by this field". See "Web UI". |
| Web UI, source form | Message field and format inputs on the three source forms. |
| CLI | `logchef patterns` and `logchef compare`, with the flags of `logchef histogram` and `--output json`. |
| MCP | `log_patterns` and `what_changed` replace `compare_windows`. See "MCP and Ask Chef". |
| Ask Chef | Both tools join `allowedTools`. `compare_windows` leaves it. |
| Provisioning TOML | `meta_message_field` and `meta_message_format` on a source. |
| Docs | A new feature page, and edits to the provisioning, MCP, CLI and audit pages. |
| Metrics | No new metric. The HTTP request metrics carry the two routes (`internal/metrics/metrics.go:25`). |
| Alerts, reports, dashboards | Not affected by this RFC. RFC 0006 adds the alert rule. |

### Web UI

**The tab lives in the URL.** The parameter is `tab`, because `view` already names a saved view (`frontend/src/lib/exploreLocation.ts:37`). `frontend/src/lib/exploreLocation.ts` adds:

```ts
export type BaselineKind = 'previous' | 'day' | 'week'

export type ExploreTab =
	| { kind: 'logs' }
	| { kind: 'patterns' }
	| { kind: 'compare'; baseline: BaselineKind }

export function parseExploreTab(query: QueryParams): ExploreTab
export function tabParams(tab: ExploreTab): Record<string, string>
```

`normalizeQueryParams` does not read `tab` and `vs`, so `sameExploreLocation` ignores them (`frontend/src/lib/exploreLocation.ts:111`). A tab change is never a new location and never runs the query. The navigator stays the only URL writer. It exposes `tab` as a computed value from the route and `setTab(tab)`, which pushes a history entry with the current query plus `tabParams(tab)`. The route watcher then finds the location already shown and returns. `record()` merges `tabParams` of the current tab into every query it writes (`frontend/src/composables/useExplorerNavigator.ts:275`), so Run, zoom, window changes and copied links keep the tab.

**The data hangs off the shown run.** A new composable, `useRunAnalysis(execution, tab, compareFields)`, fetches Patterns or Compare for `runs.shown` and the tab. It builds the request from the `Execution`, not from the editor: the query text and mode, `variables`, `streamFilters`, and the resolved start and end (`frontend/src/stores/exploreRuns.ts:36`). A new run, a zoom, a window change or a tab change aborts the request in flight. It keeps one result per tab for the current execution, so switching back does not fetch again. It aborts on KeepAlive deactivation and on scope dispose (`frontend/src/views/explore/LogExplorer.vue:822`). No Pinia store is added, and nothing copies the singleton pattern of the histogram store (`frontend/src/stores/exploreHistogram.ts:17`).

The state is a discriminated union:

```ts
export type RunAnalysis =
	| { status: 'idle' }
	| { status: 'unsupported'; reason: 'native-sql' | 'no-message-field' | 'too-large' }
	| { status: 'loading' }
	| { status: 'patterns'; data: PatternsResult }
	| { status: 'compare'; data: Comparison }
	| { status: 'failed'; message: string }
```

`native-sql` and `no-message-field` are decided in the browser from the run's mode and the source. `too-large` is the server's 422.

**Components.** The new files are `frontend/src/api/analysis.ts` (one client that unwraps the envelope once), `ExploreViewTabs.vue`, `ExploreAnalysis.vue` (states and empty states), `PatternsList.vue` and `CompareReport.vue`, all under `frontend/src/views/explore/components/`. Sparklines reuse `SourceSparkline.vue` (`frontend/src/components/visualizations/SourceSparkline.vue:8`). Lists reuse `SearchInput`, `useTableSearchSort`, `LoadingState` and `EmptyState` from `frontend/src/components/layout/`. `LogExplorer.vue` gains the tab bar, one `v-if` and two click handlers. The part of `handleAddFieldFilter` that appends a LogchefQL condition is extracted, so the sidebar and both tabs share it. The field sidebar gains "Compare by this field". The added fields live in the component for the open location and are not in the URL. New strings go into all 11 catalogs in `frontend/src/i18n/locales/`.

**Capability.** The tabs check `hasSourceCapability(source, 'analysis')` (`frontend/src/lib/queryMetadata.ts:61`). The Patterns tab also checks `source._meta_message_field`.

### MCP and Ask Chef

MCP gets two tools in `internal/mcp/analysis.go`, each with `logs:read` in `toolScopes` (`internal/mcp/server.go:155`):

- `log_patterns` takes `team_id`, `source_id`, `query`, `start_time`, `end_time` and `limit` (default 20, at most 50). It returns the totals and the templates with count, share, sample and filter. It asks for no sparkline.
- `what_changed` takes the same window and filter, plus `baseline` and `fields`. It returns the Compare result.

Each tool calls the `core` wrapper inside one admission slot of a new `QueryClassAnalysis` (`internal/mcp/server.go:30`), and records its read through `startRead` (`internal/mcp/reads.go:17`). `query` is a LogchefQL filter, as in the other tools.

Ask Chef adds both names to `allowedTools` with a reading function that returns `Evidence` for the filter and window (`internal/askchef/tools.go:43`). The existing `compare_windows` tool compares two samples of at most 500 rows. The new tool replaces it, and `compare_windows` is deleted in the same change, with its entry in `toolScopes`, its Ask Chef reading and its mention in the prompt. LP13 updates the docs pages that name it.

### What the dev stack showed

These queries ran against the dev stack on 2026-10-22 and 2026-10-23, for the first version of this RFC. They prove that the rules and the ClickHouse and VictoriaLogs query shapes work. Nothing in this revision was run. "Unverified claims" lists what changed since.

A synthetic corpus of 200,000 rows had five shapes, each from 1 to 4 log statements:

| Shape | Rows | Digit-only rules, no extraction | Extraction and word rule |
|---|---|---|---|
| Plain message | 40,000 | 28 | 4 |
| JSON in a string | 40,000 | 78 | 3 |
| logfmt in a string | 40,000 | 78 | 3 |
| Access-log line | 40,000 | 26 | 1 |
| Mixed IDs | 40,000 | 16,569 | 1 |

The rules of this RFC find one template per log statement in every shape. ClickHouse and VictoriaLogs produced the same 12 template strings.

| Check | Backend | Result |
|---|---|---|
| Digit-only rules at 2M rows, with all resource caps | ClickHouse 26.7.3.19 | Error 158: more than 50,000 groups |
| The rules of this RFC on the same 2M rows | ClickHouse 26.7.3.19 | 12 templates, 351 ms, 56 MiB peak |
| Two-window `countIf` compare | ClickHouse 26.7.3.19, 2M rows | 706 ms, 18.8 MiB peak |
| Length guard, excluded counts, `total_stats` | VictoriaLogs v1.52.0, 6,000 rows | 26 ms, totals correct |
| Two-window compare with `count() if` and `math` | VictoriaLogs v1.52.0 | Correct counts and delta per template |
| Literal-run click filter against the exact template | VictoriaLogs v1.52.0 | 1,161 rows for both |

The read cap matters more than the group cap on large shared tables. An unfiltered window of a few minutes on a busy table can read tens of millions of rows. With a filter on one service, the same query reads a small part of that.

### Later: template labels

Patterns sorts by count, so the top row is usually the busiest healthy log line. On a source without a level field, nothing says which templates are errors. A small classification model can label each template, because a window has tens to hundreds of short templates after masking. No unit in this plan builds labels. The direction that Karan chose stays:

- Each template gets a `kind` (`error`, `warning`, `retry`, `success`, `health_check`, `security`, `other`) with a confidence. Each Compare pattern row gets a probability for "this change is likely to affect users".
- The model reads masked template text, the two shares and column names. It never reads raw rows, sample lines, field values or user input. Labelling is off by default, because template text can still contain words from a log statement.
- Labels never filter or hide a template, and never fire, mute or route an alert. A label below the confidence threshold does not show.
- A separate route returns labels for up to 500 templates. Patterns never waits for labels. An in-process cache keyed by model and template hash holds up to 10,000 labels. A call has a 5-second timeout and no retry.
- The threshold ships only after a test reports precision per kind on a labelled set of synthetic templates.

## Access and security

- **Who can read.** Both routes need a member of a team that has the source, and a token with `logs:read`. The check is `requireTeamHasSource`, the same as for a log query. The MCP tools and Ask Chef apply the same rule through `authorizeSource` (`internal/mcp/server.go:229`).
- **Who can set the message field.** A global admin with `sources:write`, through the existing source routes (`internal/server/server.go:414`). A provisioned source takes it from the TOML file.
- **Restricted sources (RFC 0002).** A restricted view is its own source with its own connection user, and the log database enforces the row filter. Every analysis query reads only the source's table, tenant or selector with that user. The ClickHouse statement contains no user SQL. Its filter is compiled LogchefQL, which emits no subquery, join or table function. Templates, samples and field values therefore come only from rows the source can read.
- **Injection.** ClickHouse identifiers pass `ValidateIdentifier` and are quoted (`internal/clickhouse/validate.go:33`). VictoriaLogs field names are quoted with `strconv.Quote`, as the optimized field values do (`internal/victorialogs/stream_fields.go:64`). A Loki label must match `labelNamePattern` (`internal/loki/histogram.go:26`). A `fields` entry in a request goes through the LogchefQL field compiler of its backend.
- **Regex cost.** Every rule is RE2, which runs in linear time on all three engines. A field longer than 4,096 bytes is never masked.
- **What leaves the backend.** A sample is one raw log line, cut to 1,024 bytes in the query. A template is masked text. Both are log data, shown only to a caller who may read the source's rows.
- **Secrets and outbound requests.** The feature stores no secret and calls no new host. Template labels would send template text to a model, which is why they are off by default and not built here.

## Limits and failure modes

| Bound | Value | What the user sees |
|---|---|---|
| Admission | One slot of a new `analysis` class in `queryTracker` for the whole request. 2 per user and 8 in total, fixed, as backtest has fixed caps (`internal/server/logs_handlers.go:52`) | 429 "Too many active analysis queries for this user. Limit is 2." |
| Backend concurrency | One backend query at a time inside the slot | A slower Compare |
| Time | 30 seconds for the whole request. 8 seconds per Compare field | 408 "Request timed out". A slow field is listed under "skipped" |
| Rate | The per-user query rate limiter | 429 from the limiter |
| ClickHouse | 50,000 groups, 20M rows read, 1 GiB read, 512 MiB memory, each lowered by the source's own setting | 422 with the too-large message. A field goes to `skipped_fields` |
| VictoriaLogs | `models.MaxHistogramResponseBytes` per response, a row cap of `limit + 2` or of `limit × 30` for the sparkline request, the optimizer's window limiter, `max_window_seconds` per window | 422, or 400 for a window wider than `max_window_seconds` |
| Loki | The tenant's `max_query_series` (default 500) groups, `max_query_length`, Loki's own `query_timeout`, 16 MiB per response | 422 for the series limit. Loki's message for the others |
| Output | 50 templates, 30 buckets, 20 compare rows per list, 8 fields | The `other` count. The distinct count per field |
| Text | Templates and samples cut to 2,000 characters in the response | A cut template ends in an ellipsis |
| Windows | Truncated to milliseconds. The baseline cannot overlap the current window or start before retention | 400 with the baseline message |
| Minimum count | 5 rows in at least one window for a Compare row | Smaller groups do not appear |

The `analysis` class has its own counts, apart from `preview` and `histogram` (`internal/server/logs_handlers.go:34`). An open Patterns tab therefore never blocks the user's own log queries.

Failure modes:

- **The template query of Compare fails.** The request fails. Field queries that fail are listed in `skipped_fields`, and the rest of the report shows.
- **Loki or VictoriaLogs is busy.** A 429 from the backend maps to "is busy", as for a log query. Nothing is retried.
- **The client goes away.** The request context ends, the backend query is cancelled, and the slot is released.
- **A source loses its message column.** The ClickHouse query fails with an unknown-identifier error, which the user sees as a query error. The source page shows the setting.

**Several replicas.** The feature stores nothing besides the two source columns. The admission counts are per process (`internal/server/logs_handlers.go:95`), so the global cap applies to each replica. The migration adds two columns with defaults. A replica that still runs the old binary does not name them in its statements, so replicas can upgrade one at a time.

## Backend differences

| Feature | ClickHouse | VictoriaLogs | Loki |
|---|---|---|---|
| Filter languages | LogchefQL | LogchefQL, LogsQL without pipes | LogchefQL, a LogQL log query |
| Message field | A string column, set by an admin | `_msg` by default | `_msg` by default, or a label |
| Extraction and masks | `multiIf`, `JSONExtractString`, `replaceRegexpAll` | `unpack_json`, `unpack_logfmt`, `format`, `replace_regexp` | `json`, `logfmt`, `label_format` with `regexReplaceAllLiteral` |
| Requests for Patterns | 1 | 2 | 1 to 3 |
| Requests for Compare, per key | 1 | 1 | 2 |
| Order and limit | In SQL | In LogsQL | In Go, over every group |
| Window bounds | `[start, end)` | `[start, end)` | `(start, end]` |
| Sample line | Yes | Yes | No |
| Sparkline and count | Same query | Two requests. Their sums can differ | Same plan |
| Group cap | 50,000, fails above | The response caps and the server's memory limit | `max_query_series`, fails above |
| Memory cap per query | 512 MiB | Server-side only | Server-side only |
| Default compare fields | Low-cardinality columns from the schema | The facets endpoint | Stream labels |
| JSON path as a field | `JSONExtractString` or a native JSON path | The flattened field name | The flattened label of the source's parser |

One capability flag, `analysis`, gates the feature. All three providers set it. No flag exists per backend difference. The UI reads the two visible differences from the response: an empty `sample` hides the expand control, and `skipped_fields` names what was left out.

## Seams

**Provides to RFC 0006 (Alerting v2, phase 2).** RFC 0006 lists what it needs (`rfcs/0006-alerting-v2.md:977`). Each item, and the unit that delivers it:

| RFC 0006 needs | Delivered by | Used by |
|---|---|---|
| The `GroupCounter` provider method with a baseline window, `AnalysisFilter` and `TimeRange` | LP3 for the contract and ClickHouse, LP4 for VictoriaLogs, LP5 for Loki | P0 |
| The rule that a template is `new` only when the baseline has matching rows | LP1 (`logpattern.StatusOf`) | P0, P1 |
| The source's message field | LP2 | P1 |
| The `analysis` capability | LP3, LP4, LP5, one provider each | P1 |
| `Service.Patterns` without a user | LP6 | P2 |
| `Service.ComparePeriods` without a user | LP9 | P2 |
| The helper that turns a field and a value into a LogchefQL equality clause | LP1 (`logchefql.Condition`) | P2 |

P0 can start when LP3 and LP4 are merged. P2 needs LP6 and LP9.

**What this RFC does not provide to RFC 0006.** P0 builds `Service.NewPatterns`, its "new only" filter, its minimum current count and its total before the limit (`rfcs/0006-alerting-v2.md:742`). The first version of this RFC sketched the alert as one count through a count-only mode of `GroupCountRequest`. That sketch is removed, because RFC 0006 needs the templates and not one number. P0 extends `GroupCountRequest` at one place per provider:

- On ClickHouse and VictoriaLogs the place is the `ok` sort key. A "new only" request sets it to `base = 0 AND cur >= <min>`, and one more window total counts the rows whose `ok` is 1.
- On Loki the place is the Go code that ranks the merged groups.

**Conflict to settle: Loki.** RFC 0006 says that new-pattern rules and triage are "Not in RFC 0004" for Loki (`rfcs/0006-alerting-v2.md:956`), and P0 names "both providers" (`rfcs/0006-alerting-v2.md:1140`). This revision gives Loki `GroupCounter` and the `analysis` capability. P1 validates a rule by that capability, so it would accept a Loki source. Either P0 implements its options on Loki as well, or P1 also checks the source type. See open question 2.

**Requires from RFC 0005 (LogchefQL aggregations).** `logchefql.WithoutAggregation`, which exists. This RFC changes no grammar. A regex operator such as `=~` would be a grammar change under RFC 0005's rules. See open question 1.

**Requires from RFC 0007 (saved views).** Nothing. The URL parameter `view` belongs to saved views, so this RFC uses `tab`. A saved view definition does not store the tab.

**Requires from RFC 0009 (audit log).** The `query.run` recorder and the route test with `auditExemptRoutes`. See "Audit events".

**Requires from RFC 0011 (Loki).** The provider's request helper, `filterQuery`, `histogramRun` and `streamLabels`, and the `providertest` suite, which gains the analysis checks. RFC 0011 chose not to call `/loki/api/v1/patterns` (`rfcs/0011-loki-datasource.md:123`). This RFC keeps that choice.

**RFC 0002, RFC 0003, RFC 0008, RFC 0010.** No conflict. "Access and security" covers restricted sources. Dashboards and reports get no pattern panel here.

**Merge hot spots.** `internal/datasource/service.go` (the capability constant), `internal/server/server.go` (two routes), `internal/server/audit_routes_test.go` (two exemptions), `internal/mcp/server.go` (`toolScopes`), `internal/audit/views.go` (two source fields), and the 11 locale catalogs.

## Audit events

This RFC adds no action. It adds detail to two existing ones.

| Event | Actor | Target | Change |
|---|---|---|---|
| `source.create`, `source.update` | The admin | The source | `SourceFields` records `meta_message_field` and `meta_message_format` |
| `query.run` | The user, or the caller of an MCP or Ask Chef tool | The source | Two new modes, `patterns` and `compare` |

Patterns returns raw sample lines, and both operations return values from log rows. They are reads of log data, so each request records one `query.run` event, as a tail session and a log context read do. The handler calls `newQueryRun` with a nil user (`internal/server/query_run.go:100`), so the run writes no query history row and counts in no rollup. The event holds the filter text, the language, the window from the current start to the current end, and the number of rows returned. A Compare event also holds the baseline kind in `detail`. The MCP tools record through `StartRead`, with the tool name (`internal/server/mcp_handlers.go:138`).

Both `POST` routes get an entry in `auditExemptRoutes` with the reason "the query record writer stores query.run", as `/logs/query` has (`internal/server/audit_routes_test.go:43`).

## Alternatives rejected

| Alternative | Reason |
|---|---|
| Digit-only masks | The first draft replaced only digits with `<N>`. On mixed IDs it produced a template per ID and passed the group cap at 2M rows. The word rule fixes that, at the cost of masking a few words that are not IDs, such as `http2` and `utf8`. |
| Mask the whole string of a JSON or logfmt row | Every field in the body, such as a user ID or a path, becomes part of the template, so one log statement makes thousands of templates. |
| Each backend's own normalizer, such as `collapse_nums` | The templates differ between backends, IDs with letters stay unmasked, and the click filter needs a derivation per backend. |
| Loki's `/loki/api/v1/patterns` | It needs the pattern ingester, which is off by default, and it takes a stream selector with no pipeline (https://grafana.com/docs/loki/v3.0.x/reference/api/#patterns-detection). Its templates come from Loki's own clustering, so they differ from the other backends. |
| `topk` on Loki | The provider needs totals and the distinct count, which `topk` hides. A sharded query can also hit the series limit before `topk` applies. |
| Group the loaded rows in the browser | The counts cover only the loaded page, 100 rows by default. It fails the first goal. |
| Drain-style clustering in Go over a stream of rows | Logchef must read rows out of the backend, a sample breaks "counts cover the window", and no backend can filter by a cluster that only exists in Go. |
| Classify rows with a model | The cost and latency grow with log volume, raw rows leave the deployment, and the result is not reproducible. |
| An exact pattern filter threaded through every query path | It adds a structured match to LogchefQL compilation, the histogram, field values, shares and the URL, before anyone has used the feature. The literal-text filter plus a later regex operator gets most of the benefit for one function. |
| The message field as a request parameter | Every caller then picks the field. The source already owns the timestamp and severity fields the same way. |
| `Patterns()` and `Compare()` as provider methods | Each of three providers would own ranking, buckets, top N and baselines. |
| Field queries in parallel | One request would run up to nine backend queries at once and compete with the user's own queries. |
| One endpoint with an optional baseline | One route fewer, and one response type with two shapes. |
| The tab in a Pinia store or as part of the location | A store is not shareable and is lost on Back. A tab that is part of the location runs the query again on every switch. |
| `view=patterns` in the URL | `view` names a saved view since RFC 0007. |
| A `WHERE` on the minimum count | A window whose groups all fall below it would return no row, and the totals with it. |
| A pre-check against the histogram total in the browser | The cap counts rows read, on ClickHouse only, and the browser does not know it. ClickHouse rejects an oversized read while it plans the query. |

Tradeoffs accepted:

- Five regex passes and a JSON or logfmt parse per message, in exchange for the same templates on every backend and one file to tune.
- A click filter that can match a few similar lines, in exchange for no LogchefQL grammar change.
- No Patterns or Compare on ClickHouse native SQL, in exchange for not parsing user SQL.
- A slower Compare with sequential field queries, in exchange for one backend query per request at a time.
- Two source settings and a migration, in exchange for one answer to "which field is the message".
- A group cap of 500 by default on Loki and 50,000 on ClickHouse. Each backend fails above its own cap and says so.
- No sample line and a different window edge on Loki, in exchange for one metric query shape that the provider already runs.

## Implementation plan

The path to the first unit a user can use is LP1, LP2, LP3, LP6, LP7. RFC 0006's unit P0 needs LP1 to LP4, which are the first four units. After LP3, the units LP4, LP5 and LP6 run in parallel. LP8 runs in parallel with everything after LP2. After LP9, the units LP10, LP11 and LP12 run in parallel.

Every unit that touches a store adds the migration and the queries to both stores, runs `just sqlc-generate`, and adds a conformance test in `internal/store/storetest`. Every unit ends with `just check`. Frontend units also run `bun run typecheck` and `bun run test`. Each unit names only its extra check below.

### LP1. Pattern rules and LogchefQL helpers

- Depends on: nothing.
- Creates: `internal/logpattern/` (`rules.go`, `message.go`, `render_clickhouse.go`, `render_logsql.go`, `render_logql.go`, `clause.go`, `baseline.go`, tests, `testdata/corpus.json`), `internal/logchefql/condition.go` and its test.
- Edits: `internal/logchefql/sql_generator.go`, `logsql_generator.go`, `logql_generator.go` (export `FieldSQL`, `FieldLogsQL`, `FieldLogQL`).
- Change: `MessageFormat`, `Rules`, `Mask`, the three renderers, `Clause`, `TimeRange`, `ResolveBaseline`, `Shift`, `StatusOf`, `Condition`. Nothing calls the package yet.
- Check: the corpus has one fixture per source shape of "What the dev stack showed". A test asserts the template of each line and the template count per shape. A test asserts that the text `Clause` returns is a substring match for every corpus message of its template. Table tests cover the three baseline kinds, an overlapping baseline, a zero baseline total and each status. `Condition` round-trips through `ParseLogchefQL` for values with quotes, backslashes and spaces. Golden tests pin the rendered ClickHouse expression, LogsQL pipes and LogQL stages.

### LP2. Source message settings

- Depends on: LP1.
- Creates: migration `add_source_message_settings` in both stores, `internal/store/storetest/source_message_settings.go`.
- Edits: both `queries.sql`, `internal/store/sqlite/sources.go`, `internal/store/sqlite/utility.go`, `internal/store/postgres/sources.go`, `pkg/models/source.go`, `internal/datasource/source_updates.go`, `internal/datasource/clickhouse_provider.go`, `internal/victorialogs/provider.go`, `internal/loki/provider.go`, `internal/config/provisioning.go`, `internal/provisioning/validate.go`, `reconciler.go`, `export.go`, `internal/audit/views.go`, `frontend/src/api/sources.ts`.
- Change: the two columns, the model fields, create and update validation per provider, the `_msg` default, provisioning, the audit view, and the API fields.
- Check: the conformance test writes and reads both values on SQLite and Postgres, and reads `auto` for a row created before the migration. An upgrade test runs the real migration over a fixture with one source per type and asserts `_msg` for VictoriaLogs and Loki and `''` for ClickHouse. A ClickHouse provider test rejects a missing column and a non-string column. A provisioning test round-trips both keys through export. A handler test reads both fields from `GET` and sets them with `PUT`.

### LP3. The `GroupCounter` contract and ClickHouse

- Depends on: LP1, LP2.
- Creates: `internal/datasource/analysis.go`, `internal/datasource/clickhouse_analysis.go`, `internal/clickhouse/analysis.go` and its tests.
- Edits: `internal/datasource/service.go` (`CapabilityAnalysis`), `internal/datasource/query.go` (the two errors), `internal/datasource/clickhouse_provider.go` (the descriptor), `internal/clickhouse/query.go` (`IsResourceLimitError`), `internal/datasource/providertest/providertest.go`.
- Change: the types of "The provider contract", ClickHouse `CountGroups` and `DimensionFields`, and the analysis checks of the conformance suite. The suite runs them for a provider that declares `analysis` and fails a provider that declares it without implementing `GroupCounter`.
- Check: `providertest` seeds the LP1 corpus plus rows exactly on the edge between two adjacent windows. It asserts that every template and count equals what `logpattern.Mask` gives for the seeded rows, that each edge row is counted in exactly one window, that the totals include the excluded rows, that a group below `MinCount` is not returned while the totals stay, and that the order with a baseline is by shift. A ClickHouse test with a lowered group cap reads `ErrAnalysisTooLarge`. A unit test asserts that the SQL holds no user text besides the compiled filter.

### LP4. VictoriaLogs `GroupCounter`

- Depends on: LP3. Parallel with LP5 and LP6.
- Creates: `internal/victorialogs/analysis.go` and its tests.
- Edits: `internal/victorialogs/provider.go` (the descriptor), the provider's integration test (its `providertest.Harness`).
- Change: `CountGroups` with one or two requests, `DimensionFields` over the facets endpoint, the limiter slot and the `max_window_seconds` check on optimizer sources.
- Check: the `providertest` analysis checks pass on dev VictoriaLogs with the same expectations as on ClickHouse. A test rejects LogsQL with a pipe. A test with a fake server that sends more than the row cap reads `ErrAnalysisTooLarge`. A test on an optimizer source rejects a window wider than `max_window_seconds` and holds one limiter slot per request.

### LP5. Loki `GroupCounter`

- Depends on: LP3. Parallel with LP4 and LP6.
- Creates: `internal/loki/analysis.go` and its tests.
- Edits: `internal/loki/provider.go` (the descriptor), `internal/loki/histogram.go` (`countExpr` and `histogramRun.count` take a list of labels), the provider's integration test.
- Change: `CountGroups` as in "Loki queries", `DimensionFields` over the stream labels, and the series-limit mapping.
- Check: the `providertest` analysis checks pass on dev Loki 3.7.6, with an empty `Sample` and with each edge row in exactly one window. A test with the fake Loki server that answers the series-limit 400 reads `ErrAnalysisTooLarge`. A test sends a metric query as the filter and reads the validation error. This unit runs the Loki experiments of "Unverified claims" first, because three of them decide the stage text.

### LP6. `Service.Patterns` and the Patterns route

- Depends on: LP3. Parallel with LP4 and LP5.
- Creates: `internal/datasource/analysis_service.go`, `internal/server/analysis_handlers.go` and its tests.
- Edits: `internal/core/logs.go`, `internal/core/query.go` (`AnalysisTimeout`, `PrepareAnalysisFilter`), `internal/server/server.go` (the route), `internal/server/logs_handlers.go` (`QueryClassAnalysis` and its caps), `internal/server/query_errors.go`, `internal/server/query_run.go` (the `patterns` mode), `internal/server/audit_routes_test.go`.
- Change: `Service.Patterns`, `core.GetPatterns`, `POST /logs/patterns`, admission, the two error mappings and the `query.run` event.
- Check: service tests with a fake `GroupCounter` assert `other`, the 2,000-character cut, the click filter of each row and `ErrMessageFieldNotSet`. Handler tests assert the response shape, 400 for ClickHouse SQL, 422 for `ErrAnalysisTooLarge`, 429 for a third request of one user, the stripped aggregation pipe, substituted variables, and one `query.run` event with mode `patterns` and no history row. The route test passes with the new exemption.

### LP7. Explorer tabs and the Patterns view

- Depends on: LP6. First unit a user can use.
- Creates: `frontend/src/api/analysis.ts`, `frontend/src/composables/useRunAnalysis.ts`, `ExploreViewTabs.vue`, `ExploreAnalysis.vue` and `PatternsList.vue` under `frontend/src/views/explore/components/`, and their tests.
- Edits: `frontend/src/lib/exploreLocation.ts`, `frontend/src/composables/useExplorerNavigator.ts`, `frontend/src/views/explore/LogExplorer.vue`, the 11 locale catalogs.
- Change: the `tab` parameter, `setTab`, the composable, the tab bar with **Logs** and **Patterns**, the list, the click, and the empty states with the admin's "Use `<field>`" button.
- Check: unit tests for `parseExploreTab` and for `sameExploreLocation` ignoring `tab` and `vs`. A navigator test asserts that `setTab` pushes one history entry and starts no run, and that Run keeps the tab. UI scenario on a dev ClickHouse source and, once LP4 and LP5 are merged, on VictoriaLogs and Loki: open Patterns, see templates with counts and sparklines, click one, land on Logs with the filter and a frozen window, go Back to Patterns with no new query, zoom the histogram and see Patterns refetch, open a source without a message field and see the explanation, leave Explorer and return through KeepAlive with no request in flight.

### LP8. Message settings in the source forms

- Depends on: LP2. Parallel with LP3 to LP7.
- Edits: `frontend/src/views/sources/components/ClickHouseSourceForm.vue`, `VictoriaLogsSourceForm.vue`, `LokiSourceForm.vue`, `sourceFormModels.ts`, `SourceInspectionOverview.vue`, `frontend/src/views/sources/AddSource.vue`, the locale catalogs.
- Change: a message field input, prefilled from `MESSAGE_FIELD_ALIASES` against the table's columns, and a format select.
- Check: a form test asserts the prefill and the request body. UI scenario: create a ClickHouse source whose table has a `body` column, see `body` prefilled, save, and read it back on the source page.

### LP9. `Service.ComparePeriods` and the Compare route

- Depends on: LP6.
- Edits: `internal/datasource/analysis_service.go`, `internal/core/logs.go`, `internal/server/analysis_handlers.go`, `internal/server/server.go`, `internal/server/query_run.go` (the `compare` mode), `internal/server/audit_routes_test.go`, and their tests.
- Change: `Service.ComparePeriods`, `core.ComparePeriods` and `POST /logs/compare`.
- Check: service tests with a fake `GroupCounter` assert the field order (defaults, then the caller's, at most 8), a slow field in `skipped_fields` with `timeout`, a field past the deadline with `deadline`, a failed template query failing the request, `baseline_empty` with null shares, the status of each row, and no `patterns` for a source without a message field. Handler tests assert 400 for an overlapping baseline and for a baseline before retention, and one `query.run` event with mode `compare`. An integration test on dev ClickHouse seeds two hours in which one service's share moves from 2% to 41% and reads that row first.

### LP10. The Compare view

- Depends on: LP7, LP9. Parallel with LP11 and LP12.
- Creates: `frontend/src/views/explore/components/CompareReport.vue` and its test.
- Edits: `ExploreViewTabs.vue`, `ExploreAnalysis.vue`, `useRunAnalysis.ts`, `frontend/src/components/field-sidebar/FieldSideBar.vue`, `LogExplorer.vue`, the locale catalogs.
- Change: the **Compare** tab, the baseline select with `vs`, the three parts of the report, the skipped list, and "Compare by this field".
- Check: UI scenario on the three dev backends: open Compare, switch the baseline and see the URL change with one request, click a field row and land on Logs with the condition, add a field from the sidebar and see it in the report, open a source without a message field and see fields without patterns.

### LP11. MCP tools and Ask Chef

- Depends on: LP9. Parallel with LP10 and LP12.
- Edits: `internal/mcp/analysis.go`, `internal/mcp/server.go`, `internal/server/mcp_handlers.go` (the admission class), `internal/askchef/tools.go`, `internal/askchef/prompt.go`, `internal/mcp/ui/src/useInvestigation.ts`, `internal/mcp/ui/dev/server.ts`, and the tests that name `compare_windows`.
- Change: `log_patterns` and `what_changed`. Deletes `compare_windows` and everything that names it.
- Check: MCP tests assert that both tools are listed only with `logs:read`, return the bounded result, hold one `analysis` slot and record one `query.run` event with the tool name. An Ask Chef test asserts the evidence of each tool. `rg compare_windows internal frontend` prints nothing.

### LP12. CLI commands

- Depends on: LP9. Parallel with LP10 and LP11.
- Creates: `cli/crates/logchef-cli/src/commands/patterns.rs`, `compare.rs`.
- Edits: `cli/crates/logchef-cli/src/cli.rs`, `commands/mod.rs`, `cli/crates/logchef-core/src/api/models.rs`, `cli/crates/logchef-core/src/api/mod.rs`.
- Change: `logchef patterns` and `logchef compare`, with the source, team, query and time flags of `logchef histogram`, `--vs` for the baseline, a table by default and `--output json`.
- Check: `just check-cli`. A test with a mock server asserts the request body and the table of each command. A 422 exits non-zero and prints the server's message.

### LP13. Docs

- Depends on: LP10, LP11, LP12.
- Creates: `docs/src/content/docs/features/patterns-and-compare.mdx`.
- Edits: `docs/src/content/docs/getting-started/provisioning.md`, `docs/src/content/docs/integration/mcp-server.mdx`, `docs/src/content/docs/integration/cli.mdx`, `docs/src/content/docs/operations/audit-log.md`, `CHANGELOG.md`.
- Check: the docs build passes. Every route, tool, command and setting that the pages name exists in the code.

## Changes in revision

The revision of 2026-11-22 keeps the design of the merged version and brings it up to the code on `develop`. What changed, and why:

- **Number and title.** The RFC is 0004. It was merged as 0002, which `rfcs/0002-restricted-source-access.md` already holds.
- **Loki is a third backend.** RFC 0011 landed after this RFC. Loki gets `GroupCounter` through metric queries, with three differences: no sample line, a window that is closed at its end, and a group cap of `max_query_series`.
- **The URL parameter is `tab`.** The merged version used `view`, which saved views took (`frontend/src/lib/exploreLocation.ts:37`).
- **Request fields follow the other log routes.** `start_time`, `end_time` and `extra_stream_filters` replace `start`, `end` and `stream_filters`. The source response uses `_meta_message_field`, as it does for the timestamp and severity fields.
- **Migrations have no fixed numbers.** The merged version named SQLite 000039 and Postgres 000013. Both stores are past those numbers. `sqlc.yaml` reads the migrations directory, so it needs no edit.
- **The ClickHouse SQL lives in `internal/clickhouse`.** Only that package can run a statement with its own settings (`internal/clickhouse/client.go:247`). The settings travel with the query context, not in a `SETTINGS` clause, and every overflow mode is already `throw`.
- **One statement serves Patterns and Compare, and the minimum count is a sort key.** The merged version filtered small groups "after aggregation" without saying where. A filter in the database before the limit can drop every row and lose the totals. A filter in Go after the limit can return fewer rows than exist.
- **`MinCount` is part of `GroupCountRequest`**, for the same reason.
- **Three new errors became two.** `ErrNativeSQLNotSupported` is a `ValidationError`, because each backend states a different rule.
- **Requests record `query.run`.** The audit log of RFC 0009 now records every read of log data. Patterns returns raw sample lines.
- **The admission caps are fixed numbers**, 2 per user and 8 in total. The merged version gave no numbers.
- **Each Compare field has an 8-second budget.** Without it one slow field uses the whole 30 seconds and every later field is skipped.
- **The histogram pre-check is gone.** See the last row of "Alternatives rejected" and open question 5.
- **MCP, Ask Chef and the CLI are units.** The merged version left them as follow-ups. The alert rule moved to RFC 0006, and its count-only sketch is removed.
- **`logchefql.Condition` and the exported field compilers are new.** RFC 0006 asks for the first. The second keeps one field compiler per backend.
- **The frontend fallback list does not gain `analysis`.** The list applies only to a response without capabilities, which a server with this feature never sends.
- **Template labels are shorter** and are a non-goal of this plan. Their direction is unchanged.

## Open questions for Karan

1. **Is a click filter that can match similar lines acceptable, or should a LogchefQL regex operator ship first?** Default: ship the literal-text filter. Add `=~` later as its own grammar change.
2. **Does RFC 0006 phase 2 cover Loki?** Default: yes. P0 implements its options on all three providers, and RFC 0006's backend table gets a one-line edit when P0 lands. The alternative keeps new-pattern rules and triage on ClickHouse and VictoriaLogs, with a source-type check in P1.
3. **Should the message keys be configurable per source?** Default: no. `msg` and `message` only, until a user asks.
4. **Are 20M rows read and 1 GiB read the right ClickHouse caps?** Default: yes, as fixed values that a source's own lower setting can reduce. The alternative adds two keys under `[query]` in the config.
5. **Should the browser skip the request when the histogram total is above the row cap?** Default: no. The server's 422 shows the same message, and the alternative needs the cap in the source response.
6. **Should Loki rows have a sample line?** Default: no. The alternative reads the newest 200 rows of the window with one log query, masks them in Go and attaches the first match per template.
7. **Should the table's default message column and the log detail view read `_meta_message_field`?** Default: not in this plan. Both guess from `MESSAGE_FIELD_ALIASES` today, and using the setting would remove two guesses.
8. **Should VictoriaLogs `DimensionFields` prefer stream fields over facet order?** Default: no, keep facet order.
9. **Should Patterns and Compare requests appear in the audit log?** Default: yes, as `query.run` with their own modes. The alternative exempts both routes, as the histogram is exempt.
10. **Which classifier ships first for template labels?** Not needed for this plan. Default when labels are built: the chat fallback that works with every configured provider.

## Unverified claims

Nothing in this revision ran against a backend. The table lists each runtime claim that the first version did not observe, with the experiment that settles it.

| Claim | Experiment |
|---|---|
| The nested ClickHouse statement returns the same templates and counts as the flat one that ran in October, and `sumIf(...) OVER ()` and `countIf(...) OVER ()` are accepted | Run the statement of "ClickHouse queries" on the 2M-row corpus on dev ClickHouse and compare with `logpattern.Mask` |
| ClickHouse rejects a read above `max_rows_to_read` while it plans the query, so a too-large window fails fast | Run Patterns over an unfiltered day of the largest dev table and time the 422 |
| A one-day window on a wide production table, such as an OTel table with a large `body` column, finishes within 30 seconds with a service filter | Time it on such a table before release |
| The VictoriaLogs pipeline with `_lc_kind`, the blanked oversized text and the `ok` sort key returns the templates that the October pipeline returned | Run it on dev VictoriaLogs v1.52.0 over the corpus. `math` with `default`, `min`, `max` and `floor` is documented at https://docs.victoriametrics.com/victorialogs/logsql/#math-pipe |
| VictoriaLogs skips the gap between two `_time` ranges joined by `or`, so a `day` or `week` baseline does not scan the days between | Compare the scanned bytes of a `week` compare with those of two separate one-hour queries, through the `query_stats` pipe |
| Loki's `label_format` template accepts `len`, a nested `default`, and five nested `regexReplaceAllLiteral` calls with the rule patterns | Run the stages of "Loki queries" on dev Loki 3.7.6 and on Loki 3.0. The functions are documented at https://grafana.com/docs/loki/v3.0.x/query/template_functions/ |
| A label that `json` or `logfmt` did not extract reads as an empty string in a template | Send one plain-text line through the stages and read `_lc_key` |
| `logfmt _lc_l1="msg"` on a JSON line extracts nothing and sets no `__error__` that survives `drop` | Send a JSON line without `msg` through the stages inside `count_over_time` |
| Loki, ClickHouse and VictoriaLogs give the same template for every corpus line, including escaped quotes and unquoted logfmt values | The `providertest` analysis checks of LP3, LP4 and LP5 |
| Loki answers a query above `max_query_series` with a 400 whose text contains "reached for a single query", in 3.0 and in 3.7 | Seed 600 distinct templates and run Patterns. The limit is documented at https://grafana.com/docs/loki/v3.0.x/configure/ under `max_query_series` |
| A Loki instant query at `End` with a range of the window counts `(Start, End]` and the histogram plan counts the same rows | Seed one row at each edge and compare the two counts |
| A window with a few hundred templates on Loki finishes within 30 seconds, although every line passes two parsers and five regexes | Time Patterns on the dev Loki corpus at 2M lines |
| A replica on the old binary keeps reading and writing sources after the migration, because sqlc expands `SELECT *` when it generates the code | Start the previous release against a migrated Postgres database, list sources and update one |
| The route watcher treats a URL that differs only in `tab` as already shown and starts no run | The navigator test of LP7 |
| An admin's "Use `body`" click updates the source and the open Explorer reads the new setting without a reload | The UI scenario of LP7 |
