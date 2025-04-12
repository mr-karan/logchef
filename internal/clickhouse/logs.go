package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
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

	// Parse the raw SQL query to extract the WHERE clause conditions
	var whereClauseStr string
	if params.Query != "" {
		// Preprocess SQL: The parser doesn't handle standard SQL escaped quotes properly
		// Convert doubled single quotes ('') to a placeholder
		const placeholder = "___ESCAPED_QUOTE___"
		processedSQL := strings.ReplaceAll(params.Query, "''", placeholder)

		parser := clickhouseparser.NewParser(processedSQL)
		stmts, err := parser.ParseStmts()
		if err != nil {
			c.logger.Warn("failed to parse raw SQL for histogram WHERE clause extraction", "error", err, "query", params.Query)
			// Return an error as we cannot reliably apply user filters
			return nil, fmt.Errorf("failed to parse raw SQL query '%s': %w", params.Query, err)
		}

		// We expect a single SELECT statement
		if len(stmts) == 1 {
			if selectStmt, ok := stmts[0].(*clickhouseparser.SelectQuery); ok {
				if selectStmt.Where != nil {
					// Convert the WHERE clause AST back to string
					whereClauseStr = selectStmt.Where.String()
					// Convert the placeholder back to standard SQL escaped quotes
					whereClauseStr = strings.ReplaceAll(whereClauseStr, placeholder, "''")
					c.logger.Debug("extracted WHERE clause for histogram", "where_clause", whereClauseStr)
				}
			} else {
				c.logger.Warn("parsed SQL is not a SELECT statement, ignoring potential WHERE clause for histogram", "query", params.Query)
			}
		} else if len(stmts) > 1 {
			c.logger.Warn("multiple SQL statements found, cannot reliably extract WHERE clause for histogram", "query", params.Query)
		}
		// If len(stmts) == 0, whereClauseStr remains empty.
	}

	// Build the time filter using the correct timestamp field
	timeFilter := fmt.Sprintf("`%s` >= toDateTime('%s') AND `%s` <= toDateTime('%s')",
		timestampField, params.StartTime.Format("2006-01-02 15:04:05"),
		timestampField, params.EndTime.Format("2006-01-02 15:04:05"))

	// Combine the mandatory time filter with the extracted user query conditions (if any)
	combinedWhereClause := timeFilter
	if whereClauseStr != "" {
		combinedWhereClause = fmt.Sprintf("%s AND (%s)", timeFilter, whereClauseStr)
	}

	// Build the final histogram query
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
