# PRD: Reliable LogChef Query Workflow

**Date:** 2026-04-20

---

## Summary

LogChef should feel simple to users:

- **Run** a query and inspect logs quickly.
- **Download** larger results when the browser table is not the right tool.
- **Share** an ad hoc query with a short link.
- **Save** useful queries into team collections.
- **Use the CLI** for scripts and large streams.

Internally, these are not the same workload. The implementation must split preview queries, streaming exports, share state, and CLI access into separate paths with separate limits, timeouts, memory behavior, and observability.

This is the same product idea as Metabase: keep the visible workflow simple, but enforce strong query constraints and stream bulk responses below the UI.

---

## Problem Statement

LogChef currently lets interactive query execution, browser URL state, local table exports, CLI-style usage, and large downloads collapse into one broad flow.

That creates two production failure modes:

- Raising `query.max_limit` to support downloads also made native SQL preview queries eligible to return huge result sets. A missing `LIMIT` fell back to the maximum, not a sane preview default.
- The API reads ClickHouse rows into `[]map[string]interface{}` before JSON serialization. A large result can push the Go process past the Nomad memory allocation and trigger exit code 137.

There is also a separate share/link reliability issue:

- The frontend puts full raw SQL into URL query parameters. Large ad hoc SQL, especially long `IN (...)` lists, can exceed HTTP request header/read-buffer limits and fail before any ClickHouse query runs.

The core issue is not ClickHouse alone. It is that the product has one visible action but the system lacks explicit internal workload boundaries.

---

## Goals

- Make the default browser query path safe, fast, and memory bounded.
- Keep the user model simple: Run, Download, Share, Save, CLI.
- Support large downloads and CLI workflows without buffering full results in the API process.
- Stop putting full query text in URLs.
- Preserve native SQL power while making dangerous defaults impossible.
- Add enough metrics, logs, and limits that bad workloads degrade gracefully instead of restarting the service.

---

## Non-Goals

- General-purpose pagination for arbitrary SQL.
- Async export jobs in v1.
- A new service, queue, or distributed worker system in v1.
- Perfect cardinality or cost estimation before query execution.
- Replacing ClickHouse or changing all log schemas.
- Making the browser render millions of rows.

---

## Product Contract

### Run

Run is the normal browser table flow. It is for looking at logs, not extracting a dataset.

Users can:

- Run LogchefQL.
- Run native ClickHouse SQL.
- See the first bounded set of results.
- See whether a limit was applied.
- Cancel an active query.
- Download or copy a CLI command when they need more.

Users should not need to understand internal terms like preview mode, result overflow, or row caps. The UI can say:

```text
Showing 1,000 rows. Download for larger results.
```

### Download

Download is the bulk path. It streams CSV or NDJSON from ClickHouse through LogChef.

Users can:

- Choose format.
- Choose a row limit up to the configured download maximum.
- Download directly in the browser.
- Copy the equivalent CLI command.

Download must not reuse the browser table JSON endpoint.

### Share

Share creates a short link to the current ad hoc query.

Users can:

- Copy a short URL.
- Open a shared query if they still have team/source access.
- Save the query to a collection if it should become durable team knowledge.

Raw SQL must not be stored in the browser URL.

### Save

Save keeps the existing collection model. Saved queries are durable, named, team/source-scoped objects.

Share is for temporary ad hoc exchange. Save is for reusable queries.

### CLI

CLI supports both small pretty output and large streaming output.

Users can:

- Run a small query and see a table.
- Stream CSV or NDJSON for scripts.
- Write output to a file without mixing stats into stdout.

---

## End State

When complete:

- Native SQL without `LIMIT` uses a small preview default, not the maximum configured limit.
- Browser Run cannot return more than the preview row cap or response byte cap.
- Browser Run returns clear limit/truncation metadata.
- Download streams rows and does not allocate memory proportional to total rows.
- CLI uses streaming paths for large outputs.
- Raw SQL and LogchefQL are no longer written into normal URLs.
- Ad hoc share links use stored server-side state with expiry and access checks.
- Query cancellation cancels ClickHouse work.
- Metrics expose rows, bytes, duration, truncation, rejection, active queries, active downloads, and process memory.
- Production alerts fire before memory pressure turns into a restart loop.

