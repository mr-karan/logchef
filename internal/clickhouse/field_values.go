package clickhouse

// Field distinct-value discovery: per-field and all-filterable-fields queries
// used to populate filter UIs.

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// FieldValueInfo represents a distinct value with its count for a field.
type FieldValueInfo struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// FieldValuesResult holds the distinct values for a field along with metadata.
type FieldValuesResult struct {
	FieldName     string           `json:"field_name"`
	FieldType     string           `json:"field_type"`
	IsLowCard     bool             `json:"is_low_cardinality"`
	Values        []FieldValueInfo `json:"values"`
	TotalDistinct int64            `json:"total_distinct"`
}

// FieldValuesParams holds parameters for fetching field distinct values.
type FieldValuesParams struct {
	FieldName      string
	FieldType      string
	TimestampField string    // Required: timestamp column name for time range filter
	StartTime      time.Time // Required: start of time range
	EndTime        time.Time // Required: end of time range
	Timezone       string    // Optional: timezone for time conversion (defaults to UTC)
	Limit          int       // Optional: max values to return (default 10, max 100)
	Timeout        *int      // Optional: query timeout in seconds
	LogchefQL      string    // Optional: LogchefQL query string - parsed on backend for proper SQL generation
}

// buildLogchefQLConditionsSQL parses a LogchefQL query and returns the SQL WHERE clause fragment.
// This uses the proper LogchefQL parser which handles nested fields, Map columns, JSON extraction, etc.
// Returns empty string if query is empty or invalid.
func buildLogchefQLConditionsSQL(query string) string {
	if query == "" || strings.TrimSpace(query) == "" {
		return ""
	}

	result := logchefql.Translate(query, nil)
	if !result.Valid || result.SQL == "" {
		return ""
	}

	// Return the SQL wrapped as " AND (...)" to be appended to WHERE clause
	return " AND (" + result.SQL + ")"
}

// GetFieldDistinctValues retrieves the top N distinct values for a field within a time range.
func (c *Client) GetFieldDistinctValues(ctx context.Context, database, table string, params FieldValuesParams) (*FieldValuesResult, error) {
	// Validate inputs that will be interpolated into SQL
	if err := ValidateIdentifier(params.FieldName); err != nil {
		return nil, fmt.Errorf("invalid field name: %w", err)
	}
	if err := ValidateIdentifier(params.TimestampField); err != nil {
		return nil, fmt.Errorf("invalid timestamp field: %w", err)
	}

	limit, timeoutSeconds, timezone := normalizeFieldValuesParams(params)

	if err := ValidateTimezone(timezone); err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	c.logger.Debug("fetching distinct values for field",
		"database", database, "table", table, "field", params.FieldName,
		"field_type", params.FieldType, "limit", limit)

	isLowCard := strings.Contains(params.FieldType, "LowCardinality")
	startTimeStr := params.StartTime.UTC().Format("2006-01-02 15:04:05")
	endTimeStr := params.EndTime.UTC().Format("2006-01-02 15:04:05")
	additionalConditions := buildLogchefQLConditionsSQL(params.LogchefQL)

	quotedField := quoteIdentifier(params.FieldName)

	// For string-like fields, exclude empty strings. For numeric fields, no such filter.
	emptyFilter := fmt.Sprintf("%s != ''", quotedField)
	if isNumericColumnType(params.FieldType) {
		emptyFilter = "1"
	}

	query := fmt.Sprintf(`
		SELECT %s AS value, count() AS cnt
		FROM %s.%s
		PREWHERE %s BETWEEN toDateTime('%s', '%s') AND toDateTime('%s', '%s')
		WHERE %s%s
		GROUP BY value ORDER BY cnt DESC LIMIT %d
	`, quotedField, database, table,
		params.TimestampField, startTimeStr, timezone, endTimeStr, timezone,
		emptyFilter, additionalConditions, limit)

	result, err := c.QueryWithTimeout(ctx, query, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to query distinct values for %s: %w", params.FieldName, err)
	}

	values := extractFieldValues(result)

	totalDistinct := c.queryTotalDistinct(ctx, database, table, params, startTimeStr, endTimeStr, timezone, additionalConditions, timeoutSeconds)

	return &FieldValuesResult{
		FieldName:     params.FieldName,
		FieldType:     params.FieldType,
		IsLowCard:     isLowCard,
		Values:        values,
		TotalDistinct: totalDistinct,
	}, nil
}

