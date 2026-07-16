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
	Name  string `json:"name"`  // Variable name (without braces)
	Type  string `json:"type"`  // "string", "text", "number", or "date"
	Value any    `json:"value"` // The value to substitute
}

// APIQueryRequest represents the request payload for the standard log querying endpoint.
type APIQueryRequest struct {
	Limit     int    `json:"limit"`
	QueryText string `json:"query_text"`
	// Optional ISO8601/RFC3339 time range for datasource-native query execution.
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	// Variables for template substitution in the query text.
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
	QueryText      string `json:"query_text"`                // Contains non-time filters
	Window         string `json:"window,omitempty"`          // For histogram queries: time window size like "1m", "5m", "1h"
	GroupBy        string `json:"group_by,omitempty"`        // For histogram queries: field to group by
	Timezone       string `json:"timezone,omitempty"`        // Kept for histogram, optional otherwise
	// Variables for template substitution in the query text.
	Variables []TemplateVariable `json:"variables,omitempty"`
	// Query execution timeout in seconds. If not specified, uses default timeout.
	QueryTimeout *int `json:"query_timeout,omitempty"`
}

// LogQueryResult represents the result of a log query
type LogQueryResult struct {
	Data     []map[string]any `json:"data"`
	Stats    QueryStats       `json:"stats"`
	Columns  []ColumnInfo     `json:"columns"`
	Warnings []QueryWarning   `json:"warnings,omitempty"`
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
	TargetTimestamp int64            `json:"target_timestamp"`
	BeforeLogs      []map[string]any `json:"before_logs"`
	TargetLogs      []map[string]any `json:"target_logs"` // Multiple logs might have the same timestamp
	AfterLogs       []map[string]any `json:"after_logs"`
	Stats           QueryStats       `json:"stats"`
}

// SavedQueryTab represents the active tab in the explorer
type SavedQueryTab string

