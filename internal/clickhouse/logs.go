package clickhouse

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/mr-karan/logchef/pkg/models"
)

// LogQueryParams defines parameters for querying logs.
type LogQueryParams struct {
	Limit  int
	RawSQL string
	// Query execution timeout in seconds. If not specified, uses default timeout.
	QueryTimeout *int
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
	Window   TimeWindow
	Query    string // Raw SQL query to use as base for histogram
	GroupBy  string // Optional: Field to group by for segmented histograms.
	Timezone string // Optional: Timezone identifier for time-based operations.
	// Query execution timeout in seconds. If not specified, uses default timeout.
	QueryTimeout *int
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

// GetHistogramData generates histogram data by grouping log counts into time buckets.
// It uses the provided raw SQL as the base query and applies time window aggregation.
// Timeout is always applied.
func (c *Client) GetHistogramData(ctx context.Context, tableName, timestampField string, params HistogramParams) (*HistogramResult, error) {
	// Validate query parameter
	if params.Query == "" {
		return nil, fmt.Errorf("query parameter is required for histogram data")
	}

	// Ensure timeout is always set
	if params.QueryTimeout == nil {
		defaultTimeout := DefaultQueryTimeout
		params.QueryTimeout = &defaultTimeout
	}

	// Get timezone or default to UTC
	timezone := params.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	// Convert TimeWindow to the appropriate ClickHouse interval function
	var intervalFunc string
	switch params.Window {
	case TimeWindow1s:
		intervalFunc = fmt.Sprintf("toStartOfSecond(%s, '%s')", timestampField, timezone)
	case TimeWindow5s, TimeWindow10s, TimeWindow15s, TimeWindow30s:
		// For custom second intervals, use toStartOfInterval
		seconds := strings.TrimSuffix(string(params.Window), "s")
		intervalFunc = fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s SECOND, '%s')", timestampField, seconds, timezone)
	case TimeWindow1m:
		intervalFunc = fmt.Sprintf("toStartOfMinute(%s, '%s')", timestampField, timezone)
	case TimeWindow5m:
		intervalFunc = fmt.Sprintf("toStartOfFiveMinute(%s, '%s')", timestampField, timezone)
	case TimeWindow10m, TimeWindow15m, TimeWindow30m:
		// For custom minute intervals, use toStartOfInterval
		minutes := strings.TrimSuffix(string(params.Window), "m")
		intervalFunc = fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s MINUTE, '%s')", timestampField, minutes, timezone)
	case TimeWindow1h:
		intervalFunc = fmt.Sprintf("toStartOfHour(%s, '%s')", timestampField, timezone)
	case TimeWindow2h, TimeWindow3h, TimeWindow6h, TimeWindow12h, TimeWindow24h:
		// For custom hour intervals, use toStartOfInterval
		hours := strings.TrimSuffix(string(params.Window), "h")
		intervalFunc = fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s HOUR, '%s')", timestampField, hours, timezone)
	default:
		return nil, fmt.Errorf("invalid time window: %s", params.Window)
	}

	// Process the raw SQL query
	baseQuery := ""
	// Use the query builder to remove LIMIT clause
	qb := NewQueryBuilder(tableName)
	var err error
	baseQuery, err = qb.RemoveLimitClause(params.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to process base query: %w", err)
	}

	// Extract time range conditions for better logging
	timeConditionRegex := regexp.MustCompile(fmt.Sprintf(`(?i)%s\s+BETWEEN\s+toDateTime\(['"](.+?)['"](,\s*['"](.+?)['"])?\)\s+AND\s+toDateTime\(['"](.+?)['"](,\s*['"](.+?)['"])?\)`, regexp.QuoteMeta(timestampField)))
	matches := timeConditionRegex.FindStringSubmatch(params.Query)

	if len(matches) >= 5 {
		startTime := matches[1]
		startTz := matches[3]
		if startTz == "" {
			startTz = timezone
		}
		endTime := matches[4]
		endTz := matches[6]
		if endTz == "" {
			endTz = timezone
		}

		c.logger.Debug("Extracted time filter from query",
			"start", startTime,
			"start_tz", startTz,
			"end", endTime,
			"end_tz", endTz)
	} else {
		c.logger.Debug("No time filter extracted from query, using entire dataset")
	}

	// Construct the histogram query using CTE
	var query string
	if params.GroupBy != "" && strings.TrimSpace(params.GroupBy) != "" {
		// Histogram with grouping - find top N groups
		// Ensure timestamp field is available in subquery for histogram bucketing
		modifiedQuery, err := c.ensureTimestampInQuery(baseQuery, timestampField)
		if err != nil {
			return nil, fmt.Errorf("failed to modify query for grouped histogram: %w", err)
		}

		query = fmt.Sprintf(`
			WITH
				top_groups AS (
					SELECT
						%s AS group_value,
						count(*) AS total_logs
					FROM (%s) AS raw_logs
					GROUP BY group_value
					ORDER BY total_logs DESC
					LIMIT 10
				)
			SELECT
				%s AS bucket,
				%s AS group_value,
				count(*) AS log_count
			FROM (%s) AS raw_logs
			WHERE %s GLOBAL IN (SELECT group_value FROM top_groups)
			GROUP BY
				bucket,
				group_value
			ORDER BY
				bucket ASC,
				log_count DESC
		`, params.GroupBy, modifiedQuery, intervalFunc, params.GroupBy, modifiedQuery, params.GroupBy)
	} else {
		// Standard histogram without grouping
		// Ensure timestamp field is available in subquery for histogram bucketing
		modifiedQuery, err := c.ensureTimestampInQuery(baseQuery, timestampField)
		if err != nil {
			return nil, fmt.Errorf("failed to modify query for histogram: %w", err)
		}

		query = fmt.Sprintf(`
			SELECT
				%s AS bucket,
				count(*) AS log_count
			FROM (%s) AS raw_logs
			GROUP BY bucket
			ORDER BY bucket ASC
		`, intervalFunc, modifiedQuery)
	}

	c.logger.Debug("Executing histogram query",
		"query_length", len(query),
		"has_time_filter", len(matches) >= 5,
		"timeout_seconds", *params.QueryTimeout)

	// Execute the query with timeout (always applied)
	result, err := c.QueryWithTimeout(ctx, query, params.QueryTimeout)
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

