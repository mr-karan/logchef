# AGENTS.md

Logchef is a log analytics platform: Go (Fiber) backend + Vue 3 (TypeScript/Pinia)
frontend + Rust CLI. Metadata lives in SQLite (default) or Postgres; logs live in
ClickHouse or VictoriaLogs, queried via SQL or LogchefQL (Logchef's own query DSL).

## Repo Layout

```
cmd/server/        # entrypoint; web.go embeds the built frontend (//go:embed all:ui)
internal/
  server/          # HTTP handlers (Fiber), routing in server.go
  core/            # business logic (users, teams, sources, alerts, collections, ...)
  store/sqlite/    # SQLite metadata store (default): queries.sql + migrations/ + sqlc/
  store/postgres/  # Postgres metadata store: same layout
  datasource/      # pluggable log backends (ClickHouse / VictoriaLogs providers)
  logchefql/       # LogchefQL grammar (participle) -> ClickHouse SQL / LogsQL generators
  clickhouse/      # ClickHouse client
  victorialogs/    # VictoriaLogs client
  auth/            # OIDC + local auth, sessions
  alerts/          # alert evaluation and notification
  ai/              # AI SQL assistant (OpenAI / Bedrock)
  provisioning/    # declarative teams/sources provisioning from TOML
  config/          # koanf config loading
pkg/models/        # shared domain models
pkg/logger/        # logging
frontend/          # Vue 3 SPA — src/api (API clients), src/stores (Pinia), src/views
cli/               # Rust CLI (cargo)
docs/              # Astro docs site (logchef.app)
dev/               # docker-compose dev env (ClickHouse, VictoriaLogs, Dex OIDC, seeds)
rfcs/, plans/      # design docs
```

## Commands

```bash
just build               # full build: frontend dist first, then backend (embeds dist)
just build-backend       # backend only (skip when only Go changed; runs sqlc-generate)
just run                 # build + run with config.toml (override: just CONFIG=x.toml run)
just check               # REQUIRED before committing: fmt + vet + lint + sqlc-generate + test
just test-short          # fast Go tests (no -race, no coverage)
just sqlc-generate       # regenerate sqlc code (required after touching queries.sql)

just dev-setup           # one-shot dev env: docker infra + ClickHouse tables
just run-backend         # terminal 1 (creates local.db, applies migrations, provisions)
just dev-seed            # optional: dev@localhost API user/token
just run-frontend        # terminal 2: vite dev server on :5173
just dev-docker          # infra only (ClickHouse :8123, VictoriaLogs :9428, Dex)
just dev-reset           # wipe dev log data
```

Frontend (in `frontend/`, uses **bun**, not npm/yarn):

```bash
bun run dev              # vite dev server
bun run typecheck        # vue-tsc — run before committing frontend changes
bun run test             # vitest
```

CLI (in `cli/`): `just build-cli`, `just check-cli` (fmt + clippy + tests).

## Common Workflows

**Metadata DB schema change** (applies to both stores):
1. Add a numbered migration pair: `internal/store/<store>/migrations/NNNNNN_name.up.sql`
   + `.down.sql` (next sequence number, both stores versioned independently).
2. **Add the new up-migration to the `schema:` list in `sqlc.yaml`** — easy to forget,
   and sqlc will silently type-check against the old schema without it.
3. Update `internal/store/<store>/queries.sql`, then `just sqlc-generate`.
4. Update models in `pkg/models/`.
5. Both `sqlite` and `postgres` stores implement the same store interface — a schema
   or query change usually needs to be mirrored in both. Conformance tests live in
   `internal/store/storetest/` and run against each backend.

**New API endpoint**: handler in `internal/server/` -> register route in
`internal/server/server.go` -> business logic in `internal/core/` -> frontend client
in `frontend/src/api/` -> Pinia store in `frontend/src/stores/` if stateful.

**LogchefQL change**: grammar in `internal/logchefql/grammar.go`, generators in
`sql_generator.go` (ClickHouse) and `logsql_generator.go` (VictoriaLogs). Mirror
frontend parsing in `frontend/src/utils/logchefql/` and run its vitest suite.

**New log backend**: implement the datasource provider interface in
`internal/datasource/` (see `clickhouse_provider.go`).

## Hard Rules

- **Run `just check` before committing.** It regenerates sqlc — commit the generated
  diff alongside your query changes.
- **golangci-lint is pinned** in `.github/workflows/go-tests.yml`. `just lint` fails
  when the local version differs. To upgrade, bump the pin and fix new findings together.
- **Never edit generated files**: `internal/store/*/sqlc/` (sqlc). Edit `queries.sql`
  + migrations and regenerate instead.
- Migrations run automatically at startup via golang-migrate (embedded `go:embed`).
  Treat them as forward-only; the down path exists but is thin — a bad migration on a
  live DB means restoring a backup.
- The backend embeds the frontend dist (`//go:embed all:ui` in `cmd/server/web.go`).
  A bare `go build ./cmd/server` without `just build-ui` first embeds only the
  placeholder, not the real UI.
- Frontend package manager is **bun**; lockfile is `bun.lock`. Don't add
  `package-lock.json` / `yarn.lock` / `node_modules`.
- Config: TOML files, overridable with `LOGCHEF_` env vars using double underscores
  for nesting (e.g. `LOGCHEF_SERVER__PORT`).
- SQLite DB files (`local.db*`, `logchef.db`, `prod.db`) are local state — never
  commit them.
- This is a public repo: no credentials, internal URLs, or org-specific config in
  committed files.