---

## Internal Architecture

### Execution Profiles

The user does not see these names, but the backend must.

| Profile | User action | Response | Memory model | Typical cap |
|---|---|---|---|---|
| `preview` | Run | JSON object with rows, columns, stats | bounded buffer | 1k default, 100k max |
| `download` | Download | CSV or NDJSON stream | O(columns + one row) | 1m max initially |
| `cli_stream` | CLI stream | CSV or NDJSON stream | O(columns + one row) | same as download |
| `cursor` | Load more for generated log queries | JSON page | bounded buffer | page size |
| `share` | Copy/open link | stored query payload | SQLite row | TTL-bound |

`cursor` is intentionally limited to LogchefQL/generated SQL where LogChef controls ordering and filters. Arbitrary SQL stays bounded preview or streaming download.

### Configuration

Replace the single overloaded `query.max_limit` with explicit limits.

```toml
[query]
default_preview_limit = 1000
max_preview_limit = 100000
max_response_bytes = 67108864
default_timeout_seconds = 30
max_timeout_seconds = 120
max_concurrent_per_user = 3
max_concurrent_global = 30

[export]
max_rows = 1000000
default_timeout_seconds = 120
max_timeout_seconds = 600
max_concurrent_per_user = 1
max_concurrent_global = 5
formats = ["csv", "ndjson"]

[shares]
default_ttl = "720h"
max_query_text_bytes = 1048576
```

Migration rule:

- Keep `query.max_limit` temporarily as a deprecated alias for `query.max_preview_limit`.
- Log a warning at boot if the deprecated key is used.
- Remove the alias after one release.

### Query Planning

Add an internal query planner before execution.

Input:

- source metadata
- query language: `logchefql` or `clickhouse_sql`
- raw query text
- execution profile
- requested limit
- requested timeout
- user/team/source

Output:

```go
type QueryPlan struct {
    ID              string
    Profile         QueryProfile
    Language        QueryLanguage
    SQL             string
    RequestedLimit  int
    AppliedLimit    int
    Timeout         time.Duration
    ClickHouseSettings map[string]any
    Warnings        []QueryWarning
}
```

Planner rules:

- Missing native SQL `LIMIT` on preview gets `default_preview_limit`.
- Explicit native SQL `LIMIT` on preview is capped to `max_preview_limit`.
- Download and CLI stream use `export.max_rows`.
- LogchefQL always gets an explicit limit from the selected profile.
- A profile mismatch returns `400` with a product-level message:

```text
This result is too large for Run. Use Download or CLI.
```

ClickHouse settings should be applied per query, not only globally:

```text
max_execution_time = applied timeout
max_result_rows = applied limit + 1
result_overflow_mode = break
```

The SQL `LIMIT` is still required. ClickHouse settings are a second guardrail in case parsing or query shape misses something.

### Preview Executor

Preview can remain buffered, but only after the planner makes it bounded.

Requirements:

- Uses `QueryPlan.Profile == preview`.
- Buffers at most `max_preview_limit` rows.
- Tracks approximate encoded response bytes.
- Stops before `max_response_bytes`.
- Returns `warnings`.
- Returns `limit_applied`.
- Returns `truncated` if LogChef stops early due to row or byte cap.
- Cancels ClickHouse when client cancellation or explicit cancellation happens.

Response:

```json
{
  "query_id": "01HV...",
  "data": [],
  "columns": [],
  "stats": {
    "execution_time_ms": 1200,
    "rows_returned": 1000,
    "limit_applied": 1000,
    "bytes_returned": 921337,
    "truncated": true,
    "truncated_reason": "row_limit"
  },
  "warnings": [
    {
      "code": "LIMIT_APPLIED",
      "message": "Showing 1,000 rows. Download for larger results."
    }
  ]
}
```

