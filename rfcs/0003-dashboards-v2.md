# RFC 0003: Dashboards v2

Status: Accepted. Phases 1 to 9 and 11 are implemented on develop; phase 14 is in review.
Date: 2026-11-12
Authors: Karan Sharma

## Summary

A dashboard panel today is one of four types, and three of them can only count
log rows. We migrated a production Metabase collection (2 dashboards, 88 saved
questions) into Logchef v2.6.1 and could not express most of it. Sums, trend
tiles, bar charts of query columns, daily buckets, tabs, folders and date-only
variables all fell back to tables or workarounds.

An independent review of the first draft found one blocker and 15 major issues
in authorization, client compatibility, numeric precision and time handling.
The first revision answered each finding. "What the review showed" maps each
finding to its section.

A second review confirmed that the blocker is closed. It found ten places
where the spec still allowed a lost update, a dropped feature or an unbounded
result, or described a behavior that the code does not have. This revision
writes in one decision for each. "What the re-review showed" lists them.

A third review found phase 2 ready to build, phase 1 ready after one edit and
phase 3 not ready. It raised five findings, and this revision writes in one
decision for each. "What the third review showed" lists them. The contract of
phase 2 does not change, because its implementation has started.

This RFC makes seven decisions.

1. A panel is a query plus a visualization. Every query returns a frame
   (typed columns and rows). Every visualization is a view over a frame.
   There is one fetch path, not one per panel type.
2. Two query modes exist. `count` keeps the histogram path, which is cheap and
   portable across ClickHouse and VictoriaLogs. `result` runs the query as
   written and charts its columns.
3. The panel blob moves to schema 2. The server upgrades a schema 1 blob when
   it reads the row. No store migration rewrites stored blobs. A stored blob
   records the features that it uses, and every write declares the features
   that its client understands. The server refuses a write that would drop a
   feature.
4. A save is atomic. Each dashboard has an integer revision. Every update
   carries the revision that the client loaded, and the update is one
   compare-and-swap on it.
5. Numbers stay exact. Wide integers and decimals keep every digit in tables,
   tooltips, reducers and thresholds. A chart that plots an approximation says
   so.
6. Time binding, granularity, variables and limits become query-language
   features (`{{__from}}`, `{{__interval}}`, a `day` variable type, a LIMIT
   rule), so the Explorer, the CLI and MCP get them too. Restoring the
   migrated dashboards needs none of the macros and rewrites no SQL.
7. Collections become the home for dashboards as well as saved queries. A
   collection link is either a pin, which grants nothing, or a managed link,
   which delegates view and edit. Only the creator of an item or a global
   admin creates or removes a managed link. Collections nest to a depth of 4,
   and access is set on the root.

The plan has 28 phases in three groups. Phases 1 to 3 are the foundation.
Phase 1 closes a hole in released code and ships alone, as a security fix.
Phases 4 to 9 restore everything the migrated dashboards lost, and phase 10
checks the result against the corpus. The other phases follow. The ones that
the review asked to defer do not start before that check passes.

## What the migration showed

The corpus is the export of that Metabase collection plus the version 1
dashboards we built from it. The files (`cards.json`, `dash.json`,
`dashboards.json`) stay outside this repository because they hold
organization-specific queries. The counts below come from those files.

- 88 saved questions: 57 tables, 12 trend tiles, 8 bar charts, 7 scalars,
  3 area charts, 1 pie.
- 47 questions use template variables. 45 use a date variable.
- Dashboard A has 4 scalar tiles. Dashboard B has 21 cards on two tabs:
  7 trend tiles, 5 tables, 6 bar charts, 3 area charts.

What Logchef v2.6.1 produced:

- 12 of the 20 non-table dashboard cards became tables.
- All 7 trend tiles lost their comparison with the previous day.
- 8 stat tiles needed their SQL rewritten to return one row per counted event.
- The two tabs became one long page.
- 44 saved queries needed a `toDate({{date}})` wrap. The 45th already
  converts its variable with `toDate`.

Each gap has one cause in the code.

| # | Gap | Cause |
|---|---|---|
| 1 | Stat, time series and breakdown panels only count rows | All three call the histogram endpoint (`frontend/src/stores/dashboards.ts:372-416`). ClickHouse wraps the panel SQL as `SELECT <bucket>, count(*) FROM (<sql>)` (`internal/clickhouse/logs.go:221-225`). A grouped query is rejected unless it selects the timestamp column (`internal/clickhouse/logs.go:401-403`). The stat value is the sum of bucket counts (`frontend/src/stores/dashboards.ts:397-399`). |
| 2 | No panel granularity | The bucket size targets 100 buckets for the dashboard range (`frontend/src/services/HistogramService.ts:2`, `:29-56`). Seven days gives 2h buckets. |
| 3 | No tabs | The blob has one `layout` array (`pkg/models/dashboards.go:169-181`). |
| 4 | Dashboards have no folder or team | `Dashboard` has no owner other than `created_by` (`pkg/models/dashboards.go:14-34`). Collections hold only saved queries (`pkg/models/collections.go:59-70`). |
| 5 | Collections are flat | `collections` has no parent column (`internal/store/sqlite/migrations/000019_add_collections.up.sql:16-24`). |
| 6 | Date variables always carry a time | `formatDate` always renders `'YYYY-MM-DD HH:MM:SS'` (`internal/template/variables.go:333`). ClickHouse rejects that string when it is compared with a `Date` (observed during the migration). |
| 7 | A query's own LIMIT is replaced | A positive request limit overwrites the statement's LIMIT (`internal/clickhouse/query_builder.go:329-340`). A table panel always sends one (`frontend/src/utils/dashboardRuntime.ts:82-85`). |
| 8 | Stat tiles abbreviate | `PanelStat` renders `formatCompactCount` (`frontend/src/views/dashboards/components/PanelStat.vue:12`, `frontend/src/utils/histogram-chart.ts:125`). No format options exist. |

Other limits found on the way:

- Dashboards have no variables. Panel requests never send `variables`, although
  every query endpoint accepts them (`pkg/models/query.go:114`, `:136`).
- No text panels.
- A native ClickHouse SQL panel ignores the time picker. The ClickHouse
  provider applies only the limit policy to raw SQL
  (`internal/datasource/clickhouse_provider.go:342-343`).
- At most 24 panels, heights 1 to 6 rows, widths limited to 2, 3, 4, 6, 8, 9
  and 12 (`pkg/models/dashboards.go:64-71`, `:129-137`).
- The panel builder never offers ClickHouse SQL
  (`frontend/src/views/dashboards/components/PanelBuilderDrawer.vue:104-112`).

## What the review showed

A reviewer on another model family read the first draft against the code.
Findings 1, 3 and 6 were then re-checked against the cited lines. The other
citations in this revision were read at the lines given.

| # | Finding | Where this RFC answers it |
|---|---|---|
| 1 | Blocker. Any collection participant can pin an item (`internal/core/collections.go:385-392`) and anyone can create a collection and own it (`internal/core/collections.go:96-108`). "A collection editor may edit what the collection holds" then lets a viewer edit any dashboard | "Ownership and permissions" |
| 2 | History and export would return old blobs without the redaction that the read handler applies (`internal/server/dashboard_handlers.go:95-101`). The creator skips redaction (`internal/core/dashboards.go:217-223`) | "Revision history", "Redaction" |
| 3 | Delete reuses the edit predicate (`internal/server/dashboard_handlers.go:161`). A crafted save could drop a locked panel | "Ownership and permissions" |
| 4 | Summing rows that share a key is wrong for averages and percentiles | "Rows that share a key" |
| 5 | An open schema 1 browser tab cannot render schema 2 (`frontend/src/api/dashboards.ts:35-52`, `frontend/src/stores/dashboards.ts:372`) and its save would drop schema 2 state. An omitted `updated_at` turns conflict detection off (`pkg/models/dashboards.go:55-59`) | "Client schema contract" |
| 6 | Two saves in the same second both pass the conflict check (`internal/store/sqlite/queries.sql:1016`, `internal/core/dashboards.go:324`) | "Revisions" |
| 7 | Macros had no security contract. Raw SQL preparation forwards the timezone unvalidated (`internal/core/query.go:77`) | "Macros" |
| 8 | ClickHouse sends wide integers and decimals as strings (`internal/clickhouse/query.go:503-507`, `:567-583`). As JavaScript numbers they lose digits | "Exact numbers" |
| 9 | A real group named "Other", a NULL group and an empty group must stay distinct (`internal/clickhouse/logs.go:81-88`) | "Two query modes" |
| 10 | Sampling VictoriaLogs values to find number columns contradicts the server's own rule (`internal/victorialogs/querying.go:1362-1367`) | "Column type, kind and confidence" |
| 11 | Stat reducers were undefined for NULL, empty results, ties and unordered rows | "Stat semantics" |
| 12 | A calendar range is not a rolling duration. Date-only values, SQL-side timezones and VictoriaLogs bucket alignment were unspecified | "Time" |
| 13 | Nested collections did not say what happens to access rows on a move, which queries change, or how moves serialize | "Nested collections" |
| 14 | A LIMIT in the query, a cap from the server and a proven truncation are three different things. A clamp drops `OFFSET` (`internal/clickhouse/query_builder.go:336-338`) | "LIMIT rule" |
| 15 | The CLI and MCP request schemas cannot carry what macros need (`cli/crates/logchef-core/src/api/models.rs:182`, `internal/mcp/logs.go:15`, `:91`) | "CLI and MCP" |
| 16 | Phase 1 of the first draft changed behavior. A schema 1 table fetches 100 rows by default (`frontend/src/utils/dashboardRuntime.ts:7`) and the draft's default was 1,000 | "Upgrade from schema 1" |
| 17 | Time formats for tables and a slice threshold for pies were missing. Import losses were not listed | "Number and time formats", "Import losses" |
| 18 | Higher limits shipped without load bounds | "Load bounds" |

The check of finding 1 found one more fact. Saved queries have the same hole
in released code. `UserCanEditSavedQuery` grants edit to an owner or editor of
any shared collection that contains the query
(`internal/core/saved_queries.go:342-354`,
`internal/store/sqlite/queries.sql:956-967`). A user with access to the
query's source can create a collection, pin the query and edit it. This is
read from the code and was not run as an exploit. Phase 1 closes it.

## What the re-review showed

A second reviewer on another model family read the first revision against the
code. The reviewer confirmed that the blocker is closed and raised ten
findings. Each one has a decision, and the section named holds it.

| # | Finding | Decision | Section |
|---|---|---|---|
| 1 | A save with no client revision wrote against the revision that its transaction read. Two clients with stale copies could save one after the other, and the second replaced the first | Every update carries the client's `revision`. An update without it is a 428 | "Revisions" |
| 2 | A later phase adds state inside schema 2. A schema 2 browser tab from an earlier phase would drop that state on save | The stored blob records its features. Every write declares the features of its client. The server refuses a write that lacks one | "Client schema contract" |
| 3 | A schema 1 blob at the size limit grows past the limit when it is upgraded (`pkg/models/dashboards.go:66`, `:270`) | The schema 2 limit is 512 KiB from phase 3. The upgrade overhead has a bound | "Upgrade from schema 1" |
| 4 | One cell that is not a plain decimal turned a whole VictoriaLogs number column into doubles | Each cell parses by itself into an exact value | "Exact numbers" |
| 5 | The blob had no field for a confirmed column kind | `viz.columns` | "Column type, kind and confidence" |
| 6 | One visible tab can hold more frames than the frame budget, and only hidden tabs were released | One budget per dashboard view, kept for the panels nearest the viewport | "Load bounds" |
| 7 | The byte budget was not in the cache key (`internal/cache/dashcache.go:284-299`). The streaming reader has no byte check (`internal/clickhouse/query.go:273-289`) | The byte budget is a key field. The streaming reader applies it | "Load bounds", "Cache" |
| 8 | Creating or deleting a child collection could race a move | Both take the tree lock and check again under it | "Nested collections" |
| 9 | After the link migration an editor could lose edit with no way back. The creator's account may be gone, and an admin outside the collection cannot reach it (`internal/core/collections.go:124-126`) | A report of the affected links, and a conversion that a global admin can always do | "Ownership and permissions" |
| 10 | The old frontend has no "needs repair" state. It reads `panels_corrupt` in one list action only (`frontend/src/views/dashboards/DashboardsList.vue:216`) | The RFC states what an old tab really shows. A version check shows a reload banner | "Client schema contract" |

The reviewer also asked for a different order of phases. "Phase plan" has the
new order. The corpus files were readable for this revision, and "Corpus
mapping" has the result of the check against them.

## What the third review showed

A third reviewer on another model family read the second revision against
the code, phase by phase. The reviewer found phase 2 ready, phase 1 ready
after one edit, and phase 3 not ready. Two more findings concern later
phases. Each finding has a decision, and the section named holds it.

| # | Finding | Decision | Section |
|---|---|---|---|
| 1 | A link write authorizes in one step and changes the row in another. The removal is unconditional (`internal/store/sqlite/queries.sql:898-900`, `internal/store/postgres/queries.sql:894-896`) and the insert does nothing on a conflict. A participant's removal could race a conversion and delete a managed link | The statement that changes a link names the kinds that the caller may act on, and its row count decides the answer. `POST` never converts, and `PUT` owns conversions | "Ownership and permissions" |
| 2 | The dashboard list returns every blob. The schema 2 limit of 512 KiB makes that response five times larger | The list returns summaries from derived columns and reads no blob. It ships in phase 3, with the larger limit | "The dashboard list" |
| 3 | Schema 1 accepts the language alias `sql` (`pkg/models/query.go:56-69`, `pkg/models/dashboards.go:331`) and empty strings for `chart` and `bar_mode`. The upgrade did not say what they become | The upgrade writes one canonical form for each alias, empty value, null and unknown key | "Upgrade from schema 1" |
| 4 | A number string with few digits and a huge scale passed the bounds. `0.` then 10,000 zeros then `1` made every sum with it cost a power of ten with 10,001 digits | Bounds on the literal length, the normalized coefficient and the normalized scale. A cell outside them is NULL by itself | "Exact numbers" |
| 5 | Halving each Metabase card by itself makes cards 271 and 286 overlap on dashboard B | The import converts the boundaries that cards share. All 25 corpus placements pass validation | "Import losses" |

The reviewer also listed smaller items for phases 2 and 3. Phase 2 gets two
checklist items and no change to its contract. Phase 3 gets the header
transport in the frontend, the rule for a missing features header, and the
statement that the 428 and the features header concern dashboards only.
"Phase plan" has each item under its phase.

## Survey

The first table says what the other tools do. It comes from working knowledge
of their current releases. Nothing in it was re-tested for this RFC, so treat
every cell as inferred.

| Capability | Metabase | Grafana | Superset | Kibana (Lens) | Datadog |
|---|---|---|---|---|---|
| Computed stat with delta | Number card. Trend card compares the last two periods of a series | Stat panel with reducers, sparkline, percent change | Big Number, with trendline and period lag | Metric with a secondary metric and time shift | Query Value. Change widget compares with the previous period |
| Charts from query columns | Pick x and y columns of any SQL result | Needs a time column, or a table mapped to a bar chart | Charts sit on a dataset built from SQL | Charts over ES-QL results | Query-builder aggregations, no free SQL |
| Bar, line, area, pie, combo | All, plus a combo chart | All. Combo by series override | All, plus a mixed chart | All, by layers | All, with mixed display types |
| Granularity | SQL decides. The GUI builder offers "by day" | Auto interval, minimum interval, max data points | Time grain per chart | Auto or fixed interval | Auto rollup or fixed rollup |
| Tabs | Yes | Rows. Tabs in newer releases | Yes | Collapsible sections | Groups |
| Text and section headers | Text and heading cards | Text panel, rows | Markdown, headers, dividers | Markdown panel | Notes, groups |
| Dashboard variables | Filters wired to each card | Template variables, ad hoc filters | Native filters | Controls, filter pills | Template variables |
| Cross-filter and drill-down | Click behavior. Drill-through only for GUI questions | Data links, set up by hand | Cross-filters, drill to detail | Click to filter, drilldowns | Click to related logs |
| Number format and thresholds | Column format, table conditional format, goal line | Units, decimals, thresholds, value mappings | D3 formats, table conditional format | Field formatters, color by value | Units, conditional format |
| Folders and permissions | Nested collections, group permissions | Nested folders, RBAC | Owners and roles, no folders | Spaces, tags | Lists, team ownership |
| Nested folders | Yes | Yes | No | No | No |
| Sharing and embedding | Public links, signed embeds | Public dashboards, snapshots, iframes | Embedded SDK, permalinks | Share links, iframes, PDF reports | Public and invite-only links |
| Auto-refresh | Yes | Yes | Yes | Yes | Live by default |
| Caching | Policy per question or dashboard | Query cache in paid editions | Cache layer, timeout per chart | Elasticsearch request cache | Managed |
| Alerts from a panel | Alerts on questions | Alert rule from a panel query | Alerts and reports | Threshold rules from Discover | Monitor from a widget query |
| Export | CSV, XLSX, JSON per card. PDF | CSV from inspect. JSON model | CSV, image | CSV, PDF, PNG | CSV for some widgets. JSON |
| History | Revisions with revert | Versions with diff and restore | None built in | None built in | Version history |
| Keyboard and editing | Drag grid, few shortcuts | Many shortcuts, panel edit view | Drag layout builder | Drag grid, inline editor | Drag grid, shortcuts |

Redash is not in the table. Its model, which maps the x, y and series columns
of a query result, is the closest precedent for `result` mode (inferred).


The second table says what Logchef does today, what this RFC adds, and where
we can go past the other tools.

| Capability | Logchef today | Logchef v2 | Where we go past them |
|---|---|---|---|
| Computed stat with delta | Sum of histogram counts | Any numeric column, six reducers, comparison with the previous row, sparkline. Comparison with the previous period comes later | The tile shows the date of the row it reports, so a missing day cannot pass as today |
| Charts from query columns | None. Only `table` runs SQL as written | Roles x, y and series over a frame. The builder suggests them and the blob stores them | Paste SQL and get the right chart. Visualization edits never refetch |
| Bar, line, area, pie, combo | Line, area, bars over counts. Donut and bar list for breakdowns | Bar, line, area, pie, donut, bar list. Combo by a per-series mark comes later | One visualization set over ClickHouse SQL, LogsQL stats and LogchefQL counts |
| Granularity | Auto only | Auto or fixed per panel, `{{__interval}}` in SQL, a per-panel range | The panel shows the interval it used and whether it follows the time picker |
| Tabs | None | Tabs, each with its own grid, in the URL | A hidden tab runs no queries |
| Text and section headers | None | Markdown text panels | Rendered through text bindings, never `v-html` |
| Dashboard variables | None | Variables bound by name in all three query languages, optional blocks, URL state | One variable filters ClickHouse and VictoriaLogs panels alike |
| Cross-filter and drill-down | The title opens the panel in the Explorer | A click sets a variable. A click on a count series or bucket opens the matching logs. A brush zooms the range | Every count datapoint is one click from the raw logs, with no link setup |
| Number format and thresholds | None | Format object, thresholds, table rules, time formats | Exact numbers by default, including integers past 2^53 and decimals |
| Folders and permissions | One global list. Creator or admin edits | Dashboards in collections. A managed link delegates edit. A pin does not | Per-panel source checks stay, so sharing a dashboard never shares data |
| Nested folders | Collections are flat | Collections nest to a depth of 4. Access is set on the root | No per-folder permission matrix to reason about |
| Sharing and embedding | A link to the dashboard id. No state in the URL (none found in `frontend/src/views/dashboards/DashboardView.vue`) | Links for signed-in users carry tab, range and variables. Kiosk view | Public links are out of scope for v2 |
| Auto-refresh | Off, 30s, 1m, 5m per session (`frontend/src/views/dashboards/components/DashboardToolbar.vue:23-28`) | A saved default per dashboard | Refresh reads the shared cache, so wallboards do not multiply load |
| Caching | Shared TTL cache, window snapping, one fill per key (`internal/server/dashcache.go`) | The same cache covers result panels, comparisons and variables | Panels show the cache age |
| Alerts from a panel | Alerts are a separate feature | "Create alert" prefilled from a count panel and its threshold | |
| Export | None on dashboards | CSV per panel. Dashboard JSON | |
| History | Optimistic concurrency by timestamp (`internal/core/dashboards.go:322-326`) | An integer revision per save. 50 revisions with restore, redacted for each reader | |
| Keyboard and editing | Drag, resize, a drawer with live preview | Shortcuts, undo and redo, "Add to dashboard" from the Explorer | The Explorer and dashboards round-trip in both directions |

## Goals

- Every card on the two migrated dashboards renders from its original SQL
  with no rewrite. It keeps the same kind of visualization, the same rows in
  the same order, and the same numeric and time resolution. Series colors and
  Metabase date patterns are not reproduced. "Import losses" lists what an
  import drops.
- One panel model serves ClickHouse and VictoriaLogs.
- Existing dashboards keep working with no manual step and no data rewrite.
  Until someone edits a panel, it fetches and renders as it does in v2.6.1.
- A dashboard never becomes a new way to reach log data. Panel data still
  flows through the team-scoped query endpoints
  (`frontend/src/api/dashboards.ts:118-125`). The server authorizes the team
  and source before it looks in the cache
  (`internal/server/logs_handlers.go:182`, `:228-241`).
- Edit authority never widens by accident. Only someone who holds authority
  over a dashboard can delegate it.
- A client never destroys state that it does not understand. Of two saves
  that start from one revision, at most one wins.
- A number that the UI presents as exact is exact.

## Non-goals

- Public or unauthenticated links, and embedding in other sites.
- PDF or image export, scheduled reports.
- Collapsible sections, annotations, maps, heatmaps, gauges.
- Joins or math across panels, and panels that mix sources.
- Week and month buckets.
- Pixel or color fidelity with Metabase.
- A live-tail panel. It is a natural follow-up, since both backends already
  tail.

## Design

### A panel is a query plus a visualization

Today `type` selects both the fetch path and the renderer. Schema 2 splits
them along the domain.

```
Panel = QueryPanel | TextPanel
Query = CountQuery | ResultQuery
Viz   = Stat | Chart | Pie | TopList | Table
```

A frame is what any query returns to the client.

```ts
type ColumnKind = "time" | "date" | "number" | "category" | "bool" | "other";
type KindConfidence = "declared" | "confirmed" | "guessed";
type NumberRepr = "int" | "bigint" | "decimal" | "float";

interface FrameColumn {
  name: string;
  backendType: string | null;  // what the backend declared, such as "Nullable(Decimal(18, 4))"
  kind: ColumnKind;
  confidence: KindConfidence;
  repr: NumberRepr | null;     // number columns only
}

interface PanelFrame {
  columns: FrameColumn[];
  rows: Record<string, unknown>[];
  interval: string | null;     // bucket size, when known
  limit: { source: "statement" | "request" | "default" | "max"; rows: number } | null;
  truncation: "none" | "rows" | "bytes" | "unknown";
  notices: string[];
}
```

"Column type, kind and confidence" defines the column fields. "Exact numbers"
defines `repr`. "LIMIT rule" defines `limit` and `truncation`.

The store keeps one result per panel. `PanelState` loses its four optional
shapes (`frontend/src/stores/dashboards.ts:68-92`) and gains `frame`.

Two rules follow from the split.

- A visualization edit never sends a request. Role, kind, format and
  threshold changes re-render from the frame already held.
- The fetch signature covers only `team_id`, `source_id` and `query`. It
  replaces `execSignature` (`frontend/src/utils/dashboardRuntime.ts:89-99`).

### Column type, kind and confidence

A column carries three separate facts. The first draft merged them into one
field and sampled values to find it. Sampling fails on VictoriaLogs, where
`customer_id="00123"` and `revenue="42"` both parse as numbers. The server
already rejects value inference for column types, because a type that follows
the values changes between requests
(`internal/victorialogs/querying.go:1362-1367`).

Two words in this RFC are easy to confuse. A column has a kind, such as
`number` or `time`. A visualization has roles, such as `x` and `y`, and each
role names a column.

| Fact | Meaning | Source |
|---|---|---|
| Backend type | What the backend declared for the column | ClickHouse reports a type per column. VictoriaLogs reports none, so `backendType` is null. The type that the VictoriaLogs provider fills in is a guess from the field name (`internal/victorialogs/querying.go:1368-1392`), and the frame ignores it |
| Kind | What a visualization may use the column for | The entry for the column in `viz.columns`, when the panel stores one. Otherwise the backend type. A column with neither is text, which is the kind `category` |
| Confidence | How the kind was reached | `confirmed` from an entry in `viz.columns`. `declared` from a backend type, and for an untyped column that falls back to text. `guessed` exists only in the builder, for a suggestion that nobody has saved |

ClickHouse kinds come from the declared type, after the `Nullable` and
`LowCardinality` wrappers are removed.

| ClickHouse type | Kind | `repr` |
|---|---|---|
| `Date`, `Date32` | `date` | |
| `DateTime`, `DateTime64` | `time` | |
| `Int8` to `Int32`, `UInt8` to `UInt32` | `number` | `int` |
| `Int64`, `UInt64` and wider integers | `number` | `bigint` |
| `Decimal(P, S)` | `number` | `decimal` |
| `Float32`, `Float64` | `number` | `float` |
| `Bool` | `bool` | |
| `String`, `FixedString`, `Enum`, `UUID`, `IPv4`, `IPv6` | `category` | |
| Arrays, maps, tuples, JSON | `other` | |

**Stored kinds.** A result panel stores the kinds that its author confirmed
or changed, in `viz.columns`. The first revision said that the panel stores
them and gave the blob no field for them.

```json
"columns": {
  "revenue":     { "kind": "number",   "confidence": "confirmed" },
  "customer_id": { "kind": "category", "confidence": "confirmed" }
}
```

- The key is a column name. The value holds `kind`, which is one of the six
  kinds, and `confidence`, which is always `confirmed`. The frame computes
  `declared` and `guessed` when it is built, and the blob never stores them.
