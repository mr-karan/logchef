package logchefql

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// LogQLMatcher is one matcher of a Loki stream selector, such as app=~"api.*".
// Op is "=", "!=", "=~" or "!~".
type LogQLMatcher struct {
	Name  string
	Op    string
	Value string
}

// LogQLTranslateOptions carries what the generator must know about the Loki
// source (RFC 0011 "LogchefQL to LogQL").
type LogQLTranslateOptions struct {
	// Selector is the source's default stream selector. Every compiled query
	// starts from it.
	Selector []LogQLMatcher
	// Parser is "", "json" or "logfmt". A parser runs on every compiled query,
	// so parsed fields appear as columns and filters can use them.
	Parser string
	// StreamLabels names the source's stream labels. A top-level condition on
	// one becomes a stream matcher, and a select pipe always keeps them.
	StreamLabels []string
}

type LogQLTranslateResult struct {
	Query      string            `json:"query"`
	Valid      bool              `json:"valid"`
	Error      *ParseError       `json:"error,omitempty"`
	Conditions []FilterCondition `json:"conditions"`
	FieldsUsed []string          `json:"fields_used"`
}

// logQLMessageField is the log line. LogQL filters it with line filters, never
// with a label filter.
const logQLMessageField = "_msg"

var logQLLabelNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// logQLKeywords are the words Loki's lexer always turns into a token, in any
// case, instead of an identifier (pkg/logql/syntax/lex.go at v3.7.6). A label
// with one of these names cannot be written in a filter, a matcher or keep.
// Function names such as ip and count lex as identifiers when no "(" follows.
var logQLKeywords = map[string]struct{}{
	"by": {}, "without": {}, "bool": {}, "offset": {}, "on": {}, "ignoring": {},
	"group_left": {}, "group_right": {}, "unwrap": {}, "or": {}, "and": {},
	"unless": {}, "json": {}, "regexp": {}, "logfmt": {}, "unpack": {},
	"pattern": {}, "label_format": {}, "line_format": {}, "decolorize": {},
	"drop": {}, "keep": {}, "variants": {}, "of": {},
}

// TranslateToLogQL compiles a LogchefQL query to a LogQL log query of the form
//
//	{<selector>, <hoisted>} <line filters> | <parser> | drop __error__, __error_details__ | <label filter> | __error__="" | keep <fields>
//
// Only the selector is always present.
func TranslateToLogQL(query string, options LogQLTranslateOptions) *LogQLTranslateResult {
	result := &LogQLTranslateResult{Conditions: []FilterCondition{}, FieldsUsed: []string{}}

	var ast ASTNode
	if strings.TrimSpace(query) != "" {
		parsed, perr := parseQuery(query)
		if perr != nil {
			result.Error = perr
			return result
		}
		ast = parsed
	}

	logql, perr := generateLogQL(ast, options)
	if perr != nil {
		result.Error = perr
		return result
	}
	result.Valid = true
	result.Query = logql
	if ast != nil {
		result.FieldsUsed = extractFieldsFromAST(ast)
		result.Conditions = extractConditionsFromAST(ast)
	}
	return result
}

type logQLQuery struct {
	streamLabels map[string]struct{}
	matchers     []LogQLMatcher
	lineFilters  []string
	labelFilters []string
	// numeric is set when a label filter compares numbers. Loki keeps a line
	// whose value fails to convert and marks it with __error__.
	numeric bool
}

func generateLogQL(ast ASTNode, options LogQLTranslateOptions) (string, *ParseError) {
	switch options.Parser {
	case "", "json", "logfmt":
	default:
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("unsupported Loki parser %q, use json or logfmt", options.Parser)}
	}
	q := &logQLQuery{
		streamLabels: make(map[string]struct{}, len(options.StreamLabels)),
		matchers:     slices.Clone(options.Selector),
	}
	for _, label := range options.StreamLabels {
		q.streamLabels[label] = struct{}{}
	}

	where := ast
	var selects []SelectField
	if node, ok := ast.(*QueryNode); ok {
		if node.Aggregate != nil {
			return "", errAggregationNotSupported(node.Aggregate)
		}
		where, selects = node.Where, node.Select
	}
	for _, operand := range flattenAnd(where) {
		if err := q.addOperand(operand); err != nil {
			return "", err
		}
	}

	selector, err := q.selector()
	if err != nil {
		return "", err
	}
	stages := []string{selector}
	stages = append(stages, q.lineFilters...)
	if options.Parser != "" {
		stages = append(stages, "| "+options.Parser, "| drop __error__, __error_details__")
	}
	if len(q.labelFilters) > 0 {
		stages = append(stages, "| "+strings.Join(q.labelFilters, " and "))
		if q.numeric {
			stages = append(stages, `| __error__=""`)
		}
	}
	if len(selects) > 0 {
		keep, err := q.keepFields(selects)
		if err != nil {
			return "", err
		}
		if len(keep) > 0 {
			stages = append(stages, "| keep "+strings.Join(keep, ", "))
		}
	}
	return strings.Join(stages, " "), nil
}