### Streaming Executor

Download and CLI stream must not call the current buffered query method.

Add a streaming API around ClickHouse row iteration:

```go
type RowWriter interface {
    Begin(columns []models.ColumnInfo) error
    WriteRow(row map[string]any) error
    Finish(stats StreamStats) error
}

func (c *Client) QueryStream(ctx context.Context, plan QueryPlan, writer RowWriter) error
```

Implementation requirements:

- Reuse row scanning/type conversion helpers with preview.
- Write each row directly to the response writer.
- Flush periodically.
- Count rows and bytes.
- Stop at applied limit.
- Close rows on all paths.
- Cancel ClickHouse context on client disconnect.
- Do not build `[]map[string]interface{}` for full results.

Formats:

- NDJSON is the best machine format because it can include metadata and final stats.
- CSV is the best human/spreadsheet format, but final stats should be exposed through UI state, logs, and metrics rather than appended to the file.

NDJSON:

```json
{"type":"meta","query_id":"01HV...","columns":[{"name":"timestamp","type":"DateTime"}],"limit_applied":1000000}
{"type":"row","row":{"timestamp":"2026-04-20T10:00:00Z","level":"error"}}
{"type":"stats","rows_returned":1000000,"truncated":true,"truncated_reason":"row_limit"}
```

CSV:

```text
timestamp,level,message
2026-04-20T10:00:00Z,error,...
```

### Cursor Executor

Cursor pagination is useful for generated log exploration, but not for arbitrary SQL.

Requirements:

- Only enabled for LogchefQL/generated source-table queries.
- Requires deterministic `ORDER BY`.
- Uses timestamp descending by default.
- Uses configured tie-breaker when available.
- Returns an opaque cursor.
- Marks cursor semantics as `exact` or `best_effort`.

Recommended source metadata extension:

```toml
[source.query]
timestamp_field = "http_timestamp"
cursor_tie_breaker = "request_id"
default_order = "desc"
```

If a source has no tie-breaker, timestamp-only pagination is acceptable for log browsing but must be documented as best-effort.

### Query Shares

URLs should carry identifiers, not full query text.

URL:

```text
/logs/explore?team=8&source=12&share=01HV...
```

API:

```http
POST /api/v1/teams/:teamID/sources/:sourceID/query-shares
GET  /api/v1/query-shares/:token
DELETE /api/v1/query-shares/:token
```

SQLite:

```sql
CREATE TABLE query_shares (
  token TEXT PRIMARY KEY,
  team_id INTEGER NOT NULL,
  source_id INTEGER NOT NULL,
  created_by INTEGER NOT NULL,
  payload_json TEXT NOT NULL,
  expires_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_accessed_at DATETIME
);

CREATE INDEX idx_query_shares_expiry ON query_shares(expires_at);
CREATE INDEX idx_query_shares_team_source ON query_shares(team_id, source_id);
```

Payload:

```json
{
  "mode": "sql",
  "query_language": "clickhouse_sql",
  "query_text": "SELECT ...",
  "limit": 1000,
  "time_range": {
    "type": "relative",
    "value": "15m"
  },
  "timezone": "Asia/Kolkata",
  "variables": []
}
```

Security:

- Token must be opaque and unguessable.
- Share resolution must check current user access to team and source.
- Expired shares return `404`.
- Query text should not be emitted in access logs.
- A cleanup task removes expired rows.

### Frontend State

Normal URL parameters:

```text
team
source
id
share
mode
limit
t
start
end
```

Normal URLs must not include:

```text
sql
q
```

Draft handling:

- Unsaved editor content lives in local storage or session storage.
- Draft key is scoped by user/team/source/mode.
- Loading an old URL with `?sql=` or `?q=` imports the query into a local draft, cleans the URL, and shows a one-time notice.
- Copy share link calls the server and writes the returned short URL.

Download UI:

