# RFC 0010: ClickHouse schema advisor

Status: Draft
Date: 2026-11-15
Authors: Karan Sharma

## Summary

Logchef sees every query that users run against a ClickHouse source, and it can
read the metadata of the table behind that source. The schema advisor joins the
two. For each ClickHouse source it produces a report of findings, such as "412
of 1,900 queries in 30 days filter on `request_id` with `=`, and no index covers
it". Each finding shows its evidence, the expected gain and the cost. A finding
that an in-place `ALTER` can apply also shows the DDL to copy.

Logchef never runs the DDL. A global admin reads the report on the source
inspection page or through the CLI, and applies the statements with a
ClickHouse user that may alter the table. The advisor adds one write
path: a daily rollup of the filter shape of each query (columns and match
kinds, never values). Analysis runs on demand, reads system tables plus one
budgeted sample, and stores the latest report per source.

## Problem

Logchef already holds both halves of the answer and does not join them.

- Query history keeps query text, but only the newest 200 rows per user across
  all sources (`pkg/models/query_history.go:8`), with no index on `source_id`
  (`internal/store/sqlite/migrations/000031_add_query_history.up.sql:8`).
- The daily rollup counts queries per user, team, source and language, not
  per column
  (`internal/store/sqlite/migrations/000032_add_query_stats_daily.up.sql:9`).
  The admin usage endpoint reports top sources, top users and volume
  (`internal/server/admin_query_stats.go:41`).
- Source inspection reads `system.tables`, `system.columns` and `system.parts`
  (`internal/clickhouse/schema.go:133`, `internal/clickhouse/schema.go:172`,
  `internal/clickhouse/stats.go:64`) and returns fields, sort keys, the create
  statement and the TTL (`internal/datasource/inspection.go:29`). It reads no
  skip index, column size, codec or partition key, and the page shows the
  schema as a table (`frontend/src/views/sources/SourceStats.vue:169`).
- The Explorer tells a user to filter on a sort key column
  (`frontend/src/views/explore/sortKeyHint.ts:24`). Nothing tells the table
  owner that the sort key does not match the filters that users write.

The parts exist. LogchefQL extracts filter conditions for the field sidebar
(`internal/logchefql/logchefql.go:234`), and raw SQL goes through a ClickHouse
parser (`internal/clickhouse/query_builder.go:128`). Nothing joins them.

## Goals and non-goals

Goals:

1. Record which columns and paths each query filters on, without values.
2. Produce findings from a fixed rule set, each with evidence, gain and cost.
3. Work with a read-only ClickHouse user, and report each unreadable input.
4. Put the advice on the table that owns the data when the source is a view
   or a `Distributed` table.
5. Keep the work on ClickHouse budgeted and visible.

Non-goals:

- **Running DDL.** The connection's permissions are the security boundary
  (`internal/clickhouse/query_builder.go:271`), and RFC 0002 recommends a
  read-only user. Logchef prints statements. It never sends them.
- **`set` and `minmax` skip indexes.** Their gain depends on how values cluster
  inside parts, which metadata and a sample cannot show. `ngrambf_v1` and
  `tokenbf_v1` are out too, because the 26.2 docs mark both as deprecated.
- **`system.parts_columns`.** `system.columns` already gives column sizes.
- **Projections, and a `text` index with the `keyValuePairs` tokenizer.**
  Candidates for a later rule, each with its own build gate.
- **Unrecorded queries.** Alerts, dashboard panels, exports and MCP queries
  do not call `recordQueryHistory`. The report says so.
- **VictoriaLogs and Loki**, which have no user-defined schema to tune,
  **rewriting user queries** to use a new column, and merging usage across
  several sources that read one table.
- **Retention DDL and rebuild steps.** Logchef's usage record is incomplete,
  so R8 generates no `TTL` statement. Sort key and partition findings state
  the proposed key and no migration recipe, because a safe cutover depends on
  ingestion, replication and dependent views that Logchef cannot see.
- **Key advice for specialized engines.** In `ReplacingMergeTree` and the
  collapsing, summing and aggregating engines the sorting key defines which
  rows merge. R6 and R7 cover `MergeTree` and `ReplicatedMergeTree` only.

## Design

### Data shape

Three stored objects. Each migration is named by purpose and takes the next
free number at build time in both metadata stores.

#### Filter shape

A filter shape is the set of filter items of one query. It holds no values.

```go
// pkg/models/schema_advisor.go
type MatchKind string

// One constant per kind: eq, neq, in, range, null, prefix, like, like_token,
// ilike, regex, token, position, other. The tables under "Filter extraction" define each.
const MatchEq MatchKind = "eq"

type FilterItem struct {
	Column string    `json:"column"`
	Path   []string  `json:"path,omitempty"`   // ordered segments, never joined
	Access string    `json:"access,omitempty"` // how the path is read, below
	Kind   MatchKind `json:"kind"`
}

type FilterShape []FilterItem // sorted by column, path, access, kind; no duplicates
```

`Access` is empty for a plain column. Otherwise it is `map` (`m['k']`),
`json_string` (`JSONExtractString(col, 'a', 'b')`), `json_other` (any other
`JSONExtract*` function), `subcolumn` (`col.a.b` in SQL) or `nested` (a
LogchefQL dotted field, resolved against the column type at analysis time).

Bounds on one shape: at most 16 items, `Column` at most 128 bytes, `Path` at
most 8 segments and 256 bytes. A query over a bound is recorded as unanalyzed.

#### `query_shape_stats_daily` (migration "add query shape stats")

| Column | SQLite | Postgres | Meaning |
|---|---|---|---|
| `bucket_date` | TEXT | TEXT | `YYYY-MM-DD` in UTC, as in `query_stats_daily` |
| `source_id` | INTEGER | BIGINT | no foreign key, as in `query_stats_daily` |
| `query_language` | TEXT | TEXT | `logchefql` or `clickhouse-sql` |
| `shape_hash` | TEXT | TEXT | first 16 hex characters of SHA-256 of the shape JSON, or `unanalyzed`, or `overflow` |
| `shape` | TEXT | TEXT | the `FilterShape` as JSON. `[]` for a query with no filter and for the two special hashes |
| `query_count` | INTEGER | BIGINT | |
| `total_duration_ms` | INTEGER | BIGINT | |
| `total_rows` | INTEGER | BIGINT | sum of returned rows |
| `max_lookback_s` | INTEGER NULL | BIGINT NULL | largest `now - start_time` of any query in the row |

Primary key: `(bucket_date, source_id, query_language, shape_hash)`. One more
index on `(source_id, bucket_date)`.

Both `queries.sql` files gain three queries. `IncrementQueryShapeStats` is an
upsert that adds to the three counters and keeps the larger `max_lookback_s`.
`ListQueryShapeStats(source_id, since)` returns at most 5,000 rows by
`query_count` descending. `PruneQueryShapeStats(before)` deletes old rows.
`QueryShapeCoverage(source_id, since)` aggregates over every row, not the top
5,000. It returns the number of distinct days, the total query count, the
counts under `unanalyzed` and `overflow`, the count with a NULL lookback, and
the largest `max_lookback_s`.

All three migrations in this RFC create new tables. None rebuilds a SQLite
table, so the rebuild rule of the cross-RFC seam decisions does not apply.

