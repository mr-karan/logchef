# AGENT.md

Essential information for agentic coding tools working with LogChef.

## Build/Test Commands
- **Build all**: `just build` (builds backend + frontend)
- **Build backend only**: `just build-backend` (requires `just sqlc-generate` first)
- **Build frontend only**: `just build-ui` (in frontend/ use `pnpm build`, `pnpm typecheck`)
- **Run with config**: `just run` or `just CONFIG=dev/config.toml run`
- **Test with coverage**: `just test` (generates coverage/coverage.html)
- **Quick tests**: `just test-short` (no coverage, faster)
- **Run single test**: `go test -v ./path/to/package -run TestName`
- **All checks**: `just check` (fmt, vet, lint, sqlc-generate, test)
- **Lint**: `just lint` (uses golangci-lint), `just fmt`, `just vet`

## Architecture
- **Go backend**: Fiber web framework, SQLite metadata + ClickHouse logs
- **Vue 3 frontend**: TypeScript, Pinia state management, Radix Vue + Tailwind
- **Database**: SQLite (users/teams/metadata) + ClickHouse (log data)
- **Key directories**: `internal/` (backend logic), `frontend/src/` (Vue app)
- **SQLC**: Type-safe SQL queries generated from `internal/sqlite/queries.sql`

## Code Style
- **Go**: Follow golangci-lint rules, use `github.com/mr-karan/logchef` import prefix
- **Frontend**: TypeScript strict mode, Vue 3 composition API, Pinia stores
- **Dependencies**: Check existing imports/package.json before adding new libraries
- **Database changes**: Update migrations, queries.sql, then run `just sqlc-generate`
- **Error handling**: Use standardized responses via `internal/server/response.go`
