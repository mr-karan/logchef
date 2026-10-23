# RFC 0002: Log patterns and window compare in Explorer

Status: Proposed
Date: 2026-10-23
Authors: Karan Sharma

## Summary

Explorer shows rows. During an incident the first two questions are "what kinds of messages are there?" and "what changed?". Today the user answers both by scrolling rows and guessing.

This RFC adds two server-side summaries of the query and window that Explorer is showing:

- **Patterns** groups the messages of the window into templates such as `connection to <IP> timed out after <*>`. Each template shows a count over the whole window, a sparkline, and one sample line. A click on a template filters Explorer to it.
- **Compare** compares the window with a baseline of equal length: the previous period, the same window yesterday, or the same window last week. It ranks field values and templates by how much their share of matching logs moved, and it shows the change in total volume.

Both features run as aggregate queries in the log backend. Neither feature reads rows into Logchef. Both work on ClickHouse and VictoriaLogs and produce the same template text on both.

Real sources come in several shapes. Some have a clean message column. Some keep JSON or logfmt in one string column. Some mix access-log lines with application logs. Some are access or event tables with no message at all. The design handles each shape:

- A source can name its message field and say whether that field can hold JSON or logfmt. For each row, the query takes the inner message when there is one.
- The mask rules replace every word that contains a digit, not only the digits. IDs such as `AB1234` and `FQ12_holdings_8c9f` collapse to one placeholder.
- A source without a message field gets Compare without Patterns.

The design keeps three decisions in one place each:

- `internal/logpattern` owns the message extraction, the mask rules, the ranking rules, and baseline arithmetic. It is pure Go with no I/O.
- `datasource.Service` owns the policy: limits, buckets, ranking, and the click filter. It calls one optional provider method, `CountGroups`.
- The Explorer URL owns the selected view. The navigator stays its only writer. A view switch runs no query.

