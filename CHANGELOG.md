# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/mr-karan/logchef/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/mr-karan/logchef/releases/tag/v0.5.0
