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
)

// Alert encapsulates a rule that is continuously evaluated against log data.
type Alert struct {
	ID                AlertID                `json:"id"`
	TeamID            TeamID                 `json:"team_id"`
	SourceID          SourceID               `json:"source_id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description,omitempty"`
	Query             string                 `json:"query"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
	FrequencySeconds  int                    `json:"frequency_seconds"`
	Severity          AlertSeverity          `json:"severity"`
	RoomIDs           []RoomID               `json:"room_ids"`
	Rooms             []RoomSummary          `json:"rooms"`
	IsActive          bool                   `json:"is_active"`
	LastEvaluatedAt   *time.Time             `json:"last_evaluated_at,omitempty"`
	LastTriggeredAt   *time.Time             `json:"last_triggered_at,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// AlertHistoryEntry captures individual trigger or resolution events for an alert.
type AlertHistoryEntry struct {
	ID          int64                        `json:"id"`
	AlertID     AlertID                      `json:"alert_id"`
	Status      AlertStatus                  `json:"status"`
	TriggeredAt time.Time                    `json:"triggered_at"`
	ResolvedAt  *time.Time                   `json:"resolved_at,omitempty"`
	ValueText   string                       `json:"value_text"`
	Rooms       []AlertHistoryRoomSnapshot   `json:"rooms"`
	Message     string                       `json:"message,omitempty"`
	CreatedAt   time.Time                    `json:"created_at"`
}

// CreateAlertRequest defines the payload required to create a new alert rule.
type CreateAlertRequest struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Query             string                 `json:"query"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
	FrequencySeconds  int                    `json:"frequency_seconds"`
	Severity          AlertSeverity          `json:"severity"`
	RoomIDs           []RoomID               `json:"room_ids"`
	IsActive          bool                   `json:"is_active"`
}

// UpdateAlertRequest defines updatable fields for an alert rule.
type UpdateAlertRequest struct {
	Name              *string                 `json:"name"`
	Description       *string                 `json:"description"`
	Query             *string                 `json:"query"`
	ThresholdOperator *AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    *float64                `json:"threshold_value"`
	FrequencySeconds  *int                    `json:"frequency_seconds"`
	Severity          *AlertSeverity          `json:"severity"`
	RoomIDs           *[]RoomID               `json:"room_ids"`
	IsActive          *bool                   `json:"is_active"`
}

// ResolveAlertRequest allows callers to provide context when manually resolving an alert.
type ResolveAlertRequest struct {
	Message string `json:"message"`
}

// TestAlertQueryRequest allows testing an alert query before saving.
type TestAlertQueryRequest struct {
	Query             string                 `json:"query"`
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
const DefaultAlertHistoryLimit = 50
