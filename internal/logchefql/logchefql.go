package logchefql

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/alecthomas/participle/v2"

	"github.com/mr-karan/logchef/internal/queryescape"
)

// TranslateOptions opts a caller into query shapes that the default denies.
type TranslateOptions struct {
	// AllowAggregation accepts an aggregation pipe. Leave it false on a path
	// that needs log rows, and the query fails with AGGREGATION_NOT_ALLOWED.
	AllowAggregation bool
	// Interval is the request interval, the window of a `by time` key that
	// has none of its own.
	Interval string
}

// Translate parses a LogchefQL query and returns the SQL translation with
// metadata. It rejects an aggregation.
func Translate(query string, schema *Schema) *TranslateResult {
	return TranslateWithOptions(query, schema, TranslateOptions{})
}

// TranslateWithOptions is Translate with the opt-ins of opts.
func TranslateWithOptions(query string, schema *Schema, opts TranslateOptions) *TranslateResult {
	result := &TranslateResult{
		Valid:      false,
		Conditions: []FilterCondition{},
		FieldsUsed: []string{},
	}

	if query == "" || strings.TrimSpace(query) == "" {
		result.Valid = true
		result.SQL = ""
		return result
	}

	ast, perr := parseQuery(query)
	if perr == nil {
		perr = checkAggregationAllowed(ast, opts.AllowAggregation)
	}
	if perr != nil {
		result.Error = perr
		return result
	}

	return translateAST(ast, schema, opts.Interval)
}

func translateAST(ast ASTNode, schema *Schema, interval string) *TranslateResult {
	result := &TranslateResult{
		Valid:      false,
		Conditions: []FilterCondition{},
		FieldsUsed: []string{},
	}
	generator := NewSQLGenerator(schema)
	sql, genErr := generator.Generate(ast)
	if genErr != nil {
		result.Error = genErr
		return result
	}
	if node := AggregationOf(ast); node != nil {
		agg, aggErr := generator.GenerateAggregation(node, interval)
		if aggErr != nil {
			result.Error = aggErr
			return result
		}
		result.Aggregation = agg
	}
	selectClause := ""
	if queryNode, ok := ast.(*QueryNode); ok && len(queryNode.Select) > 0 {
		selectClause, genErr = generator.GenerateSelectClause(queryNode.Select, "")
		if genErr != nil {
			result.Error = genErr
			return result
		}
	}
	result.Valid = true
	result.SQL = sql
	result.SelectClause = selectClause
	result.FieldsUsed = extractFieldsFromAST(ast)
	result.Conditions = extractConditionsFromAST(ast)
	result.Warnings = generator.Warnings()
	return result
}

// Validate checks the syntax of a LogchefQL query and the rules of its
// aggregation. It knows no source and applies no deny rule.
func Validate(query string) *ValidateResult {
	result := &ValidateResult{Valid: false}

	if query == "" || strings.TrimSpace(query) == "" {
		result.Valid = true
		return result
	}

	if _, perr := parseQuery(query); perr != nil {
		result.Error = perr
		return result
	}

	result.Valid = true
	return result
}

func convertParticipleError(query string, err error) *ParseError {
	if err == nil {
		return nil
	}

	// Errors from the pre-parse length/nesting guard (see checkQueryLimits in
	// grammar.go) are already a fully-formed *ParseError with the correct
	// code; passing them through the participle-error heuristics below would
	// overwrite that code with a generic ErrUnexpectedToken.
	if pe, ok := errors.AsType[*ParseError](err); ok {
		return pe
	}

	var perr participle.Error
	if !errors.As(err, &perr) {
		return &ParseError{Code: ErrUnexpectedToken, Message: err.Error()}
	}
	pos := perr.Position()
	position := &Position{Line: pos.Line, Column: pos.Column}

	if strings.HasPrefix(perr.Message(), "lexer:") {
		rest := ""
		if pos.Offset >= 0 && pos.Offset <= len(query) {
			rest = query[pos.Offset:]
		}
		if strings.HasPrefix(rest, `"`) || strings.HasPrefix(rest, "'") {
			return &ParseError{Code: ErrUnterminatedString, Message: fmt.Sprintf("missing closing %s for the value at column %d", rest[:1], pos.Column), Position: position}
		}
		return &ParseError{Code: ErrUnexpectedToken, Message: fmt.Sprintf("unexpected character at column %d", pos.Column), Position: position}
	}

	var unexpected *participle.UnexpectedTokenError
	if !errors.As(err, &unexpected) {
		return &ParseError{Code: ErrUnexpectedToken, Message: perr.Message(), Position: position}
	}

	code := ErrUnexpectedToken
	if unexpected.Unexpected.EOF() {
		code = ErrUnexpectedEnd
	} else if strings.Contains(strings.ToLower(perr.Message()), "ident") {
		code = ErrMissingBooleanOperator
	}
	return &ParseError{Code: code, Message: describeUnexpectedToken(query, unexpected), Position: position}
}

