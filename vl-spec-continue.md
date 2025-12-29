# VictoriaLogs Integration - Handoff Document

**Branch**: `vl`  
**Date**: December 28, 2025  
**Status**: Phase 3 Complete  
**Reference**: See `spec.md` for full specification

---

## Executive Summary

Phase 3 (LogsQL Generator) of the VictoriaLogs integration is complete. LogChefQL queries can now be translated to VictoriaLogs LogsQL syntax. Combined with Phase 1 (Backend Abstraction) and Phase 2 (VictoriaLogs HTTP Client), the core backend infrastructure for VictoriaLogs support is now complete.

---

## Phase 3 Completed Work

### New Files Created

| File | Purpose |
|------|---------|
| `internal/logchefql/logsql_generator.go` | LogsQL generator implementing operator translation from LogChefQL AST to VictoriaLogs LogsQL syntax |

### Modified Files

| File | Changes |
|------|---------|
| `internal/logchefql/types.go` | Added `LogsQL` field to `TranslateResult` struct |
| `internal/logchefql/logchefql.go` | Added `TranslateToLogsQL()` function and `BuildFullLogsQLQuery()` function with `LogsQLQueryBuildParams` struct |

### LogsQL Generator Implementation

The generator translates LogChefQL AST to VictoriaLogs LogsQL:

| LogChefQL | LogsQL |
|-----------|--------|
| `field="value"` | `field:=value` |
| `field!="value"` | `field:!=value` |
| `field~"pattern"` | `field:~"pattern"` |
| `field!~"pattern"` | `field:!~"pattern"` |
| `field>"value"` | `field:>value` |
| `field<"value"` | `field:<value` |
| `field>="value"` | `field:>=value` |
| `field<="value"` | `field:<=value` |
| `a AND b` | `a b` (space = AND) |
| `a OR b` | `(a or b)` |

### Key Functions

```go
// Translate LogChefQL to LogsQL
func TranslateToLogsQL(query string, schema *Schema) *TranslateResult

// Build complete VictoriaLogs query with time range
func BuildFullLogsQLQuery(params LogsQLQueryBuildParams) (string, error)

// Parameters for building full LogsQL query
type LogsQLQueryBuildParams struct {
    LogchefQL string
    Schema    *Schema
    StartTime time.Time
    EndTime   time.Time
    Limit     int
}
```

### Example Translation

```go
// Input LogChefQL
level="error" AND service~"api.*"

// Output LogsQL
level:=error service:~"api.*"

// Full query with time range
level:=error service:~"api.*" _time:[2025-01-01T00:00:00Z, 2025-01-02T00:00:00Z] | sort by (_time desc) | limit 100
```

### Validation

- `go build ./...` ✓
- `go test ./internal/logchefql/...` ✓ (all 80+ tests pass)
- `go test ./...` ✓ (all tests pass)

---

## Phase 2 Completed Work

### New Files Created

| File | Purpose |
|------|---------|
| `internal/backends/victorialogs/client.go` | HTTP client implementing `BackendClient` interface - handles Query, GetTableInfo, GetHistogramData, GetSurroundingLogs, GetFieldDistinctValues, Ping |
| `internal/backends/victorialogs/manager.go` | Connection manager implementing `BackendManager` interface - handles connection pooling, health checks, source lifecycle |
| `internal/backends/victorialogs/response.go` | Response parsing utilities - JSONL parsing, hits response, field names/values conversion |

### Modified Files

| File | Changes |
|------|---------|
| `internal/backends/registry.go` | Added `RegisterVictoriaLogsManager()` convenience method |
| `internal/app/app.go` | Added `VictoriaLogs` manager and `BackendRegistry` fields, initializes both managers at startup, uses registry for source loading |

### VictoriaLogs Client Implementation

The client implements all `BackendClient` interface methods:

1. **Query()** - Executes LogsQL queries via `/select/logsql/query` endpoint, parses JSONL response
2. **GetTableInfo()** - Discovers schema via `/select/logsql/field_names` endpoint (VictoriaLogs is schemaless)
3. **GetHistogramData()** - Retrieves histogram data via `/select/logsql/hits` endpoint with configurable step/grouping
4. **GetSurroundingLogs()** - Implements log context using time-based LogsQL queries with `_time:` filters
5. **GetFieldDistinctValues()** - Gets field values via `/select/logsql/field_values` endpoint
6. **GetAllFilterableFieldValues()** - Iterates discovered fields to get values for each
7. **Ping()** - Simple connectivity check with minimal query

### Manager Implementation

The manager mirrors ClickHouse manager functionality:
- Connection pooling per source
- Background health checks with configurable interval
- Automatic reconnection on failures
- Temporary client creation for validation

### Key Implementation Details

