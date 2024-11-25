package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/models"
)

// LogRepository handles log querying operations
type LogRepository struct {
	pool *ConnectionPool
}

func NewLogRepository(pool *ConnectionPool) *LogRepository {
	return &LogRepository{pool: pool}
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

	if params.StartTime != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, params.StartTime)
	}
	if params.EndTime != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, params.EndTime)
	}
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

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

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
			&log.SeverityNumber, // now int32
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

	hasMore := uint64(params.Offset + len(logs)) < totalCount

	return &models.LogResponse{
		Logs:       logs,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}