#### `schema_advisor_reports` (migration "add schema advisor reports")

One row per source. A refresh replaces the row.

Columns: `source_id` (INTEGER, primary key,
`REFERENCES sources(id) ON DELETE CASCADE`), `generated_at` (timestamp),
`generated_by` (INTEGER NULL, a user id, NULL for a scheduled run),
`window_days` (INTEGER) and `report` (TEXT, the JSON of `SchemaAdvisorReport`
below, at most 256 KiB).

```go
type AdvisorInputState string // "ok", "denied", "limited", "missing", "error", "skipped"

type AdvisorInput struct { // Name is "system.columns", "sample", ...
	Name      string            `json:"name"`
	State     AdvisorInputState `json:"state"`
	Detail    string            `json:"detail,omitempty"`
	ReadRows  uint64            `json:"read_rows"`  // work ClickHouse reported
	ReadBytes uint64            `json:"read_bytes"`
}

type AdvisorTarget struct { // Kind is "table", "distributed_local" or "view_base"
	Kind, Database, Table, Cluster, Engine string // snake_case JSON keys
}

type AdvisorEvidence struct { // "Queries with = or IN", "412 of 1,900", "logchef"
	Label, Value, Source string // snake_case JSON keys
}

type SchemaFinding struct {
	ID       string            `json:"id"`     // 16 hex chars, hash of rule, target and subject
	Rule     string            `json:"rule"`   // "skip_index_bloom"
	Impact   string            `json:"impact"` // "high", "medium", "low", "info"
	Title    string            `json:"title"`
	Column   string            `json:"column,omitempty"`
	Path     []string          `json:"path,omitempty"`
	Evidence []AdvisorEvidence `json:"evidence"`
	DDL      []string          `json:"ddl"`    // statements in the order to run
	Rebuild  bool              `json:"rebuild"` // no in-place ALTER exists, DDL is empty
	Candidate bool             `json:"candidate"` // evidence is a sample, validate first
	Gain     string            `json:"gain"`
	Cost     string            `json:"cost"`
	Verify   string            `json:"verify,omitempty"` // a query to confirm the gain
}

type SchemaAdvisorReport struct {
	Status            string          `json:"status"` // "ok", "partial", "unavailable"
	Reason            string          `json:"reason,omitempty"`
	ClickHouseVersion string          `json:"clickhouse_version"`
	Target            *AdvisorTarget  `json:"target,omitempty"`
	Inputs            []AdvisorInput  `json:"inputs"`
	QueriesAnalyzed   int64           `json:"queries_analyzed"`
	QueriesUnanalyzed int64           `json:"queries_unanalyzed"`
	Findings          []SchemaFinding `json:"findings"` // at most 50
	Notes             []string        `json:"notes,omitempty"`
}
```

The server writes `Title`, `Gain`, `Cost` and evidence labels in English, as
inspection labels already are (`internal/datasource/clickhouse_provider.go:912`).
The CLI needs the same text. A finding id is stable across refreshes.
It is the hash of `rule | target database | target table | column | path | access`.

#### `schema_advisor_dismissals` (migration "add schema advisor dismissals")

Columns: `source_id` (INTEGER, `REFERENCES sources(id) ON DELETE CASCADE`),
`finding_id` (TEXT), `reason` (TEXT, at most 500 bytes), `dismissed_by`
(INTEGER NULL) and `dismissed_at` (timestamp).
Primary key: `(source_id, finding_id)`. A dismissal outlives a refresh. The
API joins dismissals onto the report when it reads it.

### Filter extraction

A new package `internal/advisor` holds extraction and rules. Both are pure
functions with no I/O.
`advisor.ShapeOf(query, language, rel advisor.Relation) (models.FilterShape, bool)`
returns the shape and whether the query was analyzable. `Relation` is the
source's database and table name.

**LogchefQL.** Parse with `logchefql.ParseLogchefQL` and `ConvertToAST`, then
walk the tree the way `extractConditionsFromAST` does
(`internal/logchefql/logchefql.go:234`). That function is unexported and
flattens the path into the field name, so this RFC adds an exported
`logchefql.FilterItems(ast)` that keeps `NestedField.Base` and
`NestedField.Path` apart (`internal/logchefql/types.go:60`). A nested field
gets access `nested`. The analysis resolves it the way the generator does
(`internal/logchefql/sql_generator.go:262`). On a `Map` the segments join with
`.` into one key (`internal/logchefql/sql_generator.go:325`). On a `String`
they become the arguments of `JSONExtractString`.

| LogchefQL operator | Kind |
|---|---|
| `=` with value `NULL`, `!=` with value `NULL` | `null` |
| `=` | `eq` |
| `!=` | `neq` |
| `~`, `!~` | `position` |
| `>`, `<`, `>=`, `<=` | `range` |

`~` maps to `position` because the generator compiles it to
`positionCaseInsensitive(col, 'x') > 0`
(`internal/logchefql/sql_generator.go:436`).

**Raw SQL.** Parse with the parser that the query builder uses
(`internal/clickhouse/query_builder.go:128`). The query is analyzable when it
is one `SELECT` with one table in `FROM`, no `JOIN`, no set operation, and no
subquery in `FROM`, and when that table is the source's table. The table
matches when it is the bare table name or `database.table`, compared as
`identIs` compares identifiers (`internal/clickhouse/query_builder.go:240`).
Raw SQL runs in `ExtendedMode`, which accepts a `SELECT` on any table
(`internal/clickhouse/query_builder.go:269`), so a query on another table is
recorded as unanalyzed. The extractor reads `WHERE` and `PREWHERE` of that
`SELECT` only.

| SQL form | Column and path | Kind |
|---|---|---|
| `col = lit`, `col == lit` | `col` | `eq` |
| `col != lit`, `col <> lit`, `col NOT IN (...)` | `col` | `neq` |
| `col IN (...)` | `col` | `in` |
| `<`, `>`, `<=`, `>=`, `BETWEEN` | `col` | `range` |
| `IS NULL`, `IS NOT NULL`, `isNull`, `isNotNull` | `col` | `null` |
| `LIKE 'x%'`, `startsWith(col, lit)` | `col` | `prefix` |
| `LIKE` whose pattern holds a complete token (below) | `col` | `like_token` |
| other `LIKE` | `col` | `like` |
| `ILIKE` | `col` | `ilike` |
| `match(col, lit)`, `REGEXP` | `col` | `regex` |
| `hasToken`, `hasAllTokens`, `hasAnyTokens` | `col` | `token` |
| `position*`, `multiSearch*` | `col` | `position` |
| `m['k']` on the column side | `m`, path `[k]`, access `map` | by operator |
| `JSONExtractString(col, 'a', 'b')` on the column side | `col`, path `[a, b]`, access `json_string` | by operator |
| any other `JSONExtract*` function | `col`, its path, access `json_other` | by operator |
| `col.a.b` | `col`, path `[a, b]`, access `subcolumn` | by operator |
| any other function around the column, such as `lower(col)` | `col` | `other` |

A `LIKE` pattern holds a complete token when it has a run of at least 3 ASCII
letters or digits whose two sides are each a literal character that is not a
letter or digit, or a pattern edge without a wildcard. `'% timeout %'` and
`'conn refused%'` qualify. `'%timeout%'` does not. The extractor stores only
the kind. A `match` is always `regex`, because it parses no regular expression.

