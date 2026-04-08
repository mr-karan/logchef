# RFC 0001: Datasource Architecture for ClickHouse and VictoriaLogs

Status: Draft
Date: 2026-04-08
Authors: Codex

## Summary

LogChef should support ClickHouse and VictoriaLogs as distinct datasource backends behind a shared LogChef control plane.

This RFC proposes:

- turning `Source` into a generic datasource model
- introducing a provider interface and registry in the backend
- keeping native query languages per datasource
- preserving shared LogChef features such as RBAC, saved queries, alerting, and log exploration
- shipping VictoriaLogs support in phases without regressing existing ClickHouse behavior

The key decision is to stop treating SQL as LogChef's universal internal query representation. SQL remains native only for ClickHouse. VictoriaLogs will use native LogsQL.

## Motivation

LogChef currently assumes that every source is a ClickHouse table:

- source storage is ClickHouse-shaped in `pkg/models/source.go`
- SQLite stores `host`, `database`, and `table_name` directly in `sources`
- source creation assumes table existence or table auto-creation
- log query execution assumes `raw_sql`
- alerts assume SQL queries executed via ClickHouse
- the frontend generates ClickHouse SQL as the shared execution format

This is visible in:

- `pkg/models/source.go`
- `internal/core/source.go`
- `internal/core/logs.go`
- `internal/alerts/manager.go`
- `frontend/src/services/QueryService.ts`
- `frontend/src/views/sources/AddSource.vue`

That shape works for ClickHouse, but it is the wrong abstraction for VictoriaLogs:

- VictoriaLogs is schema-less
- there is no `database.table` identity
- query execution is HTTP + LogsQL, not SQL over a native driver
- field discovery is dynamic and time-range-dependent
- alert evaluation is a better fit for `stats_query` than for raw log retrieval

If LogChef is going to behave like Grafana with multiple backends, datasource type must become a first-class concern across storage, API, backend execution, and UI.

## Goals

- Support `clickhouse` and `victorialogs` as first-class datasource types.
- Keep LogChef as the control plane for:
  - team-scoped access
  - saved queries
  - alerting
  - query UI
  - field sidebar
  - exports
- Preserve current ClickHouse behavior during the transition.
- Ship a high-value VictoriaLogs MVP without pretending it is SQL-compatible.
- Make future backends possible without repeating this refactor.

## Non-goals

- Full backend-neutral LogchefQL in the first VictoriaLogs release.
- Auto-provisioning VictoriaLogs storage objects.
- Full feature parity for AI query generation in v1.
- Matching ClickHouse table stats UX for VictoriaLogs.
- Replacing VictoriaLogs native alerting or `vmalert`; LogChef alerting remains its own workflow.

## Current State

### Tight couplings that must be broken

1. `Source` is structurally ClickHouse-only.
2. `sources` rows are identified by `UNIQUE(database, table_name)`.
3. Query execution expects `raw_sql`.
4. Histogram generation expects SQL and ClickHouse time bucketing.
5. Schema and field handling assume typed static columns.
6. Alerts execute SQL through `clickhouse.Manager`.
7. The frontend assumes:
   - ClickHouse source setup
   - `LogchefQL` plus `SQL` tabs
   - SQL as the execution target for most flows

### VictoriaLogs capability mapping

VictoriaLogs already exposes the primitives needed for a strong MVP:

- `/select/logsql/query` for log retrieval
- `/select/logsql/hits` for count-over-time histograms and grouped histogram buckets
- `/select/logsql/field_names` for discovered fields
- `/select/logsql/field_values` for discovered values
- `/select/logsql/stats_query` for instant numeric evaluation
- `/select/logsql/stats_query_range` for range aggregations
This means LogChef does not need to emulate ClickHouse behavior for VictoriaLogs. It needs a provider layer.

## Decision

LogChef will adopt a datasource architecture with:

- a generic datasource model persisted in SQLite
- provider-specific connection/config JSON
- provider-specific query execution
- provider-specific discovery/introspection
- a shared application-level API and UI

Native query languages will remain provider-specific:

- ClickHouse:
  - `logchefql`
  - `clickhouse-sql`
- VictoriaLogs:
  - `logsql`

`LogchefQL` remains ClickHouse-only until a later RFC expands it into a backend-neutral subset.

## Proposed Architecture

### 1. Source Model

#### Final model

