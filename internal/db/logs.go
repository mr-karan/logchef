package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/mr-karan/logchef/pkg/models"
)

// LogRepository handles log querying operations
type LogRepository struct {
	Executor   QueryExecutor
	sourceRepo *models.SourceRepository
}

// hasMoreResults checks if there are more results after the current offset
func (r *LogRepository) hasMoreResults(ctx context.Context, source *models.Source, params models.LogQueryParams) (bool, error) {
	// Build a count query for remaining records
	sb := sqlbuilder.ClickHouse.NewSelectBuilder()
	sb.Select("count(*) as count").
		From("logs." + source.TableName).
		Where(
			sb.Between("timestamp",
				sqlbuilder.Raw(fmt.Sprintf("toDateTime64('%s', 3)", params.StartTime.Format("2006-01-02 15:04:05.000"))),
				sqlbuilder.Raw(fmt.Sprintf("toDateTime64('%s', 3)", params.EndTime.Format("2006-01-02 15:04:05.000"))),
			),
		).
		Offset(params.Offset + params.Limit)

	result, err := r.Executor.ExecuteQuery(ctx, source, sb)
	if err != nil {
		return false, fmt.Errorf("failed to check remaining records: %w", err)
	}

	return result.TotalCount > 0, nil
}

func NewLogRepository(executor QueryExecutor, sourceRepo *models.SourceRepository) *LogRepository {
	return &LogRepository{
		Executor:   executor,
		sourceRepo: sourceRepo,
	}
}

// QueryLogs fetches logs based on the provided parameters
func (r *LogRepository) QueryLogs(ctx context.Context, sourceID string, params models.LogQueryParams) (*models.LogResponse, error) {
	source, err := r.sourceRepo.Get(sourceID)
	if err != nil {
		return nil, fmt.Errorf("source not found: %w", err)
	}

	// Build query using sqlbuilder
	sb := sqlbuilder.ClickHouse.NewSelectBuilder()
	sb.Select("*").
		From("logs." + source.TableName).
		Where(
			sb.Between("timestamp",
				sqlbuilder.Raw(fmt.Sprintf("toDateTime64('%s', 3)", params.StartTime.Format("2006-01-02 15:04:05.000"))),
				sqlbuilder.Raw(fmt.Sprintf("toDateTime64('%s', 3)", params.EndTime.Format("2006-01-02 15:04:05.000"))),
			),
		)

	// Add filters
	if params.ServiceName != "" {
		sb.Where(sb.Equal("service_name", params.ServiceName))
	}
	if params.Namespace != "" {
		sb.Where(sb.Equal("namespace", params.Namespace))
	}
	if params.SeverityText != "" {
		sb.Where(sb.Equal("severity_text", params.SeverityText))
	}
	if params.SearchQuery != "" {
		sb.Where(sb.Like("body", "%"+params.SearchQuery+"%"))
	}

	// Add pagination
	sb.OrderBy("timestamp DESC").
		Limit(params.Limit).
		Offset(params.Offset)

	// Execute the query
	result, err := r.Executor.ExecuteQuery(ctx, source, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Check if there are more results
	hasMore, err := r.hasMoreResults(ctx, source, params)
	if err != nil {
		return nil, fmt.Errorf("failed to check for more results: %w", err)
	}

	// Update hasMore in the result
	result.HasMore = hasMore
	return result, nil
}

// GetLogSchema analyzes recent logs to determine schema including nested JSON fields
func (r *LogRepository) GetLogSchema(ctx context.Context, source *models.Source, startTime, endTime time.Time) ([]models.LogSchema, error) {
	// First get the table schema from Clickhouse
	conn, err := r.Executor.(*ClickhouseExecutor).pool.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Get column information directly from Clickhouse
	rows, err := conn.Query(ctx, fmt.Sprintf("DESCRIBE TABLE logs.%s", source.TableName))
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}
	defer rows.Close()

	var schema []models.LogSchema
	for rows.Next() {
		var name, typ, defaultType, defaultExpression, comment, codecExpression, ttlExpression string
		if err := rows.Scan(&name, &typ, &defaultType, &defaultExpression, &comment, &codecExpression, &ttlExpression); err != nil {
			return nil, fmt.Errorf("failed to scan column info: %w", err)
		}

		// Create base schema entry
		schemaEntry := models.LogSchema{
			Name: name,
			Type: typ,
			Path: []string{name},
		}

		// For Map types, analyze the structure
		if strings.HasPrefix(typ, "Map(") {
			schemaEntry.IsNested = true
			children, err := r.analyzeMapColumn(ctx, source, name, startTime, endTime)
			if err != nil {
				return nil, fmt.Errorf("failed to analyze map column %s: %w", name, err)
			}
			schemaEntry.Children = children
		}

		schema = append(schema, schemaEntry)
	}

	return schema, nil
}

