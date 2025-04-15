package clickhouse

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

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
	GroupBy   string // Optional: Field to group by for segmented histograms.
}

// HistogramData represents a single time bucket and its log count in a histogram.
type HistogramData struct {
	Bucket     time.Time `json:"bucket"`      // Start time of the bucket.
	LogCount   int       `json:"log_count"`   // Number of logs in the bucket.
	GroupValue string    `json:"group_value"` // Value of the group for grouped histograms.
}

// HistogramResult holds the complete histogram data and its granularity.
type HistogramResult struct {
	Granularity string          `json:"granularity"` // The time window used (e.g., "5m").
	Data        []HistogramData `json:"data"`
}

// Helper function to find the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Helper function to extract WHERE clause from SQL query
func extractWhereClause(sql string) string {
	// Simple extraction using case-insensitive regex to find WHERE clause
	// This is a basic implementation that might need to be enhanced for complex queries
	wherePattern := `(?i)WHERE\s+(.*?)(?:\s+(?:GROUP\s+BY|ORDER\s+BY|LIMIT|HAVING|OFFSET|UNION|INTERSECT|EXCEPT|$))`
	re := regexp.MustCompile(wherePattern)
	matches := re.FindStringSubmatch(sql)

	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// If no WHERE clause found or if the input might be just conditions
	// Check if it contains standard SQL operators
	operatorPattern := `(?i)(=|!=|<>|<|>|<=|>=|LIKE|IN\s*\(|NOT IN\s*\(|BETWEEN)`
	hasOperators := regexp.MustCompile(operatorPattern).MatchString(sql)

	if hasOperators {
		// Looks like conditions without WHERE keyword
		return strings.TrimSpace(sql)
	}

	return ""
}

// GetHistogramData generates histogram data by grouping log counts into time buckets.
// It applies the time range and an optional WHERE clause filter provided in params.Query.
// If params.GroupBy is provided, results will be grouped by that field.
func (c *Client) GetHistogramData(ctx context.Context, tableName, timestampField string, params HistogramParams) (*HistogramResult, error) {
	// Convert TimeWindow to the appropriate ClickHouse interval function
	var intervalFunc string
	switch params.Window {
	case TimeWindow1s:
		intervalFunc = fmt.Sprintf("toStartOfSecond(%s)", timestampField)
	case TimeWindow5s, TimeWindow10s, TimeWindow15s, TimeWindow30s:
		// For custom second intervals, use toStartOfInterval
		seconds := strings.TrimSuffix(string(params.Window), "s")
		intervalFunc = fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s SECOND)", timestampField, seconds)
	case TimeWindow1m:
		intervalFunc = fmt.Sprintf("toStartOfMinute(%s)", timestampField)
	case TimeWindow5m:
		intervalFunc = fmt.Sprintf("toStartOfFiveMinute(%s)", timestampField)
	case TimeWindow10m, TimeWindow15m, TimeWindow30m:
		// For custom minute intervals, use toStartOfInterval
		minutes := strings.TrimSuffix(string(params.Window), "m")
		intervalFunc = fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s MINUTE)", timestampField, minutes)
	case TimeWindow1h:
		intervalFunc = fmt.Sprintf("toStartOfHour(%s)", timestampField)
	case TimeWindow2h, TimeWindow3h, TimeWindow6h, TimeWindow12h, TimeWindow24h:
		// For custom hour intervals, use toStartOfInterval
		hours := strings.TrimSuffix(string(params.Window), "h")
		intervalFunc = fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s HOUR)", timestampField, hours)
	default:
		return nil, fmt.Errorf("invalid time window: %s", params.Window)
	}

	// Construct time range filter
	timeFilter := fmt.Sprintf("%s >= toDateTime('%s') AND %s <= toDateTime('%s')",
		timestampField, params.StartTime.Format("2006-01-02 15:04:05"),
		timestampField, params.EndTime.Format("2006-01-02 15:04:05"))

	// Extract WHERE clauses from user query
	var whereClauseStr string
	if params.Query != "" {
		whereClause := extractWhereClause(params.Query)
		if whereClause != "" {
			whereClauseStr = whereClause
		} else {
			whereClauseStr = params.Query
		}
	}

	// Combine time filter with user filter
	combinedWhereClause := timeFilter
	if whereClauseStr != "" {
		combinedWhereClause = fmt.Sprintf("(%s) AND (%s)", timeFilter, whereClauseStr)
	}

	var query string
	if params.GroupBy != "" && strings.TrimSpace(params.GroupBy) != "" {
		// Use CTE approach for grouped data with top values
		// Add GLOBAL keyword for distributed tables
		query = fmt.Sprintf(`
			WITH top_groups AS (
				SELECT
					%s AS group_value,
					count(*) AS total_logs
				FROM %s
				WHERE %s
				GROUP BY group_value
				ORDER BY total_logs DESC
				LIMIT 10
			)
			SELECT
				%s AS bucket,
				%s AS group_value,
				count(*) AS log_count
			FROM %s
			WHERE
				%s
				AND %s GLOBAL IN (SELECT group_value FROM top_groups)
			GROUP BY
				bucket,
				group_value
			ORDER BY
				bucket ASC,
				log_count DESC
		`, params.GroupBy, tableName, combinedWhereClause,
			intervalFunc, params.GroupBy, tableName,
			combinedWhereClause, params.GroupBy)
	} else {
		// Standard histogram query without grouping
		query = fmt.Sprintf(`
			SELECT
				%s AS bucket,
				count(*) AS log_count
			FROM %s
			WHERE %s
			GROUP BY bucket
			ORDER BY bucket ASC
		`, intervalFunc, tableName, combinedWhereClause)
	}

	// Execute the query
	result, err := c.Query(ctx, query)
	if err != nil {
		c.logger.Error("failed to execute histogram query", "error", err, "table", tableName)
		return nil, fmt.Errorf("failed to execute histogram query: %w", err)
	}

	// Parse results into HistogramData
	var results []HistogramData

	for _, row := range result.Logs {
		bucket, okB := row["bucket"].(time.Time)
		countVal, okC := row["log_count"] // Type can vary (uint64, int64, etc.)

		if !okB || !okC {
			c.logger.Warn("unexpected type in histogram row, skipping",
				"bucket_val", row["bucket"],
				"count_val", row["log_count"])
			continue
		}

		// Safely convert count to int
		count := 0
		switch v := countVal.(type) {
		case uint64:
			count = int(v)
		case int64:
			count = int(v)
		case int:
			count = v
		case float64:
			count = int(v)
		default:
			c.logger.Warn("unexpected numeric type for log_count in histogram row",
				"type", fmt.Sprintf("%T", countVal))
			continue
		}

		groupValueStr := ""
		if params.GroupBy != "" {
			groupVal, okG := row["group_value"]
			if !okG {
				c.logger.Warn("missing group_value in histogram row")
				continue
			}

			// Convert group value to string
			switch v := groupVal.(type) {
			case string:
				groupValueStr = v
			case nil:
				groupValueStr = "null"
			default:
				groupValueStr = fmt.Sprintf("%v", v)
			}
		}

		results = append(results, HistogramData{
			Bucket:     bucket,
			LogCount:   count,
			GroupValue: groupValueStr,
		})
	}

	return &HistogramResult{
		Granularity: string(params.Window),
		Data:        results,
	}, nil
}