- The current local table export remains available as “Export shown rows” if needed.
- “Download results” calls the streaming endpoint.
- Large limits live in the download flow, not the Run limit selector.
- The UI can show the equivalent CLI command.

### CLI Behavior

Small default:

```bash
logchef query 'level="error"' --limit 100
```

Streaming:

```bash
logchef query 'level="error"' --stream --format ndjson --limit 1000000
logchef sql 'SELECT ...' --stream --format csv --output result.csv
```

Rules:

- Table output uses preview.
- `--stream`, `--format csv`, `--format ndjson`, or large `--limit` uses streaming.
- Stats go to stderr.
- Row data goes to stdout or `--output`.
- CLI exits non-zero on server-side error.
- CLI should warn when output is capped by server config.

---

## API Contract

### Preview

Existing endpoint remains, but semantics become preview-only:

```http
POST /api/v1/teams/:teamID/sources/:sourceID/logs/query
```

Request:

```json
{
  "raw_sql": "SELECT * FROM logs.http_access WHERE ...",
  "limit": 1000,
  "query_timeout": 30,
  "variables": []
}
```

Response:

```json
{
  "query_id": "01HV...",
  "data": [],
  "columns": [],
  "stats": {
    "execution_time_ms": 1200,
    "rows_returned": 1000,
    "bytes_returned": 921337,
    "limit_applied": 1000,
    "truncated": true,
    "truncated_reason": "row_limit"
  },
  "warnings": []
}
```

### Download

```http
POST /api/v1/teams/:teamID/sources/:sourceID/logs/export
Accept: text/csv
```

```http
POST /api/v1/teams/:teamID/sources/:sourceID/logs/export
Accept: application/x-ndjson
```

Request:

```json
{
  "raw_sql": "SELECT ...",
  "format": "csv",
  "limit": 1000000,
  "query_timeout": 300,
  "variables": []
}
```

Headers:

```text
Content-Type: text/csv; charset=utf-8
Content-Disposition: attachment; filename="logchef-2026-04-20.csv"
X-LogChef-Query-ID: 01HV...
X-LogChef-Limit-Applied: 1000000
```

### Share

```http
POST /api/v1/teams/:teamID/sources/:sourceID/query-shares
```

```json
{
  "payload": {
    "mode": "sql",
    "query_language": "clickhouse_sql",
    "query_text": "SELECT ...",
    "limit": 1000,
    "time_range": {"type": "relative", "value": "15m"},
    "timezone": "Asia/Kolkata",
    "variables": []
  }
}
```

Response:

```json
{
  "token": "01HV...",
  "url": "/logs/explore?team=8&source=12&share=01HV...",
  "expires_at": "2026-05-20T00:00:00Z"
}
```

Resolve:

```http
GET /api/v1/query-shares/:token
```

---

## Reliability Requirements

### Limits

Limits are enforcement, not UI hints.

- Preview row cap enforced in planner and executor.
- Download row cap enforced in planner and executor.
- Response byte cap enforced for preview.
- Query text byte cap enforced for shares.
- Timeout enforced in HTTP client, server context, and ClickHouse settings.

### Concurrency

Add simple in-process limiters first.

- Preview and download have separate pools.
- Per-user limits prevent one user from saturating the service.
- Global limits protect the process.

This does not need Redis or a distributed lock in v1. A single LogChef instance can own the limit. If LogChef becomes horizontally scaled later, add a shared limiter.

### Cancellation

Cancellation paths:

- Browser cancel button.
- Browser tab closed / HTTP disconnect.
- CLI interrupted.
- Server timeout.
- Query tracker cleanup.

All paths must cancel the Go context passed to ClickHouse.

### Memory

Preview:

- Memory bounded by row cap, byte cap, and JSON response size.

Download/CLI:

- Memory bounded by scan destinations, row map, encoder buffers, and columns.
- Memory must not grow with total returned rows.

### Server Buffers

Increasing Fiber read buffers is not the fix for large SQL URLs. The fix is not placing SQL in URLs.

