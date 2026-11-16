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

// AlertEditorMode captures which alert authoring workflow created the alert.
type AlertEditorMode string

const (
	AlertEditorModeCondition AlertEditorMode = "condition"
	AlertEditorModeNative    AlertEditorMode = "native"
)

func NormalizeAlertEditorMode(mode AlertEditorMode) AlertEditorMode {
	switch mode {
	case AlertEditorModeCondition:
		return AlertEditorModeCondition
	case AlertEditorModeNative:
		return AlertEditorModeNative
	default:
		return mode
	}
}

func (m AlertEditorMode) Valid() bool {
	switch NormalizeAlertEditorMode(m) {
	case AlertEditorModeCondition, AlertEditorModeNative:
		return true
	default:
		return false
	}
}

func ResolveAlertMetadata(language QueryLanguage, mode AlertEditorMode) (QueryLanguage, AlertEditorMode, error) {
	normalizedLanguage := NormalizeQueryLanguage(language)
	normalizedMode := NormalizeAlertEditorMode(mode)

	if normalizedLanguage == "" {
		switch normalizedMode {
		case AlertEditorModeCondition:
			normalizedLanguage = QueryLanguageClickHouseSQL
		default:
			return "", "", ErrInvalidAlertQueryConfiguration{Value: "query_language is required"}
		}
	}

	switch normalizedLanguage {
	case QueryLanguageClickHouseSQL, QueryLanguageLogsQL, QueryLanguageLogQL:
	default:
		return "", "", ErrInvalidAlertQueryConfiguration{Value: string(normalizedLanguage)}
	}

	if normalizedMode == "" {
		normalizedMode = AlertEditorModeNative
	}

	if !normalizedMode.Valid() {
		return "", "", ErrInvalidAlertQueryConfiguration{Value: string(mode)}
	}

	return normalizedLanguage, normalizedMode, nil
}

type ErrInvalidAlertQueryConfiguration struct {
	Value string
}

func (e ErrInvalidAlertQueryConfiguration) Error() string {
	return "invalid alert query configuration: " + e.Value
}

// AlertState captures the persisted lifecycle state of an alert rule.
type AlertState string

const (
	AlertStateFiring   AlertState = "firing"
	AlertStateResolved AlertState = "resolved"
)

