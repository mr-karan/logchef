package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
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

// TimeWindow represents the granularity for time series aggregation
type TimeWindow string

const (
	TimeWindow1m  TimeWindow = "1m"  // 1 minute
	TimeWindow5m  TimeWindow = "5m"  // 5 minutes
	TimeWindow15m TimeWindow = "15m" // 15 minutes
	TimeWindow1h  TimeWindow = "1h"  // 1 hour
	TimeWindow6h  TimeWindow = "6h"  // 6 hours
	TimeWindow24h TimeWindow = "24h" // 24 hours
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

// HistogramParams represents parameters for histogram query
type HistogramParams struct {
	StartTime time.Time
	EndTime   time.Time
	Window    TimeWindow
	Query     string // Raw SQL query
}

// HistogramData represents a single data point in the histogram
type HistogramData struct {
	Bucket   time.Time `json:"bucket"`
	LogCount int       `json:"log_count"`
}

// HistogramResult represents the result of a histogram query
type HistogramResult struct {
	Granularity string          `json:"granularity"`
	Data        []HistogramData `json:"data"`
}

// GetTimeSeries retrieves time series data for log counts
func (m *Manager) GetTimeSeries(ctx context.Context, sourceID models.SourceID, source *models.Source, params TimeSeriesParams) (*TimeSeriesResult, error) {
	// Get client for the source
	client, err := m.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("getting connection for source %d: %w", sourceID, err)
	}

	// Get table name from source
	tableName := fmt.Sprintf("%s.%s", source.Connection.Database, source.Connection.TableName)

	// Execute the time series query
	return client.GetTimeSeries(ctx, params, tableName)
}

// GetLogContext retrieves logs before and after a specific timestamp
func (m *Manager) GetLogContext(ctx context.Context, sourceID models.SourceID, source *models.Source, params LogContextParams) (*LogContextResult, error) {
	// Get client for the source
	client, err := m.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("getting connection for source %d: %w", sourceID, err)
	}

	// Get table name from source
	tableName := fmt.Sprintf("%s.%s", source.Connection.Database, source.Connection.TableName)

	// Execute the context query
	return client.GetLogContext(ctx, params, tableName)
}

// GetHistogramData returns histogram data for a specific source and time range
func (c *Client) GetHistogramData(ctx context.Context, tableName string, timestampField string, params HistogramParams) (*HistogramResult, error) {
	// Determine interval value and unit based on window
	intervalValue, intervalUnit := getIntervalFromWindow(params.Window)

	// Extract WHERE clause from the raw SQL query
	// This would typically extract conditions from a query like:
	// SELECT * FROM table WHERE condition1 AND condition2 ORDER BY col LIMIT 100
	whereClause := extractWhereClause(params.Query)

	// Build the time filter
	timeFilter := fmt.Sprintf("`%s` >= toDateTime('%s') AND `%s` <= toDateTime('%s')",
		timestampField, params.StartTime.Format("2006-01-02 15:04:05"),
		timestampField, params.EndTime.Format("2006-01-02 15:04:05"))

	// Combine time filter with user query conditions
	combinedWhereClause := timeFilter
	if whereClause != "" && whereClause != params.Query {
		combinedWhereClause = timeFilter + " AND (" + whereClause + ")"
	}

	// Build the histogram query using the bucket aggregation
	query := fmt.Sprintf(`
		SELECT
			toStartOfInterval(%s, INTERVAL %d %s) AS bucket,
			count(*) AS log_count
		FROM %s
		WHERE %s
		GROUP BY bucket
		ORDER BY bucket ASC
	`, timestampField, intervalValue, intervalUnit, tableName, combinedWhereClause)

	// Execute the query
	result, err := c.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute histogram query: %w", err)
	}

	// Parse the results
	var results []HistogramData
	for _, row := range result.Logs {
		bucket, ok := row["bucket"].(time.Time)
		if !ok {
			c.logger.Warn("unexpected type for bucket in histogram row", "value", row["bucket"])
			continue
		}

		count := int(0)
		switch v := row["log_count"].(type) {
		case uint64:
			count = int(v)
		case int64:
			count = int(v)
		case int:
			count = v
		default:
			c.logger.Warn("unexpected type for log_count in histogram row", "type", fmt.Sprintf("%T", row["log_count"]))
			continue
		}

		results = append(results, HistogramData{
			Bucket:   bucket,
			LogCount: count,
		})
	}

	return &HistogramResult{
		Granularity: string(params.Window),
		Data:        results,
	}, nil
}

// Helper function to extract the interval value and unit from a TimeWindow
func getIntervalFromWindow(window TimeWindow) (int, string) {
	switch window {
	case TimeWindow1m:
		return 1, "minute"
	case TimeWindow5m:
		return 5, "minute"
	case TimeWindow15m:
		return 15, "minute"
	case TimeWindow1h:
		return 1, "hour"
	case TimeWindow6h:
		return 6, "hour"
	case TimeWindow24h:
		return 24, "hour"
	default:
		// Default to 1 hour if window is not recognized
		return 1, "hour"
	}
}

// Helper function to extract WHERE clause conditions from a full SQL query string.
// It aims to isolate the part between WHERE and clauses like GROUP BY, ORDER BY, LIMIT etc.
func extractWhereClause(sqlQuery string) string {
	if sqlQuery == "" {
		return ""
	}

	// Normalize whitespace and convert to uppercase for easier searching
	normalizedSQL := strings.Join(strings.Fields(sqlQuery), " ")
	upperSQL := strings.ToUpper(normalizedSQL)

	// Find the start of the WHERE clause
	whereIndex := strings.Index(upperSQL, " WHERE ")
	if whereIndex == -1 {
		return "" // No WHERE clause found
	}

	// Start searching for conditions after " WHERE "
	startIndex := whereIndex + len(" WHERE ")

	// Find the end of the WHERE conditions by looking for subsequent clauses
	endIndex := len(normalizedSQL) // Default to end of string

	// Keywords that typically terminate a WHERE clause
	terminatingKeywords := []string{
		" GROUP BY ", " HAVING ", " ORDER BY ", " LIMIT ", " OFFSET ",
		" UNION ", " EXCEPT ", " INTERSECT ", " WINDOW ", " SETTINGS ",
	}

	for _, keyword := range terminatingKeywords {
		idx := strings.Index(upperSQL[startIndex:], keyword)
		if idx != -1 {
			// Found a terminating keyword, adjust endIndex if this keyword appears earlier
			potentialEndIndex := startIndex + idx
			if potentialEndIndex < endIndex {
				endIndex = potentialEndIndex
			}
		}
	}

	// Extract the conditions
	if startIndex >= endIndex {
		return "" // Should not happen if WHERE was found, but safety check
	}

	conditions := normalizedSQL[startIndex:endIndex]

	// Basic check for balanced parentheses - this is not foolproof but helps catch simple errors
	if strings.Count(conditions, "(") != strings.Count(conditions, ")") {
		// Fallback or log warning: The extracted clause might be incomplete due to complex structure
		// For now, we'll return what we found, but ideally, a proper parser is needed.
		// Consider logging a warning here if logging is available.
	}

	return strings.TrimSpace(conditions)
}

// Placeholder for LogChefQL to SQL conversion
func (c *Client) convertLogChefQLToSQL(logchefQL string) (string, error) {
	// This would use your existing LogChefQL parser and converter
	// For now, return the query as is assuming it's already in SQL-like format
	return logchefQL, nil
}