The persisted source model becomes:

- `id`
- `name`
- `_meta_is_auto_created`
- `source_type`
- `_meta_ts_field`
- `_meta_severity_field`
- `connection_config`
- `identity_key`
- `description`
- `ttl_days`
- `managed`
- `secret_ref`
- timestamps

Provider-specific connection details move into `connection_config`.

`_meta_is_auto_created` stays because it already controls ClickHouse table provisioning behavior. For VictoriaLogs it is always stored as `false` and ignored by provider logic.

Examples:

```json
{
  "source_type": "clickhouse",
  "connection_config": {
    "host": "clickhouse.internal:9000",
    "database": "default",
    "table_name": "logs",
    "auth": {
      "mode": "basic",
      "username": "default",
      "password": "secret"
    }
  }
}
```

```json
{
  "source_type": "victorialogs",
  "connection_config": {
    "base_url": "https://logs.example.com",
    "auth": {
      "mode": "bearer",
      "token": "secret"
    },
    "tenant": {
      "account_id": "12",
      "project_id": "34"
    },
    "scope": {
      "query": "{app=\"payments\"} kubernetes.namespace:=prod"
    }
  }
}
```

#### Why JSON instead of more columns

This repo already has a strong provider mismatch:

- ClickHouse needs `database` and `table_name`
- VictoriaLogs needs `base_url`, auth, tenant headers, and immutable scope

Adding top-level columns per backend will permanently leak provider specifics into the shared schema. `connection_config` gives a stable shape while allowing provider evolution.

#### Identity

The current `UNIQUE(database, table_name)` constraint is not reusable for non-ClickHouse backends.

Introduce `identity_key TEXT` and a unique index on it.

Each provider computes a canonical identity key:

- ClickHouse:
  - `clickhouse:<normalized-host>/<database>/<table>`
- VictoriaLogs:
  - `victorialogs:<normalized-base-url>|acct=<id>|proj=<id>|scope=<normalized-scope>`

This lets LogChef enforce source uniqueness without encoding backend rules into SQLite schema constraints.

#### Source mutability rule

`source_type` should be immutable after creation in v1.

Changing a source from ClickHouse to VictoriaLogs in place would invalidate:

- saved queries
- alerts
- source-specific UI assumptions
- provider-specific identity keys

If an operator needs to switch backends, they should create a new source and rebind saved assets explicitly.

### 2. SQLite Migration Strategy

This RFC chooses an incremental migration, not a flag day.

#### Migration 1

This cannot be a simple additive migration.

The current `sources` table has:

- `host`, `username`, `password`, `database`, `table_name` as `NOT NULL`
- table-level `UNIQUE(database, table_name)`

That shape prevents storing a VictoriaLogs source cleanly, even if `source_type` is added later.

Migration 1 should therefore rebuild `sources`:

1. create `sources_new` with the generic columns
2. copy existing rows with:
   - `source_type = 'clickhouse'`
   - `connection_config` built from the old ClickHouse columns
   - `identity_key` built from canonical ClickHouse identity
3. preserve existing `id`, timestamps, `managed`, and `secret_ref`
4. drop old `sources`
5. rename `sources_new` to `sources`
6. recreate indexes with `identity_key` as the unique source anchor

The new table should include:

- `id`
- `name`
- `_meta_is_auto_created`
- `_meta_ts_field`
- `_meta_severity_field`
- `source_type`
- `connection_config`
- `identity_key`
- `description`
- `ttl_days`
- `managed`
- `secret_ref`
- `created_at`
- `updated_at`

Compatibility with existing ClickHouse code should live in Go mapping and JSON translation, not by keeping backend-specific database columns alive indefinitely.

#### Migration 2

Add alert and saved-query language fields:

- `team_queries.query_language`
- `alerts.query_language`

Backfill:

- saved query `sql` -> `clickhouse-sql`
- saved query `logchefql` -> `logchefql`
- alert stored SQL -> `clickhouse-sql`

Keep legacy query-type columns until the frontend and API cut over.

#### Migration 3

Remove transitional compatibility code:

- legacy ClickHouse create/update payload normalization
- old saved-query `query_type` assumptions
- old alert UI assumptions that equate `query_type=sql` with execution language

At this point, `editor_mode` can replace transitional UI-only fields where needed.

### 3. Backend Provider Layer

Add a new shared package:

- `internal/datasource`

Keep backend-specific low-level code in:

- `internal/clickhouse`
- `internal/victorialogs`

#### Core interfaces

```go
package datasource

type SourceType string

const (
    SourceTypeClickHouse   SourceType = "clickhouse"
    SourceTypeVictoriaLogs SourceType = "victorialogs"
)

type Capability string

const (
    CapabilityLogchefQL        Capability = "logchefql"
    CapabilityNativeQuery      Capability = "native_query"
    CapabilityFieldDiscovery   Capability = "field_discovery"
    CapabilityHistogram        Capability = "histogram"
    CapabilityAlertEvaluation  Capability = "alert_evaluation"
    CapabilityAutoCreate       Capability = "auto_create"
    CapabilityAISQLGeneration  Capability = "ai_sql_generation"
    CapabilityDetailedStats    Capability = "detailed_stats"
)

type Provider interface {
    Type() SourceType
    Capabilities() []Capability

    Validate(context.Context, *models.Source) (*ValidationResult, error)
    Health(context.Context, *models.Source) (*models.SourceHealth, error)
    Describe(context.Context, *models.Source) (*DescribeResult, error)

    QueryLogs(context.Context, *models.Source, QueryRequest) (*models.QueryResult, error)
    Histogram(context.Context, *models.Source, HistogramRequest) (*HistogramResult, error)
    FieldNames(context.Context, *models.Source, DiscoveryRequest) ([]FieldInfo, error)
    FieldValues(context.Context, *models.Source, FieldValuesRequest) (*FieldValuesResult, error)

    EvaluateAlert(context.Context, *models.Source, AlertEvalRequest) (*AlertEvalResult, error)
}
```

Add a registry/service:

```go
type Registry interface {
    Get(SourceType) (Provider, error)
}

type Service struct {
    registry Registry
    db       *sqlite.DB
    log      *slog.Logger
}
```

The service resolves the source from SQLite, selects the provider, and delegates the operation.

#### Why this split

This minimizes churn:

- existing `internal/clickhouse` code stays reusable
- the new datasource service becomes the seam used by `core`, `server`, `alerts`, and `app`
- VictoriaLogs can be built in parallel without rewriting ClickHouse internals first

### 4. ClickHouse Provider

The ClickHouse provider is the compatibility anchor.

It should wrap existing behavior first:

- source validation and ping
- schema introspection
- SQL execution
- histogram
- field values
- alert evaluation

Existing packages that can be wrapped instead of rewritten immediately:

- `internal/clickhouse`
- `internal/core/source.go`
- `internal/core/logs.go`

In the first refactor, the ClickHouse provider should continue to expose:

- `logchefql`
- `clickhouse-sql`

### 5. VictoriaLogs Provider

Add a new package:

- `internal/victorialogs`

This package should implement:

- HTTP client
- request signing/auth
- tenant header injection
- immutable scope filter injection
- query builders for common LogChef flows

#### VictoriaLogs operation mapping

| LogChef feature | VictoriaLogs primitive |
|---|---|
| log retrieval | `/select/logsql/query` |
| histogram | `/select/logsql/hits` |
| field names | `/select/logsql/field_names` |
| field values | `/select/logsql/field_values` |
| instant numeric alert eval | `/select/logsql/stats_query` |
| range numeric preview | `/select/logsql/stats_query_range` |

#### Important provider rules

- The provider owns injection of immutable source scope.
- The provider owns tenant header injection.
- User query text must not be allowed to override source-level scope.
- Query timeouts must map to VictoriaLogs timeout args.
- Context queries should prefer `_stream_id` plus `_time`, not timestamp-only pagination.

### 6. Query Model

#### Query languages

The app-level query language identifiers become:

- `logchefql`
- `clickhouse-sql`
- `logsql`

#### Generic request envelope

Replace the SQL-shaped request with a provider-neutral envelope:

```json
{
  "query_language": "logchefql",
  "query_text": "level=\"error\"",
  "start_time": "2026-04-08T12:00:00Z",
  "end_time": "2026-04-08T13:00:00Z",
  "timezone": "Asia/Kolkata",
  "limit": 100,
  "offset": 0,
  "query_timeout": 30,
  "variables": []
}
```

Notes:

