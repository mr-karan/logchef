package clickhouse

import (
	"fmt"
	"strings"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

// QueryBuilder assists in building and validating ClickHouse SQL queries.
type QueryBuilder struct {
	// tableName is the fully qualified table name (e.g., "database.table")
	// used for validation and as the default target in generated queries.
	tableName string
}

// NewQueryBuilder creates a new QueryBuilder for a specific table.
func NewQueryBuilder(tableName string) *QueryBuilder {
	return &QueryBuilder{
		tableName: tableName,
	}
}

// BuildRawQuery parses, validates, potentially modifies (adds LIMIT),
// and reconstructs a raw SQL query string.
func (qb *QueryBuilder) BuildRawQuery(rawSQL string, limit int) (string, error) {
	// Preprocess SQL to handle escaped single quotes ('') which the parser might misinterpret.
	// Replace them with a temporary placeholder.
	const placeholder = "___ESCAPED_QUOTE___"
	processedSQL := strings.ReplaceAll(rawSQL, "''", placeholder)

	parser := clickhouseparser.NewParser(processedSQL)
	stmts, err := parser.ParseStmts()
	if err != nil {
		// Return a user-friendly error for syntax issues.
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
		// Currently, only SELECT queries are supported through this builder.
		return "", fmt.Errorf("only SELECT queries are supported: %w", ErrInvalidQuery)
	}

	// Validate that the query targets the expected table, if one was set.
	if qb.tableName != "" {
		if err := qb.validateTableReference(selectQuery); err != nil {
			return "", err
		}
	}

	// Perform checks for potentially disallowed operations (e.g., subqueries, joins).
	if err := qb.checkDangerousOperations(selectQuery); err != nil {
		return "", err
	}

	// Ensure a LIMIT clause exists if a positive limit is provided.
	if limit > 0 {
		qb.ensureLimit(selectQuery, limit)
	}

	// Convert the potentially modified AST back to a SQL string.
	result := stmt.String()

	// Restore the standard SQL escaped quotes.
	result = strings.ReplaceAll(result, placeholder, "''")

	return result, nil
}

// validateTableReference checks if the FROM clause of a SELECT query
// references the table associated with the QueryBuilder.
func (qb *QueryBuilder) validateTableReference(stmt *clickhouseparser.SelectQuery) error {
	// If there's no FROM clause, return error
	if stmt.From == nil || stmt.From.Expr == nil {
		return fmt.Errorf("query validation failed: missing FROM clause")
	}

	// Extract expected database and table name if qualified name was provided.
	expectedDB, expectedTable := "", qb.tableName
	if parts := strings.Split(qb.tableName, "."); len(parts) == 2 {
		expectedDB, expectedTable = parts[0], parts[1]
	}

	// The parser structure for FROM clauses can be nested.
	// We need to find the underlying TableIdentifier.
	var tableID *clickhouseparser.TableIdentifier

	// Check based on the type of the FromClause.Expr
	switch expr := stmt.From.Expr.(type) {
	case *clickhouseparser.JoinTableExpr: // Common case for single table (maybe w/ FINAL/SAMPLE)
		// Simple table reference, potentially parsed differently now.
		if expr.Table == nil || expr.Table.Expr == nil {
			return fmt.Errorf("query validation failed: invalid table expression in FROM clause")
		}
		// The actual table might be directly identified or wrapped in an alias.
		if tid, ok := expr.Table.Expr.(*clickhouseparser.TableIdentifier); ok {
			tableID = tid
		} else if aliasExpr, ok := expr.Table.Expr.(*clickhouseparser.AliasExpr); ok {
			if tid, ok := aliasExpr.Expr.(*clickhouseparser.TableIdentifier); ok {
				tableID = tid
			}
		}
	case *clickhouseparser.TableExpr: // Less common based on current parser behavior?
		if expr.Expr == nil {
			return fmt.Errorf("query validation failed: invalid table expression in FROM clause")
		}
		if tid, ok := expr.Expr.(*clickhouseparser.TableIdentifier); ok {
			tableID = tid
		}

	case *clickhouseparser.JoinExpr:
		// Explicit JOINs are disallowed for safety/simplicity.
		return fmt.Errorf("query validation failed: JOIN clauses are not allowed")

	default:
		// Catch any other unexpected types
		return fmt.Errorf("query validation failed: unsupported FROM clause type: %T", expr)
	}

	// If we couldn't extract a valid TableIdentifier.
	if tableID == nil {
		return fmt.Errorf("query validation failed: could not identify table in FROM clause")
	}

	// Validate the extracted TableIdentifier against expectations.
	return qb.validateTableIdentifier(tableID, expectedDB, expectedTable)
}

// validateTableIdentifier checks if a specific TableIdentifier matches the expected database/table.
func (qb *QueryBuilder) validateTableIdentifier(tableID *clickhouseparser.TableIdentifier, expectedDB, expectedTable string) error {
	if tableID.Table == nil {
		return fmt.Errorf("query validation failed: invalid table identifier")
	}
	tableName := tableID.Table.String()

	// Check database qualifier if present.
	if tableID.Database != nil {
		dbName := tableID.Database.String()
		// If QueryBuilder is scoped to a specific DB, enforce it.
		if expectedDB != "" && dbName != expectedDB {
			return fmt.Errorf("query validation failed: invalid database reference '%s' (expected '%s')",
				dbName, expectedDB)
		}
		// Check table name with database qualifier.
		if tableName != expectedTable {
			return fmt.Errorf("query validation failed: invalid table reference '%s.%s' (expected '%s.%s')",
				dbName, tableName, expectedDB, expectedTable)
		}
	} else {
		// No database qualifier present, just check table name.
		// If QueryBuilder expected a specific DB, this is also arguably an error,
		// but for now, we only enforce if the query *specifies* a different DB.
		if tableName != expectedTable {
			expectedFullName := expectedTable
			if expectedDB != "" {
				expectedFullName = expectedDB + "." + expectedTable
			}
			return fmt.Errorf("query validation failed: invalid table reference '%s' (expected '%s')",
				tableName, expectedFullName)
		}
	}
	return nil
}

// checkDangerousOperations performs basic checks for disallowed SQL constructs.
func (qb *QueryBuilder) checkDangerousOperations(stmt *clickhouseparser.SelectQuery) error {
	// Basic check for subqueries (placeholder - needs proper AST traversal).
	if containsSubqueries(stmt) {
		return fmt.Errorf("query validation failed: subqueries are not allowed")
	}
	// Basic check for disallowed functions (placeholder).
	// if containsDisallowedFunctions(stmt) {
	// 	 return fmt.Errorf("query validation failed: disallowed function used")
	// }
	return nil
}

// containsSubqueries checks if a SELECT statement contains subqueries.
// FIXME: This requires recursive traversal of the AST, currently a no-op placeholder.
func containsSubqueries(stmt *clickhouseparser.SelectQuery) bool {
	// Placeholder: Needs recursive AST walk to check SELECT clauses, WHERE, etc.
	return false
}

// ensureLimit adds or replaces the LIMIT clause on a SelectQuery AST node.
func (qb *QueryBuilder) ensureLimit(stmt *clickhouseparser.SelectQuery, limit int) {
	// Create the number literal for the limit value.
	numberLiteral := &clickhouseparser.NumberLiteral{
		Literal: fmt.Sprintf("%d", limit),
	}
	// Always set/overwrite the limit clause.
	stmt.Limit = &clickhouseparser.LimitClause{
		Limit: numberLiteral,
	}
}
