package logchefql

import (
	"fmt"
	"strings"
)

// SQLGenerator converts an AST into ClickHouse SQL
type SQLGenerator struct {
	// colTypes and defaultMapCol are derived from schema once at construction so
	// column lookups during generation are O(1) instead of a linear scan per
	// expression/select field.
	colTypes      map[string]string
	defaultMapCol string
}

// NewSQLGenerator creates a new SQL generator with optional schema
func NewSQLGenerator(schema *Schema) *SQLGenerator {
	g := &SQLGenerator{}
	if schema != nil {
		g.colTypes = make(map[string]string, len(schema.Columns))
		for _, col := range schema.Columns {
			if _, ok := g.colTypes[col.Name]; !ok {
				g.colTypes[col.Name] = col.Type
			}
			// First Map(...) column in schema order is the default map column.
			if g.defaultMapCol == "" && g.isMapType(col.Type) {
				g.defaultMapCol = col.Name
			}
		}
	}
	return g
}

// Generate converts an AST node to SQL WHERE clause conditions.
func (g *SQLGenerator) Generate(node ASTNode) (string, *ParseError) {
	if node == nil {
		return "", nil
	}
	return g.visit(node)
}

// GenerateSelectClause generates the SELECT clause from select fields
func (g *SQLGenerator) GenerateSelectClause(selectFields []SelectField, defaultTimestampField string) (string, *ParseError) {
	if len(selectFields) == 0 {
		return "*", nil
	}

	var columns []string

	// Always include timestamp field first if specified
	if defaultTimestampField != "" {
		columns = append(columns, g.escapeIdentifier(defaultTimestampField))
	}

	// Generate column expressions for each select field
	for _, sf := range selectFields {
		expr, err := g.generateSelectFieldExpression(sf)
		if err != nil {
			return "", err
		}
		if expr != "" {
			columns = append(columns, expr)
		}
	}

	if len(columns) == 0 {
		return "*", nil
	}

	return strings.Join(columns, ", "), nil
}

func (g *SQLGenerator) visit(node ASTNode) (string, *ParseError) {
	switch n := node.(type) {
	case *ExpressionNode:
		if n == nil {
			return "", unsupportedASTNode(node)
		}
		return g.visitExpression(n)
	case *LogicalNode:
		if n == nil {
			return "", unsupportedASTNode(node)
		}
		return g.visitLogical(n)
	case *GroupNode:
		if n == nil {
			return "", unsupportedASTNode(node)
		}
		return g.visitGroup(n)
	case *QueryNode:
		if n == nil {
			return "", unsupportedASTNode(node)
		}
		return g.visitQuery(n)
	default:
		return "", unsupportedASTNode(node)
	}
}

func (g *SQLGenerator) visitQuery(node *QueryNode) (string, *ParseError) {
	if node.Where != nil {
		return g.visit(node.Where)
	}
	return "", nil
}

func (g *SQLGenerator) visitExpression(node *ExpressionNode) (string, *ParseError) {
	field := g.resolveField(node.Key)
	if field.resolutionError != nil {
		return "", field.resolutionError
	}
	if field.sql == "" {
		return "", &ParseError{Code: ErrInvalidIdentifier, Message: "field name is required"}
	}
	if _, numeric := node.Value.(NumericLiteral); numeric && g.isStringType(field.columnType) {
		return "", &ParseError{
			Code:    ErrUnsupportedFeature,
			Message: "numeric comparisons require a numeric field; quote the value to compare text",
		}
	}
	return g.generateComparisonExpression(field, node.Operator, g.formatValue(node.Value))
}

func (g *SQLGenerator) visitLogical(node *LogicalNode) (string, *ParseError) {
	if node.Operator != BoolAnd && node.Operator != BoolOr {
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("unsupported boolean operator %q", node.Operator)}
	}
	if len(node.Children) == 0 {
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: "logical expression has no children"}
	}

	if len(node.Children) == 1 {
		if node.Children[0] == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "logical expression contains a nil child"}
		}
		return g.visit(node.Children[0])
	}

	var conditions []string
	for _, child := range node.Children {
		sql, err := g.visit(child)
		if err != nil {
			return "", err
		}
		if sql == "" {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "logical expression contains an empty child"}
		}
		conditions = append(conditions, sql)
	}

	// Wrap each condition in parentheses and join with operator
	var wrapped []string
	for _, c := range conditions {
		wrapped = append(wrapped, fmt.Sprintf("(%s)", c))
	}

	return strings.Join(wrapped, fmt.Sprintf(" %s ", node.Operator)), nil
}

