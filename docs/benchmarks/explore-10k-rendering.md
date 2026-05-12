# Explore 10k Rendering Benchmark

Date: 2026-05-07

This benchmark compares the Explore compact log list on `main` with the
`chore/explore-perf` working tree. The goal is to measure browser-side behavior
when the user runs a 10k-row query, not raw ClickHouse throughput.

## Summary

The backend query time is effectively unchanged between branches: about `58 ms`
to `59 ms` for 10k rows. The improvement is in frontend rendering and main
thread pressure.

| Metric | `main` average | `chore/explore-perf` average | Delta |
| --- | ---: | ---: | ---: |
| Rendered log row DOM nodes | `10,000` | `29` | `99.71%` fewer |
| Total DOM elements after render | `271,211` | `2,157` | `99.20%` fewer |
| Used JS heap after render | `323.7 MB` | `95.0 MB` | `70.65%` lower |
| Run click to result paint | `2,459.0 ms` | `1,113.5 ms` | `54.72%` faster |
| Browser-observed query XHR | `2,237.0 ms` | `1,041.8 ms` | `53.43%` faster |
| Backend execution time | `58.0 ms` | `59.0 ms` | roughly equal |
| Long-task blocking time | `2,211.7 ms` | `109.0 ms` | `95.07%` lower |

The largest win is row virtualization: `main` keeps all 10,000 rows in the DOM,
while the perf branch keeps only the visible rows plus overscan.

## Additional Sanity Checks

After the main 10k query benchmark, the following interaction checks were run
against the perf branch to cover cases not exercised by the run-to-paint test.

| Check | Result |
| --- | --- |
| 200-row query | `190.8 ms` from Run click to paint; backend execution `17 ms`; `29` rendered row nodes; `2,157` DOM elements; one `76 ms` long task |
| 10k top-to-bottom scroll traversal | 90-frame scrollbar-style traversal completed in `1,872.9 ms`; visible indexes moved from the top of the result set to `9972-9999`; rendered row nodes stayed between `28` and `40`; zero observed long tasks |
| Row expansion | Clicking a compact row expanded it and showed the `Show Context` action |
| Context modal | Clicking `Show Context` opened the `Log Context` modal and loaded surrounding rows |

The scroll traversal originally exposed avoidable work from measuring every
virtual row and rendering logfmt highlight markup while scrolling. The perf
branch now measures only expanded rows and renders plain compact text during
active scrolling, restoring highlighted markup after scroll settles.

## Table View Follow-up

The headline benchmark above is for compact mode. Table mode has a different
cost profile because it renders richer cells and per-cell actions.

After direct table-mode testing with the same 10k query, the table was changed
to pre-page rows before handing them to TanStack Table and to mount copy/filter
cell actions only for the hovered cell. This keeps table mode from building
hidden action controls for every visible cell.

| Table-mode check | Before | After |
| --- | ---: | ---: |
| Total DOM elements | `9,284` | `3,431` |
| Mounted cell action groups | `500` | `0` idle, `1` on hover |
| Run click to result paint | `817.5 ms` | `715.4 ms` |
| Browser-observed query XHR | `616.5 ms` | `491.3 ms` |
| Long-task blocking time | `868 ms` | `619 ms` |

Table mode is still intentionally heavier than compact mode: it renders a
paginated grid with rich cells, column resize/reorder, expansion, and
copy/filter actions. For large scans, compact mode remains the smoother default
view for continuous scrolling.

## Environment

- Browser automation: `agent-browser`
- Browser: Headless Chrome `148.0.0.0`
- Viewport: `1440 x 900`
- App mode: Explore compact results
- Route: `/logs/explore?team=1&source=1&t=90d&limit=10000`
- `main`: clean worktree at `79e7cf5`, served from `/tmp/logchef-main` on port
  `5174`
- Perf branch: `chore/explore-perf` working tree, served on port `5173`
- API: local backend on port `8125`
- Dataset: local ClickHouse dev source `default.http`
- Matching rows in the 90-day window: `441,350`
- Query response size: `2,003,295` response characters, `1,992,436` bytes
  returned by the API stats

The existing dev dataset is large enough for this benchmark. No additional log
ingestion is required for a `limit=10000` test.

## Method

The browser was opened to the same route on both branches with the same local
preferences:

- `display_mode = compact`
- fields panel closed
- relative time range `90d`
- limit `10000`
- empty LogChefQL query, so the backend returns the latest 10k rows in the time
  window

For each branch, the page was loaded until the 10k result state was visible.
Then `agent-browser eval` patched `XMLHttpRequest` in the page, clicked the
visible `Run` button, waited for the LogChefQL query request to finish, and
sampled DOM, heap, backend stats, and long tasks after two
`requestAnimationFrame` turns plus a short settle delay.

`Browser-observed query XHR` is not pure network time. It includes browser main
thread delay before XHR completion can be observed. The backend's own execution
time comes from the API response `stats.execution_time_ms`.

## Raw Runs

### `chore/explore-perf`

| Run | Row nodes | DOM elements | Heap used | Run to query end | Run to paint | Query XHR | Backend exec | Long tasks | Long-task total | Max long task |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | `29` | `2,157` | `85 MB` | `1,069.1 ms` | `1,125.2 ms` | `1,043.1 ms` | `47 ms` | `1` | `132 ms` | `132 ms` |
| 2 | `29` | `2,157` | `94 MB` | `1,052.2 ms` | `1,114.3 ms` | `1,039.0 ms` | `64 ms` | `1` | `101 ms` | `101 ms` |
| 3 | `29` | `2,157` | `106 MB` | `1,056.8 ms` | `1,101.0 ms` | `1,043.3 ms` | `66 ms` | `1` | `94 ms` | `94 ms` |

### `main`

| Run | Row nodes | DOM elements | Heap used | Run to query end | Run to paint | Query XHR | Backend exec | Long tasks | Long-task total | Max long task |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | `10,000` | `271,211` | `242 MB` | `2,526.5 ms` | `2,741.2 ms` | `2,518.6 ms` | `51 ms` | `5` | `2,459 ms` | `1,656 ms` |
| 2 | `10,000` | `271,211` | `323 MB` | `2,136.8 ms` | `2,311.8 ms` | `2,129.3 ms` | `63 ms` | `6` | `1,931 ms` | `1,187 ms` |
| 3 | `10,000` | `271,211` | `406 MB` | `2,069.4 ms` | `2,324.0 ms` | `2,063.2 ms` | `60 ms` | `7` | `2,245 ms` | `1,382 ms` |

## Interpretation

The API returns the same 10k rows on both branches and ClickHouse execution time
is nearly identical. The main branch pays the browser cost of maintaining and
updating 10,000 rendered rows, which creates about `271k` DOM elements and
multi-second long tasks.

The perf branch keeps the result set in memory but only renders the visible
rows. That drops DOM size by two orders of magnitude, cuts used heap by about
`229 MB` on average, and removes almost all long-task blocking during the run.

These numbers are local dev measurements, so they should be treated as
branch-to-branch comparisons rather than production latency guarantees.