- An entry overrides the kind that the backend type gives. With an entry, an
  author turns a `UInt64` id into a label, or turns a ClickHouse `String`
  that holds numbers into a number.
- The builder writes an entry in two cases. In the first, the author changed
  the kind of a column. In the second, a role, a format or a rule uses a
  column that has no backend type, and the author saved the panel with the
  kind that the builder showed. That save is the confirmation. A column that
  nothing uses gets no entry.
- `viz.columns` sits under `viz` and not under `query`. A kind changes how a
  frame is read and not what is fetched, so a kind edit sends no request.
- `count` mode has fixed columns and forbids `viz.columns`.

**Suggestions.** VictoriaLogs returns every value as a string. The builder
samples the preview frame to suggest kinds, and marks each suggestion as
`guessed`.

- A column is suggested as `number` when every non-empty sampled value is a
  plain decimal literal with no leading zero, no leading plus sign and at most
  15 significant digits. `"00123"` is therefore a category.
- A column is suggested as `time` when every non-empty sampled value parses as
  RFC 3339.
- Every other column is suggested as `category`.

`customer_id="123"` and `revenue="42"` still look the same to these rules, so
the builder never applies a suggestion without the author. It shows a marker
next to each guessed kind, and the author can change any kind before saving.

**Reading a cell under a confirmed kind.** A confirmed kind applies to each
cell by itself.

| Confirmed kind | A cell is valid when it is |
|---|---|
| `number` | A JSON number, or a string that is a finite decimal or exponent literal. "Exact numbers" gives the grammar and its bounds |
| `time` | A timestamp from a typed backend column, or an RFC 3339 string |
| `date` | A date from a typed backend column, or a `YYYY-MM-DD` string |
| `bool` | A JSON boolean, or the string `true` or `false` |
| `category`, `other` | Anything. The cell shows as text |

A cell that is not valid for its confirmed kind becomes NULL. No other cell
in the column changes. The panel shows a notice with the number of such cells
for each column. For `number`, `time`, `date` and `bool`, an empty string is
NULL and is not counted, because VictoriaLogs sends an absent field as an
empty value (inferred from its row format, not re-tested).

**The renderer never infers.** It reads the kind of a column from
`viz.columns`, then from the backend type, and otherwise treats the column as
text. A saved panel therefore keeps its shape when later values look
different. A stored name can still disagree with a frame, and each case has
one outcome.

- A role names a column that the frame lacks. The panel shows an error that
  names the column.
- A key in `viz.columns` names a column that the frame lacks, and no role
  uses it. The panel renders and shows a notice that names the column. The
  server cannot know result columns at save time, so it accepts any column
  name.
- A `y` or `value` role names a column whose kind is not `number`. The panel
  shows an error with the column and its backend type.
- A cell is not valid for the confirmed kind of its column. The cell is NULL,
  and the notice above counts it.

A declared type can also mislead. `customer_id UInt64` is a number to
ClickHouse and a label to the author. The author sets its kind to `category`
in the builder. The entry in `viz.columns` then makes every visualization
treat the column as a label.

### Two query modes

`count` counts matching log rows over time. It is today's histogram path and
it stays, for four reasons. LogchefQL has no aggregation syntax. VictoriaLogs
has no SQL, and its hits API is the portable primitive
(`internal/victorialogs/querying.go:339-357`). The path already folds series
beyond the top 10 into a remainder (`internal/clickhouse/logs.go:71-74`). The
client already fills empty buckets.

The client adapts the histogram response into a frame with fixed columns:
`time`, `count`, and `series` when `group_by` is set. A LogsQL count panel
counts the filter part of the query, because the hits API ignores pipes. That
is today's behavior.

The histogram response keeps the identity of a group apart from its text.
`is_other` marks the synthetic remainder and `is_null` marks a SQL NULL group
(`internal/clickhouse/logs.go:77-88`). The chart code already keys series on
those flags (`frontend/src/utils/histogram-chart.ts:321-337`). The frame keeps
the separation. A `series` cell in a count frame is a key, not a label.

```ts
type SeriesKey =
  | { kind: "value"; value: string }  // a real group, including "" and "Other"
  | { kind: "null" }                  // the SQL NULL group
  | { kind: "other" };                // the synthetic remainder
```

| Key | Label | Filter that a click adds |
|---|---|---|
| `value` | The value, or `(empty)` for `""` | `field = "<value>"`. This also holds for `""` and for a real group named "Other" |
| `null` | `(null)` | A null test on the field, where the query language has one. Otherwise the click adds no series filter |
| `other` | `Other` | None. No single filter selects the remainder |

Grouping, colors and legends use the key. Only the label is display text. A
real group named "Other", the NULL group, the empty group and the remainder
are four different series. VictoriaLogs marks its catch-all series
structurally too (`internal/victorialogs/querying.go:410-420`), and it has no
NULL group.

A result frame has no synthetic rows. Its cells are the values that the query
returned, and JSON null and `""` stay distinct.

`result` runs the query as written and returns its rows.

| Language | Endpoint | Notes |
|---|---|---|
| `clickhouse-sql` | `POST /logs/query` | Any single SELECT. The ClickHouse user's grants are the boundary, as in the Explorer |
| `logsql` | `POST /logs/query` | Stats pipes pass through untouched (`internal/victorialogs/querying.go:1268-1281`). Values arrive as strings |
| `logchefql` | `POST /logchefql/query` | Returns log rows. Useful for tables |

A VictoriaLogs result panel looks like this:

```
level:error | stats by (_time:1h, service) count() as errors
```

`count` accepts all three languages. A `clickhouse-sql` count panel exists
only so that schema 1 blobs keep working. The builder offers `count` for
LogchefQL and LogsQL, and `result` for all three.

The builder offers ClickHouse SQL from now on. The comment that hides it gives
no reason, and the deleted editor it points to says only "intentionally never
offered here". The likely reason (inferred) is gap 1: under schema 1 a SQL
panel could only be counted, and grouped SQL failed in the histogram wrapper.
The API already accepts `clickhouse-sql` panels, and the Explorer already runs
the same SQL through the same endpoint, so the builder adds no new access.

### Visualizations map columns to roles

| Viz | Roles | Options |
|---|---|---|
| `stat` | `value`, `order_by` | `reduce`, `compare`, `sparkline`, `color_mode`, `format`, `thresholds` |
| `chart` | `x`, `y[]`, `series` | `mark`, `stack`, `x_scale`, `orientation`, `show_values`, `legend`, `format`, `thresholds`, `click`. Later phases add `combine` and `overrides` |
| `pie` | `label`, `value` | `donut`, `min_slice_percent`, `format`, `click` |
| `toplist` | `label`, `value` | `max_rows`, `format`, `click` |
| `table` | none | `visible_columns`, `column_formats`, `rules`, `click` |

Every visualization in `result` mode also takes `columns`, the stored column
kinds from "Column type, kind and confidence". The list of columns that a
table shows is `visible_columns`. Schema 1 calls that list `options.columns`,
and the name `columns` now belongs to the kinds.

In `count` mode the roles are fixed (`x = time`, `y = [count]`,
`series = series`, `value = count`, `label = series`) and the blob must not
store them.

In `result` mode the blob stores the roles. `stat` needs `value`. `chart`
needs `x` and at least one `y`. `pie` and `toplist` need `label` and `value`.
`order_by` and `series` are optional, and an absent `series` means one series
per `y` column.

The builder fills the roles from the first preview frame, and the author can
change them before saving.

- `stat`: `value` is the first number column. `order_by` is the first `time`
  or `date` column, if any.
- `chart`: `x` is the first `time` or `date` column, else the first category
  column. `y` is every number column other than `x`. When the frame has
  exactly one category column next to a time column and one number column,
  that category column is `series`.
- `pie` and `toplist`: `label` is the first column that is not a number.
  `value` is the first number column.

For a new result panel the builder also suggests the visualization. One row
with one number column gives `stat`. A `time` or `date` column plus a number
column gives a line `chart`. One category column plus number columns with at
most 50 rows gives a bar `chart`. Anything else gives `table`.

`chart` draws time or category axes from one component.

- `x_scale` is `auto` (time for a `time` or `date` column, category
  otherwise), `time` or `category`. A category axis keeps the row order of the
  query, also when the `x` column holds dates. A time axis sorts by time.
- `mark` is `line`, `area` or `bar`. `stack` is `none` or `stacked`, and a bar
  chart with `none` draws grouped bars.
- `y` keeps its stored order. The order sets the stacking order and the
  legend order.
- A chart draws at most 50 series. Beyond 50 it draws the first 50 in query
  order and shows a notice such as "50 of 132 series shown". The author
  controls which series those are with `ORDER BY`.

`pie` draws slices in query order, at most 50. `min_slice_percent` folds every
slice below that share of the total into one remainder slice. It is absent by
default, so nothing folds unless the author asks. A pie shows parts of a
whole, so its values must be additive, and a sum of small slices is sound
there. A pie drops NULL values with a notice and shows an error for a negative
value.

`toplist` shows the first `max_rows` rows in query order and a notice such as
"20 of 85 rows shown".

### Rows that share a key

The key is `x` plus `series` for a chart, and `label` for `pie` and `toplist`.

Count frames sum rows that share a key. Counts are additive, so the sum is
right, and the remainder series is itself a sum. A count frame therefore feeds
a pie with no special case.

Result frames must have unique keys. The first draft summed duplicates, and
that is wrong for most measures. Two rows with averages 10 and 20 over
populations of 1 and 9 would show 30, and the true average is 19. Percentiles
cannot be combined at all. A fold of the smallest series into "Other" has the
same defect, and "smallest" has no meaning across `y` columns with different
units.

- A result frame with duplicate keys renders a panel error. The error gives
  the number of duplicate keys and the first one, and tells the author to
  group the query by those columns.
- `viz.combine` lets the author pick a rule: `sum`, `min` or `max`. It is
  absent by default, and it ships after restoration. It offers no `avg`,
  because the average of rows is not the average of what the rows summarize.
- A result frame never gets an automatic "Other".

### Stat semantics

`stat` reduces one column to one value. The reducers are `last`, `first`,
`sum`, `avg`, `min` and `max`. The default is `sum` in `count` mode and `last`
in `result` mode.

**Order.** `order_by` names the column that orders the rows. The client sorts
ascending by that column before it reduces, and the sort is stable. It
compares `date` values as calendar dates, `time` values as instants, numbers
by value and everything else as text. A row whose `order_by` cell is NULL or
does not parse is left out of the ordered sequence, and the tile shows a
notice with the number of such rows. The ordered sequence feeds `first`,
`last`, the sparkline and the comparison. `sum`, `avg`, `min` and `max` use
every row.

Without `order_by` the rows keep the order of the result. A query with no
`ORDER BY` returns rows in no defined order, so `first` and `last` then mean
something only for a one-row result. Validation requires `order_by` for
`compare.mode = "previous_row"` and for `sparkline`. The builder warns when
`reduce` is `first` or `last`, the preview has more than one row, and
`order_by` is not set.

**Ties.** The selected row can share its `order_by` value with another row.
The choice between them then follows the result order, which the query may
not define. The tile shows a notice with the number of rows that share the
value. A trend tile expects one row per day, so a tie usually means that the
query lacks a `GROUP BY`.

**NULL and empty results.**

| Case | Outcome |
|---|---|
| No rows, `count` mode | 0. No matching log rows is a real count |
| No rows, `result` mode | "No data" |
| `sum`, `avg`, `min`, `max` | Ignore NULL values. "No data" when every value is NULL |
| `first`, `last` | Report the selected row as it is. A NULL value shows as an empty value next to the row's date. The reducer does not skip back to an older row, because that presents an old value as current |
| NaN and infinity | ClickHouse sends these as NULL (`internal/clickhouse/query.go:545-550`), and they count as NULL |

**The row date.** With `order_by` set and `reduce` equal to `first` or `last`,
the tile shows the `order_by` value of the selected row, such as "12 Nov". A
"today" tile whose query returned no row for today then visibly reports
yesterday.

**Comparison.** `compare.mode` is `none` or `previous_row`. A later phase adds
`previous_period`.

- `previous_row` needs `reduce` to be `last` or `first`, and it needs
  `order_by`. This is the Metabase trend tile.
- The neighbor is the row next to the selected row in the ordered sequence.
  For `last` it is the row before. For `first` it is the row after.
- The neighbor is a row, not a calendar period. If yesterday has no row, the
  neighbor is the latest earlier row. The tile therefore shows both dates,
  such as "12 Nov vs 9 Nov".
- With fewer than two rows in the sequence, the tile shows the value and no
  comparison.
- A NULL on either side shows no delta. A previous value of 0 shows the
  absolute change and no percent.
- `compare.good` is `up`, `down` or `neutral`, and colors the delta. The tile
  shows the absolute and the percent change.

The corpus needs both rules. Cards 268, 270, 274, 275 and 302 have no
`ORDER BY`, so their panels set `order_by` to the date column. Card 584
returns only the latest date, so its panel sets `compare.mode` to `none`.

### Exact numbers

ClickHouse results do not always arrive as JSON numbers. The server sends an
integer as a JSON number only inside ±(2^53 - 1), and as a decimal string
beyond that (`internal/clickhouse/query.go:501-507`, `:590-602`). It sends
every `Decimal` as a string (`internal/clickhouse/query.go:567-583`). The
choice is made per value, so one `UInt64` column can mix numbers and strings.
A client that passes these values through `Number` turns
`"9007199254740993"` into 9007199254740992. VictoriaLogs sends every value as
a string.

The frame keeps the representation of a number (`repr`) apart from the kind of
its column.

| `repr` | Values on the wire | Arithmetic |
|---|---|---|
| `int` | JSON numbers | Exact, as integers |
| `bigint` | JSON numbers or decimal strings | Exact, in `BigInt` |
| `decimal` | Decimal strings | Exact, as a `BigInt` coefficient with a decimal scale. A ClickHouse `Decimal(P, S)` column has one scale. A string column that the author confirmed as a number has a scale per cell |
| `float` | JSON numbers | IEEE 754 double, with no claim of exactness |

**Number strings.** VictoriaLogs sends every value as a string, and an author
can confirm a ClickHouse `String` column as a number. Such a column has the
`repr` `decimal`, and the frame parses each cell by itself.

- A cell is valid when it is a finite decimal or exponent literal. That is an
  optional sign, digits with an optional fraction, and an optional exponent.
  `42`, `-0.50`, `1.5e6` and `2E-3` are valid. `NaN`, `Inf`, `0x1f`, `1,000`
  and `12ms` are not.
- A valid cell becomes an exact coefficient and scale. The value is the
  coefficient divided by ten to the power of the scale. `1.5e6` is the
  coefficient 15 with the scale -5, which is exactly 1,500,000. A valid cell
  never passes through a double.
- The parser normalizes the value. It removes the leading zeros of the
  digits, removes the trailing zeros, and moves the trailing zeros into the
  scale. `1500`, `1.5e3` and `1500.00` all become the coefficient 15 with the
  scale -2. Zero is the coefficient 0 with the scale 0.
- Four bounds apply, and a cell outside any of them is not valid.
  - **Length.** The literal has at most 512 characters. This check runs
    before anything else reads the cell, so a cell of any size costs one
    length comparison.
  - **Exponent digits.** The exponent part has at most 4 digits. The parser
    then reads it as a small integer.
  - **Coefficient.** The normalized coefficient has at most 100 digits. That
    covers the widest ClickHouse decimal, which has 76 digits.
  - **Scale.** The normalized scale lies between -400 and 400.
- The scale bound is 400 for two reasons. The shortest literal that
  round-trips a double has at most 17 significant digits and a decimal
  exponent between -324 and 308, so its normalized scale lies between -308
  and 340. A ClickHouse decimal has a scale of at most 76. 400 covers both
  with room.
- The length bound follows from the other two. The longest plain literal of
  a value inside them has 501 characters: a sign, 100 digits and 400 zeros.
  512 leaves room for it. No value inside the bounds needs an exponent of
  more than 3 digits.
- The bounds cap the cost of arithmetic. Two valid values differ by at most
  800 in scale, so bringing them to one scale multiplies a coefficient by at
  most ten to the power of 800. A sum of `n` cells has at most 900 digits
  plus the digits of `n`. The result of arithmetic is not checked against
  the bounds again.
- The second revision bounded the significant digits and the written
  exponent only. The literal `0.`, then 10,000 zeros, then `1` has one
  significant digit and no exponent, so it passed. Its scale is 10,001, and
  adding it to a whole number multiplies by ten to the power of 10,001. The
  old text also did not say whether the zeros of a `1` followed by 10,000
  zeros count as significant digits. Normalization answers that. They move
  into the scale, and the scale is then -10,000. Now both literals fail the
  length bound, and a shorter literal of either shape fails the scale bound.
- A value from a typed ClickHouse column never reaches a bound. `Int256` has
  at most 78 digits, and a `Decimal` has at most 76 digits and a scale of at
  most 76.
- A threshold value or a table rule value that is a decimal string follows
  the same grammar and the same bounds. The validator rejects one that is
  outside them.
- A cell that is not valid becomes NULL, and the panel notice counts it. A
  cell outside a bound is one such cell. It becomes NULL by itself, and no
  other cell in the column changes. The first revision turned the whole
  column into doubles when one cell failed. One bad row then took exactness
  from every other row. Now a valid cell never loses exactness because of
  another row.

The rules below hold for every exact representation.

- The frame parses each value once, into an exact value or a double. No code
  path calls `Number` on a `bigint` or `decimal` value.
- Tables and tooltips show the exact value. The formatter inserts grouping
  separators into the digit string. `decimals` rounds half away from zero.
- `sum`, `min`, `max`, `first` and `last` are exact for `int`, `bigint` and
  `decimal`. `avg` divides the exact sum by the count and keeps 10 fraction
  digits, the most that a format can show.
- The absolute change of a comparison is exact. The percent change is a ratio,
  so the client computes it as a double from the exact values.
- Thresholds and table rules compare exactly. A threshold value is a JSON
  number or a decimal string, so a step can sit above 2^53.
- `format.scale` multiplies in decimal arithmetic for the exact
  representations.
- Sorts compare exact values.
- Chart coordinates are doubles, because the chart library plots doubles. A
  value is approximate when the conversion to a double and back changes it.
  When a chart plots such a value, the axis shows an "approximate" marker. The
  tooltip still reads the exact value from the frame.
- Compact notation is an approximation by definition. Its tooltip shows the
  exact value.

`BigInt` is part of the language, so this needs no dependency. One module,
`frontend/src/utils/exactNumber.ts`, owns parsing, arithmetic, comparison and
formatting.

### Number and time formats

One number format serves stat values, chart axes, chart value labels and table
columns.

```json
{
  "notation": "auto",
  "decimals": 2,
  "grouping": true,
  "scale": 1,
  "unit": "none",
  "prefix": "",
  "suffix": " M"
}
```

- `notation`: `auto`, `standard` or `compact`. `auto` shows the exact grouped
  number when it fits the tile at the smallest font size, and the compact form
  otherwise. The tooltip always shows the exact value. `auto` is the default,
  so 237,497 no longer renders as 237.5K.
- `decimals`: 0 to 10. When absent, up to 2 fraction digits with trailing
  zeros removed.
- `grouping`: thousands separators in the viewer's locale.
- `scale`: a multiplier applied before display.
- `unit`: `none`, `percent`, `bytes`, `duration_ms` or `duration_s`.
- `prefix` and `suffix`: up to 16 characters each.

A time format serves table columns with the kind `time` or `date`, and the row
date on a stat.

```json
{ "kind": "time", "date": "iso", "time": "seconds" }
```

- `date`: `iso` (`2026-11-12`), `medium` (`12 Nov 2026`, in the viewer's
  locale) or `none`.
- `time`: `none`, `minutes` (`14:05`), `seconds` (`14:05:09`) or `millis`
  (`14:05:09.123`), on a 24-hour clock.
- A `time` column shows in the viewer's timezone. A `date` column shows the
  calendar date that the query returned and never shifts.
- The default is `iso` with `seconds` for a `time` column, and `iso` with
  `none` for a `date` column.

`column_formats` maps a column name to a format. `kind` is `number` or `time`,
and an absent `kind` means `number`. Cards 286, 287 and 619 set a timestamp
with seconds. They get it from this format.

Thresholds are ordered steps. The first step has no value and is the base.

```json
[
  { "value": null, "color": "green" },
  { "value": 100, "color": "amber" },
  { "value": 500, "color": "red" }
]
```

A value takes the color of the last step whose `value` it reaches. Colors are
palette names (`green`, `amber`, `red`, `blue`, `purple`, `gray`) that resolve
to theme tokens, so they hold in light and dark mode. On a stat,
`color_mode` applies the color to the value or to the tile background. On a
chart, each step with a value draws a reference line.

Tables take rules, which also cover text matches.

```json
{ "columns": ["details"], "op": "contains", "value": "retry", "color": "red", "scope": "cell" }
```

`op` is one of `>`, `>=`, `<`, `<=`, `=`, `!=`, `contains`, `not_contains`,
`empty`, `not_empty`. `scope` is `cell` or `row`. Cards 287 and 619 need the
`row` scope. Card 320 needs the `cell` scope, and card 182 needs it together
with a subset of visible columns.

### Import losses

The corpus came from Metabase. These parts of a card have no place in the
panel model, and an import drops or changes them. This revision checked the
list against the corpus files.

- Series colors. Charts take colors from the theme. The bar charts 273 and
  278 and the area chart 282 set a hex color for a series that their query
  returns, and they lose it.
- Rule colors. A table rule takes a palette name. The four tables with rules
  (182, 287, 320 and 619) use one hex color. The import maps it to the nearest
  palette name, which is `red`.
- Axis titles. Cards 277 and 278 set a title for the x axis. A chart has no
  option for an axis title.
- Date styles. Cards 286, 287 and 619 set only the time precision, and that
  carries over. They set no date style, so Metabase shows its default one,
  and that does not carry over. A column gets the nearest time format above.
- Grid positions. Metabase lays cards on 24 columns and Logchef on 12. The
  import converts the column boundaries that the cards of a tab share.
  "Grid conversion" below gives the rule. On the first tab of dashboard B,
  card 294 loses half a column of width, card 301 moves left by half a
  column, and card 271 moves left by half a column and gains half a column
  of width. Every other card of the corpus keeps exactly half its Metabase
  position and width.
- Stale settings. Most cards carry settings that name a column the query does
  not return, or that belong to a display the card no longer uses. Card 288
  names a pie dimension and a pie metric that its result lacks. Card 304
  colors a series that its result lacks. Card 286 formats a column that its
  result lacks. The import drops these settings.

One card needs care in the other direction. Card 619 sits on dashboard B with
its own settings, and they replace the rule of the saved question. The import
reads the settings of the dashboard card before those of the question.

An import reports every dropped or changed setting per card, so the loss is
visible when it happens.

**Grid conversion.** The second revision halved the position and the width
of each card and rounded both. Two cards that touch can then round in
different directions. On the first tab of dashboard B, card 271 starts at
column 13 with a width of 5, and card 286 starts at column 18, where card 271
ends. Halving gives card 271 the column 6.5 and the width 2.5. Rounded up,
that is column 7 and width 3, so the card ends at column 10. Card 286 becomes
column 9 with width 3. The two cards overlap, and the validator rejects the
tab.

The import converts the boundaries that the cards share, and not the cards.
It runs once per tab, and a dashboard with no tabs counts as one tab.

1. **Columns.** Take the left edge `x` and the right edge `x + w` of every
   card on the tab. Map each edge `e` to the whole number nearest to `e / 2`.
   A tie goes down, so edge 13 becomes 6. The new `x` of a card is its
   converted left edge. The new `w` is its converted right edge minus its
   converted left edge. The width is derived from the edges and never rounded
   by itself.
2. **Rows.** The same conversion runs on the top edge `y` and the bottom edge
   `y + h`, with the factor 2/3 in place of 1/2.
3. **Minimum and maximum size.** A card narrower than 2 columns gets the
   width 2 by moving its right edge. If the card then ends beyond column 12,
   it shifts left until it ends at column 12. A card with a height below 1
   gets the height 1. A card taller than 12 rows is cut to 12.
4. **Overlap.** The import sorts the cards by converted `y`, then by
   converted `x`, then by the id of the dashboard card. It places them in
   that order. A card that overlaps a card already placed moves down to the
   bottom edge of the lowest card that it overlaps. The check repeats until
   the card overlaps nothing.
5. **Report.** For each card the import reports the Metabase rectangle and
   the Logchef rectangle. It names each part of steps 3 and 4 that changed
   the card: widened, shifted, heightened, cut or pushed down.

Three properties follow.

- The mapping of an edge never reverses the order of two edges. Two cards
  that share an edge in Metabase share the converted edge, so steps 1 and 2
  create no overlap. Only step 3 can create one, and step 4 repairs it.
- Edge 24 becomes 12, so no card ends beyond the right border after step 1.
- The same export always gives the same layout. No step depends on the order
  of the cards in the export.

The row factor is a judgment, and "Open questions" lists it as number 18. A
Logchef row is 60 px tall with a 12 px gap
(`frontend/src/views/dashboards/DashboardView.vue:64-66`). A Metabase row is
shorter (inferred, not measured). The version 1 dashboards that were laid out
by hand from the corpus keep between two thirds and all of each Metabase
height. With 2/3 the tallest corpus card, which has 7 rows in Metabase, gets
5 rows.

This revision ran the rule against the 25 placements in `dash.json`. All 25
pass the layout rules of "Validation", and no card needs step 3 or step 4.
Rectangles are `x, y, w, h`.