// Grammar node names that participle reports, as words a user knows.
var expectedDescriptions = map[string]string{
	"PValue":       "a value",
	"<operator>":   "an operator such as = or ~",
	"PTerm":        "a condition",
	"PAndExpr":     "a condition",
	"POrExpr":      "a condition",
	"PComparison":  "a condition",
	"PFieldPath":   "a field name",
	"PPathSegment": "a field name",
	"PSelectItem+": "a field name",
	"PSelectItem":  "a field name",
	"<rparen>":     "a closing )",
	"<lparen>":     "an opening (",
	"<ident>":      "a field name",
	"PAggregate":   "a field name",
	"PAggItem":     "a function such as count()",
	"PAlias":       "a name",
	"PGroupKey":    "a field name or time",
	"PWindow":      "a window such as 5m",
	"PWindowUnit":  "a window such as 5m",
	"PSort":        "a column name",
	"PLimit":       "a number",
}

var expectedPattern = regexp.MustCompile(`\(expected (.+)\)$`)

// describeUnexpectedToken turns "1:26: unexpected token \"<EOF>\" (expected PValue)"
// into "expected a value after `msg~`".
func describeUnexpectedToken(query string, u *participle.UnexpectedTokenError) string {
	offset := min(max(u.Unexpected.Pos.Offset, 0), len(query))
	before := strings.Fields(query[:offset])
	after := ""
	if len(before) > 0 {
		after = fmt.Sprintf(" after %#q", before[len(before)-1])
	}
	found := ""
	if !u.Unexpected.EOF() {
		found = fmt.Sprintf(", found %#q", u.Unexpected.Value)
	}

	expected := ""
	if match := expectedPattern.FindStringSubmatch(u.Message()); match != nil {
		expected = describeExpected(match[1])
	}
	if expected == "" {
		if u.Unexpected.EOF() {
			return "query ends unexpectedly" + after
		}
		if len(before) == 0 {
			return fmt.Sprintf("expected a field name, found %#q", u.Unexpected.Value)
		}
		if u.Unexpected.Type == logchefQLLexer.Symbols()["Ident"] || u.Unexpected.Type == logchefQLLexer.Symbols()["LParen"] {
			return fmt.Sprintf("expected `and` or `or` before %#q", u.Unexpected.Value)
		}
		return fmt.Sprintf("unexpected %#q%s", u.Unexpected.Value, after)
	}
	return "expected " + expected + after + found
}

// describeExpected names what the grammar expected. Participle reports the
// rest of a sequence too, as in "PGroupKey (<comma> PGroupKey)*", so when the
// whole text does not map, only its first element is named: a single node,
// or the alternatives of a leading group.
func describeExpected(names string) string {
	if words := describeExpectedNames(strings.Fields(names)); words != "" {
		return words
	}
	names = strings.TrimSpace(names)
	if rest, ok := strings.CutPrefix(names, "("); ok {
		group, _, found := strings.Cut(rest, ")")
		if !found {
			return ""
		}
		return describeExpectedNames(strings.FieldsFunc(group, func(r rune) bool { return r == '|' || r == ' ' }))
	}
	first, _, _ := strings.Cut(names, " ")
	return describeExpectedNames([]string{first})
}

func describeExpectedNames(parts []string) string {
	words := make([]string, 0, len(parts))
	for _, part := range parts {
		word, ok := expectedDescriptions[part]
		if !ok {
			return ""
		}
		if !slices.Contains(words, word) {
			words = append(words, word)
		}
	}
	return strings.Join(words, " and ")
}

