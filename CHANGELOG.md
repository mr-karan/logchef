# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **CLI `saved-queries` command** — List saved queries and run one by name,
  numeric ID, or pasted explorer URL, with limit, variable, and output-format
  overrides.
- **CLI default team/source environment variables** —
  `LOGCHEF_DEFAULT_TEAM` and `LOGCHEF_DEFAULT_SOURCE` can supply stateless
  defaults when `--team` / `--source` are omitted.
- **CLI `--output msg` mode** — `query`, `sql`, `collections`, and
  `saved-queries` can print message text only, one row per line.
- **CLI SQL time flags** — `logchef sql` now accepts `--since`, `--from`, and
  `--to`. The predicate is injected before the first top-level
  `GROUP BY` / `ORDER BY` / `LIMIT` / `HAVING` / `SETTINGS` / `FORMAT`; the
  scanner skips string literals, quoted identifiers, comments, and
  parenthesized subqueries, so `WHERE`/`LIMIT` inside literals or nested
  selects no longer confuses injection. Use `__START__` / `__END__`
  placeholders for full control (e.g. CTEs).
- **CLI `find` command** — Discover sources with recent matches for a service,
  job, host, or message pattern. For each matched source, fires a small
  per-column sample query: label-shaped columns (service/host/job_name) get
  the top 3 values with counts; free-form text columns (msg) get a single
  truncated sample row. Suppress with `--no-samples`. Per-source query
  timeout defaults to 30s. Sources that error out (permissions, schema
  fetch, query failure) are skipped and counted; rerun with `--debug` for
  per-source diagnostics.
- **CLI `tail` command** — Follow matching LogChefQL rows with bounded polling
  and `text`, `jsonl`, or `msg` output. Dedup is stable across column-order
  changes between polls; when a poll returns at `--limit` a one-shot warning
  hints to raise `--limit` or shrink `--interval`.
- **CLI `--output json-flat` mode** — `query`, `sql`, `collections`, and
  `saved-queries` can hoist JSON-shaped `msg` fields to top-level JSON rows.
- **CLI `whoami` command** — Print the authenticated user and accessible teams.
- **CLI `auth current` subcommand** — Offline command that prints the active
  context, server URL, and token source (env vs config) without hitting the
  network. Useful for "is my `LOGCHEF_AUTH_TOKEN` even set?" diagnostics
  before any API call.
- **CLI `query --explain` / `sql --explain` alias** — `--explain` is an alias
  of `--show-sql` on both commands. Both print `Generated SQL: <sql>` to
  stderr and continue executing, so the trace coexists cleanly with
  `--output jsonl | jq` pipes. On `sql`, the printed SQL includes any
  `--since` / `--from` / `--to` injection.
- **CLI `--dry-run` on `query` and `sql`** — Prints the resolved SQL to
  stdout (no prefix, pipes cleanly) and exits without keeping results.
  `query --dry-run` still calls the server once for LogChefQL translation;
  `sql --dry-run` is fully offline.
- **CLI `auth current` token expiry** — When the token came from the saved
  config, the output now appends an `expires` timestamp:
  `token: set (from config, expires 2026-06-03T07:00:00Z)`.
- **CLI auto-disables highlighting on non-TTY output** — All five subcommands
  (`query`, `sql`, `collections`, `saved-queries`, `tail`) skip ANSI
  highlighting when stdout is piped, so `... | jq` and `... > file` produce
  clean output without `--no-highlight`. The flag still works as an
  explicit override.
- **Schema column descriptions** — ClickHouse column comments are now surfaced
  as optional schema descriptions.
- **Service accounts** — Non-login principals you can add to teams and own API
  tokens. Created from **Administration → Service Tokens**. Cannot
  authenticate via OIDC or CLI exchange.
- **Scoped API tokens** — Tokens now carry an explicit scope list
  (`logs:read`, `alerts:write`, ...). New `requireTokenScope` middleware
  enforces them on every route. Presets in the UI: Read-only, Logs viewer,
  Logs analyst, Alerts manager, Source admin, Full access. Active preset is
  highlighted while the selection matches.
- **Account-type toggle in Add Team Member dialog** — Switch between Human
  user and Service account; the dropdown filters to the selected type and
  shows `full_name` with email as a subtitle.
- **Service account badge** in team member tables, with a bot icon, so
  automation principals are visually distinct from humans.
- **Team chips and "Manage teams" button** on each service account card.
  Surfaces zero-team state ("token reaches no source") and lets you add or
  remove team memberships without leaving the page.
- **New admin endpoints**:
  - `GET/POST/DELETE /admin/service-accounts`
  - `GET/POST/DELETE /admin/service-accounts/:id/tokens`
  - `GET/POST/DELETE /admin/service-accounts/:id/teams`

