package models

import "time"

// Timeout constants for query execution
const (
	// DefaultQueryTimeoutSeconds is the default max_execution_time if not specified
	DefaultQueryTimeoutSeconds = 60
	// MaxQueryTimeoutSeconds is the maximum allowed timeout to prevent resource abuse
	MaxQueryTimeoutSeconds = 3600 // 1 hour
)

// ValidateQueryTimeout validates that a query timeout is within acceptable bounds
func ValidateQueryTimeout(timeout *int) error {
	if timeout == nil {
		return nil // No timeout specified is valid
	}
	if *timeout <= 0 {
		return ErrInvalidTimeout{Message: "Query timeout must be a positive number"}
	}
	if *timeout > MaxQueryTimeoutSeconds {
		return ErrInvalidTimeout{Message: "Query timeout cannot exceed 300 seconds"}
	}
	return nil
}

// ErrInvalidTimeout represents a query timeout validation error
type ErrInvalidTimeout struct {
	Message string
}

func (e ErrInvalidTimeout) Error() string {
	return e.Message
}

// APIQueryRequest represents the request payload for the standard log querying endpoint.
type APIQueryRequest struct {
	Limit  int    `json:"limit"`
	RawSQL string `json:"raw_sql"`
	// Query execution timeout in seconds. If not specified, uses default timeout.
	QueryTimeout *int `json:"query_timeout,omitempty"`
	// Sort and other general query params could be added here if needed later.
}

// APIHistogramRequest represents the request payload for the histogram endpoint.
type APIHistogramRequest struct {
	StartTimestamp int64  `json:"start_timestamp,omitempty"` // Legacy - Unix timestamp in milliseconds
	EndTimestamp   int64  `json:"end_timestamp,omitempty"`   // Legacy - Unix timestamp in milliseconds
	Limit          int    `json:"limit"`                     // Limit might influence histogram sampling/performance
	RawSQL         string `json:"raw_sql"`                   // Contains non-time filters
	Window         string `json:"window,omitempty"`          // For histogram queries: time window size like "1m", "5m", "1h"
	GroupBy        string `json:"group_by,omitempty"`        // For histogram queries: field to group by
	Timezone       string `json:"timezone,omitempty"`        // Kept for histogram, optional otherwise
	// Query execution timeout in seconds. If not specified, uses default timeout.
	QueryTimeout *int `json:"query_timeout,omitempty"`
}

// LogQueryResult represents the result of a log query
type LogQueryResult struct {
	Data    []map[string]interface{} `json:"data"`
	Stats   QueryStats               `json:"stats"`
	Columns []ColumnInfo             `json:"columns"`
}

// LogContextRequest represents a request to get temporal context around a log
type LogContextRequest struct {
	SourceID        SourceID `json:"source_id"`
	Timestamp       int64    `json:"timestamp"`        // Target timestamp in milliseconds
	BeforeLimit     int      `json:"before_limit"`     // Optional, defaults to 10
	AfterLimit      int      `json:"after_limit"`      // Optional, defaults to 10
	BeforeOffset    int      `json:"before_offset"`    // Offset for before query (for pagination)
	AfterOffset     int      `json:"after_offset"`     // Offset for after query (for pagination)
	ExcludeBoundary bool     `json:"exclude_boundary"` // When true, excludes logs at exact timestamp (for pagination)
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

// SavedQueryType represents the type of saved query
type SavedQueryType string

const (
	// SavedQueryTypeLogchefQL represents a query saved in LogchefQL format
	SavedQueryTypeLogchefQL SavedQueryType = "logchefql"

	// SavedQueryTypeSQL represents a query saved in SQL format
	SavedQueryTypeSQL SavedQueryType = "sql"
)

// SavedQueryContent represents the content of a saved query
type SavedQueryContent struct {
	Version   int                 `json:"version"`
	SourceID  SourceID            `json:"sourceId"`
	TimeRange SavedQueryTimeRange `json:"timeRange"`
	Limit     int                 `json:"limit"`
	Content   string              `json:"content"` // Query content (SQL or LogchefQL)
}

// SavedTeamQuery represents a saved query associated with a team
type SavedTeamQuery struct {
	ID           int            `json:"id" db:"id"`
	TeamID       TeamID         `json:"team_id" db:"team_id"`
	SourceID     SourceID       `json:"source_id" db:"source_id"`
	Name         string         `json:"name" db:"name"`
	Description  string         `json:"description" db:"description"`
	QueryType    SavedQueryType `json:"query_type" db:"query_type"`
	QueryContent string         `json:"query_content" db:"query_content"` // JSON string of SavedQueryContent
	CreatedAt    time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at" db:"updated_at"`
}

// CreateTeamQueryRequest represents a request to create a team query
type CreateTeamQueryRequest struct {
	Name         string         `json:"name" validate:"required"`
	Description  string         `json:"description"`
	SourceID     SourceID       `json:"source_id" validate:"required"`
	QueryType    SavedQueryType `json:"query_type" validate:"required"`
	QueryContent string         `json:"query_content" validate:"required"`
}

// UpdateTeamQueryRequest represents a request to update a team query
type UpdateTeamQueryRequest struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	SourceID     SourceID       `json:"source_id"`
	QueryType    SavedQueryType `json:"query_type"`
	QueryContent string         `json:"query_content"`
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

// GenerateSQLRequest defines the request body for SQL generation from natural language.
type GenerateSQLRequest struct {
	NaturalLanguageQuery string `json:"natural_language_query" validate:"required"`
	CurrentQuery         string `json:"current_query,omitempty"` // Optional current query for context
}

// GenerateSQLResponse defines the successful response for SQL generation.
type GenerateSQLResponse struct {
	SQLQuery string `json:"sql_query"`
}