func extractFieldsFromAST(node ASTNode) []string {
	seen := make(map[string]bool)
	var fields []string

	var walk func(n ASTNode)
	walk = func(n ASTNode) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *ExpressionNode:
			fieldName := getFieldName(v.Key)
			if idx := strings.Index(fieldName, "."); idx > 0 {
				fieldName = fieldName[:idx]
			}
			if fieldName != "" && !seen[fieldName] {
				seen[fieldName] = true
				fields = append(fields, fieldName)
			}
		case *LogicalNode:
			for _, child := range v.Children {
				walk(child)
			}
		case *GroupNode:
			for _, child := range v.Children {
				walk(child)
			}
		case *QueryNode:
			walk(v.Where)
		}
	}

	walk(node)
	return fields
}

func extractConditionsFromAST(node ASTNode) []FilterCondition {
	var conditions []FilterCondition

	var walk func(n ASTNode)
	walk = func(n ASTNode) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *ExpressionNode:
			field := getFieldName(v.Key)
			op := string(v.Operator)
			value := formatConditionValue(v.Value)
			isRegex := op == "~" || op == "!~"

			conditions = append(conditions, FilterCondition{
				Field:    field,
				Operator: op,
				Value:    value,
				IsRegex:  isRegex,
			})
		case *LogicalNode:
			for _, child := range v.Children {
				walk(child)
			}
		case *GroupNode:
			for _, child := range v.Children {
				walk(child)
			}
		case *QueryNode:
			walk(v.Where)
		}
	}

	walk(node)
	return conditions
}

// FilterItem is one comparison of a query's filter, without its value. Base
// and Path keep a nested field's column and path segments apart.
type FilterItem struct {
	Base     string
	Path     []string
	Operator Operator
	// Null is true when the value is the bare NULL.
	Null bool
	// Disjunct is true when the comparison sits under an OR whose branches
	// compare more than one field, so no single field narrows the result.
	Disjunct bool
}

// FilterItems returns the comparisons of a parsed query's filter. It reads
// the WHERE part only and ignores any pipe.
func FilterItems(ast ASTNode) []FilterItem {
	switch v := ast.(type) {
	case *ExpressionNode:
		item := FilterItem{Base: getFieldName(v.Key), Operator: v.Operator, Null: v.Value == nil}
		if nested, ok := v.Key.(NestedField); ok && len(nested.Path) > 0 {
			item.Base, item.Path = nested.Base, nested.Path
		}
		return []FilterItem{item}
	case *LogicalNode:
		items := filterItemsOf(v.Children)
		if v.Operator == BoolOr && !sameField(items) {
			for i := range items {
				items[i].Disjunct = true
			}
		}
		return items
	case *GroupNode:
		return filterItemsOf(v.Children)
	case *QueryNode:
		return FilterItems(v.Where)
	}
	return nil
}

func filterItemsOf(nodes []ASTNode) []FilterItem {
	var items []FilterItem
	for _, node := range nodes {
		items = append(items, FilterItems(node)...)
	}
	return items
}

func sameField(items []FilterItem) bool {
	for _, item := range items {
		if item.Base != items[0].Base || !slices.Equal(item.Path, items[0].Path) {
			return false
		}
	}
	return true
}

func getFieldName(key any) string {
	switch k := key.(type) {
	case string:
		return k
	case NestedField:
		if len(k.Path) == 0 {
			return k.Base
		}
		return k.Base + "." + strings.Join(k.Path, ".")
	default:
		return ""
	}
}

func formatConditionValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case NumericLiteral:
		return string(val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}

var (
	timeFormatRegex     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d{1,9})?$`)
	timezoneAllowedChar = regexp.MustCompile(`^[A-Za-z0-9_/+:-]+$`)
	// Allows @ prefix for ELK-style @timestamp fields
	validIdentifier = regexp.MustCompile(`^@?[a-zA-Z_][a-zA-Z0-9_]*$`)
	// Requires database.table format (ClickHouse best practice)
	validTableName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*$`)
)

func validateTimeFormat(t string) *ParseError {
	if !timeFormatRegex.MatchString(t) {
		return &ParseError{
			Code:    ErrInvalidTimeFormat,
			Message: fmt.Sprintf("invalid time format: expected 'YYYY-MM-DD HH:MM:SS' with optional fractional seconds, or RFC3339, got '%s'", t),
		}
	}
	if _, err := time.Parse("2006-01-02 15:04:05", t); err != nil {
		return &ParseError{
			Code:    ErrInvalidTimeFormat,
			Message: fmt.Sprintf("invalid time value: %s", t),
		}
	}
	return nil
}