An optional, later step labels templates with a small classification model. It reads template text only, never raw rows. See [Later: template labels](#later-template-labels).

## Motivation

A user opens Explorer on `service="checkout" and level="error"` for the last hour and gets 800,000 rows. Explorer shows the newest 100. The histogram shows that the error rate went up at 10:20. The user still does not know which errors went up.

Two things answer that question quickly:

1. A list of message templates with counts. 800,000 rows that come from 30 log statements become 30 lines.
2. A diff against a normal period. "`service=payments` went from 2% to 41% of errors" and "`failed to connect to db host=<IP>` is new" are the answers an on-call engineer wants first.

Grafana's log panels and the ClickHouse plugin do neither. VictoriaLogs has building blocks (`collapse_nums`, `count() if`) but no UI that uses them. Logchef already has the window, the filter, the access model, and the histogram. These two views are the missing step between seeing a spike and knowing its cause.

Ask Chef and the MCP server need the same answers as cheap, bounded tools. An agent that reads 30 templates with counts reasons better than an agent that reads 500 rows. This RFC builds the operations so that those tools, and a later "new pattern" alert, call the same code.

## Goals

- Counts cover every matching row in the window, on both backends, including VictoriaLogs windowed-search sources.
- The same message gives the same template on ClickHouse and VictoriaLogs.
- Common source shapes produce a short list of templates: a message column, JSON or logfmt in a string column, access-log lines, and messages that carry mixed letter-and-digit IDs.
- Sources with no message field still get Compare.
- A user can go from a template to its logs in one click.
- Compare ranks by change in share, so a small value with a large jump ranks above a large value that stayed flat.
- Every request has a timeout, an admission slot, a group cap, and a row cap. A request that is too large fails with a message that tells the user to narrow it. It never returns partial counts as if they were complete.
- One implementation serves the HTTP API, the MCP tools, Ask Chef, and later alerts.

## Non-goals for v1

- Learned clustering (Drain or similar), custom mask rules per source, and a stored catalog of templates.
- Message keys other than `msg` and `message` inside JSON or logfmt.
- Patterns or Compare on ClickHouse native SQL. The user's SQL fixes its own time range and projection, so Compare cannot add a baseline to it. VictoriaLogs native LogsQL is supported when it is a filter with no pipes.
- Sparklines in Compare. Compare rows show both counts and both shares.
- An exact pattern filter. v1 filters by the literal text of the template. See [How a click filters](#how-a-click-filters).
- The MCP tools, the Ask Chef allowlist entries, the alert type, and template labels. This RFC defines their call sites so that they are small follow-ups.

## User experience

Explorer gains a tab bar above the results: **Logs**, **Patterns**, and **Compare**. **Logs** is today's view, with its table, compact, and JSON modes unchanged. The tab bar is hidden during live tail and on sources without the `analysis` capability.

**Patterns.** The header reads "37 patterns · top 50 shown · 1,210 other (0.1%) · 4,002 without a message". The `other` share is always shown, so a long tail is visible and not hidden behind the top 50. Each row has a sparkline, the count, the share of the window, and the template. Placeholders (`<*>`, `<IP>`, `<UUID>`) render as muted chips. A row expands to show one raw sample line. A click on a row adds a filter for that template and switches to **Logs**.

When a source has no message field set, **Patterns** explains why it is empty. An admin sees a one-click button such as "Use `body`", and other users see "Ask an admin to set the message field". **Compare** works on these sources.

**Compare.** A baseline select offers **Previous period**, **Same time yesterday**, and **Same time last week**. Both windows appear as concrete timestamps. The report has three parts:

1. Volume: "812,345 vs 401,002 matching logs (+103%)".
2. Fields that shifted: `service = payments 2.0% → 41.0%`. A click adds `service="payments"` to the query. The user can add a field from the field sidebar with "Compare by this field", including a JSON path such as `attrs.method` and the path template of a URL field.
3. Patterns that shifted, with a status of **new**, **grew**, **shrank**, or **gone**. A click filters to the template. This part is absent when the source has no message field.

The URL records the view: `...&view=patterns`, or `...&view=compare&vs=day`. Back and Forward move between views without running the query again. A shared link opens on the same view.

**Large windows.** When the histogram of the shown run reports more rows than the analysis row cap, the Patterns and Compare tabs say "Add a filter or narrow the window to see patterns" instead of running a query that the backend will reject.

## Design

### Message extraction

Patterns needs the free text of each row. In many sources that text is not a column. It is the value of `msg` or `message` inside a JSON or logfmt string, next to timestamps, request IDs, and user IDs. Masking the whole string produces a template per ID. Masking only the inner message produces one template per log statement.

`internal/logpattern` defines the extraction once:

```go
package logpattern

// MessageFormat says how to read the message field of a source.
type MessageFormat string

const (
	// FormatRaw uses the field as the message.
	FormatRaw MessageFormat = "raw"
	// FormatAuto reads each row as JSON, then as logfmt, then as raw text,
	// and takes the first non-empty value of MessageKeys.
	FormatAuto MessageFormat = "auto"
)

// MessageKeys are the keys read inside JSON and logfmt bodies, in order.
var MessageKeys = []string{"msg", "message"}
```

For `auto`, each row resolves as follows:

1. If the field is valid JSON with a non-empty top-level `msg` or `message`, that value is the message.
2. Else, if the field has a logfmt `msg=` or `message=` pair, quoted or not, its value is the message.
3. Else the whole field is the message.

`ClickHouseMessageExpr` renders this as `multiIf(isValidJSON(f), ..., match(f, ...), ..., f)` with `JSONExtractString` and `extractGroups`. `LogsQLMessagePipes` renders it as `unpack_json` and `unpack_logfmt` with a `result_prefix`, followed by conditional `format` pipes that pick the first non-empty value. JSON and logfmt escapes differ between the two backends' parsers, so the conformance corpus includes escaped quotes, unquoted logfmt values, nested JSON, and invalid JSON.

### The mask rules

`internal/logpattern` holds the only list of mask rules. Both backends render their query text from it, and the click filter derives from it.

```go
// Rule replaces every match of Pattern with Placeholder. Placeholders contain
// no digits, so a later digit rule never rewrites an earlier placeholder.
type Rule struct {
	Name        string
	Pattern     string // RE2. ClickHouse (re2) and VictoriaLogs (Go regexp) read it the same way.
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

func ClickHouseExpr(messageExpr string) string // nested replaceRegexpAll calls, in rule order
func LogsQLPipes(quotedField string) string    // one `| replace_regexp (...) at <field>` per rule
```

The word rule is the important one. IDs in real logs mix letters and digits: `AB1234`, `user107`, `FQ12_holdings_8c9f`, `v0.24.5`, `2026-10-23T13:10:44`. A rule that masks only digits leaves the letters, so each ID still makes its own template. The word rule replaces the whole word. The run rule then folds sequences such as `<*>.<*>.<*>` and `<*>-<*>-<*>T<*>` into one `<*>`, so dates, versions, and IPv6 addresses become one placeholder. Hyphens and colons are not word characters, so `api-gateway-<*>` keeps its readable prefix.

We use our own list instead of each backend's native normalizer for three reasons:

- The template text is the same on both backends. A conformance test feeds one corpus through both dev backends and compares the results with a Go reference implementation of the rules.
- The word rule masks IDs that `collapse_nums` and digit-only rules leave in place.
- When a mask is wrong, one file changes.

`ClickHouse normalizeQuery` was also tried. It splits one UUID into several placeholders and has no VictoriaLogs equivalent.

Fields longer than 4,096 bytes and rows whose extracted message is empty are counted but not masked. The response reports them as `oversized` and `missing`, separate from `other`. The length guard keeps the regex cost per row bounded.

### The message field

Sources have `_meta_ts_field` and `_meta_severity_field` today. This RFC adds two settings next to them:

- `_meta_message_field`: the field that holds the message. Empty means the source has no message field.
- `_meta_message_format`: `raw` or `auto`. The default is `auto`.

Storage:

- New migrations: SQLite `000039_source_message_field` and Postgres `000013_source_message_field`. Each adds `_meta_message_field TEXT NOT NULL DEFAULT ''` and `_meta_message_format TEXT NOT NULL DEFAULT 'auto'` with a check on the two values, then sets `_msg` for VictoriaLogs sources whose message field is empty. Running the migration twice changes nothing. Both up files go into `sqlc.yaml`. The store conformance suite covers the new columns.
- `models.Source` carries the format as `logpattern.MessageFormat`, parsed once when the row is read.
- Source create and update requests, and the provisioning TOML, accept `meta_message_field` and `meta_message_format`. ClickHouse `PrepareSource` and `UpdateSource` check that the column exists and is a string type. VictoriaLogs defaults the field to `_msg`.
- The source form prefills the field from `MESSAGE_FIELD_ALIASES` in `frontend/src/lib/sourceFields.ts`, the list Explorer already uses to pick a message column. Logchef keeps one alias list. The server never guesses.

A source setting gives one answer to every caller: the UI, MCP, Ask Chef, and alerts. A request parameter would make each caller guess, and a server-side guess would pick the wrong column without saying so. Existing ClickHouse sources need an admin to set the field once.

### What each source supports

| Source | Patterns | Compare fields | Compare patterns |
|---|---|---|---|
| Message field set | Yes | Yes | Yes |
| No message field, for example an access or event table | No, with an explanation | Yes | No |

Both providers declare one capability, `analysis`. Patterns availability is a property of the source, not the provider, so the API returns it in the source response as `message_field`, and the tabs read it from there.

### One provider method

Patterns and Compare run the same query shape. Each groups the matching rows by a key, counts the rows in the current window (and the baseline), keeps a sample, and returns the top N groups. The provider interface therefore gets one optional method plus a helper that picks fields:

```go
package datasource

// TimeRange is half-open: Start <= t < End.
type TimeRange struct{ Start, End time.Time }

// AnalysisFilter is the run's filter with template variables already substituted.
type AnalysisFilter struct {
	Query         string
	Language      models.QueryLanguage // logchefql on both backends, or logsql without pipes on VictoriaLogs
	StreamFilters []string
}

// KeyTransform says how a field value becomes a group key.
type KeyTransform string

const (
	KeyValue   KeyTransform = "value"   // the field value as a string
	KeyMessage KeyTransform = "message" // logpattern extraction with the source's format, then Rules
	KeyPath    KeyTransform = "path"    // the text before '?', then Rules
)

type GroupKey struct {
	Field     string // a column, or column.path for a JSON key inside a string column
	Transform KeyTransform
}

type GroupCountRequest struct {
	Filter   AnalysisFilter
	Key      GroupKey
	Current  TimeRange
	Baseline *TimeRange // set: count both windows and order by share shift
	Buckets  int        // > 0: per-bucket counts of Current
	Limit    int
}

type GroupCounts struct {
	Groups                      []GroupCount
	CurrentTotal, BaselineTotal int64 // every matching row, not only the returned groups
	CurrentExcluded             Excluded
	Distinct                    int64
}

type Excluded struct{ Missing, Oversized int64 }

type GroupCount struct {
	Key               string
	Sample            string
	Current, Baseline int64
	Series            []int64
}

// GroupCounter is optional. A provider that does not implement it returns
// ErrOperationNotSupported through Service, like LogContextProvider.
type GroupCounter interface {
	CountGroups(ctx context.Context, source *models.Source, req GroupCountRequest) (*GroupCounts, error)
	DimensionFields(ctx context.Context, source *models.Source, window TimeRange, max int) ([]string, error)
}
```

JSON paths reuse the LogchefQL field compiler, which already turns `column.key` on a ClickHouse string column into `JSONExtractString` and passes the field through on VictoriaLogs. Patterns, Compare fields, and the click filter therefore read a JSON key the same way the query editor does.

`datasource.Service` owns the policy and calls the provider:

```go
func (s *Service) Patterns(ctx context.Context, id models.SourceID, req PatternsRequest) (*PatternsResult, error)
func (s *Service) ComparePeriods(ctx context.Context, id models.SourceID, req CompareRequest) (*CompareResult, error)
```

The policy covers the top 50 templates, 30 sparkline buckets, the top 20 compare rows, up to 8 compared fields, the ranking order, baseline resolution, the minimum count, and the click filter. It lives in `Service`, not in each provider. If each provider owned `Patterns()` and `Compare()`, both providers would contain the same rules, and the two copies would drift.

`core` adds the usual `access.AuthorizedSource` wrappers, `GetPatterns` and `ComparePeriods`, next to `GetAllFieldValues` in `internal/core/logs.go`. HTTP, MCP, and Ask Chef call these wrappers. Alerts have no user, so they call `Service` directly, as `EvaluateAlert` does today.

### Compare semantics

`logpattern.ResolveBaseline` turns the current window `[s, e)` and a baseline kind into a baseline window:

| Kind | URL `vs` | Baseline |
|---|---|---|
| `previous` | omitted | `[s - d, s)`, where `d = e - s` |
| `day` | `day` | `[s - 24h, e - 24h)` |
| `week` | `week` | `[s - 7d, e - 7d)` |

The offsets are exact UTC durations. Across a daylight-saving change, "same time yesterday" is 24 hours earlier, not the same wall-clock hour. A baseline that overlaps the current window is rejected, for example a 2-day window compared with `day`. When the source has a retention (`TTLDays > 0`), a baseline that starts before the retention cutoff is also rejected.

For each group, `shift = current / currentTotal - baseline / baselineTotal`. Shift is the change in share, from -1 to 1. The UI shows it in percentage points: 2% to 41% is `+39 pp`. Rows are ordered by the absolute value of the shift, then by current count, then by key. The database applies the same order before `LIMIT`, so a small value with a large jump is never cut by a top-N on volume. Share, not raw count, is what makes a rise in one status code visible when total traffic falls.

Two rules keep the report honest:

- A group with fewer than 5 rows in both windows is dropped after aggregation. A single new log line does not rank as the biggest change.
- If the baseline window has no matching rows, shares for the baseline are null and the status is `baseline_empty`, not `new`. The UI says "No matching logs in the baseline window". This covers expired data and sources that started recently.

Statuses are `new` (baseline 0, baseline population above 0), `gone` (current 0), `grew`, `shrank`, and `baseline_empty`.

`DimensionFields` picks the default fields to compare. During an incident the user does not yet know which field matters. On ClickHouse it reads the cached schema: the severity field first, then `LowCardinality(String)`, `Enum`, `Bool`, and integer columns of 16 bits or fewer (status codes), without the timestamp and message columns. On VictoriaLogs it calls `/select/logsql/facets` with `limit=1`, which already skips high-cardinality fields. Fields the user adds with "Compare by this field" come after the defaults, up to 8 in total. A field query that times out or exceeds the group cap goes into `skipped_fields`, which the UI shows. A failed template query fails the request.

### ClickHouse queries

All SQL is built in a new `internal/datasource/clickhouse_analysis.go`. Identifiers go through `ValidateIdentifier` and `queryescape.ClickHouseIdentifier`. The regex literals are constants from `logpattern`. The only user text in the SQL is the compiled LogchefQL fragment from the existing `logchefql.Translate` path. Windows use `>= start AND < end`, not the `BETWEEN` that older code uses, so adjacent windows never count a boundary row twice.

Patterns is one query. `m` is `ifNull(<message field>, '')` and `text` is `logpattern.ClickHouseMessageExpr(m, format)`:

```sql
SELECT
	if(length(m) > 4096, 'oversized', if(length(text) = 0, 'missing', 'message')) AS kind,
	if(kind = 'message', <logpattern.ClickHouseExpr(text)>, '') AS k,
	count() AS c,
	any(substring(m, 1, 1024)) AS sample_line,
	countResample(0, 30, 1)(intDiv(toUnixTimestamp64Milli(ts) - <startMs>, <stepMs>)) AS series,
	sum(count()) OVER () AS total,
	count() OVER () AS distinct_keys
FROM <db>.<table>
WHERE ts >= <start> AND ts < <end> AND (<compiled LogchefQL>)
GROUP BY kind, k
ORDER BY kind = 'message', c DESC
LIMIT <limit + 2>
SETTINGS max_rows_to_group_by = 50000, group_by_overflow_mode = 'throw',
	max_rows_to_read = 20000000, max_bytes_to_read = 1073741824,
	max_memory_usage = 536870912, max_execution_time = <remaining deadline>
```

The two excluded groups sort first, so `LIMIT limit + 2` always returns them along with the top templates. `countResample` returns one 30-slot array per template, so the group count does not grow with the number of buckets. Sample lines are cut to 1,024 bytes before aggregation.

Compare replaces the bucket array with `countIf(<in current>) AS cur, countIf(<in baseline>) AS base`, adds `sum(cur) OVER ()` and `sum(base) OVER ()` for the totals, and orders by the absolute share shift. The `WHERE` clause reads the two windows, not the gap between them. Field queries use `toString(<field expression>)` as the key, with the `path` transform when requested.

ClickHouse error code 158 (`TOO_MANY_ROWS`) and the read and memory limit errors map to `ErrAnalysisTooLarge`.

### VictoriaLogs queries

The LogsQL is built in a new `internal/victorialogs/analysis.go`. Every request goes through `applyScopeFilters`, which carries the source's tenant and stream scope, and through `readQueryRows` with a byte cap and a row cap. On optimizer sources each request takes a slot from the source's window limiter, and a window wider than `max_window_seconds` is rejected. A native LogsQL filter that contains a pipe is rejected with a validation error. Logchef does not drop the pipes, because the counts would no longer match the table.

Patterns uses two requests. The first returns the templates and totals:

```
<filter> | len(<field>) as _lc_len | copy <field> _lc_sample
| <logpattern.LogsQLMessagePipes(field, format)>   # sets _lc_text
| <logpattern.LogsQLPipes("_lc_text")>
| len(_lc_text) as _lc_text_len
| stats by (_lc_text)
	count() if (_lc_len:<=4096 _lc_text_len:>0) c,
	count() if (_lc_len:>4096) oversized,
	count() if (_lc_len:<=4096 _lc_text_len:0) missing,
	any(_lc_sample) sample
| total_stats sum(c) total, sum(oversized) total_oversized, sum(missing) total_missing, count() distinct
| sort by (c desc) | limit <limit>
```

The second request counts the returned templates per bucket with `stats by (_time:<step>, _lc_text) count()`, filtered to those templates. Its result has at most `limit × 30` rows. The two requests can see different late-arriving rows, so a sparkline's sum can differ slightly from the template's count. The count comes from the first request. On ClickHouse both come from one query.

Compare is one request over both windows, using `count() if (_time:[cs, ce)) cur` and `count() if (_time:[bs, be)) base`, `total_stats` for the totals, and `math` for the shift. VictoriaLogs limits the memory of a `stats` pipe on the server side. Logchef cannot set a per-query memory cap there. Operators size the VictoriaLogs memory limit for this workload.

### HTTP API

```
POST /api/v1/teams/{team}/sources/{source}/logs/patterns
POST /api/v1/teams/{team}/sources/{source}/logs/compare
```

Both routes sit next to `/logs/histogram` in `internal/server/server.go`. Each route uses `registerLimited` with `queryLimiter` and `requireTeamHasSource(TokenScopeLogsRead)`.

The request body follows the field-values convention: the filter plus explicit start and end, not a full native query.

```json
{
	"query": "service=\"checkout\" and level=\"error\"",
	"query_language": "logchefql",
	"variables": [],
	"stream_filters": [],
	"start": "2026-10-22T10:00:00Z",
	"end": "2026-10-22T11:00:00Z",
	"baseline": "previous",
	"fields": [{ "field": "attrs.method", "transform": "value" }, { "field": "url", "transform": "path" }]
}
```

`baseline` and `fields` apply to `/logs/compare` only. `fields` is optional and adds to the defaults. The Patterns response:

```json
{
	"status": "success",
	"data": {
		"message_field": "body",
		"message_format": "auto",
		"start": "2026-10-22T10:00:00Z",
		"end": "2026-10-22T11:00:00Z",
		"bucket_seconds": 120,
		"total": 812345,
		"missing": 4002,
		"oversized": 17,
		"other": 1210,
		"distinct": 37,
		"patterns": [
			{
				"template": "connection to <IP> timed out after <*>",
				"count": 402113,
				"sample": "connection to 10.0.4.7 timed out after 3000ms",
				"series": [13021, 13400],
				"filter": "body~\"connection to \" and body~\" timed out after \""
			}
		]
	}
}
```

The Compare response has `current` and `baseline` windows with their totals, plus `patterns` (absent without a message field), `fields`, and `skipped_fields`. Each row has `current`, `baseline`, `current_share`, `baseline_share`, `shift`, and `status`. A share is `null` when its window has no matching rows.

Counts are JSON numbers. Log counts per window stay far below 2^53, so the frontend does not need `bigint`.

### Bounds and errors

| Bound | Value |
|---|---|
| Admission | One slot of a new `analysis` query class in the existing `queryTracker`, for the whole request, including Compare's field queries |
| Backend concurrency | Compare's field queries run one at a time inside that slot, so one request never runs more than one backend query at once |
| Timeout | 30 seconds for the whole request, from `AnalysisTimeout` in `internal/core`. Field queries that do not start before the deadline go into `skipped_fields`. |
| ClickHouse | 50,000 groups, 20M rows read, 1 GiB read, 512 MiB memory, all with `throw` |
| VictoriaLogs | Response byte and row caps, the optimizer window limiter, `max_window_seconds` |
| Output | 50 templates, 30 buckets, 20 compare rows per list, 8 fields |
| Text | Templates and samples cut to 2,000 characters in the response, samples to 1,024 bytes in the query |
| Windows | Half-open. The baseline cannot overlap the current window or start before retention. |

The `analysis` class has its own per-user and global limits, separate from `preview`. An open Patterns tab therefore never blocks the user's own log queries, and a user who opens many tabs gets the existing "Too many active analysis queries" message.

`classifyQueryError` in `internal/server/query_errors.go` gets three new cases:

| Error | Status | Message |
|---|---|---|
| `ErrMessageFieldNotSet` | 400 | Set a message field for this source to see patterns. |
| `ErrAnalysisTooLarge` | 422 | Too many logs or patterns in this window. Narrow the time range or add a filter. |
| `ErrNativeSQLNotSupported` | 400 | Patterns and Compare need a LogchefQL query on this source. |

Timeouts, cancellations, admission limits, and validation errors use the existing mappings. A 500 never contains backend error text.

### How a click filters

The server returns each template's `filter` as LogchefQL. Both backends compile LogchefQL, and Explorer already appends LogchefQL when the user adds a filter from the field sidebar. `logpattern.Clause` builds the filter from the literal text between placeholders. Each literal run of three or more characters becomes `field~"text"` on the source's message field, and the six longest runs are kept in template order. A run that contains a quote or a backslash is dropped, because JSON and logfmt escape those characters in the stored field. A template with no usable run, such as `<*> <*>`, has no click.

LogchefQL `~` is a case-insensitive substring match on both backends. Every message that masks to a template contains each literal run of that template, and the stored field contains the message, so the filter matches every row of the pattern. It can also match a few similar rows. The Logs view after a click can therefore show more rows than the template's count. The row says so: "Filters by the template's text. Similar lines can match."

On dev VictoriaLogs the clause for a v1 template returned 1,161 rows, the same as the template's count. The filter is usually tight because templates keep long literal runs.

The click also freezes the window. Explorer replaces a relative window such as "last 1h" with the run's resolved absolute window, so the count after the click is from the same period as the template.

An exact filter needs a regex operator in LogchefQL, such as `=~`, compiled to `match()` on ClickHouse and `~"regex"` on VictoriaLogs. With that operator, only `logpattern.Clause` changes: it emits an anchored regex built from the literals and the rule patterns, against the extracted message. The API and the frontend stay the same.

### Frontend

**The view lives in the URL.** `lib/exploreLocation.ts` adds:

```ts
export type BaselineKind = 'previous' | 'day' | 'week'

export type ExploreView =
	| { kind: 'logs' }
	| { kind: 'patterns' }
	| { kind: 'compare'; baseline: BaselineKind }

export function parseExploreView(query: QueryParams): ExploreView
export function viewParams(view: ExploreView): Record<string, string>
```

`normalizeQueryParams` keeps `view` and `vs`. `sameExploreLocation` ignores them, so a view change is never a new location and never runs the query again. The navigator, `useExplorerNavigator.ts`, stays the only URL writer. It exposes `view` as a computed value from the route, `setView(view)`, which pushes a history entry and runs nothing, and `run({ view })`. Every `record()` call merges `viewParams(view)` into the query, so Run, zoom, window changes, and saved or shared links keep the view.

**The data hangs off the shown run.** A new composable, `useRunAnalysis(execution, view)`, fetches Patterns or Compare for `runs.shown` and the view. It builds the request from the execution, not from the editor: the query text, `variables`, `streamFilters`, and the resolved start and end. A new run, a zoom, a window change, or a view change aborts the request in flight. It keeps one result per view for the current execution, so switching tabs back does not fetch again. It aborts on KeepAlive deactivation and on scope dispose. No new Pinia store is added, and nothing copies the singleton pattern of `exploreHistogram`.

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

`too-large` comes from the shown run's histogram total before any request. A 422 from the server maps to the same state.

**Components.** The new files are `api/analysis.ts` (one client, unwraps the envelope once), `ExploreViewTabs.vue`, `ExploreAnalysis.vue` (states and empty states), `PatternsList.vue`, and `CompareReport.vue`. Sparklines reuse `SourceSparkline.vue`. Lists reuse `SearchInput`, `useTableSearchSort`, `LoadingState`, and `EmptyState` from `components/layout`. `LogExplorer.vue` grows by about 15 lines: the tab bar, one `v-if`, and two click handlers. The handler that appends a field filter is extracted from `handleAddFieldFilter` so the sidebar and Compare share it. The field sidebar gains "Compare by this field". New strings go into all 11 catalogs.

**Capability.** `CapabilityAnalysis = "analysis"` goes into both provider descriptors and into the frontend fallback list in `lib/queryMetadata.ts`. The tabs check `hasSourceCapability(source, 'analysis')`, and the Patterns tab also checks `source.message_field`.

### Later: MCP, Ask Chef, and alerts

MCP gets `log_patterns` and `what_changed` tools. Each one calls `core.GetPatterns` or `core.ComparePeriods` under the same `analysis` admission class, has an entry in `toolScopes`, and returns a short summary plus `Evidence` with the query and window. Ask Chef adds both names to `allowedTools` in `internal/askchef/tools.go`. The existing `compare_windows` tool compares two 500-row samples, and its `row_count` is the number of returned rows. The new tool replaces it, and `compare_windows` is deleted in the same change.

A "new pattern" alert counts templates with `base = 0`, `cur >= threshold`, and a baseline population above zero. The count must come from the database before `LIMIT`, so that the display limit of 20 never hides a new template from the alert. `GroupCountRequest` gets a count-only mode for this. The alert reads one number, which matches `ExtractFirstNumeric`. On VictoriaLogs the query carries explicit `_time` filters for both windows, so `applyAlertLookback` does not add its own window.

### Later: template labels

Patterns sorts by count, so the top row is usually the busiest healthy log line, such as "incoming request". On sources without a level field, nothing says which templates are errors. A small classification model can label each template, because templates are few and short: a window has tens to hundreds of them after masking.

Decision-style model APIs fit this task. They return one choice from a fixed set, a probability for a statement, or a score, with a confidence, instead of prose. OpenAI's Decisions API (public beta, October 2026) is one example. Other vendors and local models offer the same shape.

**What gets labelled.**

1. Each template gets a `kind`: `error`, `warning`, `retry`, `success`, `health_check`, `security`, or `other`, with a confidence.
2. Each Compare pattern row gets a probability for "this change is likely to affect users", from the template and its two shares.
3. Source setup can suggest the message field from the column names and types. An admin confirms the suggestion.

**What the model never sees.** Raw rows, sample lines, field values, and user input. The model reads masked template text, the two shares, and column names. Masking has already replaced IDs, IPs, and numbers with placeholders. Template text can still contain words from a log statement, so labelling is off by default and an operator turns it on in config.

**What labels never do.** Labels never filter or hide a template, and never fire, mute, or route an alert. They show as a chip, and they sort the list only when the user picks "Sort by kind". A label below the confidence threshold does not show.

**Shape.**

- `internal/ai` gains a `TemplateClassifier` interface at its consumer with two implementations: one for a decision-style endpoint that returns confidences, and one that uses the existing chat providers with structured output. The chat fallback returns a label with no confidence and shows it as unconfirmed.
- A separate route, `POST /logs/patterns/labels`, takes up to 500 templates and returns labels. Patterns never waits for labels. The UI fetches labels after the patterns arrive.
- An in-process LRU cache keyed by model and template hash holds up to 10,000 labels, so a template is labelled once, not on every view. v1 stores nothing in the metadata database.
- Every call has a 5-second timeout and no retry. A failed call returns no labels, and the UI shows the patterns without labels.
- Config: `[ai.template_labels]` with `enabled = false`, the provider, the model, and the confidence threshold.

**Cost.** Labelling scales with templates, not rows. 500 templates of about 40 tokens each is 20,000 input tokens per uncached window. Labelling rows instead would scale with log volume and is rejected.

**Calibration.** No vendor publishes accuracy for this task, so the threshold comes from our own data. The repository gets a labelled set of synthetic templates, and a test reports precision per kind for the configured model. The threshold ships only after that report.

## Verification so far

These queries ran against the dev stack on 2026-10-22 and 2026-10-23. They prove that the query shapes and the rules work, not that the feature works.

**Synthetic corpus.** 200,000 rows in five shapes: a plain message column, JSON in a string column, logfmt in a string column, access-log lines, and messages with mixed letter-and-digit IDs. Each shape comes from 1 to 4 log statements.

| Shape | Rows | v1 rules (digits only, no extraction) | v2 rules (extraction and word rule) |
|---|---|---|---|
| Plain message | 40,000 | 28 | 4 |
| JSON in a string | 40,000 | 78 | 3 |
| logfmt in a string | 40,000 | 78 | 3 |
| Access-log line | 40,000 | 26 | 1 |
| Mixed IDs | 40,000 | 16,569 | 1 |

v2 finds exactly one template per log statement in every shape. ClickHouse and VictoriaLogs produced the same 12 template strings for the corpus, with the extraction written as `multiIf` on ClickHouse and as `unpack_json`, `unpack_logfmt`, and conditional `format` pipes on VictoriaLogs.

**Scale.**

| Check | Backend | Result |
|---|---|---|
| v1 rules on the corpus at 2M rows, with all resource caps | ClickHouse 26.7.3.19 | Error 158: more than 50,000 groups |
| v2 rules on the same 2M rows, with all resource caps | ClickHouse 26.7.3.19 | 12 templates, 351 ms, 56 MiB peak |
| Two-window `countIf` compare | ClickHouse 26.7.3.19, 2M rows | 706 ms, 18.8 MiB peak |
| Group cap overflow | ClickHouse 26.7.3.19 | Error code 158, as expected |
| v1 mask list, length guard, excluded counts, `total_stats` | VictoriaLogs v1.52.0, 6,000 rows | 26 ms, totals correct |
| Two-window compare with `count() if` and `math` | VictoriaLogs v1.52.0 | Correct `cur`, `prev`, and `delta` per template |
| Literal-run click filter vs exact template | VictoriaLogs v1.52.0 | 1,161 = 1,161 |

The scan cap matters more than the group cap on large shared tables. An unfiltered window of a few minutes on a busy table can read tens of millions of rows. With a filter on one service, the same query reads a small part of that. This is why the UI asks for a filter before it runs a query over the row cap.

No test has run yet on a wide production table, such as an OTel table with a large `body` column. Before release, time a one-day window on such a table.

## Alternatives considered

**Digit-only masks.** The first draft of this RFC replaced only digits with `<N>`. On mixed IDs it produced a template per ID and overflowed the group cap at 2M rows. The word rule fixes this at the cost of masking a few words that are not IDs, such as `http2` and `utf8`.

**Masking the whole string of a JSON or logfmt row.** No extraction code. But every field in the body, such as a user ID or a path, becomes part of the template, so one log statement makes thousands of templates.

**Each backend's own normalizer (`collapse_nums` on VictoriaLogs, a regex chain on ClickHouse).** Less code on VictoriaLogs. But the templates differ between backends, IDs with letters stay unmasked, and the click filter needs a derivation per backend.

**Grouping the loaded rows in the browser.** No backend work. But the counts cover only the loaded page, 100 rows by default, and on windowed-search sources only the pages loaded so far. It fails the first goal.

**Drain-style clustering in Go over a stream of rows.** Better templates for free text. But Logchef must read rows out of the backend, a sample breaks "counts cover the window", and no backend can filter by a cluster that only exists in Go.

**Classifying rows with a model.** Labels on every row would let the user filter by kind. But the cost and latency grow with log volume, raw rows leave the deployment, and the result is not reproducible. Labels on templates give most of the value at a fixed, small cost.

**An exact pattern filter threaded through every query path.** A second design in this review made a click exact. It added a structured `pattern_match` that LogchefQL compilation, the histogram, field values, shares, and the URL all carry. It also added its own admission counters and capped windows at one hour. The exact click is better for the user. The cost is a change to every query path and to saved and shared content, before anyone has used the feature. The literal-text filter plus a later `=~` operator gets most of the benefit for one function.

**The message field as a request parameter.** No migration. But every caller then picks the field. The source already owns the timestamp and severity fields the same way.

**`Patterns()` and `Compare()` as provider methods.** Each provider would own ranking, buckets, top N, and baselines, so the same rules would exist twice.

**Field queries in parallel.** Faster Compare. But one request would run up to eight backend queries at once, which competes with the user's own queries for backend capacity. Sequential queries with a deadline and `skipped_fields` keep one request to one backend query at a time.

**One endpoint with an optional baseline.** One route fewer, but one response type with two shapes.

**The view in a Pinia store or as part of the location.** A store is not shareable and is lost on Back. Making the view part of the location runs the query again on every tab switch.

## Tradeoffs accepted

- We accept five regex passes and a JSON or logfmt parse per message in exchange for the same templates on both backends and one file to tune.
- We accept that words such as `http2` become `<*>` in exchange for one template per log statement when messages carry IDs.
- We accept a click filter that can match a few similar lines in exchange for no LogchefQL grammar change in v1.
- We accept no Patterns or Compare on ClickHouse native SQL in exchange for not parsing user SQL.
- We accept a slower Compare with sequential field queries in exchange for one backend query per request at a time.
- We accept two source settings and a migration in exchange for one answer to "which field is the message, and how to read it" for every caller.
- We accept that VictoriaLogs sparklines come from a second request in exchange for not multiplying the group count by the bucket count.
- We accept that ClickHouse fails above 50,000 distinct templates while VictoriaLogs answers. ClickHouse holds every group in memory, and VictoriaLogs has its own server-side limit.

## Open questions

1. Is a click filter that can match similar lines acceptable for v1, or should the LogchefQL `=~` operator ship first?
2. Should the message keys be configurable per source, or are `msg` and `message` enough until a user asks?
3. Is 20M rows read the right default cap, and should an admin be able to raise it per source?
4. What window can Patterns serve on a wide ClickHouse table within 30 seconds? The answer sets the default.
5. Should VictoriaLogs `DimensionFields` prefer stream fields over facet order?
6. Should the table's default message column and the log detail view also read `_meta_message_field`? Today both guess from `MESSAGE_FIELD_ALIASES`. Using the setting removes two guesses.
7. For template labels, which classifier ships first: a decision-style endpoint with confidences, or the chat fallback that works with every configured provider?

## Implementation plan

Each step is one PR that ends in a passing check.

1. `internal/logpattern` with its tests: message extraction, the rules, `Clause` matching every message of a fixture corpus, `ResolveBaseline`, `Shift`, and `StatusOf`. The corpus has one fixture per source shape, and a test asserts the template count and the top-50 coverage for each.
2. Message settings: migrations in both stores, `sqlc.yaml`, models, provider validation, provisioning, the source form, and conformance tests.
3. `GroupCounter` on both providers, with an integration test that seeds the corpus into dev ClickHouse and VictoriaLogs and asserts the same templates and counts, including rows exactly at window boundaries.
4. `Service.Patterns`, `Service.ComparePeriods`, the core wrappers, the two routes, the `analysis` admission class, and the error mappings.
5. Frontend: `api/analysis.ts`, the URL view, `useRunAnalysis`, the four components, "Compare by this field", and i18n. Verify in the browser on both dev backends: Patterns, Compare, a click, Back, a zoom, a source without a message field, and KeepAlive deactivation.
6. Follow-ups: MCP and Ask Chef tools (deleting `compare_windows`), the LogchefQL `=~` operator, the "new pattern" alert, and template labels.
