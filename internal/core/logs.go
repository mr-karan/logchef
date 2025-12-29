package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// --- Log Querying Functions ---

// QueryLogs retrieves logs from a specific source based on the provided parameters.
// Timeout is always applied - either from params or default value.
func QueryLogs(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, sourceID models.SourceID, params clickhouse.LogQueryParams) (*models.QueryResult, error) {
	// 1. Get source details from SQLite to validate existence and get table name
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		// Handle potential ErrNotFound from db layer
		return nil, fmt.Errorf("error getting source details: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound // Use the core package's error
	}

	// Ensure timeout is always set
	if params.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		params.QueryTimeout = &defaultTimeout
	}

	// 2. Get ClickHouse connection for the source
	client, err := chDB.GetConnection(sourceID)
	if err != nil {
		log.Error("failed to get clickhouse client for query", "source_id", sourceID, "error", err)
		// Consider returning a specific error indicating connection issue
		return nil, fmt.Errorf("error getting database connection for source %d: %w", sourceID, err)
	}

	// 3. Build the query (assuming LogQueryParams includes RawSQL or structured fields)
	// Use the query builder from the clickhouse package
	tableName := source.GetFullTableName() // e.g., "default.logs"
	qb := clickhouse.NewQueryBuilder(tableName)

	// TODO: Refine query building based on LogQueryParams structure
	// Example: If params.RawSQL is provided and validated:
	builtQuery, err := qb.BuildRawQuery(params.RawSQL, params.Limit)
	if err != nil {
		log.Error("failed to build raw SQL query", "source_id", sourceID, "raw_sql", params.RawSQL, "error", err)
		// Return a user-friendly error indicating invalid query syntax
		return nil, fmt.Errorf("invalid query syntax: %w", err)
	}

	// --- Alternatively, build query from structured params --- //
	// query := qb.BuildSelectQuery(params.StartTime, params.EndTime, params.Filter, params.Limit)

	// 4. Execute the query via the ClickHouse client with timeout (always applied)
	queryResult, err := client.QueryWithTimeout(ctx, builtQuery, params.QueryTimeout)
	if err != nil {
		log.Error("failed to execute clickhouse query", "source_id", sourceID, "error", err)
		// Consider parsing CH error for user-friendliness
		return nil, fmt.Errorf("error executing query on source %d: %w", sourceID, err)
	}

	return queryResult, nil
}

// GetSourceSchema retrieves the schema (column information) for a specific source from ClickHouse.
func GetSourceSchema(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, sourceID models.SourceID) ([]models.ColumnInfo, error) {
	// 1. Get source details from SQLite
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source details: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// 2. Get ClickHouse connection
	client, err := chDB.GetConnection(sourceID)
	if err != nil {
		log.Error("failed to get clickhouse client for schema retrieval", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("error getting database connection for source %d: %w", sourceID, err)
	}

	// 3. Get table schema from ClickHouse client
	// Use GetTableSchema which returns the full TableInfo
	tableInfo, err := client.GetTableInfo(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		log.Error("failed to get table schema from clickhouse", "source_id", sourceID, "database", source.Connection.Database, "table", source.Connection.TableName, "error", err)
		return nil, fmt.Errorf("error retrieving schema for source %d: %w", sourceID, err)
	}

	return tableInfo.Columns, nil
}

// --- Histogram Data Functions ---

// HistogramParams defines parameters specifically for histogram queries.
// Keeping it separate allows for specific validation or processing.
type HistogramParams struct {
	Window   string // e.g., "1m", "5m", "1h"
	Query    string // Optional filter query (WHERE clause part)
	GroupBy  string // Optional field to group by
	Timezone string // Optional timezone identifier (e.g., 'America/New_York', 'UTC')
	// Query execution timeout in seconds. If not specified, uses default timeout.
	QueryTimeout *int
}

// HistogramResponse structures the response for histogram data.
type HistogramResponse struct {
	Granularity string                     `json:"granularity"`
	Data        []clickhouse.HistogramData `json:"data"`
}

// GetHistogramData fetches histogram data for a specific source and time range.
func GetHistogramData(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, sourceID models.SourceID, params HistogramParams) (*HistogramResponse, error) {
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source details: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	if source.MetaTSField == "" {
		return nil, fmt.Errorf("source %d does not have a timestamp field configured", sourceID)
	}
	if params.Query == "" {
		return nil, fmt.Errorf("query parameter is required for histogram data")
	}

	if params.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		params.QueryTimeout = &defaultTimeout
	}

	client, err := chDB.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting database connection for source %d: %w", sourceID, err)
	}

	chWindow, err := parseTimeWindow(params.Window)
	if err != nil {
		return nil, err
	}

	chParams := clickhouse.HistogramParams{
		Window:       chWindow,
		Query:        params.Query,
		GroupBy:      params.GroupBy,
		Timezone:     params.Timezone,
		QueryTimeout: params.QueryTimeout,
	}

	histogramData, err := client.GetHistogramData(ctx, source.GetFullTableName(), source.MetaTSField, chParams)
	if err != nil {
		log.Error("failed to get histogram data from clickhouse", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("error generating histogram for source %d: %w", sourceID, err)
	}

	return &HistogramResponse{
		Granularity: histogramData.Granularity,
		Data:        histogramData.Data,
	}, nil
}