Rules that apply to both languages:

- The extractor walks `AND`, `OR`, `NOT` and parentheses.
- An item under `NOT` becomes `neq` when it was `eq` or `in`, and `other`
  otherwise. No skip index helps a negated match.
- When the branches of one `OR` name more than one column, every item under
  that `OR` becomes `other`. An index on one branch alone does not help.
- A comparison of two columns yields no item. The extractor reads no
  literal's value, except the wildcard position of a `LIKE` pattern.

### Recording

`writeQueryHistory` already runs in the background worker after a query
succeeds (`internal/server/query_history.go:90`). It gains one step: compute
the shape and call `IncrementQueryShapeStats`. The step also runs in demo
mode, where history rows are skipped (`internal/server/query_history.go:94`),
because a shape holds no user data.

- `models.QueryHistory` gains three fields that are not persisted to
  `query_history`: `LookbackSeconds *int64`, `SourceDatabase` and
  `SourceTable`. The four call sites hold the source and set them. The
  lookback comes from the request's start time when the request has one
  (`internal/server/logs_handlers.go:358`,
  `internal/server/query_stream.go:281`,
  `internal/server/query_stream.go:306`,
  `internal/server/windowed_handlers.go:103`).
- The recorder skips `logsql`.
- The recorder keeps a per-process set of the shape hashes seen for each
  source on the current UTC date, capped at 500. A new hash past the cap is
  recorded under `overflow`. With N replicas the bound is 500 × N rows per
  source per day. When the UTC date changes, the recorder prunes once.
- A failed write is logged and counted, as history writes are today. It never
  fails the query. Only successful queries on the preview paths are recorded.

### Table metadata

A new file `internal/clickhouse/advisor_metadata.go` adds
`(*Client).AdvisorMetadata(ctx, database, table, tsColumn, sampleColumns)`.
It returns the facts below plus one `AdvisorInput` per query.

`queryMetadata` is not used. It runs every statement with the 60 s default
timeout and takes no settings (`internal/clickhouse/schema.go:253`). A new
`(*Client).queryBudgeted(ctx, budget, query, args...)` wraps
`queryOwnWithSettings` (`internal/clickhouse/client.go:243`) and sends the
budget as server-side settings: `max_execution_time`, `max_rows_to_read`,
`max_bytes_to_read`, `max_memory_usage` and `max_threads = 2`.
`ownQuerySettings` already forces the overflow modes to `throw`
(`internal/clickhouse/result_limits.go:84`).

- **Timeout.** clickhouse-go replaces `max_execution_time` with the context
  deadline plus five seconds, so the 30 s run context would override a 5 s
  budget. `queryBudgeted` wraps the context with `withQueryTimeout`
  (`internal/clickhouse/result_limits.go:125`), which hides the deadline from
  the driver and still cancels the query after the budget plus the grace.
- **Leaf settings.** For a `Distributed` source it sends
  `max_rows_to_read_leaf`, `max_bytes_to_read_leaf` and
  `max_execution_time_leaf`, each set to the budget. It never inherits a
  larger `max_execution_time_leaf` from the source's settings, because a
  positive leaf timeout replaces `max_execution_time` on the remote query.
- **Approximate bounds.** ClickHouse checks read limits per processed chunk,
  so a query can overshoot on each thread and each server. Leaf limits are
  unreliable with `prefer_localhost_replica = 1`, the default. This RFC
  states no hard upper bound for a `Distributed` read. Each input reports the
  rows and bytes that ClickHouse counted, through the driver's progress
  callback as result queries do (`internal/clickhouse/client.go:226`). When
  ClickHouse stops a query on a budget, the input is `limited` and no rule
  may use its partial answer.

| Input | Query | Budget (time, rows read, bytes read, memory) | If it fails |
|---|---|---|---|
| `system.tables` | engine, `engine_full`, `sorting_key`, `primary_key`, `partition_key`, `create_table_query`, `total_rows`, `total_bytes` for one table | 5 s, 1M, 256 MiB, 256 MiB | report is `unavailable` |
| `system.columns` | name, type, `default_kind`, `default_expression`, `compression_codec`, `is_in_sorting_key`, `is_in_partition_key`, `is_in_primary_key`, `data_compressed_bytes`, `data_uncompressed_bytes`, at most 500 columns | same | report is `unavailable` |
| `system.data_skipping_indices` | name, `type_full`, `expr`, `granularity`, sizes, at most 200 rows | same | index rules do not fire |
| `system.parts` | per partition: active part count, rows, bytes, `min(min_time)`, `max(max_time)`, where `active`, at most 2,000 partitions | 5 s, 5M, 1 GiB, 512 MiB | partition rule B and R8 do not fire |
| `system.query_log` | `event_date >= today() - 7`, `type = 'QueryFinish'`, `query_kind = 'Select'`, `has(tables, 'db.table')`: count, median and p95 of `query_duration_ms`, median of `read_rows` and `read_bytes` | 5 s, 20M, 4 GiB, 512 MiB | evidence line omitted |
| `sample` | distinct counts of candidate columns (below) | 10 s, 50M, 4 GiB, 1 GiB | rules that need cardinality do not fire |

**Time bounds.** `system.parts.min_time` and `max_time` describe the source's
timestamp only when the partition key holds that column, and they are zero
when it gives no time column. The age of the data is known only when `is_in_partition_key` is 1 for the source's timestamp column
and `min_time` is not zero. Otherwise it is unknown, and every condition on
age is false. Part modification time measures a merge, so it is not used.

A new helper `clickhouse.IsAccessDeniedError` sits beside
`IsReadLimitExceededError` (`internal/clickhouse/query.go:360`). An input is
`denied` on code 497 (`ACCESS_DENIED`), `missing` on code 60 (`UNKNOWN_TABLE`,
which `system.query_log` returns when query logging is off), `limited` when a
budget was hit, and `error` otherwise. A missing input never fails the
report, except `system.tables` and `system.columns`. The server version comes
from `SELECT version()`, which the client already reads
(`internal/clickhouse/capabilities.go:122`).

**Sample.** The advisor first runs the rules without cardinality to find
candidate columns, at most 20 plain columns. The sample runs only when the
timestamp column is in the sorting key or the partition key, so that each
time filter prunes. Otherwise the input is `skipped`. It reads 7 slices, one
per day of the last 7 days, each 10 minutes long and at a different hour:

```sql
SELECT slice, count() AS sampled_rows, uniqCombined(`c1`) AS u0
FROM (
	SELECT 0 AS slice, `c1` FROM `db`.`tbl`
	WHERE `ts` >= {s0} AND `ts` < {s0} + INTERVAL 10 MINUTE LIMIT 150000
	UNION ALL ... -- one branch per slice, 7 in total
) GROUP BY slice WITH ROLLUP
```

It reads the source's own table, so the connection user needs no extra
grant. A `view_base` target is the exception. A view can run costly work on
every read, so the sample reads the base table, and it is `denied` when the
user holds no `SELECT` there.

