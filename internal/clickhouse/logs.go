package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/mr-karan/logchef/pkg/models"
)

// LogQueryParams defines parameters for querying logs.
type LogQueryParams struct {
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	RawSQL    string
}

// LogQueryResult represents the structured result of a log query.
type LogQueryResult struct {
	Data    []map[string]interface{} `json:"data"`
	Stats   models.QueryStats        `json:"stats"`
	Columns []models.ColumnInfo      `json:"columns"`
}

// TimeWindow represents the desired granularity for time-based aggregations.
type TimeWindow string

const (
	// Second-based windows
	TimeWindow1s  TimeWindow = "1s"  // 1 second
	TimeWindow5s  TimeWindow = "5s"  // 5 seconds
	TimeWindow10s TimeWindow = "10s" // 10 seconds
	TimeWindow15s TimeWindow = "15s" // 15 seconds
	TimeWindow30s TimeWindow = "30s" // 30 seconds

	// Minute-based windows
	TimeWindow1m  TimeWindow = "1m"  // 1 minute
	TimeWindow5m  TimeWindow = "5m"  // 5 minutes
	TimeWindow10m TimeWindow = "10m" // 10 minutes
	TimeWindow15m TimeWindow = "15m" // 15 minutes
	TimeWindow30m TimeWindow = "30m" // 30 minutes

	// Hour-based windows
	TimeWindow1h  TimeWindow = "1h"  // 1 hour
	TimeWindow2h  TimeWindow = "2h"  // 2 hours
	TimeWindow3h  TimeWindow = "3h"  // 3 hours
	TimeWindow6h  TimeWindow = "6h"  // 6 hours
	TimeWindow12h TimeWindow = "12h" // 12 hours
	TimeWindow24h TimeWindow = "24h" // 24 hours
)

// LogContextParams defines parameters for fetching logs around a specific target time.
type LogContextParams struct {
	TargetTime  time.Time
	BeforeLimit int
	AfterLimit  int
}

// LogContextResult holds the logs retrieved before, at, and after the target time.
type LogContextResult struct {
	BeforeLogs []map[string]interface{}
	TargetLogs []map[string]interface{} // Logs exactly at the target time.
	AfterLogs  []map[string]interface{}
	Stats      models.QueryStats
}

// HistogramParams defines parameters for generating histogram data.
type HistogramParams struct {
	StartTime time.Time
	EndTime   time.Time
	Window    TimeWindow
	Query     string // Optional: Raw SQL WHERE clause conditions to apply.
}

// HistogramData represents a single time bucket and its log count in a histogram.
type HistogramData struct {
	Bucket   time.Time `json:"bucket"`    // Start time of the bucket.
	LogCount int       `json:"log_count"` // Number of logs in the bucket.
}

// HistogramResult holds the complete histogram data and its granularity.
type HistogramResult struct {
	Granularity string          `json:"granularity"` // The time window used (e.g., "5m").
	Data        []HistogramData `json:"data"`
}

