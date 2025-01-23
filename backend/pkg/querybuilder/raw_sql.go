package querybuilder

import (
	"fmt"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// RawSQLBuilder builds queries from raw SQL
type RawSQLBuilder struct {
	SQL      string
	TableRef string // Full table reference (e.g., "database.table")
}

// NewRawSQLBuilder creates a new RawSQLBuilder instance
func NewRawSQLBuilder(tableRef string, sql string) *RawSQLBuilder {
	return &RawSQLBuilder{
		SQL:      sql,
		TableRef: tableRef,
	}
}

// validateSelectQuery checks if the query is a valid SELECT query
func (b *RawSQLBuilder) validateSelectQuery() error {
	stmt, err := sqlparser.Parse(b.SQL)
	if err != nil {
		return fmt.Errorf("invalid SQL syntax: %w", err)
	}

	selectStmt, ok := stmt.(*sqlparser.Select)
	if !ok {
		return fmt.Errorf("only SELECT queries are allowed")
	}

	// Check for dangerous operations in WHERE clause
	if selectStmt.Where != nil {
		if containsDangerousOperations(sqlparser.String(selectStmt.Where)) {
			return fmt.Errorf("dangerous operations detected")
		}
	}

	// Validate table names
	for _, tableExpr := range selectStmt.From {
		aliasTableExpr, ok := tableExpr.(*sqlparser.AliasedTableExpr)
		if !ok {
			continue
		}

		tableName, ok := aliasTableExpr.Expr.(sqlparser.TableName)
		if !ok {
			continue
		}

		// Check if the table being queried matches our table reference
		if b.TableRef != "" {
			tableRef := sqlparser.String(tableName)
			// Remove backticks from table reference for comparison
			tableRef = strings.ReplaceAll(tableRef, "`", "")
			if !strings.EqualFold(tableRef, b.TableRef) {
				return fmt.Errorf("invalid table reference: %s, expected: %s", tableRef, b.TableRef)
			}
		}
	}

	return nil
}

// containsDangerousOperations checks for potentially dangerous SQL operations
func containsDangerousOperations(sql string) bool {
	sql = strings.ToUpper(sql)
	dangerousKeywords := []string{
		"DROP", "DELETE", "TRUNCATE", "ALTER", "UPDATE", "INSERT",
		"SYSTEM", "SETTINGS", "GRANT", "REVOKE",
	}

	for _, keyword := range dangerousKeywords {
		if strings.Contains(sql, keyword) {
			return true
		}
	}

	return false
}

// Build implements the Builder interface for RawSQLBuilder
func (b *RawSQLBuilder) Build() (*Query, error) {
	// First check if it's a SELECT query
	stmt, err := sqlparser.Parse(b.SQL)
	if err != nil {
		return nil, fmt.Errorf("SQL validation failed: %w", err)
	}

	if _, ok := stmt.(*sqlparser.Select); !ok {
		return nil, fmt.Errorf("SQL validation failed: only SELECT queries are allowed")
	}

	// Then check for dangerous operations
	if containsDangerousOperations(b.SQL) {
		return nil, fmt.Errorf("SQL validation failed: dangerous operations detected")
	}

	// Finally validate the query details
	if err := b.validateSelectQuery(); err != nil {
		return nil, fmt.Errorf("SQL validation failed: %w", err)
	}

	// Parse the original query
	stmt, err = sqlparser.Parse(b.SQL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SQL: %w", err)
	}

	selectStmt := stmt.(*sqlparser.Select)

	// Create a new SELECT statement that wraps the original query
	wrappedSQL := fmt.Sprintf("SELECT toJSONString(tuple(*)) as raw_json FROM (%s)", sqlparser.String(selectStmt))

	return &Query{
		SQL:  wrappedSQL,
		Args: nil, // ClickHouse doesn't support query parameters
	}, nil
}
