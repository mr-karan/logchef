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

type ErrInvalidSavedQueryConfiguration struct {
	Message string
}

func (e ErrInvalidSavedQueryConfiguration) Error() string {
	return e.Message
}

// QueryLanguage captures the executable language of a query across datasources.
type QueryLanguage string

const (
	QueryLanguageLogchefQL     QueryLanguage = "logchefql"
	QueryLanguageClickHouseSQL QueryLanguage = "clickhouse-sql"
	QueryLanguageLogsQL        QueryLanguage = "logsql"
)

func NormalizeQueryLanguage(language QueryLanguage) QueryLanguage {
	switch QueryLanguage(string(language)) {
	case QueryLanguage("sql"):
		return QueryLanguageClickHouseSQL
	case QueryLanguageLogchefQL:
		return QueryLanguageLogchefQL
	case QueryLanguageClickHouseSQL:
		return QueryLanguageClickHouseSQL
	case QueryLanguageLogsQL:
		return QueryLanguageLogsQL
	default:
		return language
	}
}

func (l QueryLanguage) Valid() bool {
	switch NormalizeQueryLanguage(l) {
	case QueryLanguageLogchefQL, QueryLanguageClickHouseSQL, QueryLanguageLogsQL:
		return true
	default:
		return false
	}
}

