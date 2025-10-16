# LogChef Alerting System - Implementation Roadmap

## Overview

This document outlines the implementation plan for adding an alerting system to LogChef that monitors query results or log conditions and notifies users when thresholds are met. The design integrates seamlessly with the existing Go-Fiber/Vue3 architecture.

## Core Features

- **Alert Rules**: Custom SQL queries or condition-based log monitoring
- **Thresholds & Scheduling**: Configurable thresholds, frequency, and severity levels
- **Multi-Channel Notifications**: Email, Slack, webhook support
- **Alert History**: Complete audit trail with timestamps and status tracking
- **Proactive Detection**: Real-time monitoring for anomalies, errors, and important events

## Architecture Overview

The alerting system follows LogChef's existing patterns:
- SQLite for metadata storage (rules, channels, events) via SQLC
- ClickHouse for log data queries and aggregations
- Go services with clean interfaces and testable components
- Vue 3 frontend with Pinia state management and Radix UI components

## Database Schema

### New Tables

```sql
-- Alert rule definitions
CREATE TABLE alert_rules (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id         INTEGER NOT NULL REFERENCES teams(id),
    name            TEXT NOT NULL,
    description     TEXT,
    query_type      TEXT NOT NULL,      -- 'sql' | 'condition'
    query_text      TEXT,               -- Raw SQL for ClickHouse
    condition_json  TEXT,               -- JSON for DSL conditions
    threshold_op    TEXT NOT NULL,      -- '>' '>=' '<' '<=' '=' '!='
    threshold_value REAL,
    frequency_sec   INTEGER NOT NULL DEFAULT 60,
    severity        TEXT NOT NULL DEFAULT 'medium',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME,
    last_triggered_at DATETIME
);

-- Notification channel configurations
CREATE TABLE notification_channels (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id     INTEGER NOT NULL REFERENCES teams(id),
    type        TEXT NOT NULL,           -- email|slack|webhook
    name        TEXT,
    config_json TEXT NOT NULL,           -- Configuration data
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Many-to-many: rules to channels
CREATE TABLE alert_rule_channels (
    rule_id     INTEGER NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    channel_id  INTEGER NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, channel_id)
);

-- Alert event history
CREATE TABLE alert_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id         INTEGER NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    triggered_at    DATETIME NOT NULL,
    resolved_at     DATETIME,
    status          TEXT NOT NULL,      -- triggered | resolved | error
    triggered_value REAL,
    details_json    TEXT                -- Query result snapshot / error info
);
```

## Backend Implementation

### Package Structure: `internal/alert/`

1. **evaluator/**
   - `Evaluator` interface for rule evaluation
   - `SQLQueryEvaluator` - executes custom SQL against ClickHouse
   - `ConditionEvaluator` - builds queries from condition DSL
   - `AnomalyEvaluator` - statistical anomaly detection (Phase 2)

2. **scheduler/**
   - Cron-based scheduling using `robfig/cron/v3`
   - Worker pool for concurrent rule evaluation
   - Context-aware timeout handling

3. **notifier/**
   - `Notifier` interface for pluggable notification channels
   - Email, Slack, and webhook implementations
   - Secure configuration handling

4. **service/**
   - `AlertService` - orchestrates scheduler, evaluator, and notifiers
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
POST   /api/v1/alert-rules/:id/test      # Dry run
POST   /api/v1/alert-rules/:id/enable
POST   /api/v1/alert-rules/:id/disable

# Notification Channels
GET    /api/v1/notification-channels
POST   /api/v1/notification-channels
PUT    /api/v1/notification-channels/:id
DELETE /api/v1/notification-channels/:id

# Alert Events
GET    /api/v1/alert-events

# Real-time (optional)
/ws/alerts                              # WebSocket for live updates
```

## Frontend Implementation

### Pinia Stores

- `useAlertRulesStore.ts` - CRUD operations for alert rules
- `useAlertEventsStore.ts` - Event history and real-time updates
- `useNotificationChannelsStore.ts` - Channel management

### Vue Components

1. **AlertsOverview.vue**
   - Data table with rule status, last triggered, and controls
   - Severity badges and status indicators
   - Quick enable/disable toggles

2. **AlertRuleForm.vue**
   - Multi-step wizard for rule creation/editing
   - Query builder with SQL and condition tabs
   - Live rule testing functionality
   - Notification channel selection

3. **AlertHistory.vue**
   - Timeline view of alert events
   - Expandable event details
   - Status tracking and resolution

4. **NotificationSettings.vue**
   - Channel configuration management
   - Secure credential handling

5. **SaveAsAlertButton.vue**
   - Integration with log query results
   - Pre-populated rule creation

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
- [ ] Database schema and migrations
- [ ] SQLC query generation
- [ ] Basic alert service structure
- [ ] Simple threshold evaluator
- [ ] Email notification support

### Phase 2: Enhanced Features
- [ ] Slack and webhook notifications
- [ ] Advanced query builder UI
- [ ] Real-time WebSocket updates
- [ ] Alert event history and resolution
- [ ] Team-based permissions

### Phase 3: Advanced Capabilities
- [ ] Anomaly detection evaluator
- [ ] Composite rules (AND/OR logic)
- [ ] Rate limiting and suppression
- [ ] Escalation policies
- [ ] Dashboard integration

## Testing Strategy

### Backend Tests
- Unit tests for evaluators with mocked ClickHouse
- Notifier tests with HTTP mocks and SMTP stubs
- Integration tests for scheduler pipeline
- Database migration tests

### Frontend Tests
- Vitest unit tests for stores and components
- Cypress E2E tests for alert creation workflow
- UI component testing with mock API responses

### Load Testing
- ClickHouse query performance under alert load
- Concurrent rule evaluation scaling
- Notification delivery reliability

## Deployment Strategy

1. **Feature Flag Rollout**
   - Deploy schema changes with `ALERTING_ENABLED=false`
   - Enable UI for rule creation without evaluation
   - Gradual scheduler enablement

2. **Monitoring & Observability**
   - Alert service metrics and health checks
   - ClickHouse query performance monitoring
   - Notification delivery success rates

3. **Configuration Management**
   - Environment-specific alert thresholds
   - Notification channel credentials handling
   - Rate limiting and resource controls

## Future Enhancements

- **Additional Integrations**: PagerDuty, OpsGenie, Microsoft Teams
- **Machine Learning**: Predictive anomaly detection
- **Advanced Scheduling**: Business hours, maintenance windows
- **Alert Clustering**: Group related alerts to reduce noise
- **Mobile Notifications**: Push notifications for critical alerts

## Success Metrics

- **User Adoption**: Number of active alert rules per team
- **Performance**: Alert evaluation latency and resource usage
- **Reliability**: False positive/negative rates and notification delivery
- **User Experience**: Time to create first alert, support ticket reduction

---

This roadmap provides a comprehensive foundation for implementing a production-ready alerting system in LogChef while maintaining architectural consistency and user experience quality.