| Where | Card | Metabase | Logchef | Column change |
|---|---|---|---|---|
| A | 227 | 0, 0, 4, 3 | 0, 0, 2, 2 | |
| A | 235 | 4, 0, 4, 3 | 2, 0, 2, 2 | |
| A | 234 | 8, 0, 4, 3 | 4, 0, 2, 2 | |
| A | 226 | 12, 0, 4, 3 | 6, 0, 2, 2 | |
| B, first tab | 268 | 0, 0, 4, 3 | 0, 0, 2, 2 | |
| B, first tab | 274 | 4, 0, 4, 3 | 2, 0, 2, 2 | |
| B, first tab | 302 | 8, 0, 4, 3 | 4, 0, 2, 2 | |
| B, first tab | 270 | 12, 0, 4, 3 | 6, 0, 2, 2 | |
| B, first tab | 275 | 16, 0, 4, 3 | 8, 0, 2, 2 | |
| B, first tab | 584 | 20, 0, 4, 3 | 10, 0, 2, 2 | |
| B, first tab | 619 | 0, 3, 24, 4 | 0, 2, 12, 3 | |
| B, first tab | 320 | 0, 7, 8, 5 | 0, 5, 4, 3 | |
| B, first tab | 182 | 8, 7, 16, 5 | 4, 5, 8, 3 | |
| B, first tab | 287 | 0, 12, 24, 7 | 0, 8, 12, 5 | |
| B, first tab | 294 | 0, 19, 7, 5 | 0, 13, 3, 3 | Width 3.5 becomes 3 |
| B, first tab | 301 | 7, 19, 6, 5 | 3, 13, 3, 3 | Column 3.5 becomes 3 |
| B, first tab | 271 | 13, 19, 5, 5 | 6, 13, 3, 3 | Column 6.5 becomes 6. Width 2.5 becomes 3 |
| B, first tab | 286 | 18, 19, 6, 5 | 9, 13, 3, 3 | |
| B, second tab | 273 | 0, 0, 8, 5 | 0, 0, 4, 3 | |
| B, second tab | 303 | 8, 0, 8, 5 | 4, 0, 4, 3 | |
| B, second tab | 293 | 16, 0, 8, 5 | 8, 0, 4, 3 | |
| B, second tab | 277 | 0, 5, 8, 5 | 0, 3, 4, 4 | |
| B, second tab | 304 | 8, 5, 8, 5 | 4, 3, 4, 4 | |
| B, second tab | 282 | 16, 5, 8, 5 | 8, 3, 4, 4 | |
| B, second tab | 278 | 0, 10, 8, 6 | 0, 7, 4, 4 | |

Cards 294, 301, 271 and 286 now fill one row with four cards of width 3, and
none of them overlaps. Every converted width is 2, 3, 4, 8 or 12 and every
height is at most 5, so the corpus layout needs no size that phase 9 adds.

### Time

The first draft treated "the last 10 days" as one idea. It is two, and they
give different windows.

| Kind | Example | Window |
|---|---|---|
| Rolling duration | `10d` | Exactly 240 hours that end now. At noon the window starts at noon ten days ago, so the first day is half missing |
| Calendar range | `today`, `yesterday`, `today-10d` | Whole days. `today-10d` runs from the start of the day ten days before today until now. It covers today and the 10 dates before it |

The corpus uses calendar ranges, written in SQL. Cards 273, 303, 278, 293,
277, 282 and 304 filter with `date(ts) >= toDate(now()) - 10`. A rolling
`10d` is not that window.

- Restoration keeps the corpus SQL byte for byte. No phase rewrites a corpus
  query into macros. Those panels are fixed-window panels. Their SQL sets the
  window, and the time picker does not move it.
- The range grammar gains the calendar form `today-<n>d`.
  `default_time_range` and `query.range` accept it next to the existing forms
  (`pkg/models/dashboards.go:183-185`). The client already has a calendar kind
  for `today` and `yesterday` (`frontend/src/utils/timeWindow.ts:24`, `:73`)
  and never snaps it to a cache bucket
  (`frontend/src/utils/dashboardRuntime.ts:57-59`). `today-<n>d` joins that
  kind.
- A chart with day buckets needs a calendar range. The builder says so when a
  panel combines a `24h` interval with a rolling range.

**Date-only values and instants.** The column kinds `date` and `time` keep
these apart.

- A `date` value is a calendar date and has no timezone. The UI shows it as
  the query returned it and places it on a day axis with no conversion.
- A `time` value is an instant. The UI shows it in the viewer's timezone.
- A ClickHouse `Date` column reaches the client as a timestamp at midnight,
  because the driver scans it into a time value and the server encodes that as
  RFC 3339 (inferred from the scan path, not re-tested). The frame reads the
  calendar date from it by declared type and never formats it in the viewer's
  zone. A test with one viewer at UTC-8 and one at UTC+13 holds this rule.
- The `day` variable type renders a date-only literal.
- `{{__from}}` and `{{__to}}` are instants. A query compares them with a
  `Date` column as `event_date >= toDate({{__from}}, {{__timezone}})`.

**Whose timezone.** A window or a bucket takes its timezone from the place
that defines it.

| Defined by | Timezone | Same for every viewer |
|---|---|---|
| SQL functions in the query text, such as `today()`, `now()`, `toDate(ts)` and `date(ts)` | The ClickHouse server, or the column | Yes |
| The dashboard range, `query.range`, `{{__from}}`, `{{__to}}`, `{{__time_filter}}` | The viewer's, sent as `timezone` | No. "today" differs for two viewers in different zones |
| Count mode buckets, and `{{__interval}}` used with `{{__timezone}}` | The viewer's | No |

The corpus panels are on the first row. Every viewer sees the same days, in
the server's zone, as in Metabase. A panel chip tells the viewer which row
applies. A fixed-window panel shows "fixed window". A bound panel shows its
range and zone. The docs tell authors not to mix the rows in one query, for
example a `toDate(ts)` bucket inside a viewer-zone range.

**Bucket alignment.** ClickHouse aligns buckets with
`toStartOfInterval(ts, ..., '<zone>')`, which follows daylight-saving changes
(`internal/clickhouse/logs.go:185-212`). VictoriaLogs takes one fixed offset,
computed at the start of the range
(`internal/victorialogs/querying.go:1210-1234`). After a daylight-saving
change inside the range, its buckets sit one hour off local midnight. The
result path sends no offset at all
(`internal/victorialogs/querying.go:110-129`), so a `stats by (_time:1d)`
bucket is a UTC day.

- A count panel on VictoriaLogs keeps the fixed offset. When the range crosses
  an offset change in the viewer's zone and the bucket is longer than one
  hour, the panel shows a notice that names the change.
- A result panel on VictoriaLogs buckets in UTC unless the query writes an
  offset. `{{__tz_offset}}` renders the same fixed offset as a duration, for
  use as `stats by (_time:1d offset {{__tz_offset}})`. Whether the pinned
  VictoriaLogs version accepts that form is inferred, and phase 14 checks it
  first. If it does not, the macro is dropped and the panel chip says "UTC
  buckets".
- Tests run a range that crosses a daylight-saving change on both backends.

**Time binding.** A query is time-bound when the dashboard range reaches it.

| Query | Time-bound |
|---|---|
| LogchefQL, either mode | Always. The compiler adds the range |
| LogsQL, either mode | Always. VictoriaLogs takes `start` and `end` from the request |
| ClickHouse SQL, either mode | Only when the text uses `{{__time_filter}}`, or `{{__from}}` and `{{__to}}` |

A ClickHouse SQL panel that is not time-bound shows the "fixed window" chip.
The viewer can then see that the time picker does not change it.

### Macros

Built-in macros use a reserved `__` prefix. The server renders them from the
request's `start_time`, `end_time`, `timezone` and a new `interval` field.

| Macro | ClickHouse SQL | LogsQL |
|---|---|---|
| `{{__from}}`, `{{__to}}` | `toDateTime64('2026-11-05 18:30:00.000', 3, 'UTC')` | `"2026-11-05T18:30:00Z"` |
| `{{__time_filter}}` | `` `ts` >= <from> AND `ts` < <to> ``, on the source's timestamp column | Rejected. The request range already applies |
| `{{__interval}}` | `INTERVAL 1 DAY` | `1d` |
| `{{__interval_s}}` | `86400` | `86400` |
| `{{__timezone}}` | `'Asia/Kolkata'` | Rejected |
| `{{__tz_offset}}` | Rejected | `19800s` |

The `toDateTime64(..., 'UTC')` form matches what the server already writes for
field values (`internal/clickhouse/field_values.go:120`). The LogsQL forms
need a check against the pinned VictoriaLogs version (inferred). LogchefQL
rejects all macros, because its compiler always applies the range.

A macro puts server-built text into a query, so it follows a fixed contract.

1. **One pass.** The existing dialect-aware scanner walks the query once
   (`internal/template/variables.go:105-135`). It already skips comments and
   tracks strings, quoted identifiers and heredocs. Macros expand inside that
   walk. The scanner appends each expansion to the output and never scans the
   output again. A variable whose value is the text `{{__to}}` stays that text
   inside a string literal.
2. **Expressions only.** A macro inside a string, a quoted identifier, a
   heredoc or a raw string is an error. A macro inside a comment is left as
   it is, like all text in a comment.
3. **A closed set of names.** A request variable whose name is a built-in name
   is a 400, so a caller cannot replace a macro. New saved-query and dashboard
   variables may not start with `__`, which keeps the prefix free for later
   macros. A stored variable that already starts with `__` and is not a
   built-in name keeps working.
4. **Typed inputs.** Every macro renders from a parsed value, never from
   request text.
   - The server parses `start_time` and `end_time` as RFC 3339
     (`internal/core/query.go:84-91`) and renders them in UTC with a fixed
     layout.
   - The server validates `timezone` for every endpoint before a provider
     sees it. Raw SQL preparation forwards it unvalidated today
     (`internal/core/query.go:77`), while the histogram validates it
     (`internal/clickhouse/logs.go:111-117`). The check moves to
     `internal/core`. It is the character rule of `ValidateTimezone`
     (`internal/clickhouse/validate.go:47-58`) plus `time.LoadLocation`. The
     macro renders the name through the dialect's string formatter.
   - `interval` is one of the histogram windows
     (`internal/clickhouse/logs.go:19-41`). A table maps each window to its
     ClickHouse and LogsQL form. No free-form duration reaches the query.
   - The timestamp column in `{{__time_filter}}` comes from the source. The
     server quotes it as an identifier, as the histogram does
     (`internal/clickhouse/logs.go:186`).
5. **Missing inputs fail.** A query that uses a time macro with no range in
   the request is a 400. So is `{{__interval}}` with no `interval`.
6. **Optional blocks come from the template.** `[[ ... ]]` resolves before
   the walk, on the template text only
   (`internal/template/variables.go:99-102`). A macro counts as provided. A
   value never opens or closes a block.

Tests cover each macro in both dialects, each quoted context, a comment, a
variable value that holds macro text or `[[`, a request variable named
`__from`, a timezone with a quote or a space, and an interval outside the
set.

The macros live in `internal/template` and `internal/core/query.go`, which
HTTP and MCP share (`internal/core/query.go:3-5`). So the Explorer's SQL mode
gets them too, and a panel opened in the Explorer runs unchanged. The CLI and
MCP need request fields before they can send the inputs. "CLI and MCP" lists
them.

`query.interval` is `auto` or one of the windows above.

- `auto` keeps today's rule of about 100 buckets.
- A fixed interval that would give more than 1,000 buckets is raised to the
  next allowed window, and the panel shows the interval it used.
- In `count` mode the interval is the histogram `window`. In `result` mode it
  feeds `{{__interval}}`. The client resolves `auto` to a window before it
  sends the request.

`query.range` is an optional range, rolling or calendar. A panel that sets it
queries that range in place of the dashboard range, and shows a chip with the
range. A "last 7 days" chart can then sit on a dashboard that opens on today.

### Variables

Date-only values get a new variable type, `day`. It renders `'YYYY-MM-DD'` in
ClickHouse SQL and a quoted `YYYY-MM-DD` in LogsQL and LogchefQL. The UI
labels `day` as "Date" and the existing `date` type as "Date and time".

The `date` type does not change. A production instance holds about 130 native
saved queries whose date variables are already wrapped in `toDate()`. Their
variables keep the type `date`, so the SQL they render stays byte-identical.
A user who switches such a variable to `day` gets `toDate('2026-11-12')`,
which is also valid.

Dashboard variables reuse the saved-query variable model
(`pkg/models/query.go:270-280`) with snake_case keys.

- A panel binds a variable by name: `{{customer_id}}`. Optional blocks
  (`[[ AND customer_id = {{customer_id}} ]]`) drop out when the value is empty.
  The template package already supports both in all three languages.
- A value renders as a literal and is never expanded again. This is rule 1 of
  the macro contract.
- The client sends `variables` with each panel request, limited to the names
  the panel text uses.
- Values live in the URL as `var-<name>=<value>`. The blob stores defaults.
  A `day` default may be `today` or `yesterday`.
- A panel that needs an unset variable outside an optional block sends no
  request. It shows "Set <label> to load this panel".
- `options_source` fills a dropdown from the field-values endpoint of one
  team and source.

### LIMIT rule

A result can be short for three different reasons, and the first draft showed
them as one.

| Fact | Meaning | What the panel shows |
|---|---|---|
| The query's own limit | The author wrote `LIMIT 20`. The result is complete as asked | Nothing |
| A cap | The server added a limit or lowered one, from the request limit, the default or the configured maximum | Nothing, unless rows were cut |
| A truncation | Rows existed beyond the cap, or the byte budget cut the result | A chip |

The effective limit is the smallest of the statement's own literal LIMIT, the
request limit and the configured maximum. When the statement has no LIMIT and
the request sends none, the default limit applies. `frame.limit.source`
records which one won.

- `ORDER BY x DESC LIMIT 20` with a request limit of 100 returns 20 rows.
- `LIMIT 5000` with a request limit of 100 returns 100 rows, as today.
- A clamp changes only the row count. `OFFSET` stays. Today the replacement
  builds a new clause and drops it
  (`internal/clickhouse/query_builder.go:329-340`).
- `LIMIT n BY` is a separate clause and is never touched. A LIMIT inside a
  subquery or CTE is never touched.
- `count` mode still strips the outer LIMIT before counting
  (`internal/clickhouse/logs.go:129-133`).
- VictoriaLogs already behaves this way. A `limit` pipe and the request limit
  both apply (`internal/victorialogs/querying.go:113-116`).

**Proving a truncation.** Today the SQL carries `LIMIT N` and the reader stops
at N rows (`internal/datasource/clickhouse_provider.go:343-357`). The driver
then gets at most N rows, and the reader's truncation branch needs one more.
This holds for the buffered reader (`internal/clickhouse/query.go:175-179`)
and for the streaming reader, which serves ClickHouse queries over HTTP
(`internal/clickhouse/query.go:273-278`). A capped result therefore never
reports a row truncation.

- When a cap wins, the server asks for one extra row. It sends `LIMIT N+1`,
  moves `max_result_rows` with it, and returns at most N rows. `truncation` is
  `rows` when the extra row arrived and `none` when it did not. VictoriaLogs
  gets the same probe through its `limit` parameter.
- When the statement's own LIMIT wins, the server sends no probe. The author
  asked for that many rows, and `truncation` is `none`.
- Where the server cannot add the extra row, `truncation` is `unknown` when
  the row count equals the cap. One such case is a LIMIT expression that is
  not a literal (`internal/clickhouse/query_builder.go:343-347`).
- A reader that has a byte budget stops when the response passes it. That is
  a proven truncation, and `truncation` is `bytes`. The buffered ClickHouse
  reader has the budget today (`internal/clickhouse/query.go:187-194`), and
  so does the VictoriaLogs reader (`internal/victorialogs/querying.go:137`,
  `:209`).
  The streaming reader has none (`internal/clickhouse/query.go:273-289`), and
  its caller leaves the budget out on purpose
  (`internal/datasource/clickhouse_provider.go:304-311`). A ClickHouse query
  over HTTP is therefore never cut by bytes today. Phase 16 gives the
  streaming reader the budget for requests that ask for one. "Load bounds"
  has the rule.

| `truncation` | Chip |
|---|---|
| `none` | None |
| `rows` | "First N rows" |
| `bytes` | "Cut by size" |
| `unknown` | "May be incomplete" |

Every visualization shows the chip, not only tables. A sum or a chart over a
cut result is wrong without a sign.

The rule sits in `ensureLimitWithPolicy`, so it reaches the Explorer, the CLI
and MCP as well. This is a decided behavior change. A query with `LIMIT 20`
returns 20 rows on every surface, where the Explorer returned up to its own
limit before. The release notes say so.

`query.limit` is the request limit for a result panel: 1 to 10,000, default
1,000. Tables keep their 100-row pages. A table upgraded from schema 1 keeps
the limit it ran with. "Upgrade from schema 1" gives the rule.

### Tabs, text panels and grid limits

A tab owns a grid. The blob stores `tabs`, each with its own `layout`, and the
top-level `layout` goes away. A dashboard always has at least one tab. The tab
bar is hidden when there is exactly one. The active tab lives in the URL as
`tab=<id>`. Only the active tab's panels run, and a tab runs on first view.

A text panel has `kind: "text"` and Markdown content. It has no team, source
or query, so it is never locked. It renders through text bindings like the Ask
Chef answer (`frontend/src/utils/askChefMarkdown.ts:3-4`), with headings,
lists, bold, code and http links. A text panel with an empty title draws no
header, and serves as a section heading.

| Limit | Schema 1 | Schema 2 | Ships in phase |
|---|---|---|---|
| Panel width | 2, 3, 4, 6, 8, 9, 12 | any of 2 to 12 | 9 |
| Panel height | 1 to 6 rows | 1 to 12 rows | 9 |
| Tabs | none | 10 | 9 |
| Variables | none | 20 | 13 |
| Panels per dashboard | 24 | 60 | 16 |
| Blob size | 100 KB | 512 KiB | 3 |

Until its phase ships, the validator keeps the schema 1 value. The blob size
is the exception that ships with schema 2 itself, because an upgraded blob is
larger than its schema 1 source. "Upgrade from schema 1" gives the numbers.

A larger blob makes the dashboard list larger, because the list returns
every blob today. Phase 3 therefore also takes the blob out of the list.
"The dashboard list" below has the rule.

### The dashboard list

`GET /dashboards` returns every dashboard that the caller can view, each
with its whole panel blob. The store query selects `panels_json` for every
row (`internal/store/sqlite/queries.sql:993-1008`,
`internal/store/postgres/queries.sql:986-1001`). The core function parses
each blob to decide who may see the row, and redacts it
(`internal/core/dashboards.go:150-198`). The frontend keeps all of them in
its store (`frontend/src/stores/dashboards.ts:230-239`). Nothing bounds the
response. At the schema 1 limit, 300 dashboards can take 30 MB. At the
schema 2 limit they can take 150 MB, read from the store, held on the
server, sent, and held in the browser.

The list page reads a blob for three things: the panel count
(`frontend/src/views/dashboards/DashboardsList.vue:121-123`), the row of
type icons (`:127-133`) and the copy that Duplicate creates (`:149-154`).

**The list returns summaries.** From phase 3, `GET /dashboards` returns one
summary per dashboard and no blob. The phase that raises the blob limit is
the phase that takes the blob out of the list.

```ts
interface DashboardSummary {
  id: number;
  name: string;
  description: string;
  panel_count: number;
  panel_types: string[];     // distinct visualization kinds, sorted
  tab_count: number;
  created_by_name?: string;
  created_by_email?: string;
  revision: number;
  can_edit: boolean;
  corrupt: boolean;
  created_at: string;
  updated_at: string;
}
```

- `panel_types` holds the distinct kinds for the icon row: `stat`, `chart`,
  `pie`, `toplist` and `table`, and `text` from phase 12. For a schema 1 row
  they are the kinds that the upgrade gives its panels.
- `panel_count` and `panel_types` include locked panels. Redaction keeps the
  `viz.type` of a locked panel, so a summary shows nothing that the redacted
  blob hides.
- `corrupt` is true when the stored blob fails its schema validator, through
  a parse failure or a validation failure. Its counts are 0
  and its lists are empty. Only its creator and global admins see it, as
  today (`internal/core/dashboards.go:176-189`).
- `can_delete` joins the summary in phase 17 and `collections` in phase 18.
- The order stays `updated_at` descending, then `id` descending.

**Derived columns.** A count of panels needs the blob. The normal list query
must not read it. Only stale-summary repair reads a blob. The `dashboards` table therefore stores what the list needs,
in seven columns that every write of the blob also writes.

| Column | Type | Value |
|---|---|---|
| `panel_count` | integer | The number of panels |
| `tab_count` | integer | The number of tabs. 1 for a schema 1 blob |
| `panel_types` | text | A JSON array of the distinct visualization kinds, sorted |
| `panel_team_ids` | text | A JSON array of the distinct team ids of the query panels, sorted. The visibility filter needs it |
| `panels_corrupt` | boolean | True when the blob fails its schema validator |
| `summary_revision` | integer, default 0 | The `revision` of the blob that the first five were computed from |
| `summary_version` | integer, default 0 | Derivation algorithm version. Phase 3 writes 1 |

- `models.DashboardSummary(blob)` is a pure function that computes the first
  five values from a blob of either schema. It upgrades a schema 1 blob in
  memory first, so both schemas give the same kinds. It never fails. A blob
  that fails its schema validator gives the corrupt summary.
- A create writes the five values, `summary_revision = 1` and
  `summary_version = 1` in its insert.
  An update writes them in the compare-and-swap of "Revisions", which also
  sets `summary_revision = revision + 1` and `summary_version = 1`.
  Blob and summary change in one statement, so they cannot disagree. Phase 3 adds these columns to that
  statement and changes nothing else in it.
- The list query selects the summary fields, `created_by`, the seven columns
  and the creator's name and email. It does not select `panels_json`.
- The visibility rule does not change. The caller sees a row as a global
  admin, as its creator, or as a member of a team in `panel_team_ids`. The
  server reads the caller's teams once, as it does today
  (`internal/core/dashboards.go:159-170`), and decodes one short array per
  row. From phase 18 the ids that the caller can view through managed links
  join the filter.
- A summary is stale when `summary_revision != revision` or
  `summary_version < 1`. Defaults make unset summaries stale. A rolling
  Postgres deployment can have an older server write after startup backfill.
  The list repairs stale rows before it applies visibility or returns them.
  It reads blobs only for stale rows, computes the derived columns, and
  conditionally writes them against the revision read. It then returns the
  row normally, with no temporary creator-only visibility rule.
- Backfill and list repair share two new store methods.
  `ListStaleDashboardSummaryInputs(ctx, afterID, limit)` selects stale rows
  with their blob and revision, in id-ascending pages of 50.
  `RepairDashboardSummary(ctx, id, expectedRevision, summary)` sets the five
  values, `summary_revision = expectedRevision` and `summary_version = 1`
  only if the row still has that revision and is still stale. It returns
  the affected-row count. The list uses the stale-input scan before its
  summary query. It never loads a current row's blob. On a lost conditional
  update, it rereads and repairs that row before returning it. The final
  summary query also checks both stale markers before visibility filtering.
  A row made stale between the scan and that query triggers another repair
  pass. Three failed passes return 503 with `Retry-After: 1`, rather than
  stale visibility. A row deleted during repair is omitted. A concurrent
  repair that already made the row current requires no further write.
- Rollback cannot introduce stale rows. An older binary refuses a newer
  database migration version before it serves requests. This is inferred
  from golang-migrate's `versionExists` check reached by `m.Up()` in
  `internal/store/sqlite/sqlite.go:245-251`. A still-running older process
  in a rolling deployment is the repair case above.
- `GET /dashboards/:id` validates the stored blob against its schema before
  upgrade or redaction. A corrupt row is visible only to its creator and
  global admins. Other callers get 403. An authorized read returns 200 with
  `corrupt: true`, `raw_panels_json` as an opaque string for repair, and
  `can_edit: true` for the creator or admin only. It returns no `panels` and
  performs no upgrade. Valid rows return `corrupt: false` and no raw field.
  Today GET has no corrupt response path
  (`internal/server/dashboard_handlers.go:86-104`). Phase 3 adds it. The
  frontend `Dashboard` type becomes a discriminated union on `corrupt` and
  replaces its old `panels_corrupt` field
  (`frontend/src/api/dashboards.ts:93-94`). The raw field is never exposed
  through summaries or to a caller outside the creator and admin set.

**The backfill runs at startup, not in the migration.** The migration adds
the seven columns with defaults, so every existing row starts with
`summary_revision = 0` and is stale. A startup step then fills the rows.

- The step runs after the migrations and before the server accepts
  requests, next to the provisioning step
  (`internal/app/app.go:152-163`). It selects the rows whose
  `summary_revision` differs from `revision` or `summary_version < 1`,
  in id-ascending batches of 50 through the methods above. It computes each
  summary with `models.DashboardSummary` and writes it with
  `summary_revision = revision` and `summary_version = 1`.
  The write is conditional on the `revision` that the step read. It changes neither `revision` nor `updated_at`.
- The step is idempotent. A second start finds no stale row and writes
  nothing. A start that is interrupted leaves the rest for the next start.
- A store error fails the start, as a failed migration does.
- A migration would have to compute the summary in SQL. That means the
  mapping from schema 1 panel types to visualization kinds written twice,
  once with the SQLite JSON functions and once with the Postgres ones, and a
  third time in Go for the writes. The three could drift. One Go function
  that the writes and the backfill share cannot.
- A migration that parses JSON fails on a corrupt blob. Migrations are
  forward-only here, and a failed one blocks the start. The startup step
  marks that row corrupt and goes on.
- The cost is one read of every blob at the first start after the upgrade.
  For a few hundred dashboards that is well under a second (inferred, not
  measured).