func (l QueryLanguage) String() string {
	return string(NormalizeQueryLanguage(l))
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
	// Optional ISO8601/RFC3339 time range for datasource-native query execution.
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
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
	StartTime      string `json:"start_time,omitempty"`      // ISO8601/RFC3339 time range start
	EndTime        string `json:"end_time,omitempty"`        // ISO8601/RFC3339 time range end
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
	Data    []map[string]interface{} `json:"data"`
	Stats   QueryStats               `json:"stats"`
	Columns []ColumnInfo             `json:"columns"`
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
// Deprecated: use QueryLanguage + SavedQueryEditorMode for new logic.
type SavedQueryType string

const (
	// SavedQueryTypeLogchefQL represents a query saved in LogchefQL format
	SavedQueryTypeLogchefQL SavedQueryType = "logchefql"

	// SavedQueryTypeSQL represents a query saved in SQL format
	SavedQueryTypeSQL SavedQueryType = "sql"
)

// SavedQueryEditorMode captures the UI/editor that authored the saved query.
type SavedQueryEditorMode string

const (
	SavedQueryEditorModeBuilder SavedQueryEditorMode = "builder"
	SavedQueryEditorModeNative  SavedQueryEditorMode = "native"
)

func NormalizeSavedQueryEditorMode(mode SavedQueryEditorMode) SavedQueryEditorMode {
	switch mode {
	case SavedQueryEditorModeBuilder:
		return SavedQueryEditorModeBuilder
	case SavedQueryEditorModeNative:
		return SavedQueryEditorModeNative
	default:
		return mode
	}
}

func (m SavedQueryEditorMode) Valid() bool {
	switch NormalizeSavedQueryEditorMode(m) {
	case SavedQueryEditorModeBuilder, SavedQueryEditorModeNative:
		return true
	default:
		return false
	}
}

func LegacySavedQueryTypeFromLanguage(language QueryLanguage) SavedQueryType {
	if NormalizeQueryLanguage(language) == QueryLanguageLogchefQL {
		return SavedQueryTypeLogchefQL
	}
	return SavedQueryTypeSQL
}

func DefaultSavedQueryEditorModeForLanguage(language QueryLanguage) SavedQueryEditorMode {
	if NormalizeQueryLanguage(language) == QueryLanguageLogchefQL {
		return SavedQueryEditorModeBuilder
	}
	return SavedQueryEditorModeNative
}

func ResolveSavedQueryMetadata(queryType SavedQueryType, language QueryLanguage, mode SavedQueryEditorMode) (QueryLanguage, SavedQueryEditorMode, error) {
	normalizedType := queryType
	normalizedLanguage := NormalizeQueryLanguage(language)
	normalizedMode := NormalizeSavedQueryEditorMode(mode)

	if normalizedLanguage == "" {
		switch normalizedType {
		case SavedQueryTypeLogchefQL:
			normalizedLanguage = QueryLanguageLogchefQL
		case "", SavedQueryTypeSQL:
			normalizedLanguage = QueryLanguageClickHouseSQL
		default:
			return "", "", ErrInvalidSavedQueryConfiguration{Message: "invalid saved query type"}
		}
	}

	if !normalizedLanguage.Valid() {
		return "", "", ErrInvalidSavedQueryConfiguration{Message: "invalid saved query language"}
	}

	if normalizedMode == "" {
		if normalizedType != "" {
			switch normalizedType {
			case SavedQueryTypeLogchefQL:
				normalizedMode = SavedQueryEditorModeBuilder
			case SavedQueryTypeSQL:
				normalizedMode = SavedQueryEditorModeNative
			default:
				return "", "", ErrInvalidSavedQueryConfiguration{Message: "invalid saved query type"}
			}
		} else {
			normalizedMode = DefaultSavedQueryEditorModeForLanguage(normalizedLanguage)
		}
	}

	if !normalizedMode.Valid() {
		return "", "", ErrInvalidSavedQueryConfiguration{Message: "invalid saved query editor mode"}
	}

	if normalizedMode == SavedQueryEditorModeBuilder && normalizedLanguage != QueryLanguageLogchefQL {
		return "", "", ErrInvalidSavedQueryConfiguration{Message: "builder mode requires LogchefQL"}
	}
	if normalizedMode == SavedQueryEditorModeNative && normalizedLanguage == QueryLanguageLogchefQL {
		return "", "", ErrInvalidSavedQueryConfiguration{Message: "native mode requires a datasource-native query language"}
	}

	if normalizedType != "" && normalizedType != LegacySavedQueryTypeFromLanguage(normalizedLanguage) {
		return "", "", ErrInvalidSavedQueryConfiguration{Message: "query_type does not match query_language"}
	}

	return normalizedLanguage, normalizedMode, nil
}

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
	ID            int                  `json:"id" db:"id"`
	TeamID        TeamID               `json:"team_id" db:"team_id"`
	SourceID      SourceID             `json:"source_id" db:"source_id"`
	Name          string               `json:"name" db:"name"`
	Description   string               `json:"description" db:"description"`
	QueryType     SavedQueryType       `json:"query_type" db:"query_type"`
	QueryLanguage QueryLanguage        `json:"query_language" db:"query_language"`
	EditorMode    SavedQueryEditorMode `json:"editor_mode" db:"editor_mode"`
	QueryContent  string               `json:"query_content" db:"query_content"` // JSON string of SavedQueryContent
	IsBookmarked  bool                 `json:"is_bookmarked" db:"is_bookmarked"`
	CreatedAt     time.Time            `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at" db:"updated_at"`
	TeamName      string               `json:"team_name,omitempty"`
	SourceName    string               `json:"source_name,omitempty"`
}

// CreateTeamQueryRequest represents a request to create a team query
type CreateTeamQueryRequest struct {
	Name          string               `json:"name" validate:"required"`
	Description   string               `json:"description"`
	SourceID      SourceID             `json:"source_id" validate:"required"`
	QueryType     SavedQueryType       `json:"query_type,omitempty"`
	QueryLanguage QueryLanguage        `json:"query_language,omitempty"`
	EditorMode    SavedQueryEditorMode `json:"editor_mode,omitempty"`
	QueryContent  string               `json:"query_content" validate:"required"`
}

// UpdateTeamQueryRequest represents a request to update a team query
type UpdateTeamQueryRequest struct {
	Name          *string               `json:"name,omitempty"`
	Description   *string               `json:"description,omitempty"`
	SourceID      *SourceID             `json:"source_id,omitempty"`
	QueryType     *SavedQueryType       `json:"query_type,omitempty"`
	QueryLanguage *QueryLanguage        `json:"query_language,omitempty"`
	EditorMode    *SavedQueryEditorMode `json:"editor_mode,omitempty"`
	QueryContent  *string               `json:"query_content,omitempty"`
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
