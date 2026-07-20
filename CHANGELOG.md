# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Logchef v2.0 is a big release, and it starts with one thing: **VictoriaLogs is
now a first-class datasource.** Point Logchef at a VictoriaLogs instance and
explore it exactly like ClickHouse — same query box (LogchefQL or raw LogsQL),
the same live tail, the same alerts — with no separate tooling to learn.

There's a lot more in the box:

- **Run Logchef without an external login provider.** Built-in email + password
  auth means you can stand it up with zero OIDC setup — and if you do use SSO, it
  can now create users automatically on their first login.
- **Watch logs live.** Turn on live tail and matching rows stream in as they
  arrive, on either backend.
- **Better dashboards.** Build multi-panel views by dragging and resizing panels
  right on the grid, with time-series, stat, and table panels and line/area/bar
  chart styles.
- **A much stronger CLI (v0.2.0).** New `explain`, `histogram`, `fields`,
  `doctor`, and `open` commands, live-tail streaming in your terminal, and full
  VictoriaLogs support — see the CLI v0.2.0 notes below.
- **Faster on big results, and hardened throughout.** Large query results now
  stream instead of buffering in memory, and a broad security and reliability
  pass tightened dashboards, the VictoriaLogs connector, and query handling.

Full details below.

### Added
- **VictoriaLogs datasource.** Add a VictoriaLogs source (base URL, auth mode:
  none/basic/bearer, optional multi-tenant `account_id`/`project_id`, and an
  optional immutable scope query applied server-side to every query). Query it in
  LogchefQL (compiled to LogsQL) or write native LogsQL directly. Alerts support
  both a LogchefQL condition builder (with a field-and-aggregate picker) and
  native LogsQL via `stats_query`. See the
  [VictoriaLogs guide](https://logchef.app/tutorials/victorialogs/). Known gaps:
  AI SQL generation, log context ("surrounding logs"), and streamed
  exports/downloads remain ClickHouse-only for now.
- **Datasource capability model.** Each source now advertises a capability set
  (`schema_inspection`, `histogram`, `field_values`, `source_inspection`,
  `ai_sql_generation`, `log_context`, `exports`, `live_tail`) that both the API
  and UI gate on, instead of hardcoded source-type checks. ClickHouse supports
  all eight; VictoriaLogs currently supports the first four plus `live_tail`.
- **Built-in local authentication.** Set `[auth.local]` (`enabled`,
  `admin_email`, `admin_password`, or the matching `LOGCHEF_AUTH__LOCAL__*` env
  vars) to run Logchef with email+password auth, with or without OIDC. The
  login page shows whichever are configured. Logins are rate-limited per IP and
  per email. The bootstrap admin's password is reconciled on every startup, so
  rotation is a config change plus restart; there is no self-service password
  change yet. See [Local authentication](https://logchef.app/getting-started/configuration/#local-authentication-run-without-oidc).
- **SSO auto-provisioning (JIT user creation).** With `[auth.auto_provision]`
  (`enabled`, `allowed_domains`, `default_team_ids`, or the matching
  `LOGCHEF_AUTH__AUTO_PROVISION__*` env vars), a first-time OIDC login from an
  allowed email domain creates the user automatically instead of failing with
  "user not found". Off by default; `allowed_domains` is required when enabled
  and matched exactly (case-insensitive, no subdomains/wildcards). New users
  are always regular members (never admins), created after the
  `email_verified` check, joined to `default_team_ids` best-effort as role
  `member`, and left unmanaged by declarative provisioning. Browser login
  only; the CLI token exchange still requires an existing user. (Closes #34)
- **Live tail.** A **Live** toggle in the explorer streams matching rows in real
  time over SSE. ClickHouse sources are polled (`[tail] poll_interval`, default
  2s); VictoriaLogs sources proxy VictoriaLogs' native tail stream directly.
  Available in LogchefQL mode on any source, and in native mode on VictoriaLogs
  (LogsQL), but not available for raw ClickHouse SQL. Guardrails:
  `max_per_user`, `max_global`, `session_ttl`, `max_rows_per_sec` under `[tail]`.
- **Dashboards: grid-canvas editing.** Edit mode is now direct manipulation:
  drag a panel by its header to move it, drag its corner to resize. Adding or
  editing a panel opens a full-height panel builder drawer (replacing the old
  form sheet) with a live query preview.
- **Dashboards: chart styles.** Time series panels render as bars (default),
  line, or area, set per panel in the builder.
- **Dashboard result caching.** Dashboards cache panel query results
  server-side, per dashboard (default 10-minute TTL, configurable per dashboard,
  `0` = off), so a wall of auto-refreshing panels collapses to a single backend
  hit per TTL window instead of N panels × M viewers hammering
  ClickHouse/VictoriaLogs. Tunable via `[dashboard_cache]`; only dashboard
  panels are cached (ad-hoc explorer queries never are), and relative ranges are
  snapped to the TTL bucket so repeat views actually hit. Metrics under
  `logchef_dashboard_cache_*`.
- **Persistent query history & usage analytics.** Executed queries are recorded
  per user — browse your recent queries in the explorer and via `logchef
  history` in the CLI. Admins get a **Query Activity** view: recent activity
  across all users, plus all-time usage analytics (top sources, top users, query
  volume over time) backed by a non-pruned daily rollup.
- **Per-source ClickHouse query settings.** A source can pin ClickHouse query
  settings — `max_execution_time`, result/read row & byte caps, `readonly`,
  overflow mode — applied to every query on that source, as hard guardrails for
  shared clusters. Set them in the source form's advanced section.
- **Client-side column filters** in the results table. Click a column header's
  filter icon to narrow currently-loaded rows by a contains match, or a
  comparison (`>`, `>=`, `<`, `<=`, `=`) on numeric columns. No extra query is
  sent; filters reset on the next run. (Closes #4)
- **Renovate**: automated dependency update PRs for Go, npm, Cargo, and GitHub
  Actions, grouped by ecosystem with weekly scheduling. (Closes #62)

### Changed
- **Source credentials are never echoed back in API responses.** Source
  connection payloads now carry a `has_password` flag instead of the stored
  credential; blank credentials on update mean "keep existing" for both
  ClickHouse and VictoriaLogs providers. (Closes #49)
- **Explore mode naming is backend-neutral.** The editor's native mode is now
  called `native` (state `nativeQuery`) instead of the ClickHouse-era `sql`;
  old URLs, drafts, and query-share payloads with the legacy `sql` value still
  load correctly. Server contracts are unchanged. (Closes #46)
- **Histogram zero-fill.** Sparse or grouped time-series data (explorer and
  dashboard panels alike) now fills gaps between the first and last bucket with
  zero-count rows, so a sparse or grouped series renders as a continuous chart
  instead of floating bars.
- **Dashboard timezone handling.** Time-series panels align histogram buckets to
  the viewer's local timezone; table/stat panels are pinned to a UTC query
  internally so their time window no longer shifts for non-UTC viewers.
- **Streaming large query responses.** Preview query responses for ClickHouse
  sources now stream row-by-row instead of buffering the full JSON result in
  memory, removing a memory-spike path on large result sets. The response shape
  is unchanged. VictoriaLogs keeps the buffered path. (Closes #51)

### Fixed
- **Deleting a user now deletes their sessions too**: previously a removed
  user's existing session kept working until natural expiry. Expired sessions
  are also swept by the existing 15-minute background maintenance loop, which
  now shuts down cleanly on server stop. (Closes #53)
- **Condition-mode alerts aggregate over a real field.** `sum`/`avg`/`min`/`max`
  previously compiled against a literal `value` column on both backends and
  silently misbehaved on datasets without that field. The condition builder now
  has a field selector (schema-backed) and refuses to save without one. (Closes #43)
- **ClickHouse query results no longer lose their columns** on the alert
  evaluation path: a variable-shadowing bug zeroed out `columnsInfo` for every
  result, which the preview path masked but alert evaluation did not, failing
  any alert whose query returned rows. Present since the 1.7 query-path
  refactor; backported.
- **LogchefQL-to-SQL translate returns 400, not a silently-dropped `full_sql`**,
  on malformed time-range formats. (Closes #82)
- **Query-timeout detection for metrics is accurate**: ClickHouse timeout
  classification no longer relies on brittle string matching. (Closes #54)
- **The grouped-histogram legend no longer clips** in the explorer. (Closes #38)
- **Exports return a clean 400** (and the Download button hides) on sources
  that don't support them, instead of a 500 or a job that fails asynchronously
  after returning 202.

### Security
- **Opt-in rate limiting.** `[rate_limit]` adds per-IP and global limits on the
  unauthenticated auth/token endpoints and a per-user limit on query endpoints.
  Disabled by default (per-IP limiting needs trusted-proxy config to attribute
  the real client). Rejections counted under `logchef_rate_limit_rejections_total`.
- **Trusted-proxy client-IP resolution.** `[server] trusted_proxies` +
  `proxy_header` make `X-Forwarded-For` handling safe behind a reverse proxy: the
  forwarded header is read only when the direct peer is a configured trusted
  proxy (so it can't be spoofed), and invalid or overly-broad entries
  (`0.0.0.0/0`, `::/0`) are rejected at startup.
- **Dashboards hardening.** Fixes a stored XSS in the time-series crosshair
  tooltip, where grouped-series labels are attacker-controllable log values —
  they are now escaped. Cross-dashboard save corruption is isolated, and the
  concurrent-save clobber is closed with an optimistic-concurrency precondition
  (an `updated_at` mismatch returns `409` instead of overwriting a newer save).
  Panel LogsQL is dispatched per panel. Cross-team authorization is reworked:
  dashboards are visible from any team with per-panel redaction (a viewer sees
  only panels whose source they can reach), cross-team edits are blocked, and
  dangling team/source references and corrupt panel blobs are rejected and
  isolated rather than failing the whole dashboard. (Closes #119, #120)
- **VictoriaLogs provider hardening.** Custom headers can no longer override the
  computed auth/tenant headers or survive a switch to auth mode `none`
  (credential-leak fix), and switching auth mode no longer resurrects a stale
  credential. Connection validation now probes the real query path, not just
  field listing. LogsQL generation quotes the field names VictoriaLogs requires
  quoting (`@timestamp`, reserved words), and histogram series cap/labeling is
  corrected. The alert-lookback heuristic handles compound field names, nested
  `options(...)`, and top-level `OR` / negated `_time:`. A live-tail goroutine
  leak and a dropped-batch-on-cancel are fixed, and read deadlines are added to
  the histogram, schema, and test-alert request paths.

### Removed
- **Dead chip-based column filter component** in the results table, superseded
  by the new column-header filters. (Closes #32)

### Internal
- **Datasource provider refactor.** Sources moved from ClickHouse-shaped fields
  to a generic `connection_config` JSON blob keyed by `source_type`
  (`clickhouse` | `victorialogs`), with LogchefQL compilation, query execution,
  and inspection unified behind the provider layer.
- **golangci-lint debt cleared**: `just check` passes clean. (Closes #83)
- Nix flake files removed; Astro build cache untracked; Starlight social config
  fixed for the v0.33+ array format.
- VictoriaLogs real-instance integration suite and CI job; VictoriaLogs browser
  e2e scenario; host-network Docker Compose variant for local testing.
- **Dependency sweep.** Go and frontend dependencies bumped to latest — Go:
  `x/crypto`, `go-oidc`, Fiber 2.52.14, `clickhouse-sql-parser`; frontend: Pinia
  4, `vue`/`vue-router`/`reka-ui`/`vite`/`vitest`/`tailwind`/`zod`. Swept manually for this
  release; Renovate keeps them current afterward.

### Breaking changes
- **`provisioning.toml` source format changed.** Each `[[sources]]` now requires
  a `source_type` and a nested `[sources.connection]` block. The old flat layout
  (connection fields at the top level of `[[sources]]`) no longer parses, and the
  server **exits on startup** if it finds it (with a message pointing you here).
  If you provision sources via config, convert them before upgrading — teams,
  members, and non-connection fields (`description`, `ttl_days`, `meta_ts_field`)
  are unchanged:

  ```toml
  # Before (pre-2.0)
  [[sources]]
  name = "Production Logs"
  host = "clickhouse.internal:9000"
  database = "logs"
  table_name = "otel_logs"

  # After (2.0)
  [[sources]]
  name = "Production Logs"
  source_type = "clickhouse"

  [sources.connection]
  host = "clickhouse.internal:9000"
  database = "logs"
  table_name = "otel_logs"
  ```

  See the [provisioning guide](https://logchef.app/getting-started/provisioning/).

### Migration notes
| Backend | Migration | What it does |
|---------|-----------|---------------|
| SQLite | 000025 | Generalizes `sources` into a datasource-neutral shape: connection details move into `connection_config` JSON, discriminated by `source_type` (`clickhouse` \| `victorialogs`). |
| SQLite | 000026 | Adds `query_language` / `editor_mode` to `saved_queries` and `alerts`, backfilled from the legacy `query_type`. |
| SQLite | 000027 | Drops the legacy `query_type` column now that `query_language`/`editor_mode` are authoritative. |
| SQLite | 000028 | Adds `users.password_hash` (nullable; NULL means OIDC-only) for local authentication. |
| SQLite | 000029 | Creates `dashboards` (`panels_json` blob; `created_by` nulled, not cascaded, on user deletion). |
| SQLite | 000031 | Adds `query_history` (per-user executed-query log, capped at 200 rows/user). |
| SQLite | 000032 | Adds `query_stats_daily` (non-pruned daily usage rollup backing admin analytics). |
| Postgres | 000002-000007 | Mirrors the datasource, local-auth, dashboards, email-normalization, query-history, and usage-rollup migrations for the Postgres backend. |

All migrations apply automatically on upgrade; no manual steps required.

## [1.7.0] - 2026-07-06

Logchef 1.7 makes the **metadata store pluggable**: alongside the default
single-binary SQLite, you can now run an **opt-in Postgres backend** so multiple
replicas share state behind a load balancer for high availability. It also ships
a **redesigned Library** for saved queries and collections with a real
permission model (`owner` / `editor` / `member` + delegated edit), turns
**`alerts.enabled`** into a proper server-wide switch, surfaces **token expiry**
across UI / API / CLI, adds an admin **"All Queries"** browse, and clears a wave
of UX bugs. Under the hood: Go 1.26, a backend-agnostic store contract validated
by a conformance suite that runs against both databases, and audit-driven
hardening. **Breaking:** the Library URL consolidation (see below).

### Added
- **Pluggable Postgres metadata backend (opt-in).** Application metadata
  (users, teams, sources, saved queries, collections, alerts, API tokens,
  settings, sessions, export jobs, query shares) can now live in Postgres
  instead of SQLite, so multiple replicas can serve any request off shared
  state. **SQLite remains the default**; the zero-config single-binary start is
  unchanged. Select with `database.driver = "postgres"` and a `[postgres]` DSN
  (or `LOGCHEF_DATABASE__DRIVER` / `LOGCHEF_POSTGRES__DSN`). Startup migrations
  take a PostgreSQL advisory lock so concurrent replica boots don't race. Your
  logs always stay in ClickHouse and are unaffected.
  ([#96](https://github.com/mr-karan/logchef/pull/96)) See the
  [Database Backends & HA guide](https://logchef.app/operations/database-backends/).
- **`alerts.enabled` server switch.** Alerting can be disabled globally
  (`alerts.enabled = false` / `LOGCHEF_ALERTS__ENABLED=false`). When off, every
  alert endpoint returns a clear `503`, and `/api/v1/meta` exposes
  `alerts_enabled` so the UI hides alerting entirely.
  ([#98](https://github.com/mr-karan/logchef/pull/98))
- **"All Queries" browse view for admins**: `GET /api/v1/saved-queries?scope=all`
  (global-admin only) lists every saved query, including ones not reachable via
  any collection, each marked `runnable` for the caller (sources you can't reach
  show locked). Closes a gap where such queries had no browse surface. The
  default (source-gated) response used by the explorer and CLI is unchanged.
- **Redesigned Library.** The three saved-query / collection views collapse
  into a single `/logs/library` (collections rail + detail pane). Collections
  gain an **editor** role (`owner` / `editor` / `member`), and saved-query edit
  is **delegated**: creator, global admin, or an owner/editor of a shared
  collection containing the query can edit it (delete stays creator/admin-only).
  The Save dialog gains an inline collection picker, and the server sends
  `can_edit` / `can_delete` hints so the UI only offers actions that will work.
- **Curate collections without owning them.** Adding, moving, and removing
  queries in a shared collection is now open to any participant
  (owner / editor / member). Managing the collection itself (rename, delete,
  members) stays owner-only.
- **Collection detail upgrades**: pin an existing saved query via an "Add query"
  searchable picker, "Move to another collection" per query, and a "Created by"
  column showing each query's author.
- **Type-to-filter pickers + searchable, sortable tables** across member and
  resource management. A reusable searchable picker replaces plain dropdowns
  (invite a collection member, add a service account to a team), and the Manage
  Sources and team-member tables gain a search box and sortable columns.
- **Token expiry surfaced everywhere**: the service-tokens admin page shows the
  same expiry status as the profile API-token list (never expires / expires /
  expiring soon / expired) via a shared helper; the API-token model gains a
  computed `expired` flag; and the CLI flags an expired saved token in
  `logchef auth current`.

### Changed
- **Backend-agnostic store layer.** The metadata layer was reorganized behind a
  per-domain store contract with canonical sentinel errors (`ErrNotFound` /
  `ErrConflict`) and a `WithTx` transaction abstraction; SQLite and Postgres are
  symmetric implementations, validated by a shared conformance suite that runs
  against both in CI. `internal/sqlite` moved under `internal/store/sqlite`.
- **Upgraded to Go 1.26**, with hot-path optimizations and idiom modernization.
- **Anyone can create collections.** The old team-admin gate is dropped;
  per-collection roles (`owner` / `editor` / `member`) are the authority.
- **Collection member roster is owner-only**: previously visible to any
  team-admin who could list users; now enforced server-side.

### Fixed
- **Inline 403s no longer bounce to a full-page Forbidden view**: an access
  error on an inline action (toggle, save, delete) shows a toast and stays put;
  page-level access is still enforced by the router.
- **Dead toggles across the app work again**: Switch/Checkbox controls were
  bound to `:checked`/`@update:checked`, but the reka-ui primitives only expose
  `model-value`, so the admin Active toggle, alert enable/disable, source
  TLS/auth switches, the column selector, and variable multi-selects silently
  no-op'd. Rebound to `model-value`.
- **Saved-query resolver no longer panics** on certain resolve paths. It
  recovers and returns a clean error.
- **Saved queries wait for the source schema** before running, so opening one
  no longer races the previously selected source.
- **Save dialog only offers collections you own** (adding an item is owner-only,
  so it no longer saves the query then 403s on the pin), and the item Remove
  button gates on the current collection's ownership.
- **Correct HTTP status codes** for export / query-share access: `404` only on
  not-found, `403` when the recipient has no team access.
- **Escape-aware response byte-budget**: fixes an under-count memory regression
  from the perf pass, plus a cancellable field-value fan-out and identifier
  validation on provisioned source database/table/field names.
- **Provisioned member users get `account_type` set** correctly.
- **Add-query dialog width** no longer overflows its grid; **`?view=all`** is
  preserved on the Library route.
- **Frontend typecheck is green again and re-enabled in CI**: deduped
  `@internationalized/date` (reka-ui date-picker type drift) and cleared the
  assorted `vue-tsc` issues that had accumulated behind a disabled check.

### Breaking changes
- **Library URL consolidation.** `/logs/saved`, `/logs/collections`, and
  `/logs/collections/:id` collapse into a single `/logs/library` with **no
  redirects** from the old paths. `/logs/saved/:queryId` is kept as the
  canonical share / explorer-hydration link. Update bookmarked or documented
  old collection URLs.
- **Collection creation is no longer team-admin-gated**: any authenticated
  user can create a collection.

### Migration notes
| Backend | Migration | What it does |
|---------|-----------|-------------|
| SQLite | 000024 | Rebuilds the `collection_members` role CHECK to `owner \| editor \| member` (adds the collection **editor** role). Existing rows preserved. Applied automatically on upgrade from 1.6.1; no other new SQLite migrations. |
| Postgres | 000001_init | Fresh Postgres backends create the full schema in a single advisory-lock-guarded init migration. |

### Internal
- Backend-parity end-to-end suite (agent-browser) covering login, sources,
  query, field values, the time-range picker, histogram, collections, and admin.
- Dead-code sweep, a dev Postgres 17 service, and Postgres CI (service +
  sqlc-drift + golangci-lint to zero across the module).

### Upgrading
Drop-in for existing SQLite deployments; no config changes required (one small
SQLite migration, `000024`, applies automatically). To adopt Postgres for HA,
read the [Database Backends & HA guide](https://logchef.app/operations/database-backends/)
first. Note the current caveat that alert evaluation must run on **exactly one**
replica until leader election lands.

## [CLI v0.2.0] - Unreleased

Logchef CLI 0.2.0 is the VictoriaLogs release: `query`, `sql`, `find`, and
`tail` all work against VictoriaLogs sources, and `tail` follows both backends
over a native SSE stream (with a `--poll` fallback) instead of the old polling
loop. It also adds a batch of inspection and setup commands — `explain`,
`fields`, `histogram`, `open`, `doctor`, `skills`, and `completions` — plus a
global `--quiet`, syntax-highlighted generated queries, and actionable error
hints.

### Added
- **`explain` command**: Print the generated ClickHouse SQL / VictoriaLogs
  LogsQL for a LogchefQL query without executing it. Syntax-highlighted.
- **`fields` command**: Discover fields on a source and their top values, for
  building a query without opening the explorer.
- **`histogram` command**: Counts-over-time buckets rendered as a terminal bar
  chart. Works against both ClickHouse and VictoriaLogs sources.
- **`open` command**: Open the current query in the web explorer. Accepts a
  relative `--since` or an absolute `--from` / `--to`, `--sql` for a native
  query, and `--print` to emit the URL instead of launching a browser.
- **`doctor` command**: Diagnose your setup — config, auth, server
  reachability, version compatibility, and resolved defaults — with fix hints
  for each failing check. `--json` for machine-readable output.
- **`skills` command**: Serve the bundled, version-matched CLI skill for AI
  agents: `logchef skills get core [--full]`.
- **`completions` command**: Generate shell completions for bash, zsh, fish,
  and powershell.
- **Native SSE `tail`**: `tail` follows both ClickHouse and VictoriaLogs sources
  over a server-sent-events stream, replacing the old bounded-polling loop. Use
  `--poll` to fall back to polling.
- **Global `--quiet` / `-q`**: Suppress progress and diagnostic output on any
  command for clean scripting.

### Changed
- **VictoriaLogs parity**: `query`, `sql`, `find`, and `tail` now work against
  VictoriaLogs sources, not just ClickHouse. `sql --since` / `--from` / `--to`
  time injection and `query --dry-run` are fixed for VictoriaLogs.
- **Syntax-highlighted generated queries**: `explain` and the `--show-sql`
  trace on `query` / `sql` are syntax-highlighted on a TTY.
- **Actionable error hints**: Command failures now suggest the likely fix
  (wrong source, missing auth, unsupported capability) instead of just
  surfacing the raw server error.

## [CLI v0.1.6] - 2026-05-20

Logchef CLI 0.1.6 ships four new subcommands (`saved-queries`, `find`, `tail`,
`whoami`, `auth current`), full time-range injection on raw SQL, agent-friendly
output formats (`msg`, `json-flat`), a symmetric `--explain` / `--dry-run`
split across `query` and `sql`, and TTY-aware highlighting so pipes don't need
`--no-highlight`. Requires Logchef server v1.6.1+ for the saved-queries
resolve endpoint and ClickHouse column descriptions.

### Added
- **`saved-queries` command**: List saved queries and run one by name,
  numeric ID, or pasted explorer URL, with `--limit`, `--var`, and
  `--output` overrides.
- **`find` command**: Discover sources with recent matches for a service,
  job, host, or message pattern. For each matched source, fires a small
  per-column sample query: label-shaped columns (service/host/job_name) get
  the top 3 values with counts; free-form text columns (msg) get a single
  truncated sample row. Suppress with `--no-samples`. Per-source query
  timeout defaults to 30s. Sources that error out (permissions, schema
  fetch, query failure) are skipped and counted; rerun with `--debug` for
  per-source diagnostics.
- **`tail` command**: Follow matching LogchefQL rows with bounded polling
  and `text`, `jsonl`, or `msg` output. Dedup is stable across column-order
  changes between polls; when a poll returns at `--limit` a one-shot warning
  hints to raise `--limit` or shrink `--interval`.
- **`whoami` command**: Print the authenticated user and accessible teams.
- **`auth current` subcommand**: Offline command that prints the active
  context, server URL, and token source (env vs config) without hitting the
  network. When the token comes from saved config, also prints the expiry
  timestamp. Useful for "is my `LOGCHEF_AUTH_TOKEN` even set?" diagnostics
  before any API call.
- **SQL time flags**: `logchef sql` now accepts `--since`, `--from`, and
  `--to`. The predicate is injected before the first top-level
  `GROUP BY` / `ORDER BY` / `LIMIT` / `HAVING` / `SETTINGS` / `FORMAT`; the
  scanner skips string literals, quoted identifiers, comments, and
  parenthesized subqueries, so `WHERE`/`LIMIT` inside literals or nested
  selects no longer confuses injection. Use `__START__` / `__END__`
  placeholders for full control (e.g. CTEs).
- **`--explain` / `--show-sql` alias on `sql`**: `--explain` is now an alias
  of `--show-sql` on both `query` and `sql`. Both print
  `Generated SQL: <sql>` to stderr and continue executing, so the trace
  coexists cleanly with `--output jsonl | jq` pipes. On `sql`, the printed
  SQL includes any `--since` / `--from` / `--to` injection.
- **`--dry-run` on `query` and `sql`**: Prints the resolved SQL to stdout
  (no prefix, pipes cleanly) and exits without keeping results.
  `query --dry-run` still calls the server once for LogchefQL translation;
  `sql --dry-run` is fully offline.
- **`--output msg` mode**: `query`, `sql`, `collections`, and
  `saved-queries` can print message text only, one row per line. Falls back
  to the first selected column when `msg` isn't projected.
- **`--output json-flat` mode**: `query`, `sql`, `collections`, and
  `saved-queries` can hoist JSON-shaped `msg` fields to top-level JSON rows.
- **`LOGCHEF_DEFAULT_TEAM` / `LOGCHEF_DEFAULT_SOURCE` env vars**: Supply
  stateless defaults when `--team` / `--source` are omitted. Precedence:
  flag > env > saved config.
- **Schema column descriptions**: `schema --output text` shows an extra
  DESCRIPTION column when the source's ClickHouse columns have comments;
  `schema --output json` includes them inline.

### Changed
- **Auto-disables ANSI highlighting on non-TTY output**: All five subcommands
  (`query`, `sql`, `collections`, `saved-queries`, `tail`) skip highlighting
  when stdout is piped, so `... | jq` and `... > file` produce clean output
  without `--no-highlight`. The flag still works as an explicit override.

### Internal
- Shared `session` module extracts `ResolvedContext` + auth check across the
  nine subcommands (~250 LOC dedup).

## [1.6.1] - 2026-05-20

Patch release. Introduces **Service accounts** (non-login principals that
own scoped API tokens) and reworks the API-token surface so every token
carries an explicit scope list enforced by middleware. Also surfaces
ClickHouse column comments through the schema API for the Logchef CLI v0.1.6
to consume.

### Added
- **Service accounts**: Non-login principals you can add to teams and own
  API tokens. Created from **Administration → Service Tokens**. Cannot
  authenticate via OIDC or CLI exchange.
- **Scoped API tokens**: Tokens now carry an explicit scope list
  (`logs:read`, `alerts:write`, ...). New `requireTokenScope` middleware
  enforces them on every route. Presets in the UI: Read-only, Logs viewer,
  Logs analyst, Alerts manager, Source admin, Full access. Active preset is
  highlighted while the selection matches.
- **Account-type toggle in Add Team Member dialog**: Switch between Human
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
- **Schema column descriptions**: The schema API now surfaces ClickHouse
  column comments as an optional `description` field. Consumed by Logchef
  CLI v0.1.6's `schema` command.

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

Logchef 1.6 narrows the **Team** abstraction to access control only and adds
**Collections**: cross-team curation lists for saved queries. The unified
Saved Queries view replaces the old team-scoped collections page with a flat,
searchable table and a collection-picker dropdown. The release also adds a
new **Team Editor** role and restructures the admin URLs for consistency.

### API & URL changes
- **Saved queries are source-scoped, not team-scoped.** `team_queries` is
  rebuilt as `saved_queries(source_id, created_from_team_id, created_by, …)`.
  Visibility: any user with source access via any team. Edit: creator +
  global admin.
- **`/api/v1/saved-queries/:id/resolve`** returns a transient
  `resolved_team_id` computed from the user's access paths; no team
  ownership stored on the query itself.
- **Alerts de-teamed.** New `/api/v1/alerts` route group; `alerts.team_id`
  dropped, `alerts.created_by` added.
- **`query_shares` and `export_jobs` lose `team_id`.**
- **New `/api/v1/collections`**: CRUD for collections + members + items.
- **Collection mutation routes use `requireAnyTeamCollectionMutator`**
  (admin or editor in any team). Team admin-only routes (membership
  management, source linking, `/api/v1/users`) stay strict on
  `requireAnyTeamAdmin`.
- **Admin frontend URLs restructured.** `/management/*` → `/admin/*`,
  `/profile` → `/settings/profile`, `/admin/sources/list` → `/admin/sources`,
  `/admin/sources/edit/:id` → `/admin/sources/:id/edit`. No redirects from
  old paths.
- **Old team-scoped paths return 404.** No shims, no redirects.
- **Frontend URL:** `/logs/saved/:queryId` is the canonical share link.

### Added
- **Collections**: Cross-team curation lists. Personal collection
  auto-created per user ("My Collection"). Shared collections are
  invite-only with `owner` + `member` roles. Items a member can't run
  show with a `runnable: false` flag (lock icon in UI).
- **Unified Saved Queries view**: Single page at `/logs/saved` with a
  collection-picker dropdown (All Queries / My Collection / shared
  collections), inline search, and a Metabase-style flat table.
- **"Add to Collection" drawer**: Per-row action on saved queries.
  Slide-out panel shows all collections as checkboxes for quick
  pin/unpin. Create new collections inline.
- **"Remove from collection" action**: When viewing a specific
  collection, each row's menu gains a destructive remove action.
- **Saved query resolve with `resolved_team_id`**: The `/resolve`
  endpoint deterministically picks the correct team for execution using
  priority: explicit `?team_id` hint → `created_from_team_id` →
  first accessible team fallback.
- **`created_from_team_id`** on saved queries: nullable metadata
  recording which team context the query was saved from. Used as a
  preference hint during resolve; not an ACL gate.
- **Invite members by email**: Collection member invite uses an email
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
- **Team Editor role**: new team role between Member and Admin. Editors
  can manage collections (create, rename, invite, pin items) and save
  queries. They cannot invite team members or link sources; those stay
  admin-only. ([#94](https://github.com/mr-karan/logchef/pull/94))
- **Shared UI primitives**: `PageHeader`, `PageSection`, `EmptyState`,
  `LoadingState` under `components/layout/`. Replace the ad-hoc empty/
  loading/header markup across admin and settings pages with one
  consistent visual language.
- **`useTeamPermissions()` composable**: central frontend role-check
  API: `isGlobalAdmin`, `isAnyTeamAdmin`, `isAnyTeamCollectionMutator`,
  `isTeamAdmin(teamId)`, `isTeamCollectionMutator(teamId)`,
  `canSaveQuery`, `canEditSavedQuery(query)`, `canManageCollection(c)`.
- **Tests**: 18 backend cases for the new role helpers (cross-team
  negatives + regression guards that editors stay distinct from admins),
  28 frontend Vitest cases for `useTeamPermissions`.

### Changed
- **Saved Queries view is now the unified entry point.** The old two-page
  layout (separate /logs/saved + /logs/collections list) is replaced by
  a single flat table with the collection picker. `/logs/collections`
  is a standalone management page (create, delete, navigate to detail).
- **Alert notifications** drop `team_id` / `team_name` fields.
  Recipients resolve to users directly.
- **Explore UI polish**: quieter top bar, concrete query placeholders,
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
- **Unbounded query result OOM**: `[query] max_limit` cap (default 100k
  rows). A large unbounded result set previously exhausted the browser
  renderer.
- **Long raw SQL in the URL** no longer trips the HTTP header size limit
  on the server.
- **"No source selected" race**: explorer waits for
  `currentSourceDetails.id === contextStore.sourceId` before executing,
  so newly-selected sources don't run the previous source's query.
- **Stale-request guard** in `sourcesStore.loadSourceDetails`: a fast
  source switch is no longer overwritten by an older in-flight response.
- **Saved query loads wrong source**: resolved query's `source_id` now
  overrides stale URL `?source=` param.
- **Crash-safe export pruner**: interrupted prunes no longer leave
  orphaned download files behind.
- **Translate API errors are surfaced to the editor** instead of failing
  silently.
- **Export download URLs are relative**, so downloads work behind reverse
  proxies that rewrite hostnames.

### Removed
- **Query Folders** (the team-scoped experiment from v1.6.0-dev).
- **Bookmarks** (`is_bookmarked` column): replaced by personal
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
- [@m0nikasingh](https://github.com/m0nikasingh): OIDC email
  verification skip
  ([#86](https://github.com/mr-karan/logchef/pull/86)), native ClickHouse
  TLS ([#88](https://github.com/mr-karan/logchef/pull/88)), AI SQL
  insert mode fix ([#89](https://github.com/mr-karan/logchef/pull/89))

## [1.5.0] - 2026-04-08

### Added
- **Rich value autocomplete in LogchefQL editor**: After typing `host=`, the editor instantly suggests top field values with occurrence counts (e.g., `cdn.logchef.dev (1.7K)`). Suggestions come from the sidebar's cached field data, so no additional network calls happen during typing. Supports partial matching inside quotes, auto-quoting string values, and proper escaping of special characters.
- **Numeric field values in sidebar**: Fields like `status` (UInt16) and `bytes` (UInt32) now appear as filterable fields and auto-load their top values alongside LowCardinality fields.
- **Shared field values cache**: New Pinia store (`exploreFieldValues`) allows the sidebar and editor to share field value data, eliminating redundant API calls.

### Changed
- **Tailwind CSS v4 migration**: Upgraded from Tailwind v3 to v4 with oklch color system, `@theme` directives, and `@tailwindcss/vite` plugin (replaces PostCSS).
- **shadcn-vue Vega theme**: Switched from new-york to Vega style with Zinc base and Blue accent. Small border radius for a sharper, more technical look.
- **Sidebar defaults to collapsed**: Icon-only mode maximizes screen real estate for log viewing. Expand via rail hover or `Cmd+B`.
- **Theme toggle moved to sidebar footer**: Single-click cycle (Light → Dark → System) instead of buried in dropdown menu.
- **Histogram charts use Unovis**: Migrated from custom chart to Unovis with brush-drag zoom, crosshair tooltips, and stacked bar support.
- **Monaco editor lazy-loaded**: SQL editor loads on demand, reducing initial bundle for LogchefQL-only users.
- **Bolder chart colors**: Blue chart gradient shifted one step darker for better visibility on both light and dark backgrounds.

### Fixed
- **Hyphenated field names work everywhere**: Fields like `user-identifier` are now backtick-quoted in all SQL queries (field values, histograms, group-by). Previously caused `user - identifier` subtraction errors.
- **Validation errors return 400, not 500**: Invalid field names, timezones, and identifiers now return proper HTTP 400 Bad Request with `ValidationError` type instead of 500 Internal Server Error.
- **Histogram tooltip styling**: Fixed broken tooltip background/border after TW4 migration (`hsl(var(--...))` → `var(--...)`).
- **Histogram crosshair null guard**: Added optional chaining (`row?.ts`) to prevent crash when data is empty.
- **Editor line height mismatch**: Fixed LogchefQL editor height calculation (`baseLineHeight: 21` → `20` to match Monaco's `lineHeight`).
- **1-second histogram buckets**: Support for sub-minute bucket intervals in ClickHouse.
- **Brush zoom restored**: Click-to-zoom on histogram bars works alongside brush-drag selection.
- **Grouped histogram string values**: Fixed dereferencing of grouped string values in histogram data.
- **Session cookie handling**: Fixed local dev cookie configuration and team provisioning.
- **Team admin permissions**: Team admins can now manage members on managed (provisioned) teams.
- **Idle connection cleanup**: Added `IdleTimeout` and periodic `QueryTracker` cleanup to prevent connection leaks.
- **Noisy logs reduced**: Session management logs downgraded to DEBUG; structured slog source shortened to `file:line`.
- **Cursor pointer restored**: Added `cursor-pointer` base rule for all interactive elements (TW4 preflight removed it).

## [1.4.1] - 2026-04-02

Maintenance release on top of v1.4.0.

### Added
- **Canonical request logging**: Every API request emits a structured log
  line with the method, path, status, latency, user, and team. Companion
  activity log tracks user-visible state changes for audit.

### Changed
- **Product name standardized to "Logchef"**: Replaced lingering "LogChef"
  casing across the UI, docs, and log lines.
- **Session management logs dropped from INFO to DEBUG.** Only `user.login`
  stays at INFO; the rest is audit-grade noise that doesn't belong in the
  default log stream.
- **`slog` source field flattened to `file:line`.** Easier to grep, fewer
  bytes per line.

### Fixed
- **Team admins can manage members on provisioned (managed) teams**:
  Previously the managed flag locked them out of all membership edits.
- **Idle ClickHouse connection cleanup**: Added `IdleTimeout` and a periodic
  `QueryTracker` sweep so leaked connections don't accumulate.
- **Provisioning docs moved into the sidebar** with a clearer "Getting
  started" sub-section so first-time admins actually find them.

## [1.4.0] - 2026-03-30

### Added
- **Declarative provisioning**: Define teams, sources, and access control in a TOML config file for GitOps-style management. Resources declared in config are tagged "managed" and fully controlled by config; UI-created resources are left alone. Supports dry-run mode, separate `provisioning.toml` file, and an admin export endpoint (`GET /admin/provisioning/export`). API rejects mutations on managed resources.
- **All Teams collections view**: Browse saved queries across all your teams from a single page. New "All Teams" option in the team dropdown on the Collections page shows queries with Team and Source columns.
- **SQL input validation**: Timezone, field name, and group-by inputs are now validated before SQL interpolation, preventing injection attacks on ClickHouse queries.

### Changed
- **Auth returns 401 for expired sessions**: Backend now returns HTTP 401 (not 403) for authentication failures, so the frontend correctly redirects to login instead of showing a Forbidden page.
- **Parallel source health checks**: Admin source listing now pings all sources concurrently instead of serially, reducing page load time proportional to source count.
- **OIDC audience validation enabled**: ID token verifier now validates the audience claim to prevent token confusion attacks.

### Fixed
- **Query cancellation works end-to-end**: LogchefQL queries now use a proper cancellable context (was no-op). Frontend preserves the query ID during cancellation so backend `KILL QUERY` can execute.
- **SQL mode no longer rewrites user queries**: Time range and limit changes no longer silently modify raw SQL in SQL mode, respecting the user's query as written.
- **Histogram timestamp detection**: The timestamp field check now inspects only the SELECT clause instead of the full query, preventing false positives when the field appears in WHERE/ORDER BY.
- **No duplicate query on page load**: The auto-execute watcher now skips if URL state initialization already triggered a query.
- **Post-login redirect preserved**: The requested page is now stored in a cookie through the OIDC round-trip, so users return to their original page after login.
- **Calendar highlights today**: Date picker calendar now opens focused on today's date with default times (00:00:00 for From, 23:59:59 for To).
- **Bookmark index covers sort**: New migration adds `updated_at` to the bookmark index for efficient sorted queries.
- **Frontend type errors fixed**: Resolved 3 pre-existing TypeScript errors in SourceSparkline, TeamsList, and SourceStats.
- **QueryEditor decomposed**: Extracted AiSqlDialog, VariableConfigSheet, and VariablesPanel into focused components (2645→1839 lines).
- **Context store migrated**: Converted from Options API to Composition API setup function for consistency.
- **QueryEditor props typed**: Replaced runtime prop definitions with TypeScript interface.

## [CLI v0.1.5] - 2026-05-19

### Added
- **CSV export & streaming SQL**: New `--output csv` and `--stream` flags on
  `logchef sql`. Stream large result sets directly without timing out, or
  pipe them to a CSV file.
- **v1.6.0 API compatibility**: CLI works with v1.6's de-teamed collections
  API. Saved queries resolve from your team membership automatically.

### Changed
- **Product name standardized**: "LogChef" → "Logchef" across all CLI help
  text, prompts, and the OIDC auth landing page.

## [CLI v0.1.4] - 2026-02-05

### Added
- **CLI `teams` command**: List teams available to your account.
- **CLI `sources` command**: List sources for a team with IDs and `database.table` references.
- **CLI `schema` command**: Show columns and types for a source without running SQL.

### Changed
- CLI errors for missing team/source now suggest `logchef teams` and `logchef sources --team <team>`.

## [1.3.0] - 2026-02-05

### Added
- **Configurable query result limit**: New `[query]` config section with `max_limit` setting (default: 1,000,000 rows). Allows admins to increase export limits based on infrastructure capacity. Frontend dropdown now shows options up to 1M rows.
- **User preferences persistence**: Theme, timezone, display mode, and fields panel state now persist across sessions. Preferences sync automatically and load on login.
- **Team admins can manage their teams**: Team admins now have access to team settings and member management without requiring global admin privileges.
- **Source editing and duplication**: Edit existing source configurations and duplicate sources for quick setup of similar data sources.

### Changed
- Query limit options now dynamically loaded from server config instead of hardcoded values.
- SQL editor now has max height (300px) with scrollbar for lengthy queries.

### Fixed
- Histogram now auto-refreshes when changing Group By column selection.
- Time icon in date picker now visible in dark mode.
- Date picker Now button auto-applies and fixes initial date format issues.
- JSON strings embedded in log fields now auto-parse for better readability.
- Table auto-resizes when filter sidebar closes.

## [1.2.2] - 2026-01-27

Maintenance release on top of v1.2.1. Bundles the CLI v0.1.3 bump.

### Changed
- **`versionString` linker flag now reaches the UI sidebar**, so the version
  badge matches the running binary instead of falling back to `unknown`.
- **Alertmanager UI settings removed**: Obsolete after the SMTP / webhook
  alert delivery work in v1.2.0.

### Fixed
- **Migration description for the TLS setting** was misleading; corrected.
- **Changelog template syntax** is now escaped so `{{ ... }}` examples render
  literally.

## [CLI v0.1.3] - 2026-01-27

### Added
- **`--timeout` flag on `query`**: Override the server-side query timeout
  from the CLI.
- **Timezone auto-detection on `auth`**: `logchef auth` now records your
  local IANA timezone in the saved context so subsequent queries use it by
  default. Config gained a `version` field so future schema changes can
  migrate.
- **Pre-built binary install docs**: `docs/integration/cli` now lists
  download URLs for Linux x86_64/aarch64, macOS x86_64/aarch64, and Windows.

### Fixed
- **LogchefQL prompt example** in the interactive mode used outdated syntax
  (`level=error` without quotes). Corrected to `level="error"`.

## [1.2.1] - 2026-01-21

### Fixed
- **Explore history URL hydration**: Fixed issue where browser history navigation could fail to restore query state correctly.

## [CLI v0.1.2] - 2026-01-21

### Added
- **CLI `collections` command**: List and run saved queries from the command line
  - `logchef collections` lists all saved queries for a team/source
  - `logchef collections run <id>` executes a saved query with all output formats
  - Supports filtering by bookmarked queries with `--bookmarked`
- **CLI interactive mode**: Run `query` or `sql` without arguments for guided prompts
  - Interactive team and source selection with arrow-key navigation
  - Proper line editing with history support via `inquire` crate
  - Auth command now uses `inquire` for server URL input with defaults
- **Copy CLI command button**: Click terminal icon in explore toolbar to copy equivalent CLI command
  - Generates `logchef query` or `logchef sql` command matching current query
  - Includes time range, limit, and timeout parameters

## [CLI v0.1.1] - 2026-01-21

### Added
- **CLI `sql` command**: Execute raw ClickHouse SQL queries directly from the terminal
  - Full SQL control including time filters, aggregations, joins, and CTEs
  - Read SQL from stdin with `-` for complex multi-line queries
  - Same output formats as `query`: text, json, jsonl, table

## [1.2.0] - 2026-01-21

### Added
- **Rust CLI**: New cross-platform command-line interface written in Rust
  - `logchef auth`: Browser-based OIDC authentication with PKCE flow
  - `logchef query`: Execute LogchefQL queries with syntax highlighting (powered by [tailspin](https://github.com/bensadeh/tailspin))
  - `logchef config`: Manage CLI configuration and multiple server contexts
  - `logchef query --no-timestamp`: Hide timestamps in text output for cleaner exports
  - Multi-context support for managing dev/staging/prod instances (kubectl-style)
  - Configurable keywords and regex patterns for log highlighting
  - Configuration stored at `~/.config/logchef/logchef.json`
- **CLI OIDC config**: `oidc.cli_client_id` added to `config.toml` and docs for browser-based CLI auth
- **CLI Token Exchange API**: `POST /api/v1/cli/token` endpoint for CLI authentication
- **CLI OIDC Discovery**: `/api/v1/meta` now includes `oidc_issuer` and `cli_client_id` for CLI auth flow
- **Multi-select variables**: Select multiple values that expand to `IN (...)` clauses in SQL.
- **SQL optional clauses** (`[[ ... ]]`): Wrap variable clauses to auto-remove when value is empty.
- **Variable widget configuration**: Configure variables as text inputs, dropdowns, or multi-selects with default values.
- **Collections "All Sources" view**: Browse saved queries across all sources in one place.
- **Alert delivery via SMTP and webhooks**: Send notifications directly without Alertmanager.
- Saved query name shown in browser tab title.
- Smart LIMIT handling in SQL mode.
- Support for CTEs, JOINs, and subqueries with template variables.

### Changed
- Saved queries persist variable widget configuration and defaults.
- Relative time range refreshes before each query execution.
- Reduced log noise and redacted session IDs for security.

### Fixed
- **SQLite SQLITE_BUSY errors**: Implemented dual-connection pattern (read pool + single write connection) to eliminate database lock contention under concurrent API requests.
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
- Legacy Go CLI (`cmd/logchef/`, `internal/cli/`): replaced by Rust CLI.
- `config.sample.toml`: superseded by the fully commented `config.toml`.

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

[1.7.0]: https://github.com/mr-karan/logchef/compare/v1.6.1...v1.7.0
[1.6.1]: https://github.com/mr-karan/logchef/compare/v1.6.0...v1.6.1
[1.6.0]: https://github.com/mr-karan/logchef/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/mr-karan/logchef/compare/v1.4.1...v1.5.0
[1.4.1]: https://github.com/mr-karan/logchef/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/mr-karan/logchef/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/mr-karan/logchef/compare/v1.2.2...v1.3.0
[1.2.2]: https://github.com/mr-karan/logchef/compare/v1.2.1...v1.2.2
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
