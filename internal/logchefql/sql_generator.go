package logchefql

import (
	"fmt"
	"strings"
)

// SQLGenerator converts an AST into ClickHouse SQL
type SQLGenerator struct {
	schema *Schema
}

// NewSQLGenerator creates a new SQL generator with optional schema
func NewSQLGenerator(schema *Schema) *SQLGenerator {
	return &SQLGenerator{schema: schema}
}

// Generate converts an AST node to SQL WHERE clause conditions
func (g *SQLGenerator) Generate(node ASTNode) string {
	if node == nil {
		return ""
	}
	return g.visit(node)
}

// GenerateSelectClause generates the SELECT clause from select fields
func (g *SQLGenerator) GenerateSelectClause(selectFields []SelectField, defaultTimestampField string) string {
	if len(selectFields) == 0 {
		return "*"
	}

	var columns []string

	// Always include timestamp field first if specified
	if defaultTimestampField != "" {
		columns = append(columns, g.escapeIdentifier(defaultTimestampField))
	}

	// Generate column expressions for each select field
	for _, sf := range selectFields {
		expr := g.generateSelectFieldExpression(sf)
		if expr != "" {
			columns = append(columns, expr)
		}
	}

	if len(columns) == 0 {
		return "*"
	}

	return strings.Join(columns, ", ")
}

func (g *SQLGenerator) visit(node ASTNode) string {
	switch n := node.(type) {
	case *ExpressionNode:
		return g.visitExpression(n)
	case *LogicalNode:
		return g.visitLogical(n)
	case *GroupNode:
		return g.visitGroup(n)
	case *QueryNode:
		return g.visitQuery(n)
	default:
		return ""
	}
}

func (g *SQLGenerator) visitQuery(node *QueryNode) string {
	if node.Where != nil {
		return g.visit(node.Where)
	}
	return ""
}

func (g *SQLGenerator) visitExpression(node *ExpressionNode) string {
	// Check if we have a nested field
	if nf, ok := node.Key.(NestedField); ok {
		columnType := g.getColumnType(nf.Base)
		return g.generateNestedFieldAccess(nf.Base, nf.Path, columnType, node.Operator, node.Value)
	}

	// Handle simple field access
	key, ok := node.Key.(string)
	if !ok {
		return ""
	}

	column := g.escapeIdentifier(key)
	value := g.formatValue(node.Value, node.Operator)

	switch node.Operator {
	case OpRegex:
		return fmt.Sprintf("positionCaseInsensitive(%s, %s) > 0", column, value)
	case OpNotRegex:
		return fmt.Sprintf("positionCaseInsensitive(%s, %s) = 0", column, value)
	case OpEquals:
		return fmt.Sprintf("%s = %s", column, value)
	case OpNotEquals:
		return fmt.Sprintf("%s != %s", column, value)
	case OpGT:
		return fmt.Sprintf("%s > %s", column, value)
	case OpLT:
		return fmt.Sprintf("%s < %s", column, value)
	case OpGTE:
		return fmt.Sprintf("%s >= %s", column, value)
	case OpLTE:
		return fmt.Sprintf("%s <= %s", column, value)
	default:
		return ""
	}
}

func (g *SQLGenerator) visitLogical(node *LogicalNode) string {
	if len(node.Children) == 0 {
		return ""
	}

	if len(node.Children) == 1 {
		return g.visit(node.Children[0])
	}

	var conditions []string
	for _, child := range node.Children {
		sql := g.visit(child)
		if sql != "" {
			conditions = append(conditions, sql)
		}
	}

	if len(conditions) == 0 {
		return ""
	}

	if len(conditions) == 1 {
		return conditions[0]
	}

	// Wrap each condition in parentheses and join with operator
	var wrapped []string
	for _, c := range conditions {
		wrapped = append(wrapped, fmt.Sprintf("(%s)", c))
	}

	return strings.Join(wrapped, fmt.Sprintf(" %s ", node.Operator))
}

