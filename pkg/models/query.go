package models

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