// ensureTimestampInQuery modifies a SELECT query to ensure the timestamp field is included
// in the SELECT clause for histogram bucketing. Uses the ClickHouse SQL parser for reliability.
func (c *Client) ensureTimestampInQuery(query, timestampField string) (string, error) {
	// Use the same preprocessing as in QueryBuilder
	const placeholder = "___ESCAPED_QUOTE___"
	processedSQL := strings.ReplaceAll(query, "''", placeholder)

	parser := clickhouseparser.NewParser(processedSQL)
	stmts, err := parser.ParseStmts()
	if err != nil {
		return "", fmt.Errorf("invalid SQL syntax: %w", err)
	}

	if len(stmts) == 0 {
		return "", fmt.Errorf("no SQL statements found")
	}
	if len(stmts) > 1 {
		return "", fmt.Errorf("multiple SQL statements are not supported")
	}

	stmt := stmts[0]
	selectQuery, ok := stmt.(*clickhouseparser.SelectQuery)
	if !ok {
		return "", fmt.Errorf("only SELECT queries are supported")
	}

	// Check if timestamp field is already in SELECT clause
	if c.hasTimestampInSelect(selectQuery, timestampField) {
		// Already has timestamp, return original query
		result := strings.ReplaceAll(query, placeholder, "''")
		return result, nil
	}

	// Add timestamp field to SELECT clause
	if err := c.addTimestampToSelect(selectQuery, timestampField); err != nil {
		return "", fmt.Errorf("failed to add timestamp to SELECT clause: %w", err)
	}

	// Convert back to SQL string and restore escaped quotes
	result := stmt.String()
	result = strings.ReplaceAll(result, placeholder, "''")

	return result, nil
}

// hasTimestampInSelect checks if the timestamp field is already in the SELECT clause
func (c *Client) hasTimestampInSelect(selectQuery *clickhouseparser.SelectQuery, timestampField string) bool {
	if selectQuery.SelectItems == nil {
		return false
	}

	for _, selectItem := range selectQuery.SelectItems {
		// Check for * wildcard - look at the Expr field of SelectItem
		if ident, ok := selectItem.Expr.(*clickhouseparser.Ident); ok {
			if ident.Name == "*" {
				return true // * includes all columns including timestamp
			}
			if ident.Name == timestampField {
				return true
			}
		}

		// Check for column references in other expression types
		if colIdent, ok := selectItem.Expr.(*clickhouseparser.ColumnIdentifier); ok {
			if colIdent.Column != nil && colIdent.Column.Name == timestampField {
				return true
			}
		}
	}

	return false
}

// addTimestampToSelect adds the timestamp field to the SELECT clause
func (c *Client) addTimestampToSelect(selectQuery *clickhouseparser.SelectQuery, timestampField string) error {
	if selectQuery.SelectItems == nil {
		selectQuery.SelectItems = []*clickhouseparser.SelectItem{}
	}

	// Create a new SelectItem for the timestamp field
	timestampSelectItem := &clickhouseparser.SelectItem{
		Expr: &clickhouseparser.Ident{
			Name: timestampField,
		},
	}

	// Add to the select items list
	selectQuery.SelectItems = append(selectQuery.SelectItems, timestampSelectItem)

	return nil
}