Server request/body limits should still be explicit:

- Accept normal JSON request bodies large enough for ad hoc SQL and variables.
- Reject share payloads above `shares.max_query_text_bytes`.
- Keep headers small by design.

---

## Observability

Metrics:

```text
logchef_query_requests_total{profile,language,status,source_id}
logchef_query_duration_seconds_bucket{profile,source_id}
logchef_query_rows_returned_bucket{profile,source_id}
logchef_query_bytes_returned_bucket{profile,source_id}
logchef_query_truncated_total{profile,reason,source_id}
logchef_query_rejected_total{profile,reason,source_id}
logchef_active_queries{profile}
logchef_export_disconnects_total{source_id}
logchef_query_limit_applied_total{profile,reason,source_id}
process_resident_memory_bytes
go_memstats_heap_alloc_bytes
```

Structured logs:

- query ID
- user email or ID
- team ID
- source ID
- profile
- language
- requested limit
- applied limit
- requested timeout
- applied timeout
- rows returned
- bytes returned
- duration
- truncated
- rejection reason

Never log full query text by default. For debugging, log query hash and a short prefix.

Dashboards:

- Top users by rows returned.
- Top users by bytes returned.
- Top sources by rows/bytes.
- Active preview and download queries.
- Rejections by reason.
- Truncation rate.
- Process RSS vs Nomad memory allocation.
- Restarts and OOM kills.

Alerts:

- Process RSS above 80% of allocation for 5 minutes.
- Active downloads at global limit for 5 minutes.
- Query rejection spike.
- Restart count greater than 0.
- Preview p95 duration above target.

---

## Testing Strategy

### Backend Unit Tests

Query planner:

- no SQL limit uses `default_preview_limit`
- explicit SQL limit below cap is preserved
- explicit SQL limit above preview cap is capped or rejected according to profile
- download limit uses export cap
- timeout capped to profile max
- warnings are emitted correctly

Query builder:

- top-level `LIMIT` detection
- no multiple statements
- invalid SQL error shape
- legacy `max_limit` alias behavior

Share store:

- create, resolve, expire, delete
- team/source access required
- oversized payload rejected

### Backend Integration Tests

Preview:

- returns rows, columns, stats, warnings
- caps rows
- caps response bytes
- cancellation removes query tracker entry

Export:

- streams NDJSON without buffering full result
- streams CSV with headers
- cancels on client disconnect
- applies row cap
- emits metrics

### Frontend Tests

- URL sync excludes raw SQL and LogchefQL.
- Old `?sql=` URL imports draft and cleans URL.
- Copy share link calls share API and writes short URL.
- Run warning displays applied limit.
- Download flow calls export endpoint, not table CSV utility.
- Limit selector only shows preview-safe values.

### Manual Production Verification

- Run native SQL with no `LIMIT`; confirm only default preview rows.
- Run native SQL with `LIMIT 1000000`; confirm Run does not return 1m rows.
- Download 1m rows; confirm RSS stays flat relative to row count.
- Open a shared long SQL query; confirm URL is short.
- Cancel a query; confirm ClickHouse query is cancelled and tracker cleaned.

---

## Rollout Plan

### Phase 1: Safe Preview Semantics

- Add explicit config fields.
- Keep deprecated `max_limit` alias.
- Add query planner.
- Change missing native SQL `LIMIT` fallback to `default_preview_limit`.
- Cap preview limits.
- Add warnings and richer stats to response.
- Add tests around limit behavior.

This phase prevents the same OOM class even before streaming download exists.

### Phase 2: URL State and Share Links

- Stop writing `sql` and `q` into normal URLs.
- Add local draft storage.
- Add query share SQLite migration and APIs.
- Add “Copy share link”.
- Resolve `share` on page load.
- Import and clean legacy `?sql=` / `?q=` URLs.

This phase fixes `Request Header Fields Too Large` correctly.

### Phase 3: Streaming Download and CLI

