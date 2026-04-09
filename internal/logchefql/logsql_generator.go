package logchefql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var safeLogsQLFieldPattern = regexp.MustCompile(`^@?[A-Za-z_][A-Za-z0-9_@.]*$`)

type LogsQLTranslateOptions struct {
	DefaultTimestampField string
}

type LogsQLTranslateResult struct {
	Query      string            `json:"query"`
	Valid      bool              `json:"valid"`
	Error      *ParseError       `json:"error,omitempty"`
	Conditions []FilterCondition `json:"conditions"`
	FieldsUsed []string          `json:"fields_used"`
}

type LogsQLGenerator struct {
	defaultTimestampField string
}

func NewLogsQLGenerator(options *LogsQLTranslateOptions) *LogsQLGenerator {
	generator := &LogsQLGenerator{}
	if options != nil {
		generator.defaultTimestampField = strings.TrimSpace(options.DefaultTimestampField)
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
		return result
	}

	pq, err := ParseLogchefQL(query)
	if err != nil {
		result.Error = convertParticipleError(err)
		return result
	}

	ast := ConvertToAST(pq)
	generator := NewLogsQLGenerator(options)
	logsql, genErr := generator.Generate(ast)
	if genErr != nil {
		result.Error = genErr
		return result
	}

	result.Valid = true
	result.Query = logsql
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
		return g.visitExpression(n)
	case *LogicalNode:
		return g.visitLogical(n)
	case *GroupNode:
		return g.visitGroup(n)
	case *QueryNode:
		return g.visitQuery(n)
	default:
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: fmt.Sprintf("unsupported LogchefQL node type %T", node)}
	}
}

func (g *LogsQLGenerator) visitQuery(node *QueryNode) (string, *ParseError) {
	whereQuery := "*"
	if node.Where != nil {
		query, err := g.visit(node.Where)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(query) != "" {
			whereQuery = query
		}
	}

	if len(node.Select) == 0 {
		return whereQuery, nil
	}

	fields := g.buildFieldsPipe(node.Select)
	if fields == "" {
		return whereQuery, nil
	}

	return fmt.Sprintf("%s | fields %s", whereQuery, fields), nil
}

func (g *LogsQLGenerator) visitLogical(node *LogicalNode) (string, *ParseError) {
	if len(node.Children) == 0 {
		return "", nil
	}

	if len(node.Children) == 1 {
		return g.visit(node.Children[0])
	}

	parts := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		part, err := g.visit(child)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(part) == "" {
			continue
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
		part, err := g.visit(child)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(part) == "" {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return fmt.Sprintf("(%s)", strings.Join(parts, " AND ")), nil
}

func (g *LogsQLGenerator) visitExpression(node *ExpressionNode) (string, *ParseError) {
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

func (g *LogsQLGenerator) buildFieldsPipe(selectFields []SelectField) string {
	if len(selectFields) == 0 {
		return ""
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
		addField(g.formatFieldName(getFieldName(selectField.Field)))
	}

	return strings.Join(fields, ", ")
}

func (g *LogsQLGenerator) formatFieldName(fieldName string) string {
	trimmed := strings.TrimSpace(fieldName)
	if trimmed == "" {
		return ""
	}
	if safeLogsQLFieldPattern.MatchString(trimmed) {
		return trimmed
	}
	return strconv.Quote(trimmed)
}

func (g *LogsQLGenerator) formatValue(value interface{}) (string, *ParseError) {
	switch v := value.(type) {
	case nil:
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: "null comparisons are not supported for LogsQL translation"}
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
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

func (g *LogsQLGenerator) formatSubstringAsRegex(value interface{}) (string, *ParseError) {
	text, ok := value.(string)
	if !ok {
		return "", &ParseError{Code: ErrUnsupportedFeature, Message: "substring matches require string values"}
	}
	return strconv.Quote("(?i)" + regexp.QuoteMeta(text)), nil
}
