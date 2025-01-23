package models

// FilterOperator represents a filter operation
type FilterOperator string

const (
	FilterOperatorEquals        FilterOperator = "="
	FilterOperatorNotEquals     FilterOperator = "!="
	FilterOperatorContains      FilterOperator = "CONTAINS"
	FilterOperatorNotContains   FilterOperator = "NOT_CONTAINS"
	FilterOperatorIn            FilterOperator = "IN"
	FilterOperatorNotIn         FilterOperator = "NOT_IN"
	FilterOperatorGreaterThan   FilterOperator = ">"
	FilterOperatorLessThan      FilterOperator = "<"
	FilterOperatorGreaterEquals FilterOperator = ">="
	FilterOperatorLessEquals    FilterOperator = "<="
)

// SortOrder represents sort direction
type SortOrder string

const (
	SortOrderAsc  SortOrder = "ASC"
	SortOrderDesc SortOrder = "DESC"
)

// GroupOperator represents a group operator
type GroupOperator string

const (
	GroupOperatorAnd GroupOperator = "AND"
	GroupOperatorOr  GroupOperator = "OR"
)

// FilterCondition represents a single filter condition
type FilterCondition struct {
	Field    string         `json:"field"`
	Operator FilterOperator `json:"operator"`
	Value    string         `json:"value"`
}

// FilterGroup represents a group of filter conditions with a logical operator
type FilterGroup struct {
	Operator   GroupOperator     `json:"operator"`
	Conditions []FilterCondition `json:"conditions"`
}

// SortOptions represents sorting configuration
type SortOptions struct {
	Field string    `json:"field"`
	Order SortOrder `json:"order"`
}

// QueryMode represents different query modes
type QueryMode string

const (
	QueryModeFilters   QueryMode = "filters"
	QueryModeRawSQL    QueryMode = "raw_sql"
	QueryModeLogChefQL QueryMode = "logchefql"
)

// LogQueryRequest represents the request for querying logs
type LogQueryRequest struct {
	StartTimestamp int64         `json:"start_timestamp"`
	EndTimestamp   int64         `json:"end_timestamp"`
	Limit          int           `json:"limit"`
	FilterGroups   []FilterGroup `json:"filterGroups,omitempty"`
	Sort           *SortOptions  `json:"sort,omitempty"`
	Mode           QueryMode     `json:"mode,omitempty"`
	RawSQL         string        `json:"raw_sql,omitempty"`
	LogChefQL      string        `json:"logchefql,omitempty"`
}

// LogQueryResponse represents the response for querying logs
type LogQueryResponse struct {
	Data  []map[string]interface{} `json:"data"`
	Stats map[string]interface{}   `json:"stats"`
}
