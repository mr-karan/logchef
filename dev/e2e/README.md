# logchef e2e (agent-browser)

A small, repeatable end-to-end suite that drives the running logchef **frontend**
as a real user (via [`agent-browser`](https://www.npmjs.com/package/agent-browser))
and asserts on the rendered UI. Because the app is identical regardless of the
metadata store, running it against each backend doubles as a **SQLite ⇄ Postgres
parity check**.

## What it covers

| Scenario | Asserts |
|----------|---------|
| `login` | Dex OIDC login lands on the explorer (Run control + team selector) |
| `sources` | a team + source can be selected and a source is bound |
| `query` | running a query returns log rows from ClickHouse (widens the time range first) |
| `field_values` | the filterable-fields sidebar populates distinct values + counts (exercises the bounded-concurrency field-values fan-out) |
| `histogram` | the histogram toggle renders without error |
| `collections` | the Collections menu opens |
| `admin_users` | the admin users page lists the seeded admin |

## Prerequisites

- `agent-browser` on PATH: `npm i -g agent-browser && agent-browser install`
- A running logchef stack (backend :8125, Vite frontend :5173, ClickHouse, Dex).
  See `.claude/skills/logchef-dev` for the full setup. In short: the dev infra
  (ClickHouse + Dex + Mailpit) runs via `just dev-docker-detach`, the backend via
  `just run-backend`, and the frontend via `bun run dev` in `frontend/`.
- Sample data in ClickHouse. The suite widens the time range to **Last 24h**, so
  any data ingested in the last day is fine (see `.claude/skills/logchef-dev` for
  the vector/`INSERT` snippets). Older data → nothing to query.

## Run

```bash
# all scenarios, defaults (admin@logchef.internal / password @ :5173)
dev/e2e/run.sh

# label the run (used for screenshot filenames + the report line)
BACKEND=postgres dev/e2e/run.sh

# only specific scenarios
dev/e2e/run.sh login query field_values

# override target / credentials / screenshot dir
BASE_URL=http://localhost:5173 EMAIL=admin@logchef.internal PASSWORD=password \
  ART_DIR=/tmp/logchef-e2e dev/e2e/run.sh
```

Exit code is non-zero if any assertion fails (CI-friendly). Screenshots for key
steps are written to `$ART_DIR` (default `/tmp/logchef-e2e`).

### Backend parity

Point the backend at each store and run the suite with a matching label:

```bash
# SQLite (default)
LOGCHEF_DATABASE__DRIVER=sqlite ./bin/logchef -config config.toml &
BACKEND=sqlite dev/e2e/run.sh

# Postgres
LOGCHEF_DATABASE__DRIVER=postgres \
  LOGCHEF_POSTGRES__DSN='postgres://logchef:logchef@localhost:5432/logchef?sslmode=disable' \
  ./bin/logchef -config config.toml &
BACKEND=postgres dev/e2e/run.sh
```

Both should report `N passed, 0 failed`.

## Layout

- `run.sh` — entry point: preflight, `login()`, iterate scenarios, report + exit code.
- `lib.sh` — the testing library: `agent-browser` wrappers, snapshot-driven ref
  lookup (`ref`, `cbox_ref`), the Dex `login()` flow, `select_team_source`,
  `set_wide_time_range`, and polling assertions (`assert_present`, `assert_control`).
- `scenarios.sh` — one `scn_<name>` function per scenario.

## Adding a scenario

1. Write `scn_<name>()` in `scenarios.sh` using the `assert_*` / `click_by` / `ref`
   helpers. Re-snapshot before each interaction — refs go stale on any DOM change.
2. Append `<name>` to `ALL_SCENARIOS` in `run.sh`.

## Notes / gotchas

- **Refs are ephemeral.** `agent-browser` assigns `@eN` fresh on every snapshot;
  the helpers always resolve a ref immediately before acting on it.
- **Assertions poll.** UI content renders after async XHRs, so `assert_present`
  retries for a few seconds rather than checking once. This is what makes the
  suite reliable despite browser timing.
- **The top comboboxes have no accessible name** (team, source, refresh, grouping,
  page size all show as bare `combobox`), so `cbox_ref` keys off the *value* shown
  after the colon (the source box is the one whose value looks like `db.table`).
- **Query editor (CodeMirror)** isn't scripted here — its content-editable is
  awkward to target reliably. Filtering is better exercised by clicking a field
  value in the sidebar; extend `scenarios.sh` if you need typed queries.
