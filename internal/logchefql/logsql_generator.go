package logchefql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// safeLogsQLFieldPattern matches field names that LogsQL accepts as a bare
// (unquoted) compound token: a leading word (letters/digits/underscore,
// starting with a letter or underscore) optionally followed by more word
// segments glued together with one of LogsQL's compound-token glue
// characters (`+ - / : . $`). Anything else -- including a leading `@`,
// embedded `@`, whitespace, or quote characters -- must be quoted.
//
// This mirrors VictoriaLogs' lexer (lib/logstorage/parser.go /
// lib/logstorage/tokenizer.go as of commit 19a73b56): `isTokenRune` accepts
// only letters, digits and `_`; `glueCompoundTokens` is `+ - / : . $`; and a
// compound token cannot start with any other character (notably `@`).
var safeLogsQLFieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:[+\-/:.$][A-Za-z0-9_]+)*$`)

// logsQLReservedTokens lists the words LogsQL reserves for operators,
// punctuation, pipe names, stats functions and math functions. A field name
// that is otherwise a valid bare compound token still must be quoted if it
// exactly equals one of these (case-insensitively), or VictoriaLogs parses
// it as the keyword instead of a field reference. Sourced from
// lib/logstorage/parser.go's `reservedKeywords`, lib/logstorage/pipe.go's
// pipe parser registry, lib/logstorage/pipe_stats.go's stats func registry,
// and lib/logstorage/pipe_math.go's math func registry (commit 19a73b56).
var logsQLReservedTokens = buildLogsQLReservedTokens()

func buildLogsQLReservedTokens() map[string]struct{} {
	tokens := []string{
		// Reserved keywords (operators, punctuation, filter functions).
		"", "and", "or", "not", "!", "(", ")", "{", "}", "=", "!=", "=~", "!~", ",",
		"|", ":", "*", "[", "]", "now", "offset", "-",
		"contains_all", "contains_any", "json_array_contains_any", "contains_common_case",
		"eq_field", "equals_common_case", "exact", "i", "in", "ipv4_range", "ipv6_range",
		"le_field", "len_range", "lt_field", "pattern_match", "pattern_match_full",
		"pattern_match_prefix", "pattern_match_suffix", "range", "re", "seq",
		"string_range", "value_type", "options", "if", "by", "as", "from",

		// Pipe names ('| <name> ...').
		"block_stats", "blocks_count", "coalesce", "collapse_nums", "copy", "cp",
		"decolorize", "del", "delete", "drop", "drop_empty_fields", "extract",
		"extract_regexp", "eval", "facets", "field_names", "field_values", "fields",
		"filter", "first", "format", "generate_sequence", "hash", "join",
		"json_array_concat", "json_array_len", "head", "keep", "last", "len",
		"limit", "math", "mv", "order", "pack_json", "pack_logfmt", "query_stats",
		"rename", "replace", "replace_regexp", "rm", "running_stats", "sample",
		"set_stream_fields", "skip", "sort", "split", "stats", "stats_remote",
		"stream_context", "time_add", "top", "total_stats", "union", "uniq",
		"unpack_json", "unpack_logfmt", "unpack_syslog", "unpack_words", "unroll",
		"where",

		// Stats function names ('stats by (...) <func>(...)').
		"any", "avg", "count", "count_empty", "count_uniq", "count_uniq_hash",
		"field_max", "field_min", "histogram", "json_values", "max", "median",
		"min", "quantile", "rate", "rate_sum", "row_any", "row_max", "row_min",
		"stddev", "sum", "sum_len", "uniq_values", "values",

		// Math function names ('| math <expr>').
		"abs", "ceil", "exp", "floor", "ln", "rand", "round",
	}

	set := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		set[tok] = struct{}{}
	}
	return set
}

// isSafeLogsQLFieldName reports whether name can be emitted as a bare
// (unquoted) LogsQL field token: it must look like a valid compound token
// AND not collide with a reserved keyword, pipe name, or function name.
func isSafeLogsQLFieldName(name string) bool {
	if !safeLogsQLFieldPattern.MatchString(name) {
		return false
	}
	_, reserved := logsQLReservedTokens[strings.ToLower(name)]
	return !reserved
}

type LogsQLTranslateOptions struct {
	DefaultTimestampField string
	// AllowAggregation accepts an aggregation pipe. Leave it false on a path
	// that needs log rows, and the query fails with AGGREGATION_NOT_ALLOWED.
	AllowAggregation bool
	// Interval is the request interval, the bucket window of a `by time` key
	// that has no window of its own.
	Interval string
	// BucketOffset aligns time buckets to the request timezone, as a LogsQL
	// duration such as "19800s". "" means UTC.
	BucketOffset string
}

type LogsQLTranslateResult struct {
	Query string `json:"query"`
	// Filter is Query without its aggregation pipes.
	Filter     string            `json:"filter"`
	Valid      bool              `json:"valid"`
	Error      *ParseError       `json:"error,omitempty"`
	Conditions []FilterCondition `json:"conditions"`
	FieldsUsed []string          `json:"fields_used"`
	// Aggregation describes the result of an aggregated query, with each
	// column's Source set to the field VictoriaLogs returns it under. Nil for
	// a query that returns log rows.
	Aggregation *AggregationInfo `json:"aggregation,omitempty"`
}

type LogsQLGenerator struct {
	defaultTimestampField string
	interval              string
	bucketOffset          string
}

func NewLogsQLGenerator(options *LogsQLTranslateOptions) *LogsQLGenerator {
	generator := &LogsQLGenerator{}
	if options != nil {
		generator.defaultTimestampField = options.DefaultTimestampField
		generator.interval = options.Interval
		generator.bucketOffset = options.BucketOffset
	}
	return generator
}

func TranslateToLogsQL(query string, options *LogsQLTranslateOptions) *LogsQLTranslateResult {
	result := &LogsQLTranslateResult{
		Valid:      false,
		Conditions: []FilterCondition{},
		FieldsUsed: []string{},
	}

	if strings.TrimSpace(query) == "" {
		result.Valid = true
		result.Query = "*"
		result.Filter = "*"
		return result
	}

	ast, perr := parseQuery(query)
	if perr == nil {
		perr = checkAggregationAllowed(ast, options != nil && options.AllowAggregation)
	}
	if perr != nil {
		result.Error = perr
		return result
	}

	generator := NewLogsQLGenerator(options)
	if queryNode, ok := ast.(*QueryNode); ok && queryNode.Aggregate != nil {
		compiled, genErr := generator.generateAggregation(queryNode)
		if genErr != nil {
			result.Error = genErr
			return result
		}
		result.Query, result.Filter, result.Aggregation = compiled.query, compiled.filter, compiled.info
	} else {
		logsql, genErr := generator.Generate(ast)
		if genErr != nil {
			result.Error = genErr
			return result
		}
		result.Query, result.Filter = logsql, logsql
	}

	result.Valid = true
	result.FieldsUsed = extractFieldsFromAST(ast)
	result.Conditions = extractConditionsFromAST(ast)
	return result
}

func (g *LogsQLGenerator) Generate(node ASTNode) (string, *ParseError) {
	if node == nil {
		return "*", nil
	}
	return g.visit(node)
}

func (g *LogsQLGenerator) visit(node ASTNode) (string, *ParseError) {
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
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("unsupported LogchefQL node type %T", node)}
	}
}

func (g *LogsQLGenerator) visitQuery(node *QueryNode) (string, *ParseError) {
	if node.Aggregate != nil {
		compiled, err := g.generateAggregation(node)
		if err != nil {
			return "", err
		}
		return compiled.query, nil
	}

	whereQuery, err := g.filter(node.Where)
	if err != nil {
		return "", err
	}

	if len(node.Select) == 0 {
		return whereQuery, nil
	}

	fields, err := g.buildFieldsPipe(node.Select)
	if err != nil {
		return "", err
	}
	if fields == "" {
		return whereQuery, nil
	}

	return fmt.Sprintf("%s | fields %s", whereQuery, fields), nil
}

// filter compiles the filter of a query. A query without one matches every row.
func (g *LogsQLGenerator) filter(where ASTNode) (string, *ParseError) {
	if where == nil {
		return "*", nil
	}
	query, err := g.visit(where)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(query) == "" {
		return "*", nil
	}
	return query, nil
}

func (g *LogsQLGenerator) visitLogical(node *LogicalNode) (string, *ParseError) {
	if node.Operator != BoolAnd && node.Operator != BoolOr {
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("unsupported boolean operator %q", node.Operator)}
	}
	if len(node.Children) == 0 {
		return "", nil
	}

	if len(node.Children) == 1 {
		return g.visit(node.Children[0])
	}

	parts := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		if child == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "logical expression contains a nil child"}
		}
		part, err := g.visit(child)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(part) == "" {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "logical expression contains an empty child"}
		}
		parts = append(parts, fmt.Sprintf("(%s)", part))
	}

	if len(parts) == 0 {
		return "", nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return strings.Join(parts, fmt.Sprintf(" %s ", node.Operator)), nil
}

func (g *LogsQLGenerator) visitGroup(node *GroupNode) (string, *ParseError) {
	if len(node.Children) == 0 {
		return "", nil
	}
	if len(node.Children) == 1 {
		part, err := g.visit(node.Children[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s)", part), nil
	}

	parts := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		if child == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "group contains a nil child"}
		}
		part, err := g.visit(child)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(part) == "" {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "group contains an empty child"}
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return fmt.Sprintf("(%s)", strings.Join(parts, " AND ")), nil
}

func (g *LogsQLGenerator) visitExpression(node *ExpressionNode) (string, *ParseError) {
	if nested, ok := node.Key.(NestedField); ok && (nested.Base == "" || len(nested.Path) == 0) {
		return "", &ParseError{Code: ErrInvalidIdentifier, Message: "field name is required"}
	}
	fieldName := g.formatFieldName(getFieldName(node.Key))
	if fieldName == "" {
		return "", &ParseError{Code: ErrInvalidIdentifier, Message: "field name is required"}
	}

	value, err := g.formatValue(node.Value)
	if err != nil {
		return "", err
	}

	switch node.Operator {
	case OpEquals:
		if node.Value == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "null comparisons are not supported for LogsQL translation"}
		}
		return fmt.Sprintf("%s:=%s", fieldName, value), nil
	case OpNotEquals:
		if node.Value == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "null comparisons are not supported for LogsQL translation"}
		}
		return fmt.Sprintf("NOT %s:=%s", fieldName, value), nil
	case OpRegex:
		if node.Value == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "null comparisons are not supported for LogsQL translation"}
		}
		regexValue, regexErr := g.formatSubstringAsRegex(node.Value)
		if regexErr != nil {
			return "", regexErr
		}
		return fmt.Sprintf("%s:~%s", fieldName, regexValue), nil
	case OpNotRegex:
		if node.Value == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "null comparisons are not supported for LogsQL translation"}
		}
		regexValue, regexErr := g.formatSubstringAsRegex(node.Value)
		if regexErr != nil {
			return "", regexErr
		}
		return fmt.Sprintf("NOT %s:~%s", fieldName, regexValue), nil
	case OpGT, OpGTE, OpLT, OpLTE:
		if node.Value == nil {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: "null comparisons are not supported for LogsQL translation"}
		}
		return fmt.Sprintf("%s:%s%s", fieldName, node.Operator, value), nil
	default:
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("unsupported operator %q for LogsQL translation", node.Operator)}
	}
}

func (g *LogsQLGenerator) buildFieldsPipe(selectFields []SelectField) (string, *ParseError) {
	if len(selectFields) == 0 {
		return "", nil
	}

	seen := make(map[string]struct{}, len(selectFields)+1)
	fields := make([]string, 0, len(selectFields)+1)

	addField := func(fieldName string) {
		trimmed := strings.TrimSpace(fieldName)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		fields = append(fields, trimmed)
	}

	addField(g.formatFieldName(g.defaultTimestampField))

	for _, selectField := range selectFields {
		fieldName := getFieldName(selectField.Field)
		if fieldName == "" {
			return "", &ParseError{Code: ErrInvalidIdentifier, Message: "field name is required"}
		}
		addField(g.formatFieldName(fieldName))
	}

	return strings.Join(fields, ", "), nil
}

func (g *LogsQLGenerator) formatFieldName(fieldName string) string {
	if fieldName == "" {
		return ""
	}
	if isSafeLogsQLFieldName(fieldName) {
		return fieldName
	}
	return strconv.Quote(fieldName)
}

func (g *LogsQLGenerator) formatValue(value any) (string, *ParseError) {
	switch v := value.(type) {
	case nil:
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: "null comparisons are not supported for LogsQL translation"}
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case NumericLiteral:
		return string(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int32, int64, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case string:
		return strconv.Quote(v), nil
	default:
		return strconv.Quote(fmt.Sprintf("%v", v)), nil
	}
}

func (g *LogsQLGenerator) formatSubstringAsRegex(value any) (string, *ParseError) {
	text, ok := value.(string)
	if !ok {
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: "substring matches require string values"}
	}
	return strconv.Quote("(?i)" + regexp.QuoteMeta(text)), nil
}

// logsQLAggregation is a compiled aggregation: the whole query, its filter,
// and the description of its result.
type logsQLAggregation struct {
	query  string
	filter string
	info   *AggregationInfo
}

// logsQLFuncs maps each function to its LogsQL form. %s is the formatted
// field. A number function counts only rows that have the field, so a row
// without it is skipped and not read as an empty value. The filter is
// `NOT f:""` and not the any value filter `f:*`: on VictoriaLogs v1.52.0
// `if (f:*)` matches no row of a block that stores f as an integer column,
// so every number function over an integer field returned null.
var logsQLFuncs = map[AggFunc]string{
	AggCountDistinct: "count_uniq(%s)",
	AggSum:           `sum(%[1]s) if (NOT %[1]s:"")`,
	AggAvg:           `avg(%[1]s) if (NOT %[1]s:"")`,
	AggMin:           `min(%[1]s) if (NOT %[1]s:"")`,
	AggMax:           `max(%[1]s) if (NOT %[1]s:"")`,
	AggP50:           `quantile(0.5, %[1]s) if (NOT %[1]s:"")`,
	AggP75:           `quantile(0.75, %[1]s) if (NOT %[1]s:"")`,
	AggP90:           `quantile(0.9, %[1]s) if (NOT %[1]s:"")`,
	AggP95:           `quantile(0.95, %[1]s) if (NOT %[1]s:"")`,
	AggP99:           `quantile(0.99, %[1]s) if (NOT %[1]s:"")`,
}

// generateAggregation compiles a query with an aggregation pipe into a
// `stats` pipe and its `sort` and `limit` stages. The query never renames: a
// value takes the internal name __lc_v<i>, a key keeps its field name and the
// bucket is _time. VictoriaLogs rejects a value name that equals a `by`
// field, so an alias in the query could collide where an internal name
// cannot. The provider writes each row under the output names.
func (g *LogsQLGenerator) generateAggregation(node *QueryNode) (*logsQLAggregation, *ParseError) {
	agg := node.Aggregate
	filter, err := g.filter(node.Where)
	if err != nil {
		return nil, err
	}
	info, err := g.aggregationInfo(agg)
	if err != nil {
		return nil, err
	}

	var by, keys, values []string
	for _, col := range info.Columns {
		switch col.Role {
		case RoleTime:
			bucket := "_time:" + info.Interval
			if g.bucketOffset != "" {
				bucket += " offset " + g.bucketOffset
			}
			by = append(by, bucket)
			keys = append(keys, g.statsFieldName(col.Source))
		case RoleGroup:
			by = append(by, g.statsFieldName(col.Source))
			keys = append(keys, g.statsFieldName(col.Source))
		case RoleValue:
			a := agg.Aggregates[len(values)]
			expr := "count()"
			if a.Func != AggCount {
				expr = fmt.Sprintf(logsQLFuncs[a.Func], g.statsFieldName(getFieldName(a.Field)))
			}
			values = append(values, expr+" as "+col.Source)
		}
	}

	stats := "stats "
	if len(by) > 0 {
		stats += "by (" + strings.Join(by, ", ") + ") "
	}
	pipes := []string{filter, stats + strings.Join(values, ", ")}
	if order := g.aggregationOrder(agg, info, keys); len(order) > 0 {
		pipes = append(pipes, "sort by ("+strings.Join(order, ", ")+")")
	}
	if agg.Limit > 0 {
		pipes = append(pipes, "limit "+strconv.Itoa(agg.Limit))
	}
	return &logsQLAggregation{query: strings.Join(pipes, " | "), filter: filter, info: info}, nil
}

// statsFieldName formats a field for the stats, sort and if clauses. A bare
// name with a colon is a bucket there, so `by (latency:10)` would group the
// field latency in buckets of 10: such a name is quoted.
func (g *LogsQLGenerator) statsFieldName(name string) string {
	if strings.Contains(name, ":") {
		return strconv.Quote(name)
	}
	return g.formatFieldName(name)
}

func logsQLValueName(i int) string {
	return reservedPrefix + "v" + strconv.Itoa(i)
}

// aggregationInfo describes the result and records the field that
// VictoriaLogs returns each column under.
func (g *LogsQLGenerator) aggregationInfo(agg *AggregationNode) (*AggregationInfo, *ParseError) {
	info := agg.Info(g.interval)
	fieldKeys := make([]GroupKey, 0, len(agg.By))
	for _, key := range agg.By {
		if key.Time && info.Interval == "" {
			return nil, aggregationError(key.pos, "this query buckets by time, so the request needs an interval")
		}
		if !key.Time {
			fieldKeys = append(fieldKeys, key)
		}
	}
	hasTime := len(fieldKeys) < len(agg.By)
	value := 0
	for i := range info.Columns {
		col := &info.Columns[i]
		switch col.Role {
		case RoleTime:
			col.Source = "_time"
		case RoleGroup:
			key := fieldKeys[0]
			fieldKeys = fieldKeys[1:]
			col.Source = getFieldName(key.Field)
			if hasTime && col.Source == "_time" {
				pos := key.pos
				return nil, &ParseError{
					Code:     ErrAggregationNotSupported,
					Message:  "cannot group by the field _time and by time together on this source, because the time bucket is the _time field",
					Position: &pos,
				}
			}
		case RoleValue:
			col.Source = logsQLValueName(value)
			value++
		}
	}
	return info, nil
}

// aggregationOrder lists the `sort by` entries. keys are the formatted key
// fields in result order, the time bucket first. The order is total, so a
// capped result keeps the same rows from run to run: a `sort` stage, or the
// first value descending for a grouped shape, then every key ascending.
func (g *LogsQLGenerator) aggregationOrder(agg *AggregationNode, info *AggregationInfo, keys []string) []string {
	var first string
	desc := false
	switch {
	case agg.Sort != nil:
		for _, col := range info.Columns {
			if col.Name == agg.Sort.Name {
				first, desc = g.statsFieldName(col.Source), agg.Sort.Desc
				break
			}
		}
	case info.Shape == ShapeGrouped:
		first, desc = logsQLValueName(0), true
	}

	order := make([]string, 0, len(keys)+1)
	if first != "" {
		entry := first
		if desc {
			entry += " desc"
		}
		order = append(order, entry)
	}
	for _, key := range keys {
		if key != first {
			order = append(order, key)
		}
	}
	return order
}
