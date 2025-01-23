package clickhouse

import (
	"backend-v2/pkg/models"
	"backend-v2/pkg/querybuilder"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// LogQueryParams represents parameters for querying logs
type LogQueryParams struct {
	StartTime    time.Time
	EndTime      time.Time
	Limit        int
	FilterGroups []models.FilterGroup
	Sort         *models.SortOptions
	Mode         models.QueryMode
	RawSQL       string // Used only for raw_sql mode
	LogChefQL    string // Used only for logchefql mode
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
		"filter_groups", params.FilterGroups,
		"sort", params.Sort,
		"mode", params.Mode,
	)

	// Create query builder based on mode
	var builder querybuilder.Builder
	opts := querybuilder.Options{
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		Limit:     params.Limit,
		Sort:      params.Sort,
	}

	switch params.Mode {
	case models.QueryModeRawSQL:
		if params.RawSQL == "" {
			return nil, fmt.Errorf("raw SQL query cannot be empty")
		}
		builder = querybuilder.NewRawSQLBuilder(c.Source.GetFullTableName(), params.RawSQL)
	case models.QueryModeLogChefQL:
		if params.LogChefQL == "" {
			return nil, fmt.Errorf("LogchefQL query cannot be empty")
		}
		builder = querybuilder.NewLogchefQLBuilder(c.Source.GetFullTableName(), opts, params.LogChefQL)
	case models.QueryModeFilters:
		builder = querybuilder.NewFilterBuilder(c.Source.GetFullTableName(), opts, params.FilterGroups)
	default:
		return nil, fmt.Errorf("unsupported query mode: %s", params.Mode)
	}

	// Build the query
	query, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("error building query: %w", err)
	}

	c.log.Debug("executing query",
		"query", query.SQL,
		"args", query.Args,
	)

	// Start timing the query
	start := time.Now()

	// Execute query
	rows, err := c.DB.Query(ctx, query.SQL, query.Args...)
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