func validateTimezone(tz string) *ParseError {
	if tz == "" {
		return &ParseError{
			Code:    ErrInvalidTimezone,
			Message: "timezone cannot be empty",
		}
	}
	if !timezoneAllowedChar.MatchString(tz) {
		return &ParseError{
			Code:    ErrInvalidTimezone,
			Message: "invalid timezone: contains disallowed characters",
		}
	}
	if len(tz) > 64 {
		return &ParseError{
			Code:    ErrInvalidTimezone,
			Message: "invalid timezone: too long",
		}
	}
	return nil
}

func validateIdentifier(name, fieldName string) *ParseError {
	if !validIdentifier.MatchString(name) {
		return &ParseError{
			Code:    ErrInvalidIdentifier,
			Message: fmt.Sprintf("invalid %s '%s': must start with letter or underscore, contain only alphanumeric and underscore", fieldName, name),
		}
	}
	return nil
}

func validateTableName(name string) *ParseError {
	if !validTableName.MatchString(name) {
		return &ParseError{
			Code:    ErrInvalidIdentifier,
			Message: fmt.Sprintf("invalid table name '%s': expected format 'database.table' with valid identifiers", name),
		}
	}
	return nil
}

// BuildFullQueryFromTranslation builds a complete SQL query from an already
// translated query. Callers that need both preview metadata and executable SQL
// can translate once and pass that result here.
func BuildFullQueryFromTranslation(params QueryBuildParams, translateResult *TranslateResult) (string, error) {
	start, end, err := prepareTimeRange(params)
	if err != nil {
		return "", err
	}
	if translateResult == nil {
		return "", &ParseError{Code: ErrUnexpectedToken, Message: "translation result is required"}
	}
	if !translateResult.Valid {
		if translateResult.Error != nil {
			return "", translateResult.Error
		}
		return "", &ParseError{Code: ErrUnexpectedToken, Message: "invalid LogchefQL query"}
	}
	return buildFullQueryFromTranslation(params, start, end, translateResult)
}

// BuildAggregationQuery writes the aggregated SELECT of a translated query
// with timeCondition as its time filter. An alert passes the
// {{__time_filter}} macro, which its renderer expands for each evaluation
// window. A time key needs a range to check its buckets, so it is rejected.
func BuildAggregationQuery(translateResult *TranslateResult, tableName, timestampField, timeCondition string) (string, error) {
	agg := translateResult.Aggregation
	if agg == nil {
		return "", &ParseError{Code: ErrInvalidAggregation, Message: "the query has no aggregation, such as | count()"}
	}
	if slices.ContainsFunc(agg.node.By, func(key GroupKey) bool { return key.Time }) {
		return "", &ParseError{Code: ErrInvalidAggregation, Message: "a time key needs a time range. Remove time from by"}
	}
	if err := validateTableName(tableName); err != nil {
		return "", err
	}
	if err := validateIdentifier(timestampField, "timestamp field"); err != nil {
		return "", err
	}
	var query strings.Builder
	params := QueryBuildParams{TableName: tableName, TimestampField: timestampField}
	if err := agg.writeQuery(&query, params, timeCondition, translateResult.SQL); err != nil {
		return "", err
	}
	return query.String(), nil
}

// timeBound is one end of the query time range: a SQL timestamp and the
// timezone ClickHouse reads it in.
type timeBound struct {
	value string
	zone  string
}

