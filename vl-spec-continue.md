# VictoriaLogs Integration - Handoff Document

**Branch**: `vl`  
**Date**: December 28, 2025  
**Status**: Phase 1 Complete  
**Reference**: See `spec.md` for full specification

---

## Executive Summary

Phase 1 (Backend Abstraction) of the VictoriaLogs integration is complete. The foundation is in place for adding VictoriaLogs as a second backend alongside ClickHouse. All existing ClickHouse functionality continues to work unchanged.

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

### Phase 2: VictoriaLogs HTTP Client (spec.md section 6.2)

**Goal**: Implement VictoriaLogs backend client

**Files to Create**:

```
internal/backends/victorialogs/
├── client.go        # HTTP client implementing BackendClient
├── manager.go       # Connection management implementing BackendManager  
├── response.go      # JSONL/JSON response parsing
└── query_builder.go # LogsQL query construction helpers
```

**Key Implementation Tasks**:

1. **HTTP Client** (`client.go`):
   ```go
   type Client struct {
       httpClient *http.Client
       baseURL    string
       accountID  string  // Multi-tenant header
       projectID  string  // Multi-tenant header
       logger     *slog.Logger
   }
   ```
   
   Implement all `BackendClient` methods using VictoriaLogs HTTP API:
   - `Query()` → `POST /select/logsql/query`
   - `GetTableInfo()` → `GET /select/logsql/field_names` (VL is schemaless)
   - `GetHistogramData()` → `GET /select/logsql/hits`
   - `GetSurroundingLogs()` → Two queries with time filters (no native API)
   - `GetFieldDistinctValues()` → `GET /select/logsql/field_values`
   - `Ping()` → Simple query to verify connectivity

2. **Response Parsing** (`response.go`):
   - Parse JSONL streaming responses from VL query endpoint
   - Convert to `models.QueryResult` format
   - Extract stats from response headers

3. **Manager** (`manager.go`):
   - Implement `BackendManager` interface
   - Simple connection management (HTTP doesn't need pooling like native protocol)
   - Health checking via HTTP

4. **Register with BackendRegistry**:
   - Update `internal/app/app.go` to create VictoriaLogs manager
   - Register with `BackendRegistry.RegisterManager()`

**VictoriaLogs API Reference**:
- Query: `GET/POST /select/logsql/query?query=<logsql>&start=<time>&end=<time>&limit=<n>`
- Histogram: `GET /select/logsql/hits?query=<logsql>&start=<time>&end=<time>&step=<duration>`
- Field names: `GET /select/logsql/field_names?query=<logsql>&start=<time>&end=<time>`
- Field values: `GET /select/logsql/field_values?query=<logsql>&field=<name>&start=<time>&end=<time>`

---

### Phase 3: LogsQL Generator (spec.md section 6.3)

**Goal**: Translate LogChefQL to LogsQL

**Files to Create**:

```
internal/logchefql/
└── logsql_generator.go  # NEW - generates LogsQL from AST
```

**Key Implementation Tasks**:

1. **Create LogsQL Generator**:
   ```go
   type LogsQLGenerator struct {
       schema *Schema
   }
   
   func (g *LogsQLGenerator) Generate(node ASTNode) string
   ```

2. **Operator Mapping** (from spec.md):

   | LogChefQL | LogsQL |
   |-----------|--------|
   | `field="value"` | `field:=value` |
   | `field!="value"` | `field:!=value` |
   | `field~"pattern"` | `field:~"pattern"` |
   | `field!~"pattern"` | `field:!~"pattern"` |
   | `field>"value"` | `field:>value` |
   | `a AND b` | `a b` (space = AND) |
   | `a OR b` | `(a or b)` |

3. **Add Translation Function**:
   ```go
   func TranslateToLogsQL(query string, schema *Schema) *TranslateResult
   func BuildFullLogsQLQuery(params QueryBuildParams) (string, error)
   ```

4. **Support Raw LogsQL Mode**:
   - Update `internal/logchefql/detect.go` to detect LogsQL
   - Pass through raw LogsQL queries for VictoriaLogs sources

---

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

No new Go dependencies added in Phase 1. Phase 2 will likely only need standard library (`net/http`).

---

## Migration Notes

After deploying Phase 1:
1. Run database migrations: `just migrate-up` or equivalent
2. Existing sources automatically get `backend_type = 'clickhouse'`
3. No user action required - full backward compatibility

---

## Estimated Remaining Effort

| Phase | Description | Estimate |
|-------|-------------|----------|
| Phase 2 | VictoriaLogs Client | 2 weeks |
| Phase 3 | LogsQL Generator | 1-2 weeks |
| Phase 4 | API & Frontend | 2 weeks |
| Phase 5 | Advanced Features | 2 weeks |
| Testing | Integration testing, docs | 1-2 weeks |
| **Total Remaining** | | **8-10 weeks** |

---

## Questions for Next Agent

1. Should VictoriaLogs manager maintain persistent HTTP connections or create per-request?
2. How should we handle VictoriaLogs' schemaless nature in the UI? (fields discovered dynamically)
3. Should we support VictoriaLogs' stream labels (`{app="nginx"}`) as a first-class concept?
4. What's the priority for live tailing feature?
