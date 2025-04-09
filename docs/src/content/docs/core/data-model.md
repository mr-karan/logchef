---
title: Data Model
description: Understanding LogChef's data model and metadata storage
---

LogChef uses a dual-database approach to efficiently manage both log data and system metadata:

## SQLite Metadata Store

SQLite serves as LogChef's metadata store, managing all system configuration, user information, and organizational relationships. The database schema includes:

### Sources

- Represents connections to remote ClickHouse databases
- Stores connection details (host, credentials, database name, table name)
- Each source can be shared with multiple teams

### Users & Authentication

- User accounts with email, name, and role information
- Session management for tracking active user sessions
- Integration with OIDC authentication

### Teams

- Organizational units for grouping users
- Role-based access within teams (admin, member)
- Controls access to log sources

### Saved Queries

- Team-specific saved queries for reuse
- Supports both SQL and LogChefQL query types
- Links queries to specific sources

## ClickHouse Log Storage

LogChef connects to multiple remote ClickHouse databases (called "sources") to execute log queries:

- **Schema Flexibility**: When creating a source, you can:

  - Use your own custom schema
  - Customize the built-in OpenTelemetry (OTEL) schema
  - Use the default OTEL schema as-is

- **Minimal Requirements**: The only mandatory requirement is that your log table must have a timestamp field (DateTime or DateTime64)

- **Schema Agnostic**: Beyond the timestamp field, LogChef is entirely schema-agnostic and can work with any column structure

- **Example Fields**: A typical OTEL-based schema might include:

  - `timestamp`: When the log was generated (required)
  - `severity_text`: Log level (ERROR, WARN, INFO, DEBUG)
  - `severity_number`: Numeric representation of severity
  - `service_name`: Name of the service generating the log
  - `namespace`: Logical grouping (e.g., application area)
  - `body`: The actual log message content
  - `log_attributes`: Additional contextual fields as key-value pairs

- LogChef executes optimized queries against these sources and returns results to the UI

This separation of concerns allows LogChef to efficiently scale by keeping system metadata separate from high-volume log data.