// Alert encapsulates a rule that is continuously evaluated against log data.
// Alerts are scoped to a single source. Visibility (read access) is granted to
// any user with source access via any team; edit access is creator + global
// admin (legacy alerts with NULL CreatedBy are global-admin-only).
type Alert struct {
	ID                AlertID                `json:"id"`
	SourceID          SourceID               `json:"source_id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description,omitempty"`
	QueryLanguage     QueryLanguage          `json:"query_language"`
	EditorMode        AlertEditorMode        `json:"editor_mode"`
	Query             string                 `json:"query"`
	ConditionJSON     string                 `json:"condition_json,omitempty"`
	LookbackSeconds   int                    `json:"lookback_seconds"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
	FrequencySeconds  int                    `json:"frequency_seconds"`
	Severity          AlertSeverity          `json:"severity"`
	Labels            map[string]string      `json:"labels,omitempty"`
	Annotations       map[string]string      `json:"annotations,omitempty"`
	DestinationIDs    []DestinationID        `json:"destination_ids"`
	GeneratorURL      string                 `json:"generator_url,omitempty"`
	// GroupBy names the result columns that split the rule into one state per
	// group. Empty means ungrouped.
	GroupBy         []string   `json:"group_by"`
	IsActive        bool       `json:"is_active"`
	LastState       AlertState `json:"last_state"`
	LastEvaluatedAt *time.Time `json:"last_evaluated_at,omitempty"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedBy       *UserID    `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	// InstanceCounts is filled by the list and get routes.
	InstanceCounts *AlertInstanceCounts `json:"instance_counts,omitempty"`
	// EvalSeq is read with the rest of the rule. An evaluation commits only
	// while the stored value still equals it.
	EvalSeq int64 `json:"-"`
}

// MaxAlertGroupBy bounds the group columns of one rule.
const MaxAlertGroupBy = 4

// AlertInstanceCounts summarises a rule's instances by state.
type AlertInstanceCounts struct {
	Firing int `json:"firing"`
	OK     int `json:"ok"`
}

// Add counts n instances in state.
func (c *AlertInstanceCounts) Add(state AlertInstanceState, n int) {
	switch state {
	case AlertInstanceStateFiring:
		c.Firing += n
	case AlertInstanceStateOK:
		c.OK += n
	}
}

// AlertInstanceState is the state of one group of a rule.
type AlertInstanceState string

const (
	AlertInstanceStateOK     AlertInstanceState = "ok"
	AlertInstanceStateFiring AlertInstanceState = "firing"
)

// AlertInstance holds the state of one group of a rule. An ungrouped rule has
// one instance with an empty GroupKey.
type AlertInstance struct {
	AlertID    AlertID            `json:"alert_id"`
	GroupKey   string             `json:"group_key"`
	Group      map[string]string  `json:"group"`
	State      AlertInstanceState `json:"state"`
	Value      *float64           `json:"value,omitempty"`
	StateSince time.Time          `json:"state_since"`
	LastSeenAt *time.Time         `json:"last_seen_at,omitempty"`
	HistoryID  *int64             `json:"history_id,omitempty"`
	// Notified reports that the firing notification of the open episode is
	// settled: delivered, or out of delivery attempts.
	Notified bool `json:"-"`
}

// AlertHistoryEntry captures individual trigger or resolution events for an alert.
type AlertHistoryEntry struct {
	ID      int64   `json:"id"`
	AlertID AlertID `json:"alert_id"`
	// GroupKey and Group name the group of the episode. Both are empty for an
	// ungrouped rule and for evaluation errors.
	GroupKey    string            `json:"group_key"`
	Group       map[string]string `json:"group,omitempty"`
	Status      AlertStatus       `json:"status"`
	TriggeredAt time.Time         `json:"triggered_at"`
	ResolvedAt  *time.Time        `json:"resolved_at,omitempty"`
	Value       *float64          `json:"value,omitempty"`
	Message     string            `json:"message,omitempty"`
	Payload     map[string]any    `json:"payload,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// CreateAlertRequest defines the payload required to create a new alert rule.
type CreateAlertRequest struct {
	SourceID          SourceID               `json:"source_id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	QueryLanguage     QueryLanguage          `json:"query_language,omitempty"`
	EditorMode        AlertEditorMode        `json:"editor_mode,omitempty"`
	Query             string                 `json:"query"`
	ConditionJSON     string                 `json:"condition_json"`
	LookbackSeconds   int                    `json:"lookback_seconds"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
	FrequencySeconds  int                    `json:"frequency_seconds"`
	Severity          AlertSeverity          `json:"severity"`
	Labels            map[string]string      `json:"labels"`
	Annotations       map[string]string      `json:"annotations"`
	DestinationIDs    []DestinationID        `json:"destination_ids"`
	GeneratorURL      string                 `json:"generator_url"`
	GroupBy           []string               `json:"group_by"`
	IsActive          bool                   `json:"is_active"`
}

// UpdateAlertRequest defines updatable fields for an alert rule.
type UpdateAlertRequest struct {
	Name              *string                 `json:"name"`
	Description       *string                 `json:"description"`
	QueryLanguage     *QueryLanguage          `json:"query_language,omitempty"`
	EditorMode        *AlertEditorMode        `json:"editor_mode,omitempty"`
	Query             *string                 `json:"query"`
	ConditionJSON     *string                 `json:"condition_json"`
	LookbackSeconds   *int                    `json:"lookback_seconds"`
	ThresholdOperator *AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    *float64                `json:"threshold_value"`
	FrequencySeconds  *int                    `json:"frequency_seconds"`
	Severity          *AlertSeverity          `json:"severity"`
	Labels            *map[string]string      `json:"labels"`
	Annotations       *map[string]string      `json:"annotations"`
	DestinationIDs    *[]DestinationID        `json:"destination_ids"`
	GeneratorURL      *string                 `json:"generator_url"`
	GroupBy           *[]string               `json:"group_by"`
	IsActive          *bool                   `json:"is_active"`
}

// ResolveAlertRequest allows callers to provide context when manually resolving an alert.
type ResolveAlertRequest struct {
	Message string `json:"message"`
	// GroupKey resolves one group. Nil resolves every group of the rule.
	GroupKey *string `json:"group_key"`
}

// TestAlertQueryRequest allows testing an alert query before saving.
type TestAlertQueryRequest struct {
	QueryLanguage     QueryLanguage          `json:"query_language,omitempty"`
	EditorMode        AlertEditorMode        `json:"editor_mode,omitempty"`
	Query             string                 `json:"query"`
	ConditionJSON     string                 `json:"condition_json"`
	LookbackSeconds   int                    `json:"lookback_seconds"`
	ThresholdOperator AlertThresholdOperator `json:"threshold_operator"`
	ThresholdValue    float64                `json:"threshold_value"`
	GroupBy           []string               `json:"group_by"`
}

// MaxTestAlertSamples bounds the samples a test response lists.
const MaxTestAlertSamples = 20

// AlertSample is the value one group read in a test evaluation.
type AlertSample struct {
	Group        map[string]string `json:"group"`
	Value        float64           `json:"value"`
	ThresholdMet bool              `json:"threshold_met"`
}

// TestAlertQueryResponse returns the result of a test query execution with performance metrics.
// For a grouped rule, Value is the first sample's and ThresholdMet reports
// that at least one group meets the threshold.
type TestAlertQueryResponse struct {
	Value           float64       `json:"value"`
	ThresholdMet    bool          `json:"threshold_met"`
	ExecutionTimeMs int64         `json:"execution_time_ms"`
	RowsReturned    int           `json:"rows_returned"`
	Warnings        []string      `json:"warnings"`
	Samples         []AlertSample `json:"samples"`
	GroupsTotal     int           `json:"groups_total"`
}

// DefaultAlertHistoryLimit controls the number of history entries returned when unspecified.
const DefaultAlertHistoryLimit = 100