`distinct` is the rollup row. A slice is **truncated** when it returns
150,000 rows. An unordered `LIMIT` returns the first rows in key order, so on
a table ordered by `(tenant, ts)` every truncated slice can hold the same few
tenants. A column is **saturated** only when no slice is truncated, at least
5 slices hold 20,000 rows or more, and `distinct` is at most 1.5 times the
largest distinct count of one slice. A lower bound on the distinct count (R1)
may use truncated slices. An upper bound (R4, R6) needs saturation, so a
table that writes more than 150,000 rows in 10 minutes gets no R4 or R6
finding. The finding is still a `Candidate`, because 7 days say nothing
certain about older parts.

`system.query_log` is local to the server that answers, and its evidence line
says so. It also holds the queries of other clients, which the rollup lacks.

### Views and Distributed tables

The advisor resolves a target table before it runs rules. Usage always comes
from the source. Metadata and DDL always refer to the target.

| Source engine | Target | Notes |
|---|---|---|
| `*MergeTree` | the source table | `kind: "table"` |
| `Distributed` | the local table from `EngineParams[1]` and `[2]`, as `getStatsTableLocation` does (`internal/datasource/clickhouse_provider.go:884`) | `kind: "distributed_local"`. Every `ALTER` gets `ON CLUSTER '<cluster>'`. Sizes cover the connected replica only, and an evidence line says so. The sample reads the `Distributed` table with leaf budgets. |
| `View` | the base table, when the view's `SELECT` has one table in `FROM`, no `JOIN`, no set operation and no subquery | `kind: "view_base"`. A finding fires only for a column that the view passes through under the same name, by `SELECT *` or by a bare identifier. |
| `View`, any other shape | none | `status: "unavailable"`, `reason: "view_not_resolvable"` |
| `MaterializedView`, `Merge`, `Buffer`, `Memory`, dictionaries, table functions | none | `status: "unavailable"`, `reason: "unsupported_engine"` |

Resolution follows at most 3 hops and reuses the cycle guard of
`getTableInfo` (`internal/clickhouse/schema.go:48`). A base table that the
connection user cannot see has no row in `system.tables`. The report is then
`unavailable` with `reason: "target_not_readable"`, which is the expected
result for the restricted sources of RFC 0002.

### Rule set

All syntax below was checked against the ClickHouse 26.2 documentation as
published on 2026-11-15. The table before R1 names each page. Each rule names
its build gates, the experiments under "Unverified claims" that must pass in CI
before the rule is registered. A rule whose gate fails ships disabled.

Shared terms. The window is the last 30 days of `query_shape_stats_daily`
(`window_days`, 7 to 90). **Q** is the number of analyzed queries in the
window with at least one filter item, and the share of an item is its query
count divided by Q. No usage rule fires when Q is under 100. A rule ignores
an item whose column the target table lacks. Thresholds are constants in
`internal/advisor/thresholds.go`, not configuration.

Usage rules also need `QueryShapeCoverage` to show `unanalyzed` plus
`overflow` under 20% of all queries in the window. Otherwise the report adds
the note "usage record is incomplete" and no usage rule fires. Impact is `high` at a share of 50% or more, `medium` from 20%, and `low`
below. Storage rules use the column's share of the table's compressed bytes
with the same cut points.

| Fact used | Docs page under https://clickhouse.com/docs/ |
|---|---|
| `bloom_filter` default rate 0.025, default granularity 1, and the functions each index type serves (R1, R2) | `engines/table-engines/mergetree-family/mergetree` |
| `ADD INDEX` changes metadata, `MATERIALIZE INDEX` is a mutation (R1, R2) | `sql-reference/statements/alter/skipping-index` |
| `text` index syntax, general availability from 26.2, preprocessor applies to the needle (R2) | `engines/table-engines/mergetree-family/textindexes` |
| `ADD COLUMN ... MATERIALIZED`, `MATERIALIZE COLUMN`, type changes on key columns (R3, R4) | `sql-reference/statements/alter/column` |
| 10,000 and 100,000 distinct values (R4) | `sql-reference/data-types/lowcardinality` |
| codec syntax, default LZ4 self-managed and ZSTD in Cloud (R5) | `sql-reference/statements/create/table` |
| `MODIFY ORDER BY` cannot add an existing column (R6) | `sql-reference/statements/alter/order-by` |
| about a thousand partitions at most (R7) | `engines/table-engines/mergetree-family/custom-partitioning-key` |

#### R1 `skip_index_bloom`: bloom filter for equality on a high-cardinality column

| | |
|---|---|
| Trigger | `eq` or `in` items on plain column `c` reach a share of 5% and at least 20 queries. `c` is not the first sort key column. No existing skip index has `expr` equal to `c`. The sample shows at least 10,000 distinct values. |
| Evidence | query count and share, mean duration of those queries, sampled distinct count, column size, existing indexes on the table |
| DDL | `ALTER TABLE db.t ADD INDEX IF NOT EXISTS idx_c_bloom c TYPE bloom_filter GRANULARITY 1;` then `ALTER TABLE db.t MATERIALIZE INDEX idx_c_bloom;` |
| Gain | ClickHouse skips granules that cannot hold the value. Large for a needle such as a request id. None for `!=` and `NOT IN`. |
| Cost and risk | `ADD INDEX` changes metadata only. `MATERIALIZE INDEX` is a mutation over every part. Inserts and merges build the index from then on. |
| False positives | The cardinality floor excludes columns such as `level`, where every granule holds every value. Unknown cardinality means no finding. `Verify` gives `EXPLAIN indexes = 1 SELECT count() FROM db.t WHERE c = '<value>'`. |
| Build gate | G1 |

#### R2 `text_index`: text index for token and pattern search

| | |
|---|---|
| Trigger | Server version is 26.8 or later, the oldest version that CI tests with a `text` index. On an older server the report adds the note "text index advice needs ClickHouse 26.8". `token` and `like_token` items on `String` column `c` reach a share of 10% and at least 20 queries. No existing index of type `text`, `tokenbf_v1` or `ngrambf_v1` has `c` in `expr`. |
| Evidence | query count and share per kind, mean duration, column size |
| DDL | `ALTER TABLE db.t ADD INDEX IF NOT EXISTS idx_c_text c TYPE text(tokenizer = splitByNonAlpha);` then `ALTER TABLE db.t MATERIALIZE INDEX idx_c_text;` |
| Gain | `hasToken`, `hasAllTokens` and `hasAnyTokens` read the index. `LIKE` uses it when the pattern holds a complete token. |
| Cost and risk | The index covers each whole part. Materializing a column with very many tokens uses much memory. Index size is not predictable from metadata. |
| False positives | `like`, `prefix`, `regex`, `position` and `ilike` items do not count toward the trigger. A `match` uses the index only when a complete token can be taken from its pattern, and a regular expression such as `[0-9]+` holds none. ClickHouse 26.8 also serves `LIKE '%token%'` and `ILIKE` from the index by scanning its dictionary (`use_text_index_like_evaluation_by_dictionary_scan`, on by default), but the docs promise no index for them, so they still do not count: the trigger errs toward no finding. The docs list no skip index that serves `position`, and `ILIKE` needs a lowercasing preprocessor. When `position` items alone would pass the trigger, the report adds the note "N LogchefQL queries search `c` with `~`, which compiles to `positionCaseInsensitive`. No index serves it." See open question 3. |
| Semantics | The index has no `preprocessor`. The docs state that token functions apply the index's preprocessor to the needle, so `preprocessor = lower(c)` would turn a case-sensitive `hasToken` into a case-insensitive one. That changes results, and the advisor does not offer it. |
| Build gate | G1, G2 |

