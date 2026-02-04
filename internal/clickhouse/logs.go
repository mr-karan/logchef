package clickhouse

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// LogQueryParams defines parameters for querying logs.
type LogQueryParams struct {
	Limit    int
	MaxLimit int
	RawSQL   string
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
	TargetTime      time.Time
	BeforeLimit     int
	AfterLimit      int
	BeforeOffset    int  // Offset for before query (for pagination)
	AfterOffset     int  // Offset for after query (for pagination)
	ExcludeBoundary bool // When true, use < instead of <= for before query (for pagination)
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
func (c *Client) GetHistogramData(ctx context.Context, tableName, timestampField string, params HistogramParams) (*HistogramResult, error) {
	if params.Query == "" {
		return nil, fmt.Errorf("query parameter is required for histogram data")
	}

	if params.QueryTimeout == nil {
		defaultTimeout := DefaultQueryTimeout
		params.QueryTimeout = &defaultTimeout
	}

	timezone := params.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	intervalFunc, err := windowToIntervalFunc(params.Window, timestampField, timezone)
	if err != nil {
		return nil, err
	}

	qb := NewQueryBuilder(tableName, 0)
	baseQuery, err := qb.RemoveLimitClause(params.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to process base query: %w", err)
	}

	query, err := c.buildHistogramQuery(baseQuery, timestampField, intervalFunc, params.GroupBy)
	if err != nil {
		return nil, err
	}

	result, err := c.QueryWithTimeout(ctx, query, params.QueryTimeout)
	if err != nil {
		c.logger.Error("failed to execute histogram query", "error", err, "table", tableName)
		return nil, fmt.Errorf("failed to execute histogram query: %w", err)
	}

	results := c.parseHistogramResults(result, params.GroupBy != "")

	return &HistogramResult{
		Granularity: string(params.Window),
		Data:        results,
	}, nil
}

func windowToIntervalFunc(window TimeWindow, timestampField, timezone string) (string, error) {
	switch window {
	case TimeWindow1s:
		return fmt.Sprintf("toStartOfSecond(%s, '%s')", timestampField, timezone), nil
	case TimeWindow5s, TimeWindow10s, TimeWindow15s, TimeWindow30s:
		seconds := strings.TrimSuffix(string(window), "s")
		return fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s SECOND, '%s')", timestampField, seconds, timezone), nil
	case TimeWindow1m:
		return fmt.Sprintf("toStartOfMinute(%s, '%s')", timestampField, timezone), nil
	case TimeWindow5m:
		return fmt.Sprintf("toStartOfFiveMinute(%s, '%s')", timestampField, timezone), nil
	case TimeWindow10m, TimeWindow15m, TimeWindow30m:
		minutes := strings.TrimSuffix(string(window), "m")
		return fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s MINUTE, '%s')", timestampField, minutes, timezone), nil
	case TimeWindow1h:
		return fmt.Sprintf("toStartOfHour(%s, '%s')", timestampField, timezone), nil
	case TimeWindow2h, TimeWindow3h, TimeWindow6h, TimeWindow12h, TimeWindow24h:
		hours := strings.TrimSuffix(string(window), "h")
		return fmt.Sprintf("toStartOfInterval(%s, INTERVAL %s HOUR, '%s')", timestampField, hours, timezone), nil
	default:
		return "", fmt.Errorf("invalid time window: %s", window)
	}
}

func (c *Client) buildHistogramQuery(baseQuery, timestampField, intervalFunc, groupBy string) (string, error) {
	modifiedQuery, err := c.ensureTimestampInQuery(baseQuery, timestampField)
	if err != nil {
		return "", fmt.Errorf("failed to modify query for histogram: %w", err)
	}

	if groupBy != "" && strings.TrimSpace(groupBy) != "" {
		return fmt.Sprintf(`
			WITH top_groups AS (
				SELECT %s AS group_value, count(*) AS total_logs
				FROM (%s) AS raw_logs
				GROUP BY group_value ORDER BY total_logs DESC LIMIT 10
			)
			SELECT %s AS bucket, %s AS group_value, count(*) AS log_count
			FROM (%s) AS raw_logs
			WHERE %s GLOBAL IN (SELECT group_value FROM top_groups)
			GROUP BY bucket, group_value ORDER BY bucket ASC, log_count DESC
		`, groupBy, modifiedQuery, intervalFunc, groupBy, modifiedQuery, groupBy), nil
	}

	return fmt.Sprintf(`
		SELECT %s AS bucket, count(*) AS log_count
		FROM (%s) AS raw_logs
		GROUP BY bucket ORDER BY bucket ASC
	`, intervalFunc, modifiedQuery), nil
}

