# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.1.0] - 2025-12-29

### Added
- **Bookmark Favorite Queries** - Star saved queries for quick access ([#60](https://github.com/mr-karan/logchef/pull/60))
  - Bookmarked queries appear at top of collections dropdown
  - Copy shareable URL for any saved query
  - Direct link format: `/logs/collection/:teamId/:sourceId/:collectionId`

### Changed
- **LogChefQL Parser Rewrite** - Replaced hand-written tokenizer with grammar-based parser using [participle](https://github.com/alecthomas/participle)
  - Better error messages with position-aware diagnostics
  - More maintainable and extensible grammar definitions
  - Improved query type detection (LogChefQL vs SQL)
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

- **Alerting with Alertmanager** - SQL-based alerts that integrate with your existing alert routing
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
- **Alerting System** - SQL-based alerting with Alertmanager integration
  - Create alerts using LogchefQL or SQL queries
  - Configure thresholds, frequency, and severity
  - Route alerts to Slack, PagerDuty, email via Alertmanager
  - Alert history with execution logs
  - Dedicated alert detail page with edit and history tabs
- **Admin Settings UI** - Runtime configuration management via web interface
  - Manage alerts, AI, authentication, and server settings
  - Settings stored in database, override config.toml at runtime
  - Config-to-database seeding on first boot
- Alertmanager health check functionality with test connection button
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
- Alert `delivery_failed` flag cleared after successful Alertmanager retry
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
- **Grafana Dashboard** - Pre-built dashboard for LogChef monitoring
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
- Handle LogChef QL variables properly in query translation
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
- LogChef logo and credits

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

[Unreleased]: https://github.com/mr-karan/logchef/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/mr-karan/logchef/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/mr-karan/logchef/compare/v0.6.0...v1.0.0
[0.6.0]: https://github.com/mr-karan/logchef/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/mr-karan/logchef/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/mr-karan/logchef/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/mr-karan/logchef/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/mr-karan/logchef/compare/v0.2.0...v0.2.2
[0.2.0]: https://github.com/mr-karan/logchef/releases/tag/v0.2.0