// GetHistogramData generates histogram data by grouping log counts into time buckets.
// It applies the time range and an optional WHERE clause filter provided in params.Query.
func (c *Client) GetHistogramData(ctx context.Context, tableName string, timestampField string, params HistogramParams) (*HistogramResult, error) {
	intervalValue, intervalUnit, err := getIntervalFromWindow(params.Window)
	if err != nil {
		return nil, fmt.Errorf("failed to get interval from window: %w", err)
	}

	// Attempt to parse the optional user-provided query to extract WHERE conditions.
	var whereClauseStr string
	if params.Query != "" {
		const placeholder = "___ESCAPED_QUOTE___"
		processedSQL := strings.ReplaceAll(params.Query, "''", placeholder)

		parser := clickhouseparser.NewParser(processedSQL)
		stmts, err := parser.ParseStmts()
		if err != nil {
			c.logger.Warn("failed to parse raw SQL for histogram WHERE clause extraction", "error", err, "query", params.Query)
			// Do not proceed if the filter query is invalid.
			return nil, fmt.Errorf("failed to parse raw SQL filter '%s': %w", params.Query, err)
		}

		if len(stmts) == 1 {
			if selectStmt, ok := stmts[0].(*clickhouseparser.SelectQuery); ok {
				if selectStmt.Where != nil && selectStmt.Where.Expr != nil {
					// Extract the WHERE expression, excluding the "WHERE" keyword.
					whereClauseStr = selectStmt.Where.Expr.String()
					whereClauseStr = strings.ReplaceAll(whereClauseStr, placeholder, "''") // Restore quotes
					c.logger.Debug("extracted WHERE clause conditions for histogram", "conditions", whereClauseStr)
				}
			} else {
				c.logger.Warn("parsed SQL filter is not a SELECT statement, ignoring for histogram", "query", params.Query)
			}
		} else if len(stmts) > 1 {
			c.logger.Warn("multiple SQL statements found in filter, cannot reliably extract WHERE clause for histogram", "query", params.Query)
		}
	}

	// Construct the final WHERE clause for the histogram query.
	// Combine the time range filter with the extracted user filter.
	timeFilter := fmt.Sprintf("`%s` >= toDateTime('%s') AND `%s` <= toDateTime('%s')",
		timestampField, params.StartTime.Format("2006-01-02 15:04:05"),
		timestampField, params.EndTime.Format("2006-01-02 15:04:05"))

	combinedWhereClause := timeFilter
	if whereClauseStr != "" {
		// Combine time filter and user filter with AND.
		combinedWhereClause = fmt.Sprintf("(%s) AND (%s)", timeFilter, whereClauseStr)
	}

	// Build the histogram aggregation query.
	query := fmt.Sprintf(`
		SELECT
			toStartOfInterval(%s, INTERVAL %d %s) AS bucket,
			count(*) AS log_count
		FROM %s
		WHERE %s
		GROUP BY bucket
		ORDER BY bucket ASC
	`, timestampField, intervalValue, intervalUnit, tableName, combinedWhereClause)

	// Execute the query.
	result, err := c.Query(ctx, query)
	if err != nil {
		c.logger.Error("failed to execute histogram query", "error", err, "table", tableName)
		return nil, fmt.Errorf("failed to execute histogram query: %w", err)
	}

	// Parse results into HistogramData.
	var results []HistogramData
	for _, row := range result.Logs {
		bucket, okB := row["bucket"].(time.Time)
		countVal, okC := row["log_count"] // Type can vary (uint64, int64, etc.)

		if !okB || !okC {
			c.logger.Warn("unexpected type in histogram row, skipping", "bucket_val", row["bucket"], "count_val", row["log_count"])
			continue
		}

		// Safely convert count to int.
		count := 0
		switch v := countVal.(type) {
		case uint64:
			count = int(v)
		case int64:
			count = int(v)
		case int:
			count = v
		case float64: // Handle potential float conversion if needed
			count = int(v)
		default:
			c.logger.Warn("unexpected numeric type for log_count in histogram row", "type", fmt.Sprintf("%T", countVal))
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

// getIntervalFromWindow converts a TimeWindow constant into a numeric value and unit string
// suitable for ClickHouse INTERVAL clauses. Returns error if window value is unsupported.
func getIntervalFromWindow(window TimeWindow) (int, string, error) {
	switch window {
	// Second-based intervals
	case TimeWindow1s:
		return 1, "second", nil
	case TimeWindow5s:
		return 5, "second", nil
	case TimeWindow10s:
		return 10, "second", nil
	case TimeWindow15s:
		return 15, "second", nil
	case TimeWindow30s:
		return 30, "second", nil

	// Minute-based intervals
	case TimeWindow1m:
		return 1, "minute", nil
	case TimeWindow5m:
		return 5, "minute", nil
	case TimeWindow10m:
		return 10, "minute", nil
	case TimeWindow15m:
		return 15, "minute", nil
	case TimeWindow30m:
		return 30, "minute", nil

	// Hour-based intervals
	case TimeWindow1h:
		return 1, "hour", nil
	case TimeWindow2h:
		return 2, "hour", nil
	case TimeWindow3h:
		return 3, "hour", nil
	case TimeWindow6h:
		return 6, "hour", nil
	case TimeWindow12h:
		return 12, "hour", nil
	case TimeWindow24h:
		return 24, "hour", nil
	default:
		// Return a proper error instead of panicking
		return 0, "", fmt.Errorf("unsupported time window value: %s", window)
	}
}
