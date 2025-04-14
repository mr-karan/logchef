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

	log.Debug("querying logs", "source_id", sourceID, "database", source.Connection.Database, "table", source.Connection.TableName, "limit", params.Limit)

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

	// 4. Execute the query via the ClickHouse client
	log.Debug("executing clickhouse query", "source_id", sourceID, "query_len", len(builtQuery))
	queryResult, err := client.Query(ctx, builtQuery)
	if err != nil {
		log.Error("failed to execute clickhouse query", "source_id", sourceID, "error", err)
		// Consider parsing CH error for user-friendliness
		return nil, fmt.Errorf("error executing query on source %d: %w", sourceID, err)
	}

	log.Debug("log query successful", "source_id", sourceID, "rows_returned", len(queryResult.Logs))
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

	log.Debug("getting source schema", "source_id", sourceID, "database", source.Connection.Database, "table", source.Connection.TableName)

	// 2. Get ClickHouse connection
	client, err := chDB.GetConnection(sourceID)
	if err != nil {
		log.Error("failed to get clickhouse client for schema retrieval", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("error getting database connection for source %d: %w", sourceID, err)
	}

	// 3. Get table schema from ClickHouse client
	// Use GetTableSchema which returns the full TableInfo
	tableInfo, err := client.GetTableSchema(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		log.Error("failed to get table schema from clickhouse", "source_id", sourceID, "database", source.Connection.Database, "table", source.Connection.TableName, "error", err)
		return nil, fmt.Errorf("error retrieving schema for source %d: %w", sourceID, err)
	}

	log.Debug("schema retrieval successful", "source_id", sourceID, "column_count", len(tableInfo.Columns))
	return tableInfo.Columns, nil
}

// --- Histogram Data Functions ---

// HistogramParams defines parameters specifically for histogram queries.
// Keeping it separate allows for specific validation or processing.
type HistogramParams struct {
	StartTime time.Time
	EndTime   time.Time
	Window    string // e.g., "1m", "5m", "1h"
	Query     string // Optional filter query (WHERE clause part)
}

// HistogramResponse structures the response for histogram data.
type HistogramResponse struct {
	Granularity string                     `json:"granularity"`
	Data        []clickhouse.HistogramData `json:"data"`
}

// GetHistogramData fetches histogram data for a specific source and time range.
// It uses the source's configured timestamp field.
func GetHistogramData(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, sourceID models.SourceID, params HistogramParams) (*HistogramResponse, error) {
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
		log.Error("histogram query attempted on source without configured timestamp field", "source_id", sourceID)
		return nil, fmt.Errorf("source %d does not have a timestamp field configured, cannot generate histogram", sourceID)
	}

	log.Debug("getting histogram data",
		"source_id", sourceID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
		"ts_field", source.MetaTSField,
		"start_time", params.StartTime,
		"end_time", params.EndTime,
		"window", params.Window,
		"filter_query_len", len(params.Query),
	)

	// 2. Get ClickHouse connection
	client, err := chDB.GetConnection(sourceID)
	if err != nil {
		log.Error("failed to get clickhouse client for histogram", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("error getting database connection for source %d: %w", sourceID, err)
	}

	// 3. Prepare parameters for the ClickHouse client call
	// Validate and convert window string to clickhouse.TimeWindow
	var chWindow clickhouse.TimeWindow
	switch params.Window {
	case "1m":
		chWindow = clickhouse.TimeWindow1m
	case "5m":
		chWindow = clickhouse.TimeWindow5m
	case "15m":
		chWindow = clickhouse.TimeWindow15m
	case "1h":
		chWindow = clickhouse.TimeWindow1h
	case "6h":
		chWindow = clickhouse.TimeWindow6h
	case "24h", "1d": // Allow "1d" as alias for 24h
		chWindow = clickhouse.TimeWindow24h
	default:
		log.Warn("invalid histogram window specified, defaulting to 1h", "source_id", sourceID, "invalid_window", params.Window)
		chWindow = clickhouse.TimeWindow1h // Default or return error
		// return nil, fmt.Errorf("invalid histogram window: %s", params.Window)
	}

	chParams := clickhouse.HistogramParams{
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		Window:    chWindow,
		Query:     params.Query, // Pass the optional filter query
	}

	// 4. Call the ClickHouse client method
	histogramData, err := client.GetHistogramData(
		ctx,
		source.GetFullTableName(), // e.g., "default.logs"
		source.MetaTSField,        // The configured timestamp field
		chParams,
	)
	if err != nil {
		log.Error("failed to get histogram data from clickhouse", "source_id", sourceID, "error", err)
		// Consider parsing CH error
		return nil, fmt.Errorf("error generating histogram for source %d: %w", sourceID, err)
	}

	log.Debug("histogram data retrieval successful", "source_id", sourceID, "bucket_count", len(histogramData.Data))

	// 5. Format the response
	return &HistogramResponse{
		Granularity: histogramData.Granularity,
		Data:        histogramData.Data,
	}, nil
}

// Note: GetSourceStats was part of the logs service but seems more related to source management.
// It has been moved to internal/core/source.go.
