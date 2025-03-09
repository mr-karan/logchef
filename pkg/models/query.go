package models

import "time"

// FilterOperator represents a filter operation
type FilterOperator string

const (
	// FilterOperatorEquals represents an equality comparison
	FilterOperatorEquals FilterOperator = "="

	// FilterOperatorNotEquals represents an inequality comparison
	FilterOperatorNotEquals FilterOperator = "!="

	// FilterOperatorContains represents a case-sensitive LIKE operation
	FilterOperatorContains FilterOperator = "contains"

	// FilterOperatorNotContains represents a case-sensitive NOT LIKE operation
	FilterOperatorNotContains FilterOperator = "not_contains"

	// FilterOperatorIContains represents a case-insensitive LIKE operation
	FilterOperatorIContains FilterOperator = "icontains"

	// FilterOperatorStartsWith represents a prefix match operation
	FilterOperatorStartsWith FilterOperator = "startswith"

	// FilterOperatorEndsWith represents a suffix match operation
	FilterOperatorEndsWith FilterOperator = "endswith"

	// FilterOperatorIn represents an IN operation
	FilterOperatorIn FilterOperator = "in"

	// FilterOperatorNotIn represents a NOT IN operation
	FilterOperatorNotIn FilterOperator = "not_in"

	// FilterOperatorIsNull represents an IS NULL check
	FilterOperatorIsNull FilterOperator = "is_null"

	// FilterOperatorIsNotNull represents an IS NOT NULL check
	FilterOperatorIsNotNull FilterOperator = "is_not_null"
)

// SortOrder represents sort direction
type SortOrder string

const (
	// SortOrderAsc represents ascending sort order
	SortOrderAsc SortOrder = "ASC"

	// SortOrderDesc represents descending sort order
	SortOrderDesc SortOrder = "DESC"
)

// GroupOperator represents a logical operator for combining filter groups
type GroupOperator string

const (
	// GroupOperatorAnd represents a logical AND operation
	GroupOperatorAnd GroupOperator = "AND"

	// GroupOperatorOr represents a logical OR operation
	GroupOperatorOr GroupOperator = "OR"
)

// FilterCondition represents a single filter condition
type FilterCondition struct {
	Field    string         `json:"field"`
	Operator FilterOperator `json:"operator"`
	Value    string         `json:"value"` // Changed from interface{} for better type safety
}

// SortOptions represents sorting configuration
type SortOptions struct {
	Field string    `json:"field"`
	Order SortOrder `json:"order"`
}

// LogQueryRequest represents the request for querying logs
type LogQueryRequest struct {
	StartTimestamp int64        `json:"start_timestamp"`
	EndTimestamp   int64        `json:"end_timestamp"`
	Limit          int          `json:"limit"`
	RawSQL         string       `json:"raw_sql"`
	Sort           *SortOptions `json:"sort,omitempty"`
}

// LogQueryResult represents the result of a log query
type LogQueryResult struct {
	Data    []map[string]interface{} `json:"data"`
	Stats   QueryStats               `json:"stats"`
	Columns []ColumnInfo             `json:"columns"`
}

// LogContextRequest represents a request to get temporal context around a log
type LogContextRequest struct {
	SourceID    SourceID `json:"source_id"`
	Timestamp   int64    `json:"timestamp"`    // Target timestamp in milliseconds
	BeforeLimit int      `json:"before_limit"` // Optional, defaults to 5
	AfterLimit  int      `json:"after_limit"`  // Optional, defaults to 5
}

// LogContextResponse represents temporal context query results
type LogContextResponse struct {
	TargetTimestamp int64                    `json:"target_timestamp"`
	BeforeLogs      []map[string]interface{} `json:"before_logs"`
	TargetLogs      []map[string]interface{} `json:"target_logs"` // Multiple logs might have the same timestamp
	AfterLogs       []map[string]interface{} `json:"after_logs"`
	Stats           QueryStats               `json:"stats"`
}

// SavedQueryTab represents the active tab in the explorer
type SavedQueryTab string

const (
	// SavedQueryTabFilters represents the filters tab
	SavedQueryTabFilters SavedQueryTab = "filters"

	// SavedQueryTabRawSQL represents the raw SQL tab
	SavedQueryTabRawSQL SavedQueryTab = "raw_sql"
)

// SavedQueryTimeRange represents a time range for a saved query
type SavedQueryTimeRange struct {
	Absolute struct {
		Start int64 `json:"start"` // Unix timestamp in milliseconds
		End   int64 `json:"end"`   // Unix timestamp in milliseconds
	} `json:"absolute"`
}

// SavedQueryContent represents the content of a saved query
type SavedQueryContent struct {
	Version   int                 `json:"version"`
	ActiveTab SavedQueryTab       `json:"activeTab"`
	SourceID  SourceID            `json:"sourceId"`
	TimeRange SavedQueryTimeRange `json:"timeRange"`
	Limit     int                 `json:"limit"`
	RawSQL    string              `json:"rawSql"`
}

// SavedTeamQuery represents a saved query associated with a team
type SavedTeamQuery struct {
	ID           int       `json:"id" db:"id"`
	TeamID       TeamID    `json:"team_id" db:"team_id"`
	SourceID     SourceID  `json:"source_id" db:"source_id"`
	Name         string    `json:"name" db:"name"`
	Description  string    `json:"description" db:"description"`
	QueryContent string    `json:"query_content" db:"query_content"` // JSON string of SavedQueryContent
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// CreateTeamQueryRequest represents a request to create a team query
type CreateTeamQueryRequest struct {
	Name         string   `json:"name" validate:"required"`
	Description  string   `json:"description"`
	SourceID     SourceID `json:"source_id" validate:"required"`
	QueryContent string   `json:"query_content" validate:"required"`
}

// UpdateTeamQueryRequest represents a request to update a team query
type UpdateTeamQueryRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SourceID     SourceID `json:"source_id"`
	QueryContent string   `json:"query_content"`
}

// SavedQuery represents a generic saved query
type SavedQuery struct {
	ID          int       `json:"id" db:"id"`
	TeamID      string    `json:"team_id" db:"team_id"`
	SourceID    string    `json:"source_id" db:"source_id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	QuerySQL    string    `json:"query_sql" db:"query_sql"`
	CreatedBy   UserID    `json:"created_by" db:"created_by"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}