#### R3 `materialized_column`: column for a hot Map key or JSON path

| | |
|---|---|
| Trigger | Items with one path on column `m` reach a share of 10% and at least 20 queries. Either `m` is `Map(String, String)` or `Map(LowCardinality(String), String)` and the access is `map` or `nested`, or `m` is `String` and the access is `json_string` or `nested`. No column has a `default_expression` equal to the generated expression. |
| Evidence | query count and share, kinds used, mean duration, size of `m` and its share of the table |
| DDL, Map | `ALTER TABLE db.t ADD COLUMN IF NOT EXISTS m_k String MATERIALIZED m['k'];` |
| DDL, String JSON | `ALTER TABLE db.t ADD COLUMN IF NOT EXISTS m_a_b String MATERIALIZED JSONExtractString(m, 'a', 'b');` |
| Optional DDL | `ALTER TABLE db.t MATERIALIZE COLUMN m_k;` to fill existing parts |
| Gain | A filter on the new column reads one small column, not the whole map or JSON string. The new column can then take a bloom filter (R1). |
| Cost and risk | New parts store the value twice. `MATERIALIZE COLUMN` is a mutation that rewrites the column in every part. Queries must name the new column to gain. Logchef does not rewrite `m.k`. |
| False positives | The expression is rebuilt from the ordered segments and the access, so `'a.b'` and `'a', 'b'` stay distinct. Both forms yield `String`, which is the type of the expression. A map with another value type, a `json_other` item and a native `JSON` column get no finding. The generated name replaces every character outside `[A-Za-z0-9_]` with `_`, and the rule skips the finding when that name exists. |
| Build gate | G3 |

#### R4 `low_cardinality`: `LowCardinality` for a repetitive string column

| | |
|---|---|
| Trigger | `c` is `String` or `Nullable(String)`. It is in no sorting, primary or partition key. Its uncompressed size is at least 1 GiB or 1% of the table. The sample shows `c` saturated with under 1,000 distinct values. |
| Evidence | rows and distinct count per slice and in total, with the line "from 7 slices of the last 7 days", column compressed and uncompressed size |
| DDL | `ALTER TABLE db.t MODIFY COLUMN c LowCardinality(String);` or `LowCardinality(Nullable(String))` |
| Gain | Dictionary encoding. Smaller column, faster `=` and `GROUP BY`. |
| Cost and risk | The type change is a mutation that rewrites the column in every part. Clients that read the column type see the new type. |
| False positives | The docs give 10,000 distinct values as the bound for a gain and 100,000 as the point of harm. The rule uses 1,000 and requires saturation. The finding is a `Candidate`. Its `Verify` is `SELECT uniq(c) FROM db.t`, which the admin runs when a full scan is acceptable. Key columns are excluded, because a key column's type may change only when the data does not. |
| Build gate | G4 |

#### R5 `codec`: codec for a large column on the default codec

| | |
|---|---|
| Trigger A | `c` is the source's timestamp column, of type `DateTime` or `DateTime64`, with an empty `compression_codec`, and at least 1 GiB compressed. |
| DDL A | `ALTER TABLE db.t MODIFY COLUMN c CODEC(Delta, ZSTD(1));` |
| Trigger B | `c` is a `String` with an empty `compression_codec`, at least 10 GiB compressed, at least 20% of the table, and a ratio of uncompressed to compressed under 3. |
| DDL B | `ALTER TABLE db.t MODIFY COLUMN c CODEC(ZSTD(1));` |
| Evidence | column sizes, ratio, share of the table, current codec |
| Gain | Smaller column on disk. The finding gives no number. |
| Cost and risk | More CPU on insert and merge. Trigger B gains nothing where the server default is already ZSTD. The docs state that default for ClickHouse Cloud, and Logchef cannot tell the two apart. |
| False positives | Fires only when the column has no explicit codec, so a deliberate choice is never contradicted. The size floors keep small tables out. |
| Build gate | G5 |

#### R6 `sort_key`: sorting key that does not match the common filter

| | |
|---|---|
| Trigger | The target engine is `MergeTree` or `ReplicatedMergeTree`. `primary_key` equals `sorting_key`. Q is at least 200 over at least 14 distinct days. `eq` or `in` items on plain column `c` reach a share of 50%. `c` does not come before the timestamp column in the sorting key. The sample shows `c` saturated with under 10,000 distinct values. |
| Evidence | query count and share, the current sorting key, distinct counts per slice, table size, and the proposed key |
| Proposed key | The qualifying columns, at most 3, in ascending order of sampled distinct count, followed by the current key. |
| DDL | None. `Rebuild: true`, `Candidate: true`. |
| Gain | Queries that filter on `c` read only the matching key range. |
| Cost and risk | A new table and a planned cutover. Rows that arrive during a copy stay in the old table unless ingestion pauses or a catch-up step moves them. Queries that filter on time alone read more granules per time range. |
| False positives | The engine and primary key conditions keep out tables where the key decides which rows merge or where a separate primary key would no longer be a prefix. The 50% share and the 14-day spread keep one busy week from moving a key. A high-cardinality column goes to R1. |
| Build gate | none, because the finding has no DDL |


#### R7 `partitioning`: partition key that is missing or too fine

| | |
|---|---|
| Trigger A | The target engine is `MergeTree` or `ReplicatedMergeTree`. No partition key, and the table holds at least 100 GiB. |
| Trigger B | Same engines. More than 1,000 active partitions. |
| Evidence | partition key, partition count, table size, and the proposed key |
| Proposed key | `toYYYYMM(ts)`, or `toDate(ts)` when the table has a TTL under 90 days. |
| DDL | None. `Rebuild: true`. |
| Gain | A: TTL and manual cleanup drop whole parts. B: fewer files per query. |
| Cost and risk | As R6. A new partition key also changes which rows a merge can combine. |
| False positives | 1,000 is the bound that the docs give. Trigger A uses size alone, because an unpartitioned table has no known age. |
| Build gate | none |

#### R8 `retention_review`: old data that the Explorer never reads

| | |
|---|---|
| Trigger | The table has no TTL (`extractTTLFromTableInfo`, `internal/datasource/clickhouse_provider.go:810`). The age of the data is known and over 90 days. `QueryShapeCoverage` over 90 days shows at least 60 recorded days, at least 200 queries, under 5% of them with a NULL lookback, and a largest lookback under half the age of the oldest part. |
| Evidence | age of the oldest part, bytes in partitions older than the largest lookback, the largest lookback, recorded days, and the share of queries with no lookback |
| DDL | None. Impact is always `info`. |
| Gain | None stated. The finding is a prompt to review retention. |
| Cost and risk | None, because nothing is generated. The text says "Logchef sees only Explorer queries. Alerts, dashboards, exports and other clients may read older data. Choose a retention period from your own policy." |
| False positives | The lookback comes from an aggregate over every rollup row, so the 5,000-row cap cannot hide a rare long query. Unknown age, thin coverage or missing lookbacks suppress the finding. |
| Build gate | none |

### Analysis run

`core.RefreshSchemaAdvice(ctx, store, ds, principal, sourceID, windowDays)`:

1. Require a global admin and `sources:read`, as
   `InspectSourceActivityAsAdmin` does (`internal/core/source.go:129`).
