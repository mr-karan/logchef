# RFC 0011: Loki datasource

Status: Draft
Date: 2026-11-15
Authors: Karan Sharma

## Summary

Logchef queries two log backends, ClickHouse and VictoriaLogs. This RFC adds
Grafana Loki as a third, built the way VictoriaLogs was: a provider package
behind `datasource.Provider`, a LogchefQL generator, a native editor mode and a
source form.

A Loki source is a base URL, an optional tenant, and a default stream selector.
Users search it with LogchefQL, which compiles to LogQL, or with native LogQL.
The Explorer, histogram, field sidebar, live tail, saved queries, alerts and
dashboards work on it. Exports, windowed search and the source activity chart
do not, and "Non-goals" says why.

The target is Loki 3.7. Three facts about Loki shape the design. Every query
needs a stream selector. Loki returns at most 5,000 entries per request. Loki
counts and aggregates only through metric queries.

## Problem

Teams that run Loki cannot point Logchef at it. The migration guide tells them
to move their logs to ClickHouse or VictoriaLogs first
(`docs/src/content/docs/migrate/from-loki.md:7-10`).

The provider layer was built for more than two backends, but the two existing
backends are named in many places:

| Place | Evidence |
|---|---|
| Source types are a closed set of two | `pkg/models/source.go:14-17`, `pkg/models/source.go:26-33` |
| Both metadata stores reject any other type with a CHECK constraint | `internal/store/sqlite/migrations/000025_datasource_sources.up.sql:11`, `internal/store/postgres/migrations/000002_datasource_multibackend.up.sql:10` |
| Query languages are a closed set of three | `pkg/models/query.go:50-54`, `pkg/models/query.go:73` |
| Alert metadata accepts only ClickHouse SQL and LogsQL | `pkg/models/alerts.go:76`, `pkg/models/alerts.go:90` |
| Identity, hydration and redaction switch on the source type | `pkg/models/source.go:347`, `pkg/models/source.go:366`, `pkg/models/source.go:393`, `pkg/models/source.go:465` |
| Native queries pick a template dialect with `IsVictoriaLogs()` | `internal/core/query.go:474` |
| LogchefQL queries pass a time range only when the target is LogsQL | `internal/core/query.go:268` |
| The AI target is LogsQL for VictoriaLogs and ClickHouse SQL for anything else | `internal/server/ai_sql_handlers.go:68` |
| The frontend derives the native language from "supports `logsql`" | `frontend/src/lib/queryMetadata.ts:88` |
| The source form models two datasource types | `frontend/src/views/sources/components/sourceFormModels.ts:10` |
| Provisioning validates two connection shapes | `internal/provisioning/validate.go:85`, `internal/config/provisioning.go:151` |

The interface itself needs little. `datasource.Provider` has 17 methods
(`internal/datasource/service.go:19-37`), and optional interfaces cover tail,
log context, LogchefQL compilation and streaming
(`internal/datasource/service.go:257`). The VictoriaLogs provider is the
template (`internal/victorialogs/provider.go:148`).

## Goals and non-goals

Goals:

- A global admin adds a Loki source in the UI, by provisioning TOML or by the
  API, and links it to teams.
- LogchefQL and native LogQL both run in the Explorer, saved queries,
  dashboards and the CLI. LogQL alerts run in native and condition mode.
- The histogram, field sidebar, live tail and log context work.
- Every request to Loki has a bound on entries, bytes and time.
- The dev stack has a Loki service with seed data, and CI runs integration
  tests against it.

Non-goals, each with its reason:

- **Exports.** Both export paths call the ClickHouse client directly
  (`internal/server/export_handlers.go:72`,
  `internal/server/export_job_handlers.go:231`). VictoriaLogs has no exports
  for the same reason. Loki does not set `CapabilityExports`. A provider-neutral
  export is its own RFC.
- **Windowed search.** The handler accepts only VictoriaLogs
  (`internal/server/windowed_handlers.go:57`). Loki's query frontend already
  splits a query by time (`split_queries_by_interval`, default `1h`) and stops
  when it has enough entries, so a second splitter in Logchef adds requests and
  no speed.
- **Source activity chart.** `SourceActivityProvider` needs row counts per hour
  for seven days (`internal/datasource/inspection_cache.go:27`). Loki can count
  rows only by reading every chunk in the range, and its index statistics
  exclude data that ingesters still hold. Loki does not implement the
  interface. The inspection page shows index statistics instead.
- **Extra stream filters.** `extra_stream_filters` stays VictoriaLogs-only
  (`internal/datasource/stream_filters.go:35`). A LogchefQL filter on a stream
  label already becomes a stream matcher.
- **Query-time enforcement of a restriction.** See "Access and security".
- **Ingestion, rules, and the Loki ruler API.** Logchef reads logs. It
  evaluates its own alerts.
- **A LogQL parser in Go.** The official parser lives in the Loki module, which
  would add hundreds of transitive dependencies. The design never classifies
  or rewrites LogQL text. Loki's response type and Loki's own errors decide.
- **LogchefQL aggregations on Loki.** RFC 0005 compiles one aggregation to one
  backend query. The Loki generator rejects aggregation nodes in this RFC.
  "Seams" records the mapping for a later RFC.

## Design

### Loki version and API

The provider targets Loki 3.7.x and is tested against `grafana/loki:3.7.6`.
Loki 3.0 is the minimum, because structured metadata and
`/loki/api/v1/detected_fields` are on by default from 3.0. The provider reads
the version from `/loki/api/v1/status/buildinfo` and shows it on the
inspection page. An older version produces a warning at validation and no
error.

All endpoints are in the
[Loki HTTP API reference for 3.7](https://grafana.com/docs/loki/v3.7.x/reference/loki-http-api/).
The section names below are that page's headings.

| Endpoint | Docs section | Used for |
|---|---|---|
| `GET /loki/api/v1/query_range` | Query logs within a range of time | Log search, histogram, tail polls, log context, metric results |
| `GET /loki/api/v1/query` | Query logs at a single point in time | Alert evaluation, field values, top groups |
| `GET /loki/api/v1/labels` | Query labels | Schema, health |
| `GET /loki/api/v1/label/<name>/values` | Query label values | Sidebar values for stream labels |
| `GET /loki/api/v1/series` | Query streams | Log context, to prove that a row names one stream |
| `GET /loki/api/v1/detected_fields` | Detected fields | Schema for structured metadata and parsed fields |
| `GET /loki/api/v1/index/stats` | Query log statistics | Inspection page |
| `GET /loki/api/v1/status/buildinfo` | Show build information | Version |

The provider does not call `/loki/api/v1/tail`, `/ready`,
`/loki/api/v1/index/volume` or `/loki/api/v1/patterns`. "Live tail" and
"Alternatives rejected" give the reasons.

### Data shape

**Source type and language.** `pkg/models/source.go` gains
`SourceTypeLoki SourceType = "loki"`. `pkg/models/query.go` gains
`QueryLanguageLogQL QueryLanguage = "logql"`. Both `Valid()` methods and
`NormalizeQueryLanguage` accept the new values.

**Connection.** A Loki source stores this JSON in `connection_config`, as
VictoriaLogs does (`pkg/models/source.go:218`):

```go
type LokiConnectionInfo struct {
	BaseURL  string            `json:"base_url"`
	Auth     LokiAuth          `json:"auth,omitempty"`
	TenantID string            `json:"tenant_id,omitempty"`
	Selector string            `json:"selector"`
	Principal string           `json:"principal,omitempty"`
	Parser   string            `json:"parser,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Limits   *LokiLimits       `json:"limits,omitempty"`
}

