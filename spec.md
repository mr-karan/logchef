# VictoriaLogs Integration Specification

## Executive Summary

This document outlines the technical feasibility and implementation plan for adding VictoriaLogs as a pluggable logging backend source in LogChef, alongside the existing ClickHouse support. The integration will maintain a consistent user experience while leveraging the unique capabilities of each backend.

**Status**: Feasibility Analysis Complete  
**Branch**: `vl`  
**Related**: [GitHub Discussion #28](https://github.com/mr-karan/logchef/discussions/28)

---

## Table of Contents

1. [Current Architecture Analysis](#current-architecture-analysis)
2. [VictoriaLogs Overview](#victorialogs-overview)
3. [Feasibility Assessment](#feasibility-assessment)
4. [Architecture Design](#architecture-design)
5. [Query Language Strategy](#query-language-strategy)
6. [Implementation Plan](#implementation-plan)
7. [API Mapping](#api-mapping)
8. [Migration & Compatibility](#migration--compatibility)
9. [Risks & Mitigations](#risks--mitigations)
10. [Timeline Estimate](#timeline-estimate)

---

## Current Architecture Analysis

### Overview

LogChef currently supports **only ClickHouse** as the logging backend. The architecture is well-structured but tightly coupled to ClickHouse-specific concepts.

### Key Components

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Frontend (Vue)                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │
│  │ LogChefQL   │  │  Raw SQL    │  │  AI Query   │                  │
│  │   Editor    │  │   Editor    │  │  Generator  │                  │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘                  │
└─────────┼────────────────┼────────────────┼─────────────────────────┘
          │                │                │
          ▼                ▼                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        API Layer (Fiber)                            │
│  /teams/{teamID}/sources/{sourceID}/logs/query                      │
│  /teams/{teamID}/sources/{sourceID}/logs/histogram                  │
│  /teams/{teamID}/sources/{sourceID}/logs/context                    │
└─────────────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Core Business Logic                            │
│  internal/core/logs.go    - Query orchestration                     │
│  internal/core/source.go  - Source management                       │
└─────────────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    LogChefQL Translation                            │
│  internal/logchefql/logchefql.go  - Parser & translator             │
│  internal/logchefql/sql_generator.go - ClickHouse SQL generation    │
└─────────────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────────┐
│                   ClickHouse Layer (Current)                        │
│  internal/clickhouse/client.go   - Connection & query execution     │
│  internal/clickhouse/manager.go  - Connection pool & health         │
│  internal/clickhouse/logs.go     - Histogram, context queries       │
│  internal/clickhouse/query_builder.go - SQL validation              │
└─────────────────────────────────────────────────────────────────────┘
```

### Current Data Models

**Source Model** (`pkg/models/source.go`):
```go
type Source struct {
    ID                SourceID       `json:"id"`
    Name              string         `json:"name"`
    Connection        ConnectionInfo `json:"connection"`  // ClickHouse-specific
    MetaTSField       string         `json:"_meta_ts_field"`
    MetaSeverityField string         `json:"_meta_severity_field"`
    // ClickHouse-specific fields
    Engine       string   `json:"engine,omitempty"`
    EngineParams []string `json:"engine_params,omitempty"`
    SortKeys     []string `json:"sort_keys,omitempty"`
}

type ConnectionInfo struct {
    Host      string `json:"host"`
    Username  string `json:"username"`
    Password  string `json:"password"`
    Database  string `json:"database"`
    TableName string `json:"table_name"`
}
```

### Extension Points Identified

1. **Query Hooks Interface** - Already exists for logging/metrics
2. **Manager Pattern** - Connection pooling is abstracted
3. **Core Functions** - Take dependencies as parameters (injectable)
4. **LogChefQL AST** - Parser/AST is separate from SQL generation

### Current Limitations

1. `ConnectionInfo` is ClickHouse-specific
2. `sql_generator.go` only outputs ClickHouse SQL
3. Schema models have hardcoded ClickHouse CREATE TABLE statements
4. Query builder uses ClickHouse SQL parser
5. Frontend expects ClickHouse-specific response fields

---

## VictoriaLogs Overview

### What is VictoriaLogs?

VictoriaLogs is a fast, open-source, schemaless log database from VictoriaMetrics. It's designed for high-volume log ingestion and querying with a simple HTTP API.

### Key Differences from ClickHouse

| Aspect | ClickHouse | VictoriaLogs |
|--------|------------|--------------|
| Protocol | Native TCP (port 9000) | HTTP REST (port 9428) |
| Query Language | SQL | LogsQL (pipe-based) |
| Schema | Predefined tables/columns | Schemaless (JSON fields) |
| Data Model | Relational tables | Log streams with labels |
| Time Field | Configurable column | Built-in `_time` |
| Message Field | Configurable column | Built-in `_msg` |
| Authentication | Username/Password | HTTP headers (AccountID/ProjectID) |

### VictoriaLogs HTTP API

Key endpoints for LogChef integration:

```
# Query logs (equivalent to /logs/query)
GET/POST /select/logsql/query?query=<logsql>&start=<time>&end=<time>&limit=<n>

# Histogram data (equivalent to /logs/histogram)  
GET/POST /select/logsql/hits?query=<logsql>&start=<time>&end=<time>&step=<duration>

# Field names (equivalent to schema)
GET /select/logsql/field_names?query=<logsql>&start=<time>&end=<time>

# Field values (equivalent to field values)
GET /select/logsql/field_values?query=<logsql>&field=<name>&start=<time>&end=<time>

# Streams (log stream metadata)
GET /select/logsql/streams?query=<logsql>&start=<time>&end=<time>

# Live tailing
GET /select/logsql/tail?query=<logsql>

# Facets (field value distribution)
GET /select/logsql/facets?query=<logsql>&start=<time>&end=<time>
```

### LogsQL Query Language

LogsQL follows a **filter-first, pipe-based** syntax:

```
<filters> | <pipe1> | <pipe2> | ... | <pipeN>
```

#### Filter Syntax

| LogsQL | Meaning |
|--------|---------|
| `error` | Word filter (contains "error") |
| `"connection refused"` | Phrase filter |
| `level:error` | Exact field match |
| `level:=error` | Exact match (explicit) |
| `level:!=error` | Not equal |
| `level:~"err.*"` | Regex match |
| `level:!~"err.*"` | Regex not match |
| `_time:5m` | Last 5 minutes |
| `_time:[2024-01-01, 2024-01-02]` | Time range |
| `{app="nginx"}` | Stream filter (labels) |
| `*` | Match all |

#### Pipe Operators

| Pipe | Purpose |
|------|---------|
| `\| fields <f1>, <f2>` | Select specific fields |
| `\| limit <n>` | Limit results |
| `\| sort by (<field>)` | Sort results |
| `\| stats by (<field>) count()` | Aggregation |
| `\| filter <condition>` | Post-filter |
| `\| uniq by (<field>)` | Unique values |
| `\| top <n> (<field>)` | Top N values |

### SQL to LogsQL Conversion

VictoriaMetrics provides a [SQL to LogsQL tutorial](https://docs.victoriametrics.com/victorialogs/sql-to-logsql/) with conversion rules:

| SQL | LogsQL |
|-----|--------|
| `SELECT * FROM table WHERE field='value'` | `field:=value` |
| `SELECT * FROM table WHERE field LIKE '%error%'` | `error` or `field:~"error"` |
| `SELECT field1, field2 FROM ...` | `* \| fields field1, field2` |
| `SELECT count(*) FROM ... GROUP BY level` | `* \| stats by (level) count()` |
| `ORDER BY timestamp DESC LIMIT 10` | `* \| sort by (_time desc) limit 10` |
| `WHERE field IN ('a', 'b', 'c')` | `field:in(a, b, c)` |

---

## Feasibility Assessment

### Technical Feasibility: **HIGH**

The integration is technically feasible with the following key findings:

#### Positive Factors

1. **Clean API Separation**: LogChef's core functions accept dependencies as parameters, enabling backend swapping
2. **LogChefQL AST Exists**: Parser creates an AST that can be translated to different backends
3. **HTTP API Simplicity**: VictoriaLogs uses simple HTTP endpoints (vs ClickHouse native protocol)
4. **Feature Parity**: VictoriaLogs provides equivalent functionality for all LogChef features

#### Challenges

1. **Query Language Difference**: LogsQL is fundamentally different from SQL
2. **Schema Discovery**: VictoriaLogs is schemaless - columns are discovered dynamically
3. **Raw Query Mode**: Need to support raw LogsQL like we support raw ClickHouse SQL
4. **Response Format**: Different JSON structure requires adaptation

### Feature Mapping

| LogChef Feature | ClickHouse Implementation | VictoriaLogs Equivalent |
|-----------------|---------------------------|-------------------------|
| Log Query | `SELECT * FROM table WHERE...` | `/select/logsql/query` |
| Histogram | Custom SQL with time buckets | `/select/logsql/hits` |
| Log Context | Before/after queries | Not directly available* |
| Field Values | `SELECT DISTINCT...` | `/select/logsql/field_values` |
| Schema | `system.columns` | `/select/logsql/field_names` |
| AI SQL Generation | Works with SQL | Needs LogsQL prompts |
| Alerts | SQL-based queries | LogsQL queries |

*Log context would need custom implementation using time filters

---

## Architecture Design

### Proposed Backend Abstraction

```go
// pkg/backends/backend.go

// BackendType represents the type of log storage backend
type BackendType string

const (
    BackendTypeClickHouse   BackendType = "clickhouse"
    BackendTypeVictoriaLogs BackendType = "victorialogs"
)

// BackendClient is the interface for log storage backends
type BackendClient interface {
    // Query execution
    Query(ctx context.Context, query string, timeout *int) (*models.QueryResult, error)
    
    // Schema/metadata
    GetFieldNames(ctx context.Context, timeRange TimeRange) ([]models.ColumnInfo, error)
    GetFieldValues(ctx context.Context, field string, params FieldValuesParams) (*FieldValuesResult, error)
    
    // Histogram
    GetHistogram(ctx context.Context, query string, params HistogramParams) (*HistogramResult, error)
    
    // Health
    Ping(ctx context.Context) error
    Close() error
}

// BackendManager manages connections to backend clients
type BackendManager interface {
    GetClient(sourceID models.SourceID) (BackendClient, error)
    AddSource(ctx context.Context, source *models.Source) error
    RemoveSource(sourceID models.SourceID) error
    GetHealth(ctx context.Context, sourceID models.SourceID) models.SourceHealth
}
```

### Extended Source Model

```go
// pkg/models/source.go

type BackendType string

const (
    BackendClickHouse   BackendType = "clickhouse"
    BackendVictoriaLogs BackendType = "victorialogs"
)

type Source struct {
    ID          SourceID    `json:"id"`
    Name        string      `json:"name"`
    BackendType BackendType `json:"backend_type"`  // NEW
    
    // Union of connection configs (only one populated based on BackendType)
    ClickHouseConnection   *ClickHouseConnectionInfo   `json:"clickhouse_connection,omitempty"`
    VictoriaLogsConnection *VictoriaLogsConnectionInfo `json:"victorialogs_connection,omitempty"`
    
    // Common metadata
    MetaTSField       string `json:"_meta_ts_field"`       // "_time" for VL, configurable for CH
    MetaSeverityField string `json:"_meta_severity_field"` // optional
    
    // Runtime fields
    IsConnected bool         `json:"is_connected"`
    Columns     []ColumnInfo `json:"columns,omitempty"`
}

type ClickHouseConnectionInfo struct {
    Host      string `json:"host"`
    Username  string `json:"username"`
    Password  string `json:"password"`
    Database  string `json:"database"`
    TableName string `json:"table_name"`
}

type VictoriaLogsConnectionInfo struct {
    URL       string `json:"url"`        // e.g., "http://localhost:9428"
    AccountID string `json:"account_id"` // Multi-tenant support
    ProjectID string `json:"project_id"` // Multi-tenant support
    // Stream labels to filter (optional)
    StreamLabels map[string]string `json:"stream_labels,omitempty"`
}
```

### Directory Structure

```
internal/
├── backends/
│   ├── backend.go           # Interface definitions
│   ├── factory.go           # Backend factory
│   ├── clickhouse/          # Existing ClickHouse code (refactored)
│   │   ├── client.go
│   │   ├── manager.go
│   │   ├── query_builder.go
│   │   └── ...
│   └── victorialogs/        # NEW
│       ├── client.go        # HTTP client for VL
│       ├── manager.go       # Connection management
│       ├── query_builder.go # LogsQL construction
│       └── response.go      # Response parsing
├── logchefql/
│   ├── logchefql.go         # Parser (unchanged)
│   ├── types.go             # AST types (unchanged)
│   ├── sql_generator.go     # ClickHouse SQL (existing)
│   └── logsql_generator.go  # VictoriaLogs LogsQL (NEW)
└── core/
    ├── logs.go              # Updated to use BackendClient interface
    └── source.go            # Updated for multi-backend sources
```

---

## Query Language Strategy

### Three Query Modes

LogChef will support three query modes per backend:

| Mode | ClickHouse | VictoriaLogs |
|------|------------|--------------|
| LogChefQL | Translated to SQL | Translated to LogsQL |
| Raw Query | Raw ClickHouse SQL | Raw LogsQL |
| AI Generated | SQL from natural language | LogsQL from natural language |

### LogChefQL Translation

The existing LogChefQL parser produces an AST that can be translated to either backend:

```go
// Existing: LogChefQL -> AST -> ClickHouse SQL
result := logchefql.Translate(query, schema)  // Returns SQL

// New: LogChefQL -> AST -> LogsQL
result := logchefql.TranslateToLogsQL(query, schema)  // Returns LogsQL
```

#### LogsQL Generator Implementation

```go
// internal/logchefql/logsql_generator.go

type LogsQLGenerator struct {
    schema *Schema
}

func (g *LogsQLGenerator) Generate(node ASTNode) string {
    // Convert AST to LogsQL syntax
}

func (g *LogsQLGenerator) visitExpression(node *ExpressionNode) string {
    key := getFieldName(node.Key)
    value := g.formatValue(node.Value)
    
    switch node.Operator {
    case OpEquals:
        return fmt.Sprintf("%s:=%s", key, value)
    case OpNotEquals:
        return fmt.Sprintf("%s:!=%s", key, value)
    case OpRegex:
        return fmt.Sprintf("%s:~%s", key, value)
    case OpNotRegex:
        return fmt.Sprintf("%s:!~%s", key, value)
    case OpGT:
        return fmt.Sprintf("%s:>%s", key, value)
    case OpLT:
        return fmt.Sprintf("%s:<%s", key, value)
    case OpGTE:
        return fmt.Sprintf("%s:>=%s", key, value)
    case OpLTE:
        return fmt.Sprintf("%s:<=%s", key, value)
    }
    return ""
}

func (g *LogsQLGenerator) visitLogical(node *LogicalNode) string {
    // LogsQL uses space for AND, "or" keyword for OR
    var conditions []string
    for _, child := range node.Children {
        conditions = append(conditions, g.visit(child))
    }
    
    if node.Operator == BoolOr {
        return "(" + strings.Join(conditions, " or ") + ")"
    }
    return strings.Join(conditions, " ")  // AND is implicit (space)
}
```

### Operator Mapping

| LogChefQL | ClickHouse SQL | LogsQL |
|-----------|---------------|--------|
| `field="value"` | `field = 'value'` | `field:=value` |
| `field!="value"` | `field != 'value'` | `field:!=value` |
| `field~"pattern"` | `positionCaseInsensitive(field, 'pattern') > 0` | `field:~"pattern"` |
| `field!~"pattern"` | `positionCaseInsensitive(field, 'pattern') = 0` | `field:!~"pattern"` |
| `field>"value"` | `field > 'value'` | `field:>value` |
| `a AND b` | `(a) AND (b)` | `a b` (space = AND) |
| `a OR b` | `(a) OR (b)` | `(a or b)` |
| Nested field `log.level` | `JSONExtractString(log, 'level')` | `log.level:value` |

### Full Query Building

```go
// For VictoriaLogs, build complete LogsQL query with time range
func BuildVictoriaLogsQuery(params QueryBuildParams) (string, error) {
    result := logchefql.TranslateToLogsQL(params.LogchefQL, params.Schema)
    if !result.Valid {
        return "", result.Error
    }
    
    var query strings.Builder
    
    // Time filter (VictoriaLogs uses _time: syntax)
    query.WriteString(fmt.Sprintf("_time:[%s, %s]", params.StartTime, params.EndTime))
    
    // Add LogchefQL conditions
    if result.LogsQL != "" {
        query.WriteString(" ")
        query.WriteString(result.LogsQL)
    }
    
    // Add field selection if specified
    if result.SelectClause != "" {
        query.WriteString(" | fields ")
        query.WriteString(result.SelectClause)
    }
    
    // Add sort and limit
    query.WriteString(" | sort by (_time desc)")
    if params.Limit > 0 {
        query.WriteString(fmt.Sprintf(" | limit %d", params.Limit))
    }
    
    return query.String(), nil
}
```

---

## Implementation Plan

### Phase 1: Backend Abstraction (Foundation)

**Goal**: Create interfaces without breaking existing functionality

1. **Define Backend Interfaces** (`internal/backends/backend.go`)
   - `BackendClient` interface
   - `BackendManager` interface
   - Common types (TimeRange, HistogramParams, etc.)

2. **Refactor ClickHouse to Implement Interface**
   - Move `internal/clickhouse/` to `internal/backends/clickhouse/`
   - Implement `BackendClient` interface
   - Implement `BackendManager` interface
   - Update imports throughout codebase

3. **Update Core Layer**
   - Modify `internal/core/logs.go` to use `BackendClient`
   - Modify `internal/core/source.go` for multi-backend sources
   - Keep backward compatibility with existing sources

4. **Database Migration**
   - Add `backend_type` column to sources table (default: "clickhouse")
   - Add `victorialogs_connection` JSON column

### Phase 2: VictoriaLogs Client

**Goal**: Implement VictoriaLogs backend client

1. **HTTP Client** (`internal/backends/victorialogs/client.go`)
   ```go
   type Client struct {
       httpClient *http.Client
       baseURL    string
       accountID  string
       projectID  string
       logger     *slog.Logger
   }
   
   func (c *Client) Query(ctx context.Context, logsql string, timeout *int) (*models.QueryResult, error)
   func (c *Client) GetFieldNames(ctx context.Context, tr TimeRange) ([]models.ColumnInfo, error)
   func (c *Client) GetHistogram(ctx context.Context, query string, params HistogramParams) (*HistogramResult, error)
   ```

2. **Response Parsing** (`internal/backends/victorialogs/response.go`)
   - Parse JSONL streaming response
   - Convert to `models.QueryResult`
   - Handle stats extraction

3. **Manager** (`internal/backends/victorialogs/manager.go`)
   - Connection pool management
   - Health checking via HTTP
   - Reconnection logic

### Phase 3: LogsQL Generator

**Goal**: Translate LogChefQL to LogsQL

1. **Create LogsQL Generator** (`internal/logchefql/logsql_generator.go`)
   - Implement `Generate(node ASTNode) string`
   - Handle all operators and expressions
   - Support nested fields

2. **Add Translation Function**
   ```go
   func TranslateToLogsQL(query string, schema *Schema) *TranslateResult
   func BuildFullLogsQLQuery(params QueryBuildParams) (string, error)
   ```

3. **Update API Handlers**
   - Detect backend type from source
   - Route to appropriate translator

### Phase 4: API & Frontend Updates

**Goal**: Full end-to-end integration

1. **API Updates**
   - Update source creation endpoint for VictoriaLogs
   - Add backend type to source responses
   - Update query endpoints to handle both backends

2. **Frontend Updates**
   - Add VictoriaLogs as source type option
   - Update connection form for VictoriaLogs config
   - Show "LogsQL" label for raw query mode on VL sources
   - Update AI prompts for LogsQL generation

3. **Documentation**
   - Update user docs for VictoriaLogs sources
   - Add LogsQL examples

### Phase 5: Advanced Features

**Goal**: Feature parity and optimization

1. **Log Context for VictoriaLogs**
   - Implement using time-based queries
   - Query before/after target timestamp

2. **Alerts Integration**
   - Support LogsQL in alert rules
   - Update alert evaluation for VL backend

3. **Live Tailing**
   - Implement using `/select/logsql/tail` endpoint
   - WebSocket/SSE integration

---

## API Mapping

### Query Execution

**ClickHouse**:
```go
client.QueryWithTimeout(ctx, sqlQuery, timeout)
```

**VictoriaLogs**:
```http
POST /select/logsql/query
Content-Type: application/x-www-form-urlencoded

query=_time:5m error | limit 100
```

### Histogram

**ClickHouse**: Custom SQL with `toStartOfInterval()`

**VictoriaLogs**:
```http
GET /select/logsql/hits?query=error&start=1h&step=5m
```

Response:
```json
{
  "hits": [{
    "fields": {},
    "timestamps": ["2024-01-01T00:00:00Z", ...],
    "values": [410339, ...],
    "total": 1760176
  }]
}
```

### Field Values

**ClickHouse**: `SELECT DISTINCT field FROM table WHERE...`

**VictoriaLogs**:
```http
GET /select/logsql/field_values?query=*&field=level&start=1h&limit=10
```

### Schema Discovery

**ClickHouse**: `SELECT name, type FROM system.columns WHERE...`

**VictoriaLogs**:
```http
GET /select/logsql/field_names?query=*&start=1h
```

Note: VictoriaLogs returns field names but not types (schemaless).

---

## Migration & Compatibility

### Backward Compatibility

1. **Existing Sources**: All existing ClickHouse sources continue working unchanged
2. **API Compatibility**: No breaking changes to existing API endpoints
3. **Database Migration**: Additive only (new columns, not modifications)

### Source Migration

Sources cannot be migrated between backends (data format is different). Users must:
1. Create a new VictoriaLogs source
2. Configure log ingestion to VictoriaLogs
3. Query from the new source

### Configuration

New configuration section for VictoriaLogs defaults:

```toml
[victorialogs]
default_timeout = 60  # Default query timeout in seconds
```

---

## Risks & Mitigations

### Risk 1: Query Language Complexity
**Risk**: LogsQL is fundamentally different from SQL, making translation error-prone.
**Mitigation**: 
- Comprehensive test suite for LogChefQL -> LogsQL translation
- Use VictoriaMetrics' [SQL to LogsQL playground](https://play-sql.victoriametrics.com/) as reference

### Risk 2: Feature Gaps
**Risk**: Some ClickHouse features may not have VictoriaLogs equivalents.
**Mitigation**:
- Document unsupported features clearly
- Provide graceful degradation where possible

### Risk 3: Performance Differences
**Risk**: Query performance characteristics differ between backends.
**Mitigation**:
- Independent timeout configurations per backend
- Performance monitoring and documentation

### Risk 4: Schema Differences
**Risk**: VictoriaLogs is schemaless vs ClickHouse's strict schema.
**Mitigation**:
- Dynamic column discovery for VL
- Clear UI indicators for schemaless sources

### Risk 5: Response Format Differences
**Risk**: Different JSON structures between backends.
**Mitigation**:
- Normalize responses to common format in backend clients
- Frontend receives consistent format regardless of backend

---

## Timeline Estimate

| Phase | Description | Estimate |
|-------|-------------|----------|
| Phase 1 | Backend Abstraction | 2-3 weeks |
| Phase 2 | VictoriaLogs Client | 2 weeks |
| Phase 3 | LogsQL Generator | 1-2 weeks |
| Phase 4 | API & Frontend | 2 weeks |
| Phase 5 | Advanced Features | 2 weeks |
| Testing & Polish | Integration testing, docs | 1-2 weeks |
| **Total** | | **10-13 weeks** |

### Milestones

1. **M1 (Phase 1 complete)**: Existing ClickHouse functionality works through new interfaces
2. **M2 (Phase 2+3 complete)**: Basic VictoriaLogs queries work
3. **M3 (Phase 4 complete)**: Full UI support for VictoriaLogs sources
4. **M4 (Phase 5 complete)**: Feature parity with alerts and live tailing

---

## Appendix A: LogsQL Examples

### Basic Queries

```logsql
# All logs in last 5 minutes
_time:5m

# Error logs
error

# Specific field value
level:=error

# Multiple conditions (AND)
level:=error service:=nginx

# OR conditions
level:=error or level:=warn

# Regex match
message:~"connection.*refused"

# Time range
_time:[2024-01-01, 2024-01-02]
```

### With Pipes

```logsql
# Select specific fields
_time:5m level:=error | fields _time, _msg, service

# Aggregation
_time:1h | stats by (level) count()

# Top errors by service
_time:1h level:=error | stats by (service) count() as errors | sort by (errors desc) | limit 10

# Unique values
_time:1h | uniq by (user_id)
```

### Stream Filters

```logsql
# Filter by stream labels
{app="nginx", env="prod"} error

# Combine with field filters
{app="nginx"} status:>=500
```

---

## Appendix B: Database Schema Changes

```sql
-- Migration: Add VictoriaLogs support

-- Add backend_type column with default for existing sources
ALTER TABLE sources ADD COLUMN backend_type TEXT NOT NULL DEFAULT 'clickhouse';

-- Add VictoriaLogs connection info (stored as JSON)
ALTER TABLE sources ADD COLUMN victorialogs_connection TEXT;

-- Create index for backend type queries
CREATE INDEX idx_sources_backend_type ON sources(backend_type);
```

---

## Appendix C: Frontend Changes Summary

### New Components
- `VictoriaLogsConnectionForm.vue` - Connection configuration for VL
- `BackendTypeSelector.vue` - Choose between ClickHouse/VictoriaLogs

### Modified Components
- `SourceForm.vue` - Support multiple backend types
- `QueryEditor.vue` - Show "LogsQL" vs "SQL" based on backend
- `RawQueryInput.vue` - Syntax highlighting for LogsQL

### API Types
```typescript
interface Source {
  id: number;
  name: string;
  backend_type: 'clickhouse' | 'victorialogs';  // NEW
  clickhouse_connection?: ClickHouseConnection;
  victorialogs_connection?: VictoriaLogsConnection;
  // ... rest unchanged
}

interface VictoriaLogsConnection {
  url: string;
  account_id?: string;
  project_id?: string;
  stream_labels?: Record<string, string>;
}
```

---

## Conclusion

Adding VictoriaLogs as a backend source to LogChef is **technically feasible** and aligns with the platform's vision of being a versatile log analytics tool. The existing architecture provides good extension points, and VictoriaLogs' HTTP API simplifies integration compared to native protocols.

Key success factors:
1. Clean backend abstraction that doesn't break existing functionality
2. Comprehensive LogChefQL -> LogsQL translation
3. Clear documentation of feature differences
4. Thorough testing of both backends

The estimated timeline of 10-13 weeks allows for careful implementation with proper testing and documentation.