2. Load the source. A source that is not ClickHouse is a validation error.
3. Read `ListQueryShapeStats` and build the usage facts.
4. Call the provider for metadata, through a new optional interface
   `SchemaAdvisorProvider` that `ClickHouseProvider` implements, in the style
   of the activity provider (`internal/datasource/inspection_cache.go:28`).
5. Run the rules, sample the candidates, run the rules again with cardinality.
6. Sort findings by impact and share, keep 50, and upsert the report.

The run has a 30 s deadline. One run per source at a time in a process
(`singleflight`), two runs per process, and a third gets 429. The first slice
runs on demand only, when an admin presses **Run analysis** or calls the
refresh route. U13 adds a weekly run on the job runner of RFC 0006, off by
default and switched on per source. The feature is complete without it.

Cost on ClickHouse per run: five budgeted reads on system tables, one
`SELECT version()` and one sample. The report shows what each one read. A
report older than 7 days shows a "stale" badge and stays until a trigger.

### API

All routes sit in the admin group (`internal/server/server.go:348`), next to
the stats route (`internal/server/server.go:386`). Handlers go in a new file
`internal/server/schema_advisor_handlers.go`.

| Route | Scope | Result |
|---|---|---|
| `GET /api/v1/admin/sources/:sourceID/advisor` | `sources:read` | 200 with the stored report |
| `POST /api/v1/admin/sources/:sourceID/advisor/refresh` | `sources:read` | 200 with the new report |
| `PUT /api/v1/admin/sources/:sourceID/advisor/findings/:findingID/dismissal` | `sources:write` | 200 with the dismissal |
| `DELETE /api/v1/admin/sources/:sourceID/advisor/findings/:findingID/dismissal` | `sources:write` | 204 |

Refresh takes `sources:read` because the stats route already refreshes from
ClickHouse under that scope (`internal/server/source_handlers.go:175`).
`GET` and refresh return `source_id`, `generated_at`, `generated_by`,
`window_days`, `stale`, `report` (the `SchemaAdvisorReport`) and `dismissals`
(a list of `finding_id`, `reason`, `dismissed_by`, `dismissed_at`).
Before the first run, `GET` returns 200 with `"generated_at": null` and
`"report": null`. Refresh body: `{"window_days": 30}`, optional. Dismissal
body: `{"reason": "..."}`, optional.

Errors:

| Case | Status |
|---|---|
| source not found | 404 |
| source is not ClickHouse | 400, validation error |
| `window_days` outside 7 to 90, `reason` over 500 bytes, `findingID` not 16 hex characters | 400 |
| two runs already active | 429 |
| ClickHouse unreachable, or the 30 s deadline passed | 502, external service error, and the stored report is kept |
| not a global admin, or scope missing | 403 |

### Client surfaces

**Web UI.** A new component
`frontend/src/views/sources/components/SourceInspectionAdvisor.vue` renders
under the schema table in `SourceStats.vue`, for ClickHouse sources only. The
route is already admin-only (`frontend/src/router/index.ts:201`).

- The header has the report time, a stale badge, a **Run analysis** button
  and a chip for every input whose state is not `ok`.
- Each finding is a card: impact badge, title, evidence rows, the DDL in a
  code block with a copy button, gain, cost and the verify query. `rebuild`
  shows "Needs a table rebuild", and `candidate` shows "Validate first".
- **Dismiss** opens a dialog with an optional reason. A **Show dismissed**
  toggle lists dismissed findings with **Restore**.
- `EmptyState`, `LoadingState` and `ErrorAlert` cover the empty, running and
  failed states. API calls go in `frontend/src/api/sources.ts` and state in
  `frontend/src/stores/sources.ts`. New strings go in all five catalogs under
  `frontend/src/i18n/locales`. Finding text comes from the server untranslated.

**CLI.** A new command `logchef advisor <source>` in
`cli/crates/logchef-cli/src/commands/advisor.rs`, registered beside `Sources`
(`cli/crates/logchef-cli/src/cli.rs:128`). It prints findings as text, or the
raw response with `--output json`. `--refresh` refreshes first, and `--ddl`
prints only the statements. A 403 exits non-zero and names the needed scope.

**MCP, Ask Chef and provisioning TOML.** Not affected. See "Changes after
build".

**Docs.** A new page `docs/src/content/docs/features/schema-advisor.mdx`, and
one line per metric in `docs/src/content/docs/operations/metrics.mdx`.

**Metrics.** `logchef_schema_advisor_runs_total{status}`,
`logchef_schema_advisor_run_duration_seconds`, and
`logchef_query_shape_records_total{result}`.

## Access and security

- Reading, refreshing and dismissing need a global admin. Table sizes, index
  names and other teams' usage are admin data, as on the inspection page.
- Token scopes are the existing `sources:read` and `sources:write`. The
  rollup stores no literal, no query text and no user id.
- DDL text is built from identifiers that ClickHouse returned and from path
  segments, escaped with `queryescape.ClickHouseIdentifier` and
  `queryescape.ClickHouseString`. Logchef never executes it.
- **Restricted sources (RFC 0002).** A restricted user cannot read system
  tables when `select_from_system_db_requires_grant` is on. The report is
  then `unavailable`, and no base-table metadata leaks. The sample of a view
  reads the base table only when the user may, so it widens nothing.
- The only outbound requests are queries on the source's own connection.

## Limits and failure modes

| Bound | Value | What the user sees |
|---|---|---|
| Items per shape | 16 | query counted as unanalyzed |
| Distinct shapes per source per day per process | 500 | counted under `overflow`, and a note in the report |
| Rollup retention | 90 days | windows are capped at 90 days |
| Shape rows read per run | 5,000 | note "usage truncated to the 5,000 most common shapes" |
| Sample | 7 slices of 150,000 rows, 20 columns, 10 s, 50M rows read, 4 GiB read, 1 GiB memory, 2 threads. Read budgets are approximate. | input `sample` is `limited`, cardinality rules do not fire. A truncated slice suppresses R4 and R6. |
| `system.query_log` read | 5 s, 20M rows read, 4 GiB read | input is `limited`, evidence line omitted |
| `unanalyzed` plus `overflow` at 20% or more | | note "usage record is incomplete", usage rules do not fire |
| Findings per report | 50 | note "N more findings not shown" |
| Report size | 256 KiB | evidence is trimmed from the lowest-impact findings first |
| Run deadline | 30 s | 502, the stored report is kept |
| Concurrent runs per process | 2 | 429 |
| Q below 100 | | note "too few recorded queries for usage rules". Storage rules still run. |

Several replicas on Postgres. Each replica records shapes with an upsert, so
counters add up, and the shape cap makes the row bound 500 × N. Two replicas
can analyze one source at once, and the later upsert wins. Runs and the
prune are idempotent, so neither needs a lock.

## Backend differences

ClickHouse gets the full feature. VictoriaLogs and Loki (RFC 0011) get none.
For those sources the routes return 400 with "Schema advice is
available for ClickHouse sources only", and the UI section is not rendered.
No capability flag is added. The advisor is an optional interface that only
`ClickHouseProvider` implements, and core checks for it with a type assertion.

## Seams

- **Requires from RFC 0006 (job runner).** A way to register a named job with
  a weekly schedule that runs on one replica per tick. After downtime the
  runner selects the latest due occurrence and skips older ones. The job
  calls `core.RefreshSchemaAdvice` for each opted-in source, one at a time.
  It ignores the effective time `At`. Only U13 depends on this.
