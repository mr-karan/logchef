package logchefql

import (
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

var logchefQLLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Whitespace", Pattern: `[ \t\n\r]+`},

	{Name: "String", Pattern: `"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`},

	{Name: "Operator", Pattern: `!=|!~|>=|<=|[=~><]`},

	{Name: "Pipe", Pattern: `\|`},

	{Name: "LParen", Pattern: `\(`},
	{Name: "RParen", Pattern: `\)`},

	{Name: "Dot", Pattern: `\.`},

	{Name: "Number", Pattern: `[-+]?[0-9]*\.?[0-9]+`},

	{Name: "Ident", Pattern: `@?[a-zA-Z_][a-zA-Z0-9_:@-]*`},
})

// PQuery is the top-level query with optional WHERE clause and optional SELECT (pipe)
type PQuery struct {
	Where  *POrExpr       `parser:"@@?"`
	Select []*PSelectItem `parser:"( Pipe @@+ )?"`
}

// POrExpr handles OR precedence (lowest)
type POrExpr struct {
	Left  *PAndExpr  `parser:"@@"`
	Right []*POrTail `parser:"@@*"`
}

// POrTail captures "or <and_expr>"
type POrTail struct {
	Right *PAndExpr `parser:"'or':Ident @@"`
}

// PAndExpr handles AND precedence (higher than OR)
type PAndExpr struct {
	Left  *PTerm      `parser:"@@"`
	Right []*PAndTail `parser:"@@*"`
}

// PAndTail captures "and <term>"
type PAndTail struct {
	Right *PTerm `parser:"'and':Ident @@"`
}

// PTerm is either a grouped expression or a comparison
type PTerm struct {
	Group      *POrExpr     `parser:"( LParen @@ RParen"`
	Comparison *PComparison `parser:"| @@ )"`
}

type PComparison struct {
	Field    *PFieldPath `parser:"@@"`
	Operator string      `parser:"@Operator"`
	Value    *PValue     `parser:"@@"`
}

type PFieldPath struct {
	First *PPathSegment   `parser:"@@"`
	Rest  []*PPathSegment `parser:"( Dot @@ )*"`
}

type PPathSegment struct {
	Ident  *string `parser:"( @Ident"`
	Quoted *string `parser:"| @String )"`
}

// PValue can be a string, number, or bare identifier (for booleans/null/unquoted values)
type PValue struct {
	String *string  `parser:"( @String"`
	Number *float64 `parser:"| @Number"`
	Ident  *string  `parser:"| @Ident )"`
}

type PSelectItem struct {
	Field *PFieldPath `parser:"@@"`
}

// Parser instance
var logchefQLParser = participle.MustBuild[PQuery](
	participle.Lexer(logchefQLLexer),
	participle.CaseInsensitive("Ident"),
	participle.Elide("Whitespace"),
	participle.UseLookahead(2),
)

// ParseLogchefQL parses a LogchefQL query string into the Participle AST
func ParseLogchefQL(input string) (*PQuery, error) {
	return logchefQLParser.ParseString("", input)
}

// ConvertToAST converts the Participle AST to the existing AST types
func ConvertToAST(pq *PQuery) ASTNode {
	if pq == nil {
		return nil
	}

	if len(pq.Select) > 0 {
		var whereClause ASTNode
		if pq.Where != nil {
			whereClause = convertOrExpr(pq.Where)
		}

		selectFields := make([]SelectField, 0, len(pq.Select))
		for _, item := range pq.Select {
			selectFields = append(selectFields, convertSelectItem(item))
		}

		return &QueryNode{
			Where:  whereClause,
			Select: selectFields,
		}
	}

	if pq.Where != nil {
		return convertOrExpr(pq.Where)
	}

	return nil
}

func convertOrExpr(or *POrExpr) ASTNode {
	if or == nil {
		return nil
	}

	left := convertAndExpr(or.Left)
	if len(or.Right) == 0 {
		return left
	}

	children := []ASTNode{left}
	for _, tail := range or.Right {
		children = append(children, convertAndExpr(tail.Right))
	}

	return &LogicalNode{
		Operator: BoolOr,
		Children: children,
	}
}

func convertAndExpr(and *PAndExpr) ASTNode {
	if and == nil {
		return nil
	}

	left := convertTerm(and.Left)
	if len(and.Right) == 0 {
		return left
	}

	children := []ASTNode{left}
	for _, tail := range and.Right {
		children = append(children, convertTerm(tail.Right))
	}

	return &LogicalNode{
		Operator: BoolAnd,
		Children: children,
	}
}

func convertTerm(term *PTerm) ASTNode {
	if term == nil {
		return nil
	}

	if term.Group != nil {
		inner := convertOrExpr(term.Group)
		return &GroupNode{Children: []ASTNode{inner}}
	}

	return convertComparison(term.Comparison)
}

func convertComparison(cmp *PComparison) ASTNode {
	if cmp == nil {
		return nil
	}

	key := convertFieldPath(cmp.Field)
	op, _ := ParseOperator(cmp.Operator)
	value, quoted := convertValue(cmp.Value)

	return &ExpressionNode{
		Key:      key,
		Operator: op,
		Value:    value,
		Quoted:   quoted,
	}
}

func convertFieldPath(fp *PFieldPath) interface{} {
	if fp == nil || fp.First == nil {
		return ""
	}

	base := getSegmentValue(fp.First)

	if len(fp.Rest) == 0 {
		return base
	}

	path := make([]string, 0, len(fp.Rest))
	for _, seg := range fp.Rest {
		path = append(path, getSegmentValue(seg))
	}

	return NestedField{Base: base, Path: path}
}

func getSegmentValue(seg *PPathSegment) string {
	if seg == nil {
		return ""
	}
	if seg.Ident != nil {
		return *seg.Ident
	}
	if seg.Quoted != nil {
		s := *seg.Quoted
		if len(s) >= 2 {
			return unescapeString(s[1 : len(s)-1])
		}
		return s
	}
	return ""
}

func convertValue(v *PValue) (interface{}, bool) {
	if v == nil {
		return nil, false
	}

	if v.String != nil {
		s := *v.String
		if len(s) >= 2 {
			s = unescapeString(s[1 : len(s)-1])
		}
		return s, true
	}

	if v.Number != nil {
		return *v.Number, false
	}

	if v.Ident != nil {
		switch strings.ToLower(*v.Ident) {
		case "true":
			return true, false
		case "false":
			return false, false
		case "null":
			return nil, false
		default:
			return *v.Ident, false
		}
	}

	return nil, false
}

// unescapeString handles escape sequences in string literals
func unescapeString(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				result.WriteByte('\n')
			case 't':
				result.WriteByte('\t')
			case 'r':
				result.WriteByte('\r')
			case '\\':
				result.WriteByte('\\')
			case '"':
				result.WriteByte('"')
			case '\'':
				result.WriteByte('\'')
			default:
				// Unknown escape, keep as-is
				result.WriteByte(s[i+1])
			}
			i += 2
		} else {
			result.WriteByte(s[i])
			i++
		}
	}

	return result.String()
}

func convertSelectItem(item *PSelectItem) SelectField {
	if item == nil {
		return SelectField{}
	}

	return SelectField{
		Field: convertFieldPath(item.Field),
	}
}