**Names and descriptions get a bound.** A summary is small only when its
text is. Nothing bounds the name or the description of a dashboard today
(`internal/core/dashboards.go:35-49`). From phase 3 a create or an update
refuses a name of more than 200 Unicode code points (Go runes) or a
description of more than 2,000 code points, with a 400.
A row from before phase 3 can be longer. The list query cuts such a name to 200 code points and such a description to
2,000, and `GET /dashboards/:id` returns them whole. Dashboard duplication
in `frontend/src/views/dashboards/DashboardsList.vue:177` applies
`common.copyName` through a new `dashboardCopyName` helper in
`frontend/src/utils/dashboardPanels.ts`. The helper truncates the base name
by code points until the localized copy name fits in 200. It preserves the
localized suffix and never splits a surrogate pair. The dialog enforces
the same bound on subsequent edits.

**No pagination now.** A summary is a few hundred bytes for a typical row,
so a thousand dashboards make a response of a few hundred kilobytes. The
list page also searches and sorts the whole list in the browser
(`frontend/src/views/dashboards/DashboardsList.vue:76-87`), so pages would
need search and sort on the server as well. A cursor can come later without
breaking a client. The request gains optional `limit` and `cursor`
parameters, and a request without them still gets the whole list. The body
stays an array, and the next cursor goes in a response header. The order is
already total (`internal/store/sqlite/queries.sql:1008`), so the cursor is
the pair of `updated_at` and `id`.

**A schema 1 client.** The list has one shape for every
client, and no client gets a blob from it. A v2.6.1 browser tab still calls
the list, and its Duplicate action reads `panels` without a check
(`frontend/src/utils/dashboardPanels.ts:528-543`). For a schema 1 client,
with the schema header absent or equal to `1`, the server adds two constant
fields to every
summary: `panels`, which is the empty schema 1 blob, and
`panels_corrupt: true`. They are the first two fields of the stub in
"Client schema contract". `can_edit` keeps its computed value, because the
old list uses it only to offer Delete
(`frontend/src/views/dashboards/DashboardsList.vue:218-220`). The old list
then shows a panel count of 0 and hides Duplicate (`:215-217`), and a click
still opens the dashboard through `GET /dashboards/:id`. A script
that read blobs from the list reads each dashboard by id from this phase.
The release notes say so. The two fields go away together with schema 1
write support.

**Frontend.**

- `frontend/src/api/dashboards.ts`: a `DashboardSummary` type, and `list`
  returns it (`frontend/src/api/dashboards.ts:111`). `panels_corrupt` leaves
  the `Dashboard` type (`frontend/src/api/dashboards.ts:93-94`). The corrupt
  union variant carries `raw_panels_json` and no `panels`.
- `frontend/src/stores/dashboards.ts:230-239`: `fetchDashboards` keeps
  summaries. A create no longer puts the full dashboard at the head of the
  list (`:280`), and a save no longer replaces the list entry with the full
  dashboard (`:816-817`). The list page fetches the list when it mounts
  (`frontend/src/views/dashboards/DashboardsList.vue:113-115`), so neither
  patch is needed. The delete filter stays
  (`frontend/src/stores/dashboards.ts:292`).
- `frontend/src/views/dashboards/DashboardsList.vue`: `panelCount` reads
  `panel_count` (`:121-123`). `panelTypeIcons` reads `panel_types`, with
  icons keyed by visualization kind (`:127-133`).
- Duplicate. `openDuplicate` fetches the full dashboard with
  `GET /dashboards/:id` before it opens the dialog
  (`frontend/src/views/dashboards/DashboardsList.vue:168-180`), and
  `duplicateCopy` builds the copy from that response (`:149-154`). The
  request goes through the API client and not through `loadDashboard`,
  which replaces the dashboard that the store holds as current
  (`frontend/src/stores/dashboards.ts:242-258`). The existing `openRequest`
  counter covers the new request
  (`frontend/src/views/dashboards/DashboardsList.vue:156-158`). A failed request
  shows its error and opens no dialog. Locked panels stay out of the copy,
  as today.
- `duplicateAction` hides Duplicate for a summary with `corrupt`
  (`frontend/src/views/dashboards/DashboardsList.vue:215-217`). That was the
  frontend's one use of `panels_corrupt`.

**Check.** A handler test stores two sets of 300 dashboards with the same
names, descriptions, panel counts and panel kinds. In the first set every
query is short. In the second set the query texts pad every blob to within
1 KiB of the 512 KiB limit. The two list responses have the same length in
bytes, and that length is below 1 MiB. No element has a `panels` key when
the request carries schema header `2`. An absent header and explicit `1`
both receive the constant compatibility fields. The test also runs the
backfill over both sets twice and asserts that the second run writes nothing.

### Load bounds

Sixty panels, comparisons and variables multiply requests. The panel limit
rises only together with the bounds below, in phase 16. The blob limit rises
earlier, in phase 3. A larger blob adds no request.

- **One pool.** Every request that a dashboard view sends goes through one
  pool of 4 (`frontend/src/stores/dashboards.ts:44-47`). That covers panel
  queries, the LogchefQL translate step, comparison requests and variable
  option lookups. A comparison is a second task in the pool, never a request
  beside it.
- **A refresh does not cancel work that is still wanted.** Today a refresh
  aborts the whole previous batch
  (`frontend/src/stores/dashboards.ts:501-505`). With 60 panels and a
  30-second refresh, a slow panel would be cancelled on every tick and never
  finish. Each request gets a key of panel, range selection and variable
  values. The range selection is what the picker holds, such as `7d` or
  `today`, not the resolved instants, because a rolling range moves on every
  tick.
  - An auto-refresh tick leaves a request with an unchanged key running and
    does not queue that panel again. The next tick picks the panel up.
  - A change of range or of a variable cancels the requests whose key
    changed, and only those.
- **Hidden tabs.** When the viewer leaves a tab, its queued tasks are dropped.
  Requests already in flight finish and keep their result. The pool bounds
  them.
- **Response size.** A panel request carries a byte budget in a new
  `max_response_bytes` request field. The frontend sends 4 MiB. The effective
  budget is the smaller of the field and the configured budget
  (`internal/config/config.go:101-102`). A cut shows as
  `truncation: "bytes"`. 4 MiB is the default size cap of one cache entry
  (`internal/config/config.go:507`), so a response below the budget fits one
  entry.
  - For a request that carries the field, every reader applies the effective
    budget. The streaming ClickHouse reader gets the check in phase 16
    (`internal/clickhouse/query.go:273-289`). The cache fill and the streaming
    fallback both use that reader, so both cut at the same point.
  - A request without the field keeps today's behavior. On the streaming
    reader it has no byte cut, so the Explorer does not change.
  - The budget that the reader applies is a field of the cache key
    (`internal/cache/dashcache.go:284-299`). Without it, two requests for one
    query with different budgets share one entry. A fill with a small budget
    would serve its cut result to a request with a large budget. A fill with a
    large budget would serve more bytes than a small-budget request asked for.
    The key encoding changes, so `keyVersion` goes to 2
    (`internal/cache/dashcache.go:39-41`).
- **Retained frames.** One dashboard view retains at most 64 MiB of frames,
  over all its tabs.
  - A frame costs the size of its decoded response body. That size is an
    approximation of the heap that the frame holds, and it is not a heap
    measurement. Parsed rows take more heap than their JSON text (inferred,
    not measured).
  - 64 MiB holds 16 frames at the 4 MiB response budget, so one frame always
    fits. Without the budget, 60 panels at the response budget would retain
    240 MiB. 64 MiB is also the default size of the whole server-side
    dashboard cache (`internal/config/config.go:506`). Phase 16 measures a
    60-panel dashboard and records the heap that 64 MiB of frames takes.
  - Admission ranks every panel by its distance from the viewport. Panels
    inside the viewport come first, in reading order. The other panels of the
    active tab follow, nearest first. Panels of hidden tabs come last, with
    the most recently viewed tab first. The store walks the ranking and keeps
    frames while the total stays within the budget. It releases every frame
    that does not fit.
  - Admission runs when a frame arrives, when the viewer scrolls and when the
    viewer changes tabs.
  - A panel whose frame was released shows "Result released to save memory"
    and a Reload button. It also reloads by itself when it scrolls into view.
    The reload reads the server cache when the entry is still there.
  - A released panel reloads by itself at most once each time it enters the
    viewport. A panel that the viewer reloads by hand ranks first until the
    viewport changes. A viewport whose panels exceed the budget therefore
    settles with some panels released, and never reloads them in a loop.
  - An auto-refresh tick skips a released panel that is outside the viewport.
- **Oversized cache entries.** The byte budget counts approximate row bytes,
  and a cache entry holds the encoded response. A response that was cut at
  the budget can therefore exceed the entry cap. The cache fill then gives up
  (`internal/server/dashcache.go:106-119`) and the request falls back to the
  streaming path (`internal/server/dashcache.go:141-167`), which applies the
  same budget. Such a panel runs its query for every viewer and every
  refresh. It shows a "not cached" chip, read from the `X-Logchef-Cache`
  header.

Tests cover a tab switch during a refresh, a refresh tick while the previous
batch is still loading, and both at once. Each asserts that at most 4 requests
are in flight and that every visible panel reaches a final state.

Two more tests cover the frame budget and the cache key.

- Frame budget. One tab holds more frames than the budget, and no other tab
  exists to release. The test asserts five things. The retained bytes stay
  within the budget. The panels nearest the viewport hold their frames. The
  other panels show the released state. A scroll to a released panel reloads
  it and releases a farther one. No panel reloads twice without a scroll or a
  click.
- Cache key. Two requests differ only in their byte budget, and the result is
  larger than the small budget and smaller than the large one. In one order
  the small budget fills the cache first, and in the other order the large
  budget does. In both orders the small-budget request gets a cut result with
  `truncation: "bytes"`, the large-budget request gets the whole result, and
  the cache holds two entries. A third case sends a result above the entry
  cap through the streaming fallback and asserts `truncation: "bytes"`.

### Drill-down and cross-filter

`click` on a visualization has three actions.

- `explore`: opens the Explorer over the clicked window. It is the default for
  `count` panels. A LogchefQL panel adds the clicked series as a filter, built
  from the series key and not from its label. A time chart narrows the window
  to the clicked bucket.
- `set_variable`: writes the clicked row's `column` into a dashboard variable.
  Every panel that uses the variable runs again.
- `none`: the default for `result` panels.

A drag across a time chart sets the dashboard range. Every panel menu has
"Open in Explorer", which carries the panel's window and variable values.

### Ownership and permissions

Collections become the home for dashboards. A new table,
`collection_dashboards`, links a dashboard to a collection. A dashboard can
sit in several collections, as a saved query can.

Dashboards get no `team_id`. A collection shared with a team already gives
every team member the Member role (`pkg/models/collections.go:49-50`). A
second ownership path would compete with it, and one dashboard can hold
panels from several teams.

**The hole in the first draft.** Any participant may pin an item into a
collection (`internal/core/collections.go:385-392`). Anyone can create a
shared collection and becomes its owner
(`internal/core/collections.go:96-108`). The first draft let the owners and
editors of a collection edit every dashboard in it. A user who could only
view a dashboard could then create a collection, pin the dashboard and edit
it. An exclusion for personal collections does not help, because the
attacker's collection is a shared one.

**Two kinds of link.** A link between a collection and a dashboard has a
kind.

| Kind | Meaning | Grants |
|---|---|---|
| `pin` | A bookmark. The dashboard is useful here | Nothing. It lists the dashboard for participants who can already view it |
| `managed` | A delegation. This collection looks after the dashboard | View, to every participant of the collection's root. Edit, to the owners and editors of the root |

Authority comes from the dashboard, not from the collection. Owner authority
over a dashboard belongs to its creator and to global admins. Only owner
authority creates or removes a managed link.