- Add `QueryStream`.
- Add CSV and NDJSON row writers.
- Add `/logs/export`.
- Change browser “Download results” to stream endpoint.
- Update CLI to use stream endpoint for large results.
- Add export metrics and cancellation.

This phase allows large outputs without API OOM.

### Phase 4: Cursor for Generated Log Queries

- Add cursor API for LogchefQL/generated log queries.
- Use timestamp and optional tie-breaker.
- Keep arbitrary SQL out of cursor pagination.
- Add Load More / infinite page behavior where it improves UX.

This phase improves exploration without pretending arbitrary SQL has safe pagination.

### Phase 5: Hardening

- Add concurrency limiters.
- Add response byte cap.
- Add process memory guard for new downloads.
- Add dashboards and alerts.
- Remove deprecated `max_limit` behavior after rollout.

---

## Acceptance Criteria

- [ ] Browser Run uses `default_preview_limit` when SQL has no explicit limit.
- [ ] Browser Run cannot return more than `max_preview_limit`.
- [ ] Browser Run cannot exceed configured response byte cap.
- [ ] Preview response includes `limit_applied`, `rows_returned`, `bytes_returned`, `truncated`, and `warnings`.
- [ ] Large downloads use `/logs/export`.
- [ ] Export streams CSV and NDJSON without buffering the full result.
- [ ] API memory remains bounded during a 1,000,000-row export.
- [ ] CLI streams large output and writes stats to stderr.
- [ ] Raw SQL and LogchefQL are no longer written into normal browser URLs.
- [ ] Old URLs with `?sql=` and `?q=` still load during migration and clean themselves.
- [ ] Ad hoc share links use opaque tokens and expire.
- [ ] Share resolution enforces team/source access.
- [ ] Query cancellation cancels ClickHouse execution.
- [ ] Metrics and structured logs expose profile, limits, rows, bytes, duration, truncation, and rejection reason.
- [ ] Alerts catch memory pressure before OOM restarts.

---

## Key Code Areas

Backend:

- `internal/config/config.go` - explicit query/export/share config.
- `internal/clickhouse/query_builder.go` - SQL parsing and limit rewriting.
- `internal/clickhouse/client.go` - split buffered preview from streaming execution.
- `internal/core/logs.go` - route execution through query plans.
- `internal/server/logs_handlers.go` - preview, export, cancellation handlers.
- `internal/server/server.go` - route registration and server request limits.
- `internal/sqlite/migrations/` - query share migration.
- `internal/metrics/` - query/export metrics.

Frontend:

- `frontend/src/stores/explore.ts` - URL state, local drafts, query metadata.
- `frontend/src/composables/useUrlState.ts` - remove query text from URL sync.
- `frontend/src/api/explore.ts` - preview and export API clients.
- `frontend/src/views/explore/LogExplorer.vue` - run/download/share actions.
- `frontend/src/views/explore/components/ResultsToolbar.vue` - download/share entry points.
- `frontend/src/views/explore/table/export.ts` - local shown-rows export remains separate from server download.

CLI:

- Locate existing CLI entrypoints and route large output through `/logs/export`.

---

## Open Decisions

| Decision | Recommendation |
|---|---|
| Preview default | `1000` rows |
| Preview max | `100000` rows for now |
| Download max | `1000000` rows initially |
| Async exports | Not v1; use synchronous streaming first |
| Arbitrary SQL pagination | No |
| LogchefQL cursor | Yes, when deterministic order is available |
| Share storage | SQLite with TTL cleanup |
| CSV truncation signal | UI/header/metrics, not appended to CSV |
| NDJSON stats | Include final stats line |

---

## References

- Metabase ClickHouse connection docs expose ClickHouse settings such as `max_result_rows` and connection limits.
- Metabase applies small API row limits for browser display and separate larger limits for exports.
- Metabase uses streaming response writers for exported/API result formats instead of one giant in-memory result object.
- LogChef should copy these concepts, not Metabase's Clojure/JDBC implementation.
