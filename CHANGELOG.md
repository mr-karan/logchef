# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Field Values Sidebar** - Kibana-inspired field exploration panel that displays available fields and their top distinct values
  - Shows top 10 unique values for `LowCardinality`, `Enum`, and `String` columns with occurrence counts
  - Click any value to instantly add it as a filter (`field="value"`) or exclude it (`field!="value"`)
  - Auto-expands fields with 6 or fewer distinct values for quick access
  - Respects the selected time range to show relevant values and optimize query performance
  - Displays value count badges on collapsed fields
  - Sidebar filters values based on active LogchefQL query - shows only relevant values matching current filters
  - Auto-refresh on query execution - sidebar updates when you run a query
  - **Progressive per-field loading** - values load in parallel (max 4 concurrent) with per-field status
  - **Hybrid loading strategy** - LowCardinality/Enum fields auto-load, String fields require click (avoids slow high-cardinality queries)
  - Per-field error handling with retry button - one failed field doesn't block others
- Backend LogchefQL parser (`internal/logchefql/`) - full parsing, validation, and SQL generation in Go
  - Pipe operator (`|`) for custom SELECT clauses: `namespace="prod" | namespace msg.level`
  - Dot notation for nested JSON fields: `log_attributes.user.name = "john"`
  - Quoted field support for dotted keys: `log_attributes."user.name" = "alice"`
  - Type-aware SQL generation for Map, JSON, and String columns
- LogchefQL API endpoints for translation (`/logchefql/translate`), validation (`/logchefql/validate`), and direct query execution (`/logchefql/query`)
- API endpoints for field value exploration (`/fields/values`, `/fields/:fieldName/values`)
- Query cancellation support - cancel long-running queries from the UI with the Cancel button or `Esc` key, which also cancels the query in ClickHouse
- Frontend API client for LogchefQL backend integration
- Build commands: `just build-ui-analyze` for bundle analysis, `just clean-all` for deep clean including node_modules
- Double-click column header resizer to auto-fit column width to content

### Changed
- **Breaking:** LogchefQL parsing and SQL generation moved from frontend to backend
- **Architecture:** Backend is now the single source of truth for SQL generation
  - LogchefQL queries execute via `/logchefql/query` - backend builds and executes full SQL
  - "View as SQL" button shows the actual executed SQL from backend
  - Mode switching (LogchefQL → SQL) fetches full SQL from backend via `/logchefql/translate`
- LogchefQL validation now uses backend API with debounced calls
- Frontend no longer generates SQL - only renders what backend provides
- Field values API now accepts `logchefql` query param instead of `conditions` - backend handles parsing for proper SQL generation
- `/logchefql/translate` endpoint time parameters now optional (only required for full SQL generation)
- Pipe operator now includes timestamp field in SELECT for proper ordering
- Data table UX improvements:
  - Rows are more compact (reduced padding) for better log density
  - Expand/collapse indicator chevron on each row
  - Cell click-to-copy with visual feedback
  - Cell action buttons in floating overlay (doesn't affect column width)
  - Column resizing has no max constraint - resize freely as needed
- Column width defaults adjusted: lower minimums, higher maximums for flexibility

### Fixed
- Histogram queries now work with MATERIALIZED timestamp columns (fixes [#59](https://github.com/mr-karan/logchef/discussions/59))
  - ClickHouse's `SELECT *` does not include MATERIALIZED columns
  - Histogram query builder now explicitly adds the timestamp field to ensure it's available in subqueries
- Surrounding logs (context modal) now works with MATERIALIZED timestamp columns
- Field sidebar excludes complex types (Map, Array, Tuple, JSON) that can't have simple distinct values
- Field values queries now have 15s context timeout to prevent ClickHouse query pileup
- Removed "View as SQL" button from LogchefQL mode (unnecessary duplication)

### Removed
- Frontend LogchefQL parser (`frontend/src/utils/logchefql/`) - replaced by backend implementation

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

[Unreleased]: https://github.com/mr-karan/logchef/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/mr-karan/logchef/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/mr-karan/logchef/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/mr-karan/logchef/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/mr-karan/logchef/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/mr-karan/logchef/compare/v0.2.0...v0.2.2
[0.2.0]: https://github.com/mr-karan/logchef/releases/tag/v0.2.0
