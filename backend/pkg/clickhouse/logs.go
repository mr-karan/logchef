package clickhouse

import (
	"backend-v2/pkg/models"
	"time"
)

// LogQueryParams represents parameters for querying logs
type LogQueryParams struct {
	StartTime  time.Time
	EndTime    time.Time
	Limit      int
	Conditions []models.FilterCondition
	Sort       *models.SortOptions
	Mode       models.QueryMode
	RawSQL     string // Used only for raw_sql mode
	LogChefQL  string // Used only for logchefql mode
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
	Table     string // Fully qualified table name
}

// TimeSeriesResult represents the result of time series aggregation
type TimeSeriesResult struct {
	Buckets []TimeSeriesBucket `json:"buckets"`
}
