package logchefql

import (
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
)

// Query represents the root node of a LogchefQL query
type Query struct {
	Pos     lexer.Position
	Filters []*Filter `parser:"@@ ( \";\" @@ )*"`
}

func (q *Query) ToSQL(tableName string) (string, []interface{}) {
	builder := NewSQLBuilder(q, tableName)
	query, args, err := builder.Build()
	if err != nil {
		// Handle error gracefully
		return "", nil
	}
	return query, args
}

// buildFieldPath constructs the JSON path for field access
func buildFieldPath(field *Field) string {
	var parts []string

	// Start with the base field name
	parts = append(parts, field.Name)

	// Add any subfields
	if len(field.SubFields) > 0 {
		parts = append(parts, field.SubFields...)
	}

	return strings.Join(parts, ".")
}

// Filter represents a single filter condition
type Filter struct {
	Pos      lexer.Position
	Field    *Field `json:"field" parser:"@@"`
	Operator string `json:"operator" parser:"@(\"=\" | \"!=\" | \"~\" | \"!~\" | \">\" | \"<\" | \">=\" | \"<=\")"`
	Value    Value  `json:"value" parser:"@@"`
}

// Field represents a field reference, which can be a simple field or a JSON path
type Field struct {
	Pos       lexer.Position
	Name      string   `json:"name" parser:"@Ident"`
	SubFields []string `json:"subFields" parser:"( \".\" @Ident )*"`
}

func (f *Field) IsJSONPath() bool {
	return len(f.SubFields) > 0
}

func (f *Field) FullPath() string {
	if len(f.SubFields) > 0 {
		// For JSON paths (starting with 'p'), only return the subfields
		if f.Name == "p" {
			return strings.Join(f.SubFields, ".")
		}
		return f.Name + "." + strings.Join(f.SubFields, ".")
	}
	return f.Name
}

// Value represents a filter value
type Value struct {
	String       *string `parser:"  @String"`
	Number       *string `parser:"| @Number"`
	RelativeTime *string `parser:"| @RelativeTime"`
}

// GetValue returns the string value, handling both normal strings and relative times
func (v *Value) GetValue() string {
	if v.String != nil {
		return *v.String
	}
	if v.Number != nil {
		return *v.Number
	}
	if v.RelativeTime != nil {
		return *v.RelativeTime
	}
	return ""
}