- `query_text` replaces `raw_sql`.
- `start_time` and `end_time` stay explicit because they are needed by:
  - `logchefql`
  - VictoriaLogs field discovery
  - VictoriaLogs histograms
  - alerts and previews
- For ClickHouse native SQL, the provider may ignore these fields unless macros or server-side wrapping are introduced.

#### Query response additions

Add provider-neutral row locator metadata for log context:

```json
{
  "data": [
    {
      "_time": "2026-04-08T12:34:56Z",
      "_stream_id": "abc",
      "_meta": {
        "row_locator": {
          "provider": "victorialogs",
          "timestamp_field": "_time",
          "timestamp": "2026-04-08T12:34:56Z",
          "stream_id": "abc"
        }
      }
    }
  ]
}
```

ClickHouse can initially return timestamp-only locators for compatibility and improve later.

### 7. Saved Queries

Current saved queries encode `query_type` as `sql` or `logchefql`. That is no longer sufficient.

#### Final shape

- `editor_mode`
  - `builder`
  - `native`
- `query_language`
  - `logchefql`
  - `clickhouse-sql`
  - `logsql`

For the migration phase, add `query_language` first and keep `query_type` until the frontend is switched over.

Concrete migration rule:

- existing `query_type=logchefql` rows backfill to `query_language=logchefql`
- existing `query_type=sql` rows backfill to `query_language=clickhouse-sql`
- `SavedQueryContent` may keep its current JSON structure in the first pass; the row-level language field becomes the execution source of truth

#### Behavior

- ClickHouse saved queries may continue to use `logchefql` or `clickhouse-sql`.
- VictoriaLogs saved queries use `logsql`.
- Saved queries remain tied to a specific source ID, so there is no need for cross-provider portability in this RFC.

### 8. Alerts

Current alerts mix editor intent and execution language. Stored `Query` is SQL regardless of `query_type`.

That model should be split.

#### Final shape

- `editor_mode`
  - `condition`
  - `native`
- `query_language`
  - `clickhouse-sql`
  - `logsql`
- `query`
  - provider-native executable query

#### Execution rules

- ClickHouse:
  - native alerts execute SQL
  - condition-builder alerts compile to SQL
- VictoriaLogs:
  - native alerts execute numeric LogsQL through `stats_query`
  - condition-builder alerts are a later phase

#### MVP decision

VictoriaLogs alerting ships as native LogsQL only.

Condition-builder parity for VictoriaLogs is intentionally deferred, because the current condition-builder pipeline is SQL-shaped and should not block initial backend support.

Concrete migration rule:

- add `alerts.query_language TEXT NOT NULL DEFAULT 'clickhouse-sql'`
- backfill all existing alerts to `clickhouse-sql`, including `query_type=condition`, because the current evaluator stores executable SQL in `alerts.query`
- keep `query_type` temporarily as the editor intent field until the alert UI is migrated to `editor_mode`

### 9. Source Description and Capabilities

Replace the current ClickHouse-only source detail response with a generic source description.

Suggested response:

```json
{
  "id": 1,
  "name": "Payments Logs",
  "source_type": "victorialogs",
  "is_connected": true,
  "capabilities": [
    "native_query",
    "field_discovery",
    "histogram",
    "alert_evaluation"
  ],
  "describe": {
    "provider_summary": {
      "base_url": "https://logs.example.com",
      "tenant": {
        "account_id": "12",
        "project_id": "34"
      }
    },
    "fields": [
      {"name": "_time"},
      {"name": "_msg"},
      {"name": "service"},
      {"name": "level"}
    ]
  }
}
```

For ClickHouse, `describe` can continue to include:

- columns
- create statement
- engine
- sort keys
- table stats

For VictoriaLogs, `describe` should include:

- discovered fields
- tenant scope
- immutable source filter
- provider capabilities

### 10. API Changes

#### Admin source APIs

Current payloads are ClickHouse-specific.

Replace with:

```json
{
  "name": "Payments Logs",
  "source_type": "victorialogs",
  "meta_ts_field": "_time",
  "meta_severity_field": "level",
  "description": "Production payments logs",
  "connection": {
    "base_url": "https://logs.example.com",
    "auth": {
      "mode": "bearer",
      "token": "secret"
    },
    "tenant": {
      "account_id": "12",
      "project_id": "34"
    },
    "scope": {
      "query": "{app=\"payments\"} kubernetes.namespace:=prod"
    }
  }
}
```

