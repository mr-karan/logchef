package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
)

// LogQueryParams represents parameters for querying logs
type LogQueryParams struct {
	StartTime time.Time
	EndTime   time.Time
	Limit     int
}

// LogQueryResult represents the result of a log query
type LogQueryResult struct {
	Data  []map[string]interface{} `json:"data"`
	Stats QueryStats               `json:"stats"`
}

// QueryStats represents statistics about the query execution
type QueryStats struct {
	ExecutionTimeMs float64 `json:"execution_time_ms"`
}

// QueryLogs queries logs from the source with pagination and time range
func (c *Connection) QueryLogs(ctx context.Context, params LogQueryParams) (*LogQueryResult, error) {
	c.log.Debug("querying logs",
		"table", c.Source.TableName,
		"start_time", params.StartTime,
		"end_time", params.EndTime,
		"limit", params.Limit,
	)

	// Use SQL builder to construct query
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("toJSONString(tuple(*)) as raw_json")
	sb.From(c.Source.GetFullTableName())

	// Add time range conditions
	if !params.StartTime.IsZero() {
		sb.Where(sb.GE("timestamp", sqlbuilder.Raw(fmt.Sprintf("toDateTime64(%f, 3)", float64(params.StartTime.UnixNano())/1e9))))
	}
	if !params.EndTime.IsZero() {
		sb.Where(sb.LE("timestamp", sqlbuilder.Raw(fmt.Sprintf("toDateTime64(%f, 3)", float64(params.EndTime.UnixNano())/1e9))))
	}

	// Add order by timestamp desc and limit
	sb.OrderBy("timestamp DESC")
	if params.Limit > 0 {
		sb.Limit(params.Limit)
	}

	// Build the query
	query, args := sb.Build()
	c.log.Debug("executing query",
		"query", query,
		"args", args,
	)

	// Start timing the query
	start := time.Now()

	// Execute query
	rows, err := c.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("error executing query: %w", err)
	}
	defer rows.Close()

	// Read rows
	var result LogQueryResult
	for rows.Next() {
		var jsonStr string
		if err := rows.Scan(&jsonStr); err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}

		// Parse JSON string into map
		var rowData map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &rowData); err != nil {
			return nil, fmt.Errorf("error parsing JSON: %w", err)
		}

		result.Data = append(result.Data, rowData)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Calculate execution time
	result.Stats = QueryStats{
		ExecutionTimeMs: float64(time.Since(start).Microseconds()) / 1000.0,
	}

	return &result, nil
}
