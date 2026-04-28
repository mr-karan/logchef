package models

import (
	"encoding/json"
	"time"
)

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

// TemplateVariable represents a variable for SQL template substitution.
// Variables in the SQL query (e.g., {{from_date}}) will be replaced with their values.
type TemplateVariable struct {
	Name  string      `json:"name"`  // Variable name (without braces)
	Type  string      `json:"type"`  // "string", "text", "number", or "date"
	Value interface{} `json:"value"` // The value to substitute
}

// APIQueryRequest represents the request payload for the standard log querying endpoint.
type APIQueryRequest struct {
	Limit  int    `json:"limit"`
	RawSQL string `json:"raw_sql"`
	// Variables for template substitution in the SQL query.
	// Example: {"name": "from_date", "type": "date", "value": "2026-01-01T00:00:00Z"}
	Variables []TemplateVariable `json:"variables,omitempty"`
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
	// Variables for template substitution in the SQL query.
	Variables []TemplateVariable `json:"variables,omitempty"`
	// Query execution timeout in seconds. If not specified, uses default timeout.
	QueryTimeout *int `json:"query_timeout,omitempty"`
}

// LogQueryResult represents the result of a log query
type LogQueryResult struct {
	Data     []map[string]interface{} `json:"data"`
	Stats    QueryStats               `json:"stats"`
	Columns  []ColumnInfo             `json:"columns"`
	Warnings []QueryWarning           `json:"warnings,omitempty"`
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
// Either Relative OR Absolute should be set, not both.
// If Relative is set, it takes precedence (e.g., "15m", "1h", "24h", "7d")
type SavedQueryTimeRange struct {
	Relative string `json:"relative,omitempty"` // Relative time string like "15m", "1h", "7d"
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

type SavedQueryVariableOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

type SavedQueryVariable struct {
	Name         string                     `json:"name"`
	Type         string                     `json:"type"`
	Label        string                     `json:"label"`
	InputType    string                     `json:"inputType"`
	Value        interface{}                `json:"value"`
	DefaultValue interface{}                `json:"defaultValue,omitempty"`
	IsOptional   bool                       `json:"isOptional,omitempty"`
	IsRequired   bool                       `json:"isRequired,omitempty"`
	Options      []SavedQueryVariableOption `json:"options,omitempty"`
}

type SavedQueryContent struct {
	Version   int                  `json:"version"`
	SourceID  SourceID             `json:"sourceId"`
	TimeRange SavedQueryTimeRange  `json:"timeRange"`
	Limit     int                  `json:"limit"`
	Content   string               `json:"content"`
	Variables []SavedQueryVariable `json:"variables"`
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
	IsBookmarked bool           `json:"is_bookmarked" db:"is_bookmarked"`
	CreatedAt    time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at" db:"updated_at"`
	TeamName     string         `json:"team_name,omitempty"`
	SourceName   string         `json:"source_name,omitempty"`
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

// QueryShare stores an ad hoc query payload behind a short token.
type QueryShare struct {
	Token          string          `json:"token" db:"token"`
	TeamID         TeamID          `json:"team_id" db:"team_id"`
	SourceID       SourceID        `json:"source_id" db:"source_id"`
	CreatedBy      UserID          `json:"created_by" db:"created_by"`
	Payload        json.RawMessage `json:"payload" db:"payload_json"`
	ExpiresAt      time.Time       `json:"expires_at" db:"expires_at"`
	LastAccessedAt *time.Time      `json:"last_accessed_at,omitempty" db:"last_accessed_at"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	CreatedByEmail string          `json:"created_by_email,omitempty"`
	CreatedByName  string          `json:"created_by_name,omitempty"`
}

// QuerySharePayload is the client-owned, durable state for a shared ad hoc query.
type QuerySharePayload struct {
	Version   int                  `json:"version"`
	Mode      string               `json:"mode"`
	Query     string               `json:"query"`
	Limit     int                  `json:"limit"`
	TimeRange SavedQueryTimeRange  `json:"time_range"`
	Timezone  string               `json:"timezone,omitempty"`
	Variables []SavedQueryVariable `json:"variables,omitempty"`
}

// CreateQueryShareRequest creates a short share token for a query payload.
type CreateQueryShareRequest struct {
	Payload          json.RawMessage `json:"payload"`
	ExpiresInSeconds int             `json:"expires_in_seconds,omitempty"`
}

// QueryShareResponse is returned for create and read operations.
type QueryShareResponse struct {
	Token     string          `json:"token"`
	ShareURL  string          `json:"share_url,omitempty"`
	TeamID    TeamID          `json:"team_id"`
	SourceID  SourceID        `json:"source_id"`
	Payload   json.RawMessage `json:"payload"`
	ExpiresAt time.Time       `json:"expires_at"`
	CreatedAt time.Time       `json:"created_at"`
	CreatedBy UserID          `json:"created_by"`
}

type ExportJobStatus string

const (
	ExportJobStatusPending  ExportJobStatus = "pending"
	ExportJobStatusRunning  ExportJobStatus = "running"
	ExportJobStatusComplete ExportJobStatus = "complete"
	ExportJobStatusFailed   ExportJobStatus = "failed"
)

// CreateExportJobRequest creates an async export job that produces a completed artifact.
type CreateExportJobRequest struct {
	RawSQL       string             `json:"raw_sql"`
	Format       string             `json:"format"`
	Limit        int                `json:"limit,omitempty"`
	QueryTimeout *int               `json:"query_timeout,omitempty"`
	Variables    []TemplateVariable `json:"variables,omitempty"`
}

// ExportJob stores an async export request and its eventual artifact metadata.
type ExportJob struct {
	ID             string          `json:"id" db:"id"`
	TeamID         TeamID          `json:"team_id" db:"team_id"`
	SourceID       SourceID        `json:"source_id" db:"source_id"`
	CreatedBy      UserID          `json:"created_by" db:"created_by"`
	Status         ExportJobStatus `json:"status" db:"status"`
	Format         string          `json:"format" db:"format"`
	RequestPayload json.RawMessage `json:"request_payload" db:"request_json"`
	FileName       string          `json:"file_name,omitempty" db:"file_name"`
	FilePath       string          `json:"-" db:"file_path"`
	ErrorMessage   string          `json:"error_message,omitempty" db:"error_message"`
	RowsExported   int             `json:"rows_exported,omitempty" db:"rows_exported"`
	BytesWritten   int64           `json:"bytes_written,omitempty" db:"bytes_written"`
	ExpiresAt      time.Time       `json:"expires_at" db:"expires_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" db:"updated_at"`
}

// ExportJobResponse is returned for create/status operations.
type ExportJobResponse struct {
	ID           string          `json:"id"`
	Status       ExportJobStatus `json:"status"`
	Format       string          `json:"format"`
	FileName     string          `json:"file_name,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	RowsExported int             `json:"rows_exported,omitempty"`
	BytesWritten int64           `json:"bytes_written,omitempty"`
	ExpiresAt    time.Time       `json:"expires_at"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	StatusURL    string          `json:"status_url,omitempty"`
	DownloadURL  string          `json:"download_url,omitempty"`
}
