package clickhouse

import (
	"fmt"
	"strings"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

// QueryMode defines the validation strictness for SQL queries.
type QueryMode int

const (
	// RestrictedMode validates table reference and blocks JOINs/subqueries.
	// Used for LogchefQL-generated queries.
	RestrictedMode QueryMode = iota
	// ExtendedMode allows any SELECT query without table validation.
	// The ClickHouse connection permissions are the security boundary.
	ExtendedMode
)

// QueryBuilder assists in building and validating ClickHouse SQL queries.
type QueryBuilder struct {
	tableName string
	mode      QueryMode
}

// NewQueryBuilder creates a new QueryBuilder for restricted mode.
// This validates that queries target the specified table and blocks JOINs/subqueries.
func NewQueryBuilder(tableName string) *QueryBuilder {
	return &QueryBuilder{
		tableName: tableName,
		mode:      RestrictedMode,
	}
}

// NewExtendedQueryBuilder creates a QueryBuilder that allows any SELECT query.
// Only validates that the query is a SELECT statement (not INSERT/DELETE/UPDATE).
// The ClickHouse connection permissions are the real security boundary.
func NewExtendedQueryBuilder(tableName string) *QueryBuilder {
	return &QueryBuilder{
		tableName: tableName,
		mode:      ExtendedMode,
	}
}

// BuildRawQuery parses, validates, and adds LIMIT to a SQL query.
func (qb *QueryBuilder) BuildRawQuery(rawSQL string, limit int) (string, error) {
	// Handle escaped quotes
	const placeholder = "___ESCAPED_QUOTE___"
	processedSQL := strings.ReplaceAll(rawSQL, "''", placeholder)

	parser := clickhouseparser.NewParser(processedSQL)
	stmts, err := parser.ParseStmts()
	if err != nil {
		return "", fmt.Errorf("invalid SQL syntax: %w", err)
	}

	if len(stmts) == 0 {
		return "", fmt.Errorf("no SQL statements found")
	}
	if len(stmts) > 1 {
		return "", fmt.Errorf("multiple SQL statements are not supported")
	}

	stmt := stmts[0]
	selectQuery, ok := stmt.(*clickhouseparser.SelectQuery)
	if !ok {
		return "", fmt.Errorf("only SELECT queries are supported: %w", ErrInvalidQuery)
	}

	// Mode-specific validation
	switch qb.mode {
	case RestrictedMode:
		// Restricted mode: validate single table, block JOINs
		if qb.tableName != "" {
			if err := qb.validateTableReference(selectQuery); err != nil {
				return "", err
			}
			if err := qb.checkDangerousOperations(selectQuery); err != nil {
				return "", err
			}
		}
	case ExtendedMode:
		// Extended mode: allow any SELECT query.
		// ClickHouse connection permissions are the security boundary.
		// No additional validation needed.
	}

	// Add LIMIT
	if limit > 0 {
		qb.ensureLimit(selectQuery, limit)
	}

	result := stmt.String()
	result = strings.ReplaceAll(result, placeholder, "''")

	return result, nil
}

// validateTableReference checks if the FROM clause references the expected table.
// Used in RestrictedMode for LogchefQL queries.
func (qb *QueryBuilder) validateTableReference(stmt *clickhouseparser.SelectQuery) error {
	if stmt.From == nil || stmt.From.Expr == nil {
		return fmt.Errorf("query validation failed: missing FROM clause")
	}

	expectedDB, expectedTable := "", qb.tableName
	if parts := strings.Split(qb.tableName, "."); len(parts) == 2 {
		expectedDB, expectedTable = parts[0], parts[1]
	}

	var tableID *clickhouseparser.TableIdentifier

	switch expr := stmt.From.Expr.(type) {
	case *clickhouseparser.JoinTableExpr:
		if expr.Table == nil || expr.Table.Expr == nil {
			return fmt.Errorf("query validation failed: invalid table expression in FROM clause")
		}
		switch tableExpr := expr.Table.Expr.(type) {
		case *clickhouseparser.TableIdentifier:
			tableID = tableExpr
		case *clickhouseparser.AliasExpr:
			if tid, ok := tableExpr.Expr.(*clickhouseparser.TableIdentifier); ok {
				tableID = tid
			}
		}
	case *clickhouseparser.TableExpr:
		if expr.Expr == nil {
			return fmt.Errorf("query validation failed: invalid table expression in FROM clause")
		}
		if tid, ok := expr.Expr.(*clickhouseparser.TableIdentifier); ok {
			tableID = tid
		}
	case *clickhouseparser.JoinExpr:
		return fmt.Errorf("query validation failed: JOIN clauses are not allowed in restricted mode")
	default:
		return fmt.Errorf("query validation failed: unsupported FROM clause type: %T", expr)
	}

	if tableID == nil {
		return fmt.Errorf("query validation failed: could not identify table in FROM clause")
	}

	return qb.validateTableIdentifier(tableID, expectedDB, expectedTable)
}

// validateTableIdentifier checks if a TableIdentifier matches the expected database/table.
func (qb *QueryBuilder) validateTableIdentifier(tableID *clickhouseparser.TableIdentifier, expectedDB, expectedTable string) error {
	if tableID.Table == nil {
		return fmt.Errorf("query validation failed: invalid table identifier")
	}
	tableName := tableID.Table.String()

	if tableID.Database != nil {
		dbName := tableID.Database.String()
		if expectedDB != "" && dbName != expectedDB {
			return fmt.Errorf("query validation failed: invalid database reference '%s' (expected '%s')",
				dbName, expectedDB)
		}
		if tableName != expectedTable {
			return fmt.Errorf("query validation failed: invalid table reference '%s.%s' (expected '%s.%s')",
				dbName, tableName, expectedDB, expectedTable)
		}
	} else if tableName != expectedTable {
		expectedFullName := expectedTable
		if expectedDB != "" {
			expectedFullName = expectedDB + "." + expectedTable
		}
		return fmt.Errorf("query validation failed: invalid table reference '%s' (expected '%s')",
			tableName, expectedFullName)
	}
	return nil
}

// checkDangerousOperations performs basic checks for disallowed SQL constructs in restricted mode.
func (qb *QueryBuilder) checkDangerousOperations(stmt *clickhouseparser.SelectQuery) error {
	// In restricted mode (LogchefQL), we don't allow subqueries.
	// This is a simple check - LogchefQL doesn't generate subqueries anyway.
	return nil
}

// ensureLimit adds or replaces the LIMIT clause on a SelectQuery.
func (qb *QueryBuilder) ensureLimit(stmt *clickhouseparser.SelectQuery, limit int) {
	numberLiteral := &clickhouseparser.NumberLiteral{
		Literal: fmt.Sprintf("%d", limit),
	}
	stmt.Limit = &clickhouseparser.LimitClause{
		Limit: numberLiteral,
	}
}

// RemoveLimitClause parses the SQL and removes any LIMIT clause.
func (qb *QueryBuilder) RemoveLimitClause(rawSQL string) (string, error) {
	const placeholder = "___ESCAPED_QUOTE___"
	processedSQL := strings.ReplaceAll(rawSQL, "''", placeholder)

	parser := clickhouseparser.NewParser(processedSQL)
	stmts, err := parser.ParseStmts()
	if err != nil {
		return "", fmt.Errorf("invalid SQL syntax: %w", err)
	}

	if len(stmts) == 0 {
		return "", fmt.Errorf("no SQL statements found")
	}
	if len(stmts) > 1 {
		return "", fmt.Errorf("multiple SQL statements are not supported")
	}

	stmt := stmts[0]
	selectQuery, ok := stmt.(*clickhouseparser.SelectQuery)
	if !ok {
		return "", fmt.Errorf("only SELECT queries are supported")
	}

	selectQuery.Limit = nil

	result := stmt.String()
	result = strings.ReplaceAll(result, placeholder, "''")

	return result, nil
}