### Changed
- **`UserProfile` "Create API Token" dialog defaults to the Read-only preset.**
  Previously defaulted to Full access (`*`), which made every checkbox appear
  disabled-but-checked and gave no choice unless the user manually clicked
  away.
- **Read-only preset includes every `:read` scope.** Now covers
  `tokens:read`, `users:read`, and `settings:read` in addition to the
  resource read scopes. Admin-gated routes still require admin role at the
  auth layer, so the wider scope set only matters for admin-owned tokens.
- **`/admin/users/*` now 404s on service accounts.** Service principals are
  managed through the dedicated `/admin/service-accounts/*` path so admins
  can't accidentally promote a service account to admin via the human-user
  CRUD path.

### Fixed
- **`TokenScopePicker` checkboxes are now interactive.** The component was
  binding to `:checked` / `@update:checked`, but the shadcn-vue Checkbox
  forwards reka-ui's `CheckboxRootProps`, which uses `model-value` /
  `update:model-value`. The bug was hidden behind the old Full-access
  default; switching the default surfaced it.
- **Token creation rejects empty scopes (HTTP 400).** Previously, a request
  with no scopes silently defaulted to `["*"]`, minting a god-token.
- **Corrupt or empty stored scopes fail closed.** A row with malformed JSON
  in `api_tokens.scopes` now grants no access instead of `*`.

### Migration notes
| Migration | What it does |
|-----------|-------------|
| 000023 | Adds `users.account_type` (`human`/`service`) and `api_tokens.scopes` (JSON array). Existing users default to `human`; existing tokens default to `["*"]` to preserve behavior. Index on `users(account_type)`. |

## [1.6.0] - 2026-05-13

LogChef 1.6 narrows the **Team** abstraction to access control only and adds
**Collections** — cross-team curation lists for saved queries. The unified
Saved Queries view replaces the old team-scoped collections page with a flat,
searchable table and a collection-picker dropdown. The release also adds a
new **Team Editor** role and restructures the admin URLs for consistency.

### API & URL changes
- **Saved queries are source-scoped, not team-scoped.** `team_queries` is
  rebuilt as `saved_queries(source_id, created_from_team_id, created_by, …)`.
  Visibility: any user with source access via any team. Edit: creator +
  global admin.
- **`/api/v1/saved-queries/:id/resolve`** returns a transient
  `resolved_team_id` computed from the user's access paths — no team
  ownership stored on the query itself.
- **Alerts de-teamed.** New `/api/v1/alerts` route group; `alerts.team_id`
  dropped, `alerts.created_by` added.
- **`query_shares` and `export_jobs` lose `team_id`.**
- **New `/api/v1/collections`** — CRUD for collections + members + items.
- **Collection mutation routes use `requireAnyTeamCollectionMutator`**
  (admin or editor in any team). Team admin–only routes (membership
  management, source linking, `/api/v1/users`) stay strict on
  `requireAnyTeamAdmin`.
- **Admin frontend URLs restructured.** `/management/*` → `/admin/*`,
  `/profile` → `/settings/profile`, `/admin/sources/list` → `/admin/sources`,
  `/admin/sources/edit/:id` → `/admin/sources/:id/edit`. No redirects from
  old paths.
- **Old team-scoped paths return 404.** No shims, no redirects.
- **Frontend URL:** `/logs/saved/:queryId` is the canonical share link.

### Added
- **Collections** — Cross-team curation lists. Personal collection
  auto-created per user ("My Collection"). Shared collections are
  invite-only with `owner` + `member` roles. Items a member can't run
  show with a `runnable: false` flag (lock icon in UI).
- **Unified Saved Queries view** — Single page at `/logs/saved` with a
  collection-picker dropdown (All Queries / My Collection / shared
  collections), inline search, and a Metabase-style flat table.
- **"Add to Collection" drawer** — Per-row action on saved queries.
  Slide-out panel shows all collections as checkboxes for quick
  pin/unpin. Create new collections inline.
- **"Remove from collection" action** — When viewing a specific
  collection, each row's menu gains a destructive remove action.
- **Saved query resolve with `resolved_team_id`** — The `/resolve`
  endpoint deterministically picks the correct team for execution using
  priority: explicit `?team_id` hint → `created_from_team_id` →
  first accessible team fallback.
- **`created_from_team_id`** on saved queries — nullable metadata
  recording which team context the query was saved from. Used as a
  preference hint during resolve; not an ACL gate.
- **Invite members by email** — Collection member invite uses an email
  dropdown (same UX as team member management), not a raw user ID.
