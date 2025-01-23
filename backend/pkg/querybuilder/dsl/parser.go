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
	{Name: "Operator", Pattern: `(?:AND|OR|IN|=~|=|!=|>=|<=|>|<)`},
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

// ParseQuery parses a query string into filter groups
func ParseQuery(queryStr string) ([]models.FilterGroup, error) {
	query, err := parser.ParseString("", queryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	return convertToFilterGroups(query.Groups), nil
}

// convertToFilterGroups converts AST groups to models.FilterGroup
func convertToFilterGroups(groups []*FilterGroup) []models.FilterGroup {
	var result []models.FilterGroup

	for _, g := range groups {
		group := models.FilterGroup{
			Operator:   models.GroupOperatorAnd,
			Conditions: make([]models.FilterCondition, 0),
		}

		// Convert conditions
		for _, c := range g.Conditions {
			condition := models.FilterCondition{
				Field:    c.Field,
				Operator: convertOperator(c.Operator),
				Value:    convertValue(c.Value),
			}
			group.Conditions = append(group.Conditions, condition)
		}

		result = append(result, group)

		// Handle OR groups recursively
		if g.Or != nil {
			orGroups := convertToFilterGroups([]*FilterGroup{g.Or})
			result = append(result, orGroups...)
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
	case ">":
		return models.FilterOperatorGreaterThan
	case "<":
		return models.FilterOperatorLessThan
	case ">=":
		return models.FilterOperatorGreaterEquals
	case "<=":
		return models.FilterOperatorLessEquals
	case "=~":
		return models.FilterOperatorContains
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
