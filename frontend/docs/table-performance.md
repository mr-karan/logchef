# Explorer table performance

## September 2026 comparison

Baseline: `56acbfc4` on main. Candidate: `perf/explorer-rendering`.
Both production frontend builds used the same local backend and ClickHouse data.
The browser ran on Linux without CPU throttling. Each build used a fresh browser
process, with two batches of six repetitions per interaction.

The query returned 1,000 synthetic logs with ten data columns, including a map
column. Table view displayed 50 rows per page. No filter or expanded row was
active at the start. Times include the DOM update and two animation frames;
these are local interaction benchmarks, not field INP measurements.

| Median, milliseconds | Main | Candidate |
| --- | ---: | ---: |
| Next page | 148.5 | 59.5 |
| Previous page | 171 | 63 |
| Apply text filter | 50 | 33 |
| Clear text filter | 142 | 67 |
| Expand row | 36 | 33 |
| Collapse row | 47 | 33 |

Total page DOM elements dropped from 9,605 to 3,555. Idle table action buttons
dropped from 1,400 to zero. Hovering or focusing one ordinary cell creates its
three action buttons. Keyboard focus keeps them mounted when the pointer leaves.

The largest change removes permanently mounted copy/filter controls from every
cell. Query result arrays also opt out of deep reactivity at the two successful
query-response boundaries. A computed editor schema avoids allocating the same
schema object on unrelated Explorer renders. These smaller changes have not
been assigned independent speedup percentages.

Timing varies with hardware, GC, data shape, and browser state. Earlier runs were
slower for both builds. Compare paired runs rather than treating these values as
performance budgets or production guarantees.

## Reproduce

1. Build main and the candidate with `bun install --frozen-lockfile` and
   `bun run build`. Serve each build with access to the same local backend.
2. Create a local ClickHouse source using the default auto-created schema.
3. Insert 1,000 synthetic logs into that source's table:

   ```sql
   INSERT INTO default.perf_logs
     (timestamp, severity_text, service_name, body, log_attributes)
   SELECT
     now64(3) - toIntervalMillisecond(number),
     if(number % 2 = 0, 'INFO', 'ERROR'),
     'perf-review',
     concat('Synthetic log message ', toString(number), ' request processed successfully'),
     map('request_id', toString(number), 'component', 'worker',
         'details', repeat('sample ', 30))
   FROM numbers(1000);
   ```

4. In a fresh named `agent-browser` session, authenticate to the local app.
   Select this source, a time range containing the fixture, and a 1,000-row
   result limit. Run the query. Confirm the result count is 1,000.
5. Select Table view, show all ten columns, and use 50 rows per page.
   Start on the first page with no table filter and all rows collapsed.
6. Run the script twice, saving its JSON output:

   ```sh
   agent-browser --session table-perf eval --stdin \
     < frontend/scripts/benchmark-table.js
   ```

7. Close the browser and repeat for the other build. Compare medians across
   both batches. Do not run both benchmarks concurrently.

Also check hover, keyboard focus, copy, filter drill-down, row expansion,
timezone changes, and switching between table, compact, and JSON views.

## Follow-up changes

Query responses now include compilation conditions and fields used. Explorer
uses that metadata from the accepted execution snapshot rather than making
another translation request. The snapshot preserves the executed query when
the user edits the query, mode, or limit while the request is in flight.
ClickHouse streaming and VictoriaLogs buffered responses include the metadata,
including dashboard cache fills.

Column definitions compile literal highlight patterns once. Their watcher now
tracks highlight patterns, query fields, and severity fields in addition to
schema and timezone changes.

A browser microbenchmark of the existing timestamp formatter took a median of
0.5 ms per 1,000 UTC timestamps and 0.8 ms per 1,000 local timestamps. The local
browser timezone was Asia/Calcutta. Each mode used 15 batches of 1,000 distinct
ISO timestamps through the Vite-served module, without CPU throttling.
This measures formatting only, not table rendering or end-to-end query time.
These measurements do not justify a timestamp-format cache.

Reproduce on the local Vite frontend:

```sh
agent-browser --session table-perf eval --stdin \
  < frontend/scripts/benchmark-timestamps.js
```