1. **Multi-tenancy**: Supports `AccountID` and `ProjectID` headers for VictoriaLogs multi-tenant deployments
2. **Time handling**: Uses RFC3339 format for timestamps, handles VictoriaLogs' `[start, end)` exclusive end semantics
3. **Response parsing**: Handles both JSON and JSONL response formats from different endpoints
4. **Schemaless support**: Dynamically infers column types from actual data

---

---

## Phase 1 Completed Work

### New Files Created

| File | Purpose |
|------|---------|
| `internal/backends/backend.go` | Core interfaces: `BackendClient`, `BackendManager`, and common types (`TimeRange`, `HistogramParams`, `LogContextParams`, `FieldValuesParams`, etc.) |
| `internal/backends/clickhouse_adapter.go` | Adapter wrapping `*clickhouse.Client` to implement `BackendClient` interface |
| `internal/backends/clickhouse_manager_adapter.go` | Adapter wrapping `*clickhouse.Manager` to implement `BackendManager` interface |
| `internal/backends/registry.go` | `BackendRegistry` - manages multiple backend managers, routes requests by source backend type |
| `internal/sqlite/migrations/000007_add_backend_type.up.sql` | Migration adding `backend_type` (default: 'clickhouse') and `victorialogs_connection` columns |
| `internal/sqlite/migrations/000007_add_backend_type.down.sql` | Rollback migration |

### Modified Files

| File | Changes |
|------|---------|
| `pkg/models/source.go` | Added `BackendType` enum, `VictoriaLogsConnectionInfo` struct, `BackendType` field to `Source`, helper methods (`GetEffectiveBackendType()`, `IsClickHouse()`, `IsVictoriaLogs()`) |
| `internal/sqlite/sources.go` | Updated `CreateSource` and `UpdateSource` to handle `backend_type` and `victorialogs_connection` columns |
| `internal/sqlite/utility.go` | Added `parseVictoriaLogsConnection()` and `serializeVictoriaLogsConnection()` helpers, updated `mapSourceRowToModel()` |
| `internal/sqlite/queries.sql` | Updated INSERT and UPDATE queries with new columns |
| `sqlc.yaml` | Added migration 000007 to schema list |
| `internal/sqlite/sqlc/*` | Regenerated via `sqlc generate` |

### Key Architecture Decisions

1. **Adapter Pattern**: Existing ClickHouse code is wrapped in adapters rather than modified, ensuring zero risk to existing functionality.

2. **Backward Compatibility**: The `BackendType` field defaults to `"clickhouse"` via `GetEffectiveBackendType()`. Existing sources work without any changes.

3. **Registry Pattern**: `BackendRegistry` is the central coordinator that:
   - Holds references to all backend managers (one per type)
   - Routes `GetClient()` calls to the correct manager based on source's backend type
   - Handles source lifecycle (add/remove) delegation

4. **Interface Design**: `BackendClient` interface mirrors the existing ClickHouse client methods:
   - `Query(ctx, query, timeout)` - Execute queries
   - `GetTableInfo(ctx, database, table)` - Schema metadata
   - `GetHistogramData(ctx, tableName, timestampField, params)` - Histogram generation
   - `GetSurroundingLogs(ctx, tableName, timestampField, params, timeout)` - Log context
   - `GetFieldDistinctValues(ctx, database, table, params)` - Field value discovery
   - `Ping(ctx, database, table)` - Health check
   - `Close()` / `Reconnect(ctx)` - Lifecycle

### Validation

- `go fmt ./...` ✓
- `go vet ./...` ✓  
- `go test ./...` ✓ (all tests pass)
- `go build ./...` ✓

---

## Remaining Phases

### Phase 4: API & Frontend Updates (spec.md section 6.4)

**Goal**: Full end-to-end integration

**Backend Tasks**:

1. **Update Source Handlers** (`internal/server/source_handlers.go`):
   - Accept `backend_type` in create source request
   - Accept `victorialogs_connection` for VL sources
   - Validate VL connection before creating source

2. **Update Query Handlers** (`internal/server/logs_handlers.go`):
   - Detect backend type from source
   - Route to appropriate query translator (SQL vs LogsQL)
   - Use `BackendRegistry.GetClient()` instead of direct ClickHouse access

3. **Update Core Functions** (`internal/core/logs.go`, `internal/core/source.go`):
   - Replace `*clickhouse.Manager` with `*backends.BackendRegistry`
   - Use interface methods for all backend operations

**Frontend Tasks** (delegate to frontend-ui-ux-engineer):

1. **New Components**:
   - `VictoriaLogsConnectionForm.vue` - URL, AccountID, ProjectID inputs
   - `BackendTypeSelector.vue` - Radio/dropdown to choose backend

2. **Modified Components**:
   - `SourceForm.vue` - Support multiple backend types
   - `QueryEditor.vue` - Show "LogsQL" vs "SQL" based on backend
   - `RawQueryInput.vue` - Different syntax highlighting for LogsQL