// analyzeMapColumn examines the contents of a Map column to discover its structure
func (r *LogRepository) analyzeMapColumn(ctx context.Context, source *models.Source, columnName string, startTime, endTime time.Time) ([]models.LogSchema, error) {
	// Build a query to get distinct keys from the map column
	query := fmt.Sprintf(`
		SELECT DISTINCT arrayJoin(mapKeys(%s)) as key,
		toTypeName(mapValues(%s)[indexOf(mapKeys(%s), arrayJoin(mapKeys(%s)))]) as value_type
		FROM logs.%s
		WHERE timestamp BETWEEN ? AND ?
		LIMIT 1000
	`, columnName, columnName, columnName, columnName, source.TableName)

	conn, err := r.Executor.(*ClickhouseExecutor).pool.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	rows, err := conn.Query(ctx, query, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query map keys: %w", err)
	}
	defer rows.Close()

	var children []models.LogSchema
	for rows.Next() {
		var key, valueType string
		if err := rows.Scan(&key, &valueType); err != nil {
			return nil, fmt.Errorf("failed to scan map key info: %w", err)
		}

		children = append(children, models.LogSchema{
			Name:     key,
			Type:     valueType,
			Path:     []string{columnName, key},
			IsNested: false,
		})
	}

	return children, nil
}

// ExecuteRawQuery executes a raw SQL query and returns the results
func (r *LogRepository) ExecuteRawQuery(ctx context.Context, sourceID string, query string, args []interface{}) (*models.LogResponse, error) {
	source, err := r.sourceRepo.Get(sourceID)
	if err != nil {
		return nil, fmt.Errorf("source not found: %w", err)
	}

	// Use the executor to run the query
	return r.Executor.ExecuteRawQuery(ctx, source, query, args)
}

// GetTotalLogsCount returns the total number of logs for a given source and query parameters
func (r *LogRepository) GetTotalLogsCount(ctx context.Context, source *models.Source, params models.LogQueryParams) (int, error) {
	// Build query using sqlbuilder for better safety and readability
	sb := sqlbuilder.ClickHouse.NewSelectBuilder()
	sb.Select("count(*) as count").
		From("logs." + source.TableName).
		Where(
			sb.Between("timestamp",
				sqlbuilder.Raw(fmt.Sprintf("toDateTime64('%s', 3)", params.StartTime.Format("2006-01-02 15:04:05.000"))),
				sqlbuilder.Raw(fmt.Sprintf("toDateTime64('%s', 3)", params.EndTime.Format("2006-01-02 15:04:05.000"))),
			),
		)

	// Add filters
	if params.ServiceName != "" {
		sb.Where(sb.Equal("service_name", params.ServiceName))
	}
	if params.Namespace != "" {
		sb.Where(sb.Equal("namespace", params.Namespace))
	}
	if params.SeverityText != "" {
		sb.Where(sb.Equal("severity_text", params.SeverityText))
	}
	if params.SearchQuery != "" {
		sb.Where(sb.Like("body", "%"+params.SearchQuery+"%"))
	}

	result, err := r.Executor.ExecuteQuery(ctx, source, sb)
	if err != nil {
		return 0, fmt.Errorf("failed to get total count: %w", err)
	}

	return result.TotalCount, nil
}
