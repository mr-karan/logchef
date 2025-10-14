package models

import "time"

// AlertQueryType represents the strategy used to evaluate an alert.
type AlertQueryType string

const (
	// AlertQueryTypeSQL indicates a raw SQL query will be executed against ClickHouse.
	AlertQueryTypeSQL AlertQueryType = "sql"
	// AlertQueryTypeLogCondition indicates a filter-based condition evaluated over recent logs.
	AlertQueryTypeLogCondition AlertQueryType = "log_condition"
)

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
)

// AlertChannelType enumerates supported outbound notification channels.
type AlertChannelType string

const (
	AlertChannelEmail   AlertChannelType = "email"
	AlertChannelSlack   AlertChannelType = "slack"
	AlertChannelWebhook AlertChannelType = "webhook"
)

// AlertChannel represents a single notification target.
type AlertChannel struct {
	Type   AlertChannelType `json:"type"`
	Target string           `json:"target"`
	// Metadata allows the UI to store channel specific configuration.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Alert encapsulates a rule that is continuously evaluated against log data.
type Alert struct {
	ID                AlertID                `json:"id"`
	TeamID            TeamID                 `json:"team_id"`
	SourceID          SourceID               `json:"source_id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description,omitempty"`
	QueryType         AlertQueryType         `json:"query_type"`
	Query             string                 `json:"query"`
	LookbackSeconds   int                    `json:"lookback_seconds"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
	FrequencySeconds  int                    `json:"frequency_seconds"`
	Severity          AlertSeverity          `json:"severity"`
	Channels          []AlertChannel         `json:"channels"`
	IsActive          bool                   `json:"is_active"`
	LastEvaluatedAt   *time.Time             `json:"last_evaluated_at,omitempty"`
	LastTriggeredAt   *time.Time             `json:"last_triggered_at,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// AlertHistoryEntry captures individual trigger or resolution events for an alert.
type AlertHistoryEntry struct {
	ID          int64          `json:"id"`
	AlertID     AlertID        `json:"alert_id"`
	Status      AlertStatus    `json:"status"`
	TriggeredAt time.Time      `json:"triggered_at"`
	ResolvedAt  *time.Time     `json:"resolved_at,omitempty"`
	ValueText   string         `json:"value_text"`
	Channels    []AlertChannel `json:"channels"`
	Message     string         `json:"message,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// CreateAlertRequest defines the payload required to create a new alert rule.
type CreateAlertRequest struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	QueryType         AlertQueryType         `json:"query_type"`
	Query             string                 `json:"query"`
	LookbackSeconds   int                    `json:"lookback_seconds"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
	FrequencySeconds  int                    `json:"frequency_seconds"`
	Severity          AlertSeverity          `json:"severity"`
	Channels          []AlertChannel         `json:"channels"`
	IsActive          bool                   `json:"is_active"`
}

// UpdateAlertRequest defines updatable fields for an alert rule.
type UpdateAlertRequest struct {
	Name              *string                 `json:"name"`
	Description       *string                 `json:"description"`
	QueryType         *AlertQueryType         `json:"query_type"`
	Query             *string                 `json:"query"`
	LookbackSeconds   *int                    `json:"lookback_seconds"`
	ThresholdOperator *AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    *float64                `json:"threshold_value"`
	FrequencySeconds  *int                    `json:"frequency_seconds"`
	Severity          *AlertSeverity          `json:"severity"`
	Channels          *[]AlertChannel         `json:"channels"`
	IsActive          *bool                   `json:"is_active"`
}

// ResolveAlertRequest allows callers to provide context when manually resolving an alert.
type ResolveAlertRequest struct {
	Message string `json:"message"`
}

// DefaultAlertHistoryLimit controls the number of history entries returned when unspecified.
const DefaultAlertHistoryLimit = 50