- **Shareable saved-query links** with configurable TTL.
- **Backend-streamed result downloads** with synchronous admission
  control (HTTP 429 at capacity).
- **Calendar month/year drill-down** in the date picker.
- **OIDC `skip_email_verified_check`** option.
  ([#85](https://github.com/mr-karan/logchef/issues/85),
  [#86](https://github.com/mr-karan/logchef/pull/86))
- **Native ClickHouse TLS.**
  ([#88](https://github.com/mr-karan/logchef/pull/88))
- **Team Editor role** — new team role between Member and Admin. Editors
  can manage collections (create, rename, invite, pin items) and save
  queries. They cannot invite team members or link sources — those stay
  admin-only. ([#94](https://github.com/mr-karan/logchef/pull/94))
- **Shared UI primitives** — `PageHeader`, `PageSection`, `EmptyState`,
  `LoadingState` under `components/layout/`. Replace the ad-hoc empty/
  loading/header markup across admin and settings pages with one
  consistent visual language.
- **`useTeamPermissions()` composable** — central frontend role-check
  API: `isGlobalAdmin`, `isAnyTeamAdmin`, `isAnyTeamCollectionMutator`,
  `isTeamAdmin(teamId)`, `isTeamCollectionMutator(teamId)`,
  `canSaveQuery`, `canEditSavedQuery(query)`, `canManageCollection(c)`.
- **Tests** — 18 backend cases for the new role helpers (cross-team
  negatives + regression guards that editors stay distinct from admins),
  28 frontend Vitest cases for `useTeamPermissions`.

### Changed
- **Saved Queries view is now the unified entry point.** The old two-page
  layout (separate /logs/saved + /logs/collections list) is replaced by
  a single flat table with the collection picker. `/logs/collections`
  is a standalone management page (create, delete, navigate to detail).
- **Alert notifications** drop `team_id` / `team_name` fields.
  Recipients resolve to users directly.
- **Explore UI polish** — quieter top bar, concrete query placeholders,
  tighter histogram styling, Local|UTC segmented timezone control.
- **Export → Download** rename; backend-streamed pipe is the only path.
- **AI SQL insert** clears saved-query state and switches to SQL mode.
- **Save button** in the query editor is now visible to all team members
  (it was previously gated to admins by mistake; backend always allowed
  it). Edit/delete of a saved query still requires the creator or a
  global admin.
- **Distinct icons** for LogchefQL vs SQL saved queries (`Search` and
  `Database`); the previous near-identical file icons were hard to tell
  apart at small sizes.
- **Admin/settings pages** migrated to the shared `PageHeader` +
  `PageSection` layout. The whole-page Card wrapper pattern is gone.
- **Sidebar links** carry team + source context into Explorer and Alerts
  via a single `resolveTo()` helper.

### Fixed
- **Unbounded query result OOM** — `[query] max_limit` cap (default 100k
  rows). A large unbounded result set previously exhausted the browser
  renderer.
- **Long raw SQL in the URL** no longer trips the HTTP header size limit
  on the server.
- **"No source selected" race** — explorer waits for
  `currentSourceDetails.id === contextStore.sourceId` before executing,
  so newly-selected sources don't run the previous source's query.
- **Stale-request guard** in `sourcesStore.loadSourceDetails` — a fast
  source switch is no longer overwritten by an older in-flight response.
- **Saved query loads wrong source** — resolved query's `source_id` now
  overrides stale URL `?source=` param.
- **Crash-safe export pruner** — interrupted prunes no longer leave
  orphaned download files behind.
- **Translate API errors are surfaced to the editor** instead of failing
  silently.
- **Export download URLs are relative**, so downloads work behind reverse
  proxies that rewrite hostnames.

### Removed
- **Query Folders** (the team-scoped experiment from v1.6.0-dev).
- **Bookmarks** (`is_bookmarked` column) — replaced by personal
  collections.
- **`team_id` on saved queries, alerts, query shares, export jobs.**
- Dead frontend code paths (`loadTeamSourceQueries`,
  `createTeamSourceQuery`, `useQueryFoldersStore`, etc.).

### Migration notes (000016 → 000021)
| Migration | What it does |
|-----------|-------------|
| 000016 | Drops `query_folders` + `query_folder_items` |
| 000017 | Rebuilds `team_queries` → `saved_queries`, drops `team_id`, adds `created_by` |
| 000018 | Drops `team_id` from `alerts`, `query_shares`, `export_jobs`; adds `alerts.created_by` |
| 000019 | Creates `collections`, `collection_members`, `collection_items` |
| 000020 | Drops `is_bookmarked`; seeds personal collections; migrates bookmarks to collection items |
| 000021 | Adds `created_from_team_id` to `saved_queries`; backfills from `team_sources` |

### Follow-ups
- `logchef-mcp` (separate repo) needs rewiring to `/api/v1/saved-queries`.
- Provisionable collections out of scope for 1.6.

### Contributors
- [@m0nikasingh](https://github.com/m0nikasingh) — OIDC email
  verification skip
  ([#86](https://github.com/mr-karan/logchef/pull/86)), native ClickHouse
  TLS ([#88](https://github.com/mr-karan/logchef/pull/88)), AI SQL
  insert mode fix ([#89](https://github.com/mr-karan/logchef/pull/89))

## [1.5.0] - 2026-04-08

### Added
- **Rich value autocomplete in LogchefQL editor** — After typing `host=`, the editor instantly suggests top field values with occurrence counts (e.g., `cdn.logchef.dev (1.7K)`). Suggestions come from the sidebar's cached field data — no additional network calls during typing. Supports partial matching inside quotes, auto-quoting string values, and proper escaping of special characters.
- **Numeric field values in sidebar** — Fields like `status` (UInt16) and `bytes` (UInt32) now appear as filterable fields and auto-load their top values alongside LowCardinality fields.
- **Shared field values cache** — New Pinia store (`exploreFieldValues`) allows the sidebar and editor to share field value data, eliminating redundant API calls.

### Changed
- **Tailwind CSS v4 migration** — Upgraded from Tailwind v3 to v4 with oklch color system, `@theme` directives, and `@tailwindcss/vite` plugin (replaces PostCSS).
- **shadcn-vue Vega theme** — Switched from new-york to Vega style with Zinc base and Blue accent. Small border radius for a sharper, more technical look.
- **Sidebar defaults to collapsed** — Icon-only mode maximizes screen real estate for log viewing. Expand via rail hover or `Cmd+B`.
- **Theme toggle moved to sidebar footer** — Single-click cycle (Light → Dark → System) instead of buried in dropdown menu.
- **Histogram charts use Unovis** — Migrated from custom chart to Unovis with brush-drag zoom, crosshair tooltips, and stacked bar support.
- **Monaco editor lazy-loaded** — SQL editor loads on demand, reducing initial bundle for LogchefQL-only users.
- **Bolder chart colors** — Blue chart gradient shifted one step darker for better visibility on both light and dark backgrounds.

### Fixed
- **Hyphenated field names work everywhere** — Fields like `user-identifier` are now backtick-quoted in all SQL queries (field values, histograms, group-by). Previously caused `user - identifier` subtraction errors.
- **Validation errors return 400, not 500** — Invalid field names, timezones, and identifiers now return proper HTTP 400 Bad Request with `ValidationError` type instead of 500 Internal Server Error.
- **Histogram tooltip styling** — Fixed broken tooltip background/border after TW4 migration (`hsl(var(--...))` → `var(--...)`).
- **Histogram crosshair null guard** — Added optional chaining (`row?.ts`) to prevent crash when data is empty.
- **Editor line height mismatch** — Fixed LogchefQL editor height calculation (`baseLineHeight: 21` → `20` to match Monaco's `lineHeight`).
- **1-second histogram buckets** — Support for sub-minute bucket intervals in ClickHouse.
- **Brush zoom restored** — Click-to-zoom on histogram bars works alongside brush-drag selection.
- **Grouped histogram string values** — Fixed dereferencing of grouped string values in histogram data.
- **Session cookie handling** — Fixed local dev cookie configuration and team provisioning.
- **Team admin permissions** — Team admins can now manage members on managed (provisioned) teams.
- **Idle connection cleanup** — Added `IdleTimeout` and periodic `QueryTracker` cleanup to prevent connection leaks.
- **Noisy logs reduced** — Session management logs downgraded to DEBUG; structured slog source shortened to `file:line`.
- **Cursor pointer restored** — Added `cursor-pointer` base rule for all interactive elements (TW4 preflight removed it).

## [1.4.0] - 2026-03-30

### Added
- **Declarative provisioning** — Define teams, sources, and access control in a TOML config file for GitOps-style management. Resources declared in config are tagged "managed" and fully controlled by config; UI-created resources are left alone. Supports dry-run mode, separate `provisioning.toml` file, and an admin export endpoint (`GET /admin/provisioning/export`). API rejects mutations on managed resources.
- **All Teams collections view** — Browse saved queries across all your teams from a single page. New "All Teams" option in the team dropdown on the Collections page shows queries with Team and Source columns.
- **SQL input validation** — Timezone, field name, and group-by inputs are now validated before SQL interpolation, preventing injection attacks on ClickHouse queries.

### Changed
- **Auth returns 401 for expired sessions** — Backend now returns HTTP 401 (not 403) for authentication failures, so the frontend correctly redirects to login instead of showing a Forbidden page.
- **Parallel source health checks** — Admin source listing now pings all sources concurrently instead of serially, reducing page load time proportional to source count.
- **OIDC audience validation enabled** — ID token verifier now validates the audience claim to prevent token confusion attacks.

### Fixed
- **Query cancellation works end-to-end** — LogchefQL queries now use a proper cancellable context (was no-op). Frontend preserves the query ID during cancellation so backend `KILL QUERY` can execute.
- **SQL mode no longer rewrites user queries** — Time range and limit changes no longer silently modify raw SQL in SQL mode, respecting the user's query as written.
- **Histogram timestamp detection** — The timestamp field check now inspects only the SELECT clause instead of the full query, preventing false positives when the field appears in WHERE/ORDER BY.
- **No duplicate query on page load** — The auto-execute watcher now skips if URL state initialization already triggered a query.
- **Post-login redirect preserved** — The requested page is now stored in a cookie through the OIDC round-trip, so users return to their original page after login.
- **Calendar highlights today** — Date picker calendar now opens focused on today's date with default times (00:00:00 for From, 23:59:59 for To).
- **Bookmark index covers sort** — New migration adds `updated_at` to the bookmark index for efficient sorted queries.
- **Frontend type errors fixed** — Resolved 3 pre-existing TypeScript errors in SourceSparkline, TeamsList, and SourceStats.
- **QueryEditor decomposed** — Extracted AiSqlDialog, VariableConfigSheet, and VariablesPanel into focused components (2645→1839 lines).
- **Context store migrated** — Converted from Options API to Composition API setup function for consistency.
- **QueryEditor props typed** — Replaced runtime prop definitions with TypeScript interface.

## [CLI v0.1.4] - 2026-02-05

### Added
- **CLI `teams` command** — List teams available to your account.
- **CLI `sources` command** — List sources for a team with IDs and `database.table` references.
- **CLI `schema` command** — Show columns and types for a source without running SQL.

### Changed
- CLI errors for missing team/source now suggest `logchef teams` and `logchef sources --team <team>`.

## [1.3.0] - 2026-02-05

### Added
- **Configurable query result limit** — New `[query]` config section with `max_limit` setting (default: 1,000,000 rows). Allows admins to increase export limits based on infrastructure capacity. Frontend dropdown now shows options up to 1M rows.
- **User preferences persistence** — Theme, timezone, display mode, and fields panel state now persist across sessions. Preferences sync automatically and load on login.
- **Team admins can manage their teams** — Team admins now have access to team settings and member management without requiring global admin privileges.
- **Source editing and duplication** — Edit existing source configurations and duplicate sources for quick setup of similar data sources.

### Changed
- Query limit options now dynamically loaded from server config instead of hardcoded values.
- SQL editor now has max height (300px) with scrollbar for lengthy queries.

### Fixed
- Histogram now auto-refreshes when changing Group By column selection.
- Time icon in date picker now visible in dark mode.
- Date picker Now button auto-applies and fixes initial date format issues.
- JSON strings embedded in log fields now auto-parse for better readability.
- Table auto-resizes when filter sidebar closes.

## [1.2.1] - 2026-01-21

### Fixed
- **Explore history URL hydration** — Fixed issue where browser history navigation could fail to restore query state correctly.

## [CLI v0.1.2] - 2026-01-21

### Added
- **CLI `collections` command** — List and run saved queries from the command line
  - `logchef collections` lists all saved queries for a team/source
  - `logchef collections run <id>` executes a saved query with all output formats
  - Supports filtering by bookmarked queries with `--bookmarked`
- **CLI interactive mode** — Run `query` or `sql` without arguments for guided prompts
  - Interactive team and source selection with arrow-key navigation
  - Proper line editing with history support via `inquire` crate
  - Auth command now uses `inquire` for server URL input with defaults
- **Copy CLI command button** — Click terminal icon in explore toolbar to copy equivalent CLI command
  - Generates `logchef query` or `logchef sql` command matching current query
  - Includes time range, limit, and timeout parameters

## [CLI v0.1.1] - 2026-01-21

### Added
- **CLI `sql` command** — Execute raw ClickHouse SQL queries directly from the terminal
  - Full SQL control including time filters, aggregations, joins, and CTEs
  - Read SQL from stdin with `-` for complex multi-line queries
  - Same output formats as `query`: text, json, jsonl, table

## [1.2.0] - 2026-01-21

### Added
- **Rust CLI** — New cross-platform command-line interface written in Rust
  - `logchef auth` — Browser-based OIDC authentication with PKCE flow
  - `logchef query` — Execute LogchefQL queries with syntax highlighting (powered by [tailspin](https://github.com/bensadeh/tailspin))
  - `logchef config` — Manage CLI configuration and multiple server contexts
  - `logchef query --no-timestamp` — Hide timestamps in text output for cleaner exports
  - Multi-context support for managing dev/staging/prod instances (kubectl-style)
  - Configurable keywords and regex patterns for log highlighting
  - Configuration stored at `~/.config/logchef/logchef.json`
- **CLI OIDC config** — `oidc.cli_client_id` added to `config.toml` and docs for browser-based CLI auth
- **CLI Token Exchange API** — `POST /api/v1/cli/token` endpoint for CLI authentication
- **CLI OIDC Discovery** — `/api/v1/meta` now includes `oidc_issuer` and `cli_client_id` for CLI auth flow
- **Multi-select variables** — Select multiple values that expand to `IN (...)` clauses in SQL.
- **SQL optional clauses** (`[[ ... ]]`) — Wrap variable clauses to auto-remove when value is empty.
- **Variable widget configuration** — Configure variables as text inputs, dropdowns, or multi-selects with default values.
- **Collections "All Sources" view** — Browse saved queries across all sources in one place.
- **Alert delivery via SMTP and webhooks** — Send notifications directly without Alertmanager.
- Saved query name shown in browser tab title.
- Smart LIMIT handling in SQL mode.
- Support for CTEs, JOINs, and subqueries with template variables.

### Changed
- Saved queries persist variable widget configuration and defaults.
- Relative time range refreshes before each query execution.
- Reduced log noise and redacted session IDs for security.

### Fixed
- **SQLite SQLITE_BUSY errors** — Implemented dual-connection pattern (read pool + single write connection) to eliminate database lock contention under concurrent API requests.
- Saved query updates use the current editor content.
- Alert timestamps use ISO8601 UTC formatting for last triggered.
- Alert relative time formatting edge cases.
- Variable date display uses consistent YYYY-MM-DD format.
- Template variables validated and sent consistently to backend.
- Canceled requests on page reload no longer show error toasts.
- Collections race condition causing empty list on initial load.
- Y-scroll bar eliminated on explorer main content area.
- Variable datetime-local format accepts values without seconds.

### Removed
- Legacy Go CLI (`cmd/logchef/`, `internal/cli/`) — replaced by Rust CLI.
- `config.sample.toml` — superseded by the fully commented `config.toml`.

## [1.1.0] - 2025-12-29

### Added
- **Bookmark Favorite Queries** - Star saved queries for quick access ([#60](https://github.com/mr-karan/logchef/pull/60))
  - Bookmarked queries appear at top of collections dropdown
  - Copy shareable URL for any saved query
  - Direct link format: `/logs/collection/:teamId/:sourceId/:collectionId`

### Changed
- **LogchefQL Parser Rewrite** - Replaced hand-written tokenizer with grammar-based parser using [participle](https://github.com/alecthomas/participle)
  - Better error messages with position-aware diagnostics
  - More maintainable and extensible grammar definitions
  - Improved query type detection (LogchefQL vs SQL)
- **Frontend Tooling Migration** - Switched from pnpm + Vite to Bun + rolldown-vite
  - Build time: ~2.3s (was >55s)
  - Dev server start: ~1s (was ~3s)
  - Install time: ~8s (was ~25s)
- **Frontend State Management** - Refactored stores and composables
  - Centralized URL state synchronization
  - Cleaner explore store with better state transitions
  - Improved context and teams store initialization

### Fixed
- Proper context propagation throughout backend (contextcheck compliance)
- Reduced cyclomatic complexity in high-complexity functions
- **Saved Query Navigation** - Switching between saved queries no longer shows stale content
- **Saved Query Validation** - Backend now accepts relative-only time ranges (was: "absolute start time must be positive")
- **Cross-Page Context** - Team/source selection preserved when navigating between Explorer, Collections, and Alerts
- **Sidebar Navigation** - Links now include full context params (team + source)

### Contributors
- [@rhnvrm](https://github.com/rhnvrm) - Bookmark favorite queries feature ([#60](https://github.com/mr-karan/logchef/pull/60))

## [1.0.0] - 2025-12-22

The 1.0 release marks Logchef as production-ready. Eight months of development brought alerting, a proper backend query language, field exploration, and many UX improvements.

### Highlights

- **Alerting system** - SQL-based alerts with notification delivery
- **LogchefQL Backend Parser** - Full parsing, validation, and type-aware SQL generation in Go
- **Field Values Sidebar** - Kibana-style field exploration with click-to-filter
- **Query Cancellation** - Cancel long-running queries in ClickHouse, not just the UI

### Added
- **Field Values Sidebar** - Kibana-inspired field exploration panel
  - Shows top 10 unique values for `LowCardinality`, `Enum`, and `String` columns with occurrence counts
  - Click any value to add it as a filter (`field="value"`) or exclude it (`field!="value"`)
  - Auto-expands fields with ≤6 distinct values for quick access
  - Respects time range and active LogchefQL query filters
  - **Progressive per-field loading** - values load in parallel (max 4 concurrent) with per-field status
  - **Hybrid loading strategy** - LowCardinality/Enum fields auto-load, String fields require click
  - Per-field error handling with retry button
- **Backend LogchefQL parser** (`internal/logchefql/`) - full parsing, validation, and SQL generation in Go
  - Pipe operator (`|`) for custom SELECT clauses: `namespace="prod" | namespace msg.level`
  - Dot notation for nested JSON: `log_attributes.user.name = "john"`
  - Quoted field syntax for dotted keys: `log_attributes."http.status_code" >= 500`
  - Type-aware SQL for Map, JSON, and String columns
- LogchefQL API endpoints: `/logchefql/translate`, `/logchefql/validate`, `/logchefql/query`
- Field value exploration endpoints: `/fields/values`, `/fields/:fieldName/values`
- **Query cancellation** - Cancel button or `Esc` key cancels the query in ClickHouse
- Build commands: `just build-ui-analyze` for bundle analysis, `just clean-all` for deep clean
- Double-click column header resizer to auto-fit column width

### Changed
- **Breaking:** LogchefQL parsing moved from frontend to backend
- **Architecture:** Backend is now the single source of truth for SQL generation
  - Queries execute via `/logchefql/query` - backend builds and executes full SQL
  - "View as SQL" shows actual executed SQL from backend
  - Mode switching (LogchefQL → SQL) fetches SQL from `/logchefql/translate`
- LogchefQL validation uses backend API with debounced calls
- Field values API accepts `logchefql` param instead of `conditions`
- Pipe operator includes timestamp field in SELECT for proper ordering
- **Data table UX improvements:**
  - Compact rows for better log density
  - Expand/collapse chevron on each row
  - Click-to-copy cells with visual feedback
  - Cell action buttons in floating overlay
  - Unrestricted column resizing

### Fixed
- Histogram queries work with MATERIALIZED timestamp columns ([#59](https://github.com/mr-karan/logchef/discussions/59))
- Surrounding logs (context modal) works with MATERIALIZED timestamp columns
- Field sidebar excludes complex types (Map, Array, Tuple, JSON)
- Field values queries have 15s timeout to prevent query pileup
- Export in compact mode no longer returns undefined values
- Alerts create redirect and dark mode AI input styling

### Removed
- Frontend LogchefQL parser - replaced by backend implementation

## [0.6.0] - 2025-12-04

### Added
- **Alerting System** - SQL-based alerting with notification delivery
  - Create alerts using LogchefQL or SQL queries
  - Configure thresholds, frequency, and severity
  - Route alerts to Slack, PagerDuty, email via webhooks and SMTP
  - Alert history with execution logs
  - Dedicated alert detail page with edit and history tabs
- **Admin Settings UI** - Runtime configuration management via web interface
  - Manage alerts, AI, authentication, and server settings
  - Settings stored in database, override config.toml at runtime
  - Config-to-database seeding on first boot
- Duplicate source feature for quick configuration copying
- Keyboard typeahead navigation in team member and source dropdowns
- Alert history retention limit enforcement
- `config.sample.toml` showing minimal essential configuration

### Changed
- Runtime configuration now loaded from database, overriding config.toml for non-essential settings
- Simplified `config.sample.toml` to show only bootstrap essentials (server, sqlite, oidc, auth, logging)
- AI base_url now defaults to standard OpenAI API endpoint (`https://api.openai.com/v1`)
- Redesigned alerts list UI for better readability
- Simplified alerts by removing `query_type` and `lookback_seconds` fields

### Fixed
- Database settings now actually used at runtime (LoadRuntimeConfig integration)
- Active tab persistence when saving settings (no longer jumps to Alerts tab)
- Number input values properly converted to strings before API submission
- Acronyms (URL, API, AI, TLS, ID) now properly formatted in settings UI
- Alert `delivery_failed` flag cleared after successful retry
- Available users and sources now sorted alphabetically in team dialogs
- Null check added for test query warnings in AlertForm
- Frontend context and source loading logic improvements
- Duplicate `updateCustomFields` method removed from monaco-adapter

### Removed
- Rooms feature (refactored out)

## [0.5.0] - 2025-10-03

### Added
- **LogchefQL Query Language Improvements**
  - Pipe operator (`|`) for custom SELECT fields: `namespace="prod" | namespace msg.level`
  - Dot notation for nested JSON fields: `log_attributes.user.name = "john"`
  - Quoted field support for dotted keys: `log_attributes."user.name" = "alice"`
  - Type-aware SQL generation for Map, JSON, and String columns
- **Query History** - localStorage-based history per team-source, shows last 10 executed queries
- **Enhanced Source Stats** - Table schema info with column types, TTL expressions, sort keys, primary key display
- **Structured Error Handling** - Position-aware error reporting with line/column info, user-friendly messages
- Phase 2 P0 safety improvements for LogchefQL

### Fixed
- Preserve quoted literals and harden numeric coercion to avoid precision loss
- BigInt checks for safe integer range

## [0.4.0] - 2025-08-12

### Added
- **Query Variables** - Use `{{variable_name}}` in LogchefQL or SQL, input fields appear for each variable
- **Prometheus Metrics** - Comprehensive metrics with meaningful labels for monitoring
- **Grafana Dashboard** - Pre-built dashboard for Logchef monitoring
- **Compact Log Viewer** - Terminal-style compact view for log exploration
- Enhanced AI SQL Assistant with current query context
- Histogram toggle in UI
- Tooltips on theme switchers

### Changed
- Refactored table controls for consistent UI across viewing modes
- Refactored team/source context management for better robustness
- Simplified histogram generation with LogchefQL-only rule
- Replaced toast component with Sonner
- Centralized route↔store sync

### Fixed
- Query cancellation improvements
- Team switching race conditions and 403 errors
- Saved queries load when team sources aren't fully loaded yet
- Collections navigation route in SavedQueriesDropdown
- Handle Logchef QL variables properly in query translation
- Vue warnings in QueryEditor component
- Docker compose missing API token config

### Contributors
- [@songxuanqing](https://github.com/songxuanqing) - Query variables feature ([#9](https://github.com/mr-karan/logchef/issues/9))

## [0.3.0] - 2025-06-13

### Added
- **MCP Server Integration** - Model Context Protocol server for AI assistant integration
- MCP server documentation

## [0.2.2] - 2025-06-12

### Added
- **AI SQL Assistant** - Natural language to SQL query generation using OpenAI-compatible APIs
- **API Token Authentication** - Programmatic access via API tokens
- Query timeout settings and version info display
- TeamEditor role for saving queries to collections
- Logchef logo and credits

### Changed
- AI assistant includes current query context for better suggestions
- Updated quickstart instructions with specific release version

### Fixed
- Stale histogram data and empty table on source switch
- Panic when timestamp not in SELECT clause
- Query editor content updates on query_id change with KeepAlive

### Contributors
- [@vedang](https://github.com/vedang) - Placeholder text improvements
- [@r--w](https://github.com/r--w) - Documentation link fix
- [@gowthamgts](https://github.com/gowthamgts) - Quickstart link fix

## [0.2.0] - 2025-04-27

Initial public release.

### Added
- **Log Explorer** - Interactive log exploration with filtering and search
- **LogchefQL** - Custom query language for log filtering
- **SQL Mode** - Full ClickHouse SQL support for advanced queries
- **Saved Queries** - Save and share queries within teams
- **Team Management** - Multi-tenant access with RBAC
- **Source Management** - Configure multiple ClickHouse data sources
- **Histogram Visualization** - Time-based log distribution charts
- **Monaco Editor** - Syntax highlighting and autocompletion
- **OIDC Authentication** - Single sign-on support
- **Dark/Light Theme** - User preference support
- **Docker Deployment** - Docker Compose setup for quick start

### Infrastructure
- Single binary deployment
- SQLite for metadata storage
- ClickHouse for log storage
- Embedded web UI
- Prometheus metrics endpoint

[1.6.0]: https://github.com/mr-karan/logchef/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/mr-karan/logchef/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/mr-karan/logchef/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/mr-karan/logchef/compare/v1.2.1...v1.3.0
[1.2.1]: https://github.com/mr-karan/logchef/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/mr-karan/logchef/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/mr-karan/logchef/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/mr-karan/logchef/compare/v0.6.0...v1.0.0
[0.6.0]: https://github.com/mr-karan/logchef/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/mr-karan/logchef/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/mr-karan/logchef/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/mr-karan/logchef/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/mr-karan/logchef/compare/v0.2.0...v0.2.2
[0.2.0]: https://github.com/mr-karan/logchef/releases/tag/v0.2.0
