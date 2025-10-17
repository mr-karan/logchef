# LogChef Alerting System - Implementation Roadmap

## Overview

This document outlines the implementation plan for adding an alerting system to LogChef that monitors query results or log conditions and notifies users when thresholds are met. Alert delivery is delegated to an external Alertmanager instance, keeping the LogChef codebase focused on evaluation while leveraging Alertmanager’s mature routing and notification ecosystem. The design integrates seamlessly with the existing Go-Fiber/Vue3 architecture.

## Core Features

- **Alert Rules**: Custom SQL queries or condition-based log monitoring that produce a numeric result
- **Threshold Evaluation**: Configurable comparison operators, rolling windows, frequency, and severity levels
- **Alertmanager Integration**: Direct delivery to a Prometheus Alertmanager instance with rich labels and annotations
- **Alert History**: Optional local audit trail with timestamps, values, and resolution state
- **UI Workflow**: Rule builder with live testing, metadata management, and quick links into Alertmanager

## Architecture Overview

The alerting system follows LogChef's existing patterns while outsourcing notification delivery:
- SQLite for metadata storage (rules plus optional evaluation history) via SQLC
- ClickHouse for log data queries and aggregations
- Go services with clean interfaces, including an Alertmanager client
- Vue 3 frontend with Pinia state management and Radix UI components
- Declarative configuration (e.g. `config.toml`) for pointing to a team’s Alertmanager endpoint

## Database Schema

### New Tables

```sql
-- Alert rule definitions
CREATE TABLE alert_rules (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id          INTEGER NOT NULL REFERENCES teams(id),
    name             TEXT NOT NULL,
    description      TEXT,
    query_type       TEXT NOT NULL,      -- 'sql' | 'condition'
    query_text       TEXT,               -- Raw SQL for ClickHouse
    condition_json   TEXT,               -- JSON for DSL conditions
    threshold_op     TEXT NOT NULL,      -- '>' '>=' '<' '<=' '=' '!='
    threshold_value  REAL NOT NULL,
    frequency_sec    INTEGER NOT NULL DEFAULT 60,
    severity         TEXT NOT NULL DEFAULT 'warning',
    labels_json      TEXT,               -- Extra labels forwarded to Alertmanager
    annotations_json TEXT,               -- Additional annotations (runbook, summary)
    generator_url    TEXT,               -- Optional deep link back into LogChef
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME,
    last_triggered_at DATETIME,
    last_state       TEXT NOT NULL DEFAULT 'resolved'
);

-- Optional: evaluation history for audit trail
CREATE TABLE alert_events (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id          INTEGER NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    triggered_at     DATETIME NOT NULL,
    resolved_at      DATETIME,
    triggered_value  REAL,
    details_json     TEXT                -- Query result snapshot / payload metadata
);
```

### Configuration

Alertmanager connectivity is defined centrally (e.g. in `config.toml`) to keep deployment simple:

```toml
[alerting]
enabled = true
frequency_sec_default = 60
alertmanager_url = "https://alertmanager.internal/api/v2/alerts"
external_url = "https://logchef.example.com"  # Used as generatorURL
timeout_sec = 10
tls_insecure_skip_verify = false
```

Per-team overrides can be layered in later if required; the initial milestone assumes a single Alertmanager endpoint per LogChef deployment.

## Backend Implementation

### Package Structure: `internal/alert/`