Timestamp sorting separately compares UTC nanosecond keys. It handles
variable-width fractions and timezone offsets without rounding to milliseconds.
Invalid or missing timestamps compare before valid timestamps in ascending order.

The production-build comparison above measures the initial DOM reduction.
It has not been rerun for these follow-up changes.

PR #93 is a design reference. Its old compact-view virtualization and manual
pagination code were not imported; current main already limits rendered rows
through pagination.

## Wide schemas (issue #106)

VictoriaLogs sources expose a different field set on every query. Two table
behaviors made a 200-plus field source unusable. Both changed together.

Saved table state defaulted every column it had not seen before to visible, so
each query added its new fields as visible columns and persisted them. The
6-column default for schemaless sources only applied to the very first query.
`resolveColumnVisibility` now keeps saved entries for columns absent from the
current result and lets unseen columns take their default only while the total
visible count stays under the cap. Explicit user choices are never overridden.

Every cell mounted a `CellWithActions` component with four listeners. Hover and
focus are now tracked with delegated `mouseover` and `focusin` listeners on
`<tbody>`, and only the active cell mounts its action buttons.

Body rows render only the columns under the horizontal viewport plus three
overscan columns on each side. Hidden columns on each side collapse into one
spacer `<td>` with a matching `colspan` and explicit width. The header stays
fully rendered, so `table-layout: fixed` still takes widths from it. An
unmeasured container initially uses a provisional 1024px viewport. This bounds
the first mount too: waiting for measurement after rendering every column could
freeze the renderer before the observer runs, especially with older saved state
that explicitly enables all columns.

The body constructs TanStack cells only for the current page's viewport columns.
Calling `row.getVisibleCells().slice(...)` would first allocate cells for every
schema column (including hidden columns), retaining them in TanStack's row
cache. Auto-fit reads row values directly and expanded-row colspan uses the
visible column count, so neither operation populates that cache either.
The viewport cell map is replaced on page/range changes rather than retaining
cells from previously visited pages or horizontal ranges.

Saved table state carries a `visibilityVersion`. Versions before the cap saved
every field a schemaless source ever returned as visible, so for VictoriaLogs
the table discards saved visibility without the current version and falls back
to the default 6 columns. Column order and widths are kept. The table persists
only after the source type is known, so untrusted visibility is never stamped
with the current version.

The table was not the only component that mounted the whole source schema. The
Fields sidebar rendered one disclosure per schema field and auto-loaded values
for every priority field. It now renders 100 fields at a time with a "Show
more" control, and search still covers every field. Field values load only for
rendered fields; growing the list or searching loads the new fields and keeps
the rest. A new query, source, or reopened panel resets them.

The Group by picker used Reka `Select`, which mounts every option even while
closed and registers each option with a linear scan, so a 3000-field schema
cost seconds before the picker was ever opened. It now uses `SearchableSelect`,
which mounts nothing while closed and renders the first 100 matches, with a
hint to type to narrow the list.

### Measurements with the sidebar and Group by changes

Medians of 5 runs of `frontend/scripts/benchmark-wide-schema.ts` (see Reproduce):
Chrome via agent-browser, a fresh tab per run, `Performance.getMetrics` after
`HeapProfiler.collectGarbage`, production builds served by `vite preview`,
Fields sidebar open, `limit=1000`. Baseline is main at `ebb896da` and the first
table-only commit `0aeb817e`.

Scenario C is the reported case: table state saved by v2.0.2 with all 267
fields of the `env="perf"` source visible, then 1000 rows per page.

| | main | `0aeb817e` | final |
| --- | ---: | ---: | ---: |
| visible columns | 267 | 267 | 6 |
| first rows | 3.1 s | 2.3 s | 0.7 s |
| switch to 1000 rows per page | 17.0 s | 0.8 s | 0.4 s |
| DOM nodes at 1000 rows | 2,308,984 | 126,034 | 62,650 |
| JS event listeners at 1000 rows | 1,138,340 | 16,950 | 2,900 |
| JS heap used at 1000 rows | 1,762 MB | 192 MB | 73 MB |
| open Group by | 8.0 s | 0.6 s | 0.09 s |

