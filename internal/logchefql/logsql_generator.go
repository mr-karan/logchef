package logchefql

import (
	"fmt"
	"strings"
)

type LogsQLGenerator struct {
	schema *Schema
}

func NewLogsQLGenerator(schema *Schema) *LogsQLGenerator {
	return &LogsQLGenerator{schema: schema}
}

func (g *LogsQLGenerator) Generate(node ASTNode) string {
	if node == nil {
		return ""
	}
	return g.visit(node)
}

func (g *LogsQLGenerator) visit(node ASTNode) string {
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

func (g *LogsQLGenerator) visitQuery(node *QueryNode) string {
	if node.Where != nil {
		return g.visit(node.Where)
	}
	return ""
}

func (g *LogsQLGenerator) visitExpression(node *ExpressionNode) string {
	key := g.formatKey(node.Key)
	value := g.formatValue(node.Value)

	switch node.Operator {
	case OpEquals:
		return fmt.Sprintf("%s:=%s", key, value)
	case OpNotEquals:
		return fmt.Sprintf("%s:!=%s", key, value)
	case OpRegex:
		return fmt.Sprintf("%s:~%s", key, value)
	case OpNotRegex:
		return fmt.Sprintf("%s:!~%s", key, value)
	case OpGT:
		return fmt.Sprintf("%s:>%s", key, value)
	case OpLT:
		return fmt.Sprintf("%s:<%s", key, value)
	case OpGTE:
		return fmt.Sprintf("%s:>=%s", key, value)
	case OpLTE:
		return fmt.Sprintf("%s:<=%s", key, value)
	default:
		return ""
	}
}

func (g *LogsQLGenerator) visitLogical(node *LogicalNode) string {
	if len(node.Children) == 0 {
		return ""
	}

	if len(node.Children) == 1 {
		return g.visit(node.Children[0])
	}

	var conditions []string
	for _, child := range node.Children {
		logsql := g.visit(child)
		if logsql != "" {
			conditions = append(conditions, logsql)
		}
	}

	if len(conditions) == 0 {
		return ""
	}

	if len(conditions) == 1 {
		return conditions[0]
	}

	if node.Operator == BoolOr {
		return "(" + strings.Join(conditions, " or ") + ")"
	}

	return strings.Join(conditions, " ")
}

func (g *LogsQLGenerator) visitGroup(node *GroupNode) string {
	if len(node.Children) == 0 {
		return ""
	}

	if len(node.Children) == 1 {
		return g.visit(node.Children[0])
	}

	var conditions []string
	for _, child := range node.Children {
		logsql := g.visit(child)
		if logsql != "" {
			conditions = append(conditions, logsql)
		}
	}

	if len(conditions) == 0 {
		return ""
	}

	if len(conditions) == 1 {
		return conditions[0]
	}

	return "(" + strings.Join(conditions, " ") + ")"
}

func (g *LogsQLGenerator) formatKey(key interface{}) string {
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

func (g *LogsQLGenerator) formatValue(value interface{}) string {
	if value == nil {
		return "\"\""
	}

	switch v := value.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v)
	case string:
		if g.needsQuoting(v) {
			return fmt.Sprintf("\"%s\"", g.escapeLogsQLString(v))
		}
		return v
	default:
		s := fmt.Sprintf("%v", v)
		if g.needsQuoting(s) {
			return fmt.Sprintf("\"%s\"", g.escapeLogsQLString(s))
		}
		return s
	}
}

func (g *LogsQLGenerator) needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, c := range s {
		if c == ' ' || c == '"' || c == '\'' || c == '(' || c == ')' ||
			c == ':' || c == '|' || c == '\\' || c == '\n' || c == '\r' || c == '\t' {
			return true
		}
	}
	return false
}

func (g *LogsQLGenerator) escapeLogsQLString(s string) string {
	result := strings.ReplaceAll(s, "\\", "\\\\")
	result = strings.ReplaceAll(result, "\"", "\\\"")
	result = strings.ReplaceAll(result, "\n", "\\n")
	result = strings.ReplaceAll(result, "\r", "\\r")
	result = strings.ReplaceAll(result, "\t", "\\t")
	return result
}

func (g *LogsQLGenerator) GenerateSelectClause(selectFields []SelectField) string {
	if len(selectFields) == 0 {
		return ""
	}

	var fields []string
	for _, sf := range selectFields {
		field := g.formatKey(sf.Field)
		if field != "" {
			fields = append(fields, field)
		}
	}

	if len(fields) == 0 {
		return ""
	}

	return strings.Join(fields, ", ")
}
