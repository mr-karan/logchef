package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/mr-karan/logchef/pkg/models"
)

// LogRepository handles log querying operations
type LogRepository struct {
	pool       *ConnectionPool
	sourceRepo *models.SourceRepository
}

func NewLogRepository(pool *ConnectionPool, sourceRepo *models.SourceRepository) *LogRepository {
	return &LogRepository{
		pool:       pool,
		sourceRepo: sourceRepo,
	}
}

// QueryLogs fetches logs based on the provided parameters
func (r *LogRepository) QueryLogs(ctx context.Context, sourceID string, params models.LogQueryParams) (*models.LogResponse, error) {
	conn, err := r.pool.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Build WHERE clause
	var conditions []string
	var args []interface{}

	// Add default time range if not provided
	if params.StartTime == nil {
		defaultStartTime := time.Now().Add(-1 * time.Hour) // Default to last hour
		params.StartTime = &defaultStartTime
	}
	if params.EndTime == nil {
		defaultEndTime := time.Now()
		params.EndTime = &defaultEndTime
	}

	// Add timestamp conditions using toDateTime64 for proper conversion
	conditions = append(conditions, "timestamp >= toDateTime64(?, 3)")
	args = append(args, params.StartTime.Format("2006-01-02 15:04:05.000"))
	conditions = append(conditions, "timestamp <= toDateTime64(?, 3)")
	args = append(args, params.EndTime.Format("2006-01-02 15:04:05.000"))

	if params.ServiceName != "" {
		conditions = append(conditions, "service_name = ?")
		args = append(args, params.ServiceName)
	}
	if params.Namespace != "" {
		conditions = append(conditions, "namespace = ?")
		args = append(args, params.Namespace)
	}
	if params.SeverityText != "" {
		conditions = append(conditions, "severity_text = ?")
		args = append(args, params.SeverityText)
	}
	if params.SearchQuery != "" {
		conditions = append(conditions, "body ILIKE ?")
		args = append(args, "%"+params.SearchQuery+"%")
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	// Count total matching rows
	countQuery := fmt.Sprintf(`
		SELECT count()
		FROM %s
		%s
	`, params.TableName, whereClause)

	var totalCount uint64
	err = conn.QueryRow(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count logs: %w", err)
	}

	// Fetch paginated results
	if params.Limit == 0 {
		params.Limit = 100 // default limit
	}

	query := fmt.Sprintf(`
		SELECT
			id,
			timestamp,
			trace_id,
			span_id,
			trace_flags,
			severity_text,
			severity_number,
			service_name,
			namespace,
			body,
			log_attributes
		FROM %s
		%s
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, params.TableName, whereClause)

	args = append(args, params.Limit, params.Offset)

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs: %w", err)
	}
	defer rows.Close()

	var logs []*models.Log
	for rows.Next() {
		log := &models.Log{}
		err := rows.Scan(
			&log.ID,
			&log.Timestamp,
			&log.TraceID,
			&log.SpanID,
			&log.TraceFlags,
			&log.SeverityText,
			&log.SeverityNumber,
			&log.ServiceName,
			&log.Namespace,
			&log.Body,
			&log.LogAttributes,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan log: %w", err)
		}
		logs = append(logs, log)
	}

	hasMore := uint64(params.Offset+len(logs)) < totalCount

	return &models.LogResponse{
		Logs:       logs,
		TotalCount: totalCount,
		HasMore:    hasMore,
		StartTime:  *params.StartTime,
		EndTime:    *params.EndTime,
	}, nil
}

// GetLogSchema analyzes recent logs to determine schema
func (r *LogRepository) GetLogSchema(ctx context.Context, source *models.Source, startTime, endTime time.Time) ([]models.LogSchema, error) {
	conn, err := r.pool.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Get base schema
	schema, err := r.getBaseSchema(ctx, conn, source.TableName)
	if err != nil {
		return nil, err
	}

	// For Map type fields, analyze recent logs to get keys
	for i, field := range schema {
		if strings.Contains(field.Type, "Map(") {
			query := fmt.Sprintf(`
				SELECT DISTINCT arrayJoin(mapKeys(%s)) as key
				FROM %s
				WHERE timestamp BETWEEN ? AND ?
				LIMIT 100
			`, field.Name, source.TableName)

			rows, err := conn.Query(ctx, query, startTime, endTime)
			if err != nil {
				continue // Skip if analysis fails
			}
			defer rows.Close()

			var children []models.LogSchema
			for rows.Next() {
				var key string
				if err := rows.Scan(&key); err != nil {
					continue
				}

				children = append(children, models.LogSchema{
					Name:     fmt.Sprintf("%s.%s", field.Name, key),
					Type:     "String", // Map values are strings in our case
					Path:     append(field.Path, key),
					IsNested: true,
					Parent:   field.Name,
					Children: nil,
				})
			}

			schema[i].Children = children
		}
	}

	return schema, nil
}

// Helper function to get column names from schema
func getColumnNames(schema []models.LogSchema) []string {
	names := make([]string, len(schema))
	for i, field := range schema {
		names[i] = field.Name
	}
	return names
}

// Add this new method
func (r *LogRepository) getBaseSchema(ctx context.Context, conn driver.Conn, tableName string) ([]models.LogSchema, error) {
	query := `
		SELECT
			name,
			type,
			position
		FROM system.columns
		WHERE table = ?
		ORDER BY position
	`
	rows, err := conn.Query(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get table schema: %w", err)
	}
	defer rows.Close()

	var schema []models.LogSchema
	for rows.Next() {
		var (
			name     string
			dataType string
			position uint64
		)
		if err := rows.Scan(&name, &dataType, &position); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		path := strings.Split(name, ".")
		isNested := len(path) > 1 ||
			strings.Contains(dataType, "Map") ||
			strings.Contains(dataType, "Array") ||
			strings.Contains(dataType, "JSON")

		schema = append(schema, models.LogSchema{
			Name:     name,
			Type:     dataType,
			Path:     path,
			IsNested: isNested,
		})
	}

	return schema, nil
}

// ExecuteRawQuery executes a raw SQL query and returns the results
func (r *LogRepository) ExecuteRawQuery(ctx context.Context, sourceID string, query string, args []interface{}) (*models.LogResponse, error) {
	// Get connection from pool
	conn, err := r.pool.GetConnection(sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Execute the query
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Parse the results
	var logs []*models.Log
	for rows.Next() {
		log := &models.Log{}
		err := rows.Scan(
			&log.ID,
			&log.Timestamp,
			&log.TraceID,
			&log.SpanID,
			&log.TraceFlags,
			&log.SeverityText,
			&log.SeverityNumber,
			&log.ServiceName,
			&log.Namespace,
			&log.Body,
			&log.LogAttributes,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return &models.LogResponse{
		Logs: logs,
	}, nil
}