Scenario D is the `env="perf-xl"` source (3006 field names) with fresh table
state.

| | main | `0aeb817e` | final |
| --- | ---: | ---: | ---: |
| first rows | 41.8 s | 43.8 s | 1.6 s |
| sidebar field rows mounted | 3006 | 3006 | 99 |
| DOM nodes at 1000 rows | 320,290 | 303,490 | 62,660 |
| JS event listeners at 1000 rows | 105,884 | 80,694 | 2,900 |
| JS heap used at 1000 rows | 833 MB | 816 MB | 94 MB |
| open Group by | 3.7 s | 3.6 s | 0.09 s |

With all 267 fields chosen in the current version (`chosen-all-visible`), the
final build keeps every column: 109,780 DOM nodes and 122 MB of heap at 1000
rows, and the switch to 1000 rows per page takes 0.7 s.

### Measurements of the table-only commit

Fixture: 6000 lines across four VictoriaLogs streams, 88 fields per stream and
268 distinct fields for `env="perf"`. Chrome via agent-browser, metrics from
`Performance.getMetrics` after a forced garbage collection, 1000 rows rendered.
Baseline is main at `ebb896da`. Interaction times are local measurements from
click to two animation frames later, not field INP.

Scenario A is the issue as reported: fresh table state, query one stream, then
query `env="perf"`.

| | main | fix |
| --- | ---: | ---: |
| visible columns after the second query | 186 | 6 |
| DOM nodes | 1,543,646 | 77,224 |
| JS event listeners | 757,457 | 9,775 |
| JS heap used | 1,228 MB | 139 MB |

Scenario B is the worst case by choice: all 268 columns selected in the column
picker, 1000 rows per page, paging between two pages of a 2000-row result.

| | main | fix without column virtualization | fix |
| --- | ---: | ---: | ---: |
| rendered body cells | 268,000 | 268,000 | 9,000 to 12,000 |
| DOM nodes | 2,208,362 | 1,403,090 | 212,923 |
| JS event listeners | 1,087,427 | 14,256 | 23,243 |
| JS heap used | 1,710 MB | 958 MB | 313 MB |
| next page, p50 | 58.1 s | 23.1 s | 1.2 s |
| next page, p95 | 103.0 s | 43.6 s | 3.0 s |
| renderer crashes | yes, at ~9.5 GB RSS | yes, on the second iteration | none |

The main and intermediate paging rows come from 2 of 10 requested iterations,
because longer runs crashed the renderer. The final column ran all 10.

### Reproduce

1. Ingest both fixtures into the dev VictoriaLogs at `:9428` with
   `just dev-ingest-wide`. `env="perf"` has 267 field names;
   `env="perf-xl"` has 3000 attribute names spread over ten streams so that no
   VictoriaLogs block exceeds its limit of 2000 unique field names.
2. Add two VictoriaLogs sources for `http://localhost:9428`, scoped with
   `{env="perf"}` and `{env="perf-xl"}`, and link them to a team.
3. Serve each build with `bunx vite preview --outDir <dist> --port <port>`, log
   in once, and pass the browser's CDP URL (`agent-browser get cdp-url`) to
   `frontend/scripts/benchmark-wide-schema.ts`:

   ```sh
   bun frontend/scripts/benchmark-wide-schema.ts --cdp "$CDP_URL" \
     --origin http://localhost:4173 --team 1 --source <id> \
     --scenario legacy-all-visible --runs 5
   ```

   Each run opens a fresh tab, writes the scenario's saved table state, loads
   Explore with `limit=1000`, and records first rows, memory after a forced
   garbage collection, the switch to 1000 rows per page, and opening Group by.
   The script prints each run to stderr and the medians to stdout.
4. For scenario A, clear `localStorage` keys starting with `logchef-tableState`,
   run `service="wide-a"` then `env="perf"`, and count `thead th`. For scenario
   B, tick Select All in the column picker, set 1000 rows per page, and time
   `Go to next page` and `Go to previous page` over 10 iterations with
   `limit=2000`.
