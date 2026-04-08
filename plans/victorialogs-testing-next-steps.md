# VictoriaLogs Datasource: Testing and Next Steps

## Scope Completed

This branch has already landed the architectural cleanup required to treat datasources as first-class:

- datasource-backed source model and provider service
- ClickHouse and VictoriaLogs as separate datasource types
- provider-routed source management, query execution, discovery, and alert evaluation
- provider-specific source forms in the UI
- datasource-aware provisioning config
- VictoriaLogs dev service, seed data, and provider tests
- removal of the legacy log context feature
- removal of legacy query contracts:
  - saved queries and alerts no longer persist `query_type`
  - explore/native query APIs no longer use `raw_sql`
  - CLI, frontend, backend, and SQLite now use `query_language`, `editor_mode`, and `query_text`

## Commits Not Yet Reviewed Via Manual Smoke Test

The latest branch-local work before this document was:

- `98068d2` `add victorialogs dev coverage and local seed`
- `230e6d3` `drop legacy flat provisioning source fields`
- `27216c4` `document victorialogs official docker reference`
- `c88e6ea` `remove legacy query metadata contracts`

## What Is Still Pending

These are the remaining meaningful tasks before calling the branch fully merge-ready:

1. Run a real SQLite upgrade smoke test.
   This should exercise `000014_remove_legacy_query_type` against a pre-cleanup database, not just sqlc generation and unit coverage.

2. Run an end-to-end manual app smoke test with the local dev stack.
   The goal is to verify that the datasource abstraction works in the live UI and HTTP paths, not only in unit tests.

3. Run a full frontend build/typecheck after fixing the local frontend environment issue.
   Targeted Vue SFC compile checks and TS transpile checks already passed, but the full Vite/typecheck path is still blocked by the known local frontend setup issue.

## Recommended Testing Order

### 1. Bring up the dev stack

```bash
just dev-setup
just run-backend
just run-frontend
just dev-ingest-logs
```

Expected local services:

- LogChef frontend: `http://localhost:5173`
- VictoriaLogs health: `http://localhost:9428/health`
- Mailpit UI: `http://localhost:8025`

Default local login:

- `admin@logchef.internal`
- `password`

### 2. Smoke test ClickHouse sources

Verify the existing ClickHouse path still works after the datasource refactor:

1. Open the ClickHouse-backed dev sources.
2. Run a native SQL query.
3. Run a LogchefQL query.
4. Save a query, reload it, edit it, and re-run it.
5. Create a ClickHouse alert in:
   - condition mode
   - native SQL mode
6. Test the alert query from the UI.

Expected result:

- existing ClickHouse behavior should remain unchanged
- saved queries should round-trip with `query_language` and `editor_mode`
- alerts should still evaluate and persist correctly

### 3. Smoke test VictoriaLogs sources

Verify the new datasource path works as a first-class source:

1. Open the `VictoriaLogs Demo` source.
2. Run a native LogsQL query from Explore.
3. Confirm histogram loading works for the same query.
4. Check field discovery and field value loading in the sidebar.
5. Save a LogsQL query, reload it, and re-run it.
6. Create a native VictoriaLogs alert.
7. Run alert query test from the UI.

Expected result:

- no ClickHouse-specific UI assumptions leak into the VictoriaLogs flow
- saved queries should persist as `query_language=logsql`
- alerts should persist as `query_language=logsql`, `editor_mode=native`

### 4. SQLite migration smoke test

Use a database that predates the `query_language/editor_mode/query_text` cleanup and verify the app upgrades successfully.

Minimum checks:

1. Start from an older local DB snapshot.
2. Boot the backend and let migrations run.
3. Confirm:
   - `team_queries` no longer has `query_type`
   - `alerts` no longer have `query_type`
   - existing saved queries still load
   - existing alerts still load and test successfully

### 5. Final validation commands

These should stay green after any follow-up fixes:

```bash
go test ./...
cargo check --manifest-path cli/Cargo.toml
git diff --check
```

If the frontend environment is repaired, also run:

```bash
pnpm --dir frontend build
pnpm --dir frontend exec vue-tsc --noEmit
```

## Intentional Non-Blocking Leftovers

These do not block the datasource architecture itself:

- metrics still use the label name `query_type` in Prometheus output
- historical migrations still reference `query_type`
- the RFC still documents the intermediate migration path and old contract names

Those are historical or observability details, not active product/API contracts.

## Merge Bar

This branch should be considered ready once:

1. ClickHouse manual smoke passes.
2. VictoriaLogs manual smoke passes.
3. SQLite upgrade smoke passes.
4. Full frontend build/typecheck passes in a repaired local environment.
