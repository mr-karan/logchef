package dsl

import (
	"backend-v2/pkg/models"
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// Define lexer rules
var queryLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "whitespace", Pattern: `\s+`},
	{Name: "String", Pattern: `"(?:[^"\\]|\\.)*"`},
	{Name: "Number", Pattern: `[-+]?\d*\.?\d+`},
	{Name: "Operator", Pattern: `(?:AND|OR|IN|NOT_IN|=~|!~|=|!=|is_null|is_not_null)`},
	{Name: "Ident", Pattern: `[a-zA-Z][a-zA-Z0-9_]*`},
	{Name: "Punct", Pattern: `[(),\[\]]`},
})

// AST structures
type Query struct {
	Groups []*FilterGroup `parser:"@@+"`
}

type FilterGroup struct {
	Conditions []*Condition `parser:"( '(' @@ ( 'AND' @@ )* ')' | @@ ( 'AND' @@ )* )"`
	Or         *FilterGroup `parser:"( 'OR' @@ )?"`
}

type Condition struct {
	Field    string `parser:"@Ident"`
	Operator string `parser:"@Operator"`
	Value    *Value `parser:"@@"`
}

type Value struct {
	String *string   `parser:"  @String"`
	Number *float64  `parser:"| @Number"`
	List   []*string `parser:"| '[' @String (',' @String)* ']'"`
}

var parser = participle.MustBuild[Query](
	participle.Lexer(queryLexer),
	participle.Elide("whitespace"),
)

// ParseQuery parses a LogchefQL query string into a list of filter conditions
func ParseQuery(queryStr string) ([]models.FilterCondition, error) {
	query, err := parser.ParseString("", queryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	return convertToConditions(query.Groups), nil
}

// convertToConditions converts AST groups to a flat list of filter conditions
func convertToConditions(groups []*FilterGroup) []models.FilterCondition {
	var result []models.FilterCondition

	for _, g := range groups {
		// Convert conditions
		for _, c := range g.Conditions {
			condition := models.FilterCondition{
				Field:    c.Field,
				Operator: convertOperator(c.Operator),
				Value:    convertValue(c.Value),
			}
			result = append(result, condition)
		}

		// Handle OR groups recursively
		if g.Or != nil {
			orConditions := convertToConditions([]*FilterGroup{g.Or})
			result = append(result, orConditions...)
		}
	}

	return result
}

// convertOperator maps DSL operators to models.FilterOperator
func convertOperator(op string) models.FilterOperator {
	switch op {
	case "=":
		return models.FilterOperatorEquals
	case "!=":
		return models.FilterOperatorNotEquals
	case "=~":
		return models.FilterOperatorContains
	case "!~":
		return models.FilterOperatorNotContains
	case "IN":
		return models.FilterOperatorIn
	case "NOT_IN":
		return models.FilterOperatorNotIn
	case "is_null":
		return models.FilterOperatorIsNull
	case "is_not_null":
		return models.FilterOperatorIsNotNull
	default:
		return models.FilterOperatorEquals
	}
}

// convertValue converts AST Value to string
func convertValue(v *Value) string {
	if v.String != nil {
		return strings.Trim(*v.String, "\"")
	}
	if v.Number != nil {
		return fmt.Sprintf("%v", *v.Number)
	}
	if len(v.List) > 0 {
		var values []string
		for _, s := range v.List {
			values = append(values, strings.Trim(*s, "\""))
		}
		return strings.Join(values, ",")
	}
	return ""
}
