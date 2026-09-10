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

## Further candidates

- Reuse compilation metadata from query execution instead of making the
  Explorer's additional post-execution translation request. Cover variables,
  cached responses, and stale requests before changing that flow.
- Compile per-column highlight patterns once when column definitions change.
  First correct the watcher dependencies for highlight/query-field changes.
- Profile timestamp formatting after the DOM reduction. Add a bounded cache
  only if repeated formatting remains significant.

PR #93 is a design reference. Its old compact-view virtualization and manual
pagination code were not imported; current main already limits rendered rows
through pagination.