1. **evaluator/**
   - `Evaluator` interface for rule evaluation
   - `SQLQueryEvaluator` - executes custom SQL against ClickHouse
   - `ConditionEvaluator` - builds queries from condition DSL
   - State tracking helpers for firing → resolved transitions

2. **alertmanager/**
   - Minimal client wrapping `/api/v2/alerts`
   - Payload builders for labels, annotations, generator URLs
   - Retry and backoff handling

3. **scheduler/**
   - Cron-based scheduling using `robfig/cron/v3`
   - Worker pool for concurrent rule evaluation
   - Context-aware timeout handling

4. **service/**
   - `AlertService` - orchestrates scheduler, evaluator, and Alertmanager client
   - Helper methods for manual triggering and resolution
   - Integration with existing LogChef services

### REST API Endpoints

```
# Alert Rules
GET    /api/v1/alert-rules
POST   /api/v1/alert-rules
GET    /api/v1/alert-rules/:id
PUT    /api/v1/alert-rules/:id
DELETE /api/v1/alert-rules/:id
POST   /api/v1/alert-rules/:id/test        # Dry run / preview
POST   /api/v1/alert-rules/:id/enable
POST   /api/v1/alert-rules/:id/disable
POST   /api/v1/alert-rules/:id/evaluate    # Manual evaluation (sends to Alertmanager)

# Alert Events (optional audit trail)
GET    /api/v1/alert-events

# Real-time (optional)
/ws/alerts                                  # WebSocket for live updates / status changes
```

## Frontend Implementation

### Pinia Stores

- `useAlertRulesStore.ts` - CRUD operations for alert rules
- `useAlertEventsStore.ts` - Event history and real-time updates
- `useAlertmanagerStore.ts` - Connectivity status, configuration metadata, health checks

### Vue Components

1. **AlertsOverview.vue**
   - Data table with rule status, last triggered, and controls
   - Severity badges, Alertmanager state (firing/resolved), and quick enable/disable toggles
   - Link out to the matching Alertmanager search

2. **AlertRuleForm.vue**
   - Multi-step wizard for rule creation/editing
   - Query builder with SQL and condition tabs
   - Live rule testing functionality with preview ranges
   - Labels and annotations editor (summary, runbook URL, etc.)

3. **AlertHistory.vue**
   - Timeline view of alert events
   - Expandable event details
   - Status tracking and resolution

4. **SaveAsAlertButton.vue**
   - Integration with log query results
   - Pre-populated rule creation

5. **AlertmanagerStatusBanner.vue** (optional)
   - Surface connectivity errors or silences
   - Provide CTA to open Alertmanager UI

## Example Query Templates

### Threshold Alert (Error Count)
```sql
SELECT count() AS error_count
FROM logs
WHERE level = 'ERROR'
  AND timestamp >= now() - INTERVAL 5 MINUTE;
```

### Anomaly Detection Query
```sql
WITH current_window AS (
    SELECT count() AS current_count
    FROM logs
    WHERE level = 'ERROR'
      AND timestamp >= now() - INTERVAL 5 MINUTE
),
historical_baseline AS (
    SELECT 
        quantile(0.95)(window_count) AS p95,
        avg(window_count) AS avg_count,
        stddevPop(window_count) AS std_dev
    FROM (
        SELECT count() AS window_count
        FROM logs
        WHERE level = 'ERROR'
          AND timestamp BETWEEN now() - INTERVAL 1 DAY AND now()
        GROUP BY toStartOfInterval(timestamp, INTERVAL 5 MINUTE)
    )
)
SELECT 
    current_window.current_count,
    historical_baseline.*
FROM current_window, historical_baseline;
```

## Implementation Phases

### Phase 1: Core Infrastructure
- [ ] Database schema and migrations (alert_rules + optional alert_events)
- [ ] SQLC query generation
- [ ] Alertmanager client with configuration loading and retries
- [ ] Threshold evaluator and scheduler wiring
- [ ] REST API for rule CRUD, test, enable/disable, manual evaluate
- [ ] Minimal Vue UI for listing and editing rules
- [ ] Basic audit logging of evaluations

### Phase 2: Enhanced Features
- [ ] Condition-builder UI and reusable query templates
- [ ] Live preview with selectable time ranges (table/chart)
- [ ] Alert event history UI and WebSocket updates
- [ ] Labels/annotations editor with validation helpers
- [ ] Alertmanager connectivity health indicators
- [ ] Team-based permissions and RBAC controls

### Phase 3: Advanced Capabilities
- [ ] Optional webhook fallback mode for non-Alertmanager users
- [ ] Per-team Alertmanager overrides and secrets management
- [ ] Anomaly detection evaluator
- [ ] Composite rules (AND/OR logic) and dependency suppression
- [ ] Business-hour scheduling and maintenance windows
- [ ] Dashboard integration and shared presets

## Testing Strategy

### Backend Tests
- Unit tests for evaluators with mocked ClickHouse
- Alertmanager client tests with HTTP mocks and retry scenarios
- Integration tests for scheduler pipeline
- Database migration tests

### Frontend Tests
- Vitest unit tests for stores and components
- Cypress E2E tests for alert creation workflow
- UI component testing with mock API responses

### Load Testing
- ClickHouse query performance under alert load
- Concurrent rule evaluation scaling
- Alertmanager delivery reliability (latency, retries, failures)

## Deployment Strategy

1. **Feature Flag Rollout**
   - Deploy schema changes with `ALERTING_ENABLED=false`
   - Enable UI for rule creation without Alertmanager delivery
   - Gradual scheduler enablement with canary rules

2. **Monitoring & Observability**
   - Alert service metrics and health checks
   - ClickHouse query performance monitoring
   - Alertmanager client success/failure counters and latency

3. **Configuration Management**
   - Environment-specific alert thresholds
   - Alertmanager endpoint, auth, and TLS handling
   - Retry, timeout, and batching controls

## Future Enhancements

- **Alertmanager Sync**: Surface silences, acknowledgements, and routing summaries inside LogChef
- **Machine Learning**: Predictive anomaly detection and adaptive thresholds
- **Advanced Scheduling**: Business hours, maintenance windows, holiday calendars
- **Alert Clustering**: Group related alerts to reduce noise
- **Standalone Notifications**: Optional built-in email/Slack sender for lightweight deployments

## Success Metrics

- **User Adoption**: Number of active alert rules per team
- **Performance**: Alert evaluation latency and resource usage
- **Reliability**: False positive/negative rates and Alertmanager delivery success
- **User Experience**: Time to create first alert, support ticket reduction

---

This roadmap provides a comprehensive foundation for implementing a production-ready alerting system in LogChef while maintaining architectural consistency and user experience quality.