- **Requires from RFC 0009 (audit).** Units P2 and P3. This RFC follows that
  contract: two action constants in `internal/audit/actions.go`, one view
  function `SchemaAdvisorDismissalFields` in `internal/audit/views.go`, a
  route name and one `auditSet` line on each dismissal route. The refresh
  route gets an entry in `auditExemptRoutes` with the reason "analysis run,
  recorded in schema_advisor_reports". No recorder is called directly.
- **RFC 0005.** No dependency. `logchefql.FilterItems` reads the `Where` node
  only, whatever stages 0005 adds.
- **RFC 0002.** No conflict. Its change 2 can reuse `IsAccessDeniedError`.

No seam conflict found.

## Audit events

| Event | Actor | Target |
|---|---|---|
| `schema_advisor_dismissal.create` | the admin | source, with `finding_id` and `rule` as detail, and `reason` in the field view |
| `schema_advisor_dismissal.delete` | the admin | source, with `finding_id` |

Both verbs come from the verb list of RFC 0009. A dismissal is a
configuration change. A refresh is a run, so it emits no event, and the
report row keeps who ran it and when. No field holds query text.

## Alternatives rejected

- **Analyze `query_history` at request time.** Rejected because history keeps
  200 rows per user across all sources, and demo mode records nothing.
- **A rollup per column, not per shape.** Rejected because the sort key rule
  needs to know which columns occur together.
- **Read usage from `system.query_log` alone.** It sees every client.
  Rejected as the base because many deployments deny it, it is local to one
  server, and arbitrary SQL is harder to analyze than LogchefQL.
- **Run `ALTER` from Logchef behind an admin confirmation.** Rejected. It
  needs a privileged connection per source and contradicts RFC 0002.

## Implementation plan

U1 and U3 have no dependencies. U2 and U4 depend on U1. Units in one row of
dependencies can be built in parallel, as can U6 and U8 to U11 after U5. U5
with U6 is the first slice that a user can use. A build gate is an
integration test in the `clickhouse-integration` job, which runs ClickHouse
23.7, 24.1 and 26.8 (`.github/workflows/go-tests.yml:103`). It runs on every
version at or above the rule's minimum and executes the generated DDL on a
table with existing parts. The job tests three packages
(`.github/workflows/go-tests.yml:138`), so U4 adds `./internal/advisor` to
its `go test` command. "Unverified claims" names each gate's file and unit.

**U1. Filter shape extraction.** Depends on nothing.
- Files: create `pkg/models/schema_advisor.go` (shape types only),
  `internal/advisor/shape.go` and its test. Edit
  `internal/logchefql/logchefql.go` for the exported `FilterItems`.
- Check: table tests, one case per row of both mapping tables, `LIKE`
  patterns with and without a complete token, plus `NOT`,
  mixed-column `OR`, column-to-column, 17 items, `JOIN`, subquery and
  unparsable input, a query on another table, and `JSONExtractString(m, 'a.b')`
  against `(m, 'a', 'b')`. One test asserts that no literal from the input
  appears in the shape JSON.

**U2. Shape rollup and recorder.** Depends on U1.
- Files: the migration pair "add query shape stats", both `queries.sql` plus
  sqlc output, `internal/store/store.go` and both stores, the storetest,
  `internal/server/query_history.go` and the four call sites.
- Change: the recorder per "Recording", with its metric.
- Check: storetest proves on both stores that two increments of one shape sum
  the counters and keep the larger lookback, that prune deletes only old
  rows, and that `QueryShapeCoverage` counts a row outside the top 5,000. A
  server test runs one LogchefQL and one SQL query and reads two rollup
  rows. A test with 501 distinct shapes finds one `overflow` row.

**U3. ClickHouse advisor metadata.** Depends on nothing.
- Files: create `internal/clickhouse/advisor_metadata.go` and its test. Edit
  `internal/clickhouse/query.go` for `IsAccessDeniedError`.
- Check: unit tests for view resolution (`SELECT *`, renamed column, `JOIN`,
  nested view, cycle), the error-to-state mapping, and unknown age for an
  unpartitioned table. Integration tests, in the style of
  `internal/logchefql/sql_generator_integration_test.go`, read back a
  MergeTree table with one index. A fixture ordered by `(tenant, ts)`, where a
  second column has few values for the first tenants and many overall, ends
  with truncated slices and no saturation. Gates G6, G7 and G8 pass.

**U4. Rule engine with R1 and R3.** Depends on U1.
- Files: create `internal/advisor/facts.go`, `rules.go`, `thresholds.go`,
  `rule_bloom.go`, `rule_materialized_column.go` and tests. Add the report
  types to `pkg/models/schema_advisor.go`. `advisor.Analyze(facts)` is pure.