func (c *Client) parseHistogramResults(result *models.QueryResult, hasGroupBy bool) []HistogramData {
	results := make([]HistogramData, 0, len(result.Logs))

	for _, row := range result.Logs {
		data, ok := c.parseHistogramRow(row, hasGroupBy)
		if ok {
			results = append(results, data)
		}
	}
	return results
}

func (c *Client) parseHistogramRow(row map[string]any, hasGroupBy bool) (HistogramData, bool) {
	bucket, okB := row["bucket"].(time.Time)
	countVal, okC := row["log_count"]
	if !okB || !okC {
		return HistogramData{}, false
	}

	count, ok := toInt(countVal)
	if !ok {
		return HistogramData{}, false
	}

	groupValue := ""
	if hasGroupBy {
		groupValue, ok = extractGroupValue(row)
		if !ok {
			return HistogramData{}, false
		}
	}

	return HistogramData{Bucket: bucket, LogCount: count, GroupValue: groupValue}, true
}

func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case uint64:
		// #nosec G115 -- histogram counts are bounded by actual log entries
		return int(min(val, uint64(math.MaxInt))), true
	case int64:
		return int(val), true
	case int:
		return val, true
	case float64:
		return int(val), true
	default:
		return 0, false
	}
}

func extractGroupValue(row map[string]any) (string, bool) {
	groupVal, ok := row["group_value"]
	if !ok {
		return "", false
	}
	switch v := groupVal.(type) {
	case string:
		return v, true
	case nil:
		return "null", true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

// ensureTimestampInQuery ensures the timestamp field is available for histogram bucketing.
// IMPORTANT: In ClickHouse, MATERIALIZED columns are NOT included in SELECT *.
// When we wrap a query in a subquery for histogram, we must explicitly select the timestamp field.
func (c *Client) ensureTimestampInQuery(query, timestampField string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query cannot be empty")
	}

	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	escapedTsField := fmt.Sprintf("`%s`", timestampField)

	// Check if timestamp field is already explicitly mentioned in SELECT clause
	if strings.Contains(upperQuery, strings.ToUpper(timestampField)) {
		// Timestamp field is already present, return as-is
		return query, nil
	}

	// For SELECT * queries, we need to explicitly add the timestamp field
	// because MATERIALIZED columns are NOT included in SELECT *
	// Replace "SELECT *" with "SELECT *, `timestamp_field`"
	selectStarRegex := regexp.MustCompile(`(?i)SELECT\s+\*`)
	if selectStarRegex.MatchString(query) {
		modifiedQuery := selectStarRegex.ReplaceAllString(query, fmt.Sprintf("SELECT *, %s", escapedTsField))
		if c.logger != nil {
			c.logger.Debug("Added timestamp field to SELECT * for histogram",
				"timestamp_field", timestampField,
				"reason", "MATERIALIZED columns not included in SELECT *")
		}
		return modifiedQuery, nil
	}

	// For any other case, try to add the timestamp field after SELECT
	// This handles cases like "SELECT col1, col2 FROM ..."
	selectRegex := regexp.MustCompile(`(?i)^SELECT\s+`)
	if selectRegex.MatchString(query) {
		modifiedQuery := selectRegex.ReplaceAllString(query, fmt.Sprintf("SELECT %s, ", escapedTsField))
		if c.logger != nil {
			c.logger.Debug("Prepended timestamp field to SELECT for histogram",
				"timestamp_field", timestampField)
		}
		return modifiedQuery, nil
	}

	if c.logger != nil {
		c.logger.Warn("Could not modify query to include timestamp field",
			"query_preview", query[:min(100, len(query))])
	}
	return query, nil
}

// GetSurroundingLogs retrieves logs around a specific timestamp, similar to grep -C.
// It executes 2 queries: one for logs at or before the target time, one for logs after.
// The target timestamp logs are included at the end of BeforeLogs (after reversing).
func (c *Client) GetSurroundingLogs(ctx context.Context, tableName, timestampField string, params LogContextParams, queryTimeout *int) (*LogContextResult, error) {
	// Ensure timeout is set
	if queryTimeout == nil {
		defaultTimeout := DefaultQueryTimeout
		queryTimeout = &defaultTimeout
	}

	// Validate limits
	if params.BeforeLimit <= 0 {
		params.BeforeLimit = 10
	}
	if params.AfterLimit <= 0 {
		params.AfterLimit = 10
	}

	// Cap limits to prevent excessive queries
	if params.BeforeLimit > 100 {
		params.BeforeLimit = 100
	}
	if params.AfterLimit > 100 {
		params.AfterLimit = 100
	}

	c.logger.Debug("fetching surrounding logs",
		"table", tableName,
		"timestamp_field", timestampField,
		"target_time", params.TargetTime,
		"before_limit", params.BeforeLimit,
		"after_limit", params.AfterLimit,
		"timeout_seconds", *queryTimeout)

	var result LogContextResult
	var totalExecutionMs float64

	// Format the target timestamp for ClickHouse DateTime64
	targetTimeStr := params.TargetTime.UTC().Format("2006-01-02 15:04:05.000")

	// Determine comparison operator for before query
	// Use < (exclusive) for pagination to avoid duplicates, <= (inclusive) for initial load
	beforeOp := "<="
	if params.ExcludeBoundary {
		beforeOp = "<"
	}

	// Query 1: Get logs AT OR BEFORE the target time (ordered DESC to get closest ones first)
	// Use OFFSET for pagination when loading more
	// Note: Explicitly include timestamp field in SELECT to handle MATERIALIZED columns
	// (SELECT * doesn't include MATERIALIZED columns in ClickHouse)
	beforeQuery := fmt.Sprintf(`
		SELECT %s, * FROM %s
		WHERE %s %s toDateTime64('%s', 3, 'UTC')
		ORDER BY %s DESC
		LIMIT %d OFFSET %d
	`, timestampField, tableName, timestampField, beforeOp, targetTimeStr, timestampField, params.BeforeLimit, params.BeforeOffset)

	beforeResult, err := c.QueryWithTimeout(ctx, beforeQuery, queryTimeout)
	if err != nil {
		c.logger.Error("failed to query before logs", "error", err)
		return nil, fmt.Errorf("failed to query logs before target time: %w", err)
	}
	// Reverse so logs appear in chronological order (oldest first, target timestamp at end)
	result.BeforeLogs = reverseLogSlice(beforeResult.Logs)
	totalExecutionMs += beforeResult.Stats.ExecutionTimeMs

	// Query 2: Get logs AFTER the target time (ordered ASC to get closest ones first)
	// Uses > (exclusive) for the timestamp, with OFFSET for pagination
	// Note: Explicitly include timestamp field in SELECT to handle MATERIALIZED columns
	afterQuery := fmt.Sprintf(`
		SELECT %s, * FROM %s
		WHERE %s > toDateTime64('%s', 3, 'UTC')
		ORDER BY %s ASC
		LIMIT %d OFFSET %d
	`, timestampField, tableName, timestampField, targetTimeStr, timestampField, params.AfterLimit, params.AfterOffset)

	afterResult, err := c.QueryWithTimeout(ctx, afterQuery, queryTimeout)
	if err != nil {
		c.logger.Error("failed to query after logs", "error", err)
		return nil, fmt.Errorf("failed to query logs after target time: %w", err)
	}
	result.AfterLogs = afterResult.Logs
	totalExecutionMs += afterResult.Stats.ExecutionTimeMs

	// TargetLogs is kept empty - target timestamp logs are included in BeforeLogs
	result.TargetLogs = []map[string]interface{}{}

	// Aggregate stats
	result.Stats = models.QueryStats{
		RowsRead:        len(result.BeforeLogs) + len(result.AfterLogs),
		ExecutionTimeMs: totalExecutionMs,
	}

	c.logger.Debug("surrounding logs query complete",
		"before_count", len(result.BeforeLogs),
		"after_count", len(result.AfterLogs),
		"total_execution_ms", totalExecutionMs)

	return &result, nil
}

// reverseLogSlice reverses a slice of log maps in place and returns it.
func reverseLogSlice(logs []map[string]interface{}) []map[string]interface{} {
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	return logs
}