// time is the instant of the bound.
func (b timeBound) time() (time.Time, *ParseError) {
	location, err := time.LoadLocation(b.zone)
	if err != nil {
		return time.Time{}, validateTimezone(b.zone)
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", b.value, location)
	if err != nil {
		return time.Time{}, validateTimeFormat(b.value)
	}
	return parsed, nil
}

// prepareTimeRange validates params and returns the start and end bounds.
func prepareTimeRange(params QueryBuildParams) (start, end timeBound, err error) {
	if err := validateTimezone(params.Timezone); err != nil {
		return timeBound{}, timeBound{}, err
	}
	start, perr := parseTimeBound(params.StartTime, params.Timezone)
	if perr != nil {
		return timeBound{}, timeBound{}, perr
	}
	end, perr = parseTimeBound(params.EndTime, params.Timezone)
	if perr != nil {
		return timeBound{}, timeBound{}, perr
	}
	if err := validateTableName(params.TableName); err != nil {
		return timeBound{}, timeBound{}, err
	}
	if err := validateIdentifier(params.TimestampField, "timestamp field"); err != nil {
		return timeBound{}, timeBound{}, err
	}
	return start, end, nil
}

// parseTimeBound accepts "YYYY-MM-DD HH:MM:SS" with optional fractional
// seconds, read in timezone, or an RFC3339 time. An RFC3339 time is an
// absolute instant, so it becomes a UTC timestamp read in 'UTC': a local wall
// clock would merge the two occurrences of an hour that repeats when DST
// ends. The value always passes validateTimeFormat before it reaches SQL.
func parseTimeBound(value, timezone string) (timeBound, *ParseError) {
	zone := timezone
	if strings.ContainsRune(value, 'T') {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return timeBound{}, validateTimeFormat(value)
		}
		value = parsed.UTC().Format("2006-01-02 15:04:05.999999999")
		zone = "UTC"
	}
	if err := validateTimeFormat(value); err != nil {
		return timeBound{}, err
	}
	return timeBound{value: value, zone: zone}, nil
}

func buildFullQueryFromTranslation(params QueryBuildParams, start, end timeBound, translateResult *TranslateResult) (string, error) {
	var query strings.Builder

	if agg := translateResult.Aggregation; agg != nil {
		if err := agg.checkBuckets(start, end); err != nil {
			return "", err
		}
		if err := agg.writeQuery(&query, params, timeFilter(params, start, end), translateResult.SQL); err != nil {
			return "", err
		}
		return query.String(), nil
	}

	query.WriteString("SELECT ")
	if translateResult.SelectClause != "" {
		timestampInSelect := strings.Contains(translateResult.SelectClause, queryescape.ClickHouseIdentifier(params.TimestampField))
		if params.TimestampField != "" && !timestampInSelect {
			fmt.Fprintf(&query, "%s, ", queryescape.ClickHouseIdentifier(params.TimestampField))
		}
		query.WriteString(translateResult.SelectClause)
	} else {
		query.WriteString("*")
	}
	query.WriteString("\n")

	// FROM clause
	query.WriteString("FROM ")
	query.WriteString(params.TableName)
	query.WriteString("\n")

	// WHERE clause with time range
	query.WriteString("WHERE ")
	query.WriteString(timeFilter(params, start, end))

	// Add LogchefQL conditions if present
	if translateResult.SQL != "" {
		query.WriteString("\n  AND (")
		query.WriteString(translateResult.SQL)
		query.WriteString(")")
	}
	query.WriteString("\n")

	// ORDER BY clause
	query.WriteString("ORDER BY ")
	query.WriteString(queryescape.ClickHouseIdentifier(params.TimestampField))
	query.WriteString(" DESC")

	// LIMIT clause
	if params.Limit > 0 {
		fmt.Fprintf(&query, "\nLIMIT %d", params.Limit)
	}

	return query.String(), nil
}

// timeFilter is the condition that keeps the rows of the time range.
func timeFilter(params QueryBuildParams, start, end timeBound) string {
	column := queryescape.ClickHouseIdentifier(params.TimestampField)
	if strings.Contains(start.value, ".") || strings.Contains(end.value, ".") {
		return fmt.Sprintf("%s BETWEEN toDateTime64('%s', 9, '%s') AND toDateTime64('%s', 9, '%s')", column, start.value, start.zone, end.value, end.zone)
	}
	return fmt.Sprintf("%s BETWEEN toDateTime('%s', '%s') AND toDateTime('%s', '%s')", column, start.value, start.zone, end.value, end.zone)
}

// QueryBuildParams contains parameters for building a full SQL query
type QueryBuildParams struct {
	LogchefQL      string  // The LogchefQL query string
	Schema         *Schema // Optional schema for type-aware SQL generation
	TableName      string  // Fully qualified table name (database.table)
	TimestampField string  // Name of the timestamp column
	StartTime      string  // SQL timestamp with optional fractional seconds, or RFC3339
	EndTime        string  // SQL timestamp with optional fractional seconds, or RFC3339
	Timezone       string  // Timezone for time conversion
	Limit          int     // Result limit
}