// flattenAnd returns the top-level and operands. Parentheses around an and or
// a single condition are dropped, since and is associative. Parentheses around
// an or stay, because and binds tighter.
func flattenAnd(node ASTNode) []ASTNode {
	switch n := node.(type) {
	case nil:
		return nil
	case *GroupNode:
		if len(n.Children) == 1 {
			if inner, ok := n.Children[0].(*LogicalNode); !ok || inner.Operator == BoolAnd {
				return flattenAnd(n.Children[0])
			}
		}
	case *LogicalNode:
		if n.Operator == BoolAnd {
			var operands []ASTNode
			for _, child := range n.Children {
				operands = append(operands, flattenAnd(child)...)
			}
			return operands
		}
	}
	return []ASTNode{node}
}

func unwrapGroups(node ASTNode) ASTNode {
	for {
		group, ok := node.(*GroupNode)
		if !ok || len(group.Children) != 1 {
			return node
		}
		node = group.Children[0]
	}
}

// addOperand places one top-level and operand: a stream matcher, a line
// filter, or a part of the label filter.
func (q *logQLQuery) addOperand(operand ASTNode) *ParseError {
	switch n := unwrapGroups(operand).(type) {
	case *ExpressionNode:
		name, err := logQLFieldName(n.Key)
		if err != nil {
			return err
		}
		if name == logQLMessageField {
			filter, err := logQLLineFilter(n)
			if err != nil {
				return err
			}
			q.lineFilters = append(q.lineFilters, filter)
			return nil
		}
		if _, ok := q.streamLabels[name]; ok && n.Value != nil {
			if op, ok := logQLStreamOp(n.Operator); ok {
				q.matchers = append(q.matchers, LogQLMatcher{Name: name, Op: op, Value: logQLMatcherValue(n)})
				return nil
			}
		}
	case *LogicalNode:
		if filter, ok := logQLMessageAlternation(n); ok {
			q.lineFilters = append(q.lineFilters, filter)
			return nil
		}
	}
	filter, err := q.labelFilter(operand)
	if err != nil {
		return err
	}
	q.labelFilters = append(q.labelFilters, filter)
	return nil
}

func logQLStreamOp(op Operator) (string, bool) {
	switch op {
	case OpEquals:
		return "=", true
	case OpNotEquals:
		return "!=", true
	case OpRegex:
		return "=~", true
	case OpNotRegex:
		return "!~", true
	default:
		return "", false
	}
}

// logQLMatcherValue is the matcher value of a stream condition. ~ is a
// case-insensitive substring, and label regexes are anchored.
func logQLMatcherValue(n *ExpressionNode) string {
	text := logQLValueText(n.Value)
	if n.Operator == OpRegex || n.Operator == OpNotRegex {
		return "(?i).*" + regexp.QuoteMeta(text) + ".*"
	}
	return text
}

// logQLLineFilter compiles a condition on _msg. Line regexes are not anchored.
func logQLLineFilter(n *ExpressionNode) (string, *ParseError) {
	if n.Value == nil {
		return "", errLogQLNull()
	}
	text := regexp.QuoteMeta(logQLValueText(n.Value))
	switch n.Operator {
	case OpRegex:
		return "|~ " + strconv.Quote("(?i)"+text), nil
	case OpNotRegex:
		return "!~ " + strconv.Quote("(?i)"+text), nil
	case OpEquals:
		return "|~ " + strconv.Quote("^"+text+"$"), nil
	case OpNotEquals:
		return "!~ " + strconv.Quote("^"+text+"$"), nil
	default:
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("Loki cannot compare the log line with %s; use ~ or = on _msg", n.Operator)}
	}
}

// logQLMessageAlternation compiles an or whose every operand is a positive
// match on _msg into one line filter.
func logQLMessageAlternation(n *LogicalNode) (string, bool) {
	if n.Operator != BoolOr {
		return "", false
	}
	texts := make([]string, 0, len(n.Children))
	exact := make([]bool, 0, len(n.Children))
	for _, child := range n.Children {
		expr, ok := unwrapGroups(child).(*ExpressionNode)
		if !ok || expr.Value == nil || (expr.Operator != OpRegex && expr.Operator != OpEquals) {
			return "", false
		}
		if name, err := logQLFieldName(expr.Key); err != nil || name != logQLMessageField {
			return "", false
		}
		texts = append(texts, regexp.QuoteMeta(logQLValueText(expr.Value)))
		exact = append(exact, expr.Operator == OpEquals)
	}
	if !slices.Contains(exact, true) {
		return "|~ " + strconv.Quote("(?i)("+strings.Join(texts, "|")+")"), true
	}
	for i, text := range texts {
		if exact[i] {
			texts[i] = "^" + text + "$"
		} else {
			texts[i] = "(?i:" + text + ")"
		}
	}
	return "|~ " + strconv.Quote(strings.Join(texts, "|")), true
}