For backwards compatibility:

- keep the current ClickHouse payload accepted for one release
- normalize it into `source_type=clickhouse` internally

#### Generic query APIs

Keep existing route paths where possible:

- `POST /teams/:teamID/sources/:sourceID/logs/query`
- `POST /teams/:teamID/sources/:sourceID/logs/histogram`
- `GET /teams/:teamID/sources/:sourceID/fields/values`

But change payload semantics to provider-neutral query envelopes.

#### New metadata endpoints

Add:

- `GET /api/v1/source-types`
- `GET /api/v1/source-types/:type/schema`
- `GET /api/v1/teams/:teamID/sources/:sourceID/capabilities`
- `GET /api/v1/teams/:teamID/sources/:sourceID/describe`

#### LogchefQL routes

Keep:

- `/logchefql/translate`
- `/logchefql/validate`
- `/logchefql/query`

but only advertise them for ClickHouse sources.

Do not add fake translation endpoints for VictoriaLogs in the first phase.

### 11. Frontend Changes

#### Source list and source details

Add:

- datasource type badge in source list
- capability badges in source details
- provider-specific detail panels

ClickHouse source details keep schema/stats.
VictoriaLogs source details show tenant/scope/discovered fields/capabilities.

#### Add Source screen

Current `AddSource.vue` is ClickHouse-only.

Refactor source creation into:

1. datasource type picker
2. provider-specific form component

Proposed components:

- `ClickHouseSourceForm.vue`
- `VictoriaLogsSourceForm.vue`

ClickHouse-specific UI that must remain provider-gated:

- create vs connect existing table
- CREATE TABLE preview
- TTL-driven schema generation

VictoriaLogs-specific UI:

- base URL
- auth mode
- AccountID / ProjectID
- immutable scope query
- test connection

The existing create/edit route can stay the same, but the screen needs a type selector at the top and must render provider-specific subforms instead of one giant conditional template.

#### Explore screen

The editor tabs become dynamic per source type.

ClickHouse:

- `LogchefQL`
- `SQL`

VictoriaLogs:

- `LogsQL`

The current `QueryService` must stop generating SQL as the shared query format.

Primary frontend touchpoints in this phase:

- `frontend/src/views/explore/LogExplorer.vue`
- `frontend/src/composables/useQuery.ts`
- `frontend/src/services/QueryService.ts`
- `frontend/src/services/SqlManager.ts`
- `frontend/src/composables/useFieldValuesLoader.ts`

#### Alert screen

ClickHouse:

- keep condition builder
- keep native SQL

VictoriaLogs MVP:

- native LogsQL only

The UI must clearly show that alert query language depends on datasource type.

Primary frontend touchpoints in this phase:

- `frontend/src/views/alerts/AlertCreate.vue`
- `frontend/src/views/alerts/AlertDetail.vue`
- `frontend/src/stores/alerts.ts`

#### Saved Queries

Saved query labels should show language explicitly:

- `LogchefQL`
- `SQL`
- `LogsQL`

Primary frontend touchpoints in this phase:

- `frontend/src/components/collections/SaveQueryModal.vue`
- `frontend/src/stores/savedQueries.ts`
- `frontend/src/views/collections/SavedQueriesView.vue`

### 12. Security and RBAC

This is one of the main product reasons to support VictoriaLogs via LogChef.

#### Rules

- users never set tenant headers directly
- users never set datasource-level auth directly
- users never control immutable source scope
- all VictoriaLogs requests are executed through LogChef using source-owned auth and scope

#### Source scope enforcement

Each VictoriaLogs source may define an immutable source-level scope query.

Examples:

- `{app="payments"}`
- `{cluster="prod"} kubernetes.namespace:=prod`

The provider prepends or injects this scope into every query, histogram, discovery, and alert evaluation call.

This gives LogChef meaningful per-source RBAC on top of a shared VictoriaLogs cluster.

### 13. Dev and Test Strategy

#### Dev environment

Extend `dev/docker-compose.yml` with VictoriaLogs for local development.

Add:

- VictoriaLogs service
- sample seed source for VictoriaLogs
- minimal ingestion docs for local testing

#### Tests

Add provider contract tests:

- validate connection
- health check
- query logs
- histogram
- field names
- field values
- log context
- alert evaluation

ClickHouse and VictoriaLogs should both pass the same contract suite where the capability exists.