func (g *SQLGenerator) visitGroup(node *GroupNode) string {
	if len(node.Children) == 0 {
		return ""
	}

	if len(node.Children) == 1 {
		return g.visit(node.Children[0])
	}

	// Handle multiple expressions in a group - default to AND
	var conditions []string
	for _, child := range node.Children {
		sql := g.visit(child)
		if sql != "" {
			conditions = append(conditions, sql)
		}
	}

	if len(conditions) == 0 {
		return ""
	}

	if len(conditions) == 1 {
		return conditions[0]
	}

	return fmt.Sprintf("(%s)", strings.Join(conditions, " AND "))
}

func (g *SQLGenerator) escapeIdentifier(identifier string) string {
	// Escape backticks by doubling them
	escaped := strings.ReplaceAll(identifier, "`", "``")
	return fmt.Sprintf("`%s`", escaped)
}

func (g *SQLGenerator) escapeSQLString(value string) string {
	// Escape backslashes first, then single quotes
	result := strings.ReplaceAll(value, "\\", "\\\\")
	result = strings.ReplaceAll(result, "'", "''")
	result = strings.ReplaceAll(result, "\x00", "\\0")
	result = strings.ReplaceAll(result, "\r", "\\r")
	result = strings.ReplaceAll(result, "\n", "\\n")
	return result
}

func (g *SQLGenerator) formatValue(value interface{}, op Operator) string {
	if value == nil {
		return "NULL"
	}

	switch v := value.(type) {
	case bool:
		if v {
			return "1"
		}
		return "0"
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v)
	case string:
		escaped := g.escapeSQLString(v)
		return fmt.Sprintf("'%s'", escaped)
	default:
		escaped := g.escapeSQLString(fmt.Sprintf("%v", v))
		return fmt.Sprintf("'%s'", escaped)
	}
}

func (g *SQLGenerator) getColumnType(columnName string) string {
	if g.schema == nil {
		return ""
	}

	for _, col := range g.schema.Columns {
		if col.Name == columnName {
			return col.Type
		}
	}
	return ""
}

func (g *SQLGenerator) isMapType(columnType string) bool {
	lower := strings.ToLower(columnType)
	return strings.HasPrefix(lower, "map(")
}

func (g *SQLGenerator) isJsonType(columnType string) bool {
	lower := strings.ToLower(columnType)
	return lower == "json" || strings.HasPrefix(lower, "json(") || lower == "newjson"
}

func (g *SQLGenerator) isStringType(columnType string) bool {
	lower := strings.ToLower(columnType)
	return lower == "string" ||
		strings.HasPrefix(lower, "string(") ||
		strings.HasPrefix(lower, "fixedstring(") ||
		strings.HasPrefix(lower, "lowcardinality(string)")
}

func (g *SQLGenerator) generateNestedFieldAccess(baseColumn string, path []string, columnType string, operator Operator, value interface{}) string {
	formattedValue := g.formatValue(value, operator)

	// If no schema info, fallback to JSON extraction
	if columnType == "" {
		return g.generateJsonExtraction(baseColumn, path, operator, formattedValue)
	}

	// Handle different column types
	if g.isMapType(columnType) {
		return g.generateMapAccess(baseColumn, path, operator, formattedValue)
	} else if g.isJsonType(columnType) {
		return g.generateJsonExtraction(baseColumn, path, operator, formattedValue)
	} else if g.isStringType(columnType) {
		// String column might contain JSON - try JSON extraction
		return g.generateJsonExtraction(baseColumn, path, operator, formattedValue)
	}

	// Fallback to JSON extraction for unknown types
	return g.generateJsonExtraction(baseColumn, path, operator, formattedValue)
}

func (g *SQLGenerator) generateMapAccess(baseColumn string, path []string, operator Operator, formattedValue string) string {
	escapedColumn := g.escapeIdentifier(baseColumn)

	// For ClickHouse Maps, access nested keys using dot notation as a single key
	var escapedPath []string
	for _, segment := range path {
		// Strip surrounding quotes if present
		s := strings.TrimPrefix(segment, "\"")
		s = strings.TrimSuffix(s, "\"")
		s = strings.TrimPrefix(s, "'")
		s = strings.TrimSuffix(s, "'")
		escapedPath = append(escapedPath, g.escapeSQLString(s))
	}
	fullKey := strings.Join(escapedPath, ".")
	mapAccess := fmt.Sprintf("%s['%s']", escapedColumn, fullKey)

	return g.generateComparisonExpression(mapAccess, operator, formattedValue)
}