func normalizeFieldValuesParams(params FieldValuesParams) (limit int, timeout *int, timezone string) {
	limit = params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	timeout = params.Timeout
	if timeout == nil {
		defaultTimeout := 10
		timeout = &defaultTimeout
	}

	timezone = params.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	return
}

func extractFieldValues(result *models.QueryResult) []FieldValueInfo {
	values := make([]FieldValueInfo, 0, len(result.Logs))
	for _, row := range result.Logs {
		val, ok := extractStringFromRow(row, "value")
		if !ok || val == "" {
			continue
		}

		count, ok := extractInt64FromRow(row, "cnt")
		if !ok {
			continue
		}

		values = append(values, FieldValueInfo{Value: val, Count: count})
	}
	return values
}

func extractStringFromRow(row map[string]any, key string) (string, bool) {
	rawVal, exists := row[key]
	if !exists {
		return "", false
	}

	switch v := rawVal.(type) {
	case string:
		return v, true
	case *string:
		if v == nil {
			return "", false
		}
		return *v, true
	case []byte:
		return string(v), true
	case *[]byte:
		if v == nil {
			return "", false
		}
		return string(*v), true
	case nil:
		return "", false
	default:
		return fmt.Sprintf("%v", v), true
	}
}

func extractInt64FromRow(row map[string]any, key string) (int64, bool) {
	rawVal, exists := row[key]
	if !exists {
		return 0, false
	}

	switch v := rawVal.(type) {
	case uint64:
		// #nosec G115 -- count values from DB are bounded by actual row counts
		return int64(min(v, uint64(math.MaxInt64))), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}

func (c *Client) queryTotalDistinct(ctx context.Context, database, table string, params FieldValuesParams, startTimeStr, endTimeStr, timezone, additionalConditions string, timeoutSeconds *int) int64 {
	quotedField := quoteIdentifier(params.FieldName)
	emptyFilter := fmt.Sprintf("%s != ''", quotedField)
	if isNumericColumnType(params.FieldType) {
		emptyFilter = "1"
	}

	query := fmt.Sprintf(`
		SELECT uniq(%s) AS total
		FROM %s.%s
		PREWHERE %s BETWEEN toDateTime('%s', '%s') AND toDateTime('%s', '%s')
		WHERE %s%s
	`, quotedField, database, table,
		params.TimestampField, startTimeStr, timezone, endTimeStr, timezone,
		emptyFilter, additionalConditions)

	result, err := c.QueryWithTimeout(ctx, query, timeoutSeconds)
	if err != nil || len(result.Logs) == 0 {
		return 0
	}

	if total, ok := result.Logs[0]["total"]; ok {
		switch v := total.(type) {
		case uint64:
			// #nosec G115 -- distinct count values are bounded by actual row counts
			return int64(min(v, uint64(math.MaxInt64)))
		case int64:
			return v
		}
	}
	return 0
}

// AllFieldValuesParams holds parameters for fetching field values for filterable columns.
type AllFieldValuesParams struct {
	TimestampField string    // Required: timestamp column name for time range filter
	StartTime      time.Time // Required: start of time range
	EndTime        time.Time // Required: end of time range
	Timezone       string    // Optional: timezone for time conversion (defaults to UTC)
	Limit          int       // Optional: max values per field (default 10, max 100)
	Timeout        *int      // Optional: query timeout in seconds (default 5s for String fields)
	LogchefQL      string    // Optional: LogchefQL query string - parsed on backend for proper SQL generation
}

// isNumericColumnType returns true for integer, float, and decimal types.
// Handles any nesting order of LowCardinality/Nullable wrappers.
func isNumericColumnType(colType string) bool {
	clean := strings.ToLower(colType)
	// Strip all wrapper layers regardless of order
	for {
		prev := clean
		clean = strings.TrimPrefix(clean, "lowcardinality(")
		clean = strings.TrimPrefix(clean, "nullable(")
		clean = strings.TrimSuffix(clean, ")")
		if clean == prev {
			break
		}
	}

	return strings.HasPrefix(clean, "uint") ||
		strings.HasPrefix(clean, "int") ||
		strings.HasPrefix(clean, "float") ||
		strings.HasPrefix(clean, "decimal")
}

// isFilterableColumnType returns true if the column type is suitable for distinct value queries.
// LowCardinality fields are always fast. String and numeric fields are included with timeout protection.
func isFilterableColumnType(colType string) bool {
	lowerType := strings.ToLower(colType)
	if strings.HasPrefix(lowerType, "map(") ||
		strings.HasPrefix(lowerType, "array(") ||
		strings.HasPrefix(lowerType, "tuple(") ||
		lowerType == "json" ||
		strings.HasPrefix(lowerType, "json(") {
		return false
	}

	// DateTime types are not useful for distinct value filtering
	if strings.HasPrefix(lowerType, "datetime") || lowerType == "date" || lowerType == "date32" {
		return false
	}

	if strings.Contains(colType, "LowCardinality") {
		return true
	}

	if colType == "String" || colType == "Nullable(String)" {
		return true
	}

	if strings.HasPrefix(colType, "Enum") {
		return true
	}

	if isNumericColumnType(colType) {
		return true
	}

	return false
}

// GetAllFilterableFieldValues retrieves distinct values for all filterable fields within a time range.
// Filterable fields include: LowCardinality, String, Nullable(String), and Enum types.
// This is useful for populating a field sidebar with filterable values.
// For String fields, a shorter timeout is used to gracefully handle high cardinality columns.
// IMPORTANT: Time range is required to avoid scanning entire tables.
func (c *Client) GetAllFilterableFieldValues(ctx context.Context, database, table string, params AllFieldValuesParams) (map[string]*FieldValuesResult, error) {
	// Reuse existing getColumns function to get column metadata
	columns, err := c.getColumns(ctx, database, table)
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	results := make(map[string]*FieldValuesResult)
	var mu sync.Mutex

	// Default timeout for String fields (shorter to fail fast on high cardinality)
	stringFieldTimeout := 5
	lowCardTimeout := 10

	// Fan out the per-field distinct-value queries with bounded concurrency. Each
	// query already carries its own timeout, so one slow field can't stall the
	// rest; the semaphore caps how many hit ClickHouse at once.
	sem := make(chan struct{}, fieldValuesConcurrency)
	var wg sync.WaitGroup

	for _, col := range columns {
		// Stop launching new work once the caller's context is done.
		if ctx.Err() != nil {
			c.logger.Debug("context cancelled, stopping field value queries", "error", ctx.Err())
			break
		}

		// Check if this column type is suitable for distinct value queries
		if !isFilterableColumnType(col.Type) {
			continue
		}

		// Use shorter timeout for regular String fields (may be high cardinality)
		timeout := params.Timeout
		if timeout == nil {
			if strings.Contains(col.Type, "LowCardinality") {
				timeout = &lowCardTimeout
			} else {
				timeout = &stringFieldTimeout
			}
		}

		// Build params for individual field query
		fieldParams := FieldValuesParams{
			FieldName:      col.Name,
			FieldType:      col.Type,
			TimestampField: params.TimestampField,
			StartTime:      params.StartTime,
			EndTime:        params.EndTime,
			Timezone:       params.Timezone,
			Limit:          params.Limit,
			Timeout:        timeout,
			LogchefQL:      params.LogchefQL, // Pass through user's LogchefQL query
		}

		// Acquire a slot, but honor cancellation while all slots are busy
		// (otherwise a cancelled request keeps launching fields up to the
		// per-field timeout).
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			c.logger.Debug("context cancelled while awaiting field-value slot", "error", ctx.Err())
		}
		if ctx.Err() != nil {
			break // exit the loop; wg.Wait below drains in-flight queries
		}
		wg.Go(func() {
			defer func() { <-sem }()

			fieldResult, err := c.GetFieldDistinctValues(ctx, database, table, fieldParams)
			if err != nil {
				// Log but don't fail - this field just won't have values shown.
				// Common for high cardinality String fields that timeout.
				c.logger.Debug("skipping field values (likely timeout or high cardinality)",
					"field", fieldParams.FieldName,
					"type", fieldParams.FieldType,
					"error", err)
				return
			}
			mu.Lock()
			results[fieldParams.FieldName] = fieldResult
			mu.Unlock()
		})
	}

	wg.Wait()
	return results, nil
}

// GetAllLowCardinalityFieldValues is deprecated, use GetAllFilterableFieldValues instead.
// Kept for backwards compatibility.
func (c *Client) GetAllLowCardinalityFieldValues(ctx context.Context, database, table string, params AllFieldValuesParams) (map[string]*FieldValuesResult, error) {
	return c.GetAllFilterableFieldValues(ctx, database, table, params)
}