3. **API Types** (`frontend/src/api/sources.ts`):
   ```typescript
   interface Source {
     backend_type: 'clickhouse' | 'victorialogs';
     victorialogs_connection?: VictoriaLogsConnection;
   }
   
   interface VictoriaLogsConnection {
     url: string;
     account_id?: string;
     project_id?: string;
     stream_labels?: Record<string, string>;
   }
   ```

---

### Phase 5: Advanced Features (spec.md section 6.5)

**Goal**: Feature parity and optimization

1. **Log Context for VictoriaLogs**:
   - Implement using time-based queries (no native API)
   - Query: `_time:[target-1s, target] | sort by (_time desc) | limit N`
   - Query: `_time:[target, target+1s] | sort by (_time asc) | limit N`

2. **Alerts Integration**:
   - Update `internal/alerts/manager.go` to use `BackendRegistry`
   - Support LogsQL in alert rule definitions
   - Update alert evaluation for VL backend

3. **Live Tailing** (optional):
   - Implement using `/select/logsql/tail` endpoint
   - WebSocket/SSE integration for real-time streaming

---

## Code Pointers

### Using the New Backend Abstraction

**Getting a client for a source**:
```go
// In core functions, use the registry
client, err := registry.GetClient(sourceID)
if err != nil {
    return nil, err
}

// Execute query (works for both CH and VL)
result, err := client.Query(ctx, query, timeout)
```

**Checking backend type**:
```go
source, _ := db.GetSource(ctx, sourceID)
if source.IsVictoriaLogs() {
    // Use LogsQL translation
} else {
    // Use SQL translation (default)
}
```

**Registry initialization** (to be added in app.go):
```go
registry := backends.NewBackendRegistry(logger)

// Register ClickHouse (existing)
chManager := clickhouse.NewManager(...)
registry.RegisterClickHouseManager(chManager)

// Register VictoriaLogs (Phase 2)
vlManager := victorialogs.NewManager(...)
registry.RegisterManager(models.BackendVictoriaLogs, vlManager)
```

### Key Interfaces

```go
// BackendClient - implemented by ClickHouseAdapter and (future) VictoriaLogs client
type BackendClient interface {
    Query(ctx context.Context, query string, timeoutSeconds *int) (*models.QueryResult, error)
    GetTableInfo(ctx context.Context, database, table string) (*TableInfo, error)
    GetHistogramData(ctx context.Context, tableName, timestampField string, params HistogramParams) (*HistogramResult, error)
    GetSurroundingLogs(ctx context.Context, tableName, timestampField string, params LogContextParams, timeoutSeconds *int) (*LogContextResult, error)
    GetFieldDistinctValues(ctx context.Context, database, table string, params FieldValuesParams) (*FieldValuesResult, error)
    Ping(ctx context.Context, database, table string) error
    Close() error
    Reconnect(ctx context.Context) error
}

// BackendManager - manages client lifecycle
type BackendManager interface {
    GetClient(sourceID models.SourceID) (BackendClient, error)
    AddSource(ctx context.Context, source *models.Source) error
    RemoveSource(sourceID models.SourceID) error
    GetHealth(ctx context.Context, sourceID models.SourceID) models.SourceHealth
    CreateTemporaryClient(ctx context.Context, source *models.Source) (BackendClient, error)
    // ... health check methods
}
```

---

## Testing Notes

- All existing tests pass
- No golangci-lint in environment (skipped)
- Run `go test ./...` to verify changes don't break existing functionality
- Run `just check` when golangci-lint is available

---

## Dependencies

No new Go dependencies added. Phase 2 uses only standard library (`net/http`).

---

## Migration Notes

After deploying:
1. Run database migrations: `just migrate-up` or equivalent
2. Existing sources automatically get `backend_type = 'clickhouse'`
3. No user action required - full backward compatibility

---

## Estimated Remaining Effort

| Phase | Description | Estimate |
|-------|-------------|----------|
| ~~Phase 1~~ | ~~Backend Abstraction~~ | ~~2-3 weeks~~ ✓ |
| ~~Phase 2~~ | ~~VictoriaLogs Client~~ | ~~2 weeks~~ ✓ |
| ~~Phase 3~~ | ~~LogsQL Generator~~ | ~~1-2 weeks~~ ✓ |
| Phase 4 | API & Frontend | 2 weeks |
| Phase 5 | Advanced Features | 2 weeks |
| Testing | Integration testing, docs | 1-2 weeks |
| **Total Remaining** | | **4-6 weeks** |

---

## Questions for Next Phase

1. How should we handle VictoriaLogs' schemaless nature in the UI? (fields discovered dynamically)
2. Should we support VictoriaLogs' stream labels (`{app="nginx"}`) as a first-class concept?
3. What's the priority for live tailing feature?
4. Should raw LogsQL detection be added to `detect.go` (to pass through user-typed LogsQL), or should we always translate via LogChefQL?
