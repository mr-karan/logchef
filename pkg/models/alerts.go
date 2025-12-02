package models

import "time"

// AlertThresholdOperator represents the comparison operator used when checking the evaluated value.
type AlertThresholdOperator string

const (
	AlertThresholdGreaterThan        AlertThresholdOperator = "gt"
	AlertThresholdGreaterThanOrEqual AlertThresholdOperator = "gte"
	AlertThresholdLessThan           AlertThresholdOperator = "lt"
	AlertThresholdLessThanOrEqual    AlertThresholdOperator = "lte"
	AlertThresholdEqual              AlertThresholdOperator = "eq"
	AlertThresholdNotEqual           AlertThresholdOperator = "neq"
)

// AlertSeverity is a lightweight severity indicator for routing and display.
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

// AlertStatus captures the lifecycle state of an alert history entry.
type AlertStatus string

const (
	AlertStatusTriggered AlertStatus = "triggered"
	AlertStatusResolved  AlertStatus = "resolved"
	AlertStatusError     AlertStatus = "error"
)

// AlertQueryType represents the underlying evaluation strategy.
type AlertQueryType string

const (
	AlertQueryTypeSQL       AlertQueryType = "sql"
	AlertQueryTypeCondition AlertQueryType = "condition"
)

// AlertState captures the persisted lifecycle state of an alert rule.
type AlertState string

const (
	AlertStateFiring   AlertState = "firing"
	AlertStateResolved AlertState = "resolved"
)

// Alert encapsulates a rule that is continuously evaluated against log data.
type Alert struct {
	ID               AlertID                `json:"id"`
	TeamID           TeamID                 `json:"team_id"`
	SourceID         SourceID               `json:"source_id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description,omitempty"`
	QueryType        AlertQueryType         `json:"query_type"`
	Query            string                 `json:"query"`
	ConditionJSON    string                 `json:"condition_json,omitempty"`
	LookbackSeconds  int                    `json:"lookback_seconds"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue   float64                `json:"threshold_value"`
	FrequencySeconds int                    `json:"frequency_seconds"`
	Severity         AlertSeverity          `json:"severity"`
	Labels           map[string]string      `json:"labels,omitempty"`
	Annotations      map[string]string      `json:"annotations,omitempty"`
	GeneratorURL     string                 `json:"generator_url,omitempty"`
	IsActive         bool                   `json:"is_active"`
	LastState        AlertState             `json:"last_state"`
	LastEvaluatedAt  *time.Time             `json:"last_evaluated_at,omitempty"`
	LastTriggeredAt  *time.Time             `json:"last_triggered_at,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// AlertHistoryEntry captures individual trigger or resolution events for an alert.
type AlertHistoryEntry struct {
	ID          int64                  `json:"id"`
	AlertID     AlertID                `json:"alert_id"`
	Status      AlertStatus            `json:"status"`
	TriggeredAt time.Time              `json:"triggered_at"`
	ResolvedAt  *time.Time             `json:"resolved_at,omitempty"`
	Value       *float64               `json:"value,omitempty"`
	Message     string                 `json:"message,omitempty"`
	Payload     map[string]any         `json:"payload,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

// CreateAlertRequest defines the payload required to create a new alert rule.
type CreateAlertRequest struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	QueryType         AlertQueryType         `json:"query_type"`
	Query             string                 `json:"query"`
	ConditionJSON     string                 `json:"condition_json"`
	LookbackSeconds   int                    `json:"lookback_seconds"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
	FrequencySeconds  int                    `json:"frequency_seconds"`
	Severity          AlertSeverity          `json:"severity"`
	Labels            map[string]string      `json:"labels"`
	Annotations       map[string]string      `json:"annotations"`
	GeneratorURL      string                 `json:"generator_url"`
	IsActive          bool                   `json:"is_active"`
}

// UpdateAlertRequest defines updatable fields for an alert rule.
type UpdateAlertRequest struct {
	Name              *string                 `json:"name"`
	Description       *string                 `json:"description"`
	QueryType         *AlertQueryType         `json:"query_type"`
	Query             *string                 `json:"query"`
	ConditionJSON     *string                 `json:"condition_json"`
	LookbackSeconds   *int                    `json:"lookback_seconds"`
	ThresholdOperator *AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    *float64                `json:"threshold_value"`
	FrequencySeconds  *int                    `json:"frequency_seconds"`
	Severity          *AlertSeverity          `json:"severity"`
	Labels            *map[string]string      `json:"labels"`
	Annotations       *map[string]string      `json:"annotations"`
	GeneratorURL      *string                 `json:"generator_url"`
	IsActive          *bool                   `json:"is_active"`
}

// ResolveAlertRequest allows callers to provide context when manually resolving an alert.
type ResolveAlertRequest struct {
	Message string `json:"message"`
}

// TestAlertQueryRequest allows testing an alert query before saving.
type TestAlertQueryRequest struct {
	QueryType         AlertQueryType         `json:"query_type"`
	Query             string                 `json:"query"`
	ConditionJSON     string                 `json:"condition_json"`
	LookbackSeconds   int                    `json:"lookback_seconds"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
}

// TestAlertQueryResponse returns the result of a test query execution with performance metrics.
type TestAlertQueryResponse struct {
	Value          float64  `json:"value"`
	ThresholdMet   bool     `json:"threshold_met"`
	ExecutionTimeMs int64   `json:"execution_time_ms"`
	RowsReturned   int      `json:"rows_returned"`
	Warnings       []string `json:"warnings"`
}

// DefaultAlertHistoryLimit controls the number of history entries returned when unspecified.
const DefaultAlertHistoryLimit = 100
