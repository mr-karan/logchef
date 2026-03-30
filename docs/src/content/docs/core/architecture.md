---
title: Architecture
description: Comprehensive overview of Logchef's architecture, components, and data flow
---

## Architectural Overview

Logchef is architected as a specialized query and visualization layer on top of ClickHouse. Its design emphasizes a clear separation of concerns:

- **Query Engine**: Core focus on transforming user queries into optimized ClickHouse SQL
- **No Ingestion Pipeline**: The architecture intentionally excludes log collection, focusing exclusively on the query interface
- **ClickHouse Integration**: Deep integration with ClickHouse's query capabilities while maintaining schema flexibility

This architectural approach allows Logchef to leverage the existing ecosystem of log collection tools while providing a specialized interface for exploring logs once they're in ClickHouse.

## System Overview

![Logchef Architecture Diagram](./logchef-architecture.png)

### Technology Stack

#### Backend

- **Go**: Logchef's core backend is written in Go, providing high performance, concurrency, and efficient resource utilization
- **SQLite**: Lightweight database used for metadata storage of users, teams, sources, and saved queries
- **ClickHouse**: High-performance columnar database optimized for analytical queries on log data

#### Frontend

- **Vue.js**: Modern JavaScript framework used to build the reactive user interface
- **Tailwind CSS**: Utility-first CSS framework for styling the UI components

## Core Components

### 1. Query Engine

- Converts simple search syntax to optimized ClickHouse SQL
- Manages query execution across multiple sources
- Supports both simple search syntax and raw SQL for complex queries

### 2. Authentication Service

- Integrates with OIDC providers (like Keycloak, Zitadel etc)
- Manages user sessions and authorization
- Enforces role-based access control

### 3. Source Manager

- Manages connections to remote ClickHouse instances
- Handles source registration and validation
- Provides connection pooling mechanisms

## Data Flow

1. **Log Ingestion** (external to Logchef):

   - Various collectors (Vector, Filebeat, etc.) send logs to ClickHouse
   - Each collector handles its own schema mapping and transformations

2. **Log Querying** (Logchef's domain):
   - Users construct queries via the UI (simple syntax or SQL)
   - Logchef translates simple syntax to optimized ClickHouse SQL
   - Queries are executed against the appropriate ClickHouse source(s)
   - Results are processed, formatted, and displayed in the UI

## Data Storage

### SQLite Metadata Store

SQLite manages all system configuration and relationships:

- **Sources**: Connection details to remote ClickHouse databases
- **Users**: Account information and authentication data
- **Teams**: Organizational units with role-based access
- **Saved Queries**: Team-specific saved queries (supports both simple syntax and raw SQL)

### ClickHouse Log Storage

Logchef connects to multiple remote ClickHouse databases as sources:

- **Schema Flexibility**: Sources can:

  - Use the default OTEL schema as-is
  - Customize the built-in OpenTelemetry (OTEL) schema
  - Use custom schemas

- **Requirements**: Only a `timestamp` field (DateTime/DateTime64) is mandatory

- **Schema Agnostic**: Beyond `timestamp`, any column structure is supported

## Deployment Considerations

- **Single Binary**: Logchef runs as a lightweight single binary with minimal resource requirements
- **Stateless Operation**: Core application is stateless for horizontal scaling (only SQLite metadata is persistent)
- **Proxying**: Can be deployed behind reverse proxies like Nginx or Caddy

This architecture ensures:

- Fast log querying across multiple data sources
- Efficient metadata management
- Scalable log storage and retrieval
- Robust access controls
- Clean and responsive user experience
