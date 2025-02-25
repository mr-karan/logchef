package clickhouse

import (
	"context"
	"fmt"
	"time"

	"logchef/pkg/models"
)

// LogQueryParams represents parameters for querying logs
type LogQueryParams struct {
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	RawSQL    string
}

// LogQueryResult represents the result of a log query
type LogQueryResult struct {
	Data    []map[string]interface{} `json:"data"`
	Stats   models.QueryStats        `json:"stats"`
	Columns []models.ColumnInfo      `json:"columns"`
}

// TimeWindow represents a duration for time series aggregation
type TimeWindow string

const (
	TimeWindow1m  TimeWindow = "1m"  // For ranges up to 1 hour
	TimeWindow5m  TimeWindow = "5m"  // For ranges up to 6 hours
	TimeWindow15m TimeWindow = "15m" // For ranges up to 12 hours
	TimeWindow1h  TimeWindow = "1h"  // For ranges up to 24 hours
	TimeWindow6h  TimeWindow = "6h"  // For ranges up to 7 days
	TimeWindow24h TimeWindow = "24h" // For ranges beyond 7 days
)

// TimeSeriesBucket represents a single time bucket with count
type TimeSeriesBucket struct {
	Timestamp int64 `json:"timestamp"` // Unix timestamp in milliseconds
	Count     int64 `json:"count"`
}

// TimeSeriesParams represents parameters for time series aggregation
type TimeSeriesParams struct {
	StartTime time.Time
	EndTime   time.Time
	Window    TimeWindow
}

// TimeSeriesResult represents the result of time series aggregation
type TimeSeriesResult struct {
	Buckets []TimeSeriesBucket `json:"buckets"`
}

// LogContextParams represents parameters for getting log context
type LogContextParams struct {
	TargetTime  time.Time
	BeforeLimit int
	AfterLimit  int
}

// LogContextResult represents the result of a context query
type LogContextResult struct {
	BeforeLogs []map[string]interface{}
	TargetLogs []map[string]interface{}
	AfterLogs  []map[string]interface{}
	Stats      models.QueryStats
}

// GetTimeSeries retrieves time series data for log counts
func (m *Manager) GetTimeSeries(ctx context.Context, sourceID string, source *models.Source, params TimeSeriesParams) (*TimeSeriesResult, error) {
	// Get client for the source
	client, err := m.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("getting connection for source %s: %w", sourceID, err)
	}

	// Get table name from source
	tableName := fmt.Sprintf("%s.%s", source.Connection.Database, source.Connection.TableName)

	// Execute the time series query
	return client.GetTimeSeries(ctx, params, tableName)
}

// GetLogContext retrieves logs before and after a specific timestamp
func (m *Manager) GetLogContext(ctx context.Context, sourceID string, source *models.Source, params LogContextParams) (*LogContextResult, error) {
	// Get client for the source
	client, err := m.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("getting connection for source %s: %w", sourceID, err)
	}

	// Get table name from source
	tableName := fmt.Sprintf("%s.%s", source.Connection.Database, source.Connection.TableName)

	// Execute the context query
	return client.GetLogContext(ctx, params, tableName)
}
