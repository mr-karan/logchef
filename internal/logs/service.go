package logs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/sqlite"
)

// ErrSourceNotFound is returned when a source is not found
var ErrSourceNotFound = fmt.Errorf("source not found")

// Service handles operations related to logs
type Service struct {
	db        *sqlite.DB
	manager   *clickhouse.Manager
	log       *slog.Logger
	validator *Validator
}

// New creates a new logs service
func New(db *sqlite.DB, manager *clickhouse.Manager, log *slog.Logger) *Service {
	return &Service{
		db:        db,
		manager:   manager,
		log:       log.With("component", "logs_service"),
		validator: NewValidator(),
	}
}

// QueryLogs retrieves logs from a source with pagination and time range
func (s *Service) QueryLogs(ctx context.Context, sourceID models.SourceID, params clickhouse.LogQueryParams) (*models.QueryResult, error) {
	// Get source from SQLite
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Get client from manager
	client, err := s.manager.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}

	// Use the query builder to handle the raw SQL
	tableName := source.GetFullTableName()
	qb := clickhouse.NewQueryBuilder(tableName)
	builtQuery, err := qb.BuildRawQuery(params.RawSQL, params.Limit)
	if err != nil {
		return nil, fmt.Errorf("error building raw SQL query: %w", err)
	}

	// Execute query
	return client.Query(ctx, builtQuery)
}

// GetTimeSeries retrieves time series data for log counts
func (s *Service) GetTimeSeries(ctx context.Context, sourceID models.SourceID, params clickhouse.TimeSeriesParams) (*clickhouse.TimeSeriesResult, error) {
	// Get source from SQLite
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Execute the time series query
	return s.manager.GetTimeSeries(ctx, sourceID, source, params)
}

// GetLogContext retrieves logs before and after a specific timestamp
func (s *Service) GetLogContext(ctx context.Context, req *models.LogContextRequest) (*models.LogContextResponse, error) {
	// Get source from SQLite
	source, err := s.db.GetSource(ctx, req.SourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Prepare parameters
	params := clickhouse.LogContextParams{
		TargetTime:  time.UnixMilli(req.Timestamp),
		BeforeLimit: req.BeforeLimit,
		AfterLimit:  req.AfterLimit,
	}

	// Execute the context query
	result, err := s.manager.GetLogContext(ctx, req.SourceID, source, params)
	if err != nil {
		return nil, fmt.Errorf("error getting log context: %w", err)
	}

	// Convert to response format
	return &models.LogContextResponse{
		TargetTimestamp: req.Timestamp,
		BeforeLogs:      result.BeforeLogs,
		TargetLogs:      result.TargetLogs,
		AfterLogs:       result.AfterLogs,
		Stats:           result.Stats,
	}, nil
}

// GetSourceStats retrieves statistics for a specific source
func (s *Service) GetSourceStats(ctx context.Context, sourceID models.SourceID) (map[string]interface{}, error) {
	// Get source from SQLite
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// For now, return some basic stats
	// This should be replaced with actual stats from ClickHouse in the future
	return map[string]interface{}{
		"source_id":   source.ID,
		"database":    source.Connection.Database,
		"table":       source.Connection.TableName,
		"description": source.Description,
		"status":      "active",
	}, nil
}

// GetSourceSchema retrieves the schema (column information) for a specific source
func (s *Service) GetSourceSchema(ctx context.Context, sourceID models.SourceID) ([]models.ColumnInfo, error) {
	// Get source from SQLite
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// For now, return some basic schema info
	// This should be replaced with actual schema from ClickHouse in the future
	return []models.ColumnInfo{
		{Name: "timestamp", Type: "DateTime"},
		{Name: "level", Type: "String"},
		{Name: "message", Type: "String"},
	}, nil
}

// HistogramParams parameters for histogram data
type HistogramParams struct {
	StartTime time.Time
	EndTime   time.Time
	Window    string
	RawSQL    string
}

// HistogramResponse response for histogram data
type HistogramResponse struct {
	Granularity string                     `json:"granularity"`
	Data        []clickhouse.HistogramData `json:"data"`
}

// GetHistogramData fetches histogram data for a specific source and time range
func (s *Service) GetHistogramData(ctx context.Context, sourceID models.SourceID, params HistogramParams) (*HistogramResponse, error) {
	// Get the source from the database
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Get the ClickHouse client
	client, err := s.manager.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting clickhouse client: %w", err)
	}

	// Set up clickhouse params
	chParams := clickhouse.HistogramParams{
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		Window:    clickhouse.TimeWindow(params.Window),
		Query:     params.RawSQL,
	}

	// Get the histogram data
	histogramData, err := client.GetHistogramData(
		ctx,
		source.GetFullTableName(),
		source.MetaTSField,
		chParams,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting histogram data: %w", err)
	}

	// Return the response
	return &HistogramResponse{
		Granularity: histogramData.Granularity,
		Data:        histogramData.Data,
	}, nil
}
