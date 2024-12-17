package db

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/huandu/go-sqlbuilder"
	"github.com/mr-karan/logchef/pkg/models"
)

// QueryExecutor represents a common interface for executing queries
type QueryExecutor interface {
	// ExecuteQuery executes a query and returns logs response
	ExecuteQuery(ctx context.Context, source *models.Source, builder *sqlbuilder.SelectBuilder) (*models.LogResponse, error)
	// ExecuteRawQuery executes a raw SQL query
	ExecuteRawQuery(ctx context.Context, source *models.Source, query string, args []interface{}) (*models.LogResponse, error)
}

// ClickhouseExecutor implements QueryExecutor for Clickhouse
type ClickhouseExecutor struct {
	pool   *ConnectionPool
	logger *QueryLogger
}

func NewClickhouseExecutor(pool *ConnectionPool) *ClickhouseExecutor {
	return &ClickhouseExecutor{
		pool: pool,
	}
}

// ExecuteQuery executes a query built using sqlbuilder
func (e *ClickhouseExecutor) ExecuteQuery(ctx context.Context, source *models.Source, builder *sqlbuilder.SelectBuilder) (*models.LogResponse, error) {
	conn, err := e.pool.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	logger := NewQueryLogger(source.ID)
	start := time.Now()

	// Execute query
	query, args := builder.Build()
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	logger.LogQuery(query, args, err, time.Since(start))

	return e.scanRows(rows)
}

// ExecuteRawQuery executes raw SQL
func (e *ClickhouseExecutor) ExecuteRawQuery(ctx context.Context, source *models.Source, query string, args []interface{}) (*models.LogResponse, error) {
	conn, err := e.pool.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	logger := NewQueryLogger(source.ID)
	start := time.Now()

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	logger.LogQuery(query, args, err, time.Since(start))

	return e.scanRows(rows)
}

// scanRows dynamically scans rows based on column information
func (e *ClickhouseExecutor) scanRows(rows driver.Rows) (*models.LogResponse, error) {
	columns := rows.Columns()
	columnTypes := rows.ColumnTypes()

	// Special case for COUNT queries
	if len(columns) == 1 && (columns[0] == "COUNT(*)" || columns[0] == "count") {
		var count uint64
		if !rows.Next() {
			return &models.LogResponse{
				Logs:       []models.Log{},
				TotalCount: 0,
				HasMore:    false,
			}, nil
		}
		if err := rows.Scan(&count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		return &models.LogResponse{
			Logs:       []models.Log{},
			TotalCount: int(count),
			HasMore:    false,
		}, nil
	}

	types := GetColumnTypes(columns, columnTypes)
	var logs []models.Log
	for rows.Next() {
		values := make([]any, len(columns))
		for i := range values {
			values[i] = types[i].GoType
		}

		if err := rows.Scan(values...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Create log entry without Fields wrapper
		logData := make(models.Log)
		for i, col := range columns {
			logData[col] = convertToJSON(values[i], types[i].JSONType)
		}

		logs = append(logs, logData)
	}

	return &models.LogResponse{
		Logs:       logs,
		TotalCount: len(logs),
		HasMore:    false,
	}, nil
}