// labelFilter renders a condition tree as a LogQL label filter, keeping its
// and, or and parentheses. Stream labels, structured metadata and parsed
// fields are all valid here, but the log line is not.
func (q *logQLQuery) labelFilter(node ASTNode) (string, *ParseError) {
	switch n := node.(type) {
	case *ExpressionNode:
		return q.labelComparison(n)
	case *GroupNode:
		parts := make([]string, 0, len(n.Children))
		for _, child := range n.Children {
			part, err := q.labelFilter(child)
			if err != nil {
				return "", err
			}
			parts = append(parts, part)
		}
		return "(" + strings.Join(parts, " and ") + ")", nil
	case *LogicalNode:
		children := n.Children
		if n.Operator == BoolOr {
			ordered, err := orderLogQLOr(children)
			if err != nil {
				return "", err
			}
			children = ordered
		}
		parts := make([]string, 0, len(children))
		for _, child := range children {
			part, err := q.labelFilter(child)
			if err != nil {
				return "", err
			}
			parts = append(parts, part)
		}
		return strings.Join(parts, " "+strings.ToLower(string(n.Operator))+" "), nil
	default:
		return "", unsupportedASTNode(node)
	}
}

// orderLogQLOr moves the operands that compare numbers to the end of an or.
// Loki's or stops at the first true operand, and a numeric comparison on a
// value that is not a number returns true and sets __error__, which the final
// __error__="" stage then drops. Run last, such a comparison errors only when
// every other operand is false, so dropping the line is the right answer.
// That holds for one numeric operand, or for several plain comparisons on one
// field, which all fail together.
func orderLogQLOr(children []ASTNode) ([]ASTNode, *ParseError) {
	var plain, numeric []ASTNode
	for _, child := range children {
		if logQLComparesNumbers(child) {
			numeric = append(numeric, child)
		} else {
			plain = append(plain, child)
		}
	}
	if len(numeric) > 1 {
		field := ""
		for _, child := range numeric {
			expr, ok := unwrapGroups(child).(*ExpressionNode)
			if !ok || (field != "" && getFieldName(expr.Key) != field) {
				return nil, &ParseError{
					Code:    ErrUnsupportedFeature,
					Message: "Loki can run an or with numeric comparisons on one field only, because a value that is not a number ends the or early. Split the query",
				}
			}
			field = getFieldName(expr.Key)
		}
	}
	return append(plain, numeric...), nil
}

func logQLComparesNumbers(node ASTNode) bool {
	switch n := node.(type) {
	case *ExpressionNode:
		switch n.Operator {
		case OpGT, OpGTE, OpLT, OpLTE:
			return true
		case OpEquals, OpNotEquals:
			_, ok := n.Value.(NumericLiteral)
			return ok
		default:
			return false
		}
	case *GroupNode:
		return slices.ContainsFunc(n.Children, logQLComparesNumbers)
	case *LogicalNode:
		return slices.ContainsFunc(n.Children, logQLComparesNumbers)
	default:
		return false
	}
}

func (q *logQLQuery) labelComparison(n *ExpressionNode) (string, *ParseError) {
	name, err := logQLFieldName(n.Key)
	if err != nil {
		return "", err
	}
	if name == logQLMessageField {
		return "", &ParseError{
			Code:    ErrUnsupportedFeature,
			Message: "Loki cannot join a condition on _msg with an or to a condition on another field, or negate _msg inside an or. Move the _msg condition out of the or",
		}
	}
	if n.Value == nil {
		return "", errLogQLNull()
	}
	switch n.Operator {
	case OpEquals, OpNotEquals:
		if number, ok := n.Value.(NumericLiteral); ok {
			q.numeric = true
			return name + string(n.Operator) + string(number), nil
		}
		return name + string(n.Operator) + strconv.Quote(logQLValueText(n.Value)), nil
	case OpRegex, OpNotRegex:
		op, _ := logQLStreamOp(n.Operator)
		return name + op + strconv.Quote(logQLMatcherValue(n)), nil
	case OpGT, OpGTE, OpLT, OpLTE:
		number, ok := logQLNumber(n.Value)
		if !ok {
			return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("%s %s needs a number on Loki", name, n.Operator)}
		}
		q.numeric = true
		return name + string(n.Operator) + number, nil
	default:
		return "", &ParseError{Code: ErrUnknownOperator, Message: fmt.Sprintf("unsupported operator %q for LogQL translation", n.Operator)}
	}
}