func parseTimeWindow(window string) (clickhouse.TimeWindow, error) {
	windowMap := map[string]clickhouse.TimeWindow{
		"1s": clickhouse.TimeWindow1s, "5s": clickhouse.TimeWindow5s,
		"10s": clickhouse.TimeWindow10s, "15s": clickhouse.TimeWindow15s, "30s": clickhouse.TimeWindow30s,
		"1m": clickhouse.TimeWindow1m, "5m": clickhouse.TimeWindow5m,
		"10m": clickhouse.TimeWindow10m, "15m": clickhouse.TimeWindow15m, "30m": clickhouse.TimeWindow30m,
		"1h": clickhouse.TimeWindow1h, "2h": clickhouse.TimeWindow2h, "3h": clickhouse.TimeWindow3h,
		"6h": clickhouse.TimeWindow6h, "12h": clickhouse.TimeWindow12h,
		"24h": clickhouse.TimeWindow24h, "1d": clickhouse.TimeWindow24h,
	}

	if tw, ok := windowMap[window]; ok {
		return tw, nil
	}
	return "", fmt.Errorf("invalid histogram window: %s", window)
}

// --- Log Context Functions ---

// LogContextParams defines parameters for the log context query.
type LogContextParams struct {
	TargetTimestamp int64 // Unix timestamp in milliseconds
	BeforeLimit     int   // Number of logs to fetch before target time
	AfterLimit      int   // Number of logs to fetch after target time
	BeforeOffset    int   // Offset for before query (for pagination)
	AfterOffset     int   // Offset for after query (for pagination)
	ExcludeBoundary bool  // When true, use < instead of <= for before query (for pagination)
	QueryTimeout    *int  // Optional query timeout in seconds
}

// LogContextResponse structures the response for log context data.
type LogContextResponse struct {
	TargetTimestamp int64                    `json:"target_timestamp"`
	BeforeLogs      []map[string]interface{} `json:"before_logs"`
	TargetLogs      []map[string]interface{} `json:"target_logs"`
	AfterLogs       []map[string]interface{} `json:"after_logs"`
	Stats           models.QueryStats        `json:"stats"`
}

// GetLogContext retrieves surrounding logs around a specific timestamp for contextual analysis.
// This is similar to grep -C, showing N logs before and M logs after the target time.
func GetLogContext(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, sourceID models.SourceID, params LogContextParams) (*LogContextResponse, error) {
	// 1. Get source details (especially the timestamp field)
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source details: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Ensure MetaTSField is configured for the source
	if source.MetaTSField == "" {
		log.Error("log context query attempted on source without configured timestamp field", "source_id", sourceID)
		return nil, fmt.Errorf("source %d does not have a timestamp field configured", sourceID)
	}

	// Apply defaults for limits
	if params.BeforeLimit <= 0 {
		params.BeforeLimit = 10
	}
	if params.AfterLimit <= 0 {
		params.AfterLimit = 10
	}

	// Ensure timeout is set
	if params.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		params.QueryTimeout = &defaultTimeout
	}

	// 2. Get ClickHouse connection
	client, err := chDB.GetConnection(sourceID)
	if err != nil {
		log.Error("failed to get clickhouse client for log context", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("error getting database connection for source %d: %w", sourceID, err)
	}

	// 3. Convert millisecond timestamp to time.Time
	targetTime := time.UnixMilli(params.TargetTimestamp)

	// 4. Prepare parameters for the ClickHouse client call
	chParams := clickhouse.LogContextParams{
		TargetTime:      targetTime,
		BeforeLimit:     params.BeforeLimit,
		AfterLimit:      params.AfterLimit,
		BeforeOffset:    params.BeforeOffset,
		AfterOffset:     params.AfterOffset,
		ExcludeBoundary: params.ExcludeBoundary,
	}

	// 5. Call the ClickHouse client method
	contextResult, err := client.GetSurroundingLogs(
		ctx,
		source.GetFullTableName(), // e.g., "default.logs"
		source.MetaTSField,        // The configured timestamp field
		chParams,
		params.QueryTimeout,
	)
	if err != nil {
		log.Error("failed to get surrounding logs from clickhouse", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("error retrieving log context for source %d: %w", sourceID, err)
	}

	// 6. Format the response
	return &LogContextResponse{
		TargetTimestamp: params.TargetTimestamp,
		BeforeLogs:      contextResult.BeforeLogs,
		TargetLogs:      contextResult.TargetLogs,
		AfterLogs:       contextResult.AfterLogs,
		Stats:           contextResult.Stats,
	}, nil
}
