# RFC 0002: Log patterns and window compare in Explorer

Status: Proposed
Date: 2026-10-22
Authors: Karan Sharma

## Summary

Explorer shows rows. During an incident the first two questions are "what kinds of messages are there?" and "what changed?". Today the user answers both by scrolling rows and guessing.

This RFC adds two server-side summaries of the query and window that Explorer is showing:

- **Patterns** groups the messages of the window into templates such as `connection to <IP>:<N> timed out after <N>ms`. Each template shows a count over the whole window, a sparkline, and one sample line. A click on a template filters Explorer to it.
- **Compare** compares the window with a baseline of equal length: the previous period, the same window yesterday, or the same window last week. It ranks field values and templates by how much their share of matching logs moved, and it shows the change in total volume.

Both features run as one aggregate query per question in the log backend. Neither feature reads rows into Logchef. Both work on ClickHouse and VictoriaLogs and produce the same template text on both.

The design keeps three decisions in one place each:

- `internal/logpattern` owns the mask list, the ranking rules, and baseline arithmetic. It is pure Go with no I/O.
- `datasource.Service` owns the policy: limits, buckets, ranking, and the click filter. It calls one optional provider method, `CountGroups`.
- The Explorer URL owns the selected view. The navigator stays its only writer. A view switch runs no query.

## Motivation

A user opens Explorer on `service="checkout" and level="error"` for the last hour and gets 800,000 rows. Explorer shows the newest 100. The histogram shows that the error rate went up at 10:20. The user still does not know which errors went up.

Two things answer that question quickly:

1. A list of message templates with counts. 800,000 rows that come from 30 log statements become 30 lines.
2. A diff against a normal period. "`service=payments` went from 2% to 41% of errors" and "`failed to connect to db host=<IP> port=<N>` is new" are the answers an on-call engineer wants first.

Grafana's log panels and the ClickHouse plugin do neither. VictoriaLogs has building blocks (`collapse_nums`, `count() if`) but no UI that uses them. Logchef already has the window, the filter, the access model, and the histogram. These two views are the missing step between seeing a spike and knowing its cause.

Ask Chef and the MCP server need the same answers as cheap, bounded tools. An agent that reads 30 templates with counts reasons better than an agent that reads 500 rows. This RFC builds the operations so that those tools, and a later "new pattern" alert, call the same code.

## Goals

- Counts cover every matching row in the window, on both backends, including VictoriaLogs windowed-search sources.
- The same message gives the same template on ClickHouse and VictoriaLogs.
- A user can go from a template to its logs in one click.
- Compare ranks by change in share, so a small value with a large jump ranks above a large value that stayed flat.
- Every request has a timeout, an admission slot, a group cap, and a row cap. A request that is too large fails with a message that tells the user to narrow it. It never returns partial counts as if they were complete.
- One implementation serves the HTTP API, the MCP tools, Ask Chef, and later alerts.

## Non-goals for v1

