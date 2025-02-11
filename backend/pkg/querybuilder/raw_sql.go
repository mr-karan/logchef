package querybuilder

import (
	"fmt"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// RawSQLBuilder builds raw SQL queries
type RawSQLBuilder struct {
	tableName string
	rawSQL    string
	limit     int
}

// NewRawSQLBuilder creates a new RawSQLBuilder
func NewRawSQLBuilder(tableName string, rawSQL string, limit int) *RawSQLBuilder {
	return &RawSQLBuilder{
		tableName: tableName,
		rawSQL:    rawSQL,
		limit:     limit,
	}
}

// Build builds the query
func (b *RawSQLBuilder) Build() (*Query, error) {
	// Replace table name placeholder if present
	query := strings.ReplaceAll(b.rawSQL, "{{table}}", b.tableName)

	// Validate the query
	if err := b.validateQuery(query); err != nil {
		return nil, err
	}

	// Check if query already has a LIMIT clause
	hasLimit := strings.Contains(strings.ToUpper(query), "LIMIT")
	if !hasLimit {
		// Append LIMIT clause
		query = fmt.Sprintf("%s\nLIMIT %d", query, b.limit)
	}

	return &Query{
		SQL:  query,
		Args: nil,
	}, nil
}

// validateQuery validates the raw SQL query
func (b *RawSQLBuilder) validateQuery(query string) error {
	// Basic validation - ensure it's a SELECT query
	trimmedQuery := strings.TrimSpace(strings.ToUpper(query))
	if !strings.HasPrefix(trimmedQuery, "SELECT") {
		return fmt.Errorf("only SELECT queries are allowed")
	}

	// Check for dangerous operations
	dangerousKeywords := []string{
		"DROP", "DELETE", "TRUNCATE", "ALTER", "CREATE",
		"INSERT", "UPDATE", "RENAME", "REPLACE",
	}

	for _, keyword := range dangerousKeywords {
		if strings.Contains(trimmedQuery, keyword) {
			return fmt.Errorf("dangerous operation detected: %s", keyword)
		}
	}

	return nil
}

// validateSelectQuery checks if the query is a valid SELECT query
func (b *RawSQLBuilder) validateSelectQuery() error {
	stmt, err := sqlparser.Parse(b.rawSQL)
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
		if b.tableName != "" {
			tableRef := sqlparser.String(tableName)
			// Remove backticks from table reference for comparison
			tableRef = strings.ReplaceAll(tableRef, "`", "")
			if !strings.EqualFold(tableRef, b.tableName) {
				return fmt.Errorf("invalid table reference: %s, expected: %s", tableRef, b.tableName)
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
