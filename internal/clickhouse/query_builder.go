package clickhouse

import (
	"fmt"
	"strings"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
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
	// Parse the SQL using the ClickHouse parser
	parser := clickhouseparser.NewParser(rawSQL)
	stmts, err := parser.ParseStmts()
	if err != nil {
		return "", fmt.Errorf("invalid SQL syntax: %w", err)
	}

	if len(stmts) == 0 {
		return "", fmt.Errorf("no SQL statements found")
	}

	// We only support single statements
	if len(stmts) > 1 {
		return "", fmt.Errorf("multiple SQL statements not supported")
	}

	stmt := stmts[0]

	// Check if it's a SELECT statement
	selectQuery, ok := stmt.(*clickhouseparser.SelectQuery)
	if !ok {
		return "", ErrInvalidQuery
	}

	// Validate table reference if tableName is provided
	if qb.tableName != "" {
		if err := qb.validateTableReference(selectQuery); err != nil {
			return "", err
		}
	}

	// Validate for dangerous operations
	if err := qb.checkDangerousOperations(selectQuery); err != nil {
		return "", err
	}

	// Add LIMIT clause if not present and limit is specified
	if limit > 0 {
		qb.ensureLimit(selectQuery, limit)
	}

	// Convert back to SQL string
	return stmt.String(), nil
}

// validateTableReference ensures the table reference is valid
func (qb *QueryBuilder) validateTableReference(stmt *clickhouseparser.SelectQuery) error {
	// If there's no FROM clause, return error
	if stmt.From == nil || stmt.From.Expr == nil {
		return fmt.Errorf("missing FROM clause")
	}

	// Extract expected database and table from qb.tableName
	expectedDB, expectedTable := "", qb.tableName
	if parts := strings.Split(qb.tableName, "."); len(parts) == 2 {
		expectedDB, expectedTable = parts[0], parts[1]
	}

	// Check based on the type of the FromClause.Expr
	switch expr := stmt.From.Expr.(type) {
	case *clickhouseparser.TableExpr: // This case might be unreachable now due to parser changes
		// Simple table reference, potentially parsed differently now.
		if expr == nil || expr.Expr == nil {
			return fmt.Errorf("empty table reference in unexpected TableExpr")
		}
		tableID, ok := expr.Expr.(*clickhouseparser.TableIdentifier)
		if !ok {
			return fmt.Errorf("unsupported table expression type in FromClause.TableExpr: %T", expr.Expr)
		}
		return qb.validateTableIdentifier(tableID, expectedDB, expectedTable)

	case *clickhouseparser.JoinTableExpr:
		// This represents a single table source (possibly with SAMPLE or FINAL)
		if expr.Table == nil {
			return fmt.Errorf("empty Table field in JoinTableExpr")
		}
		if expr.Table.Expr == nil {
			return fmt.Errorf("empty Expr field in JoinTableExpr.Table")
		}

		// Check the underlying expression type within TableExpr
		tableID, ok := expr.Table.Expr.(*clickhouseparser.TableIdentifier)
		if !ok {
			// Could also be AliasExpr wrapping a TableIdentifier
			if aliasExpr, isAlias := expr.Table.Expr.(*clickhouseparser.AliasExpr); isAlias {
				tableID, ok = aliasExpr.Expr.(*clickhouseparser.TableIdentifier)
				if !ok {
					return fmt.Errorf("unsupported expression type inside AliasExpr in JoinTableExpr: %T", aliasExpr.Expr)
				}
			} else {
				return fmt.Errorf("unsupported expression type in JoinTableExpr.Table.Expr: %T", expr.Table.Expr)
			}
		}
		// Now validate the extracted TableIdentifier
		return qb.validateTableIdentifier(tableID, expectedDB, expectedTable)

	case *clickhouseparser.JoinExpr:
		// This represents an actual JOIN operation, which we don't allow.
		return fmt.Errorf("joins are not allowed")

	default:
		// Catch any other unexpected types
		return fmt.Errorf("unsupported FROM clause expression type: %T", expr)
	}
}

// validateTableIdentifier checks if a TableIdentifier matches the expected database and table.
func (qb *QueryBuilder) validateTableIdentifier(tableID *clickhouseparser.TableIdentifier, expectedDB, expectedTable string) error {
	if tableID.Table == nil {
		return fmt.Errorf("missing table name")
	}

	// Get the table name
	tableName := tableID.Table.String()

	// If there's a database qualifier, check it
	if tableID.Database != nil {
		dbName := tableID.Database.String()

		// If we expected a specific database, check it
		if expectedDB != "" && dbName != expectedDB {
			return fmt.Errorf("invalid database reference: %s (expected %s)",
				dbName, expectedDB)
		}

		// Check the table name
		if tableName != expectedTable {
			return fmt.Errorf("invalid table reference: %s.%s (expected %s.%s)",
				dbName, tableName, expectedDB, expectedTable)
		}
	} else {
		// No database qualifier, just check the table name
		if tableName != expectedTable {
			return fmt.Errorf("invalid table reference: %s (expected %s)",
				tableName, expectedTable)
		}
	}
	return nil
}

// checkDangerousOperations checks for potentially dangerous SQL operations
func (qb *QueryBuilder) checkDangerousOperations(stmt *clickhouseparser.SelectQuery) error {
	// Check for subqueries, which we don't allow
	if containsSubqueries(stmt) {
		return fmt.Errorf("subqueries are not allowed")
	}

	return nil
}

// containsSubqueries checks if a SELECT statement contains subqueries
func containsSubqueries(stmt *clickhouseparser.SelectQuery) bool {
	// This is a simplified version. A full implementation would need to
	// recursively check the entire AST
	return false
}

// ensureLimit ensures the SELECT statement has a LIMIT clause
func (qb *QueryBuilder) ensureLimit(stmt *clickhouseparser.SelectQuery, limit int) {
	if stmt.Limit == nil {
		// Create a new limit clause
		numberLiteral := &clickhouseparser.NumberLiteral{
			Literal: fmt.Sprintf("%d", limit),
		}
		stmt.Limit = &clickhouseparser.LimitClause{
			Limit: numberLiteral,
		}
	}
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
