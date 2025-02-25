package clickhouse

import (
	"fmt"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// QueryBuilder provides methods to build ClickHouse queries
type QueryBuilder struct {
	tableName string
}

// NewQueryBuilder creates a new query builder for a specific table
func NewQueryBuilder(tableName string) *QueryBuilder {
	return &QueryBuilder{
		tableName: tableName,
	}
}

// BuildRawQuery builds and validates a raw SQL query
func (qb *QueryBuilder) BuildRawQuery(rawSQL string, limit int) (string, error) {
	// Parse the query to validate and manipulate it
	stmt, err := sqlparser.Parse(rawSQL)
	if err != nil {
		return "", fmt.Errorf("invalid SQL syntax: %w", err)
	}

	// Ensure it's a SELECT statement
	selectStmt, ok := stmt.(*sqlparser.Select)
	if !ok {
		return "", ErrInvalidQuery
	}

	// Validate table reference if tableName is provided
	if qb.tableName != "" {
		if err := qb.validateTableReference(selectStmt.From); err != nil {
			return "", err
		}
	}

	// Validate WHERE clause for dangerous operations
	if selectStmt.Where != nil {
		whereStr := sqlparser.String(selectStmt.Where)
		if containsDangerousOperations(whereStr) {
			return "", fmt.Errorf("dangerous operations detected in WHERE clause")
		}
	}

	// Add LIMIT clause if not present and limit is specified
	if limit > 0 && selectStmt.Limit == nil {
		selectStmt.Limit = &sqlparser.Limit{
			Rowcount: sqlparser.NewIntVal([]byte(fmt.Sprintf("%d", limit))),
		}
	}

	// Convert back to SQL string
	return sqlparser.String(selectStmt), nil
}

// validateTableReference ensures the table reference is valid
func (qb *QueryBuilder) validateTableReference(tableExprs sqlparser.TableExprs) error {
	for _, tableExpr := range tableExprs {
		switch expr := tableExpr.(type) {
		case *sqlparser.AliasedTableExpr:
			switch tableExpr := expr.Expr.(type) {
			case sqlparser.TableName:
				tableName := fmt.Sprintf("%s.%s", tableExpr.Qualifier.String(), tableExpr.Name.String())
				if tableName != qb.tableName && qb.tableName != "" {
					return fmt.Errorf("invalid table reference: %s", tableName)
				}
			case *sqlparser.Subquery:
				return fmt.Errorf("subqueries are not allowed")
			}
		case *sqlparser.JoinTableExpr:
			return fmt.Errorf("joins are not allowed")
		}
	}
	return nil
}

// containsDangerousOperations checks for potentially dangerous SQL operations
func containsDangerousOperations(sql string) bool {
	sql = strings.ToUpper(sql)
	dangerousKeywords := []string{
		"DROP", "DELETE", "TRUNCATE", "ALTER", "SYSTEM",
		"SETTINGS", "CREATE", "INSERT", "UPDATE",
	}

	for _, keyword := range dangerousKeywords {
		if strings.Contains(sql, keyword) {
			return true
		}
	}

	return false
}

// BuildTimeSeriesQuery builds a query for time series data
func (qb *QueryBuilder) BuildTimeSeriesQuery(startTime, endTime int64, interval string) string {
	return fmt.Sprintf(`
		SELECT
			toUnixTimestamp(toStartOf%s(timestamp)) * 1000 as ts,
			count() as count
		FROM %s
		WHERE timestamp >= toDateTime(%d) AND timestamp <= toDateTime(%d)
		GROUP BY ts
		ORDER BY ts ASC
	`, interval, qb.tableName, startTime, endTime)
}

// BuildLogContextQueries builds queries for log context
func (qb *QueryBuilder) BuildLogContextQueries(targetTime int64, beforeLimit, afterLimit int) (before, target, after string) {
	before = fmt.Sprintf(`
		SELECT *
		FROM %s
		WHERE timestamp < toDateTime(%d)
		ORDER BY timestamp DESC
		LIMIT %d
	`, qb.tableName, targetTime, beforeLimit)

	target = fmt.Sprintf(`
		SELECT *
		FROM %s
		WHERE timestamp = toDateTime(%d)
		ORDER BY timestamp ASC
	`, qb.tableName, targetTime)

	after = fmt.Sprintf(`
		SELECT *
		FROM %s
		WHERE timestamp > toDateTime(%d)
		ORDER BY timestamp ASC
		LIMIT %d
	`, qb.tableName, targetTime, afterLimit)

	return before, target, after
}

// BuildContextQuery builds a unified context query that returns logs before and after a timestamp
func (qb *QueryBuilder) BuildContextQuery(targetTime int64, beforeLimit, afterLimit int) string {
	return fmt.Sprintf(`
		WITH target_timestamp AS (
			SELECT %d as ts_millis
		)
		SELECT * FROM (
			SELECT *
			FROM %s
			WHERE timestamp < fromUnixTimestamp64Milli((SELECT ts_millis FROM target_timestamp))
			ORDER BY timestamp DESC
			LIMIT %d

			UNION ALL

			SELECT *
			FROM %s
			WHERE timestamp = fromUnixTimestamp64Milli((SELECT ts_millis FROM target_timestamp))

			UNION ALL

			SELECT *
			FROM %s
			WHERE timestamp > fromUnixTimestamp64Milli((SELECT ts_millis FROM target_timestamp))
			ORDER BY timestamp ASC
			LIMIT %d
		) ORDER BY timestamp ASC`,
		targetTime,
		qb.tableName,
		beforeLimit,
		qb.tableName,
		qb.tableName,
		afterLimit,
	)
}
