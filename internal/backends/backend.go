// Package backends provides a unified interface for log storage backends.
// It abstracts the differences between ClickHouse, VictoriaLogs, and potentially
// other logging backends, allowing the core application logic to work with
// any supported backend transparently.
package backends

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// BackendType represents the type of log storage backend.
type BackendType string

const (
	// BackendTypeClickHouse represents a ClickHouse backend.
	BackendTypeClickHouse BackendType = "clickhouse"
	// BackendTypeVictoriaLogs represents a VictoriaLogs backend.
	BackendTypeVictoriaLogs BackendType = "victorialogs"
)

// String returns the string representation of the backend type.
func (b BackendType) String() string {
	return string(b)
}

// IsValid checks if the backend type is a recognized type.
func (b BackendType) IsValid() bool {
	switch b {
	case BackendTypeClickHouse, BackendTypeVictoriaLogs:
		return true
	default:
		return false
	}
}

// TimeRange represents a time range for queries.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// QueryParams holds common parameters for log queries.
type QueryParams struct {
	// Query is the raw query string (SQL for ClickHouse, LogsQL for VictoriaLogs)
	Query string
	// Limit is the maximum number of results to return
	Limit int
	// TimeoutSeconds is the query execution timeout
	TimeoutSeconds *int
}

// HistogramParams holds parameters for histogram queries.
type HistogramParams struct {
	// Query is the base query to generate histogram from
	Query string
	// Window is the time bucket size (e.g., "1m", "5m", "1h")
	Window string
	// GroupBy is an optional field to group histogram data by
	GroupBy string
	// Timezone is the timezone for time-based operations
	Timezone string
	// TimeoutSeconds is the query execution timeout
	TimeoutSeconds *int
}

// HistogramData represents a single time bucket in a histogram.
type HistogramData struct {
	// Bucket is the start time of the bucket
	Bucket time.Time `json:"bucket"`
	// LogCount is the number of logs in the bucket
	LogCount int `json:"log_count"`
	// GroupValue is the value of the group for grouped histograms
	GroupValue string `json:"group_value,omitempty"`
}

// HistogramResult holds the complete histogram data.
type HistogramResult struct {
	// Granularity is the time window used (e.g., "5m")
	Granularity string `json:"granularity"`
	// Data contains the histogram buckets
	Data []HistogramData `json:"data"`
}

// LogContextParams holds parameters for fetching logs around a specific timestamp.
type LogContextParams struct {
	// TargetTime is the timestamp to center the context around
	TargetTime time.Time
	// BeforeLimit is the number of logs to fetch before target time
	BeforeLimit int
	// AfterLimit is the number of logs to fetch after target time
	AfterLimit int
	// BeforeOffset is the offset for pagination in before query
	BeforeOffset int
	// AfterOffset is the offset for pagination in after query
	AfterOffset int
	// ExcludeBoundary when true, uses < instead of <= for before query
	ExcludeBoundary bool
}

// LogContextResult holds logs retrieved around a target timestamp.
type LogContextResult struct {
	// BeforeLogs are logs at or before the target time (chronological order)
	BeforeLogs []map[string]interface{}
	// TargetLogs are logs exactly at the target time
	TargetLogs []map[string]interface{}
	// AfterLogs are logs after the target time
	AfterLogs []map[string]interface{}
	// Stats contains query execution statistics
	Stats models.QueryStats
}

// FieldValuesParams holds parameters for fetching distinct field values.
type FieldValuesParams struct {
	// FieldName is the name of the field to get values for
	FieldName string
	// FieldType is the type of the field (for optimization hints)
	FieldType string
	// TimestampField is the name of the timestamp column
	TimestampField string
	// TimeRange is the time range to search within
	TimeRange TimeRange
	// Timezone is the timezone for time conversion
	Timezone string
	// Limit is the maximum number of values to return
	Limit int
	// TimeoutSeconds is the query timeout
	TimeoutSeconds *int
	// FilterQuery is an optional query to filter results (LogChefQL/LogsQL)
	FilterQuery string
}

// FieldValueInfo represents a distinct value with its count.
type FieldValueInfo struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// FieldValuesResult holds distinct values for a field.
type FieldValuesResult struct {
	// FieldName is the name of the field
	FieldName string `json:"field_name"`
	// FieldType is the type of the field
	FieldType string `json:"field_type"`
	// IsLowCardinality indicates if the field is low cardinality
	IsLowCardinality bool `json:"is_low_cardinality"`
	// Values contains the distinct values with counts
	Values []FieldValueInfo `json:"values"`
	// TotalDistinct is the total number of distinct values
	TotalDistinct int64 `json:"total_distinct"`
}