func (g *SQLGenerator) generateJsonExtraction(baseColumn string, path []string, operator Operator, formattedValue string) string {
	escapedColumn := g.escapeIdentifier(baseColumn)

	// ClickHouse JSONExtractString requires separate parameters for nested access
	var pathParams []string
	for _, segment := range path {
		// Strip surrounding quotes if present
		s := strings.TrimPrefix(segment, "\"")
		s = strings.TrimSuffix(s, "\"")
		s = strings.TrimPrefix(s, "'")
		s = strings.TrimSuffix(s, "'")
		pathParams = append(pathParams, fmt.Sprintf("'%s'", g.escapeSQLString(s)))
	}

	jsonExtract := fmt.Sprintf("JSONExtractString(%s, %s)", escapedColumn, strings.Join(pathParams, ", "))
	return g.generateComparisonExpression(jsonExtract, operator, formattedValue)
}

func (g *SQLGenerator) generateComparisonExpression(columnExpression string, operator Operator, formattedValue string) string {
	switch operator {
	case OpRegex:
		return fmt.Sprintf("positionCaseInsensitive(%s, %s) > 0", columnExpression, formattedValue)
	case OpNotRegex:
		return fmt.Sprintf("positionCaseInsensitive(%s, %s) = 0", columnExpression, formattedValue)
	case OpEquals:
		return fmt.Sprintf("%s = %s", columnExpression, formattedValue)
	case OpNotEquals:
		return fmt.Sprintf("%s != %s", columnExpression, formattedValue)
	case OpGT:
		return fmt.Sprintf("%s > %s", columnExpression, formattedValue)
	case OpLT:
		return fmt.Sprintf("%s < %s", columnExpression, formattedValue)
	case OpGTE:
		return fmt.Sprintf("%s >= %s", columnExpression, formattedValue)
	case OpLTE:
		return fmt.Sprintf("%s <= %s", columnExpression, formattedValue)
	default:
		return ""
	}
}

func (g *SQLGenerator) generateSelectFieldExpression(selectField SelectField) string {
	var columnExpression string

	// Check if it's a nested field
	if nf, ok := selectField.Field.(NestedField); ok {
		columnType := g.getColumnType(nf.Base)

		if columnType != "" && g.isMapType(columnType) {
			// Map column: use subscript notation
			escapedColumn := g.escapeIdentifier(nf.Base)
			var escapedPath []string
			for _, segment := range nf.Path {
				s := strings.TrimPrefix(segment, "\"")
				s = strings.TrimSuffix(s, "\"")
				s = strings.TrimPrefix(s, "'")
				s = strings.TrimSuffix(s, "'")
				escapedPath = append(escapedPath, g.escapeSQLString(s))
			}
			fullKey := strings.Join(escapedPath, ".")
			columnExpression = fmt.Sprintf("%s['%s']", escapedColumn, fullKey)
		} else {
			// JSON or String column: use JSONExtractString
			escapedColumn := g.escapeIdentifier(nf.Base)
			var pathParams []string
			for _, segment := range nf.Path {
				s := strings.TrimPrefix(segment, "\"")
				s = strings.TrimSuffix(s, "\"")
				s = strings.TrimPrefix(s, "'")
				s = strings.TrimSuffix(s, "'")
				pathParams = append(pathParams, fmt.Sprintf("'%s'", g.escapeSQLString(s)))
			}
			columnExpression = fmt.Sprintf("JSONExtractString(%s, %s)", escapedColumn, strings.Join(pathParams, ", "))
		}
	} else if fieldName, ok := selectField.Field.(string); ok {
		// Simple field
		columnExpression = g.escapeIdentifier(fieldName)
	} else {
		return ""
	}

	// Add alias if provided, or generate one for nested fields
	if selectField.Alias != "" {
		return fmt.Sprintf("%s AS %s", columnExpression, g.escapeIdentifier(selectField.Alias))
	} else if nf, ok := selectField.Field.(NestedField); ok {
		// Auto-generate alias for nested fields
		autoAlias := nf.Base + "_" + strings.Join(nf.Path, "_")
		return fmt.Sprintf("%s AS %s", columnExpression, g.escapeIdentifier(autoAlias))
	}

	return columnExpression
}
