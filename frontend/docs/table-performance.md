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
unmeasured container renders every column, which keeps jsdom tests unchanged.

### Measurements

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

1. Ingest a wide fixture into the dev VictoriaLogs at `:9428` with
   `/insert/jsonline?_stream_fields=service,env&_time_field=timestamp&_msg_field=message`:
   four services, 20 shared fields, 60 service-specific fields each.
2. Open the VictoriaLogs Demo source in Table view with `limit=1000`, clear
   `localStorage` keys starting with `logchef-tableState`, run
   `service="wide-a"` then `env="perf"`, and count `thead th`.
3. Tick Select All in the column picker, set 1000 rows per page, and read
   `Performance.getMetrics` over CDP after `HeapProfiler.collectGarbage`.
4. Re-run with `limit=2000` and time `Go to next page` and `Go to previous page`
   over 10 iterations.