// TableInfo represents metadata about a log table/stream.
type TableInfo struct {
	// Database is the database/namespace name
	Database string `json:"database"`
	// Name is the table/stream name
	Name string `json:"name"`
	// Engine is the storage engine type (e.g., "MergeTree", "Distributed")
	Engine string `json:"engine,omitempty"`
	// EngineParams are engine-specific parameters
	EngineParams []string `json:"engine_params,omitempty"`
	// Columns contains column/field metadata
	Columns []models.ColumnInfo `json:"columns"`
	// SortKeys are the primary sorting columns
	SortKeys []string `json:"sort_keys,omitempty"`
	// CreateQuery is the CREATE TABLE statement (if available)
	CreateQuery string `json:"create_query,omitempty"`
}

// BackendClient is the interface for executing queries against a log backend.
// Implementations must be safe for concurrent use.
type BackendClient interface {
	// Query executes a query and returns the results.
	// For ClickHouse: raw SQL query
	// For VictoriaLogs: LogsQL query
	Query(ctx context.Context, query string, timeoutSeconds *int) (*models.QueryResult, error)

	// GetTableInfo retrieves metadata about a table/stream.
	GetTableInfo(ctx context.Context, database, table string) (*TableInfo, error)

	// GetHistogramData generates histogram data for the given query.
	GetHistogramData(ctx context.Context, tableName, timestampField string, params HistogramParams) (*HistogramResult, error)

	// GetSurroundingLogs retrieves logs around a specific timestamp (log context).
	GetSurroundingLogs(ctx context.Context, tableName, timestampField string, params LogContextParams, timeoutSeconds *int) (*LogContextResult, error)

	// GetFieldDistinctValues retrieves distinct values for a field.
	GetFieldDistinctValues(ctx context.Context, database, table string, params FieldValuesParams) (*FieldValuesResult, error)

	// GetAllFilterableFieldValues retrieves distinct values for all filterable fields.
	GetAllFilterableFieldValues(ctx context.Context, database, table string, params AllFieldValuesParams) (map[string]*FieldValuesResult, error)

	// Ping checks connectivity to the backend.
	// If database and table are provided, also verifies they exist.
	Ping(ctx context.Context, database, table string) error

	// Close releases any resources held by the client.
	Close() error

	// Reconnect attempts to re-establish the connection.
	Reconnect(ctx context.Context) error
}

// AllFieldValuesParams holds parameters for fetching values for all filterable fields.
type AllFieldValuesParams struct {
	// TimestampField is the timestamp column name for time range filter
	TimestampField string
	// TimeRange is the time range to search within
	TimeRange TimeRange
	// Timezone is the timezone for time conversion
	Timezone string
	// Limit is the max values per field
	Limit int
	// TimeoutSeconds is the query timeout
	TimeoutSeconds *int
	// FilterQuery is an optional query to filter results
	FilterQuery string
}

// BackendManager manages connections to backend clients.
// It provides connection pooling, health checking, and source lifecycle management.
type BackendManager interface {
	// GetClient returns the client for a given source ID.
	GetClient(sourceID models.SourceID) (BackendClient, error)

	// AddSource adds a new source and establishes a connection.
	AddSource(ctx context.Context, source *models.Source) error

	// RemoveSource removes a source and closes its connection.
	RemoveSource(sourceID models.SourceID) error

	// GetHealth returns the health status for a source.
	GetHealth(ctx context.Context, sourceID models.SourceID) models.SourceHealth

	// GetCachedHealth returns the cached health status (no live check).
	GetCachedHealth(sourceID models.SourceID) models.SourceHealth

	// CreateTemporaryClient creates an unmanaged client for validation purposes.
	// The caller is responsible for closing the returned client.
	CreateTemporaryClient(ctx context.Context, source *models.Source) (BackendClient, error)

	// Close closes all managed connections.
	Close() error

	// StartBackgroundHealthChecks starts periodic health checking.
	StartBackgroundHealthChecks(interval time.Duration)

	// StopBackgroundHealthChecks stops the health check goroutine.
	StopBackgroundHealthChecks()
}