- Check: table tests per rule with a firing case, one case per false-positive
  guard and a golden DDL string. A column named `` a`b `` proves the escaping.
  Edit `.github/workflows/go-tests.yml`. Gates G1 (bloom) and G3 pass.

**U5. Report storage, core and API.** Depends on U2, U3 and U4.
- Files: the migration pair "add schema advisor reports", store methods,
  `internal/core/schema_advisor.go`,
  `internal/server/schema_advisor_handlers.go`, `internal/server/server.go`,
  `internal/datasource/service.go` and `clickhouse_provider.go`.
- Change: `SchemaAdvisorProvider`, the `GET` and refresh routes, the two run
  metrics. If RFC 0009 P3 has merged, add the refresh route to
  `auditExemptRoutes`. Otherwise the later of the two PRs adds it.
- Check: handler tests for every row of the error table. An integration test
  seeds the rollup, refreshes against dev ClickHouse and finds an R1 finding
  for the seeded column. A VictoriaLogs source returns 400.

**U6. Web UI.** Depends on U5.
- Files: create `SourceInspectionAdvisor.vue` and its test. Edit
  `SourceStats.vue`, the sources API client and store, and five locale files.
- Check: `bun run typecheck` and `bun run test` pass. UI scenario: on a
  ClickHouse source press **Run analysis**, copy a finding's DDL and run it
  in `clickhouse-client` unedited. A VictoriaLogs source has no section.

**U7. Dismissals.** Depends on U5, U6 and RFC 0009 P3.
- Files: the migration pair "add schema advisor dismissals", store methods,
  the two named routes, the dialog and the toggle. Edit
  `internal/audit/actions.go` and `internal/audit/views.go`.
- Check: storetest for upsert and delete. A handler test finds one audit
  event per dismissal and per restore, with no `detail.unstaged`. UI
  scenario: dismiss a finding, refresh, find it under **Show dismissed**,
  restore it.

**U8. Rules R4 and R5.** Depends on U4. Creates `rule_low_cardinality.go`,
`rule_codec.go` and tests.
- Check: table tests as in U4. A key column, an unsaturated column, a column
  with a truncated slice and a column with an explicit codec do not fire.
  Gates G4 and G5 pass.

**U9. Rules R6, R7 and R8.** Depends on U4. Creates `rule_sort_key.go`,
`rule_partitioning.go`, `rule_retention_review.go` and tests.
- Check: table tests. R6 does not fire with 13 days of data, on a
  `ReplacingMergeTree`, or with a separate primary key. R6 does not fire with a truncated slice. R7 A fires
  on size alone. R8 does not fire with unknown age, with 59 recorded days or when a
  TTL exists, and no finding of the three carries DDL.

**U10. Rule R2 and notes.** Depends on U4. Creates `rule_text_index.go` and
its test.
- Check: fires on `token` and `like_token` items at 26.8, not at 26.7, and
  not on `like`, `regex`, `position` or `ilike` items alone. Gates G1 (text)
  and G2 pass on 26.8.

**U11. CLI.** Depends on U5. Creates
`cli/crates/logchef-cli/src/commands/advisor.rs`, edits `mod.rs` and `cli.rs`.
- Check: `just check-cli` passes. `logchef advisor <source> --ddl` prints
  only statements. A member token exits non-zero with the scope message.

**U13. Docs, then scheduled refresh.** Docs depend on U6 to U10. The refresh
depends on U5 and RFC 0006 and is its own PR, with a per-source switch.
- Files: the docs page, `operations/metrics.mdx` and `CHANGELOG.md`. The
  refresh PR registers the weekly job per "Seams".
- Check: the docs build passes. With two replicas on Postgres, one tick
  writes one report per opted-in source and none for the others.

## Changes after build

- **U12 (MCP tool) is dropped.** No MCP caller can be a global admin. `/mcp`
  accepts only OAuth access tokens (`internal/server/mcp_handlers.go:66`), and
  `Principal.RequireGlobalAdmin` rejects OAuth and Ask Chef's assistant
  principal whatever the user's role (`internal/core/access/access.go:149`).
  The tool could never return a report. This is the same rule that keeps
  `audit:read` token-only in RFC 0009. Admins read the report in the web UI
  or with `logchef advisor`, using an API token.
- **G2 and R2 match U10 (#325).** ClickHouse 26.8 serves `LIKE '%token%'` and
  `ILIKE` from a `text` index by a dictionary scan. The R2 "False positives"
  cell and the G2 row say so, and R2 still does not count those items.
- **U13 shipped its docs first.** The page is
  `docs/src/content/docs/features/schema-advisor.mdx`. The weekly scheduled
  refresh followed in its own PR. The per-source switch is the job row itself:
  `PUT /api/v1/admin/sources/:sourceID/advisor/schedule` creates or deletes a
  `schema_advisor.refresh` job keyed by the source id, on Monday 03:00 UTC with
  a 30-day window, audited as `schema_advisor_schedule.update`. Contrary to
  "Seams", the job does not ignore `At`. A run stands for its tick, as every
  RFC 0006 job does: the usage window ends on the tick's day, the rules measure
  ages from it, and `generated_at` is the tick. `ListQueryShapeStats` and
  `QueryShapeCoverage` take an inclusive `until` date for that, so a run
  claimed late after downtime does not count usage recorded after its tick.
- **The `system.query_log` read became evidence.** Every finding ends with one
  line from it, "Reads of the source in 7 days, all clients", with the query
  count, median and p95 duration, and median rows and bytes read, on the
  server that answered. A read that is not `ok` drops the line. No new read
  was added.

## Open questions for Karan

1. **Who sees the report?** Default: global admins only. Team admins would
   see table sizes and other teams' usage.
2. **Should usage from alerts and dashboard panels count?** Default: no for
   the first release. They are the first input to add next.
3. **Should LogchefQL `~` compile to a predicate that a text index serves?**
   Default: no change here. That is a follow-up RFC.
4. **Scheduled refresh on by default?** Default: no. It is a per-source
   switch that an admin turns on after seeing what one run reads.
5. **Is the retention review wanted?** Default: keep it. It generates no
   statement and only reports old data that the Explorer never read.

## Unverified claims

Each claim is a build gate, an integration test that its owning unit adds.
A rule stays unregistered until its gates pass. Rule gates live in
`internal/advisor/gates_integration_test.go`, metadata gates in
`internal/clickhouse/advisor_metadata_integration_test.go`.

| Gate | Owner, package | Claim | Experiment | If it fails |
|---|---|---|---|---|
| G1 | U4 for bloom, U10 for text, `internal/advisor` | A `bloom_filter` or `text` index added with `ADD INDEX` plus `MATERIALIZE INDEX` is used for the rule's predicates on parts that existed before, and query results do not change. | Create a table with 3 parts, record results, run the DDL, compare results and read `EXPLAIN indexes = 1`. | R1 or R2 ships disabled. |
| G2 | U10, `internal/advisor`, 26.8 only | A `text` index without a preprocessor serves `hasToken`, `hasAllTokens`, `hasAnyTokens` and a `LIKE` with a complete token, serves no `match(c, '[0-9]+')`, and keeps mixed-case results identical, including on parts without the index. `positionCaseInsensitive` uses no index. `LIKE '%token%'` and `ILIKE` may read the index (26.8 does, by a dictionary scan) and keep identical results; R2 does not count them. | Same table with mixed-case rows, one part left unmaterialized. Compare results for every predicate, and `EXPLAIN indexes = 1` for each eligible predicate and for `match` and `positionCaseInsensitive`. | R2 ships disabled, or the token test in "Filter extraction" changes. |
| G3 | U4, `internal/advisor` | `ADD COLUMN ... MATERIALIZED` for a Map key and for `JSONExtractString` reads the same value as the original expression on old and new parts, before and after `MATERIALIZE COLUMN`. | Compare `m['k']` with `m_k` row by row. | R3 ships disabled. |
| G4 | U8, `internal/advisor` | `MODIFY COLUMN c LowCardinality(String)` keeps the default expression, comment and codec, runs as one mutation, and keeps values. | Alter a column that has all three, read `SHOW CREATE TABLE` and `system.mutations`. | R4 emits a full column definition, or ships disabled. |
| G5 | U8, `internal/advisor` | `MODIFY COLUMN c CODEC(...)` is accepted on a `DateTime64` sorting key column, leaves existing parts untouched until a merge, and an empty `compression_codec` means the default codec. | Alter, then compare `system.parts_columns` before and after with merges stopped. | The cost text of R5 changes to "rewrites existing parts", or trigger A is dropped. |
| G6 | U3, `internal/clickhouse` | Each budget stops its query with an error that maps to `limited`. A 5 s budget ends a query under a 30 s context. `max_execution_time_leaf` overrides a larger source value. The seven-branch sample reads close to its `LIMIT` per branch when the timestamp is in the key. | Run a slow query under a 30 s context with a 5 s budget. Run the real sample on a table larger than the budget and on a 2-shard `Distributed` table, with `prefer_localhost_replica` 0 and 1, and with a source setting of `max_execution_time_leaf = 300`. Record `ReadRows` against the budget. | The measured overshoot goes into the docs page. A budget that does not stop a query blocks U5. |
| G7 | U3, `internal/clickhouse` | A denied system table returns code 497, a `system.query_log` that never existed returns code 60, and `system.tables` returns no row for a table without a grant. | Run the five reads as a user without grants, with `select_from_system_db_requires_grant` on. | The state mapping in U3 changes. |
| G8 | U3, `internal/clickhouse` | `has(tables, 'db.table')` in `system.query_log` matches a read through a view or a `Distributed` table. | Run one query through each, flush logs, and read `tables`. | The evidence line is dropped for those targets. |