type LokiAuth struct {
	Mode     string `json:"mode,omitempty"` // "none", "basic", "bearer"
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

type LokiLimits struct {
	MaxEntriesPerQuery     int `json:"max_entries_per_query,omitempty"`
	SidebarLookbackSeconds int `json:"sidebar_lookback_seconds,omitempty"`
	SchemaLookbackSeconds  int `json:"schema_lookback_seconds,omitempty"`
}
```

| Field | Rule |
|---|---|
| `base_url` | Required. `http` or `https`. The provider appends `/loki/api/v1/...` |
| `auth.mode` | `none`, `basic` or `bearer`. Unlike VictoriaLogs, an unset mode means `none`. There is no auto-detect |
| `tenant_id` | Optional. Sent as `X-Scope-OrgID`. At most 150 bytes. Letters, digits and ``!-_.*'()``, with a pipe between tenant IDs |
| `selector` | Required. One LogQL stream selector, such as `{namespace="payments"}`. It must have a matcher that does not match the empty string. The form suggests `{service_name=~".+"}` |
| `principal` | Optional. A name for the access scope of the credential, such as `payments-readonly`. At most 64 characters of letters, digits, `-`, `_` and `.`. Not a secret and not sent to Loki. It is part of the identity |
| `parser` | Empty, `json` or `logfmt`. See "Field classes" |
| `headers` | Extra request headers. `Authorization` and `X-Scope-OrgID` are reserved and rejected, as the VictoriaLogs provider rejects its own (`internal/victorialogs/provider.go:50-54`) |
| `limits.max_entries_per_query` | Entries per request. Default 5000, range 100 to 50000. Must not exceed the tenant's `max_entries_limit_per_query` |
| `limits.sidebar_lookback_seconds` | Default 900, range 60 to 86400 |
| `limits.schema_lookback_seconds` | Default 3600, range 60 to 86400 |

`meta_ts_field` defaults to `_time`. `meta_severity_field` defaults to
`detected_level`, the structured metadata key that Loki adds when
`discover_log_levels` is on, which is the default.

**Identity.** `BuildLokiIdentityKey` returns
`loki:<normalized base URL>|tenant=<tenant_id>|selector=<selector>|principal=<principal>`.
It reuses the URL normalization that VictoriaLogs uses. Two sources that
differ only in the selector are different sources. `CreateSource` rejects a
duplicate identity (`internal/datasource/service.go:132-135`), so two sources
that reach one endpoint and tenant through different gateway credentials must
differ in `principal`. The key never holds a password or a token, and a
credential rotation does not change it. `base_url`, `tenant_id`, `selector`
and `principal` are immutable after creation, as the VictoriaLogs tenant and
scope are.

**Redaction.** `RedactedConnectionConfig` blanks `auth.password`, `auth.token`
and every header value. A blank secret on update means "keep the stored
value", as in `mergeRedactedConnectionSecrets`
(`internal/victorialogs/provider.go:436`).

**Row shape.** Loki returns streams. Each stream has a label set and a list of
`[nanosecond timestamp, line]` pairs. The provider merges the streams by
timestamp and flattens each entry into one row:

| Column | Value |
|---|---|
| `_time` | The entry timestamp as RFC 3339 with nanoseconds, in UTC |
| `_msg` | The log line |
| one column per label | Every label of the entry's stream object. Loki puts stream labels, structured metadata and parsed fields into that one object |

`_time` and `_msg` match the VictoriaLogs names, so the Explorer's message and
timestamp handling needs no new case, and `_msg~"timeout"` means the same in
LogchefQL on both backends. A label named `_time` or `_msg` loses to the
reserved column. Every label value is a string.

A metric query returns a matrix or a vector. Each sample becomes one row with
`time` (RFC 3339, UTC), `value` (the sample as a decimal string) and one column
per series label. `value` comes first, because alert evaluation reads the first
column (`internal/victorialogs/querying.go:750-763`).

**Field classes.** LogQL treats three kinds of field differently, and the
schema records the kind in `ColumnInfo.Type`:

| Class | Where it comes from | `Type` | Needs a parser stage |
|---|---|---|---|
| Stream label | `/loki/api/v1/labels` | `label` | No. It is also valid in the stream selector |
| Structured metadata | `detected_fields` entries with no parser | `string`, `int`, `float`, `boolean`, `duration`, `bytes` | No |
| Parsed field | `detected_fields` entries whose `parsers` list has the source's `parser` | Same types | Yes |

`parser` is a property of the source. When it is `json` or `logfmt`, every
compiled LogchefQL query runs that parser, so parsed fields appear as columns
and LogchefQL can filter on them. When it is empty, Logchef shows only stream
labels and structured metadata, and a user who wants parsed fields writes
`| json` in native LogQL. One setting per source keeps a query's meaning
independent of what a sample of recent lines happened to contain.

### Migrations

Both stores constrain `sources.source_type`. Each needs one migration, named
by purpose, with the next free number at build time.

| Store | Migration | Change |
|---|---|---|
| Postgres | `allow_loki_source_type` | Drop the CHECK on `sources.source_type` and add it again with `'clickhouse', 'victorialogs', 'loki'`. The down migration restores the two-value CHECK and fails if a Loki source exists |
| SQLite | `allow_loki_source_type` | SQLite cannot alter a CHECK. Rebuild `sources` as `sources_new` with the three-value CHECK, copy every row, drop and rename, and recreate the indexes, as migration 25 did (`internal/store/sqlite/migrations/000025_datasource_sources.up.sql:7-71`). The build unit copies the column list from the current schema, not from migration 25 |

The SQLite rebuild relies on how Logchef runs migrations. They run on a
dedicated connection opened with the bare file path
(`internal/store/sqlite/sqlite.go:126-129`), so `foreign_keys` is off for the
whole migration and dropping `sources` does not cascade into child tables.
The migration ships an upgrade test beside `TestDatasourceUpgradeFromV24`
(`internal/store/sqlite/upgrade_test.go:23`). The test seeds a source with
team links, saved queries, alerts and dashboards, runs the migration, and
asserts that every child row survives and still joins to its source.

No query in `queries.sql` changes, so `just sqlc-generate` produces no diff.
`query_language` columns have no CHECK in either store, so `logql` needs no
migration. A store conformance test in `internal/store/storetest/` creates a
`loki` source in both backends.

### API

No new route. Existing routes accept the new values.

| Route or field | Change |
|---|---|
| `POST /admin/sources`, `POST /admin/sources/validate` | Accept `source_type: "loki"` with a `connection` object of the shape above |
| `PUT /admin/sources/:id` | Accepts changes to `auth`, `parser`, `headers`, `limits`, name, description and meta fields. Rejects a changed `base_url`, `tenant_id`, `selector` or `principal` with 400 |
| Source responses | `source_type: "loki"`, `query_languages: ["logchefql", "logql"]`, the capability list under "Capability flags" |
| `POST /logs/query`, saved queries, alerts, dashboards, tail | Accept `query_language: "logql"` on a Loki source |
| `POST /logs/context` | New optional `stream_labels` object. See "Log context" |

**Errors.** Today a provider failure is a 400 when it is a
`datasource.ValidationError` and a 500 otherwise
(`internal/server/query_errors.go:31-63`). That contract cannot say "the
backend is busy" or "the backend is down". This RFC adds one provider-neutral
error type, `datasource.UpstreamError`, with a `Kind`, the upstream HTTP
status and a message. `classifyQueryError` gains one case per kind, placed
before its default case:

| Loki outcome | Provider returns | HTTP response |
|---|---|---|
| 400, 404, 422 | `ValidationError` with Loki's message | 400, `ValidationError`, as today |
| 401, 403 | `UpstreamError{Kind: UpstreamAuth}` | 502, `ExternalServiceError`, "The source rejected Logchef's credentials" |
| 429 | `UpstreamError{Kind: UpstreamBusy}` | 429, `ExternalServiceError`, "Loki is busy" |
| 5xx, transport failure, malformed body | `UpstreamError{Kind: UpstreamUnavailable}` | 502, `ExternalServiceError` |
| Context deadline | The context error | 408, as today for a cancelled request |

Every query route already sends its errors through `sendQueryError` or
`classifyQueryError`: logs, LogchefQL, histogram, alert test and the stream
writer (`internal/server/histogram_handlers.go:38`,
`internal/server/alert_handlers.go:301`, `internal/server/query_stream.go:136`).
The log context and field value handlers adopt it in unit L3 where they do not
use it yet. VictoriaLogs keeps its current errors. Moving it to
`UpstreamError` is a follow-up and not part of this RFC.

Token scopes do not change. A Loki source needs the same scopes as any source
for the same route.

### Provider interface changes

This RFC owns four changes to the datasource contract and adds no capability
flag. The fourth is `datasource.UpstreamError`, described under "API".

1. **`QueryRequest.Step time.Duration`** (`internal/datasource/query.go:12`).
   Core fills it from the request's `interval`
   (`pkg/models/query.go:113`). The Loki provider sends it as `step` on a
   metric range query. The other providers ignore it. Without it, a dashboard
   panel that writes `[{{__interval}}]` gets Loki's default step, and its
   buckets overlap or leave gaps.
2. **`LogContextRequest.StreamLabels map[string]string`**
   (`internal/datasource/query.go:63`). The Loki provider needs the stream of
   the target row. ClickHouse ignores the field.
3. **`template.LogQL`**, a fourth dialect beside `LogsQL`
   (`internal/template/variables.go:72-81`). `nativeTemplateTarget`
   (`internal/core/query.go:474`) selects it for a Loki source.

`Descriptor` does not change. `LogchefQLNeedsColumns` is `true` for Loki,
because the generator must know which fields are stream labels
(`internal/datasource/service.go:70-72`).

**The LogQL template dialect.** It scans double-quoted strings with backslash
escapes, backtick raw strings and `#` comments, as the LogsQL dialect does. It
formats a string value with `strconv.Quote`. Its macros:

| Macro | LogQL |
|---|---|
| `{{__interval}}` | A LogQL duration such as `5m` or `1d` |
| `{{__interval_s}}` | Seconds as an integer |
| `{{__from}}`, `{{__to}}`, `{{__time_filter}}`, `{{__timezone}}`, `{{__tz_offset}}` | Rejected. LogQL has no time literal, and the request range always applies |

LogQL's own `line_format` templates use the same braces, such as
`| line_format "{{.msg}} {{__line__}}"`. Inside a string token, the LogQL
dialect leaves a `{{name}}` unchanged when `name` is neither a request variable
nor a macro in the table. The other dialects keep failing on an undefined name.

### Capability flags

`Describe()` for Loki returns:

| Capability | Set | Notes |
|---|---|---|
| `schema_inspection` | Yes | Labels and detected fields |
| `histogram` | Yes | `count_over_time` |
| `field_values` | Yes | |
| `source_inspection` | Yes | Details and index statistics. No activity |
| `live_tail` | Yes | Polling |
| `log_context` | Yes | Per stream, for rows of a LogchefQL query |
| `ai_sql_generation` | Yes, from unit L14 | A LogQL prompt target |
| `exports` | No | Non-goal |

`QueryLanguages` is `logchefql`, `logql`. `SavedQueryEditorModes` is `builder`,
`native`. `AlertEditorModes` is `condition`, `native`.

### How Loki serves each provider method

| Method | Loki behavior |
|---|---|
| `Type`, `Describe` | `loki`, and the table above |
| `PrepareSource` | Validates the connection fields, runs `ValidateConnection`, builds the identity key, sets the meta field defaults |
| `ValidateConnection` | Three calls with an 8 second budget: `buildinfo` for the version, `labels` with `since=5m` to prove auth and tenant, and `query_range` with the selector, `limit=1` and `since=5m` to prove that the selector parses. A 401 or 403 reports "credentials or tenant rejected". A 400 on the third call reports Loki's parse message against the `selector` field |
| `UpdateSource` | Merges redacted secrets, rejects a change to an immutable field, validates again |
| `InitializeSource`, `RemoveSource` | Cache or drop the connection, serialized per source ID as in VictoriaLogs |
| `CheckSourceConnectionStatus`, `CachedSourceConnectionStatus` | `GET /loki/api/v1/labels?since=5m` with the source's headers, under `models.SourceHealthTimeout`. `/ready` is not used, because a gateway or a hosted Loki often does not expose it, and it proves neither auth nor tenant. Background checks run as in `internal/victorialogs/health.go:27` |
| `PopulateSourceDetails`, `GetSourceSchema` | `_time`, `_msg`, then stream labels, then detected fields. See "Field discovery" |
| `QueryLogs` | See "Log queries and paging" |
| `Histogram` | See "Histogram" |
| `GetFieldValues`, `GetAllFieldValues` | See "Field discovery" |
| `InspectSource` | Details: base URL, tenant, selector, parser, Loki version. Storage: `streams`, `chunks`, `entries` and `bytes` from `/index/stats` for the selector over 24 hours, with the hint "From the index. Excludes data not yet flushed". Schema: the fields above |
| `EvaluateAlert` | See "Alerts" |
| `CompileLogchefQL` (optional) | See "LogchefQL to LogQL" |
| `TailLogs` (optional) | See "Live tail" |
| `GetLogContext` (optional) | See "Log context" |
| `QueryLogsStream`, `PrepareWindowedQuery`, `StreamFields`, `InspectSourceActivity` (optional) | Not implemented. The service reports `ErrOperationNotSupported` |

Every request carries the source's auth, tenant and custom headers, uses the
dial and response-header timeouts that the VictoriaLogs transport uses
(`internal/victorialogs/provider.go:27-41`), and runs under the caller's
context. Loki has no per-request timeout parameter, so `QueryTimeout` becomes
the context deadline. The provider cancels the HTTP request when it expires.

### Log queries and paging

`QueryLogs` receives native LogQL in `RawQuery`. LogchefQL arrives already
compiled. The provider does not inspect the text to learn whether it is a log
query or a metric query. Loki's grammar accepts forms that a text test gets
wrong, such as the parenthesized log query `({app="api"})`
([`syntax.y` at v3.7.6](https://github.com/grafana/loki/blob/v3.7.6/pkg/logql/syntax/syntax.y#L108-L112)).

The provider sends every query to `query_range` with the log parameters and
the metric parameter together: `direction`, `limit` and `step`. Loki applies
the ones that fit the query.

`step` is always sent, because Loki validates it before it knows the query
kind. Its shared request parser rejects a range that holds more than 11,000
steps, for log queries too
([`ParseRangeQuery` at v3.7.6](https://github.com/grafana/loki/blob/v3.7.6/pkg/loghttp/query.go#L472-L512)).
A log search over 24 hours with a 1 second step would fail that check, though
log evaluation never uses the step. The provider therefore sends
`max(QueryRequest.Step, ceil(range / 11000))`, rounded up to a whole second
and never below 1 second. For a log result the value has no effect. For a
metric result that asked for a smaller step, the result carries the warning
"Step raised to N seconds to stay within Loki's limit of 11,000 points".

The response's `resultType` then decides the path: `streams` is a log result, and
`matrix` is a metric result. Each operation that needs one kind checks the
result type or relies on Loki's parse error, as its section says.

**A log result** comes from `query_range` with `direction=backward`, the request's
`start` and `end` as nanosecond integers, and a `limit`. The provider applies
the limit rule that VictoriaLogs applies (`internal/victorialogs/querying.go:1346`):
the effective limit is the request limit, or the default, capped by the
maximum, and the provider asks for one entry more to prove a truncation.

Loki rejects a `limit` above the tenant's `max_entries_limit_per_query`, 5,000
by default. Logchef's maximum preview limit is 100,000 by default
(`internal/config/config.go:469`). So the provider pages by time:

1. Request `min(remaining, max_entries_per_query)` entries in `[start, end)`.
2. Merge the returned streams by timestamp, newest first. Ties order by the
   stream's sorted label string, then by line.
3. If the page was full and entries remain to fetch, set `end` to the oldest
   returned timestamp plus one nanosecond and request again. Drop entries
   already seen at that boundary timestamp, compared by timestamp, label string
   and line.
4. Stop when a page is short, when the limit is reached, or when the byte
   budget is spent.

If one nanosecond holds more entries than a page, paging cannot advance. The
provider then returns what it has with the truncation `rows` and the warning
"More than N entries share one timestamp. Narrow the query."

The byte budget is `MaxResponseBytes`, counted over raw response bodies. Each
page is read through a reader that stops one byte past the remaining budget.
When a page crosses the budget and earlier pages exist, the provider drops that
page and reports the truncation `bytes`. When the first page crosses the
budget, the provider retries it once with a tenth of the entry limit, and
returns a 400 "response exceeds the size limit" if the smaller page also
crosses.

Loki also bounds the range. A range longer than `max_query_length` (default
`30d1h`) returns a 400, which the user sees with Loki's message. Logchef does
not split the range. Loki's query frontend does.

**A metric result** has no pages. The limit applies to the number of rows
after flattening. A metric query on Loki fails when it returns more than
`max_query_series` series (default 500), and the user sees that message.

`QueryStats` takes `ExecutionTimeMs` from `stats.summary.execTime`, `RowsRead`
from `totalLinesProcessed` and `BytesRead` from `totalBytesProcessed`, summed
over pages.

### LogchefQL to LogQL

`internal/logchefql/logql_generator.go` adds `TranslateToLogQL(query, options)`
beside `TranslateToLogsQL` (`internal/logchefql/logsql_generator.go:109`). The
grammar does not change. The options carry the source's selector, parser and
the set of stream label names.

A compiled query has four parts in a fixed order:

```
{<source selector matchers>, <hoisted matchers>} <line filters> | <parser> | drop __error__, __error_details__ | <label filter> | __error__="" | keep <fields>
```

Only the selector is always present. The generator hoists a condition into the
stream selector when it is a top-level `and` operand, its field is a stream
label, and its operator is `=`, `!=`, `~` or `!~`. A condition on `_msg`
becomes a line filter when it is a top-level `and` operand. Every other
condition goes into one label filter expression that keeps the query's `and`,
`or` and parentheses.

| LogchefQL | LogQL | Notes |
|---|---|---|
| Empty query | `{selector}` | |
| `app="api"`, stream label | `{..., app="api"}` | Hoisted |
| `app!="api"`, stream label | `{..., app!="api"}` | Also matches streams without the label, as in Loki |
| `app~"pay"`, stream label | `{..., app=~"(?i).*pay.*"}` | `~` is a case-insensitive substring, as on ClickHouse. The value is regex-quoted. Label regexes are anchored, so the generator adds `.*` |
| `app!~"pay"`, stream label | `{..., app!~"(?i).*pay.*"}` | |
| `_msg~"timeout"` | `{...} \|~ "(?i)timeout"` | Line filter. Line regexes are not anchored |
| `_msg!~"timeout"` | `{...} !~ "(?i)timeout"` | |
| `_msg="exact line"` | `{...} \|~ "^exact line$"` | Regex-quoted |
| `_msg!="exact line"` | `{...} !~ "^exact line$"` | |
| `_msg~"a" or _msg~"b"` | `{...} \|~ "(?i)(a\|b)"` | Only when every operand of the `or` is a positive match on `_msg` |
| `trace_id="abc"`, not a stream label | `{...} \| trace_id="abc"` | Label filter. No parser when the source has none |
| `status>=500` | `{...} \| status>=500 \| __error__=""` | Numeric comparison on any class. Loki keeps a line whose value fails to convert and marks it with `__error__`, so the generator filters those out |
| `a="x" or b="y"` | `{...} \| a="x" or b="y"` | Stream labels are valid in a label filter, so an `or` across classes works |
| `(a="x" or b="y") and c!="z"` | `{...} \| (a="x" or b="y") and c!="z"` | `c` is hoisted when it is a stream label |
| `k8s.pod.name="p"` | `{...} \| k8s_pod_name="p"` | A dotted path joins with `_`. Loki's `json` parser flattens nested keys this way, and Loki's OTLP ingestion turns dots in attribute names into underscores |
| Any field filter, source parser `json` | `{...} \| json \| drop __error__, __error_details__ \| ...` | The parser stage is added to every compiled query on that source |
| `level="error" \| ts msg` | `{...} \| level="error" \| keep ts, msg, <every stream label>` | The select pipe becomes `keep`. `_time` and `_msg` are always returned. Stream labels are always kept, because they identify the row's stream for log context |
| `x=true`, `x=42` | `x="true"`, `x=42` | A bare identifier is a string. A number stays a number |

What LogchefQL cannot express on Loki. Each case returns
`ErrUnsupportedFeature` with a message that names the construct:

- `>`, `>=`, `<`, `<=` on `_msg`.
- An `or` that mixes `_msg` with another field, or that holds a negated `_msg`
  match. LogQL cannot join a line filter and a label filter with `or`.
- A comparison with `null`. The LogsQL generator rejects it too
  (`internal/logchefql/logsql_generator.go:288-290`).
- A field name that is not a valid Loki label name after dots become
  underscores. This covers quoted path segments with other characters and a
  leading `@`.
- Any aggregation node that RFC 0005 adds to the grammar. The error is the
  typed "unsupported on this backend" error that RFC 0005 defines.
- A query whose final selector matches the empty string. The source selector
  prevents this case, and the generator still checks it.

`CompileLogchefQL` returns `Language: QueryLanguageLogQL`, with `Query` and
`FilterOnly` both set to the LogQL text, as the VictoriaLogs compiler does
(`internal/victorialogs/querying.go:281-299`). Core passes the time range
separately when the compiled language is LogsQL
(`internal/core/query.go:268`). That test becomes "is not ClickHouse SQL".

`frontend/src/utils/logchefql/` parses and validates LogchefQL and does not
generate backend queries, so it is not affected.

### Native LogQL editor mode

The Explorer's native editor accepts LogQL on a Loki source, as it accepts
LogsQL on a VictoriaLogs source.

- The server runs the text as written. It adds no source selector, parser or
  limit pipe. A log query pages as above. A metric query returns rows of
  `time`, `value` and labels.
- Request variables and the macros in "Provider interface changes" apply.
- Saved queries store `query_language: "logql"`, `editor_mode: "native"`. The
  save path does not parse LogQL, for the reason given for LogsQL
  (`internal/core/saved_queries.go:128-134`).
- The editor label is "LogQL". The editor highlights strings, comments,
  durations, pipeline keywords and metric functions.
- The empty-editor hint is the source selector, so a first query runs.
  Switching from LogchefQL to native fills the editor with the compiled LogQL.

### Histogram

The histogram is a visual aid. It shows where the matching entries fall in
time. It never counts an entry outside the requested range, but it can differ
from the search by entries that sit exactly on a bucket boundary. The reason
is below.

`Histogram` counts with metric queries of this form:

```
sum(count_over_time(<log query> [<width>]))
```

**What Loki counts.** Loki evaluates `count_over_time` at time `t` over
`(t - width, t]`. An earlier draft shifted the evaluation by one nanosecond to
match the search's `[start, end)`. That cannot work. With time splitting on,
which is the default, Loki's metric splitter moves the start down and the end
up to multiples of the step before it runs the query
([`splitters.go` at v3.7.6](https://github.com/grafana/loki/blob/v3.7.6/pkg/querier/queryrange/splitters.go#L229-L318)).
The design instead sends only times that are already multiples of the step,
so that this alignment changes nothing.

**Buckets.** Bucket boundaries are multiples of the window since the Unix
epoch, in UTC. Loki aligns to those multiples, so a timezone offset cannot be
applied. A one-day bucket starts at 00:00 UTC. Dashboards show the notice that
they show for a fixed-offset frame, with the offset "UTC".

Each bucket is the intersection of `[b, b + window)` with the request's
`[start, end)`, and it is labelled `b`. The provider computes the buckets
first and then picks the requests:

| Case | Request | Loki counts |
|---|---|---|
| `start` and `end` fall in one bucket | One instant query at `end` with `[end - start]` | `(start, end]` |
| Full buckets, from the first boundary at or after `start` to the last boundary at or before `end` | One `query_range` with `step=window` and `[window]`, from the end of the first full bucket to the end of the last. Every time is a multiple of the window | `(b, b + window]` each |
| Leading partial bucket, when `start` is not on a boundary | One instant query at the first boundary `b1` with `[b1 - start]` | `(start, b1]` |
| Trailing partial bucket, when `end` is not on a boundary | One instant query at `end` with `[end - bn]`, where `bn` is the last boundary | `(bn, end]` |

The first row is the whole histogram when it applies, and the other rows are
then not used. Otherwise the range query is skipped when no full bucket
exists, and each partial query is skipped when its edge is on a boundary. A
range that starts and ends on boundaries is one `query_range` and nothing
else. A partial width is written in milliseconds, which is the precision of
the request's times. An ungrouped histogram is at most three requests.

For the range `[10s, 50s)` with a 60 second window, the first row applies. One
instant query at 50s with `[40s]` counts `(10s, 50s]`. Entries at 5s and 60s
are not counted.

**Where it differs from search.** Every window is open at its start and
closed at its end, and search is the reverse. So an entry exactly at `start`
is not counted, an entry exactly at `end` is counted, and an entry exactly on
an inner boundary is counted in the bucket before it. Entries strictly inside
a bucket are always counted in that bucket. The histogram docs state this.

`<log query>` is `HistogramRequest.Query`. The provider does not test its
kind. A metric query inside `count_over_time` is a LogQL parse error, and the
provider returns Loki's 400 as "The histogram needs a LogQL log query",
followed by Loki's message. The histogram handler already bounds the number
of buckets, and the provider rejects a request above 11,000 buckets.

With `GroupBy`, the provider first runs one instant query at `end` over the
whole range, `topk(10, sum by (<g>) (count_over_time(<q> [<end - start>])))`,
to pick the ten largest groups. It then runs the requests above twice: once as
`sum by (<g>) (count_over_time(<q> | <g>=~"<v1>|<v2>|..." [<width>]))` for
those groups, with regex-quoted values, and once ungrouped for the total. That
is at most seven requests.

`Other` per bucket is the total minus the ten groups, with `IsOther` set, and
the result carries the same notice text that the other providers use when
series are capped. A group whose label is absent has the value `""`. Loki has
no NULL group, so `IsNull` is never set.

### Field discovery

**Schema.** `GetSourceSchema` returns `_time` and `_msg`, then every name from
`/loki/api/v1/labels` with the source selector as `query`, typed `label`, then
the entries of `/loki/api/v1/detected_fields` for the selector over
`schema_lookback_seconds`, with `limit=1000`. A detected field is kept when it
has no parser, or when its `parsers` list has the source's `parser`. A name
that is already a stream label is skipped. The inspection cache stores the
result, so LogchefQL compilation does not call Loki on every query
(`internal/datasource/service.go:398-422`).

**All field values.** `GetAllFieldValues` serves the sidebar's preload. It
returns values for stream labels only, from `/loki/api/v1/label/<name>/values`
with the hoisted selector of `QueryText` as `query`. These are index reads. The
provider reads at most 30 labels with four requests in flight, keeps at most
`Limit` values per label, and leaves `Count` at zero.

**One field's values.** `GetFieldValues` serves a click on a field. It works
for all three classes with one instant query at the range end:

```
topk(<limit>, sum by (<field>) (count_over_time(<log query> [<range>])))
```

The range is the requested range, capped at `sidebar_lookback_seconds`. The
result gives values with counts. `TotalDistinct` is the number of series
returned, and `IsLowCardinality` is true for a stream label.

### Live tail

`TailLogs` polls `query_range` with `direction=forward`. It does not use
Loki's `/loki/api/v1/tail`, for four reasons. That endpoint is a WebSocket, and
`go.mod` has no WebSocket client. It rejects a multi-tenant `X-Scope-OrgID`.
Loki allows ten concurrent tails per tenant by default. A proxy in front of
Loki often does not upgrade connections. The ClickHouse provider already tails
by polling (`internal/datasource/clickhouse_tail.go:59`), and the Loki tailer
copies its separation of a normal poll from a drain
(`internal/datasource/clickhouse_tail.go:120-128`).

The tailer keeps three values: `cursor`, the newest timestamp it has emitted;
`scanFrom`, where the next request starts; and `upper`, the end of the window
being read. An entry's key is its timestamp, sorted label string and line. The
set of emitted keys covers `[cursor - LookbackMargin, cursor]` and is pruned as
the cursor moves.

1. **Normal poll.** On a tick, set `upper = now` and
   `scanFrom = cursor - LookbackMargin`. The margin rescan picks up entries
   that arrived late.
2. **Read a page.** Request `[scanFrom, upper)` with a limit of 1,000. Emit the
   entries whose key is not in the set, oldest first, and advance `cursor`.
3. **Short page.** The window is drained. Wait for the next tick.
4. **Full page.** Enter the drain. Keep `upper` fixed. Set `scanFrom` to the
   newest timestamp on the page, not to `cursor - LookbackMargin`, and read
   the next page at once. `scanFrom` therefore rises on every full page, and
   the overlap rescan cannot return the same page twice.
5. **No progress.** A full page whose entries all share one timestamp cannot
   advance `scanFrom`. More than 1,000 entries then share one nanosecond. The
   tailer sets `scanFrom` to that timestamp plus one nanosecond, logs a
   warning with the source ID, and continues. Entries beyond the first 1,000
   at that nanosecond are skipped.
6. **Bound.** A tick reads at most ten pages. If the window is not drained,
   the next tick continues the drain with the same `upper` and `scanFrom`, and
   starts a normal poll only after a short page.

A burst of 1,500 entries inside the margin is read as one full page and one
short page, and each entry is emitted once. `PollInterval` and
`LookbackMargin` default to the ClickHouse values, 2 seconds and 5 seconds
(`internal/datasource/clickhouse_tail.go:34-37`). Consecutive failed polls
end the tail with the last error at the limit that the ClickHouse tailer uses.

A tail needs a log result. If the first response has the result type `matrix`,
the provider returns the validation error "Live tail needs a LogQL log query".

The Explorer arms live tail only for LogchefQL or native LogsQL today
(`frontend/src/stores/explore.ts:185-192`). That test becomes "LogchefQL, or a
native language other than ClickHouse SQL".

### Log context

Context on Loki means the lines around a row in the same stream. The provider
must know the row's stream, and a row carries that identity only when its
stream labels are intact.

- **LogchefQL rows.** A compiled query never drops or rewrites a stream
  label. Its `keep` stage always lists every stream label. The row therefore
  holds every label of its stream, and the frontend sends them as
  `stream_labels`: the row's value for each schema field of type `label`.
- **Native LogQL rows.** A native pipeline can `keep`, `drop` or
  `label_format` stream labels, and Logchef cannot see that it did. The
  Explorer disables context on rows of a native query, with the reason
  "Context on Loki needs a LogchefQL query, because a native pipeline can
  change stream labels". The server does not trust that rule. The series
  check below guards every request.

The label list in the schema can also be stale, so the provider proves the
identity before it reads.

1. It builds a selector from `stream_labels`, with every label as an exact
   matcher, and calls `/loki/api/v1/series` with it over
   `[target - 1h, target + 1h]`, the range that the context reads. Each
   returned series is the complete label set of one stream.
2. Exactly one returned label set must equal the supplied map: the same names
   and the same values, with none missing and none extra. Anything else is a
   400, "This row does not identify one stream". A count of one is not
   enough. For streams `{app="api", pod="a"}` and `{app="api", pod="b"}`, a row
   that holds only `app` is rejected even when `/series` returns one stream,
   because `{app="api"}` is not that stream's complete label set.
3. The context selector is built from the verified set, not from the request.
   It holds every label of the set as an exact matcher, and `name=""` for
   every other stream label that step 1 returned on any series. A stream that
   has the same labels plus one more therefore does not match.

The provider then runs two queries with that selector: backward over
`[target - 1h, target)` for the lines before, and forward over
`[target, target + 1h]` for the lines after. `ExcludeBoundary` moves the bounds
by one nanosecond. Loki has no offset, so the provider requests
`offset + limit` entries and drops the first `offset`. It rejects a request
where `offset + limit` exceeds `max_entries_per_query`. Parsed fields and
structured metadata are not part of the selector.

### Alerts

`EvaluateAlert` runs an instant query on `/loki/api/v1/query`. It maps each vector sample to a row with `value` first, then the sorted
labels, as the VictoriaLogs provider does
(`internal/victorialogs/querying.go:703-773`).

- The language must be `logql`. `ResolveAlertMetadata` accepts it
  (`pkg/models/alerts.go:76`).
- **Evaluation time.** RFC 0006 adds `AlertQueryRequest.At`, the effective
  time of the run. The provider sends it as the instant query's `time`
  parameter, so a catch-up run or a replay reads the logs of that time and not
  of the present. Until RFC 0006 lands, the field does not exist and the
  provider sends the current time. The Loki unit that lands second adds the
  one line.
- **Lookback.** A LogQL metric query carries its own range, such as `[5m]`,
  and the provider does not rewrite the query. `lookback_seconds` stays
  required and stored for every rule, as RFC 0006 decides, because backtest
  needs it. For a native LogQL rule the range in the query decides what is
  counted. The alert form keeps the lookback input and warns when the query
  holds no range equal to it. The warning is a text match for
  `[<lookback>s]` or its duration form, and it never blocks a save.
- A log query gets a 400 from Loki. The provider reports "A Loki alert needs a
  metric query, such as `sum(count_over_time({app="api"} |= "error" [5m]))`".
- An empty vector returns zero rows. Core already treats zero rows as the value
  0 with a warning (`internal/core/alerts.go:526-527`).
- Condition mode builds the query in the frontend, as it does for LogsQL
  (`frontend/src/composables/useAlertForm.ts:254-258`). It compiles the
  LogchefQL filter to LogQL, then wraps it:

| Aggregate | Generated LogQL |
|---|---|
| count | `sum(count_over_time(<q> [<lookback>s]))` |
| sum, min, max of a field | `sum(sum_over_time(<q> \| unwrap <f> \| __error__="" [<lookback>s]))`, and `min(min_over_time(...))`, `max(max_over_time(...))` |
| avg of a field | `avg_over_time(<q> \| unwrap <f> \| __error__="" [<lookback>s]) by ()` |

### Dashboards

RFC 0003 defines two query modes. Both work on Loki.

| Mode | Language | Path |
|---|---|---|
| `count` | `logchefql`, `logql` | The histogram path above. A LogQL count panel needs a log query |
| `result` | `logql` | `POST /logs/query`. A metric query returns `time`, `value` and label columns. Values are strings, so the frame is untyped, as for LogsQL (`frontend/src/stores/dashboards.ts:535`) |
| `result` | `logchefql` | `POST /logchefql/query`. Log rows |

Every Loki query is time-bound, because the request range always applies. The
limit rule holds as it does for VictoriaLogs.

`pkg/models/dashboards_v2.go:101` adds `logql` to the valid languages. The
enum-origin table adds `logql` under a new feature named `loki_queries`
(`pkg/models/dashboards_v2.go:446`), so a client that predates this RFC gets
RFC 0003's version banner and does not misread the panel.

### Client surfaces

**Web UI.**

- `AddSource.vue` gets a third radio card, "Grafana Loki"
  (`frontend/src/views/sources/AddSource.vue:454-459`). A new
  `LokiSourceForm.vue` reuses the base URL, auth mode and header controls of
  `VictoriaLogsSourceForm.vue`. Its own fields are **Tenant ID**, **Default
  stream selector**, **Parser** (None, JSON, logfmt) and **Access principal**.
  The selector help text says that native LogQL can read any stream the tenant
  holds. The principal help text says "A name for what this credential can
  read. Set it when two sources share a URL, tenant and selector". The
  principal is read-only on edit, like the tenant and the selector. A
  duplicate-identity error from the server names the principal field.
  `sourceFormModels.ts` adds `"loki"` to `DatasourceType`.
- `queryMetadata.ts` adds `"logql"` to `QueryLanguage`. The native language of
  a source becomes the first entry of `query_languages` that is not
  `logchefql`, which removes the `logsql` test
  (`frontend/src/lib/queryMetadata.ts:88`). `QueryEditor.vue` takes its label
  and mode from that value
  (`frontend/src/components/query-editor/QueryEditor.vue:515-518`).
- A new helper `isSchemalessSource` in `queryMetadata.ts` is true for
  VictoriaLogs and Loki. It replaces the literal tests in
  `frontend/src/views/explore/table/defaultColumnVisibility.ts:17` and
  `frontend/src/lib/sourceFields.ts:141`.
- The stream picker and the windowed search toggle stay hidden. Both depend on
  VictoriaLogs-only data.
- New strings go into every locale file. LogQL keywords and examples are not
  translated (`frontend/src/i18n/README.md:26-30`).

**CLI.** `logchef sql` already sends the native text and needs no new flag.
Help text names LogQL beside LogsQL (`cli/crates/logchef-cli/src/cli.rs:13`).
`ui.rs` adds a `LOGQL_KEYWORDS` list for `generated_query_language: "logql"`
(`cli/crates/logchef-cli/src/ui.rs:136`), and `collections.rs` maps `logql`
(`cli/crates/logchef-cli/src/commands/collections.rs:297`).

**MCP.** No new tool. The schema descriptions name Loki and LogQL
(`internal/mcp/sources.go:28`, `internal/mcp/logs.go:20`).

**Ask Chef.** Not affected. Its tools run LogchefQL
(`internal/askchef/tools.go:49`), which compiles for Loki.

**AI query generation.** `internal/ai/generator.go` adds `TargetLogQL` with a
prompt that states the stream selector rule and the three field classes.
`deriveAITarget` maps a Loki source in native mode to it
(`internal/server/ai_sql_handlers.go:68`).

**Provisioning TOML.**

```toml
[[sources]]
name = "Loki Demo"
source_type = "loki"
secret_ref = "LOGCHEF_SOURCE_LOKI_DEMO_TOKEN"

[sources.connection]
base_url = "http://127.0.0.1:3100"
tenant_id = "dev"
selector = '{service_name=~".+"}'
parser = "json"
principal = "payments-readonly"
auth = { mode = "bearer" }
```

`secret_ref` fills `auth.token` in bearer mode and `auth.password` in basic
mode, as it does for VictoriaLogs (`internal/provisioning/validate.go:257-270`).
Validation, defaults and export each add a Loki case
(`internal/provisioning/validate.go:85`, `internal/provisioning/export.go:40`,
`internal/config/provisioning.go:151`).

**Docs.** A new tutorial `tutorials/loki.mdx` and a guide
`guide/logql-queries.md`, modeled on the VictoriaLogs pages. `from-loki.md`
gains a section "Keep Loki and query it from Logchef". The provisioning,
search syntax, alerting, comparison and roadmap pages name the new backend.

**Metrics.** Not affected.

### Dev environment and tests

**Dev stack.** `dev/docker-compose.yml` adds a `loki-local` service beside
`victorialogs-local` (`dev/docker-compose.yml:31-43`): image
`grafana/loki:3.7.6`, port 3100, a named volume, and a config file
`dev/loki.yaml`. The config is single-binary with filesystem storage, schema
v13 with TSDB, and `auth_enabled: true` so that the tenant header is exercised.

**Seed.** `dev/ingest-loki.sh` posts to `/loki/api/v1/push` with
`X-Scope-OrgID: dev`. It writes four streams with the labels `service_name`,
`env` and `level`, JSON lines, and a `trace_id` in structured metadata.
`dev/loki.toml` is a Vector config with a `loki` sink, started by `just
dev-ingest-logs`. `dev/provisioning.toml` adds a "Loki Demo" source linked to
"Dev Team". `just dev-setup` waits for Loki, and `just dev-reset` recreates
its volume.

**Unit tests.** `logql_generator_test.go` has one case per row of the mapping
table and per unsupported construct. Provider tests use `httptest` servers for
paging across a boundary timestamp, the byte budget, stream merging, header
rules and error mapping.

**Integration tests.** `internal/loki/integration_test.go` runs when
`LOGCHEF_TEST_LOKI_URL` is set, as the VictoriaLogs suite does with its
variable (`internal/victorialogs/integration_test.go:27-29`). A
`loki-integration` job starts the pinned image, beside the
`victorialogs-integration` job (`.github/workflows/go-tests.yml:66`).

**Provider conformance tests.** No shared suite exists today. Each provider
tests the same rules by itself, such as the limit rule
(`internal/datasource/limit_rule_test.go:37`) and bucket alignment
(`internal/victorialogs/bucket_alignment_test.go:23`). Unit L15 adds
`internal/datasource/providertest` with `Run(t, Harness)`. A harness gives a
provider, a source, a function that ingests fixture rows, and the native text
of three queries. The suite asserts what every provider must do:

- `QueryLogs` returns rows newest first, honors the limit, and reports the
  truncation `rows` only when more rows exist.
- The histogram's bucket counts sum to the row count of the same query.
- `GetFieldValues` returns the seeded values with their counts.
- `EvaluateAlert` returns one row whose first column is the count.
- `TailLogs` emits a row that is ingested after the tail starts, once.
- A compiled LogchefQL equality filter returns the rows that the native
  equivalent returns.

The suite runs for Loki and VictoriaLogs in their integration jobs. ClickHouse
joins when its integration tests gain an ingest helper, outside this RFC.
`dev/smoke_api.py` gains a Loki block, and `dev/e2e/scenarios.sh` repeats the
live tail scenario on the Loki source.

## Access and security

**Roles.** Only a global admin creates, edits or deletes a source, as today. A
team member with access to a Loki source can run any LogchefQL or LogQL query
on it. Token scopes are unchanged.

**What a source link grants.** Linking a Loki source to a team grants that
team everything the source's credential and tenant can read. The selector
narrows nothing for native LogQL.

**Tenancy.** `tenant_id` is fixed at creation. Users cannot set or change
`X-Scope-OrgID`, because the header is reserved in `headers` and no request
field feeds it. A pipe-separated value queries several tenants and needs
`multi_tenant_queries_enabled` on the Loki querier.

**Auth.** Loki does not authenticate by itself. A deployment puts a gateway
in front of it, and the source's basic or bearer credential is for that
gateway. Grafana Cloud takes the tenant as the basic auth user and an access
token as the password, with no tenant header.

**Restricted sources (RFC 0002).** RFC 0002 says that the log database
enforces a restriction, not Logchef. For Loki, v1 supports and verifies one
boundary: the tenant. A restricted view is a second Logchef source with its
own `tenant_id`, linked to the restricted team. Loki keeps each tenant's data
apart, and Logchef fixes the tenant header per source.

A gateway that enforces label matchers per credential inside one tenant is
not a guarantee in v1. Such a gateway must filter every endpoint that Logchef
calls, including labels, label values, series, detected fields and index
statistics, and Logchef has no fixture that proves a given gateway does. The
`principal` field lets an admin create such sources, and the docs say that
Logchef has not verified their isolation. A verified gateway fixture is
follow-up work.

**Rollout prerequisite.** A team admin can link any source to their team
today. The route allows team admins
(`internal/server/server.go:464`), and `AddTeamSource` checks only that the
source exists (`internal/core/teams.go:355`). A restricted Loki source
isolates nothing while a team admin can link the broader source beside it.
RFC 0002's change 1, "Only global admins link sources to teams", must ship
before any Loki source is presented as restricted. Until it ships, the Loki
docs and the source form say that Loki sources are for teams that may read
the whole tenant, and unit L17 does not start.

Tenant isolation is tested, not assumed. Unit L17 runs a source of one tenant
against a second tenant's data and asserts that none of it is readable by a
native log query, a selector nested in a metric query, label and field
discovery, field values, log context or live tail.

The source `selector` is not a boundary. Native LogQL runs as written and can
name any stream in the tenant. Enforcing the selector would need a LogQL
parser that rewrites every selector in a query, including those inside metric
expressions, and one missed position leaks data. The form states this next to
the field. RFC 0002's `access_note` chip applies to a Loki source as to any
other.

**Secrets.** Passwords, tokens and header values are stored in
`connection_config`, redacted in every response, and never logged. Validation
errors quote at most the first 500 characters of a Loki error body, as the
VictoriaLogs provider bounds its diagnostics.

**Outbound requests.** The provider calls only the admin-supplied base URL
with fixed paths. A user's query travels as a form value and never changes the
host or path. The provider does not follow a redirect to another host.

## Limits and failure modes

| Bound | Value | What the user sees |
|---|---|---|
| Entries per request | `limits.max_entries_per_query`, default 5000 | Nothing. The provider pages |
| Entries per query | The limit rule, up to `max_preview_limit` | The truncation chip `rows` when more exist |
| Bytes per query | `max_response_bytes`, default 64 MiB | The truncation chip `bytes`, or a 400 when one small page exceeds it |
| Requests per query | Limit divided by page size, so 20 at the defaults | Nothing |
| Range | Loki's `max_query_length`, default `30d1h` | A 400 with Loki's message |
| Series per metric query | Loki's `max_query_series`, default 500 | A 400 with Loki's message |
| Time | The request's query timeout as a context deadline. Loki's own `query_timeout` defaults to 1 minute | "Query timed out", or Loki's message |
| Source `max_entries_per_query` above the tenant limit | | A 400 from Loki that names both numbers. The provider adds "Lower the source's entries per request" |
| Histogram groups | Top 10 plus `Other` | The series-cap notice |
| Sidebar preload | 30 labels, four in flight | Remaining labels load on click |
| Field values range | `sidebar_lookback_seconds` | Counts cover the capped range |
| Tail | One poll per 2 seconds per session, 1,000 entries per poll | Rows arrive in batches |
| Loki rate limit | Loki returns 429 when a tenant's queue is full | A 429 "Loki is busy". No retry |

The provider retries nothing except the one smaller first page. A failed page
in the middle of a query fails the query. Partial pages are not returned,
because a gap in the middle of a time-ordered result looks like missing logs.

A backward page boundary is exact only if Loki returns a deterministic set at
the limit. The provider's dedup removes repeats but cannot restore an entry
that Loki omitted at a boundary timestamp. "Unverified claims" lists the test.

**Several replicas.** The provider holds per-process state only: the
connection cache, the health cache and tail cursors. A tail session lives on
the replica that serves its request, as today. Background health checks run on
every replica, one `labels` call per source per interval. Nothing needs
coordination.

## Backend differences

| Feature | ClickHouse | VictoriaLogs | Loki |
|---|---|---|---|
| Native language | ClickHouse SQL | LogsQL | LogQL |
| LogchefQL | Full | Full, no null tests | No null tests, no `or` across `_msg` and fields, no ordering on `_msg` |
| Schema | Typed columns | Sampled, untyped | Labels from the index. Other fields sampled, untyped |
| Mandatory filter | None | None | A stream selector, supplied by the source |
| Histogram | SQL | Hits API | `count_over_time` |
| Field values with counts | Yes | Yes | Yes for one field. Preload has no counts |
| Live tail | Polling | Native stream | Polling |
| Log context | Table-wide | No | Per stream |
| Alerts | SQL | Stats query | Instant metric query. The range is in the query |
| Exports | Yes | No | No |
| Windowed search | No | Yes | No |
| Source activity | Yes | Yes | No |
| Timezone in buckets | Calendar | Fixed offset | UTC |
| Histogram and search agree on boundary entries | Yes | Yes | No. See "Histogram" |

Portable: LogchefQL filters without the constructs above, saved queries in
builder mode, count panels, condition alerts on count. Capability flags gate
exports, log context and live tail. The source type gates windowed search and
the stream picker.

## Seams

**RFC 0005 (LogchefQL aggregations).** Decided across the RFCs: one LogchefQL
aggregation compiles to one backend query, with no multi-query compile and no
capability per aggregation shape. Loki LogchefQL aggregations are deferred.
This RFC requires only one thing from RFC 0005: a typed "unsupported on this
backend" error that the Loki generator returns for every aggregation node.
Loki does not set RFC 0005's aggregation capability, so the UI does not offer
aggregations on a Loki source. Users aggregate with native LogQL.

The table records how the subset would map to LogQL metric queries, for the
RFC that later lifts the deferral. Each row is one query. `<q>` is the
compiled filter, `<r>` the range, `<g>` the group fields:

| Aggregation shape | LogQL | Limit |
|---|---|---|
| Count | `sum(count_over_time(<q> [<r>]))` | |
| Count by fields | `sum by (<g>) (count_over_time(<q> [<r>]))` | 500 series |
| Sum of a numeric field | `sum by (<g>) (sum_over_time(<q> \| unwrap <f> \| __error__="" [<r>]))` | `sum_over_time` takes no `by` itself |
| Avg, min, max of a numeric field | `avg_over_time(<q> \| unwrap <f> \| __error__="" [<r>]) by (<g>)`, and the `min_over_time`, `max_over_time` forms | The direct `by` form is exact. An outer `avg by` would average per-stream averages |
| Percentile | `quantile_over_time(0.95, <q> \| unwrap <f> \| __error__="" [<r>]) by (<g>)` | |
| Distinct count of a field | `count(sum by (<f>) (count_over_time(<q> [<r>])))` | Fails above 500 distinct values |
| Per time bucket | Any form above on `query_range`, with the bucket as both `<r>` and `step` | 11,000 points per series |
| Over the whole range | Any form above on `query` at the range end, with `<r>` equal to the range | `max_query_length` |
| Top N | `topk(<n>, <vector>)` on `query` | Per evaluation time, so only the whole-range form gives a global top N |
| Several aggregates in one query | Not expressible in one LogQL query. Rejected under the one-query rule | |

An aggregated field must be a stream label, structured metadata or a parsed
field of the source's parser. LogQL has no exact distinct count function. A
LogQL range aggregation writes its range inside the query, so a later mapping
needs the time range and bucket width at compile time.

**RFC 0003 (Dashboards v2).** Requires the feature name `loki_queries` for the
new `panels.query.language` value. A `{{__range}}` macro, the dashboard range
as a duration, would let a stat panel run one instant query. This RFC does not
add it. A stat panel reduces the rows of a range query, which RFC 0003's stat
semantics already cover.

**RFC 0006 (Alerting v2).** Decided across the RFCs: handlers receive the
effective time `At`, and alerts evaluate the normal lookback ending at `At`.
Loki honours `At` through the instant query's `time` parameter.
`lookback_seconds` stays required, and a native LogQL rule stores it. This RFC
requires `AlertQueryRequest.At` from RFC 0006 and adds no field of its own.
RFC 0006's `{{__from}}`, `{{__to}}` and `{{__time_filter}}` macros stay
rejected in the LogQL dialect, because LogQL has no time literal.

**RFC 0002 (Restricted sources).** A dependency, not a conflict. RFC 0002's
change 1 is a rollout prerequisite, as "Access and security" states. Its
`access_note` chip applies to a Loki source as to any other.

**RFC 0007, 0008.** A saved view or a report on a Loki source stores
`query_language: "logql"` or `"logchefql"`. A report that needs an export is
blocked on Loki until exports are provider-neutral.

**RFC 0009.** See "Audit events". **RFC 0010.** Not affected.

**Conflicts.** The first draft conflicted with RFC 0005 and RFC 0006. It asked
RFC 0005 for multi-query compilation and per-shape capabilities, and it told
RFC 0006 to drop the lookback field for native rules. Both requests are
withdrawn in favour of the decisions above. No conflict remains.

## Audit events

This RFC adds no event. A Loki source emits the events that RFC 0009 defines
for any source: `source.create`, `source.update`, `source.delete` and
`source.validate`, with the admin as actor and the source as target. The event
payload must carry the redacted connection, never `auth` secrets or header
values.

## Alternatives rejected

**Import the Loki module for its LogQL parser.** A parser would allow selector
injection in native queries and an exact log-or-metric test. The module pulls
in the Prometheus, Kubernetes and cloud SDK trees. Loki's `resultType` and
parse errors give the same answer with no dependency, and the selector is
declared as a default, not a boundary.

**Classify a query by its first character.** A log query usually starts with
`{`. Loki's grammar also accepts a parenthesized log query, so the test calls
`({app="api"})` a metric query and breaks paging, the histogram and tail for
it.

**Enforce the source selector on native LogQL by text rewriting.** A rewrite
without a parser misses selectors inside metric expressions. A filter that
works most of the time is worse than none, because admins would trust it.

**Use `/loki/api/v1/tail`.** It streams with less delay. It needs a WebSocket
dependency, fails for multi-tenant sources, and counts against a low
per-tenant limit. Polling reuses the ClickHouse tail design.

**Detect the parser per field from `detected_fields`.** The result of a query
would depend on a sample of recent lines, and the same saved query could
compile differently an hour later.

**Run the source parser on native queries too.** It changes the meaning of
text the user wrote, and it breaks a query that already has a parser stage.

**Put a hash of the credential in the identity key.** It would separate two
sources by credential with no new field. A token rotation would then change
the identity of an existing source. The `principal` name is stable.

**Use `/index/volume_range` for the histogram.** It reads only the index. It
returns bytes, not entries, and it accepts no line or label filters, so it
cannot count a filtered query.

**Name the columns `timestamp` and `line`.** Those are Loki's words. `_time`
and `_msg` keep LogchefQL filters and Explorer defaults the same across the
two schemaless backends.

**A `dynamic_schema` capability flag.** It would replace two literal
source-type tests in the frontend. One helper function does the same with no
contract change.

## Implementation plan

L1, L2 and L5 start at once. L12 and L13 need only L1. After L3 merges, L6 to
L11 and L14 are independent of each other and run in parallel. The first slice
a user can use is L1, L3 and L4: add a Loki source in the UI and run native
LogQL.

Two dependencies sit outside this RFC. L17 needs RFC 0002's change 1, which
makes source links a global-admin action. The `At` line of L10 needs RFC
0006's `AlertQueryRequest.At`. Nothing else waits on another RFC. RFC 0005's
typed error is needed only when RFC 0005 adds aggregation nodes, and the unit
of RFC 0005 that adds them also adds the Loki rejection.

**L1. Source type, language and store constraint.** Depends on nothing.
Edit `pkg/models/source.go`, `query.go`, `alerts.go`, `dashboards_v2.go` and
`internal/store/storetest/`. Create the `allow_loki_source_type` migration
pair in both stores. Adds `SourceTypeLoki`, `QueryLanguageLogQL`,
`LokiConnectionInfo` with validation and defaults, the identity key with
`principal`, hydration, redaction, alert metadata and the dashboard language.
Also edit `internal/store/sqlite/upgrade_test.go`.
Check: `just test-short` passes. The store conformance test creates and reads
a `loki` source on SQLite and Postgres. The upgrade test seeds child rows of a
source and finds them all after the rebuild. A model test asserts that two
connections that differ only in `principal` have different identity keys, that
no secret appears in a key, and that redaction blanks the token and header
values.

**L2. Dev Loki service and seed.** Depends on nothing.
Create `dev/loki.yaml`, `dev/ingest-loki.sh`, `dev/loki.toml`. Edit both
compose files, `Justfile` and `dev/README.md`.
Check: after `just dev-docker` and `dev/ingest-loki.sh`,
`curl -s -H 'X-Scope-OrgID: dev' 'http://localhost:3100/loki/api/v1/labels'`
lists `service_name`, `env` and `level`. Without the header it returns 401.

**L3. Provider core and native LogQL.** Depends on L1. Its integration test
needs L2.
Create `internal/loki/provider.go`, `health.go`, `querying.go`, `paging.go`,
their tests and `integration_test.go`. Edit `internal/app/app.go` to register
the provider (`internal/app/app.go:121`), `internal/template/` for the LogQL
dialect, `internal/core/query.go` for `nativeTemplateTarget` and the
time-range test, `internal/datasource/query.go` for `Step`,
`internal/datasource/validation.go` for `UpstreamError`,
`internal/server/query_errors.go` for its mappings, and
`.github/workflows/go-tests.yml`. Covers lifecycle, validation, health,
headers, `QueryLogs` with paging and the byte budget, metric results,
`GetSourceSchema` and `InspectSource`. Methods that later units own return a
validation error, and `Describe()` lists only the capabilities that work.
Check: `go test ./internal/loki/ -run Integration` ingests 12,000 entries with
300 on one timestamp, queries with a limit of 12,000 and a page size of 5,000,
and gets 12,000 distinct rows newest first. An `httptest` case proves the
`bytes` truncation. A template test proves that `line_format "{{.msg}}"`
passes through unchanged. An integration case runs a log query over 24 hours
with `Step` set to 1 second and gets rows, not Loki's point-limit error. A
unit test asserts the step sent for ranges of 1 hour, 24 hours and 30 days.
An integration case runs `({service_name=~".+"})`
and a query with a leading `#` comment and gets paged log rows, and runs a
metric query and gets `time` and `value` rows. A server test with a fake
provider asserts the public responses for each `UpstreamError` kind on the
logs, histogram and context routes: 429 for busy, 502 for unavailable and for
rejected credentials, 400 for a Loki parse error.

**L4. Web UI source form and LogQL editor mode.** Depends on L1. Useful with
L3.
Create `LokiSourceForm.vue`. Edit `AddSource.vue`, `sourceFormModels.ts`,
`queryMetadata.ts`, `sourceFields.ts`, `defaultColumnVisibility.ts`,
`QueryEditor.vue`, `utils/format.ts`, `utils/cliCommand.ts`,
`utils/template.ts`, `views/admin/QueryActivity.vue`, the language unions in
`api/` and `stores/`, and all locale files.
Check: `bun run typecheck` and `bun run test` pass with new cases in
`AddSource.test.ts`, one of which creates two Loki sources that differ only in
the principal and sends `principal` in both payloads. In the app, an admin
adds the dev Loki source, the native
tab reads "LogQL", and `{service_name="payments-api"}` returns rows with
`_time`, `_msg` and label columns.

**L5. LogchefQL to LogQL generator.** Depends on nothing.
Create `internal/logchefql/logql_generator.go` and its test.
Check: a table test with one case per mapping row and per unsupported
construct. Expected strings come from this RFC's table.

**L6. LogchefQL on Loki sources.** Depends on L3 and L5.
Edit `internal/loki/querying.go` for `CompileLogchefQL` and set
`LogchefQLNeedsColumns`.
Check: an integration test runs `level="error" and _msg~"gateway"` and the
native equivalent and gets the same rows. `logchef explain` on the dev source
prints LogQL.

**L7. Histogram and dashboard panels.** Depends on L3.
Edit `internal/loki/querying.go`, `frontend/src/stores/dashboards.ts`,
`utils/panelExplore.ts` and `PanelBuilderDrawer.vue`.
Check: the integration Loki runs with its query frontend and
`split_queries_by_interval: 1h`, so every request passes the splitter. A test
seeds entries one millisecond before `start`, exactly at `start`, strictly
inside each bucket, exactly on an inner boundary, exactly at `end` and one
millisecond after `end`. It asserts each bucket's count against the rule in
"Where it differs from search": nothing outside the range is counted, the
entry at `start` is not, the entry at `end` is, and the boundary entry is in
the earlier bucket. With the boundary entries removed, the buckets sum to the
row count of the search. The single-bucket case is included: `[10s, 50s)` with
a 60 second window and entries at 5, 30 and 60 seconds counts one. A unit
test of the request planner covers a range inside one bucket, an aligned
range, and each partial edge alone. A second test asserts that a group-by
over 12 values returns 10 series and `Other`. In the app,
a count panel and a `result` panel with
`sum by (level) (count_over_time({service_name=~".+"}[{{__interval}}]))`
render on the dev source.

**L8. Field values.** Depends on L3.
Edit `internal/loki/querying.go`.
Check: an integration test asserts values and counts for a stream label, a
structured metadata key and a parsed field. The sidebar lists values for
`level`.

**L9. Live tail.** Depends on L3.
Create `internal/loki/tail.go` and its test. Edit
`frontend/src/stores/explore.ts`, `frontend/src/api/explore.ts` and
`dev/e2e/scenarios.sh`.
Check: a test ingests a row after the tail starts and receives it once, also
when its timestamp is 3 seconds older than the cursor. A test with a fake Loki
serves 1,500 entries inside the margin and asserts 1,500 emitted once and no
repeated request. A test with 1,200 entries on one timestamp asserts that the
tail advances, logs the warning and emits 1,000. A test with 25,000 entries
asserts at most ten requests per tick. The e2e live tail
scenario passes on the Loki source.

**L10. Alerts.** Depends on L3. The condition builder also needs L6.
Edit `internal/loki/querying.go` for `EvaluateAlert`, and
`frontend/src/composables/useAlertForm.ts`.
Check: an integration test evaluates a count over seeded rows and gets the
seeded number, and gets the named error for a log query. Once RFC 0006's `At`
exists, a test seeds 3 rows one hour ago and 7 rows now, and asserts 3 at
`At = now - 1h` and 7 at `At = now`. In the app, a
condition alert on `level="error"` tests successfully.

**L11. Log context.** Depends on L3.
Edit `internal/datasource/query.go`, `pkg/models/query.go`, the context
handler in `internal/server/`, `internal/loki/querying.go` and the context
request in `frontend/src/api/explore.ts`.
Also edit the Explorer row menu to disable context on native rows.
Check: an integration test seeds two interleaved streams and asserts that
context returns only the target's stream, in order, with the offset applied.
A LogchefQL query with a select pipe still yields a row that passes the
series check. A request whose `stream_labels` lack one label of the stream
gets the 400 "This row does not identify one stream", also when only one
stream matches the partial selector. So does a request with an extra label,
and one with a value that a `label_format` rewrote. With a second stream that
has the same labels plus one more, context returns no entry of that stream. A frontend test asserts that the row menu disables
context in native mode.

**L12. Provisioning.** Depends on L1.
Edit `internal/config/provisioning.go`, `internal/provisioning/validate.go`,
`export.go`, their tests and `dev/provisioning.toml`.
Check: tests cover a valid Loki source, a missing selector and a `secret_ref`
in bearer mode. Two provisioned sources that differ only in `principal` both
load. Export then import round-trips a source and keeps its `principal`.

**L13. CLI and MCP.** Depends on L1.
Edit `cli/crates/logchef-cli/src/cli.rs`, `ui.rs`, `commands/collections.rs`,
`commands/explain.rs`, `commands/open.rs`, the bundled skill text and the
schema descriptions in `internal/mcp/`.
Check: `just check-cli` passes with a highlight test for a LogQL query.
`logchef sql '{service_name="payments-api"} |= "error"'` returns rows from
the dev source.

**L14. AI query generation.** Depends on L3.
Edit `internal/ai/generator.go`, `internal/server/ai_sql_handlers.go` and the
Loki `Describe()`.
Check: a test asserts the LogQL prompt, and that
`deriveAITarget(loki, "native")` returns `TargetLogQL`.

**L15. Conformance suite and smoke tests.** Depends on L6 to L10.
Create `internal/datasource/providertest/`. Edit the Loki and VictoriaLogs
integration tests to call it, and `dev/smoke_api.py`.
Check: both integration jobs run the suite and pass. `dev/smoke_api.py`
passes against the dev stack.

**L17. Tenant-isolation verification and guide.** Depends on L6 to L11, and
on RFC 0002's change 1 being merged.
Create `internal/loki/isolation_integration_test.go` and the guide
`docs/src/content/docs/guide/loki-restricted-sources.md`. Edit `dev/loki.yaml`
and `dev/ingest-loki.sh` to seed a second tenant. The guide covers one tenant
per restricted source. It states that a label restriction by a gateway inside
one tenant is not verified by Logchef.
Check: with one source per tenant, the test asserts that the first source
returns no entry of the second tenant through a native log query, a selector
nested in `count_over_time`, labels, detected fields, field values, log
context and tail. A server test asserts that a team admin gets a 403 when
linking a source.

**L16. Docs.** Depends on L3 to L13.
Create `tutorials/loki.mdx` and `guide/logql-queries.md` under
`docs/src/content/docs/`. Edit `from-loki.md`, the provisioning, search
syntax, alerting, comparison and roadmap pages, `CHANGELOG.md` and
`CLAUDE.md`.
Check: the docs site builds. Every LogQL example in the new pages runs on the
dev source.

## Open questions for Karan

1. **Should the parser default to none or JSON?** Default: none. A wrong
   parser adds an error label to every line, and structured metadata already
   gives columns on OTLP pipelines.
2. **Should the selector be required, or default to `{service_name=~".+"}`?**
   Default: required, with that value suggested in the form. A hidden default
   fails on a Loki that does not add `service_name`.
3. **Is polling acceptable for live tail?** Default: yes. The delay is up to 2
   seconds. A WebSocket client can replace the poller later with no interface
   change.
4. **Should exports for Loki block this RFC?** Default: no. Ship without them
   and write a provider-neutral export RFC that covers VictoriaLogs too.
5. **Should the docs keep presenting Logchef as a move away from Loki?**
   Default: keep the migration guide and add the "keep Loki" section.
6. **Should log context work on native LogQL rows?** Default: no. It works for
   LogchefQL rows only, because a native pipeline can change the labels that
   identify a stream.
7. **Should Loki sources ship before RFC 0002's source-link hardening?**
   Default: yes, for teams that may read the whole tenant. The restricted
   guide and unit L17 wait for the hardening.

## Unverified claims

None of these were run. No Loki instance was started for this RFC.

| Claim | Experiment |
|---|---|
| Loki accepts `direction`, `limit` and `step` together on `query_range` for both query kinds and ignores the ones that do not apply | L3's integration case sends all three with a log query, a parenthesized log query and a metric query |
| A `query_range` whose start, end and step are multiples of the window passes Loki's splitter unchanged, and an instant query is not realigned | L7's boundary test, which runs through the query frontend with splitting on |
| `/loki/api/v1/series` returns the complete label set of each stream, with no structured metadata | L11's test compares it with the labels that were pushed |
| `count_over_time` over a parenthesized or commented log query parses, and over a metric query returns a 400 | Send each form to `/loki/api/v1/query` |
| Loki rejects a `limit` above `max_entries_limit_per_query` with a 400 that names both numbers | Query with `limit=5001` on a default config. Record the status and body |
| A backward page that ends inside a timestamp returns a set that the dedup completes on the next page | Ingest 300 entries with one timestamp across three streams. Page with `limit=100` using the `end = oldest + 1ns` rule. Count distinct rows |
| A label filter whose value fails numeric conversion keeps the line and sets `__error__` | Run `{...} \| status >= 500` over lines where `status` is `"abc"`, with and without `\| __error__=""` |
| `detected_fields` returns structured metadata with an empty `parsers` list, and parsed fields with the parser name | Call it on the dev seed, which has both |
| The `stream` object of a log result holds structured metadata and parsed labels beside stream labels, with no response flag | Query the dev seed with and without `\| json` and inspect `stream` |
| `avg_over_time(...) by ()` returns one series | Run it on the dev seed |
| Cancelling the HTTP request cancels the query inside Loki | Start a slow query, cancel, and read the querier's log |
| Loki 4xx error bodies are plain text | Send an invalid query and read the `Content-Type` |
| `label/<name>/values` with a `query` selector narrows the values | Call it with and without `query` on the dev seed |
| `{service_name=~".+"}` matches every stream on a default 3.x install | Push a stream without `service_name` and query it |
| A count over a selector that matches nothing returns an empty vector, not 0 | Evaluate it on `/loki/api/v1/query` |
| Loki's OTLP ingestion turns dots in attribute names into underscores | Push one OTLP log with `k8s.pod.name` and read the label name |
| The SQLite rebuild of `sources` keeps every child row | L1's upgrade test, which seeds child rows and asserts that they survive |