func (g *SQLGenerator) visitGroup(node *GroupNode) (string, *ParseError) {
	if len(node.Children) == 0 {
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: "group has no children"}
	}

	if len(node.Children) == 1 {
		if node.Children[0] == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "group contains a nil child"}
		}
		return g.visit(node.Children[0])
	}

	// Handle multiple expressions in a group - default to AND
	var conditions []string
	for _, child := range node.Children {
		sql, err := g.visit(child)
		if err != nil {
			return "", err
		}
		if sql == "" {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "group contains an empty child"}
		}
		conditions = append(conditions, sql)
	}

	return fmt.Sprintf("(%s)", strings.Join(conditions, " AND ")), nil
}

func (g *SQLGenerator) escapeIdentifier(identifier string) string {
	// Escape backticks by doubling them
	escaped := strings.ReplaceAll(identifier, "`", "``")
	return "`" + escaped + "`"
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

func (g *SQLGenerator) formatValue(value any) string {
	if value == nil {
		return "NULL"
	}

	switch v := value.(type) {
	case bool:
		if v {
			return "1"
		}
		return "0"
	case NumericLiteral:
		return string(v)
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
	return g.colTypes[columnName]
}

func (g *SQLGenerator) columnExists(columnName string) bool {
	_, ok := g.colTypes[columnName]
	return ok
}

func (g *SQLGenerator) findDefaultMapColumn() string {
	return g.defaultMapCol
}

func (g *SQLGenerator) isMapType(columnType string) bool {
	lower := strings.ToLower(unwrapType(columnType))
	return strings.HasPrefix(lower, "map(")
}

func (g *SQLGenerator) isJsonType(columnType string) bool {
	lower := strings.ToLower(unwrapType(columnType))
	return lower == "json" || strings.HasPrefix(lower, "json(") || lower == "newjson"
}

func (g *SQLGenerator) isStringType(columnType string) bool {
	lower := strings.ToLower(unwrapType(columnType))
	return lower == "string" || strings.HasPrefix(lower, "string(") ||
		strings.HasPrefix(lower, "fixedstring(")
}

func unwrapType(columnType string) string {
	trimmed := strings.TrimSpace(columnType)
	for _, wrapper := range []string{"nullable", "lowcardinality"} {
		if inner, ok := typeArguments(trimmed, wrapper); ok {
			return unwrapType(inner)
		}
	}
	return trimmed
}

type sqlField struct {
	sql             string
	columnType      string
	resolutionError *ParseError
}

// resolveField is shared by WHERE comparisons and pipe projections.
func (g *SQLGenerator) resolveField(field any) sqlField {
	switch f := field.(type) {
	case string:
		if strings.TrimSpace(f) == "" {
			return sqlField{}
		}
		return sqlField{sql: g.escapeIdentifier(f), columnType: g.getColumnType(f)}
	case NestedField:
		if strings.TrimSpace(f.Base) == "" || len(f.Path) == 0 {
			return sqlField{}
		}
		for _, segment := range f.Path {
			if segment == "" {
				return sqlField{}
			}
		}
		columnType := g.getColumnType(f.Base)
		switch {
		case g.isMapType(columnType):
			return sqlField{sql: g.mapAccess(f.Base, f.Path), columnType: g.mapValueType(columnType)}
		case g.isJsonType(columnType):
			pathType := g.jsonPathType(columnType, f.Path)
			if pathType == "" {
				pathType = "Dynamic"
			}
			return sqlField{sql: g.nativeJSONPath(f.Base, f.Path, columnType), columnType: pathType}
		case columnType == "" || g.isStringType(columnType):
			return sqlField{sql: g.jsonExtraction(f.Base, f.Path), columnType: "String"}
		default:
			return sqlField{resolutionError: &ParseError{
				Code:    ErrUnsupportedFeature,
				Message: fmt.Sprintf("nested field access is unsupported for column %q of type %q", f.Base, columnType),
			}}
		}
	default:
		return sqlField{}
	}
}

func (g *SQLGenerator) nativeJSONPath(base string, path []string, columnType string) string {
	baseExpression := g.escapeIdentifier(base)
	if isNullableType(columnType) {
		baseExpression = "assumeNotNull(" + baseExpression + ")"
	}
	parts := []string{baseExpression}
	for _, segment := range path {
		parts = append(parts, g.escapeIdentifier(strings.Trim(segment, "\"'")))
	}
	return strings.Join(parts, ".")
}

func isNullableType(columnType string) bool {
	trimmed := strings.TrimSpace(columnType)
	for {
		if _, ok := typeArguments(trimmed, "nullable"); ok {
			return true
		}
		inner, ok := typeArguments(trimmed, "lowcardinality")
		if !ok {
			return false
		}
		trimmed = inner
	}
}

func (g *SQLGenerator) mapAccess(baseColumn string, path []string) string {
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
	return fmt.Sprintf("%s['%s']", escapedColumn, fullKey)
}

func (g *SQLGenerator) mapValueType(columnType string) string {
	inner, ok := typeArguments(unwrapType(columnType), "map")
	if !ok {
		return ""
	}
	parts := splitTypeArguments(inner)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (g *SQLGenerator) jsonPathType(columnType string, path []string) string {
	inner, ok := typeArguments(unwrapType(columnType), "json")
	if !ok || len(path) == 0 {
		return ""
	}
	for _, declaration := range splitTypeArguments(inner) {
		name, valueType, ok := strings.Cut(strings.TrimSpace(declaration), " ")
		if ok && strings.Trim(name, "`\"'") == path[0] {
			return strings.TrimSpace(valueType)
		}
	}
	return ""
}

func typeArguments(columnType, typeName string) (string, bool) {
	trimmed := strings.TrimSpace(columnType)
	prefix := typeName + "("
	if len(trimmed) <= len(prefix) || !strings.EqualFold(trimmed[:len(prefix)], prefix) || !strings.HasSuffix(trimmed, ")") {
		return "", false
	}
	return trimmed[len(prefix) : len(trimmed)-1], true
}

func splitTypeArguments(value string) []string {
	var parts []string
	start, depth := 0, 0
	for i, r := range value {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, value[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, value[start:])
}

func (g *SQLGenerator) jsonExtraction(baseColumn string, path []string) string {
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

	return fmt.Sprintf("JSONExtractString(%s, %s)", escapedColumn, strings.Join(pathParams, ", "))
}

func (g *SQLGenerator) generateComparisonExpression(field sqlField, operator Operator, formattedValue string) (string, *ParseError) {
	columnExpression := field.sql
	switch operator {
	case OpEquals, OpNotEquals, OpRegex, OpNotRegex, OpGT, OpLT, OpGTE, OpLTE:
	default:
		return "", &ParseError{Code: ErrUnknownOperator, Message: fmt.Sprintf("unsupported operator %q", operator)}
	}
	if operator == OpEquals && formattedValue == "NULL" {
		return fmt.Sprintf("isNull(%s)", columnExpression), nil
	}
	if operator == OpNotEquals && formattedValue == "NULL" {
		return fmt.Sprintf("isNotNull(%s)", columnExpression), nil
	}
	if formattedValue == "NULL" {
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("operator %q cannot be used with NULL", operator)}
	}
	if (operator == OpRegex || operator == OpNotRegex) && field.columnType != "" && !g.isStringType(field.columnType) {
		columnExpression = "toString(" + columnExpression + ")"
	}
	switch operator {
	case OpRegex:
		return fmt.Sprintf("positionCaseInsensitive(%s, %s) > 0", columnExpression, formattedValue), nil
	case OpNotRegex:
		return fmt.Sprintf("positionCaseInsensitive(%s, %s) = 0", columnExpression, formattedValue), nil
	case OpEquals:
		return fmt.Sprintf("%s = %s", columnExpression, formattedValue), nil
	case OpNotEquals:
		return fmt.Sprintf("%s != %s", columnExpression, formattedValue), nil
	case OpGT:
		return fmt.Sprintf("%s > %s", columnExpression, formattedValue), nil
	case OpLT:
		return fmt.Sprintf("%s < %s", columnExpression, formattedValue), nil
	case OpGTE:
		return fmt.Sprintf("%s >= %s", columnExpression, formattedValue), nil
	case OpLTE:
		return fmt.Sprintf("%s <= %s", columnExpression, formattedValue), nil
	default:
		return "", &ParseError{Code: ErrUnknownOperator, Message: fmt.Sprintf("unsupported operator %q", operator)}
	}
}

func (g *SQLGenerator) generateSelectFieldExpression(selectField SelectField) (string, *ParseError) {
	field := g.resolveField(selectField.Field)
	if field.resolutionError != nil {
		return "", field.resolutionError
	}
	columnExpression := field.sql
	if columnExpression == "" {
		return "", &ParseError{Code: ErrInvalidIdentifier, Message: "field name is required"}
	}
	var nestedField *NestedField
	var simpleFieldName string

	switch f := selectField.Field.(type) {
	case NestedField:
		nestedField = &f
	case string:
		simpleFieldName = f
		if !g.columnExists(f) {
			if mapCol := g.findDefaultMapColumn(); mapCol != "" {
				columnExpression = g.mapAccess(mapCol, []string{f})
			}
		}
	}

	// Add alias if provided, or generate one for nested/map fields
	switch {
	case selectField.Alias != "":
		return fmt.Sprintf("%s AS %s", columnExpression, g.escapeIdentifier(selectField.Alias)), nil
	case nestedField != nil:
		autoAlias := nestedField.Base + "_" + strings.Join(nestedField.Path, "_")
		return fmt.Sprintf("%s AS %s", columnExpression, g.escapeIdentifier(autoAlias)), nil
	case simpleFieldName != "" && !g.columnExists(simpleFieldName):
		return fmt.Sprintf("%s AS %s", columnExpression, g.escapeIdentifier(simpleFieldName)), nil
	default:
		return columnExpression, nil
	}
}

func unsupportedASTNode(node ASTNode) *ParseError {
	return &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("unsupported LogchefQL node type %T", node)}
}
