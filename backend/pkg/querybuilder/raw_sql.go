package querybuilder

import (
	"fmt"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// RawSQLBuilder handles raw SQL query building and validation
type RawSQLBuilder struct {
	options Options
}

// NewRawSQLBuilder creates a new RawSQLBuilder instance
func NewRawSQLBuilder(opts Options) *RawSQLBuilder {
	return &RawSQLBuilder{
		options: opts,
	}
}

// Build validates and builds the final SQL query
func (b *RawSQLBuilder) Build() (*Query, error) {
	if err := b.validateSelectQuery(); err != nil {
		return nil, err
	}

	// Parse the query to manipulate it
	stmt, err := sqlparser.Parse(b.options.RawSQL)
	if err != nil {
		return nil, fmt.Errorf("invalid SQL syntax: %v", err)
	}

	// Type assert to Select statement
	selectStmt, ok := stmt.(*sqlparser.Select)
	if !ok {
		return nil, fmt.Errorf("only SELECT queries are allowed")
	}

	// Add LIMIT clause if not present
	if b.options.Limit > 0 && selectStmt.Limit == nil {
		selectStmt.Limit = &sqlparser.Limit{
			Rowcount: sqlparser.NewIntVal([]byte(fmt.Sprintf("%d", b.options.Limit))),
		}
	}

	// Convert back to SQL string
	finalSQL := sqlparser.String(selectStmt)

	return &Query{
		SQL: finalSQL,
	}, nil
}

// validateSelectQuery performs thorough validation of the SQL query
func (b *RawSQLBuilder) validateSelectQuery() error {
	// Parse the query
	stmt, err := sqlparser.Parse(b.options.RawSQL)
	if err != nil {
		return fmt.Errorf("invalid SQL syntax: %v", err)
	}

	// Ensure it's a SELECT statement
	selectStmt, ok := stmt.(*sqlparser.Select)
	if !ok {
		return fmt.Errorf("only SELECT queries are allowed")
	}

	// Validate table reference
	if err := b.validateTableReference(selectStmt.From); err != nil {
		return err
	}

	// Validate WHERE clause for dangerous operations
	if selectStmt.Where != nil {
		whereStr := sqlparser.String(selectStmt.Where)
		if containsDangerousOperations(whereStr) {
			return fmt.Errorf("dangerous operations detected in WHERE clause")
		}
	}

	return nil
}

// validateTableReference ensures the table reference is valid
func (b *RawSQLBuilder) validateTableReference(tableExprs sqlparser.TableExprs) error {
	for _, tableExpr := range tableExprs {
		switch expr := tableExpr.(type) {
		case *sqlparser.AliasedTableExpr:
			switch tableExpr := expr.Expr.(type) {
			case sqlparser.TableName:
				tableName := fmt.Sprintf("%s.%s", tableExpr.Qualifier.String(), tableExpr.Name.String())
				if tableName != b.options.TableName {
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