- Learned clustering (Drain or similar), custom mask rules per source, and a stored catalog of templates.
- Patterns or Compare on ClickHouse native SQL. The user's SQL fixes its own time range and projection, so Compare cannot add a baseline to it. VictoriaLogs native LogsQL is supported when it is a filter with no pipes.
- Sparklines in Compare. Compare rows show both counts and both shares.
- An exact pattern filter. v1 filters by the literal text of the template. See [How a click filters](#how-a-click-filters).
- The MCP tools, the Ask Chef allowlist entries, and the alert type. This RFC defines their call sites so that they are small follow-ups.

## User experience

Explorer gains a tab bar above the results: **Logs**, **Patterns**, and **Compare**. **Logs** is today's view, with its table, compact, and JSON modes unchanged. The tab bar is hidden during live tail and on sources without the `patterns` capability.

**Patterns.** The header reads "37 patterns · top 50 shown · 1,210 other · 4,002 without a message". Each row has a sparkline, the count, the share of the window, and the template. Placeholders (`<N>`, `<IP>`, `<UUID>`, `<HEX>`) render as muted chips. A row expands to show one raw sample line. A click on a row adds a filter for that template and switches to **Logs**.

**Compare.** A baseline select offers **Previous period**, **Same time yesterday**, and **Same time last week**. Both windows appear as concrete timestamps. The report has three parts:

1. Volume: "812,345 vs 401,002 matching logs (+103%)".
2. Fields that shifted: `service = payments 2.0% → 41.0%`. A click adds `service="payments"` to the query.
3. Patterns that shifted, with a status of **new**, **grew**, **shrank**, or **gone**. A click filters to the template.

The URL records the view: `...&view=patterns`, or `...&view=compare&vs=day`. Back and Forward move between views without running the query again. A shared link opens on the same view.

When a ClickHouse source has no message field set, **Patterns** explains why it is empty. An admin sees a one-click button such as "Use `body`", and other users see "Ask an admin to set the message field".

## Design

### The mask list

`internal/logpattern` holds the only list of mask rules. Both backends render their query text from it, and the click filter derives from it.

```go
package logpattern

// Rule replaces every match of Pattern with Placeholder. Placeholders contain
// no digits, so a later digit rule never rewrites an earlier placeholder.
type Rule struct {
	Name        string
	Pattern     string // RE2. ClickHouse (re2) and VictoriaLogs (Go regexp) read it the same way.
	Placeholder string
}

// Rules is ordered: specific shapes first, plain digits last.
var Rules = []Rule{
	{"uuid", `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`, "<UUID>"},
	{"ipv4", `\b[0-9]{1,3}(?:\.[0-9]{1,3}){3}\b`, "<IP>"},
	{"hex", `[0-9a-fA-F]{12,}`, "<HEX>"},
	{"number", `[0-9]+`, "<N>"},
}

func ClickHouseExpr(quotedColumn string) string // nested replaceRegexpAll calls, in rule order
func LogsQLPipes(quotedField string) string     // one `| replace_regexp (...) at <field>` per rule
```

We use our own list instead of each backend's native normalizer for three reasons:

- The template text is the same on both backends. A conformance test feeds one corpus through both dev backends and compares the results with a Go reference implementation of the rules.
- The digit rule masks digits inside words. `collapse_nums` leaves `user107` as is. On dev VictoriaLogs, `collapse_nums prettify` produced 446 templates where this list produced 57, for about five real log statements.
- When a mask is wrong, one file changes.

`ClickHouse normalizeQuery` was also tried. It splits one UUID into several placeholders and has no VictoriaLogs equivalent.

Messages longer than 4,096 bytes and empty or missing messages are counted but not masked. The response reports them as `oversized` and `missing`, separate from `other`. The length guard keeps the regex cost per row bounded.

### The message field

Patterns needs to know which field holds the free text. Sources have no such field today. They have `_meta_ts_field` and `_meta_severity_field`. This RFC adds `_meta_message_field` next to them:

- New migrations: SQLite `000039_source_message_field` and Postgres `000013_source_message_field`. Each adds the column as `NOT NULL DEFAULT ''`, then sets `_msg` for VictoriaLogs sources whose value is empty. Running the migration twice changes nothing. Both up files go into `sqlc.yaml`. The store conformance suite covers the new column.
- Source create and update requests, and the provisioning TOML, accept `meta_message_field`. ClickHouse `PrepareSource` and `UpdateSource` check that the column exists and is a string type. VictoriaLogs defaults it to `_msg`.
- The source form prefills the field from `MESSAGE_FIELD_ALIASES` in `frontend/src/lib/sourceFields.ts`, the list Explorer already uses to pick a message column. Logchef keeps one alias list. The server never guesses.

A source setting gives one answer to every caller: the UI, MCP, Ask Chef, and alerts. A request parameter would make each caller guess, and a server-side guess would pick the wrong column without saying so. Existing ClickHouse sources need an admin to set the field once.

### One provider method

Patterns and Compare run the same query shape. Each groups the matching rows by a key, counts the rows in the current window (and the baseline), keeps a sample, and returns the top N groups. The key is either the masked message field or a raw field value. The provider interface therefore gets one optional method plus a helper that picks fields:

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

type GroupKey struct {
	Field  string
	Masked bool // group by logpattern.Rules applied to Field
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
	Groups                       []GroupCount
	CurrentTotal, BaselineTotal  int64 // every matching row, not only the returned groups
	CurrentExcluded              Excluded
	Distinct                     int64
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

`datasource.Service` owns the policy and calls the provider:

```go
func (s *Service) Patterns(ctx context.Context, id models.SourceID, req PatternsRequest) (*PatternsResult, error)
func (s *Service) ComparePeriods(ctx context.Context, id models.SourceID, req CompareRequest) (*CompareResult, error)
```

The policy covers the top 50 templates, 30 sparkline buckets, the top 20 compare rows, up to 8 compared fields with 4 queries at a time, the ranking order, baseline resolution, the minimum count, and the click filter. It lives in `Service`, not in each provider. If each provider owned `Patterns()` and `Compare()`, both providers would contain the same rules, and the two copies would drift.

`core` adds the usual `access.AuthorizedSource` wrappers, `GetPatterns` and `ComparePeriods`, next to `GetAllFieldValues` in `internal/core/logs.go`. HTTP, MCP, and Ask Chef call these wrappers. Alerts have no user, so they call `Service` directly, as `EvaluateAlert` does today.

### Compare semantics

`logpattern.ResolveBaseline` turns the current window `[s, e)` and a baseline kind into a baseline window:

| Kind | URL `vs` | Baseline |
|---|---|---|
| `previous` | omitted | `[s - d, s)`, where `d = e - s` |
| `day` | `day` | `[s - 24h, e - 24h)` |
| `week` | `week` | `[s - 7d, e - 7d)` |

The offsets are exact UTC durations. Across a daylight-saving change, "same time yesterday" is 24 hours earlier, not the same wall-clock hour. A baseline that overlaps the current window is rejected, for example a 2-day window compared with `day`. When the source has a retention (`TTLDays > 0`), a baseline that starts before the retention cutoff is also rejected.

For each group, `shift = current / currentTotal - baseline / baselineTotal`. Shift is the change in share, from -1 to 1. The UI shows it in percentage points: 2% to 41% is `+39 pp`. Rows are ordered by the absolute value of the shift, then by current count, then by key. The database applies the same order before `LIMIT`, so a small value with a large jump is never cut by a top-N on volume.

Two rules keep the report honest:

- A group with fewer than 5 rows in both windows is dropped after aggregation. A single new log line does not rank as the biggest change.
- If the baseline window has no matching rows, shares for the baseline are null and the status is `baseline_empty`, not `new`. The UI says "No matching logs in the baseline window". This covers expired data and sources that started recently.

Statuses are `new` (baseline 0, baseline population above 0), `gone` (current 0), `grew`, `shrank`, and `baseline_empty`.

`DimensionFields` picks the fields to compare without a request from the user. During an incident the user does not yet know which field matters. On ClickHouse it reads the cached schema: the severity field first, then `LowCardinality(String)` and `Enum` columns, without the timestamp and message columns. On VictoriaLogs it calls `/select/logsql/facets` with `limit=1`, which already skips high-cardinality fields. A field query that times out or exceeds the group cap goes into `skipped_fields`, which the UI shows. A failed template query fails the request.

### ClickHouse queries

All SQL is built in a new `internal/datasource/clickhouse_analysis.go`. Identifiers go through `ValidateIdentifier` and `queryescape.ClickHouseIdentifier`. The regex literals are constants from `logpattern.Rules`. The only user text in the SQL is the compiled LogchefQL fragment from the existing `logchefql.Translate` path. Windows use `>= start AND < end`, not the `BETWEEN` that older code uses, so adjacent windows never count a boundary row twice.

Patterns is one query:

```sql
SELECT
	if(length(msg) = 0, 'missing', if(length(msg) > 4096, 'oversized', 'message')) AS kind,
	if(kind = 'message', <logpattern.ClickHouseExpr(msg)>, '') AS k,
	count() AS c,
	any(substring(msg, 1, 1024)) AS sample_line,
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

Compare replaces the bucket array with `countIf(<in current>) AS cur, countIf(<in baseline>) AS base`, adds `sum(cur) OVER ()` and `sum(base) OVER ()` for the totals, and orders by the absolute share shift. The `WHERE` clause reads the two windows, not the gap between them. Field queries use `toString(<field>)` as the key and skip the mask.

ClickHouse error code 158 (`TOO_MANY_ROWS`) and the read and memory limit errors map to `ErrAnalysisTooLarge`.

### VictoriaLogs queries

The LogsQL is built in a new `internal/victorialogs/analysis.go`. Every request goes through `applyScopeFilters`, which carries the source's tenant and stream scope, and through `readQueryRows` with a byte cap and a row cap. On optimizer sources each request takes a slot from the source's window limiter, and a window wider than `max_window_seconds` is rejected. A native LogsQL filter that contains a pipe is rejected with a validation error. Logchef does not drop the pipes, because the counts would no longer match the table.

Patterns uses two requests. The first returns the templates and totals:

```
<filter> | len(_msg) as _lc_len | copy _msg _lc_sample
| <logpattern.LogsQLPipes("_msg")>
| stats by (_msg)
	count() if (_lc_len:range[1, 4096]) c,
	count() if (_lc_len:>4096) oversized,
	count() if (_lc_len:0) missing,
	any(_lc_sample) sample
| total_stats sum(c) total, sum(oversized) total_oversized, sum(missing) total_missing, count() distinct
| sort by (c desc) | limit <limit>
```

The second request counts the returned templates per bucket with `stats by (_time:<step>, _msg) count()`, filtered to those templates. Its result has at most `limit × 30` rows. The two requests can see different late-arriving rows, so a sparkline's sum can differ slightly from the template's count. The count comes from the first request. On ClickHouse both come from one query.

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
	"baseline": "previous"
}
```

`baseline` applies to `/logs/compare` only. The Patterns response:

```json
{
	"status": "success",
	"data": {
		"message_field": "body",
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
				"template": "connection to <IP>:<N> timed out after <N>ms",
				"count": 402113,
				"sample": "connection to 10.0.4.7:5432 timed out after 3000ms",
				"series": [13021, 13400],
				"filter": "body~\"connection to \" and body~\" timed out after \""
			}
		]
	}
}
```

The Compare response has `current` and `baseline` windows with their totals, plus `patterns`, `fields`, and `skipped_fields`. Each row has `current`, `baseline`, `current_share`, `baseline_share`, `shift`, and `status`. A share is `null` when its window has no matching rows.

Counts are JSON numbers. Log counts per window stay far below 2^53, so the frontend does not need `bigint`.

### Bounds and errors

| Bound | Value |
|---|---|
| Admission | One slot of a new `analysis` query class in the existing `queryTracker`, for the whole request, including Compare's field queries |
| Timeout | 30 seconds for the whole request, from `AnalysisTimeout` in `internal/core` |
| ClickHouse | 50,000 groups, 20M rows read, 1 GiB read, 512 MiB memory, all with `throw` |
| VictoriaLogs | Response byte and row caps, the optimizer window limiter, `max_window_seconds` |
| Output | 50 templates, 30 buckets, 20 compare rows per list, 8 fields, 4 field queries at a time |
| Text | Templates and samples cut to 2,000 characters in the response, samples to 1,024 bytes in the query |
| Windows | Half-open. The baseline cannot overlap the current window or start before retention. |

`classifyQueryError` in `internal/server/query_errors.go` gets three new cases:

| Error | Status | Message |
|---|---|---|
| `ErrMessageFieldNotSet` | 400 | Set a message field for this source to see patterns. |
| `ErrAnalysisTooLarge` | 422 | Too many logs or patterns in this window. Narrow the time range or add a filter. |
| `ErrNativeSQLNotSupported` | 400 | Patterns and Compare need a LogchefQL query on this source. |

Timeouts, cancellations, admission limits, and validation errors use the existing mappings. A 500 never contains backend error text.

### How a click filters

The server returns each template's `filter` as LogchefQL. Both backends compile LogchefQL, and Explorer already appends LogchefQL when the user adds a filter from the field sidebar. `logpattern.Clause` builds the filter from the literal text between placeholders. Each literal run of three or more characters becomes `field~"text"`, and the six longest runs are kept in template order. A template with no such run, such as `<N> <N>`, has no click.

LogchefQL `~` is a case-insensitive substring match on both backends. Every message that masks to a template contains each literal run of that template, so the filter matches every row of the pattern. It can also match a few similar rows. The Logs view after a click can therefore show more rows than the template's count. The row says so: "Filters by the template's text. Similar lines can match."

On dev VictoriaLogs the clause for `user user<N> logged in from <IP>` returned 1,161 rows, the same as the template's count. The filter is usually tight because templates keep long literal runs.

The click also freezes the window. Explorer replaces a relative window such as "last 1h" with the run's resolved absolute window, so the count after the click is from the same period as the template.

An exact filter needs a regex operator in LogchefQL, such as `=~`, compiled to `match()` on ClickHouse and `~"regex"` on VictoriaLogs. With that operator, only `logpattern.Clause` changes: it emits an anchored regex built from the literals and the rule patterns. On dev VictoriaLogs that anchored regex returned exactly the template's count. The API and the frontend stay the same.

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
	| { status: 'unsupported'; reason: 'native-sql' | 'no-message-field' }
	| { status: 'loading' }
	| { status: 'patterns'; data: PatternsResult }
	| { status: 'compare'; data: Comparison }
	| { status: 'failed'; message: string }
```

**Components.** The new files are `api/analysis.ts` (one client, unwraps the envelope once), `ExploreViewTabs.vue`, `ExploreAnalysis.vue` (states and empty states), `PatternsList.vue`, and `CompareReport.vue`. Sparklines reuse `SourceSparkline.vue`. Lists reuse `SearchInput`, `useTableSearchSort`, `LoadingState`, and `EmptyState` from `components/layout`. `LogExplorer.vue` grows by about 15 lines: the tab bar, one `v-if`, and two click handlers. The handler that appends a field filter is extracted from `handleAddFieldFilter` so the sidebar and Compare share it. New strings go into all 11 catalogs.

**Capability.** `CapabilityPatterns = "patterns"` goes into both provider descriptors and into the frontend fallback list in `lib/queryMetadata.ts`. The tabs check `hasSourceCapability(source, 'patterns')`.

### Later: MCP, Ask Chef, and alerts

MCP gets `log_patterns` and `what_changed` tools. Each one calls `core.GetPatterns` or `core.ComparePeriods` under the same `analysis` admission class, has an entry in `toolScopes`, and returns a short summary plus `Evidence` with the query and window. Ask Chef adds both names to `allowedTools` in `internal/askchef/tools.go`. The existing `compare_windows` tool compares two 500-row samples, and its `row_count` is the number of returned rows. The new tool replaces it, and `compare_windows` is deleted in the same change.

A "new pattern" alert counts templates with `base = 0`, `cur >= threshold`, and a baseline population above zero. The count must come from the database before `LIMIT`, so that the display limit of 20 never hides a new template from the alert. `GroupCountRequest` gets a count-only mode for this. The alert reads one number, which matches `ExtractFirstNumeric`. On VictoriaLogs the query carries explicit `_time` filters for both windows, so `applyAlertLookback` does not add its own window.

## Verification so far

These queries ran against the dev stack on 2026-10-22. They prove that the query shapes work, not that the feature works.

| Check | Backend | Result |
|---|---|---|
| Mask list, length guard, excluded counts, `total_stats` | VictoriaLogs v1.52.0, 6,000 rows | 26 ms, 6 templates, totals correct |
| `collapse_nums prettify` on the same rows | VictoriaLogs v1.52.0 | 446 templates |
| Mask list with the digit rule | VictoriaLogs v1.52.0 | 57 templates on the wider dev data |
| Two-window compare with `count() if` and `math` | VictoriaLogs v1.52.0 | Correct `cur`, `prev`, and `delta` per template |
| Literal-run click filter vs exact template | VictoriaLogs v1.52.0 | 1,161 = 1,161 |
| Patterns query with exclusions, `countResample`, and all resource caps | ClickHouse 26.7.3.19, 2M generated rows | 0.38 s, 4 templates plus `missing` |
| Regex chain only, no caps | ClickHouse 26.7.3.19, 2M rows | 684 ms, 15.5 MiB peak |
| Two-window `countIf` compare | ClickHouse 26.7.3.19, 2M rows | 706 ms, 18.8 MiB peak |
| Group cap overflow | ClickHouse 26.7.3.19 | Error code 158, as expected |

No test has run yet on a wide production ClickHouse table, such as an OTel table with a large `body` column. Before release, time a one-day window on such a table.

## Alternatives considered

**Each backend's own normalizer (`collapse_nums` on VictoriaLogs, a regex chain on ClickHouse).** Less code on VictoriaLogs. But the templates differ between backends, `user107` stays unmasked, and the click filter needs a derivation per backend.

**Grouping the loaded rows in the browser.** No backend work. But the counts cover only the loaded page, 100 rows by default, and on windowed-search sources only the pages loaded so far. It fails the first goal.

**Drain-style clustering in Go over a stream of rows.** Better templates for free text. But Logchef must read rows out of the backend, a sample breaks "counts cover the window", and no backend can filter by a cluster that only exists in Go.

**An exact pattern filter threaded through every query path.** A second design in this review made a click exact. It added a structured `pattern_match` that LogchefQL compilation, the histogram, field values, shares, and the URL all carry. It also added its own admission counters and capped windows at one hour. The exact click is better for the user. The cost is a change to every query path and to saved and shared content, before anyone has used the feature. The literal-text filter plus a later `=~` operator gets most of the benefit for one function.

**The message field as a request parameter.** No migration. But every caller then picks the field, and the MCP sketch in that design hardcoded `_msg`. The source already owns the timestamp and severity fields the same way.

**`Patterns()` and `Compare()` as provider methods.** Each provider would own ranking, buckets, top N, and baselines, so the same rules would exist twice.

**One endpoint with an optional baseline.** One route fewer, but one response type with two shapes.

**The view in a Pinia store or as part of the location.** A store is not shareable and is lost on Back. Making the view part of the location runs the query again on every tab switch.

## Tradeoffs accepted

- We accept four regex passes per message in exchange for the same templates on both backends and one file to tune.
- We accept a click filter that can match a few similar lines in exchange for no LogchefQL grammar change in v1.
- We accept no Patterns or Compare on ClickHouse native SQL in exchange for not parsing user SQL.
- We accept one admission slot for up to nine backend queries in Compare in exchange for one simple budget per user. Field queries run at most four at a time.
- We accept a source migration in exchange for one answer to "which field is the message" for every caller.
- We accept that VictoriaLogs sparklines come from a second request in exchange for not multiplying the group count by the bucket count.
- We accept that ClickHouse fails above 50,000 distinct templates while VictoriaLogs answers. ClickHouse holds every group in memory, and VictoriaLogs has its own server-side limit.

## Open questions

1. Is a click filter that can match similar lines acceptable for v1, or should the LogchefQL `=~` operator ship first?
2. Is 12 hex characters the right threshold for `<HEX>`? A 13-digit millisecond timestamp becomes `<HEX>`, not `<N>`. That is harmless for grouping and odd to read.
3. What window can Patterns serve on a wide production ClickHouse table within 30 seconds? The answer sets the default and whether the UI should warn above a size.
4. Should VictoriaLogs `DimensionFields` prefer stream fields over facet order?
5. Should the table's default message column and the log detail view also read `_meta_message_field`? Today both guess from `MESSAGE_FIELD_ALIASES`. Using the setting removes two guesses.

## Implementation plan

Each step is one PR that ends in a passing check.

1. `internal/logpattern` with its tests: the rule rendering, `Clause` matching every message of a fixture corpus, `ResolveBaseline`, `Shift`, and `StatusOf`.
2. `_meta_message_field`: migrations in both stores, `sqlc.yaml`, models, provider validation, provisioning, the source form, and conformance tests.
3. `GroupCounter` on both providers, with an integration test that seeds the same rows into dev ClickHouse and VictoriaLogs and asserts the same templates and counts, including rows exactly at window boundaries.
4. `Service.Patterns`, `Service.ComparePeriods`, the core wrappers, the two routes, the `analysis` admission class, and the error mappings.
5. Frontend: `api/analysis.ts`, the URL view, `useRunAnalysis`, the four components, and i18n. Verify in the browser on both dev backends: Patterns, Compare, a click, Back, a zoom, and KeepAlive deactivation.
6. Follow-ups: MCP and Ask Chef tools (deleting `compare_windows`), the LogchefQL `=~` operator, and the "new pattern" alert.
