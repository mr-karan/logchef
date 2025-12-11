# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Field Values Sidebar** - Kibana-inspired field exploration panel that displays available fields and their top distinct values
  - Shows top 10 unique values for `LowCardinality` and `String` columns with occurrence counts
  - Click any value to instantly add it as a filter (`field="value"`) or exclude it (`field!="value"`)
  - Auto-expands fields with 6 or fewer distinct values for quick access
  - Respects the selected time range to show relevant values and optimize query performance
  - Displays value count badges on collapsed fields
  - Supports query cancellation for long-running field value queries
- Backend LogchefQL parser (`internal/logchefql/`) - full parsing, validation, and SQL generation in Go
  - Pipe operator (`|`) for custom SELECT clauses: `namespace="prod" | namespace msg.level`
  - Dot notation for nested JSON fields: `log_attributes.user.name = "john"`
  - Quoted field support for dotted keys: `log_attributes."user.name" = "alice"`
  - Type-aware SQL generation for Map, JSON, and String columns
- LogchefQL API endpoints for translation (`/logchefql/translate`), validation (`/logchefql/validate`), and direct query execution (`/logchefql/query`)
- API endpoints for field value exploration (`/fields/values`, `/fields/:fieldName/values`)
- Query cancellation support - cancel long-running queries from the UI with the Cancel button or `Esc` key, which also cancels the query in ClickHouse
- Frontend API client for LogchefQL backend integration

### Changed
- **Breaking:** LogchefQL parsing and SQL generation moved from frontend to backend
- **Architecture:** Backend is now the single source of truth for SQL generation
  - LogchefQL queries execute via `/logchefql/query` - backend builds and executes full SQL
  - "View as SQL" button shows the actual executed SQL from backend
  - Mode switching (LogchefQL → SQL) fetches full SQL from backend via `/logchefql/translate`
- LogchefQL validation now uses backend API with debounced calls
- Frontend no longer generates SQL - only renders what backend provides

### Fixed
- Histogram queries now work with MATERIALIZED timestamp columns (fixes [#59](https://github.com/mr-karan/logchef/discussions/59))
  - ClickHouse's `SELECT *` does not include MATERIALIZED columns
  - Histogram query builder now explicitly adds the timestamp field to ensure it's available in subqueries

### Removed
- Frontend LogchefQL parser (`frontend/src/utils/logchefql/`) - replaced by backend implementation

## [0.6.0] - 2025-06-05

### Added
- Database-backed system settings infrastructure for runtime configuration management
- Admin Settings UI for managing alerts, AI, authentication, and server settings
- API endpoints for system settings management (list, get, update, delete)
- Config-to-database seeding on first boot with migration strategy
- Alertmanager health check functionality with test connection button
- Duplicate source feature for quick configuration copying
- Keyboard typeahead navigation in team member and source dropdowns
- Dedicated alert detail page with edit and history tabs
- Alertmanager integration with enhanced alert management and delivery
- Alert history retention limit enforcement
- config.sample.toml showing minimal essential configuration

### Changed
- Runtime configuration now loaded from database, overriding config.toml for non-essential settings
- Simplified config.sample.toml to show only bootstrap essentials (server, sqlite, oidc, auth, logging)
- AI base_url now defaults to standard OpenAI API endpoint (https://api.openai.com/v1)
- Redesigned alerts list UI for better readability
- Simplified alerts by removing query_type and lookback_seconds fields

### Fixed
- Database settings now actually used at runtime (LoadRuntimeConfig integration)
- Active tab persistence when saving settings (no longer jumps to Alerts tab)
- Number input values properly converted to strings before API submission
- Acronyms (URL, API, AI, TLS, ID) now properly formatted in settings UI
- Alert delivery_failed flag cleared after successful Alertmanager retry
- Available users and sources now sorted alphabetically in team dialogs
- Null check added for test query warnings in AlertForm
- Frontend context and source loading logic improvements
- Duplicate updateCustomFields method removed from monaco-adapter

### Removed
- Rooms feature (refactored out)

## [0.5.0] - 2025-10-03

Initial release with alerting system, team management, and LogChef QL query language.

[Unreleased]: https://github.com/mr-karan/logchef/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/mr-karan/logchef/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/mr-karan/logchef/releases/tag/v0.5.0