| Action | Who |
|---|---|
| View a dashboard | A global admin. The creator. A member of any team that a panel references (today's rule, `internal/core/dashboards.go:258-286`). A participant of the root of a shared collection that manages it |
| See a panel's query and data | Anyone with access to the panel's team and source. Everyone else gets a locked panel. From phase 17 that includes the creator |
| Edit | A global admin. The creator. An owner or editor of the root of a shared collection that manages it. The locked-panel rule below applies to everyone but an admin |
| Delete | A global admin. The creator |
| Create a managed link | The creator or a global admin. Either must take part in the collection |
| Change the kind of an existing link | The creator, who must take part in the collection. Or a global admin, who needs no part in it |
| Remove a managed link | The same as creating one |
| Pin, unpin | Any participant of the collection who can view the dashboard |

The viewer from the example can still create a collection and pin the
dashboard. The pin grants nothing, and the viewer cannot create a managed
link.

**What a managed link hands over.** A managed link gives edit to the owners
and editors of the collection's root. Only an owner of a collection adds
members and sets their roles (`internal/core/collections.go:217-234`). The
link therefore hands the list of editors to the collection's owners. They can
appoint more editors later, and nobody asks the creator. The dialog that
creates a managed link says so.

Removing a managed link ends that delegation for everyone, so it needs the
authority that created it. A participant can no longer remove one. An owner
of the collection who wants a managed item gone asks its creator or an admin,
or deletes the collection.

- Managed links exist only in shared collections. A personal collection takes
  pins only. The view and edit queries also filter `is_personal = 0`, as the
  saved-query edit query does (`internal/store/sqlite/queries.sql:966`,
  `internal/store/postgres/queries.sql:959`).
- The kind is a column with two values on one row per collection and
  dashboard. A dashboard cannot be both pinned and managed in one collection.
- Three predicates replace one: `UserCanViewDashboard`,
  `UserCanEditDashboard` and `UserCanDeleteDashboard`. The delete handler
  calls the edit predicate today
  (`internal/server/dashboard_handlers.go:161`). That is harmless while both
  mean "creator or admin", and wrong on the day edit widens. Responses carry
  `can_edit` and `can_delete`, as saved queries do
  (`pkg/models/query.go:317-321`).
- Every save still passes the team and source checks in
  `verifyDashboardPanelRefs` (`internal/core/dashboards.go:72-124`).
- The dashboard list reads the ids that the caller can view through managed
  links in one query, not one query per dashboard.

The edit check, in its final form after nesting ships:

```sql
SELECT COUNT(*)
FROM collection_dashboards cd
JOIN collections c    ON c.id = cd.collection_id
JOIN collections root ON root.id = c.root_id
JOIN collection_members cm ON cm.collection_id = root.id
WHERE cd.dashboard_id = ?
  AND cd.kind = 'managed'
  AND root.is_personal = 0
  AND cm.user_id = ?
  AND cm.role IN ('owner', 'editor');
```

**Locked panels.** A caller who cannot reach a panel's source gets the panel
redacted, with a blank query. A save from that caller would write the blank
back or drop the panel. The first draft made such a dashboard read-only in
the client. The server now enforces the rule, and it checks the stored blob,
not the request.

1. Inside the save transaction, the server reads the stored blob and finds
   the panels that are locked for the caller.
2. If no panel is locked, the save goes on.
3. If a panel is locked and the caller is a delegated editor, the server
   refuses the save with 403. The request body plays no part in this check,
   so a crafted request cannot drop or replace a locked panel.
4. If a panel is locked and the caller is the creator, the save goes on only
   when the new blob contains none of the locked panel ids. The creator may
   delete the whole dashboard, so the creator may remove panels from it.
   `verifyDashboardPanelRefs` already requires that the caller can reach
   every panel in the new blob. The editor offers this as one confirmed
   action, "Remove N locked panels".
5. A request panel that carries `locked: true` is a 400. A redacted response
   can never be saved back.

The creator no longer skips redaction. Today the creator gets the full blob
even after losing access to a source
(`internal/core/dashboards.go:217-223`). Once other people can edit, that
exemption would show the creator queries that others wrote against sources
the creator cannot reach. Global admins keep the full view. They bypass the
membership check on save as well (`internal/core/dashboards.go:81`,
`:117-121`).

The exemption ends in phase 17, and the locked-panel rule ships with it.
Phase 17 comes immediately before phase 18, which is the first phase that
lets someone other than the creator edit. Until then the creator and admins
are the only editors, and the exemption shows nobody a query that another
person wrote. Ending it earlier would lock creators out of their own panels
and close no leak.

**Saved queries get the same two kinds.** `collection_items` gains the `kind`
column, and the saved-query edit query counts managed links only. The rules
in the table above hold for a saved query as they do for a dashboard. This
is phase 1. It closes the hole in released code and ships alone.

*The migration.* It sets `managed` on an existing link when the collection is
shared and the link was added by the query's creator or by a user who is a
global admin at migration time. Every other link becomes a pin.

- A link whose `added_by` is null becomes a pin. The stores set the column to
  null when the user who added the link is deleted
  (`internal/store/sqlite/migrations/000019_add_collections.up.sql:48`,
  `internal/store/postgres/migrations/000001_init.up.sql:241`), so nothing
  says who added it.
- No automatic rule can do better. A link that someone else added looks
  exactly like the exploit.
- An editor who relied on such a link loses edit on that query until the link
  is converted. The release notes say so.

*Writes after the migration.* The server records who adds a link
(`internal/core/collections.go:409-410`) and now also its kind. A write that
omits `kind` creates a pin, so a client from before this phase cannot create
a delegation by accident. Today any participant may remove any link
(`internal/core/collections.go:413-420`). From this phase a participant may
remove a pin, and the removal of a managed link follows the table above.

*Frontend kind defaults.* In the save-into-collection flow
(`frontend/src/composables/useSavedQueries.ts:194`) and add-to-collection
(`frontend/src/views/library/CollectionDetailPane.vue:337`), a query creator
adding to a shared, non-personal collection sends `managed` by default.
The add UI shows "Collection editors can edit", on by default. Switching
it off sends `pin`. A global admin gets the same toggle and default.
A non-creator who is not an admin always sends `pin` and sees no toggle.
A personal collection always gets `pin` and shows no toggle, for every
caller. The API default for an omitted kind stays `pin` for older clients.
The migration's creator-or-admin rule matches the new UI default.
Move (`frontend/src/views/library/CollectionDetailPane.vue:363`) preserves
the source link's kind rather than applying the add default, as below.
The all-queries pane uses the same add policy.

*Every link write is atomic.* Today the server authorizes a link write in
one step and changes the row in another, with no transaction around them
(`internal/core/collections.go:389-420`). The removal deletes the row
whatever it holds (`internal/store/sqlite/queries.sql:898-900`,
`internal/store/postgres/queries.sql:894-896`). The insert does nothing on a
conflict and reports success whatever row exists
(`internal/store/sqlite/queries.sql:892-896`,
`internal/store/postgres/queries.sql:888-892`). With one kind of link that
was harmless. With two kinds it is a hole. A participant reads a pin and is
allowed to remove it. The creator converts the pin to a managed link. The
participant's delete then removes a delegation that the participant has no
authority over. In the other direction, a creator posts a managed link over
an existing pin, the insert does nothing, and the creator is told that the
delegation exists.

Four rules close it. They hold for saved-query links from phase 1 and for
dashboard links from phase 18.

1. Each link write is one transaction (`WithTx`,
   `internal/store/store.go:36`). Postgres locks the collection row with
   `SELECT ... FOR UPDATE` before reading authority or the link. Every link
   mutation takes that lock, including inserts for a pair with no link yet.
   SQLite begins an immediate write transaction before those reads. The lock
   covers authorization, mutation and response selection until commit.
   Membership and share mutations take the same collection lock before
   changing the authority used by a link write.
2. Inside that transaction the server checks participation, personal status,
   item ownership and the caller's role. It derives the kinds that the caller
   may act on. A failed authority check returns before any mutation.
3. The statement that changes the row names those kinds in its condition.
   The statement decides whether the row changes. No earlier read of the
   link authorizes an unconditional delete.
4. The affected-row count decides the answer. One row is success. With no
   row, the server reads the link under the same lock to choose the response.
   A missing link is 404. A link whose kind forbids removal is 403. A
   successful removal keeps the 200 envelope described below.

The exact phase 1 core functions that take the collection lock are
`AddCollectionMember`, `RemoveCollectionMember`, `AddCollectionTeam`,
`RemoveCollectionTeam`, `AddCollectionItem`, the new `UpdateCollectionItem`,
and `RemoveCollectionItem` (`internal/core/collections.go:217-270`,
`internal/core/collections.go:323-383`,
`internal/core/collections.go:389-420`). Member and team-share checks and
mutations move inside their locked transactions.
Writes to `team_members` and `users.role` are outside this lock scope.
Team-derived participation and global admin status can therefore change
without the collection lock. That is acceptable because the link transaction
rechecks authorization from database state inside the transaction.
The authorization check is the decision point for those external changes.
A later revocation does not retroactively invalidate a committed link write.

A participant with no owner authority removes a link with
`DELETE ... WHERE collection_id = ? AND saved_query_id = ? AND kind = 'pin'`.
The creator and a global admin omit that kind condition only after passing
the participation rule for removal. Both sqlc queries return affected-row
counts. No write relies on SQLite's single connection in one process to
exclude writers in another process. A transaction failure rolls back the
whole operation. SQLite `SQLITE_BUSY` after the configured busy timeout
and Postgres SQLSTATE `40001` (serialization failure) or `40P01` (deadlock) return 503 with
`Retry-After: 1`. They never report a successful mutation. A caller retries
the whole transaction. This distinguishes a transient database conflict
from a different-kind link's 409, which needs an explicit conversion.

`POST` creates a link and never changes one.

| Case | Answer |
|---|---|
| No link exists, and the caller may create a link of that kind | 201 with the link |
| A link of the same kind exists | 200 with the stored link. The row does not change, and it keeps its `added_by`, `sort_order` and creation time. A repeated `POST` is therefore safe |
| A link of the other kind exists | 409 with the conflict error type. The message names the stored kind and points to `PUT`. A `POST` never converts a link, in either direction |
| `kind` is neither `pin` nor `managed` | 400 |
| `kind` is `managed` and the collection is personal | 400 |
| `kind` is `managed` and the caller is neither the item's creator nor a global admin | 403. The server checks this before it looks at the stored link |
| The collection does not exist | 404 |
| The caller takes no part in the collection | 403 |
| The saved query does not exist | 404, as today |
| The caller has no access to the saved query's source | 403 |

The insert keeps `ON CONFLICT DO NOTHING` and returns the number of inserted
rows. With no inserted row, the server reads the stored link under the
collection lock and answers 200 or 409 from its kind. Another link mutation
cannot remove or convert that row before response selection. The source
check stays (`internal/core/collections.go:402-408`), but its denial becomes
403 rather than today's fallback 400
(`internal/server/collection_handlers.go:282-285`). This is an authorization
failure, not a malformed request. The release notes state that change and
the new 403 for a non-participant on these link routes.

POST checks run in this order. The first failure wins.

1. 404 for a missing collection.
2. 403 for no participation.
3. 400 for an invalid kind or managed in a personal collection.
4. 404 for a missing query.
5. 403 for managed without creator or admin authority.
6. 403 for no source access, including on a replay.
7. 409 for an existing link of the other kind.

The query lookup precedes the managed-authority check because that check
needs the query's creator. No stored link is read before those checks.
PUT uses the same applicable order, with an admin exemption from
participation and no source-access requirement for conversion. Its missing
link check follows conversion authority. DELETE checks collection,
participation, link existence, then authority over the stored kind.

POST and PUT return the link in the existing success envelope. Its data is
`{ collection_id, saved_query_id, kind, added_by, sort_order, created_at }`.
The ids and `sort_order` are integers, `kind` is `pin` or `managed`,
`added_by` is a user id or null, and `created_at` is an RFC 3339 timestamp.
POST returns 201 on insert and 200 with the existing link on an authorized
same-kind replay. PUT returns 200 with the resulting link.
Today's POST data is `{ "message": "Item added" }`
(`internal/server/collection_handlers.go:286`). This response change is
release-noted. The `addItem` response type and a new `updateItem` method in
`frontend/src/api/collections.ts:98-101` use `CollectionItemLink` with those
six fields. `ListCollectionItems` selects `kind` on both stores
(`internal/store/sqlite/queries.sql:902`,
`internal/store/postgres/queries.sql:898`). `models.CollectionItem` gains
`kind` (`pkg/models/collections.go:63-70`), as does its frontend type.

`PUT` owns every conversion. Its body is `{ "kind": "pin" | "managed" }`,
with no default.

| Case | Answer |
|---|---|
| The link has the other kind, and the caller is the item's creator and takes part in the collection, or is a global admin | 200 with the link, now of the new kind |
| The link already has that kind, and the caller has conversion authority | 200 with the link. Nothing is written and nothing is logged |
| No link exists | 404. A `PUT` never creates a link |
| `kind` is missing, or is neither `pin` nor `managed` | 400 |
| `kind` is `managed` and the collection is personal | 400 |
| The caller takes part in the collection and is neither the item's creator nor a global admin | 403, for a conversion in either direction |
| The caller takes no part in the collection and is not a global admin | 403 |

The conversion is
`UPDATE ... SET kind = ? WHERE collection_id = ? AND saved_query_id = ? AND kind <> ?`,
with the new kind in both places. A link has one of two kinds, so one
changed row means that the link had the other kind. The log line for an
admin takes the old kind from that fact.

`DELETE` removes a link.

| Case | Answer |
|---|---|
| The link is a pin, and the caller takes part in the collection | 200 |
| The link is managed, and the caller takes part in the collection and is the item's creator or a global admin | 200 |
| The link is managed, and the caller takes part in the collection without that authority | 403. The link does not change |
| No link exists | 404 |
| The caller takes no part in the collection | 403. This includes a global admin, who can convert the link to a pin with `PUT` and cannot remove it |

A successful `DELETE` keeps today's 200 with the response envelope
(`internal/server/collection_handlers.go:307`). The frontend reads the
envelope of every response (`frontend/src/api/types.ts:16-20`), so a 204
with no body would fail in an open tab. Today a `DELETE` of a link that does
not exist answers 200. From this phase it answers 404, because the row
count now carries meaning.

The collection view moves a saved query with a `POST` to the target
collection and a `DELETE` on the source
(`frontend/src/views/library/CollectionDetailPane.vue:363-365`). From this
phase the `POST` carries the kind of the source link, and the `DELETE` runs
only after the `POST` answered 200 or 201. A caller who may not create that
kind gets the 403 on the `POST`, and nothing has moved.

Four race cases run on both stores with `-race`. Storetest runs the two writes concurrently in each forced lock order.
The cases in `internal/store/storetest/` assert the number of changed rows
and the stored link. Core tests assert HTTP-visible answers by executing
each order sequentially. They add no hook to production code.

- Remove against convert to managed. A participant removes a pin while the
  creator converts it to managed. Afterward one of two states holds. Either
  the link is gone, the removal answered 200 and the conversion answered
  404. Or the link is managed, the conversion answered 200 and the removal
  answered 403. A managed link is never removed, and a conversion that
  answered 200 never leaves no link.
- Remove against convert to pin. A participant removes a managed link while
  the creator converts it to a pin. Either the removal answered 403 and the
  link is a pin, or the conversion ran first, the removal answered 200 and
  the link is gone.
- Two creates of different kinds. A participant posts a pin while the
  creator posts a managed link, and no link exists. Exactly one answers 201,
  the other answers 409, and the stored kind is that of the 201.
- Two creates of the same kind. One answers 201 and the other 200, and one
  row exists. The replay preserves `added_by`, `sort_order` and creation time.

The storetest race harness places barriers inside `WithTx` to force both
lock acquisition orders and uses
separate database connections. SQLite includes two store instances on the
same file. A test that serializes calls through one connection is not race
coverage. Handler cases also cover a missing link, an invalid kind, an
omitted `PUT` kind, a forbidden conversion and a duplicate of each kind.
They also cover source denial as 403, combined failures in precedence order,
and all three driver conflicts as 503 with `Retry-After: 1`.

*Finding the links that lost delegation.* Migrations run when the server
starts, so no report from the new release can run before the migration does.
The migration changes nothing on a link except its kind, so a report after
the upgrade is complete. `GET /admin/collection-links/report` lists every
pin in a shared collection, for global admins. Each row has the collection,
the saved query, the query's creator or null, the user who added the link or
null, the time the link was added, and the owners and editors of the
collection. Those owners and editors are the people who get edit when the
link is converted.

- Right after the upgrade, the list is exactly the links that lost
  delegation. Pins that people add later join it, and the creation time
  separates them.
- The admin page for saved queries (`internal/server/server.go:368`) shows
  the report with a convert action on each row.
- The upgrade notes carry the migration rule as a read-only query, for an
  operator who wants the list before the upgrade.

*Conversion.* Two parties can turn a pin into a managed link.

- The query's creator, who must take part in the collection.
- A global admin. This works when the creator's account is gone, in which
  case `created_by` is null
  (`internal/store/postgres/migrations/000001_init.up.sql:215`,
  `internal/store/sqlite/migrations/000027_remove_legacy_query_type.up.sql:18`).
  It also works when the admin takes no part in the collection. Collections
  give admins no pass today (`internal/core/collections.go:124-126`), so this
  is a bypass. It covers the report and the change of a link's kind, and
  nothing else. The admin still cannot read the collection through its
  routes. Every kind change by an admin writes a log line with the admin,
  the collection, the query, the old kind and the new kind.

### Nested collections

Collections nest by a `parent_id` column. Nesting only organizes. Members and
team shares exist on root collections alone, and a child takes the caller's
role on its root. Per-child permissions are rejected. They need inheritance
and override rules that no current use case asks for. The corpus needs one
parent with five children.

Each collection also stores `root_id`, the id of the root of its tree. A root
points at itself. Authorization then needs one join and no recursive query.

- Depth is at most 4, counting the root.
- An owner or editor of the root creates children.
- Deleting a collection needs the owner role, as today
  (`internal/core/collections.go:188-202`). A child takes that role from its
  root.
- Deleting a collection deletes its children and their links. It never deletes
  saved queries or dashboards.
- A personal collection may have children. They stay private. No collection
  moves into or out of a personal tree.

**Every authorization query resolves the root.** The collection list and the
saved-query edit check use direct membership today
(`internal/store/sqlite/queries.sql:839-865`, `:956-967`). All of these change
on both stores, and each gets a conformance case in
`internal/store/storetest/`:

| Query | SQLite | Postgres |
|---|---|---|
| `ListCollectionsForUser` | `internal/store/sqlite/queries.sql:839` | `internal/store/postgres/queries.sql:835` |
| `GetCollectionMember`, as used by `GetCollectionForUser` (`internal/core/collections.go:128-159`) | `internal/store/sqlite/queries.sql:873`, by root | `internal/store/postgres/queries.sql:869`, by root |
| `CountCollectionTeamAccess` | `internal/store/sqlite/queries.sql:948` | `internal/store/postgres/queries.sql:941` |
| `CountSharedCollectionEditAccess` | `internal/store/sqlite/queries.sql:956` | `internal/store/postgres/queries.sql:949` |
| The new dashboard view and edit checks | new | new |

Each conformance case proves two things. A child inherits the role from its
root. A member or share row that points at a child grants nothing.

**Member and share writes on a child are rejected.** The request gets a 409
that names the root. The write and its "is this a root" check run under the
tree lock below, so a concurrent move cannot leave an access row on a child.

**Moves.** A move changes `parent_id`.

| Move | Who | Effect on access rows |
|---|---|---|
| Inside one tree | Owner or editor of the root | None |
| A child to another tree | Owner of both roots | None. The child had no rows |
| A root under another root | Owner of both roots | The moved root's member and share rows cannot stay, because only roots hold them. The request must carry `drop_access: true`. The move then deletes those rows in the same transaction and returns how many it deleted. Without the flag the server answers 409 with the counts |
| A child to the top | Owner of the old root | The child becomes a root. The caller becomes its owner. It has no shares |

Moving a root under another root changes who can reach it in both directions.
People who reached it only through its own rows lose access. Everyone with
access to the new root gains access to it. The confirmation dialog states
both effects with the counts.

**Every write to the tree takes one lock.** Two concurrent moves, A under B
and B under A, can each pass a cycle check and together create a cycle. Every
move runs in one transaction that first takes a tree lock,
`LockCollectionTree`. It follows the existing `LockMembershipWrites`
(`internal/store/store.go:72-73`). On Postgres it is an advisory transaction
lock (`internal/store/postgres/queries.sql:97`). On SQLite it is a no-op,
because the single write connection opens every transaction with an immediate
lock (`internal/store/sqlite/sqlite.go:87`, `:95`). Under the lock the move:

1. reads the ancestors of the target again,
2. rejects the move when the target lies inside the moved subtree,
3. rejects the move when the depth of the target plus the height of the
   moved subtree exceeds 4,
4. updates `parent_id`, and `root_id` for the whole moved subtree.

Creating a child and deleting a collection take the same lock. The first
revision locked only moves and access-row writes. A create or a delete
authorizes its caller against a tree that a concurrent move can change, so
each one checks again under the lock.

- Create. Under the lock the server reads the parent again and resolves its
  root. It checks that the caller is an owner or editor of that root and that
  the new depth is at most 4. Then it inserts the child with that `root_id`.
  Without the lock, a create that raced a move could add a child at depth 5,
  add a child under a root where the caller has no role, or store the
  `root_id` of the tree that the parent just left. `root_id` has no foreign
  key on SQLite, so the database would not catch the last case.
- Delete. Under the lock the server reads the collection again and resolves
  its root. It checks that the caller owns that root, and then it deletes.
  The delete removes exactly the subtree that exists at that moment. A child
  that a move took out first survives. A child that a move tries to take out
  afterward is gone, and that move gets a 404.
- Member and share writes already run under the lock, as stated above.
- A collection delete takes `LockMembershipWrites` today
  (`internal/core/collections.go:190-193`). A transaction that needs both
  locks takes `LockMembershipWrites` first, so two transactions never wait
  on each other.

Three storetest cases run on both stores.

- Opposite moves. The two moves run concurrently. Exactly one succeeds, and
  no cycle exists afterward.
- Create against move. One transaction creates a child under a collection at
  depth 3. Another moves that collection's tree one level deeper, under a
  root where the creator has no role. Afterward no collection is deeper than
  4, and every `root_id` equals the root that `parent_id` leads to. The child
  exists only if its creator has a role on its root.
- Delete against move. One transaction deletes a collection. Another moves
  one of its children to the top. Afterward one of two states holds. Either
  the child is a root with its own `root_id` and the rest of the subtree is
  gone, or the child is gone and the move returned 404. No row names a
  deleted collection in `parent_id` or `root_id`.

### Revisions

Conflict detection today compares timestamps. SQLite stores `updated_at` with
one-second resolution (`internal/store/sqlite/queries.sql:1016`). The check
reads the row and writes it in two steps
(`internal/core/dashboards.go:309-335`), on both stores
(`internal/store/postgres/queries.sql:1003-1011`). Two saves in the same
second, or two saves that both read before either writes, both pass. The
check is also off when the client omits `updated_at`
(`pkg/models/dashboards.go:55-59`).

Each dashboard gets an integer `revision`, starting at 1. Every response
carries it.

**Every update carries the client's revision.** The body of a
`PUT /dashboards/:id` must hold the `revision` that the client loaded. This
holds for a schema 1 body and for a schema 2 body.

- An update without `revision` is refused with 428 Precondition Required.
  The handler refuses it before it opens the transaction. The message is
  "This save does not say which revision of the dashboard it changes. Reload
  the page and apply your changes again."
- A save is one transaction. It reads the row, authorizes the caller against
  that row, applies the client contract and the locked-panel rule, and then
  writes with a compare-and-swap:

  ```sql
  UPDATE dashboards
  SET name = ?, description = ?, panels_json = ?,
      revision = revision + 1, updated_at = ?
  WHERE id = ? AND revision = ?
  RETURNING revision;
  ```

  The last parameter is the revision from the request, and nothing else. No
  returned row means that another save won. The server answers 409.
- The `updated_at` precondition goes away. The server ignores `updated_at`
  in a request body.
- Reads never write. The read-time upgrade changes neither `revision` nor
  `updated_at`.
- `updated_at` stays in responses for display.
- When history ships, the save inserts the replaced row into the history
  table and prunes old rows in this same transaction.

**Why the revision must come from the client.** The first revision of this
RFC let a request without `revision` write against the revision that the
transaction had read. That check proves only that no write landed during the
transaction. It says nothing about what the client had seen. Two clients load
revision 7. The first saves, and the row moves to revision 8. The second
saves a minute later with its stale copy. Its transaction reads revision 8,
writes revision 9, and the first client's change is gone. Only a revision
that the client names can refuse that save.

**What this changes for clients.** This is a contract change, and the release
notes announce it.

- A browser tab from v2.6.1 or earlier sends `updated_at` and no `revision`
  (`frontend/src/stores/dashboards.ts:798-807`). Its save gets the 428, and
  the tab shows the server's message in an alert
  (`frontend/src/views/dashboards/DashboardView.vue:387-392`, read from the
  save path and not run). The user reloads and saves again.
- A script that updates a dashboard needs one more field. It reads `revision`
  from the `GET` response and sends it back in the `PUT` body.
- A script that sends no precondition could overwrite any concurrent edit
  until now. After this change it cannot.

### Client schema contract

After an upgrade, browser tabs that loaded the old frontend stay open. Such a
tab cannot render a schema 2 blob. Its panel type has `type`, `query` and
`options` (`frontend/src/api/dashboards.ts:35-52`) and its fetch branches on
`type` (`frontend/src/stores/dashboards.ts:372`). The first draft also let the
server accept a schema 1 write and upgrade it. A stale tab could then save its
old copy over a dashboard that had gained tabs, variables, formats or text
panels. API scripts have the same two problems.

The first revision closed that hole for schema 1 clients only. The same hole
opens again inside schema 2. Tabs, variables, formats and text panels arrive
in later phases. A schema 2 tab from an earlier phase does not know them, and
its save would drop them. The contract therefore has two parts. The schema
decides which blob shape a client reads. The features decide which rows a
client may write.

The read tables below describe valid blobs. A corrupt blob instead follows
"The dashboard list" for every client schema. It returns no upgraded panels.

**Two request headers.** A client sends both on every dashboard request.

| Header | Meaning | When absent |
|---|---|---|
| `X-Logchef-Dashboard-Schema` | The newest panel schema that the client understands | Schema 1 |
| `X-Logchef-Dashboard-Features` | The features that the client's code understands, as names separated by commas | No features. A schema 1 client never declares any |

Schema header parsing is exact after HTTP field-value whitespace handling.
An absent `X-Logchef-Dashboard-Schema` or the value `1` means a schema 1
client. The value `2` means a schema 2 client. Every other value returns
400 with "X-Logchef-Dashboard-Schema must be 1 or 2". This includes an empty
value, `0`, `3` and `1,2`.

`X-Logchef-Dashboard-Features` is a comma-separated set of case-sensitive
lowercase names. Surrounding whitespace is trimmed from each entry.
Blank entries and duplicates are ignored. Unknown names are ignored rather
than rejected, so a newer client can still write to this server. An unknown
name never grants support for a known stored feature. A schema 1 client
declares no features. The list's compatibility fields depend on being a
schema 1 client, not on header absence alone.

**Stored features.** A schema 2 blob has a top-level `features` list. It
names the features that the blob uses.

- The server computes the list. `models.DashboardFeatures(blob)` is a pure
  function over the blob, and every write stores its sorted result. The
  server ignores a `features` list in a request body, so a client cannot
  understate what a blob uses.
- A feature is a part of the blob that an older writer would drop or misread.
  Each phase that adds such a part adds a name to the table below. The schema
  number does not change.
- An upgraded schema 1 blob uses no feature, and its list is empty.

| Feature | The blob uses it when it has | Phase |
|---|---|---|
| `result_frames` | A `stat` panel in `result` mode, a `reduce` other than `sum`, an `order_by`, or `viz.columns` | 5 |
| `formats` | A `format` object | 5 |
| `result_charts` | A `chart`, `pie` or `toplist` panel in `result` mode, or one of `x_scale`, `orientation`, `show_values`, `legend`, `min_slice_percent` and `max_rows` | 6 |
| `stat_compare` | `compare` or `sparkline` | 7 |
| `thresholds` | `thresholds` or `color_mode` | 8 |
| `table_rules` | `rules` | 8 |
| `column_formats` | `column_formats` | 8 |
| `tabs` | More than one tab | 9 |
| `free_grid` | A layout width outside 2, 3, 4, 6, 8, 9 and 12, or a height above 6 | 9 |
| `text_panels` | A panel of the kind `text` | 12 |
| `descriptions` | A panel `description` | 12 |
| `variables` | A `variables` list that is not empty | 13 |
| `panel_time` | A `query.interval` other than `auto`, a `query.range`, or a `default_time_range` of the form `today-<n>d` | 14 |
| `click` | A `click` object | 15 |
| `combine` | `viz.combine` or `overrides` | 20 |
| `compare_period` | `compare.mode = "previous_period"` | 21 |
| `saved_refresh` | `default_refresh_seconds` | 27 |

A model test keeps the table complete. It walks the schema 2 structs and the
enum tables, and it fails when a field or an enum value has neither a feature
nor the mark of the base schema. A phase that adds blob state without a
feature name therefore fails CI.

**Reads.**

| Stored row | Schema 1 client | Schema 2 client |
|---|---|---|
| Schema 1 | The stored bytes, redacted as today. Nothing changes for this client | Upgraded to schema 2 in memory, then redacted |
| Schema 2 | A stub: an empty schema 1 blob, `panels_corrupt: true`, `min_schema: 2` and `can_edit: false` | The stored blob, redacted |

- The server does not filter a read by features. A schema 2 client compares
  the blob's `features` with its own list. When the blob has a feature that
  the client lacks, the frontend opens the dashboard read-only and shows the
  reload banner. It draws a panel that it cannot interpret as a placeholder
  that says to reload.
- During a rolling deployment, a server can meet a stored feature that it does not know.
  It cannot redact what it cannot read. It serves that row as the stub and
  refuses writes to it with 409. Every other dashboard keeps working.

**What an old browser tab shows.** The first revision said that the old
frontend shows a "needs repair" state for the stub. It has no such state. The
dashboard view never reads `panels_corrupt`, and the list reads it only to
hide the Duplicate action
(`frontend/src/views/dashboards/DashboardsList.vue:216`). A tab from v2.6.1
behaves as follows.

- The tab opens a dashboard that is now schema 2. It gets the stub and shows
  an empty dashboard. The list shows the dashboard with no panels. Nothing on
  the page says why. The stub carries `can_edit: false`, and the old frontend
  opens the editor only when `can_edit` is true
  (`frontend/src/stores/dashboards.ts:566-568`). The user therefore cannot
  build panels that the save would refuse.
- The tab loaded the dashboard before the upgrade and still holds its old
  copy. Its panels keep running, and its save is refused. A tab from before
  phase 2 sends no `revision` and gets the 428 with the reload message. A tab
  from between phases 2 and 3 sends `revision` with a schema 1 body and gets
  the 409 below. The old frontend turns a conflict error into its own reload
  message (`frontend/src/stores/dashboards.ts:834-843`).
- In both cases the user reloads once, and the new frontend loads.

**The version check.** From phase 2 the frontend knows when it is stale, so
later upgrades do not depend on an error path.

- The server stamps its version into the page. It already rewrites
  `index.html` when it starts (`internal/server/spa.go:27-49`), and it adds a
  `<meta name="logchef-version">` tag in the same step. The server embeds the
  frontend that it was built with, so the stamp is the build version of the
  frontend.
- The frontend compares the stamp with the version that `/api/v1/meta`
  reports (`frontend/src/api/meta.ts:19`). It does so when the page loads and
  each time the window gets focus.
- On a mismatch it shows a banner: "Logchef was updated. Reload to continue."
  The banner stays until the reload.
- The Vite dev server serves no stamp, and the check is off without one.
- The banner is all that ships now. The frontend does not reload by itself,
  and API responses carry no version header. The server refuses every unsafe
  save anyway.

**Writes.** Every update also needs `revision`, as "Revisions" says.

| Stored row | Schema 1 body | Schema 2 body |
|---|---|---|
| None (create) | Stored as schema 1 | Stored as schema 2 |
| Schema 1 | Stored as schema 1, checked by the schema 1 validator, which does not change | Stored as schema 2 |
| Schema 2 | Refused with 409 and the conflict error type. The message says that a newer Logchef saved this dashboard and that the page needs a reload | Stored as schema 2 when the writer declared every feature in the stored row's list. Otherwise refused with 409 and the conflict error type. The message names the missing features and says to reload |

- A write can therefore never drop state that its client does not know. A
  schema 1 write cannot touch a schema 2 row. A schema 2 write cannot touch a
  row that uses a feature outside the writer's list.
- The check reads the stored row and the header, never the request body. A
  stale body cannot pass it by leaving the feature out.
- A row is schema 2 from the first save by a schema 2 client.
- The check runs inside the save transaction, before the compare-and-swap. A
  stale write cannot slip in between a newer save and its commit.
- A writer may remove a feature that it declared. A user who deletes the last
  text panel saves a blob without `text_panels`, and an older writer can then
  save that dashboard again.
- A schema 1 client keeps a working read and write path on schema 1 rows. A
  script that reads, changes and writes a dashboard keeps working until a
  schema 2 client saves it, provided that the script sends `revision`.
- Schema 1 write support is deprecated from the release that ships schema 2.
  "Open questions" asks when to remove it.

**Why a header and not a body field.** The declaration is a fact about the
client's code, and the blob is a document. The blob already has a `features`
field. A script that reads a dashboard and writes it back sends that field
back unchanged. If the body carried the declaration, that script would
declare features that its code has never seen. A client never copies a
request header from a response, so a header cannot be echoed that way. The
header also covers every route that writes a blob (create, update, restore
and import) with no new field in each body. It sits next to the schema
header, and `frontend/src/api/dashboards.ts` sets both in one place.

**The stale-client matrix.** One table-driven handler test proves the
contract, and it grows with the feature table. For each phase that adds a
feature, the test stores a blob that uses every feature up to that phase. It
then sends an update as each earlier kind of client.

| Writer | Sends | Expected |
|---|---|---|
| A tab or a script from before phase 2 | No `revision` | 428 |
| A schema 1 client from phase 2 on | `revision`, a schema 1 body, no headers | 409 |
| A schema 2 client from each earlier phase | `revision`, a schema 2 body, the feature list of that phase | 409 that names the missing features |
| The current client | `revision`, a schema 2 body, the full feature list | 200 |

In every refused case the stored row is the same afterward, byte for byte.
The test builds its cases from the feature table, so a new feature adds
cases with no new test code.

A vitest case covers the other direction. It loads a fixture blob that uses
every feature in the frontend's list, opens the editor, saves with no change
and asserts that the request body equals the fixture. A feature that the
frontend declares is then a feature that it keeps.

### Revision history

History ships after restoration. Its rules are fixed now, because the first
draft's version would have leaked.

Each save stores the row it replaces in `dashboard_revisions`, in the save
transaction. The newest 50 revisions per dashboard are kept, and the prune
runs in the same transaction.

- Reading the list or one revision needs edit authority over the dashboard.
  History is an editing tool.
- Every historical blob goes through the same steps as the current one. The
  server upgrades it in memory, computes the panels locked for the caller
  against the caller's current source access, and redacts them. An old
  revision can name sources that the current blob no longer names, so the
  check cannot reuse the result for the current blob.
- Restore is an ordinary save with the old blob. It carries the current
  `revision`. It passes validation, the reference checks, the client
  contract, the locked-panel rule and the compare-and-swap, and it creates a
  revision itself. A restore is refused when the old blob holds a panel that
  is locked for the caller.
- A JSON export is the response of the read endpoint, so it is redacted for
  the caller. An import refuses a blob with a locked panel. A CSV export
  comes from the frame that the viewer already fetched through the
  team-scoped endpoint, so it adds no server path.
- Deleting a dashboard deletes its revisions.

## Panel blob schema 2

### Example

The example uses fields from several phases. The validator accepts a field
only from the phase that ships it.

```json
{
  "version": 2,
  "features": [
    "click", "formats", "free_grid", "panel_time", "result_charts",
    "result_frames", "saved_refresh", "stat_compare", "tabs", "text_panels",
    "thresholds", "variables"
  ],
  "cache_ttl_seconds": 600,
  "default_time_range": "today",
  "default_refresh_seconds": 60,
  "variables": [
    { "name": "customer_id", "label": "Customer", "type": "text", "input_type": "input" }
  ],
  "tabs": [
    {
      "id": "today",
      "title": "Current day",
      "layout": [
        { "id": "revenue_total", "x": 0, "y": 0, "w": 3, "h": 2 },
        { "id": "top_customers", "x": 3, "y": 0, "w": 5, "h": 4 },
        { "id": "errors", "x": 8, "y": 0, "w": 4, "h": 4 }
      ]
    },
    {
      "id": "week",
      "title": "Last 7 days",
      "layout": [
        { "id": "notes", "x": 0, "y": 0, "w": 12, "h": 1 },
        { "id": "revenue_daily", "x": 0, "y": 1, "w": 6, "h": 4 }
      ]
    }
  ],
  "panels": [
    {
      "id": "revenue_total",
      "kind": "query",
      "title": "Revenue today (M)",
      "team_id": 1,
      "source_id": 3,
      "query": {
        "mode": "result",
        "language": "clickhouse-sql",
        "text": "SELECT toDate(ts) AS day, sum(amount) / 1e6 AS revenue FROM app.events WHERE ts >= today() - 4 AND msg = 'order paid' [[ AND customer_id = {{customer_id}} ]] GROUP BY day"
      },
      "viz": {
        "type": "stat",
        "value": "revenue",
        "order_by": "day",
        "reduce": "last",
        "compare": { "mode": "previous_row", "good": "up" },
        "sparkline": true,
        "format": { "decimals": 2, "suffix": " M" },
        "thresholds": [
          { "value": null, "color": "red" },
          { "value": 50, "color": "green" }
        ]
      }
    },
    {
      "id": "top_customers",
      "kind": "query",
      "title": "Top customers by revenue (M)",
      "team_id": 1,
      "source_id": 3,
      "query": {
        "mode": "result",
        "language": "clickhouse-sql",
        "text": "SELECT customer_id, sum(amount) / 1e6 AS revenue FROM app.events WHERE {{__time_filter}} AND msg = 'order paid' GROUP BY customer_id ORDER BY revenue DESC LIMIT 20"
      },
      "viz": {
        "type": "chart",
        "mark": "bar",
        "x": "customer_id",
        "x_scale": "category",
        "y": ["revenue"],
        "columns": {
          "customer_id": { "kind": "category", "confidence": "confirmed" }
        },
        "show_values": true,
        "format": { "decimals": 2 },
        "click": { "action": "set_variable", "variable": "customer_id", "column": "customer_id" }
      }
    },
    {
      "id": "errors",
      "kind": "query",
      "title": "Errors by service",
      "team_id": 1,
      "source_id": 3,
      "query": {
        "mode": "count",
        "language": "logchefql",
        "text": "level=\"error\"",
        "group_by": "service",
        "interval": "auto"
      },
      "viz": { "type": "chart", "mark": "bar", "stack": "stacked" }
    },
    {
      "id": "notes",
      "kind": "text",
      "title": "",
      "text": { "markdown": "## Weekly view\nBuckets are calendar days in your timezone." }
    },
    {
      "id": "revenue_daily",
      "kind": "query",
      "title": "Revenue per day (M)",
      "team_id": 1,
      "source_id": 3,
      "query": {
        "mode": "result",
        "language": "clickhouse-sql",
        "text": "SELECT toStartOfInterval(ts, {{__interval}}, {{__timezone}}) AS t, sum(amount) / 1e6 AS revenue FROM app.events WHERE {{__time_filter}} AND msg = 'order paid' GROUP BY t ORDER BY t",
        "interval": "24h",
        "range": "today-6d"
      },
      "viz": { "type": "chart", "mark": "bar", "x": "t", "y": ["revenue"], "show_values": true }
    }
  ]
}
```

The first panel has no `ORDER BY`, and `order_by` makes the tile correct
anyway. The second panel stores a kind. `customer_id` is a `UInt64` in the
table, and the author confirmed it as a `category`. The last panel buckets by
day, so it uses the calendar range `today-6d` and not the rolling `7d`. The
server wrote the `features` list from what the blob uses.

### Fields

Top level:

| Field | Type | Notes |
|---|---|---|
| `version` | int | Always 2 |
| `features` | array of strings | Written by the server. The sorted names of the features that the blob uses. "Client schema contract" lists them |
| `cache_ttl_seconds` | int, optional | Unchanged from schema 1 |
| `default_time_range` | string, optional | Schema 1 forms, plus `today-<n>d` |
| `default_refresh_seconds` | int, optional | 0, 30, 60 or 300 |
| `variables` | array, optional | See "Variables" |
| `tabs` | array | At least one. Each has `id`, `title`, `layout` |
| `panels` | array | Panel definitions, independent of placement |

Panel:

| Field | Type | Notes |
|---|---|---|
| `id` | string | Unique in the dashboard |
| `kind` | `query` or `text` | |
| `title` | string | May be empty |
| `description` | string, optional | Shown behind an info icon |
| `team_id`, `source_id` | int | `query` panels only |
| `query` | object | `query` panels only |
| `viz` | object | `query` panels only |
| `text` | object | `text` panels only. `{ "markdown": "..." }` |
| `locked` | bool | Response only. A request that carries it is rejected |

Query:

| Field | Type | Notes |
|---|---|---|
| `mode` | `count` or `result` | |
| `language` | `logchefql`, `clickhouse-sql`, `logsql` | |
| `text` | string | The query |
| `group_by` | string, optional | `count` only |
| `interval` | string, optional | `auto` or a histogram window. Default `auto` |
| `limit` | int, optional | `result` only. 1 to 10,000. Default 1,000 |
| `translate_limit` | int, optional | `count` with `logchefql` only. 1 to 100,000. Passed only as the translate request's `limit`. Absent leaves that request field absent |
| `range` | string, optional | Rolling or calendar range that replaces the dashboard range |

"Visualizations map columns to roles" lists the roles and options of `viz`.
One option has its own shape. `viz.columns` is a map from a column name to a
column entry, and it exists only in `result` mode.

Column entry:

| Field | Type | Notes |
|---|---|---|
| `kind` | `time`, `date`, `number`, `category`, `bool`, `other` | The kind that the author confirmed. It overrides the kind from the backend type |
| `confidence` | `confirmed` | The only value that the blob stores |

Variable:

| Field | Type | Notes |
|---|---|---|
| `name` | string | Identifier, not starting with `__` |
| `label` | string, optional | |
| `type` | `text`, `number`, `date`, `day` | |
| `input_type` | `input`, `dropdown`, `multiselect`, `datepicker` | |
| `default_value` | any, optional | |
| `options` | array, optional | `{ "value", "label" }` |
| `options_source` | object, optional | `{ "team_id", "source_id", "field" }` |

### Validation

`ValidateDashboardPanels` stays the single entry point
(`pkg/models/dashboards.go:266`). It dispatches on `version`. Schema 1
validation does not change. The rules below are for schema 2.

Two principles sit above the rules.

- **The validator accepts exactly what the running server renders.** A field
  or an enum value from a later phase is rejected until that phase ships.
  Unknown keys are rejected on write, where schema 1 ignored them. A stored
  key that nothing reads would otherwise change meaning when a later release
  starts to read it.
- **Every blob that the schema 1 validator accepts upgrades to a blob that the
  schema 2 validator accepts.** A property test holds this. Schema 1 has no
  bound on the length of one query or one title
  (`pkg/models/dashboards.go:310-339`), so schema 2 adds none. The blob size
  bounds them. The upgrade makes a blob larger, so the schema 2 size limit
  must leave room for an upgraded blob. "Upgrade from schema 1" gives the
  bound. The schema 1 validator also accepts a language alias, empty
  strings, nulls and unknown keys, and schema 2 accepts none of them. The
  upgrade writes one canonical form for each, and "Upgrade from schema 1"
  lists the cases.

The rules:

- `version` is 2. The raw blob is at most 512 KiB.
- The server ignores `features` in a request body and stores the list that it
  computes.
- 1 to 10 tabs. Tab ids are unique and not empty. Titles are 1 to 60
  characters.
- At most 24 panels, and 60 from phase 16. Panel ids are unique and not empty.
- Every panel has exactly one layout entry across all tabs, and every layout
  id names a panel.
- Layout: `w` 2 to 12, `h` 1 to 12, `x >= 0`, `y >= 0`, `x + w <= 12`. No two
  rectangles in one tab overlap.
- `kind` is `query` or `text`. A `text` panel has `text.markdown` of at most
  16 KiB, and no `team_id`, `source_id`, `query` or `viz`.
- A `query` panel has `team_id > 0`, `source_id > 0`, and a valid `mode` and
  `language`. `language` is one of the three canonical names, and the alias
  `sql` is rejected.
- An enum field holds one of its listed values. An empty string is not one.
- No panel carries `locked`.
- `group_by` appears only in `count` mode. `limit` appears only in `result`
  mode. `translate_limit` appears only in `count` mode with `logchefql`.
  It is part of the phase 3 base schema, not a later feature.
- `interval` is `auto` or a known window. `range` and `default_time_range`
  match the range grammar.
- `viz.type` is one of the five types. `count` mode forbids `table`, stored
  roles and `viz.columns`. `pie` and `toplist` in `count` mode need
  `group_by`.
- `result` mode requires the roles. `stat` has `value`. `chart` has `x` and at
  least one `y`. `pie` and `toplist` have `label` and `value`.
- `compare.mode = "previous_row"` needs `reduce` to be `last` or `first`. In
  `result` mode it also needs `order_by`, and so does `sparkline`.
- Role names are non-empty strings of at most 128 characters. The server
  cannot know result columns at save time, so the client reports a missing
  column when it renders.
- `viz.columns` has at most 100 entries. A key is a non-empty string of at
  most 128 characters. A value has exactly `kind`, which is one of the six
  kinds, and `confidence`, which must be `confirmed`. The server accepts a
  key that names no result column, for the same reason as for a role. The
  client reports such a key when it renders.
- Number format: `decimals` 0 to 10, `scale` finite and not zero, `prefix` and
  `suffix` at most 16 characters, enum fields in range. Time format: enum
  fields in range.
- Thresholds: at most 10 steps, the first with a null value, the rest strictly
  ascending by exact comparison, each value a JSON number or a decimal string,
  colors from the palette. Table rules: at most 20. A decimal string in a
  threshold or a rule passes the grammar and the four bounds of "Exact
  numbers".
- `min_slice_percent` is above 0 and below 100.
- Variables: at most 20, unique valid names with no `__` prefix, at most 200
  options each. A variable's `options_source` passes the same team and source
  checks as a panel.

The frontend mirrors these rules in `validatePanelsBlob`
(`frontend/src/utils/dashboardPanels.ts:222`). The server stays the authority.

### Upgrade from schema 1

The server upgrades panel blobs when it reads and never rewrites a stored
blob because someone read it. Summary repair writes derived columns only.

- `models.UpgradeDashboardPanels(raw)` is a pure function. It returns a
  schema 2 blob unchanged and converts a schema 1 blob. The same input always
  gives the same bytes. An upgraded blob has an empty `features` list.
- For a schema 2 client, `core.GetDashboard` calls it before visibility and
  redaction. The summary backfill uses it to derive schema 1 counts and kinds.
  `core.ListDashboards` normally reads derived columns. Its stale-summary
  repair uses the same upgrade to derive counts, without rewriting the blob.
- For a schema 1 client that reads a schema 1 row, the existing path runs
  unchanged.
- Writes do not upgrade. A schema 1 body is stored as schema 1. "Client schema
  contract" gives the reason.
- The frontend holds no upgrade code.
- A parse or schema-validation failure takes the corrupt-row contract in
  "The dashboard list". Both list derivation and GET use that definition.
  GET returns the opaque raw string to the creator or an admin, not an
  upgraded panel payload.

The upgrade writes down every default that schema 1 applied at run time. An
upgraded panel then never picks up a schema 2 default that differs.

| Schema 1 | Schema 2 |
|---|---|
| `layout` | `tabs: [{ "id": "main", "title": "Main", "layout": ... }]` |
| `query`, `query_language` | `query.text`, `query.language`. The language goes through `NormalizeQueryLanguage` first, so the alias `sql` becomes `clickhouse-sql` |
| `type: "timeseries"` | `mode: "count"`, `group_by` from options, `interval: "auto"`, `viz.type: "chart"` |
| `options.chart` on a `timeseries` panel | `mark`: `line` when it has no value (`frontend/src/api/dashboards.ts:19-24`), `bars` becomes `bar`, `area` stays `area` |
| `options.bar_mode` on a `timeseries` panel | `stack`. A `line` chart gets `none` and an `area` chart gets `stacked`, whatever `bar_mode` says. A `bars` chart gets `stacked` when `bar_mode` has no value, and `none` for `grouped` |
| `type: "stat"` | `mode: "count"`, `viz.type: "stat"`, `reduce: "sum"` |
| `type: "breakdown"` | `mode: "count"`, `group_by`, `viz.type: "toplist"`. `breakdown_view: "donut"` gives `viz.type: "pie"` with `donut: true` |
| `type: "table"` | `mode: "result"`, `viz.type: "table"`, `visible_columns` from `options.columns`. `query.limit` is the limit the panel runs with today: 100 when `options.limit` is absent, otherwise `options.limit` clamped to 1 to 1,000 (`frontend/src/utils/dashboardRuntime.ts:7-8`, `:82-85`) |

The table limit is the default that matters most. A schema 1 table fetches 100
rows when it sets no limit, and a new schema 2 result panel defaults to
1,000. Schema 1 also accepts a stored limit of up to 100,000
(`pkg/models/dashboards.go:76`, `:423-430`) and clamps it to 1,000 at run
time. The upgrade stores the clamped value, which is inside the schema 2
range.

**Normalization.** The schema 1 validator accepts spellings that schema 2
rejects: a language alias, empty strings, nulls, unknown keys and options
that the panel type does not use. The upgrade accepts exactly the blobs that
the schema 1 validator accepts, and it writes one canonical form for each
spelling. A blob that the schema 1 validator rejects fails to upgrade and
takes the corrupt-row path.

- **Language.** The schema 1 validator passes `query_language` through
  `NormalizeQueryLanguage` (`pkg/models/dashboards.go:331`), which turns the
  alias `sql` into `clickhouse-sql` (`pkg/models/query.go:56-69`). The stored
  blob keeps the alias. The upgrade calls the same function and writes the
  canonical name. The match is exact, so `SQL` and an empty language are not
  valid in schema 1 and never reach the upgrade.
- **The alias is the one place where an upgraded panel fetches differently
  from v2.6.1.** The v2.6.1 frontend compares the stored string with the
  canonical names (`frontend/src/stores/dashboards.ts:335-337`). It sends an
  `sql` panel through the LogchefQL translation, and the panel shows an error
  (read from the code, not run). After the upgrade the panel runs as
  ClickHouse SQL, which is what the server validated it as. The frontend
  cannot save the alias (`frontend/src/utils/dashboardPanels.ts:254`), so
  only an API client can have stored it.
- **Empty chart.** `options.chart` has no value when it is absent, null or
  the empty string. The validator accepts all three
  (`pkg/models/dashboards.go:418`). A `timeseries` panel then draws a line,
  which is the schema 1 default: the v2.6.1 chart draws bars and areas only
  for those two names and a line for everything else
  (`frontend/src/views/dashboards/components/PanelTimeseries.vue:87`,
  `:191-226`). The other three panel types have no chart, and the upgrade
  drops the key.
- **Empty bar mode.** `options.bar_mode` has no value when it is absent, null
  or the empty string (`pkg/models/dashboards.go:431`). A `bars` chart then
  stacks, which is the schema 1 default
  (`frontend/src/views/dashboards/components/PanelTimeseries.vue:7-15`). A
  `bar_mode` on a chart that is not `bars` has no effect in v2.6.1, and the
  upgrade drops it.
- **Empty breakdown view.** On a `breakdown` panel an absent or null
  `breakdown_view` means horizontal bars, which is `toplist`. The validator
  rejects the empty string there (`pkg/models/dashboards.go:449-451`). On
  every other panel type it accepts only a value with no content
  (`pkg/models/dashboards.go:454-456`), and the upgrade drops the key.
- **Blank group.** A `group_by` with no character other than white space
  means no grouping on a `timeseries` panel
  (`frontend/src/stores/dashboards.ts:373`), and the upgrade writes no
  `group_by`. A `breakdown` panel cannot have a blank group
  (`pkg/models/dashboards.go:446-448`). A `group_by` on a `stat` or `table`
  panel has no effect, and the upgrade drops it. The upgrade does not trim a
  group that it keeps. The server uses Go `strings.TrimSpace` to decide blankness. The frontend
  uses a new `trimDashboardGroupBy` helper in
  `frontend/src/utils/dashboardPanels.ts` for validation and request building.
  It trims the Unicode `White_Space` set, for example with
  `/^\p{White_Space}+|\p{White_Space}+$/gu`, instead of JavaScript `trim()`.
  Both sides trim U+0085 and retain U+FEFF. This aligns the edge cases with
  Go, including when the client builds a request. It corrects the old
  frontend's different whitespace definition.
- **Empty column list.** An absent, null or empty `options.columns` shows
  every column (`frontend/src/api/dashboards.ts:16-17`), and the upgrade
  writes no `visible_columns`. `columns` on a panel that is not a table has
  no effect, and the upgrade drops it. Null and empty-string column entries
  are dropped. Duplicate names keep their first occurrence, in that order.
  The result becomes `visible_columns`, omitted when no names remain.
- **Count translate limit.** On a non-table LogchefQL panel, `options.limit`
  is not ignored today. It is sent as the translate request's `limit`
  (`frontend/src/stores/dashboards.ts:346`). The upgrade preserves it as
  `query.translate_limit`, without the table clamp. The count fetch path
  forwards it to translate only, so the same-requests check includes it.
  A non-LogchefQL count panel drops the option because it never translates.
  Inference, not runtime-verified, VictoriaLogs translation ignores this
  limit when producing its LogsQL filter. The client still sends it so
  request parity holds on both backends.
- **Null and absent are the same.** Schema 1 decodes every optional value
  through a pointer, a slice or a raw message
  (`pkg/models/dashboards.go:158`, `:169-181`, `:192-199`), so a JSON null
  and a missing key give the same result. This covers `options` itself, each
  key inside it, `cache_ttl_seconds` and `default_time_range`. A null `title`
  or `query` is the empty string. The upgrade never writes a null. A value
  that has a schema 1 default becomes that default, and any other value
  becomes an absent key.
- **Empty dashboard and coordinates.** Version 1 with both `layout` and
  `panels` absent or null is an empty dashboard. Each absent or null array
  decodes as empty. The usual id-coverage validator still rejects one empty
  array paired with a nonempty other array. An empty dashboard upgrades to
  one `main` tab with an empty layout and an empty panels array.
  A null layout `x` or `y` decodes as 0
  (`pkg/models/dashboards.go:142-148`). The overlap validator runs after
  those defaults, so an overlap created at zero is still rejected.
- **Stored lock flag.** A stored `locked: true` is dropped. It is a
  response-only flag (`pkg/models/dashboards.go:161-167`). Authorization
  computes fresh locks after upgrade, never from the stored flag.
- **Key spelling.** The upgrade decodes with Go's case-insensitive field
  matching and last-duplicate-wins behavior, exactly as schema 1 validation
  does through `json.Unmarshal` (`pkg/models/dashboards.go:274-277`). It
  emits canonical schema 2 keys. Raw-string preservation uses the winning
  occurrence selected by that decoder, not the first textual match.
- **Unknown keys are dropped.** Schema 1 ignores a key that it does not
  know, at the top level, in a layout entry, in a panel and in `options`.
  The upgrade drops each one, for three reasons. The schema 2 validator
  rejects unknown keys, so a kept key would break the rule that every valid
  schema 1 blob upgrades to a valid schema 2 blob. A stored key that nothing
  reads changes meaning when a later release starts to read it. And a kept
  key would have to move as raw bytes to hold the size bound, for a value
  that no code reads. The stored row keeps its keys until a schema 2 client
  saves the dashboard. A script that stored its own keys in the blob loses
  them at that save, and the release notes say so.

Two blobs that differ only in these spellings upgrade to the same bytes. The
fixtures hold that, with one pair or group for each case:

- `query_language` as `sql` and as `clickhouse-sql`, and one fixture for each
  of the other two names.
- `chart` absent, null and `""` on a `timeseries` panel, and `chart: "bars"`
  on a `stat` panel.
- `bar_mode` absent, null and `""` with `chart: "bars"`, and
  `bar_mode: "grouped"` with `chart: "line"`.
- `breakdown_view` absent and null on a `breakdown` panel, and `""` on a
  `timeseries` panel.
- `group_by` absent, null, `""` and `"  "` on a `timeseries` panel.
- `columns` absent, null and `[]`, and `limit` absent and null, on a `table`
  panel.
- `options` absent, null and `{}` on a `stat` panel.
- `title` absent, null and `""`. `cache_ttl_seconds` and `default_time_range`
  absent and null.
- An unknown key at each of the four levels, next to the same blob without
  it.
- Table `columns: [null, "", "message", "message", "host"]` gives
  `visible_columns: ["message", "host"]`. `[null, ""]` omits the field.
- A complete valid panel with `locked: true` upgrades like the same panel
  without `locked`. Fresh source access alone determines response locks.
- `{ "version": 1 }` and version 1 with both arrays null produce the same
  empty dashboard. A layout with null `x` and `y` matches explicit zeros.
  Two rectangles overlapping after that normalization fail validation.
- A valid blob with `"VERSION": 1` and a panel with
  `"title": "first", "TITLE": "last"` matches canonical `version` and
  `title: "last"`. The winning raw string survives without re-encoding.
- A LogchefQL `stat` with `options.limit: 50000` retains
  `query.translate_limit: 50000` and sends `limit: 50000` to translate.
  ClickHouse and VictoriaLogs request fixtures both assert the field.
- A timeseries `group_by: "\u0085"` omits grouping. `"\ufeff"` keeps it.
  A value surrounded by U+0085 trims on request construction, while U+FEFF
  stays. Server and frontend fixtures assert the same results.

**Size.** The upgrade makes a blob larger. It adds the tab wrapper and the
`features` list once. It adds `kind`, `mode`, `interval`, `viz` and the
materialized defaults to every panel. The re-review measured a schema 1 blob
of exactly 102,400 bytes that became 102,492 bytes. 102,400 bytes is the
schema 1 limit (`pkg/models/dashboards.go:66`, enforced at `:270`). The first
revision kept that limit for schema 2 until a late phase, so the first save
of that dashboard by the new frontend would have failed.

- The schema 2 limit is 512 KiB from phase 3, the phase that ships schema 2.
  The schema 1 validator keeps its 100 KB limit for schema 1 bodies.
- The upgrade adds at most 64 bytes per blob and 128 bytes per panel. Schema 1
  allows 24 panels, so the overhead is at most 3,136 bytes, and an upgraded
  blob is at most 105,536 bytes. The two constants are estimates from the
  fields in the table above. The largest case that the estimate found adds 52
  bytes per blob and 86 bytes per panel. The property test enforces the
  bound.
- The bound holds only when string values keep their bytes. The upgrade
  moves every string that it carries over as the raw JSON string that it
  read, and never encodes one again. Those strings are `id`, `title`,
  `query`, `group_by`, each entry of `columns` and `default_time_range`.
  Schema 1 bounds the length of none of them. A second encoding can grow a
  string, because the Go encoder escapes `<`, `>` and `&` by default.
- The upgrade drops the keys that schema 1 ignored, so a stored blob with
  unknown keys only gets smaller. "Normalization" above gives the reasons.
- The property test has a fixture at the boundary. It is a valid schema 1
  blob of exactly 102,400 bytes with 24 panels, and one query in it holds
  `<`, `>`, `&` and non-ASCII text. The test asserts that the upgraded blob
  passes the schema 2 validator and stays within the overhead bound.

A read-time upgrade is chosen over a store migration for four reasons.

- Migrations are forward-only here. A bad one on a live database means
  restoring a backup.
- The change restructures JSON. It would have to be written twice, in SQLite
  and Postgres SQL, and tested twice.
- Untouched blobs remain schema 1. An older binary can read them if the
  database is restored to its matching migration version. A direct binary
  rollback after phase 3 refuses the newer database version, as explained
  in "The dashboard list". Read-time upgrade alone promises no database
  downgrade path.
- The function is small and pure.

### Redaction

`RedactDashboardPanels` (`pkg/models/dashboards.go:239-261`) keeps its
contract for schema 1 blobs. A schema 2 function applies the same contract to
schema 2. For a locked panel it keeps `id`, `kind`, `title`, `team_id`,
`source_id` and `viz.type`. It drops `query`, the rest of `viz` and
`description`, and sets `locked`. Tab layouts stay. Text panels are never
locked. A variable's `options_source` is dropped when the viewer cannot reach
its source, and the input falls back to free text. `DashboardPanelRefs` skips
text panels.

Redaction is an explicit call in the read handler today
(`internal/server/dashboard_handlers.go:95-101`) and in the list
(`internal/core/dashboards.go:193`). A new endpoint that forgets the call
leaks. One function now builds every dashboard response. It picks the schema
for the client, upgrades, computes the locked panels from the caller's
current source access, redacts, and sets `can_edit`. From phase 17 it also
sets `can_delete`. The read, create, update, revision and export handlers
return its output. The list returns summaries from derived columns and no
stored blob. The corrupt GET response is the sole raw-blob exception, restricted to
creator and admins. No other response path returns a stored blob directly.
A test feeds a blob with a locked panel through every route that
returns panels and asserts that the query text is absent.

From phase 17 the locked set has no exemption for the creator. "Ownership and
permissions" gives the reason and the timing.

### Cache

The cache key gains one field, the byte budget. The first revision said that
the key needed no new field.

- The key holds the byte budget that the reader applies to the request.
  "Load bounds" gives the reason and the two fill orders that the tests
  cover. The field ships in phase 16, together with the request field that
  makes budgets differ.
- `count` panels use the histogram key, which already holds the window and the
  group-by field (`internal/server/histogram_handlers.go:45-65`).
- `result` panels use the logs key. It hashes the finalized query after
  substitution (`internal/server/logs_handlers.go:228-241`), so macros and
  variable values are part of it. The key also holds the team and the source,
  and the handler authorizes both before it computes the key
  (`internal/server/logs_handlers.go:182`). One viewer cannot read another
  viewer's entry for a source they cannot reach.
- The client still snaps a rolling range to the TTL bucket before it sends the
  request (`frontend/src/utils/dashboardRuntime.ts:48-67`). `{{__to}}` is
  therefore stable inside a bucket. Calendar ranges are not snapped.
- A `previous_period` comparison is a second request with its own entry.
- Variable values multiply entries. The existing byte and entry caps bound
  them (`internal/config/config.go:57-74`).
- A response above the entry size cap is not cached and streams instead
  (`internal/server/dashcache.go:141-167`). "Load bounds" covers the effect.
- Text panels and hidden tabs send nothing.

## API changes

| Endpoint | Change |
|---|---|
| Every dashboard route | Reads the `X-Logchef-Dashboard-Schema` and `X-Logchef-Dashboard-Features` request headers. Responses carry `revision` and `can_edit`, and `can_delete` from phase 17 |
| `GET /dashboards` | Summaries from phase 3, per "The dashboard list". No stored panel blobs. Constant compatibility fields for schema 1 clients, with the header absent or `1`. `collections` joins in phase 18 |
| `GET /dashboards/:id` | Valid rows return `panels` in the client's schema, per "Client schema contract". Corrupt rows return 200 only to creator or admins, with `corrupt: true` and opaque `raw_panels_json`, without `panels`. New `collections` list of `{ id, name, kind }` in phase 18 |
| `POST /dashboards` | Takes a schema 1 or schema 2 body. A schema 2 blob is stored with the `features` list that the server computes |
| `PUT /dashboards/:id` | The body must carry `revision`, for every schema. Without it the answer is 428. A stale revision is 409. A schema 1 body on a schema 2 row is 409. A stored feature that the writer did not declare is 409. A locked panel for a delegated editor is 403 |
| `DELETE /dashboards/:id` | Uses the delete predicate |
| `POST .../logs/query` | New optional `interval` and `max_response_bytes`. Macros. The LIMIT rule and the extra-row probe. `stats` gains `limit_source` and `truncation` (`none`, `rows`, `bytes`, `unknown`). Today's `truncated` and `truncated_reason` fields stay for current clients (`internal/clickhouse/query.go:218-221`). Variable type `day`. `timezone` is validated |
| `POST .../logs/histogram` | Macros, with `window` as the interval. Variable type `day` |
| `POST .../logchefql/query`, `.../logchefql/translate` | Variable type `day`. `timezone` is validated |
| `POST /collections`, `DELETE /collections/:id` | `POST` takes an optional `parent_id`. Both run under the tree lock |
| `PUT /collections/:id/parent` | New. Moves a collection. Takes `parent_id` and `drop_access` |
| `POST /collections/:id/members`, `POST /collections/:id/teams` | 409 on a child collection |
| `POST /collections/:id/items` | Omitted `kind` means `pin`. 201 for insert, 200 for an authorized replay of the same kind, 409 for a different stored kind. Never converts |
| `PUT /collections/:id/items/:queryID` | New. Requires `kind` and creator or admin authority. 200 for conversion or authorized replay, 404 for no link, 400 for invalid kind, 403 for a forbidden conversion. A global admin needs no participation |
| `DELETE /collections/:id/items/:queryID` | Atomic kind-conditioned removal. 200 envelope for success, 404 for no link, 403 for a managed link without creator or admin authority. Participation is required |
| `GET /admin/collection-links/report` | New. Lists the pins in shared collections, for global admins |
| `GET/POST /collections/:id/dashboards`, `PUT/DELETE /collections/:id/dashboards/:dashboardID` | New in phase 18. The same atomic writes, defaults, authority and response codes as saved-query links |
| `GET /dashboards/:id/revisions`, `GET .../revisions/:revision`, `POST .../revisions/:revision/restore` | New |
| The served `index.html` | Carries the server version in a `<meta name="logchef-version">` tag |

Token scopes stay as they are. The revision routes use the dashboard scopes.

## Storage migrations

Both stores get the same six migrations, each in the phase that needs it.
SQLite continues at 000039 and Postgres at 000013.

1. `add_collection_item_kind` (phase 1): `collection_items.kind`, `pin` or
   `managed`. On both stores its DDL is
   `TEXT NOT NULL DEFAULT 'pin' CHECK (kind IN ('pin', 'managed'))`.
   The same migration sets `managed` on the links
   that "Ownership and permissions" names. A link with a null `added_by`
   stays a pin. This is the SQLite form, and Postgres compares `is_personal`
   with `false`:

   ```sql
   UPDATE collection_items
   SET kind = 'managed'
   WHERE collection_id IN (SELECT id FROM collections WHERE is_personal = 0)
     AND added_by IS NOT NULL
     AND (added_by = (SELECT q.created_by FROM saved_queries q
                      WHERE q.id = collection_items.saved_query_id)
          OR added_by IN (SELECT id FROM users WHERE role = 'admin'));
   ```

2. `add_dashboard_revision` (phase 2): `dashboards.revision`, an integer that
   is not null and defaults to 1.

3. `add_dashboard_summaries` (phase 3) adds the following columns to
   `dashboards`. Every column is `NOT NULL`. The migration sets defaults
   only. The idempotent startup backfill completes before requests are
   accepted. It pages by id ascending in batches of 50 through
   `ListStaleDashboardSummaryInputs` and `RepairDashboardSummary`. The list
   uses the same methods to repair stale rows during rolling deployments.
   Create and update write the summary with the blob in the same statement.

   | Column | SQLite DDL | Postgres DDL |
   |---|---|---|
   | `panel_count` | `INTEGER NOT NULL DEFAULT 0` | `INTEGER NOT NULL DEFAULT 0` |
   | `tab_count` | `INTEGER NOT NULL DEFAULT 0` | `INTEGER NOT NULL DEFAULT 0` |
   | `panel_types` | `TEXT NOT NULL DEFAULT '[]'` | `TEXT NOT NULL DEFAULT '[]'` |
   | `panel_team_ids` | `TEXT NOT NULL DEFAULT '[]'` | `TEXT NOT NULL DEFAULT '[]'` |
   | `panels_corrupt` | `INTEGER NOT NULL DEFAULT 0 CHECK (panels_corrupt IN (0, 1))` | `BOOLEAN NOT NULL DEFAULT FALSE` |
   | `summary_revision` | `INTEGER NOT NULL DEFAULT 0` | `INTEGER NOT NULL DEFAULT 0` |
   | `summary_version` | `INTEGER NOT NULL DEFAULT 0` | `INTEGER NOT NULL DEFAULT 0` |

4. `add_collection_dashboards` (phase 18): `collection_dashboards(collection_id,
   dashboard_id, kind, sort_order, added_by, created_at)` with the primary key
   `(collection_id, dashboard_id)`, both foreign keys `ON DELETE CASCADE`, and
   an index on `dashboard_id`.
5. `add_collection_tree` (phase 19): `collections.parent_id`, nullable,
   referencing `collections(id) ON DELETE CASCADE`, with an index. Also
   `collections.root_id`, set to the row's own id for every existing row.
   SQLite cannot add a column that is both `NOT NULL` and a foreign key
   (from the SQLite `ALTER TABLE` rules, to be checked on the pinned driver),
   so `root_id` carries no foreign key. The `parent_id` cascade deletes a
   whole tree, so no `root_id` can point at a deleted row.
6. `add_dashboard_revisions` (phase 22): `dashboard_revisions(dashboard_id,
   revision, name, description, panels_json, created_by, created_at)` with the
   primary key `(dashboard_id, revision)` and the foreign key
   `ON DELETE CASCADE`.

Each needs `queries.sql` changes, `just sqlc-generate`, and conformance cases
in `internal/store/storetest/`.

## Frontend

Paths in the table are relative to `frontend/src/`. `components/` stands for
`frontend/src/views/dashboards/components/`.

| File | Change |
|---|---|
| `api/collections.ts`, `stores/collections.ts`, `views/library/CollectionDetailPane.vue`, `views/library/AllQueriesPane.vue`, `composables/useSavedQueries.ts` | Phase 1. Link response and item `kind` types. Creator or admin defaults to managed on shared adds, with the enabled toggle. Other adds are pins. Move preserves kind. Convert through PUT |
| `components/command-palette/catalog.ts` | Phase 3. Summary type change only at `frontend/src/components/command-palette/catalog.ts:153`. The palette reads only id and name |
| `views/dashboards/DashboardsList.vue` | Phase 3. Summaries, Duplicate fetch, corrupt action, bounded copy name. The empty-create fallback at `frontend/src/views/dashboards/DashboardsList.vue:189` becomes `{ version: 2, features: [], tabs: [{ id: "main", title: "Main", layout: [] }], panels: [] }` |
| `api/dashboards.ts` | Schema 2 types as discriminated unions. `DashboardSummary` for the list. The schema header and the features header, with the list of features that the build understands. `revision` on update |
| `api/apiUtils.ts` | `RequestOptions.headers` and forwarding into the outgoing request config. Tests inspect the transmitted dashboard headers |
| `utils/panelFrame.ts` (new) | `PanelFrame`, `frameFromHistogram`, `frameFromRows`, `columnKind`, `SeriesKey`, `suggestKinds`, `suggestRoles`, `suggestViz`, `duplicateKeys`, `reduceStat` |
| `utils/exactNumber.ts` (new) | Parse, add, compare, divide and format exact values. The decimal and exponent grammar for number strings |
| `utils/numberFormat.ts` (new) | `formatValue(value, format)`, `formatTime(value, format)`, `thresholdColor` |
| `utils/dashboardPanels.ts` | Per-tab layout, phase-gated limits, schema 2 validation. `trimDashboardGroupBy` and `dashboardCopyName` in phase 3 |
| `utils/dashboardRuntime.ts` | `resolveInterval`, `resolvePanelRange`, fetch signature, request key |
| `utils/timeWindow.ts` | The `today-<n>d` calendar form |
| `stores/dashboards.ts` | `fetchPanelFrame` replaces `fetchPanelData`. One request pool. Refresh by request key. Active tab, variable values, the frame budget with admission by distance from the viewport, undo stack |
| `components/PanelStat.vue` | Value, row date, delta, sparkline, format, thresholds, notices |
| `components/PanelChart.vue` (new) | Replaces `PanelTimeseries.vue`. Time and category axes on unovis |
| `components/PanelPie.vue`, `PanelTopList.vue` (new) | Split from `PanelBreakdown.vue` |
| `components/PanelTable.vue` | Takes a frame. Column formats and rules. Exact values |
| `components/PanelText.vue` (new) | Markdown through text bindings |
| `components/DashboardTabs.vue`, `DashboardVariablesBar.vue` (new) | Tab bar. Variable inputs that reuse the query editor's controls |
| `components/PanelBuilderDrawer.vue`, `PanelBuilderOptions.vue` | Mode and language toggles, visualization picker, role selects fed by the preview frame, kind selects with a marker on guessed kinds, format and threshold sections |
| App shell | Read the version stamp from the page. Compare it with `/api/v1/meta` on load and on focus. Show the reload banner on a mismatch |

`PanelStatus` gains `blocked` for a panel that waits on a variable, and
`released` for a panel whose frame the budget released. Value labels on
charts need the unovis label component, which should fit (inferred).

## CLI and MCP

Macros and variables are query-language features, so a query copied from a
panel must run from the CLI and from MCP. Their request types cannot carry the
inputs today.

| Surface | Today | Change, in phase 14 |
|---|---|---|
| CLI `SqlQueryRequest` (`cli/crates/logchef-core/src/api/models.rs:182-194`) | `query_text`, `limit`, `timezone`, `start_time`, `end_time`, `query_timeout` | Add `interval` and `variables`. `logchef sql` gains `--interval` and a repeatable `--var name=value` |
| CLI `HistogramRequest` (`cli/crates/logchef-core/src/api/models.rs:306-322`) | No variables | Add `variables` |
| MCP `QueryLogsParams` (`internal/mcp/logs.go:15-23`) | No `timezone`, `interval` or `variables` | Add all three |
| MCP query handler (`internal/mcp/logs.go:91-95`) | Passes the text, limit, timeout and range to `core.RunQuery` | Pass `Timezone`, `Interval` and `Variables` too |
| MCP `GetLogHistogramParams` (`internal/mcp/logs.go:30-40`) | No variables | Add `variables` |

Without these fields a macro query fails with a 400 from both surfaces,
because rule 5 of the macro contract rejects a missing input. The LIMIT rule
in phase 4 changes the output of `logchef sql` and of the MCP query tool with
no schema change. Their docs and the agent skill reference change in the same
PR.

The CLI has no dashboard commands today (none in
`cli/crates/logchef-cli/src/commands`). A script that calls the dashboard API
speaks schema 1 until it sends the schema header. From phase 2 it must send
`revision` with every update. To write a schema 2 dashboard it also sends the
features header.

## Tests and acceptance

- Fixtures are synthetic. The corpus never enters the repository. A local
  script replays it against a dev instance and reports the state of each
  panel.
- `pkg/models/dashboards_test.go`: one upgrade case per schema 1 panel type
  with a golden schema 2 blob, including a table with no limit and a table
  with a limit of 50,000. The upgrade of a schema 2 blob returns it unchanged.
  The property that every valid schema 1 blob upgrades to a valid schema 2
  blob within the overhead bound, with the boundary fixture. One validation
  case per rule above. `DashboardFeatures` for one fixture per feature, and
  the test that every field and enum value has a feature or the base mark. A
  blob with `viz.columns` that encodes to the same bytes after a decode.
  Schema 1 validation cases stay as they are.
- `internal/core/dashboards_test.go`, `internal/server/dashboard_handlers_test.go`:
  every cell of the read and write tables in "Client schema contract", and the
  stale-client matrix. Each row of the permission table. The locked-panel
  rule. Redaction on every route that returns panels.
- Phase 3 named consumer tests, with new files where needed.
  `frontend/src/views/dashboards/__tests__/DashboardsList.test.ts` covers
  Duplicate's by-id fetch, failure without a dialog, request ordering,
  hiding Duplicate for corrupt rows, schema 2 empty create, and the
  200-code-point localized copy name.
  `frontend/src/api/__tests__/dashboards.test.ts` captures actual outgoing
  schema and features headers through `apiUtils.ts`.
  `internal/server/dashboard_handlers_test.go` covers corrupt GET by creator,
  admin and other callers, including parseable invalid blobs. It covers
  absent and explicit `1` compatibility and invalid schema headers. Features
  cases cover trimmed names, blanks, duplicates, case sensitivity and
  unknown names that do not block a write.
  `internal/store/storetest/dashboard_summaries.go` defines shared cases
  registered by `internal/store/sqlite/conformance_test.go` and
  `internal/store/postgres/conformance_test.go`. They cover id-ordered batches
  of 50,
  an interrupted and repeated backfill, no second-run writes, validation
  failures, stale derivation versions, older-writer rows created after
  startup, list repair with normal team visibility, current rows without
  blob reads, and a concurrent revision change that loses the repair CAS.
- `internal/clickhouse/query_builder_test.go`: the LIMIT rule, `OFFSET` kept
  under a clamp, `LIMIT n BY`, a LIMIT inside a CTE, the extra-row probe.
- `internal/clickhouse` and `internal/cache`: the byte budget in the
  streaming reader, and the byte budget in the cache key.
- `internal/template/variables_test.go`: the `day` type and each rule of the
  macro contract in both dialects.
- `internal/store/storetest/`: revisions, link kinds, the tree with its three
  race cases, and the history table on both stores.
- vitest: `panelFrame`, `exactNumber`, `numberFormat`, `dashboardPanels`,
  `dashboardRuntime`, the dashboards store, and each panel component.
- Every PR runs `just check` and, for frontend changes, `bun run typecheck`
  and `bun run test`.

These checks gate their phase. Each one failed, or could not be stated, in an
earlier version of this RFC.

| Check | Phase | What proves it |
|---|---|---|
| Link authority | 1, 18 | The exploit sequence runs as a test for saved queries and for dashboards. A user with view access creates a collection, pins the item and tries to edit it. The edit returns 403 |
| Link races | 1, 18 | The four races in "Every link write is atomic" run on both stores with `-race`, separate connections and both lock acquisition orders. Conversion to managed cannot be followed by a successful participant delete. Same-kind POST replay preserves the stored link. Different-kind POST returns 409 and never converts |
| Link migration and recovery | 1 | A migration test holds one link of each origin: added by the creator, by an admin, by another member, by a deleted user whose `added_by` is null, and in a personal collection. The first two become managed and the others become pins. The report lists exactly the demoted links. A global admin who takes no part in the collection converts a link whose creator is deleted, and the log line exists. A member who removes a managed link gets 403. A write with no `kind` creates a pin |
| Required revision | 2 | A storetest case on both stores starts 20 saves with the same revision and asserts that exactly one succeeds. It runs with `-race`. A second case saves twice inside one second. In the two-stale-bodies case, two clients load revision N and send different bodies, one after the other and not at the same time. The first gets 200 and the second gets 409, and the stored blob is the first body. The same two bodies with no `revision` both get 428, and the row does not change |
| Version banner | 2 | A frontend test changes the version that `/api/v1/meta` returns and fires a focus event. The banner appears. With no stamp in the page, the check is off |
| Stale client write | 3, and every phase that adds a feature | The stale-client matrix from "Client schema contract". In the browser, a tab loaded from the previous release saves after the upgrade, shows the reload message, and the dashboard is intact |
| Dashboard list | 3 | The two 300-dashboard sets in "The dashboard list" have equally sized summary responses below 1 MiB despite near-limit blobs. Both stores backfill twice, with no writes on the second run. The list selects no `panels_json`. Duplicate fetches by id, and corrupt summaries hide Duplicate |
| Upgrade normalization | 3 | Every alias, empty option, null versus absent value and ignored-field fixture in "Normalization" upgrades to the expected canonical bytes and passes schema 2 validation. Invalid schema 1 values still fail. Unknown keys are dropped without re-encoding carried strings |
| Upgrade size | 3 | The boundary fixture from "Upgrade from schema 1" upgrades to a valid schema 2 blob within the overhead bound |
| Behavior kept in phase 3 | 3 | Screenshots of a dashboard with all four schema 1 panel types match `main`. The network log shows the same requests, including `limit: 100` for a table with no limit |
| No redaction gap | 3, 17, 22, 24 | One test feeds a blob with a locked panel through every route that returns panels and asserts that the query text is absent. Phase 3 runs it as a viewer. Phase 17 adds the creator and phase 18 the delegated editor |
| Truncation states | 4 | Four queries produce `none`, `rows`, `bytes` and `unknown`. The `bytes` case runs on VictoriaLogs, whose reader has the byte budget today. Phase 16 adds the ClickHouse case. A `LIMIT 5000 OFFSET 100` under a request limit of 100 keeps its offset |
| Exact numbers | 5 | vitest holds `9007199254740993` through a table cell, a tooltip, a sum and a threshold comparison. It holds a 30-digit decimal and a `UInt64` column that mixes numbers and strings. A string column with the cells `12`, `1.5e3`, `abc` and an empty cell keeps 12 and 1,500 exact, shows NULL for `abc` with a notice count of 1, and counts nothing for the empty cell. The bounds have one case each side of each limit: `0.` then 399 zeros then `1` is valid and one more zero is not, `1` then 400 zeros is valid and one more zero is not, `1e-400` is valid and `1e-401` is not, a 100-digit coefficient is valid and 101 digits are not, `1.` then 300 zeros equals 1, and `1e99999` is not valid. `0.` then 10,000 zeros then `1`, and `1` then 10,000 zeros, each become NULL with a notice count of 1, the other cells of the column stay exact, and the test asserts that neither literal reaches `BigInt`. In the browser, a panel over `SELECT toUInt64(9007199254740993) AS n` shows every digit |
| Column kinds round trip | 5 | An author confirms a VictoriaLogs column as a number and saves. The stored blob has the entry in `viz.columns`. After a reload the column is a number with the confidence `confirmed`, and a second save with no edit stores the same bytes. A blob whose `viz.columns` names a column that the frame lacks renders with a notice |
| Dates do not shift | 5 | A `Date` column shows the same calendar date for viewers at UTC-8 and at UTC+13 |
| Bucket alignment | 14 | A range across a daylight-saving change runs on ClickHouse and on VictoriaLogs. ClickHouse buckets start at local midnight on both sides. The VictoriaLogs panel shows its notice |
| Load bounds | 16 | The tab-switch and refresh-overlap tests, the frame budget test and the cache key test from "Load bounds" |
| Locked panels cannot be dropped | 17, 18 | A delegated editor sends a crafted update that omits a locked panel and gets 403. The stored blob is unchanged |
| Tree races | 19 | The three storetest cases from "Nested collections": opposite moves, create against move, and delete against move |
| Corpus mapping | 10 | The two tables below, replayed |

### Corpus mapping

The review mapped the dashboard cards to panels, and the first revision could
not read the corpus to check the result. This revision checked every row
against `cards.json` and `dash.json`. The check found four errors. Three
cards in the table are saved questions that sit on neither dashboard. Five
dashboard cards had no row. Card 286 does set a time format. The color
losses named the wrong cards.

Dashboard A has 4 cards. Dashboard B has 21 cards on the tabs "Current day"
and "Last 7 days".

| Cards | Where | Metabase display | Panel | Result |
|---|---|---|---|---|
| 226, 227, 234, 235 | A | Scalar | `stat` over `count(*)`, in `result` mode | Fits |
| 268, 270, 271, 274, 275, 302 | B, first tab | Trend | `stat` with `order_by` on the date column, `reduce: "last"` and `compare.mode: "previous_row"` | Fits. Only 271 has an `ORDER BY` |
| 584 | B, first tab | Trend | `stat`, value only | Fits with no comparison. The query returns only the latest date |
| 294, 301 | B, first tab | Bar | Bar `chart` with a category `x`, a value `y`, value labels and the original `LIMIT 20` | Fits, given the LIMIT rule |
| 286 | B, first tab | Table | `table` with a time format with seconds | Fits. The card also formats a column that its result lacks, and the import drops that setting |
| 287 | B, first tab | Table | `table` with a text rule of scope `row` and a time format with seconds | Fits. The rule color becomes a palette name |
| 619 | B, first tab | Table | The same as 287. The rule comes from the settings of the dashboard card, which replace those of the saved question | Fits. The rule color becomes a palette name |
| 320 | B, first tab | Table | `table` with a text rule of scope `cell` | Fits. The rule color becomes a palette name |
| 182 | B, first tab | Table | `table` with a subset of visible columns and a text rule of scope `cell` | Fits. The rule color becomes a palette name |
| 273, 278, 293, 303 | B, second tab | Bar | Bar `chart` with `x_scale: "category"`, value labels, the query's descending order and its original window | Fits. 273 and 278 lose a series color, and 278 loses its axis title |
| 277, 282, 304 | B, second tab | Area | Area `chart` with a category axis and value labels | Fits. 282 loses a series color, and 277 loses its axis title |

The table has all 25 dashboard cards.

The review also mapped three saved questions that sit on neither dashboard.
They stay in the check, because no dashboard card has their shape.

| Card | Metabase display | Panel | Result |
|---|---|---|---|
| 272 | Scalar | `stat` over the average that the query computes. It must not count rows | Fits |
| 285 | Bar | Stacked bar `chart` with three `y` columns, in the order of the card | Fits. The query has no `ORDER BY`, so the days follow the order of the result |
| 288 | Pie | `pie` over the label and value columns that the query returns, with `min_slice_percent: 1` | Fits. The card names a dimension and a metric that its result lacks, and the import drops them |

Phase 10 replays all 28 cards and records the result per card. Restoration is
done when every row of both tables reads "Fits" on a running instance.

## Docs and other surfaces

Each phase updates the surfaces it touches in the same PR.

- `docs/src/content/docs/features/dashboards.md` describes panel types, the
  builder, limits and permissions. The collections page gains dashboards, the
  two link kinds and nesting.
- Template docs and the agent skill reference gain the `day` type, the macros
  and the calendar range form.
- New UI strings go through `frontend/src/i18n`.
- `CHANGELOG.md` records these behavior changes:
  - Saved-query links become pins unless the creator or an admin added them.
    Only the creator or an admin removes a managed link. The entry names the
    report for admins.
  - A dashboard update without `revision` is refused with 428.
  - A dashboard saved by the new frontend is readable only by clients that
    send the schema header, and writable only by clients that declare its
    features.
  - The LIMIT rule on every surface.
  - The exact-number default on stat tiles.
  - Request variables cannot use a built-in macro name.
  - The creator of a dashboard sees a locked panel for a source they can no
    longer reach.

## Phase plan

Each phase is one PR, except phase 10, which is a check and changes no
product code. "Corpus" says what changes for the two migrated dashboards once
the phase ships.

| Group | Phases | Rule |
|---|---|---|
| Foundation | 1 to 3 | In that order. 1 closes a hole in released code. It depends on nothing and ships alone, as a security fix. 2 and 3 change the save contract |
| Restoration | 4 to 10 | 4 to 9 restore what the migration lost. 10 proves it on the corpus |
| After restoration | 11 to 28 | Each is independent unless it names a phase it needs. Phases 20, 21, 22 and 28 do not start before phase 10 passes |

The foundation does not preserve behavior as a whole. Two of its phases
change behavior on purpose, and the third changes the contract for clients
other than the new frontend.

| Phase | Kind of change | What a user or a client notices |
|---|---|---|
| 1 | Security fix | Some editors lose edit on a saved query until its link is converted |
| 2 | Contract change | An update without `revision` is refused. An open tab from before the phase must reload, and a script must send the field |
| 3 | No change for dashboards or for the new frontend. Contract change for other clients | A dashboard that the new frontend saved needs the schema header to read and the features header to write |

Phase 17 is the other security change. It ends the creator's exemption from
redaction, and it ships immediately before phase 18.

### Foundation

#### 1. Link kinds for saved queries

This phase is a security fix. It depends on no other phase and ships alone.

- Outcome: pinning a saved query into a collection no longer gives anyone
  edit rights on it. An admin can list the links that lost delegation and
  convert them.
- Changes: migration 1 with its backfill. `kind` on the item routes, with
  `pin` when it is omitted. The edit query counts managed links only, on both
  stores. The authority rules for creating, converting and removing a managed
  link, with the admin bypass and its log line. Atomic mutations and affected-row
  counts on both stores. `POST` replays only the same kind and never converts.
  `PUT` requires `kind` and owns conversions. The report endpoint and its
  view on the admin page for saved queries. The collection view shows the
  kind of each link, and the creator or an admin can switch it. The release
  notes, and the read-only query in the upgrade notes.
- Verify: the "Link authority" check for saved queries and the "Link
  migration and recovery" check. The four link races run on both stores,
  with both acquisition orders and separate connections. Handler tests cover
  every response in the `POST`, `PUT` and `DELETE` tables.
- Corpus: the migration created each saved query and its collection link
  with one account, so the links stay managed. This is inferred, and the
  report on the migrated instance confirms it.

#### 2. Required revisions and the version banner

This phase is a contract change. Every update must carry `revision`.

- Outcome: of two saves that start from one revision, at most one wins. A
  browser tab learns that the server was upgraded under it.
- Changes: migration 2. `UpdateDashboard` becomes a compare-and-swap on the
  client's revision on both stores and returns the new revision.
  `core.UpdateDashboard` runs in one transaction. Responses carry `revision`,
  and the frontend sends it back. The editor captures `editBaseRevision`
  when editing starts and sends that value on save. It never substitutes the
  current store revision at save time. A request with no `revision` gets 428. The
  `updated_at` precondition goes away. The version stamp in `index.html` and
  the reload banner in the app shell. Each focus check makes a fresh
  metadata request. It cannot reuse the initialized `loadMeta`, which returns
  early (`frontend/src/stores/meta.ts:91-94`). The release notes.
- Verify: the "Required revision" and "Version banner" checks. Handler tests
  for the 409 and the 428. Browser: two tabs edit one dashboard, and the
  second save shows the conflict message. A frontend test advances the store
  revision after editing starts and asserts that save sends `editBaseRevision`.
  The focus test initializes metadata first, then asserts a new request and
  a banner when the version changes. A tab from the previous release
  saves and shows the server's reload message.
- Corpus: no change.

#### 3. Blob schema 2, the read-time upgrade and the client contract

This phase changes nothing for existing dashboards or for the new frontend.
It is a contract change for scripts.

- Outcome: existing dashboards look the same and send the same requests. The
  frontend speaks schema 2. A stale tab or an old script cannot overwrite a
  schema 2 dashboard, and no later phase can reopen that hole.
- Changes: schema 2 structs for exactly what schema 1 can express, which is
  count-mode charts, stats, bar lists and pies, and result-mode tables.
  `UpgradeDashboardPanels` with the materialized defaults and the raw string
  moves. Schema 2 validation with the schema 1 limits, except the blob size,
  which is 512 KiB. Schema 2 redaction and the one response builder. The
  schema header and the read and write tables. `DashboardFeatures`, the
  stored `features` list, the features header and the feature check in the
  save transaction. The feature table starts empty, and the completeness test
  starts with it. The store runs schema 2 panels through the two existing
  fetch paths. Migration 3, the startup summary backfill, stale-row list
  repair and the summary-only list ship here, before the larger blobs reach the list. The API types,
  dashboard store, Duplicate fetch and corrupt action use summaries as
  "The dashboard list" specifies.
- Header transport: extend `RequestOptions` and request construction to
  forward custom headers (`frontend/src/api/apiUtils.ts:13-21`,
  `frontend/src/api/apiUtils.ts:36`). Test the outgoing request headers, not
  only the arguments given to the API helper.
- Capabilities: a missing features header is an empty set. A feature-free
  schema 2 row stays writable without it. Scripts declare the features their
  code supports. They never copy the stored blob's `features` into the header.
- Scope: the 428 and features header rules apply to dashboard writes only.
  Neither the CLI nor MCP writes dashboards or saved queries today
  (`cli/crates/logchef-core/src/api/mod.rs:521`, `internal/mcp/logs.go:273`).
  This phase adds no write support to those clients.
- Verify: the "Stale client write", "Upgrade size", "Upgrade normalization",
  "Dashboard list" and "Behavior kept in phase 3" checks. A feature-free row
  saves with no features header. A feature-using row fails without it. The
  transport test captures the schema header and declared features on actual
  outgoing requests. The "No redaction gap" check as a viewer. The model, core
  and handler tests. Browser: insert a schema 1 blob into `local.db`, open
  it, save, and confirm that `json_extract(panels_json, '$.version')` is 2.
- Corpus: both dashboards load through the upgrade with no edit.

### Restoration

Each phase from here on that adds state to the blob also adds its feature
names to the table in "Client schema contract" and to the list that the
frontend declares.

#### 4. LIMIT rule and truncation states

- Outcome: a table over `ORDER BY x DESC LIMIT 20` shows 20 rows. A result
  that was cut says so, and a result that was not cut does not.
- Changes: `ensureLimitWithPolicy` keeps a lower statement limit and keeps
  `OFFSET` under a clamp. The extra-row probe on ClickHouse and VictoriaLogs.
  `limit_source` and `truncation` in the response. The three callers of
  `BuildRawQueryWithLimitPolicy`: the query path
  (`internal/datasource/clickhouse_provider.go:343`), tail
  (`internal/datasource/clickhouse_tail.go:226`) and export
  (`internal/server/export_handlers.go:98`). The chip in the Explorer. CLI
  and MCP docs.
- Verify: the "Truncation states" check.
  `go test ./internal/clickhouse -run QueryBuilder`. Browser: run a
  `LIMIT 20` query in the Explorer with the limit set to 100.
- Corpus: charts 294 and 301 keep their `LIMIT 20`. The two top-20 tables no
  longer need a panel limit of 20.

#### 5. Result frames, exact numbers, computed stats, number format

- Outcome: a stat tile shows `sum(amount) / 1e6` as "12.40 M". Counts show
  every digit. Wide integers, decimals and number strings are exact. The
  builder offers SQL for ClickHouse sources.
- Changes: `panelFrame.ts`, `exactNumber.ts`, `numberFormat.ts`,
  `fetchPanelFrame`. Column kinds from declared types and from `viz.columns`,
  and date-only handling. The kind selects in the builder. `PanelStat` with
  `value`, `order_by`, `reduce` and the row date. `PanelTable` over a frame.
  The mode and language toggles, the format section and the truncation chip.
  The features `result_frames` and `formats`.
- Verify: the "Exact numbers", "Column kinds round trip" and "Dates do not
  shift" checks. vitest for frames, formats, the store and each reducer,
  with NULL, empty and tie cases. Browser: a stat over `SELECT count(*)`, one
  over a sum with a suffix, and one over a LogsQL stats query on the dev
  VictoriaLogs source with confirmed kinds.
- Corpus: the four scalar cards of dashboard A render as stats over their
  original `count(*)` SQL. The 8 rewritten stat tiles return to their
  original SQL. The 3 sum tiles change from tables to stats with 2 decimals
  and a suffix.

#### 6. Charts from result columns

- Outcome: bar, line, area, pie and bar-list panels over any result, with
  stored roles. Value labels on bars.
- Changes: `PanelChart`, `PanelPie`, `PanelTopList`. Role selects with
  suggestions. `suggestRoles`, `suggestViz`, the unique-key check, the
  50-series bound, `x_scale`, `min_slice_percent`. Count panels render
  through the same components, keyed by series key. The feature
  `result_charts`.
- Verify: component tests with a time frame, a category frame, a frame with
  a series column, a frame with duplicate keys, and a count frame that holds
  a real group named "Other", a NULL group, an empty group and the
  remainder. The existing chart tests move over. Browser: a bar chart of a
  category column in query order, a daily area chart, a stacked bar chart
  with three metrics, a pie.
- Corpus: the bar charts 273, 278, 293, 294, 301 and 303 and the area charts
  277, 282 and 304 render as charts from their original SQL. Seven of them
  group by date and show one bar or point per day, which closes gap 2 for the
  corpus. The saved questions 285 and 288 render as a stacked bar chart and
  a pie.

#### 7. Stat comparison and sparkline

- Outcome: a trend tile shows the latest value, its date, the change from
  the previous row with that row's date, and a sparkline.
- Changes: `compare.mode = "previous_row"`, `compare.good`, `sparkline`. The
  feature `stat_compare`.
- Verify: comparison tests for one row, for NULL on either side, for a
  previous value of zero, for a missing day, for a tie and for unordered
  input. Browser: a tile over a four-day series whose SQL has no `ORDER BY`.
- Corpus: cards 268, 270, 271, 274, 275 and 302 get their comparison back
  with their original SQL. Card 584 shows its value and no comparison.

#### 8. Thresholds, table rules and column formats

- Outcome: a stat turns red above a value. A table cell or row is highlighted
  when a column contains a word. A chart shows a reference line. A table
  shows timestamps with seconds.
- Changes: `thresholds`, `color_mode`, `rules`, `column_formats` with number
  and time formats, the palette tokens. The features `thresholds`,
  `table_rules` and `column_formats`.
- Verify: `thresholdColor` tests, with a step above 2^53. Rule tests for both
  scopes. Time format tests. Browser: both themes.
- Corpus: tables 287, 619, 320 and 182 regain their rules. Tables 286, 287
  and 619 show timestamps with seconds. Table 182 also gets its subset of
  visible columns. Schema 1 could already store that subset, and the
  migration left it out.

#### 9. Tabs and free grid sizes

- Outcome: a dashboard has tabs. A hidden tab runs no queries. The tab is in
  the URL. A panel can be any width from 2 to 12 and up to 12 rows tall.
- Changes: `DashboardTabs`, per-tab reflow and drag, moving a panel between
  tabs, lazy execution, the limit of 10 tabs, the wider layout rules. The
  features `tabs` and `free_grid`.
- Verify: layout tests per tab, a store test that a hidden tab sends no
  request. Browser: two tabs, reload on the second.
- Corpus: dashboard B gets its "Current day" and "Last 7 days" tabs back.

#### 10. Restoration check

- Outcome: a recorded result for every card on the two dashboards.
- Changes: none in the product. The replay script, which lives with the
  corpus outside the repository, builds both dashboards as schema 2 panels
  from the original card SQL and settings. It reads the settings of a
  dashboard card before those of its saved question. It loads the dashboards
  on a running instance and reports per card: the panel state, the
  visualization, the row count against the same SQL run directly, and every
  dropped or changed setting.
- Verify: every row of both tables in "Corpus mapping" reads "Fits". The SQL
  in each panel equals the card's SQL byte for byte. Screenshots of both
  dashboards.
- Corpus: this phase is the corpus.

### After restoration

#### 11. The `day` variable type

This phase depends on nothing and can ship at any point.

- Outcome: a "Date" variable renders `'2026-11-12'`. Existing "Date and time"
  variables render as before.
- Changes: `TypeDay` in `internal/template/variables.go`, the variable type
  unions and the date picker without time in the frontend, the saved-query
  variable sheet, the docs and the agent skill reference.
- Verify: template tests in three dialects. A test that a `date` value still
  renders with its time part. Browser: `toDate(ts) = {{d}}` runs with no wrap.
- Corpus: the 44 date queries can drop the `toDate()` wrap. Nothing breaks if
  they keep it.

#### 12. Text panels

- Outcome: Markdown notes and section headings on the grid. A description
  behind an info icon on any panel.
- Changes: `kind: "text"`, `PanelText`, the "Add text" action. The panel
  `description`. The features `text_panels` and `descriptions`.
- Verify: parser tests, including a script tag and a `javascript:` link that
  must stay literal text.
- Corpus: no change.

#### 13. Dashboard variables

- Outcome: a variables bar filters every panel that names the variable.
  Values are in the URL.
- Changes: `variables` in the blob, `DashboardVariablesBar`, `variables` on
  panel requests, the `blocked` state, `options_source` with its team and
  source checks. Option lookups go through the request pool. The feature
  `variables`.
- Verify: a store test that only panels using the variable refetch. Handler
  tests with variables on the histogram and query endpoints. A test that a
  value holding `{{__to}}` or `[[` stays literal text. Browser: one variable
  over a ClickHouse panel and a VictoriaLogs panel.
- Corpus: no dashboard in the corpus has filters. The "for a date" questions
  can now form a dashboard with one `day` variable.

#### 14. Macros, intervals and panel range

- Outcome: a count panel can use 24h buckets on a 7-day range. A new SQL
  panel follows the time picker through `{{__time_filter}}`. A panel can pin
  its own range, rolling or calendar. Panels show "fixed window", range and
  interval chips. The same query runs from the CLI and from MCP.
- Changes: `Builtins` in `internal/template`, expanded inside the existing
  scanner walk. `interval` on the query request. Timezone validation in
  `internal/core` for every endpoint. `prepareSQLQuery` and
  `prepareHistogram`. The reserved names. The `today-<n>d` range form.
  `resolveInterval`, `resolvePanelRange`. The daylight-saving notice on
  VictoriaLogs count panels. The request fields in "CLI and MCP". Macro
  support in the Explorer's SQL mode. The feature `panel_time`.
- Verify: one test per rule of the macro contract. The "Bucket alignment"
  check. Run the rendered SQL against the dev ClickHouse and the LogsQL forms
  against the dev VictoriaLogs, `{{__tz_offset}}` first. Run one macro query
  through `logchef sql` and through the MCP tool. Browser: change the range
  and watch a macro panel follow it while a fixed panel keeps its chip.
- Corpus: no corpus query changes. The corpus panels keep the windows that
  their SQL sets, and gain the "fixed window" chip.

#### 15. Drill-down and cross-filter

- Outcome: a click on a series opens its logs. A click on a bar can set a
  variable. A drag zooms the range. "Open in Explorer" on every panel.
- Changes: `click`, the brush handler, `panelExploreQuery` for series keys
  and bucket windows. The feature `click`.
- Verify: `panelExplore` tests, with a real group named "Other", the NULL
  group and the remainder. Browser: click through to the Explorer and compare
  the row count with the bar.
- Corpus: no change is needed. The two top-20 bar charts can set a variable
  once the dashboard defines one.

#### 16. Higher panel limit and load bounds

- Outcome: a dashboard holds 60 panels and stays responsive.
- Changes: the one request pool, refresh by request key, the hidden-tab rule.
  `max_response_bytes` on panel requests, the byte check in the streaming
  reader, and the byte budget in the cache key with `keyVersion` 2. These
  three ship in one PR. The frame budget with admission by distance from the
  viewport, the `released` state and its Reload button. The "not cached"
  chip. Then the panel limit.
- Verify: the "Load bounds" check. Measure a 60-panel dashboard on the dev
  instance: peak requests in flight, retained frame bytes, the heap that
  those frames take, time to the last panel. Record the numbers in the PR.
- Corpus: no change. Dashboard B has 21 cards.

#### 17. Authorization predicates, locked panels and the end of the creator exemption

This phase is a security change, and it ships immediately before phase 18.
Phase 18 is the first phase that lets someone other than the creator edit a
dashboard. The exemption leaks nothing before that.

- Outcome: delete has its own rule. A save cannot destroy a panel that the
  caller cannot see. The creator sees a locked panel for a source they can no
  longer reach.
- Changes: the three predicates. The delete handler. The locked-panel rule in
  the save transaction. The creator exemption goes away. `can_delete` in
  responses. The "Remove N locked panels" action.
- Verify: the "No redaction gap" check as the creator, and the "Locked panels
  cannot be dropped" check at the level this phase can reach. Core tests per
  predicate. Browser: a creator who left a team opens their dashboard.
- Corpus: no change.

#### 18. Dashboards in collections

Needs phases 1 and 17.

- Outcome: the Library shows dashboards next to saved queries. The creator
  or an admin can let a collection's editors edit a dashboard. A pin grants
  nothing.
- Changes: migration 4, the dashboard link routes with `kind`, the view and
  edit queries, the list query for ids reachable through managed links, the
  Library rail, a collection filter on the dashboard list. The dialog that
  creates a managed link says that the collection's owners can appoint
  editors.
- Verify: the "Link authority" and "Locked panels cannot be dropped" checks
  for dashboards. The "No redaction gap" check as a delegated editor.
  Storetest on both stores. Core tests for each row of the permission table.
  Browser: an editor edits, a member cannot, a pin changes nothing.
- Corpus: an admin adds both dashboards to the collections that hold their
  queries, as managed links.

#### 19. Nested collections

Needs phase 18.

- Outcome: collections form a tree. Access comes from the root.
- Changes: migration 5. Root resolution in every authorization query on both
  stores. `LockCollectionTree`, taken by moves, by child creation, by
  collection deletion and by member and share writes. The checks that each
  of them repeats under the lock. The move endpoint with `drop_access`. The
  409 for member and share writes on a child. The tree in the Library rail.
- Verify: the "Tree races" check. One storetest case per query in the table
  of "Nested collections". Core tests for inherited roles, the depth limit,
  each move in the move table and the personal-tree rule.
- Corpus: the five "Parent / child" collections become children of one
  parent. Their own member and share rows are dropped in the move, and the
  parent's rows apply.

#### 20. Explicit combine and series overrides

Waits for phase 10.

- Outcome: an author can choose how rows with one key combine. A chart can
  mix marks and use a second axis.
- Changes: `viz.combine`. `overrides` with `mark`, `color` and `axis` per
  series. The feature `combine`.
- Verify: combine tests per rule, and a test that the default is still an
  error. Component tests for a bar and line combo with two axes.
- Corpus: no change.

#### 21. Previous-period comparison

Waits for phase 10. Needs phase 14.

- Outcome: a stat compares its window with the window of equal length before
  it.
- Changes: `compare.mode = "previous_period"`, for time-bound queries only.
  The second request is a task in the request pool with its own cache entry.
  The feature `compare_period`.
- Verify: tests that the second window ends where the first starts, for
  rolling and calendar ranges. A store test that the comparison never runs
  beside the pool.
- Corpus: no change. Every corpus panel has a fixed window.

#### 22. Revision history

Waits for phase 10. Needs phases 2 and 17.

- Outcome: a history list with restore, redacted for each reader.
- Changes: migration 6, the insert and prune in the save transaction, the
  revision routes through the response builder, the history sheet.
- Verify: the "No redaction gap" check on the revision routes, with a
  revision that names a source the current blob no longer names. Storetest
  for the 50-revision cap. A handler test that a restore fails when a
  referenced source is gone or locked.
- Corpus: no change.

#### 23. Add to dashboard from the Explorer

- Outcome: the Explorer can add its current query to a dashboard as a panel.
- Changes: the action, a dashboard and tab picker, the mapping from Explorer
  state to a panel.
- Verify: a mapping test per query language. Browser: add a query, open the
  dashboard, open the panel back in the Explorer.
- Corpus: no change.

#### 24. Export and import

Needs phase 17.

- Outcome: CSV per panel. Dashboard JSON export and import.
- Changes: CSV from the held frame, with exact values. JSON export from the
  read endpoint. Import through the create endpoint, which refuses locked
  panels.
- Verify: the "No redaction gap" check on export. A round trip of export and
  import gives an equal blob, with `viz.columns` and `features` in it. A CSV
  of a wide integer keeps every digit.
- Corpus: no change.

#### 25. Keyboard shortcuts, undo and redo

- Outcome: shortcuts (`e` edit, `r` refresh, `Cmd+S` save, `Esc` cancel, `d`
  duplicate, `Delete` remove). Undo and redo in edit mode.
- Changes: the key handler and the undo stack in the store.
- Verify: store tests for undo and redo. Browser for the shortcuts.
- Corpus: no change.

#### 26. Link state and kiosk view

- Outcome: a link for signed-in users carries tab, range and variables. A
  kiosk view hides the chrome.
- Changes: range in the URL next to the tab and the variables, the kiosk
  route.
- Verify: a router test that a link restores tab, range and variables.
  Browser: open a link in a second session.
- Corpus: both dashboards can run on a wallboard.

#### 27. Saved refresh default and cache age

- Outcome: a dashboard saves its default refresh. A panel shows the age of a
  cached result.
- Changes: `default_refresh_seconds`, the chip from the `Age` header. The
  feature `saved_refresh`.
- Verify: a validation test and a store test. Browser.
- Corpus: both dashboards get a saved refresh default for wallboard use.

#### 28. Alert from a panel

Waits for phase 10.

- Outcome: "Create alert" on a count panel opens the alert form with the
  query, the threshold and a lookback filled in.
- Changes: frontend only, for LogchefQL count panels. SQL result panels need
  macro support in alert evaluation, which is a separate change (inferred).
- Verify: a mapping test. Browser.
- Corpus: no change. Every corpus panel is SQL.

## Alternatives

### A server endpoint that runs a panel by dashboard id

The server would load the panel, check access and run it. Public links and
scheduled reports need this. It is rejected for now, because it adds a second
authorization path next to the team-scoped endpoints. Revisit it together with
public links.

### Drop count mode and compile LogchefQL to aggregate SQL

One mode is simpler to explain. It is rejected because VictoriaLogs has no
SQL, and the histogram path already handles top-N folding and bucket budgets
on both backends.

### Rewrite stored blobs in a store migration

Rejected under "Upgrade from schema 1".

### Upgrade in the frontend

The API would serve two shapes with no rule for which client gets which, and
validation would need the upgrade in Go anyway. Rejected.

### Upgrade a schema 1 write and store schema 2

This was the first draft. A stale tab could then save its old copy over a
dashboard that had gained schema 2 state, and a schema 1 script could no
longer read back what it had just written. Rejected under "Client schema
contract".

### Write against the revision that the transaction read

This was the first revision, for a request with no `revision`. The
compare-and-swap then proves only that no write landed during the
transaction. Two clients with stale copies can still save one after the
other, and the second replaces the first. Rejected under "Revisions".

### A new schema number for every feature

Each phase that adds blob state could raise the schema number. Every client
would then need the newest number to write any dashboard, including one that
uses none of the new state. A script would break at every release. Feature
names refuse only the writes that would drop something. Rejected under
"Client schema contract".

### Declare the writer's features in the request body

Rejected under "Client schema contract". A script that reads a dashboard and
writes it back would echo the declaration.

### A build constant for the frontend version

The frontend could compile its version in. Three build paths set the server
version (`Justfile:4`, `:12`, the `Dockerfile` and `.goreleaser.yaml:30`),
and the frontend stage of the Docker build has no version input
(`Dockerfile:7-22`). A constant that one path sets wrongly would show the
reload banner forever. The server stamps its own version into the page, and
that stamp cannot disagree with the server. Rejected.

### Reload the page automatically, and send the version on every response

The first revision had both. The reload can discard an open edit, and the
header adds a second source of the version next to `/api/v1/meta`. The server
refuses every unsafe save, so the banner is enough for now. Deferred.

### Downgrade schema 2 for schema 1 clients

The server could convert a schema 2 blob back to schema 1 when the blob uses
only schema 1 features. Old scripts would then keep reading dashboards that
the new frontend saved. It is rejected because it adds a second converter
that every later phase must keep correct, and a script can send the schema
header.

### Edit rights through any collection that holds the dashboard

This was the first draft, and it is the blocker. Rejected under "Ownership
and permissions".

### One home collection per dashboard

A dashboard would live in exactly one collection, as in Metabase and Grafana,
and pins would be a separate relation. "Who can edit this" would then have
one answer per dashboard. It is rejected for now. Saved queries already sit
in several shared collections with delegated edit, and a single home needs a
product decision on which of them becomes the home. The link kind closes the
hole with one column on the model that exists.

### Sum rows that share a key, and fold small result series into "Other"

Rejected under "Rows that share a key".

### Find column types by sampling values

Rejected under "Column type, kind and confidence".

### Turn a whole number column into doubles when one cell fails to parse

This was the first revision, for VictoriaLogs. One bad row then removed
exactness from every other row, and a tooltip could show digits that the
backend never sent. Rejected under "Exact numbers".

### Release frames of hidden tabs only

This was the first revision. A dashboard with one tab has no hidden tab, so
60 panels at the response budget would retain 240 MiB. Rejected under "Load
bounds".

### A dry run of the link migration before the upgrade

A flag on the server binary could list the links that the migration demotes,
before the migration runs. It needs a second way to open each store, one that
skips migrations and the prepared statements for the new schema. The report
after the upgrade loses nothing, because the migration changes only `kind`.
Rejected for now. "Open questions" keeps it visible.

### Keep delegation on every existing saved-query link

The migration would then change nothing for editors, and the hole would stay
open on every link that exists today. Rejected under "Ownership and
permissions".

### Keep timestamps for conflict detection, at a finer resolution

Millisecond timestamps make a same-second collision rare. The check would
still read and then write, so two saves could still both pass. Rejected under
"Revisions".

### Rewrite the corpus SQL into macros

`{{__time_filter}}` with a rolling range of `10d` is not the calendar window
that the corpus queries ask for. Rejected under "Time".

### Panels that reference saved queries

Metabase cards point at saved questions, so one edit updates every dashboard.
It is rejected. A panel that embeds its query keeps the blob self-contained,
which makes revision history, duplication and export complete, and leaves no
dangling reference when a query is deleted. The builder offers "Start from a
saved query", which copies the text.

### A chart grammar such as Vega-Lite

A grammar covers more chart kinds. It is rejected because its validation has
no clear bound, it adds a dependency, and the role model covers the corpus.
unovis is already in use.

### Render date-only values without a new type

The `date` type could render `'YYYY-MM-DD'` whenever the value has no time
part. It is rejected because it changes output for API clients that send
date-only values today. Rendering every date as `toDateTime(...)` is also
rejected. It changes the SQL of every existing query and breaks functions that
take a string.

### A `team_id` on dashboards

Rejected under "Ownership and permissions".

### Function macros such as `$__timeFilter(column)`

The template grammar has only `{{name}}` (`internal/template/variables.go:36`).
`{{__time_filter}}` covers the source's timestamp column. `{{__from}}` and
`{{__to}}` cover every other column.

## Open questions

### Resolved

1. Public links and embedding. Resolved: not in v2. v2 ships links for
   signed-in users that carry tab, range and variables, plus a kiosk view
   (phase 26). Public access exposes log data and needs the server-run
   endpoint.
2. The LIMIT rule. Resolved: it applies everywhere, to dashboards, the
   Explorer, the CLI and MCP. The release notes call it out (phase 4).
3. Ownership. Resolved: dashboards live in collections. Dashboards get no
   `team_id`.
4. Nesting. Resolved: collections nest to a depth of 4. Access is set on the
   root and inherited.
5. Panels and saved queries. Resolved: a panel copies a saved query and does
   not reference it.

The first revision raised questions 9 to 12 with a recommendation each. They
are now resolved as recommended.

9. Who may create a managed link. Resolved: the creator of the item and
   global admins only. The wider option let anyone with edit rights delegate
   further, so the creator would no longer bound who can edit.
10. Existing saved-query links. Resolved: fail closed. A link becomes a pin
    unless the query's creator or an admin added it. Some editors lose edit
    on queries that a teammate pinned for them. The release notes say so, and
    an admin can list and convert those links (phase 1).
11. The creator's exemption from redaction. Resolved: remove it. A creator
    who left a team then sees locked panels on their own dashboard and can
    only remove them. The removal ships in phase 17, immediately before other
    people can edit, because the exemption leaks nothing until then.
12. Access rows when a root collection moves under another root. Resolved:
    delete them after an explicit confirmation. Refusing the move makes the
    owner clear the rows by hand, and merging the rows widens the new root
    without a decision by anyone.

Numbers 6 to 8 and 13 to 16 are the open questions below.

### Open, carried over from the first draft

6. Colors as palette names, not hex values. Per-series hex colors from an
   import are dropped, and a rule color maps to the nearest palette name.
   Recommendation: palette names.
7. Stat tiles on existing dashboards change from 237.5K to 237,497 once
   phase 5 ships. Recommendation: accept the change. It is the reported
   defect.
8. A collection editor who cannot reach one panel's source.
   Recommendation: the dashboard is read-only for them. The server now
   enforces it.

### Open, raised by the first revision

13. Old scripts. A script that updates dashboards needs three small changes.
    It sends `revision` with every update (phase 2). It sends the schema
    header to read a dashboard that the new frontend saved, and the features
    header to write one (phase 3). Recommendation: accept it. Each change is
    one line in a script, and the release notes list them.
14. The end of schema 1 writes. Recommendation: remove them two minor
    releases after schema 2 ships, and announce it in the first of those
    releases.
15. The calendar range `today-<n>d` adds a form to the time picker.
    Recommendation: add it in phase 14. Day-bucket charts are wrong on a
    rolling range.

### Open, raised by this revision

16. A list of the affected saved-query links before the upgrade. Migrations
    run at startup, so the report endpoint works only after the migration.
    The upgrade notes carry a read-only query for an operator who wants the
    list first. The other option is a dry-run flag on the server binary, at
    the cost of a second way to open each store. Recommendation: ship the
    endpoint and the query, and add no flag. The migration changes only
    `kind`, so nothing is lost by reading the report after the upgrade.