const (
	// SavedQueryTabFilters represents the filters tab
	SavedQueryTabFilters SavedQueryTab = "filters"

	// SavedQueryTabNativeQuery represents the datasource-native query tab.
	SavedQueryTabNativeQuery SavedQueryTab = "native_query"
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

func DefaultSavedQueryEditorModeForLanguage(language QueryLanguage) SavedQueryEditorMode {
	if NormalizeQueryLanguage(language) == QueryLanguageLogchefQL {
		return SavedQueryEditorModeBuilder
	}
	return SavedQueryEditorModeNative
}

func ResolveSavedQueryMetadata(language QueryLanguage, mode SavedQueryEditorMode) (QueryLanguage, SavedQueryEditorMode, error) {
	normalizedLanguage := NormalizeQueryLanguage(language)
	normalizedMode := NormalizeSavedQueryEditorMode(mode)

	if normalizedLanguage == "" {
		switch normalizedMode {
		case SavedQueryEditorModeBuilder:
			normalizedLanguage = QueryLanguageLogchefQL
		default:
			return "", "", ErrInvalidSavedQueryConfiguration{Message: "query_language is required"}
		}
	}

	if !normalizedLanguage.Valid() {
		return "", "", ErrInvalidSavedQueryConfiguration{Message: "invalid saved query language"}
	}

	if normalizedMode == "" {
		normalizedMode = DefaultSavedQueryEditorModeForLanguage(normalizedLanguage)
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
	Value        any                        `json:"value"`
	DefaultValue any                        `json:"defaultValue,omitempty"`
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

// SavedQuery represents a cross-team saved query bound to a single source.
// Visibility is "any user with access to the source via any team they belong to".
// Edit access is "creator + global admin"; rows with NULL CreatedBy are legacy
// queries that pre-date created_by tracking and can only be edited by global admins.
//
// The legacy is_bookmarked flag is gone; users curate queries via Collections
// (each user has an auto-created personal collection that takes the role
// bookmarks used to play).
type SavedQuery struct {
	ID                int                  `json:"id" db:"id"`
	SourceID          SourceID             `json:"source_id" db:"source_id"`
	CreatedFromTeamID *TeamID              `json:"created_from_team_id,omitempty" db:"created_from_team_id"`
	Name              string               `json:"name" db:"name"`
	Description       string               `json:"description" db:"description"`
	QueryLanguage     QueryLanguage        `json:"query_language" db:"query_language"`
	EditorMode        SavedQueryEditorMode `json:"editor_mode" db:"editor_mode"`
	QueryContent      string               `json:"query_content" db:"query_content"` // JSON string of SavedQueryContent
	CreatedBy         *UserID              `json:"created_by,omitempty" db:"created_by"`
	CreatedAt         time.Time            `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at" db:"updated_at"`
	SourceName        string               `json:"source_name,omitempty"`
	// CreatedByName / CreatedByEmail identify the query's creator for display.
	// Populated where the server joins the users table (e.g. collection items);
	// empty for legacy queries with a NULL created_by.
	CreatedByName  string `json:"created_by_name,omitempty" db:"-"`
	CreatedByEmail string `json:"created_by_email,omitempty" db:"-"`
	// CanEdit / CanDelete are per-request UI authorization hints for the calling
	// user. Populated by the server (nil when not computed). CanEdit reflects
	// delegated collection-editor access; CanDelete is creator/global-admin only.
	CanEdit   *bool `json:"can_edit,omitempty" db:"-"`
	CanDelete *bool `json:"can_delete,omitempty" db:"-"`
	// Runnable indicates the calling user has source access to actually run this
	// query. Populated on browse lists (esp. the admin "all queries" surface,
	// where rows for sources the admin can't reach are shown locked). nil when
	// not computed.
	Runnable *bool `json:"runnable,omitempty" db:"-"`
}

// ResolvedSavedQuery is the explorer-facing representation of a saved query.
// Saved queries are source-scoped, but query execution is still routed through
// a team/source URL, so the resolver supplies a team that links the user to the
// query's source.
type ResolvedSavedQuery struct {
	SavedQuery
	ResolvedTeamID TeamID `json:"resolved_team_id"`
}

// CreateSavedQueryRequest is the JSON body for POST /api/v1/saved-queries.
type CreateSavedQueryRequest struct {
	Name              string               `json:"name" validate:"required"`
	Description       string               `json:"description"`
	SourceID          SourceID             `json:"source_id" validate:"required"`
	CreatedFromTeamID *TeamID              `json:"created_from_team_id,omitempty"`
	QueryLanguage     QueryLanguage        `json:"query_language,omitempty"`
	EditorMode        SavedQueryEditorMode `json:"editor_mode,omitempty"`
	QueryContent      string               `json:"query_content" validate:"required"`
}

// UpdateSavedQueryRequest is the JSON body for PUT /api/v1/saved-queries/:id.
// SourceID is intentionally not updatable — re-targeting a query to a new source
// is equivalent to creating a new one.
type UpdateSavedQueryRequest struct {
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	QueryLanguage QueryLanguage        `json:"query_language,omitempty"`
	EditorMode    SavedQueryEditorMode `json:"editor_mode,omitempty"`
	QueryContent  string               `json:"query_content"`
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
	SourceID       SourceID        `json:"source_id" db:"source_id"`
	TeamID         *TeamID         `json:"team_id,omitempty" db:"team_id"`
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
	SourceID  SourceID        `json:"source_id"`
	TeamID    *TeamID         `json:"team_id,omitempty"`
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
	// RawSQL is the query to export. QueryText is accepted as an alias so both
	// the web UI (which posts query_text, like the query/histogram endpoints)
	// and the CLI (which posts raw_sql) work; whichever is non-empty is used.
	RawSQL       string             `json:"raw_sql"`
	QueryText    string             `json:"query_text"`
	Format       string             `json:"format"`
	Limit        int                `json:"limit,omitempty"`
	QueryTimeout *int               `json:"query_timeout,omitempty"`
	Variables    []TemplateVariable `json:"variables,omitempty"`
}

// ExportJob stores an async export request and its eventual artifact metadata.
type ExportJob struct {
	ID             string          `json:"id" db:"id"`
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