// logQLNumberPattern is a LogchefQL number, which is also a valid LogQL
// number literal. strconv.ParseFloat accepts more, such as NaN and 1e3, that
// LogQL does not.
var logQLNumberPattern = regexp.MustCompile(`^[-+]?\d*\.?\d+$`)

func logQLNumber(value any) (string, bool) {
	switch v := value.(type) {
	case NumericLiteral:
		return string(v), true
	case string:
		if logQLNumberPattern.MatchString(v) {
			return v, true
		}
	}
	return "", false
}

// logQLValueText is a value as label text. A bare identifier is a string, so
// x=true compares with "true".
func logQLValueText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case NumericLiteral:
		return string(v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

// logQLFieldName maps a LogchefQL field to a Loki label name. A dotted path
// joins with "_", as Loki's json parser flattens nested keys and its OTLP
// ingestion rewrites dots in attribute names.
func logQLFieldName(key any) (string, *ParseError) {
	var name string
	switch k := key.(type) {
	case string:
		name = k
	case NestedField:
		name = strings.Join(append([]string{k.Base}, k.Path...), "_")
	}
	if name == "" {
		return "", &ParseError{Code: ErrInvalidIdentifier, Message: "field name is required"}
	}
	if !logQLLabelNamePattern.MatchString(name) {
		return "", &ParseError{
			Code:    ErrUnsupportedFeature,
			Message: fmt.Sprintf("field %q is not a valid Loki label name: use letters, digits and underscores, starting with a letter or underscore", getFieldName(key)),
		}
	}
	if _, ok := logQLKeywords[strings.ToLower(name)]; ok {
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("field %q is a LogQL keyword and cannot be used as a label name", name)}
	}
	return name, nil
}

func errLogQLNull() *ParseError {
	return &ParseError{Code: ErrUnsupportedFeature, Message: "null comparisons are not supported for LogQL translation"}
}

// selector renders the stream selector and rejects one that matches the
// empty string, which Loki refuses.
func (q *logQLQuery) selector() (string, *ParseError) {
	parts := make([]string, 0, len(q.matchers))
	nonEmpty := false
	for _, m := range q.matchers {
		if !logQLLabelNamePattern.MatchString(m.Name) {
			return "", &ParseError{Code: ErrInvalidIdentifier, Message: fmt.Sprintf("invalid label name %q in the stream selector", m.Name)}
		}
		matchesEmpty, err := logQLMatcherMatchesEmpty(m)
		if err != nil {
			return "", err
		}
		nonEmpty = nonEmpty || !matchesEmpty
		parts = append(parts, m.Name+m.Op+strconv.Quote(m.Value))
	}
	if !nonEmpty {
		return "", &ParseError{
			Code:    ErrUnsupportedFeature,
			Message: `the stream selector matches the empty string; the source selector needs a matcher such as {service_name=~".+"}`,
		}
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
}

func logQLMatcherMatchesEmpty(m LogQLMatcher) (bool, *ParseError) {
	switch m.Op {
	case "=":
		return m.Value == "", nil
	case "!=":
		return m.Value != "", nil
	case "=~", "!~":
		re, err := regexp.Compile("^(?:" + m.Value + ")$")
		if err != nil {
			return false, &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("invalid regex for label %s: %v", m.Name, err)}
		}
		return re.MatchString("") == (m.Op == "=~"), nil
	default:
		return false, &ParseError{Code: ErrUnknownOperator, Message: fmt.Sprintf("invalid operator %q for label %s in the stream selector", m.Op, m.Name)}
	}
}

// keepFields lists the selected fields and then every stream label. Loki
// always returns the timestamp and the line, and the stream labels identify
// the row's stream for log context.
func (q *logQLQuery) keepFields(selects []SelectField) ([]string, *ParseError) {
	seen := map[string]struct{}{}
	var keep []string
	add := func(name string) {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			keep = append(keep, name)
		}
	}
	for _, field := range selects {
		name, err := logQLFieldName(field.Field)
		if err != nil {
			return nil, err
		}
		if name != logQLMessageField && name != "_time" {
			add(name)
		}
	}
	labels := make([]string, 0, len(q.streamLabels))
	for label := range q.streamLabels {
		labels = append(labels, label)
	}
	slices.Sort(labels)
	for _, label := range labels {
		add(label)
	}
	return keep, nil
}