#### Compatibility tests

Before merging VictoriaLogs work, ensure ClickHouse behavior is unchanged for:

- source creation
- saved queries
- log exploration
- field sidebar
- alert evaluation

### 14. Rollout Plan

This RFC proposes the following PR sequence.

#### PR 1: Source model and migration scaffolding

- rebuild `sources` into a generic datasource table
- backfill ClickHouse rows into `connection_config`
- add `source_type` and `identity_key`
- update `models.Source`, create/update request types, and source serialization
- update SQLite queries, sqlc models, and provisioning reads/writes

No behavior change yet.

#### PR 2: Datasource registry and service

- add `internal/datasource`
- add registry/service interfaces
- wire app initialization through datasource service
- keep only ClickHouse provider registered

No product change yet.

#### PR 3: Wrap existing ClickHouse behavior in provider

- implement ClickHouse provider using current `internal/clickhouse`
- route `core` and `server` through datasource service
- preserve existing APIs

This is the first real seam.

#### PR 4: Provider-aware source management UI

- add source type picker
- split source forms
- keep ClickHouse behavior unchanged
- add datasource type badges in source list/detail

Likely files:

- `frontend/src/views/sources/AddSource.vue`
- `frontend/src/views/sources/ManageSources.vue`
- source API client/types

#### PR 5: Generic query envelope and dynamic editor capabilities

- replace `raw_sql`-centric request payloads
- add `query_language`
- make editor tabs/provider capabilities dynamic
- keep ClickHouse `LogchefQL` and `SQL`

Likely files:

- `pkg/models/query.go`
- query handlers/server request parsing
- `frontend/src/services/QueryService.ts`
- `frontend/src/composables/useQuery.ts`
- `frontend/src/views/explore/LogExplorer.vue`

#### PR 6: VictoriaLogs provider MVP

- validation
- health
- log query
- histogram via `/hits`
- field discovery
- field values
- source capabilities and description

At this point, basic log exploration works for VictoriaLogs.

#### PR 7: Alert evaluation abstraction

- route alerts through datasource service
- add `alerts.query_language`
- enable native LogsQL numeric alerts via `stats_query`

Likely files:

- `pkg/models/alerts.go`
- `internal/core/alerts.go`
- `internal/alerts/manager.go`
- `internal/sqlite/alerts.go`
- `frontend/src/views/alerts/AlertCreate.vue`
- `frontend/src/views/alerts/AlertDetail.vue`

#### PR 8: Saved queries migration

- add `team_queries.query_language`
- migrate old saved query type values
- update frontend labels and editors

Likely files:

- `pkg/models/query.go`
- `internal/core/saved_queries.go`
- `internal/sqlite/team_queries.go`
- `frontend/src/components/collections/SaveQueryModal.vue`
- `frontend/src/stores/savedQueries.ts`

#### PR 9: Dev env, documentation, cleanup

- local VictoriaLogs service
- seed/example source
- user docs
- deprecation warnings for old payload shapes

### 15. Risks

#### Risk: treating SQL as a universal format leaks back in

Mitigation:

- make `query_language` explicit everywhere
- forbid generic server code from assuming SQL

#### Risk: source migration becomes messy

Mitigation:

- create-copy-swap migration for `sources` with preserved IDs
- migration test against a real pre-RFC SQLite database
- `identity_key` as the new unique source anchor

#### Risk: frontend complexity grows too fast

Mitigation:

- capability-driven rendering
- provider-specific form/editor components
- no attempt at cross-provider builder parity in the first release

#### Risk: VictoriaLogs scope enforcement is bypassed

Mitigation:

- provider injects tenant and immutable scope server-side
- no client-controlled provider headers
- no direct query passthrough around provider layer

### 16. Follow-up RFCs

The following should be separate follow-up RFCs after this lands:

- backend-neutral LogchefQL subset
- provider-specific AI query generation
- generic condition builder across providers
- datasource plugin model for third-party backends

## Recommendation

Approve this RFC and implement it in the PR order above.

The architectural bar should be:

- ClickHouse remains stable
- VictoriaLogs ships with native LogsQL support
- LogChef owns the control plane
- datasource type becomes a first-class concept everywhere

That gives LogChef the right long-term shape for a multi-datasource log product instead of extending the current ClickHouse-only model indefinitely.
