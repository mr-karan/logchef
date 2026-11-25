package logchefql

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/internal/template"
)

// SQLGenerator converts an AST into ClickHouse SQL
type SQLGenerator struct {
	// colTypes and defaultMapCol are derived from schema once at construction so
	// column lookups during generation are O(1) instead of a linear scan per
	// expression/select field.
	colTypes      map[string]string
	defaultMapCol string
	// warnings are notes about a query that translates but may not do what
	// it says, in the order found, without repeats.
	warnings []string
}

// Warnings returns the notes the generator collected.
func (g *SQLGenerator) Warnings() []string {
	return slices.Clone(g.warnings)
}

func (g *SQLGenerator) warn(message string) {
	if !slices.Contains(g.warnings, message) {
		g.warnings = append(g.warnings, message)
	}
}

// warnJSONNumber notes that field, a JSON path read from a String column as
// text, is used as a number. JSONExtractString returns "" for a JSON number,
// so the query translates but quietly misses every numeric value.
func (g *SQLGenerator) warnJSONNumber(key any, field sqlField, consequence string) {
	g.warn(fmt.Sprintf("%s reads JSON inside the String column %s with JSONExtractString, which returns an empty string for a JSON number, so %s. For a numeric JSON value use native SQL with %s or JSONExtractInt.",
		getFieldName(key), field.jsonTextColumn, consequence, strings.Replace(field.sql, "JSONExtractString(", "JSONExtractFloat(", 1)))
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
		columns = append(columns, queryescape.ClickHouseIdentifier(defaultTimestampField))
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
		if field.jsonTextColumn != "" {
			return "", &ParseError{
				Code: ErrUnsupportedFeature,
				Message: fmt.Sprintf("%s is JSON inside the String column %s, read as text, so quote the value to compare a JSON string, or use native SQL with %s for a JSON number",
					getFieldName(node.Key), field.jsonTextColumn, strings.Replace(field.sql, "JSONExtractString(", "JSONExtractFloat(", 1)),
			}
		}
		return "", &ParseError{
			Code:    ErrUnsupportedFeature,
			Message: "numeric comparisons require a numeric field; quote the value to compare text",
		}
	}
	if field.jsonTextColumn != "" && slices.Contains([]Operator{OpGT, OpLT, OpGTE, OpLTE}, node.Operator) {
		g.warnJSONNumber(node.Key, field, "this comparison orders text, not numbers")
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
		escaped := queryescape.ClickHouseString(v)
		return fmt.Sprintf("'%s'", escaped)
	default:
		escaped := queryescape.ClickHouseString(fmt.Sprintf("%v", v))
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

func (g *SQLGenerator) isJSONType(columnType string) bool {
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
	// jsonTextColumn names the String column that a nested path reads with
	// JSONExtractString. Empty for any other field.
	jsonTextColumn string
}

// resolveField is shared by WHERE comparisons and pipe projections.
func (g *SQLGenerator) resolveField(field any) sqlField {
	switch f := field.(type) {
	case string:
		if strings.TrimSpace(f) == "" {
			return sqlField{}
		}
		return sqlField{sql: queryescape.ClickHouseIdentifier(f), columnType: g.getColumnType(f)}
	case NestedField:
		if strings.TrimSpace(f.Base) == "" || len(f.Path) == 0 {
			return sqlField{}
		}
		if slices.Contains(f.Path, "") {
			return sqlField{}
		}
		columnType := g.getColumnType(f.Base)
		switch {
		case g.isMapType(columnType):
			return sqlField{sql: g.mapAccess(f.Base, f.Path), columnType: g.mapValueType(columnType)}
		case g.isJSONType(columnType):
			pathType := g.jsonPathType(columnType, f.Path)
			if pathType == "" {
				pathType = "Dynamic"
			}
			return sqlField{sql: g.nativeJSONPath(f.Base, f.Path, columnType), columnType: pathType}
		case columnType == "" || g.isStringType(columnType):
			return sqlField{sql: g.jsonExtraction(f.Base, f.Path), columnType: "String", jsonTextColumn: f.Base}
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
	baseExpression := queryescape.ClickHouseIdentifier(base)
	if isNullableType(columnType) {
		baseExpression = "assumeNotNull(" + baseExpression + ")"
	}
	parts := []string{baseExpression}
	for _, segment := range path {
		parts = append(parts, queryescape.ClickHouseIdentifier(strings.Trim(segment, "\"'")))
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
	escapedColumn := queryescape.ClickHouseIdentifier(baseColumn)

	// For ClickHouse Maps, access nested keys using dot notation as a single key
	var escapedPath []string
	for _, segment := range path {
		// Strip surrounding quotes if present
		s := strings.TrimPrefix(segment, "\"")
		s = strings.TrimSuffix(s, "\"")
		s = strings.TrimPrefix(s, "'")
		s = strings.TrimSuffix(s, "'")
		escapedPath = append(escapedPath, queryescape.ClickHouseString(s))
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
	escapedColumn := queryescape.ClickHouseIdentifier(baseColumn)

	// ClickHouse JSONExtractString requires separate parameters for nested access
	var pathParams []string
	for _, segment := range path {
		// Strip surrounding quotes if present
		s := strings.TrimPrefix(segment, "\"")
		s = strings.TrimSuffix(s, "\"")
		s = strings.TrimPrefix(s, "'")
		s = strings.TrimSuffix(s, "'")
		pathParams = append(pathParams, fmt.Sprintf("'%s'", queryescape.ClickHouseString(s)))
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
		return fmt.Sprintf("%s AS %s", columnExpression, queryescape.ClickHouseIdentifier(selectField.Alias)), nil
	case nestedField != nil:
		autoAlias := nestedField.Base + "_" + strings.Join(nestedField.Path, "_")
		return fmt.Sprintf("%s AS %s", columnExpression, queryescape.ClickHouseIdentifier(autoAlias)), nil
	case simpleFieldName != "" && !g.columnExists(simpleFieldName):
		return fmt.Sprintf("%s AS %s", columnExpression, queryescape.ClickHouseIdentifier(simpleFieldName)), nil
	default:
		return columnExpression, nil
	}
}

func unsupportedASTNode(node ASTNode) *ParseError {
	return &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("unsupported LogchefQL node type %T", node)}
}

// SQLAggregation is an aggregation compiled to ClickHouse SQL. The time
// bucket waits for the query builder, which knows the timestamp column and
// the timezone.
type SQLAggregation struct {
	info     *AggregationInfo
	interval template.Interval
	// fieldKeys and values are the select items in written order, each
	// "<expression> AS `<name>`".
	fieldKeys []string
	// keyRefs is how GROUP BY and ORDER BY name each output column: the
	// expression of a field key, the quoted name otherwise. A dotted name
	// such as `j.host` would read the JSON subcolumn and not the alias.
	keyRefs map[string]string
	values  []string
	node    *AggregationNode
	limit   int
	orderBy []string
}

// Info describes the result of the aggregation.
func (a *SQLAggregation) Info() *AggregationInfo { return a.info }

// ref names an output column in GROUP BY and ORDER BY.
func (a *SQLAggregation) ref(name string) string {
	if expr, ok := a.keyRefs[name]; ok {
		return expr
	}
	return queryescape.ClickHouseIdentifier(name)
}

var numberTypePattern = regexp.MustCompile(`^(u?int(8|16|32|64|128|256)|b?float(16|32|64)|decimal(32|64|128|256)?(\(.*\))?)$`)

var percentiles = map[AggFunc]string{AggP50: "0.5", AggP75: "0.75", AggP90: "0.9", AggP95: "0.95", AggP99: "0.99"}

// GenerateAggregation compiles the aggregation of a query. requestInterval is
// the window of a time key that has none of its own.
func (g *SQLGenerator) GenerateAggregation(node *AggregationNode, requestInterval string) (*SQLAggregation, *ParseError) {
	info := node.Info(requestInterval)
	agg := &SQLAggregation{info: info, node: node, limit: node.Limit, keyRefs: map[string]string{}}

	for _, key := range node.By {
		if !key.Time {
			continue
		}
		if info.Interval == "" {
			return nil, aggregationError(key.pos, "this query buckets by time, so the request needs an interval")
		}
		interval, ok := template.ParseInterval(info.Interval)
		if !ok {
			return nil, aggregationError(key.pos, "invalid interval %q: use one of %s", info.Interval, strings.Join(template.IntervalWindows(), ", "))
		}
		agg.interval = interval
	}
	for _, key := range node.By {
		if key.Time {
			continue
		}
		field := g.resolveField(key.Field)
		if field.resolutionError != nil {
			return nil, field.resolutionError
		}
		expr := field.sql
		if isDynamicType(field.columnType) {
			// ClickHouse rejects a Dynamic group key. A missing path reads
			// as "", the group that a missing text field has.
			expr = "toString(" + expr + ")"
		}
		agg.fieldKeys = append(agg.fieldKeys, expr+" AS "+queryescape.ClickHouseIdentifier(key.Name))
		agg.keyRefs[key.Name] = expr
	}
	for _, value := range node.Aggregates {
		expr, err := g.aggregateExpression(value)
		if err != nil {
			return nil, err
		}
		agg.values = append(agg.values, expr+" AS "+queryescape.ClickHouseIdentifier(value.Name))
	}
	if err := checkOutputNames(node, g.columnExists); err != nil {
		return nil, err
	}
	agg.orderBy = aggregationOrder(node, info, agg.ref)
	return agg, nil
}

func (g *SQLGenerator) aggregateExpression(agg Aggregate) (string, *ParseError) {
	if agg.Func == AggCount {
		return "count()", nil
	}
	field := g.resolveField(agg.Field)
	if field.resolutionError != nil {
		return "", field.resolutionError
	}
	if agg.Func == AggCountDistinct {
		switch {
		case field.columnType == "" || g.isStringType(field.columnType):
			return fmt.Sprintf("uniqExactIf(%s, %s != '')", field.sql, field.sql), nil
		case isDynamicType(field.columnType):
			// toString reads a missing path and an empty text as "".
			return fmt.Sprintf("uniqExactIf(%s, toString(%s) != '')", field.sql, field.sql), nil
		}
		return fmt.Sprintf("uniqExact(%s)", field.sql), nil
	}
	if field.jsonTextColumn != "" {
		g.warnJSONNumber(agg.Field, field, fmt.Sprintf("%s skips every numeric value", agg.Func))
	}
	number, err := g.numberExpression(agg, field)
	if err != nil {
		return "", err
	}
	if quantile, ok := percentiles[agg.Func]; ok {
		return fmt.Sprintf("quantileOrNull(%s)(%s)", quantile, number), nil
	}
	return fmt.Sprintf("%sOrNull(%s)", agg.Func, number), nil
}

// numberExpression is the number that sum, avg, min, max and the percentiles
// read from a field. Text that is not a number becomes NULL, which every
// aggregate skips.
func (g *SQLGenerator) numberExpression(agg Aggregate, field sqlField) (string, *ParseError) {
	columnType := strings.ToLower(unwrapType(field.columnType))
	switch {
	case numberTypePattern.MatchString(columnType):
		return field.sql, nil
	case columnType == "" || g.isStringType(field.columnType):
		return "toFloat64OrNull(" + field.sql + ")", nil
	case isDynamicType(field.columnType):
		return "toFloat64OrNull(toString(" + field.sql + "))", nil
	default:
		return "", aggregationError(agg.pos, "%s needs a number field. %q is %s", agg.Func, getFieldName(agg.Field), field.columnType)
	}
}

// isDynamicType reports a Dynamic type, the type of a native JSON path with
// no declared type.
func isDynamicType(columnType string) bool {
	return strings.EqualFold(unwrapType(columnType), "dynamic")
}

// checkOutputNames rejects an output name that is a column of the source,
// other than the field that a group key names. ClickHouse reads an alias
// inside WHERE, so `count() as level` would replace the column level in the
// filter.
func checkOutputNames(node *AggregationNode, isColumn func(string) bool) *ParseError {
	for _, key := range node.By {
		if !isColumn(key.Name) {
			continue
		}
		if field, ok := key.Field.(string); ok && field == key.Name {
			continue
		}
		if key.Time && !key.aliased {
			return aggregationError(key.pos, "the time bucket is named %q, which is a column of this source. Name it with \"as\", as in time as bucket", key.Name)
		}
		return outputNameError(key.pos, key.Name)
	}
	for _, value := range node.Aggregates {
		if isColumn(value.Name) {
			return outputNameError(value.pos, value.Name)
		}
	}
	return nil
}

func outputNameError(pos Position, name string) *ParseError {
	return aggregationError(pos, "the name %q is a column of this source, and ClickHouse would read it in the filter. Choose another name with \"as\"", name)
}

// aggregationOrder is the ORDER BY of the result: the sort stage, or the
// default order of the shape, then every key ascending so the order is total.
func aggregationOrder(node *AggregationNode, info *AggregationInfo, ref func(string) string) []string {
	var order []string
	first := ""
	switch {
	case node.Sort != nil:
		first = node.Sort.Name
		direction := " ASC"
		if node.Sort.Desc {
			direction = " DESC"
		}
		order = append(order, ref(first)+direction)
	case info.Shape == ShapeGrouped:
		for _, col := range info.Columns {
			if col.Role == RoleValue {
				first = col.Name
				order = append(order, ref(first)+" DESC")
				break
			}
		}
	}
	for _, col := range info.Columns {
		if col.Role != RoleValue && col.Name != first {
			order = append(order, ref(col.Name)+" ASC")
		}
	}
	return order
}

// maxBuckets is the most time buckets one aggregation may return.
const maxBuckets = 1000

// checkBuckets rejects a time range that the bucket window cuts into more
// than maxBuckets buckets.
func (a *SQLAggregation) checkBuckets(start, end timeBound) *ParseError {
	if a.interval.Window == "" {
		return nil
	}
	from, err := start.time()
	if err != nil {
		return err
	}
	to, err := end.time()
	if err != nil {
		return err
	}
	return CheckBuckets(a.interval.Window, from, to)
}

// CheckBuckets rejects a range from..to that window cuts into more than
// maxBuckets buckets, and names the smallest window that fits. Every log
// backend runs it, so the limit and its message are the same on each.
func CheckBuckets(window string, from, to time.Time) *ParseError {
	interval, ok := template.ParseInterval(window)
	if !ok {
		return &ParseError{
			Code:    ErrInvalidAggregation,
			Message: fmt.Sprintf("invalid interval %q: use one of %s", window, strings.Join(template.IntervalWindows(), ", ")),
		}
	}
	span := to.Sub(from)
	buckets := bucketCount(span, interval)
	if buckets <= maxBuckets {
		return nil
	}
	advice := "Shorten the range"
	for _, window := range template.IntervalWindows() {
		if interval, _ := template.ParseInterval(window); bucketCount(span, interval) <= maxBuckets {
			advice = "Use " + window + " or longer"
			break
		}
	}
	return &ParseError{
		Code:    ErrInvalidAggregation,
		Message: fmt.Sprintf("%s over this range gives %d buckets. The maximum is %d. %s", interval.Window, buckets, maxBuckets, advice),
	}
}

func bucketCount(span time.Duration, interval template.Interval) int64 {
	window := time.Duration(interval.Seconds) * time.Second
	return int64((span + window - 1) / window)
}

// writeQuery writes the aggregated SELECT with the time range and the filter
// of the query. The timestamp column may be missing from the schema, so the
// name rule checks it here too.
func (a *SQLAggregation) writeQuery(query *strings.Builder, params QueryBuildParams, timeFilter, filter string) error {
	if err := checkOutputNames(a.node, func(name string) bool { return name == params.TimestampField }); err != nil {
		return err
	}
	items := make([]string, 0, len(a.info.Columns))
	if a.interval.Window != "" {
		items = append(items, fmt.Sprintf("toStartOfInterval(%s, %s, '%s') AS %s",
			queryescape.ClickHouseIdentifier(params.TimestampField), a.interval.ClickHouse(),
			queryescape.ClickHouseString(params.Timezone), queryescape.ClickHouseIdentifier(a.info.Columns[0].Name)))
	}
	items = append(items, a.fieldKeys...)
	items = append(items, a.values...)

	query.WriteString("SELECT " + strings.Join(items, ", ") + "\n")
	query.WriteString("FROM " + params.TableName + "\n")
	query.WriteString("WHERE " + timeFilter)
	if filter != "" {
		query.WriteString("\n  AND (" + filter + ")")
	}
	var groupBy []string
	for _, col := range a.info.Columns {
		if col.Role != RoleValue {
			groupBy = append(groupBy, a.ref(col.Name))
		}
	}
	if len(groupBy) > 0 {
		query.WriteString("\nGROUP BY " + strings.Join(groupBy, ", "))
	}
	if len(a.orderBy) > 0 {
		query.WriteString("\nORDER BY " + strings.Join(a.orderBy, ", "))
	}
	limit := a.limit
	if params.Limit > 0 && (limit == 0 || params.Limit < limit) {
		limit = params.Limit
	}
	if limit > 0 {
		fmt.Fprintf(query, "\nLIMIT %d", limit)
	}
	return nil
}
