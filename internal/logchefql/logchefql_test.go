package logchefql

import (
	"strings"
	"testing"
	"time"
)

var testSchema = &Schema{
	Columns: []ColumnInfo{
		{Name: "timestamp", Type: "DateTime64(3)"},
		{Name: "trace_id", Type: "String"},
		{Name: "span_id", Type: "String"},
		{Name: "trace_flags", Type: "UInt32"},
		{Name: "severity_text", Type: "LowCardinality(String)"},
		{Name: "severity_number", Type: "Int32"},
		{Name: "service_name", Type: "LowCardinality(String)"},
		{Name: "namespace", Type: "LowCardinality(String)"},
		{Name: "body", Type: "String"},
		{Name: "log_attributes", Type: "Map(LowCardinality(String), String)"},
	},
}

func TestParseLogchefQL(t *testing.T) {
	t.Run("parses simple expression", func(t *testing.T) {
		pq, err := ParseLogchefQL(`field = "value"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pq == nil || pq.Where == nil {
			t.Fatal("expected WHERE clause")
		}
	})

	t.Run("parses all comparison operators", func(t *testing.T) {
		ops := []string{"=", "!=", "~", "!~", ">", "<", ">=", "<="}
		for _, op := range ops {
			query := `field ` + op + ` "value"`
			_, err := ParseLogchefQL(query)
			if err != nil {
				t.Errorf("operator %q: unexpected error: %v", op, err)
			}
		}
	})

	t.Run("parses numeric values", func(t *testing.T) {
		tests := []string{
			`field = 42`,
			`field = 3.14`,
			`field = -10`,
			`field = +5`,
		}
		for _, query := range tests {
			_, err := ParseLogchefQL(query)
			if err != nil {
				t.Errorf("query %q: unexpected error: %v", query, err)
			}
		}
	})

	t.Run("parses boolean and null values", func(t *testing.T) {
		tests := []string{
			`field = true`,
			`field = false`,
			`field = null`,
		}
		for _, query := range tests {
			_, err := ParseLogchefQL(query)
			if err != nil {
				t.Errorf("query %q: unexpected error: %v", query, err)
			}
		}
	})

	t.Run("parses AND expressions", func(t *testing.T) {
		pq, err := ParseLogchefQL(`a = "1" and b = "2"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// For AND expression: POrExpr.Left is the PAndExpr which has the AND tails
		if pq.Where == nil || pq.Where.Left == nil || len(pq.Where.Left.Right) != 1 {
			t.Error("expected AND expr with one AND tail")
		}
	})

	t.Run("parses OR expressions", func(t *testing.T) {
		pq, err := ParseLogchefQL(`a = "1" or b = "2"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pq.Where == nil || len(pq.Where.Right) != 1 {
			t.Error("expected one OR tail")
		}
	})

	t.Run("parses mixed AND/OR with correct precedence", func(t *testing.T) {
		_, err := ParseLogchefQL(`a = "1" or b = "2" and c = "3"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses grouped expressions", func(t *testing.T) {
		_, err := ParseLogchefQL(`(a = "1" or b = "2") and c = "3"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses nested groups", func(t *testing.T) {
		_, err := ParseLogchefQL(`((a = "1"))`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses case-insensitive boolean operators", func(t *testing.T) {
		tests := []string{
			`a = "1" AND b = "2"`,
			`a = "1" And b = "2"`,
			`a = "1" OR b = "2"`,
			`a = "1" Or b = "2"`,
		}
		for _, query := range tests {
			_, err := ParseLogchefQL(query)
			if err != nil {
				t.Errorf("query %q: unexpected error: %v", query, err)
			}
		}
	})

	t.Run("parses nested field access", func(t *testing.T) {
		_, err := ParseLogchefQL(`log_attributes.level = "error"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses deeply nested fields", func(t *testing.T) {
		_, err := ParseLogchefQL(`a.b.c.d = "value"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses @timestamp field (ELK convention)", func(t *testing.T) {
		_, err := ParseLogchefQL(`@timestamp = "2024-01-01"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses pipe operator with single field", func(t *testing.T) {
		pq, err := ParseLogchefQL(`field = "value" | col1`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pq.Select) != 1 {
			t.Errorf("expected 1 select field, got %d", len(pq.Select))
		}
	})

	t.Run("parses pipe operator with multiple fields", func(t *testing.T) {
		pq, err := ParseLogchefQL(`field = "value" | col1 col2 col3`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pq.Select) != 3 {
			t.Errorf("expected 3 select fields, got %d", len(pq.Select))
		}
	})

	t.Run("parses pipe-only query (no WHERE)", func(t *testing.T) {
		pq, err := ParseLogchefQL(`| col1 col2`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pq.Where != nil {
			t.Error("expected no WHERE clause")
		}
		if len(pq.Select) != 2 {
			t.Errorf("expected 2 select fields, got %d", len(pq.Select))
		}
	})

	t.Run("parses single-quoted strings", func(t *testing.T) {
		_, err := ParseLogchefQL(`field = 'value'`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses escaped quotes in strings", func(t *testing.T) {
		_, err := ParseLogchefQL(`field = "value with \"quotes\""`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses field names with hyphens", func(t *testing.T) {
		_, err := ParseLogchefQL(`my-field = "value"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses field names with colons", func(t *testing.T) {
		_, err := ParseLogchefQL(`my:field = "value"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses quoted path segments", func(t *testing.T) {
		pq, err := ParseLogchefQL(`log_attributes."foo bar" = "value"`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pq.Where == nil || pq.Where.Left == nil || pq.Where.Left.Left == nil {
			t.Fatal("expected parsed expression")
		}
		cmp := pq.Where.Left.Left.Comparison
		if cmp == nil || cmp.Field == nil {
			t.Fatal("expected comparison with field")
		}
		if cmp.Field.First == nil || cmp.Field.First.Ident == nil || *cmp.Field.First.Ident != "log_attributes" {
			t.Errorf("expected first segment 'log_attributes', got %+v", cmp.Field.First)
		}
		if len(cmp.Field.Rest) != 1 || cmp.Field.Rest[0].Quoted == nil {
			t.Errorf("expected one quoted rest segment, got %+v", cmp.Field.Rest)
		}
	})

	t.Run("rejects unterminated string", func(t *testing.T) {
		_, err := ParseLogchefQL(`field = "unterminated`)
		if err == nil {
			t.Error("expected error for unterminated string")
		}
	})

	t.Run("rejects missing operator", func(t *testing.T) {
		_, err := ParseLogchefQL(`field "value"`)
		if err == nil {
			t.Error("expected error for missing operator")
		}
	})

	t.Run("rejects missing value", func(t *testing.T) {
		_, err := ParseLogchefQL(`field =`)
		if err == nil {
			t.Error("expected error for missing value")
		}
	})

	t.Run("rejects missing boolean operator between expressions", func(t *testing.T) {
		_, err := ParseLogchefQL(`a = "1" b = "2"`)
		if err == nil {
			t.Error("expected error for missing boolean operator")
		}
	})

	t.Run("rejects adjacent parentheses without boolean operator", func(t *testing.T) {
		_, err := ParseLogchefQL(`(a = "1") (b = "2")`)
		if err == nil {
			t.Error("expected error for adjacent parentheses")
		}
	})

	t.Run("rejects unclosed parenthesis", func(t *testing.T) {
		_, err := ParseLogchefQL(`(a = "1"`)
		if err == nil {
			t.Error("expected error for unclosed parenthesis")
		}
	})

	t.Run("accepts quoted field name after pipe", func(t *testing.T) {
		pq, err := ParseLogchefQL(`a = "1" | "field with space"`)
		if err != nil {
			t.Fatalf("expected quoted field name to be valid, got: %v", err)
		}
		if len(pq.Select) != 1 {
			t.Fatalf("expected 1 select item, got %d", len(pq.Select))
		}
	})

	t.Run("rejects number as field after pipe", func(t *testing.T) {
		_, err := ParseLogchefQL(`a = "1" | 123`)
		if err == nil {
			t.Error("expected error for number after pipe")
		}
	})

	t.Run("rejects bare pipe with no fields", func(t *testing.T) {
		_, err := ParseLogchefQL(`a = "1" |`)
		if err == nil {
			t.Error("expected error for bare pipe with no fields")
		}
	})
}

func TestConvertToAST(t *testing.T) {
	t.Run("converts simple expression", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`field = "value"`)
		ast := ConvertToAST(pq)

		expr, ok := ast.(*ExpressionNode)
		if !ok {
			t.Fatalf("expected ExpressionNode, got %T", ast)
		}
		if expr.Key != "field" {
			t.Errorf("expected key 'field', got %v", expr.Key)
		}
		if expr.Operator != OpEquals {
			t.Errorf("expected operator =, got %v", expr.Operator)
		}
		if expr.Value != "value" {
			t.Errorf("expected value 'value', got %v", expr.Value)
		}
		if !expr.Quoted {
			t.Error("expected Quoted=true for quoted string")
		}
	})

	t.Run("converts unquoted value correctly", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`field = error`)
		ast := ConvertToAST(pq)

		expr, ok := ast.(*ExpressionNode)
		if !ok {
			t.Fatalf("expected ExpressionNode, got %T", ast)
		}
		if expr.Quoted {
			t.Error("expected Quoted=false for unquoted value")
		}
	})

	t.Run("converts numeric value", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`count > 42`)
		ast := ConvertToAST(pq)

		expr, ok := ast.(*ExpressionNode)
		if !ok {
			t.Fatalf("expected ExpressionNode, got %T", ast)
		}
		if v, ok := expr.Value.(float64); !ok || v != 42 {
			t.Errorf("expected numeric value 42, got %v (%T)", expr.Value, expr.Value)
		}
	})

	t.Run("converts boolean values", func(t *testing.T) {
		tests := []struct {
			query string
			want  interface{}
		}{
			{`field = true`, true},
			{`field = false`, false},
		}
		for _, tc := range tests {
			pq, _ := ParseLogchefQL(tc.query)
			ast := ConvertToAST(pq)
			expr := ast.(*ExpressionNode)
			if expr.Value != tc.want {
				t.Errorf("query %q: expected %v, got %v", tc.query, tc.want, expr.Value)
			}
		}
	})

	t.Run("converts null value", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`field = null`)
		ast := ConvertToAST(pq)

		expr := ast.(*ExpressionNode)
		if expr.Value != nil {
			t.Errorf("expected nil for null, got %v", expr.Value)
		}
	})

	t.Run("converts AND expression", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`a = "1" and b = "2"`)
		ast := ConvertToAST(pq)

		logical, ok := ast.(*LogicalNode)
		if !ok {
			t.Fatalf("expected LogicalNode, got %T", ast)
		}
		if logical.Operator != BoolAnd {
			t.Errorf("expected AND operator, got %v", logical.Operator)
		}
		if len(logical.Children) != 2 {
			t.Errorf("expected 2 children, got %d", len(logical.Children))
		}
	})

	t.Run("converts OR expression", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`a = "1" or b = "2"`)
		ast := ConvertToAST(pq)

		logical, ok := ast.(*LogicalNode)
		if !ok {
			t.Fatalf("expected LogicalNode, got %T", ast)
		}
		if logical.Operator != BoolOr {
			t.Errorf("expected OR operator, got %v", logical.Operator)
		}
	})

	t.Run("converts grouped expression", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`(a = "1")`)
		ast := ConvertToAST(pq)

		group, ok := ast.(*GroupNode)
		if !ok {
			t.Fatalf("expected GroupNode, got %T", ast)
		}
		if len(group.Children) != 1 {
			t.Errorf("expected 1 child, got %d", len(group.Children))
		}
	})

	t.Run("converts nested field", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`log.level = "error"`)
		ast := ConvertToAST(pq)

		expr := ast.(*ExpressionNode)
		nf, ok := expr.Key.(NestedField)
		if !ok {
			t.Fatalf("expected NestedField key, got %T", expr.Key)
		}
		if nf.Base != "log" {
			t.Errorf("expected base 'log', got %s", nf.Base)
		}
		if len(nf.Path) != 1 || nf.Path[0] != "level" {
			t.Errorf("expected path ['level'], got %v", nf.Path)
		}
	})

	t.Run("converts pipe with SELECT fields", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`field = "value" | col1 col2`)
		ast := ConvertToAST(pq)

		query, ok := ast.(*QueryNode)
		if !ok {
			t.Fatalf("expected QueryNode, got %T", ast)
		}
		if len(query.Select) != 2 {
			t.Errorf("expected 2 select fields, got %d", len(query.Select))
		}
		if query.Select[0].Field != "col1" {
			t.Errorf("expected first field 'col1', got %v", query.Select[0].Field)
		}
	})

	t.Run("handles operator precedence (AND binds tighter than OR)", func(t *testing.T) {
		pq, _ := ParseLogchefQL(`a = "1" or b = "2" and c = "3"`)
		ast := ConvertToAST(pq)

		logical, ok := ast.(*LogicalNode)
		if !ok {
			t.Fatalf("expected LogicalNode (OR at top), got %T", ast)
		}
		if logical.Operator != BoolOr {
			t.Errorf("expected OR at top level, got %v", logical.Operator)
		}
		if len(logical.Children) != 2 {
			t.Fatalf("expected 2 children, got %d", len(logical.Children))
		}
		andNode, ok := logical.Children[1].(*LogicalNode)
		if !ok || andNode.Operator != BoolAnd {
			t.Error("expected second child to be AND node")
		}
	})
}

func TestAllOperators(t *testing.T) {
	tests := []struct {
		query    string
		op       Operator
		contains string
	}{
		{`field = "value"`, OpEquals, "= 'value'"},
		{`field != "value"`, OpNotEquals, "!= 'value'"},
		{`field ~ "pattern"`, OpRegex, "positionCaseInsensitive"},
		{`field !~ "pattern"`, OpNotRegex, "positionCaseInsensitive"},
		{`field > 10`, OpGT, "> 10"},
		{`field < 10`, OpLT, "< 10"},
		{`field >= 10`, OpGTE, ">= 10"},
		{`field <= 10`, OpLTE, "<= 10"},
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			result := Translate(tc.query, nil)
			if !result.Valid {
				t.Fatalf("expected valid result, got error: %v", result.Error)
			}
			if !strings.Contains(result.SQL, tc.contains) {
				t.Errorf("expected SQL to contain %q, got %q", tc.contains, result.SQL)
			}
		})
	}
}

func TestBooleanOperatorWordBoundaries(t *testing.T) {
	t.Run("does not treat 'order' as boolean operator", func(t *testing.T) {
		result := Translate(`body ~ "order"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
	})

	t.Run("does not treat 'android' as boolean operator", func(t *testing.T) {
		result := Translate(`field = "android"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
	})

	t.Run("does not treat 'sandbox' as boolean operator", func(t *testing.T) {
		result := Translate(`env = "sandbox"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
	})

	t.Run("does not treat 'coral' as boolean operator", func(t *testing.T) {
		result := Translate(`color = "coral"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
	})
}

func TestStringEscaping(t *testing.T) {
	t.Run("escapes single quotes in values", func(t *testing.T) {
		result := Translate(`field = "it's"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		if !strings.Contains(result.SQL, "it''s") {
			t.Errorf("expected escaped single quote, got %q", result.SQL)
		}
	})

	t.Run("handles backslash escapes", func(t *testing.T) {
		result := Translate(`field = "path\\to\\file"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
	})

	t.Run("handles newline in string", func(t *testing.T) {
		result := Translate(`field = "line1\nline2"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
	})
}

func TestComplexQueries(t *testing.T) {
	t.Run("complex nested AND/OR", func(t *testing.T) {
		query := `(severity = "error" or severity = "fatal") and (service = "api" or service = "web")`
		result := Translate(query, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		if !strings.Contains(result.SQL, "AND") && !strings.Contains(result.SQL, "OR") {
			t.Errorf("expected AND/OR in SQL, got %q", result.SQL)
		}
	})

	t.Run("multiple nested fields", func(t *testing.T) {
		query := `log.level = "error" and request.path ~ "/api"`
		result := Translate(query, testSchema)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
	})

	t.Run("query with pipe and complex WHERE", func(t *testing.T) {
		query := `(severity = "error" or severity = "warn") and namespace = "prod" | timestamp service body`
		result := Translate(query, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		if result.SelectClause == "" {
			t.Error("expected SelectClause to be set")
		}
	})
}

func TestSQLGenerator(t *testing.T) {
	t.Run("simple equals expression", func(t *testing.T) {
		result := Translate(`severity_text = "error"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		expected := "`severity_text` = 'error'"
		if result.SQL != expected {
			t.Errorf("expected SQL %q, got %q", expected, result.SQL)
		}
	})

	t.Run("not equals expression", func(t *testing.T) {
		result := Translate(`severity_text != "error"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		expected := "`severity_text` != 'error'"
		if result.SQL != expected {
			t.Errorf("expected SQL %q, got %q", expected, result.SQL)
		}
	})

	t.Run("regex expression", func(t *testing.T) {
		result := Translate(`body ~ "error"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.SQL, "positionCaseInsensitive") {
			t.Errorf("expected positionCaseInsensitive in SQL, got %q", result.SQL)
		}
	})

	t.Run("not regex expression", func(t *testing.T) {
		result := Translate(`body !~ "error"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.SQL, "positionCaseInsensitive") || !strings.Contains(result.SQL, "= 0") {
			t.Errorf("expected positionCaseInsensitive = 0 in SQL, got %q", result.SQL)
		}
	})

	t.Run("AND expression", func(t *testing.T) {
		result := Translate(`severity_text = "error" and service_name = "api"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.SQL, "AND") {
			t.Errorf("expected AND in SQL, got %q", result.SQL)
		}
	})

	t.Run("OR expression", func(t *testing.T) {
		result := Translate(`severity_text = "error" or severity_text = "warn"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.SQL, "OR") {
			t.Errorf("expected OR in SQL, got %q", result.SQL)
		}
	})

	t.Run("nested field with Map type", func(t *testing.T) {
		result := Translate(`log_attributes.level = "error"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		// Map type should use subscript notation
		if !strings.Contains(result.SQL, "['level']") {
			t.Errorf("expected ['level'] in SQL, got %q", result.SQL)
		}
	})

	t.Run("comparison operators", func(t *testing.T) {
		tests := []struct {
			query    string
			contains string
		}{
			{`severity_number > 3`, "> 3"},
			{`severity_number < 5`, "< 5"},
			{`severity_number >= 3`, ">= 3"},
			{`severity_number <= 5`, "<= 5"},
		}

		for _, tc := range tests {
			result := Translate(tc.query, testSchema)
			if !result.Valid {
				t.Errorf("query %q: expected valid result, got error: %v", tc.query, result.Error)
				continue
			}
			if !strings.Contains(result.SQL, tc.contains) {
				t.Errorf("query %q: expected %q in SQL, got %q", tc.query, tc.contains, result.SQL)
			}
		}
	})

	t.Run("empty query", func(t *testing.T) {
		result := Translate("", testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result for empty query")
		}
		if result.SQL != "" {
			t.Errorf("expected empty SQL, got %q", result.SQL)
		}
	})

	t.Run("whitespace only query", func(t *testing.T) {
		result := Translate("   ", testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result for whitespace query")
		}
		if result.SQL != "" {
			t.Errorf("expected empty SQL, got %q", result.SQL)
		}
	})
}

func TestTranslate(t *testing.T) {
	t.Run("extracts fields used", func(t *testing.T) {
		result := Translate(`severity_text = "error" and service_name = "api"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if len(result.FieldsUsed) != 2 {
			t.Errorf("expected 2 fields used, got %d", len(result.FieldsUsed))
		}

		fieldsMap := make(map[string]bool)
		for _, f := range result.FieldsUsed {
			fieldsMap[f] = true
		}

		if !fieldsMap["severity_text"] {
			t.Error("expected severity_text in fields used")
		}
		if !fieldsMap["service_name"] {
			t.Error("expected service_name in fields used")
		}
	})

	t.Run("extracts conditions", func(t *testing.T) {
		result := Translate(`severity_text = "error" and service_name ~ "api"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if len(result.Conditions) != 2 {
			t.Errorf("expected 2 conditions, got %d", len(result.Conditions))
		}

		// Check first condition
		if result.Conditions[0].Field != "severity_text" {
			t.Errorf("expected field 'severity_text', got %s", result.Conditions[0].Field)
		}
		if result.Conditions[0].Operator != "=" {
			t.Errorf("expected operator '=', got %s", result.Conditions[0].Operator)
		}
		if result.Conditions[0].IsRegex {
			t.Error("expected IsRegex false for '='")
		}

		// Check second condition
		if result.Conditions[1].Field != "service_name" {
			t.Errorf("expected field 'service_name', got %s", result.Conditions[1].Field)
		}
		if result.Conditions[1].Operator != "~" {
			t.Errorf("expected operator '~', got %s", result.Conditions[1].Operator)
		}
		if !result.Conditions[1].IsRegex {
			t.Error("expected IsRegex true for '~'")
		}
	})
}

func TestValidate(t *testing.T) {
	t.Run("valid query", func(t *testing.T) {
		result := Validate(`severity_text = "error"`)

		if !result.Valid {
			t.Errorf("expected valid result, got error: %v", result.Error)
		}
	})

	t.Run("invalid query - missing value", func(t *testing.T) {
		result := Validate(`severity_text =`)

		if result.Valid {
			t.Error("expected invalid result")
		}
		if result.Error == nil {
			t.Error("expected error")
		}
	})

	t.Run("invalid query - unterminated string", func(t *testing.T) {
		result := Validate(`severity_text = "error`)

		if result.Valid {
			t.Error("expected invalid result")
		}
		if result.Error == nil {
			t.Error("expected error")
		}
	})

	t.Run("invalid query - missing boolean operator", func(t *testing.T) {
		result := Validate(`severity_text = "error" service_name = "api"`)

		if result.Valid {
			t.Error("expected invalid result")
		}
		if result.Error == nil {
			t.Error("expected error")
		}
	})
}

func TestBuildFullQuery(t *testing.T) {
	t.Run("builds complete query", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `severity_text = "error"`,
			Schema:         testSchema,
			TableName:      "logs.otel_logs",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !strings.Contains(sql, "SELECT *") {
			t.Error("expected SELECT * in query")
		}
		if !strings.Contains(sql, "FROM logs.otel_logs") {
			t.Error("expected FROM clause in query")
		}
		if !strings.Contains(sql, "WHERE `timestamp` BETWEEN") {
			t.Error("expected WHERE clause with time range in query")
		}
		if !strings.Contains(sql, "severity_text") {
			t.Error("expected condition in query")
		}
		if !strings.Contains(sql, "ORDER BY `timestamp` DESC") {
			t.Error("expected ORDER BY clause in query")
		}
		if !strings.Contains(sql, "LIMIT 100") {
			t.Error("expected LIMIT clause in query")
		}
	})

	t.Run("empty logchefql query", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      "",
			Schema:         testSchema,
			TableName:      "logs.otel_logs",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Should not have AND clause for empty query
		if strings.Contains(sql, "AND (") {
			t.Error("expected no AND clause for empty query")
		}
	})

	t.Run("pipe operator includes custom SELECT clause", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `namespace="kite-alerts" | msg`,
			Schema:         testSchema,
			TableName:      "logs.nomad_apps",
			TimestampField: "_timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "Asia/Calcutta",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Should NOT have SELECT *
		if strings.Contains(sql, "SELECT *") {
			t.Errorf("expected custom SELECT clause, not SELECT *, got:\n%s", sql)
		}

		// Should have timestamp field first
		if !strings.Contains(sql, "SELECT `_timestamp`") {
			t.Errorf("expected timestamp field in SELECT, got:\n%s", sql)
		}

		// Should have msg field
		if !strings.Contains(sql, "`msg`") {
			t.Errorf("expected msg field in SELECT, got:\n%s", sql)
		}

		// Should still have WHERE clause
		if !strings.Contains(sql, "namespace") {
			t.Errorf("expected namespace condition in WHERE, got:\n%s", sql)
		}
	})

	t.Run("pipe operator with multiple fields", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `severity_text="error" | service_name body`,
			Schema:         testSchema,
			TableName:      "logs.otel_logs",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          50,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Should have timestamp field, service_name, and body in SELECT
		if !strings.Contains(sql, "`timestamp`") {
			t.Errorf("expected timestamp in SELECT, got:\n%s", sql)
		}
		if !strings.Contains(sql, "`service_name`") {
			t.Errorf("expected service_name in SELECT, got:\n%s", sql)
		}
		if !strings.Contains(sql, "`body`") {
			t.Errorf("expected body in SELECT, got:\n%s", sql)
		}
	})
}

func TestPipeOperator(t *testing.T) {
	t.Run("pipe operator with single field", func(t *testing.T) {
		result := Translate(`namespace="syslog" | service_name`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		// Should have WHERE clause
		if !strings.Contains(result.SQL, "namespace") {
			t.Errorf("expected namespace in SQL, got %q", result.SQL)
		}

		// Should have SELECT clause
		if result.SelectClause == "" {
			t.Error("expected SelectClause to be set")
		}
		if !strings.Contains(result.SelectClause, "service_name") {
			t.Errorf("expected service_name in SelectClause, got %q", result.SelectClause)
		}
	})

	t.Run("pipe operator with multiple fields", func(t *testing.T) {
		result := Translate(`namespace="prod" | namespace service_name body`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if result.SelectClause == "" {
			t.Error("expected SelectClause to be set")
		}

		// All fields should be in SELECT clause
		for _, field := range []string{"namespace", "service_name", "body"} {
			if !strings.Contains(result.SelectClause, field) {
				t.Errorf("expected %s in SelectClause, got %q", field, result.SelectClause)
			}
		}
	})

	t.Run("pipe operator with nested field", func(t *testing.T) {
		result := Translate(`namespace="prod" | log_attributes.level`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if result.SelectClause == "" {
			t.Error("expected SelectClause to be set")
		}
	})

	t.Run("query without pipe operator has no select clause", func(t *testing.T) {
		result := Translate(`namespace="syslog"`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if result.SelectClause != "" {
			t.Errorf("expected empty SelectClause for query without pipe, got %q", result.SelectClause)
		}
	})

	t.Run("accepts quoted field name after pipe", func(t *testing.T) {
		result := Translate(`namespace="prod" | "field_with_quotes"`, testSchema)

		if !result.Valid {
			t.Errorf("expected valid result for quoted field after pipe, got error: %v", result.Error)
		}
		if result.SelectClause == "" {
			t.Error("expected non-empty SelectClause")
		}
	})

	t.Run("rejects number as field after pipe", func(t *testing.T) {
		result := Translate(`namespace="prod" | 123`, testSchema)

		if result.Valid {
			t.Error("expected invalid result for number after pipe")
		}
		if result.Error == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("accepts valid field names after pipe", func(t *testing.T) {
		result := Translate(`namespace="prod" | service_name body`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if result.SelectClause == "" {
			t.Error("expected SelectClause to be set")
		}
	})
}

func TestTrailingTokensDetection(t *testing.T) {
	t.Run("detects trailing tokens after valid expression", func(t *testing.T) {
		result := Validate(`a=b (c=d)`)

		if result.Valid {
			t.Error("expected invalid result for trailing tokens")
		}
		if result.Error == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("detects adjacent parentheses without boolean operator", func(t *testing.T) {
		result := Validate(`(a=b) (c=d)`)

		if result.Valid {
			t.Error("expected invalid result for adjacent parentheses")
		}
		if result.Error == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("valid query with proper boolean operators", func(t *testing.T) {
		result := Validate(`(a="b") and (c="d")`)

		if !result.Valid {
			t.Errorf("expected valid result, got error: %v", result.Error)
		}
	})
}

func TestTimeValidation(t *testing.T) {
	t.Run("rejects invalid time format", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "invalid-time",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		_, err := BuildFullQuery(params)
		if err == nil {
			t.Error("expected error for invalid time format")
		}
		if !strings.Contains(err.Error(), "invalid time format") {
			t.Errorf("expected 'invalid time format' error, got: %v", err)
		}
	})

	t.Run("rejects timezone with dangerous characters", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC'); DROP TABLE logs; --",
			Limit:          100,
		}

		_, err := BuildFullQuery(params)
		if err == nil {
			t.Error("expected error for dangerous timezone")
		}
		if !strings.Contains(err.Error(), "invalid timezone") {
			t.Errorf("expected 'invalid timezone' error, got: %v", err)
		}
	})

	t.Run("accepts valid timezone", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			Schema:         testSchema,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "Asia/Kolkata",
			Limit:          100,
		}

		_, err := BuildFullQuery(params)
		if err != nil {
			t.Errorf("expected no error for valid timezone, got: %v", err)
		}
	})

	t.Run("accepts timezone with colon offset", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			Schema:         testSchema,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC+05:30",
			Limit:          100,
		}

		_, err := BuildFullQuery(params)
		if err != nil {
			t.Errorf("expected no error for timezone with colon offset, got: %v", err)
		}
	})

	t.Run("rejects invalid table name", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			TableName:      "logs'; DROP TABLE users; --",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		_, err := BuildFullQuery(params)
		if err == nil {
			t.Error("expected error for invalid table name")
		}
		if !strings.Contains(err.Error(), "invalid table name") {
			t.Errorf("expected 'invalid table name' error, got: %v", err)
		}
	})

	t.Run("rejects invalid timestamp field", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			TableName:      "logs.test",
			TimestampField: "timestamp; DROP TABLE",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		_, err := BuildFullQuery(params)
		if err == nil {
			t.Error("expected error for invalid timestamp field")
		}
		if !strings.Contains(err.Error(), "invalid timestamp field") {
			t.Errorf("expected 'invalid timestamp field' error, got: %v", err)
		}
	})

	t.Run("accepts @timestamp field (ELK convention)", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			Schema:         testSchema,
			TableName:      "logs.test",
			TimestampField: "@timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error for @timestamp, got: %v", err)
		}

		if !strings.Contains(sql, "`@timestamp`") {
			t.Errorf("expected backtick-quoted @timestamp in SQL:\n%s", sql)
		}
	})

	t.Run("quotes timestamp field in WHERE and ORDER BY", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			Schema:         testSchema,
			TableName:      "logs.test",
			TimestampField: "@timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !strings.Contains(sql, "WHERE `@timestamp` BETWEEN") {
			t.Errorf("expected quoted timestamp in WHERE clause:\n%s", sql)
		}
		if !strings.Contains(sql, "ORDER BY `@timestamp` DESC") {
			t.Errorf("expected quoted timestamp in ORDER BY clause:\n%s", sql)
		}
	})

	t.Run("rejects semantically invalid time (impossible date)", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-99-99 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		_, err := BuildFullQuery(params)
		if err == nil {
			t.Error("expected error for impossible date 2024-99-99")
		}
	})

	t.Run("rejects semantically invalid time (impossible hour)", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value"`,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 25:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		_, err := BuildFullQuery(params)
		if err == nil {
			t.Error("expected error for impossible hour 25:00:00")
		}
	})
}

func TestFieldsUsedExtraction(t *testing.T) {
	t.Run("does not include unquoted values as fields", func(t *testing.T) {
		result := Translate(`severity_text=error`, testSchema)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		for _, field := range result.FieldsUsed {
			if field == "error" {
				t.Error("'error' should not be in FieldsUsed - it's a value, not a field")
			}
		}

		found := false
		for _, field := range result.FieldsUsed {
			if field == "severity_text" {
				found = true
				break
			}
		}
		if !found {
			t.Error("severity_text should be in FieldsUsed")
		}
	})
}

func TestDuplicateTimestampAvoidance(t *testing.T) {
	t.Run("does not duplicate timestamp when explicitly selected", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value" | timestamp body`,
			Schema:         testSchema,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		fromIdx := strings.Index(sql, "\nFROM")
		if fromIdx == -1 {
			t.Fatalf("expected FROM clause in SQL:\n%s", sql)
		}
		selectClause := sql[:fromIdx]
		timestampCount := strings.Count(selectClause, "`timestamp`")
		if timestampCount != 1 {
			t.Errorf("timestamp appears %d times in SELECT clause, expected 1:\n%s", timestampCount, selectClause)
		}
	})

	t.Run("prepends timestamp when not explicitly selected", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `field="value" | body service_name`,
			Schema:         testSchema,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !strings.Contains(sql, "SELECT `timestamp`") {
			t.Errorf("expected timestamp to be prepended:\n%s", sql)
		}
	})
}

func TestMapColumnFallback(t *testing.T) {
	schemaWithMap := &Schema{
		Columns: []ColumnInfo{
			{Name: "timestamp", Type: "DateTime"},
			{Name: "log_attributes", Type: "Map(LowCardinality(String), String)"},
			{Name: "body", Type: "String"},
		},
	}

	t.Run("uses map column for unknown field in SELECT", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `body="test" | msg`,
			Schema:         schemaWithMap,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !strings.Contains(sql, "`log_attributes`['msg']") {
			t.Errorf("expected msg to use map column fallback, got:\n%s", sql)
		}
		if !strings.Contains(sql, "AS `msg`") {
			t.Errorf("expected alias 'msg' for map field, got:\n%s", sql)
		}
	})

	t.Run("uses direct column for known field in SELECT", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `body="test" | body`,
			Schema:         schemaWithMap,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !strings.Contains(sql, "SELECT `timestamp`, `body`") {
			t.Errorf("expected body to be used directly as column, got:\n%s", sql)
		}
	})

	t.Run("handles multiple unknown fields with map fallback", func(t *testing.T) {
		params := QueryBuildParams{
			LogchefQL:      `body="test" | msg namespace level`,
			Schema:         schemaWithMap,
			TableName:      "logs.test",
			TimestampField: "timestamp",
			StartTime:      "2024-01-01 00:00:00",
			EndTime:        "2024-01-01 23:59:59",
			Timezone:       "UTC",
			Limit:          100,
		}

		sql, err := BuildFullQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !strings.Contains(sql, "`log_attributes`['msg']") {
			t.Errorf("expected msg to use map column fallback, got:\n%s", sql)
		}
		if !strings.Contains(sql, "`log_attributes`['namespace']") {
			t.Errorf("expected namespace to use map column fallback, got:\n%s", sql)
		}
		if !strings.Contains(sql, "`log_attributes`['level']") {
			t.Errorf("expected level to use map column fallback, got:\n%s", sql)
		}
	})
}

// ============================================================================
// LogsQL Generator Tests (VictoriaLogs support)
// ============================================================================

func TestTranslateToLogsQL(t *testing.T) {
	t.Run("simple equals expression", func(t *testing.T) {
		result := TranslateToLogsQL(`severity_text = "error"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		expected := "severity_text:=error"
		if result.LogsQL != expected {
			t.Errorf("expected LogsQL %q, got %q", expected, result.LogsQL)
		}
	})

	t.Run("not equals expression", func(t *testing.T) {
		result := TranslateToLogsQL(`level != "debug"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		expected := "level:!=debug"
		if result.LogsQL != expected {
			t.Errorf("expected LogsQL %q, got %q", expected, result.LogsQL)
		}
	})

	t.Run("regex expression", func(t *testing.T) {
		result := TranslateToLogsQL(`body ~ "error.*timeout"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.LogsQL, "body:~") {
			t.Errorf("expected regex operator in LogsQL, got %q", result.LogsQL)
		}
	})

	t.Run("not regex expression", func(t *testing.T) {
		result := TranslateToLogsQL(`body !~ "debug"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.LogsQL, "body:!~") {
			t.Errorf("expected not-regex operator in LogsQL, got %q", result.LogsQL)
		}
	})

	t.Run("comparison operators", func(t *testing.T) {
		tests := []struct {
			query    string
			contains string
		}{
			{`count > 10`, "count:>10"},
			{`count < 100`, "count:<100"},
			{`count >= 5`, "count:>=5"},
			{`count <= 50`, "count:<=50"},
		}

		for _, tc := range tests {
			result := TranslateToLogsQL(tc.query, nil)
			if !result.Valid {
				t.Errorf("query %q: expected valid result, got error: %v", tc.query, result.Error)
				continue
			}
			if result.LogsQL != tc.contains {
				t.Errorf("query %q: expected %q, got %q", tc.query, tc.contains, result.LogsQL)
			}
		}
	})

	t.Run("AND expression uses space", func(t *testing.T) {
		result := TranslateToLogsQL(`level = "error" and service = "api"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.LogsQL, "level:=error") {
			t.Errorf("expected level condition in LogsQL, got %q", result.LogsQL)
		}
		if !strings.Contains(result.LogsQL, "service:=api") {
			t.Errorf("expected service condition in LogsQL, got %q", result.LogsQL)
		}
		if strings.Contains(result.LogsQL, " and ") {
			t.Errorf("AND should use space, not 'and' keyword, got %q", result.LogsQL)
		}
	})

	t.Run("OR expression uses or keyword with parentheses", func(t *testing.T) {
		result := TranslateToLogsQL(`level = "error" or level = "warn"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.LogsQL, " or ") {
			t.Errorf("expected 'or' keyword in LogsQL, got %q", result.LogsQL)
		}
		if !strings.HasPrefix(result.LogsQL, "(") || !strings.HasSuffix(result.LogsQL, ")") {
			t.Errorf("OR expression should be wrapped in parentheses, got %q", result.LogsQL)
		}
	})

	t.Run("nested field access", func(t *testing.T) {
		result := TranslateToLogsQL(`log.level = "error"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.LogsQL, "log.level:=") {
			t.Errorf("expected nested field in LogsQL, got %q", result.LogsQL)
		}
	})

	t.Run("value with special characters is quoted", func(t *testing.T) {
		result := TranslateToLogsQL(`message = "hello world"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.LogsQL, "\"hello world\"") {
			t.Errorf("expected quoted value with spaces in LogsQL, got %q", result.LogsQL)
		}
	})

	t.Run("empty query returns empty LogsQL", func(t *testing.T) {
		result := TranslateToLogsQL("", nil)

		if !result.Valid {
			t.Fatalf("expected valid result for empty query")
		}
		if result.LogsQL != "" {
			t.Errorf("expected empty LogsQL, got %q", result.LogsQL)
		}
	})

	t.Run("whitespace only query returns empty LogsQL", func(t *testing.T) {
		result := TranslateToLogsQL("   ", nil)

		if !result.Valid {
			t.Fatalf("expected valid result for whitespace query")
		}
		if result.LogsQL != "" {
			t.Errorf("expected empty LogsQL, got %q", result.LogsQL)
		}
	})

	t.Run("complex query with mixed operators", func(t *testing.T) {
		result := TranslateToLogsQL(`(level = "error" or level = "warn") and service = "api"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if !strings.Contains(result.LogsQL, "level:=error") {
			t.Errorf("expected level=error in LogsQL, got %q", result.LogsQL)
		}
		if !strings.Contains(result.LogsQL, "level:=warn") {
			t.Errorf("expected level=warn in LogsQL, got %q", result.LogsQL)
		}
		if !strings.Contains(result.LogsQL, "service:=api") {
			t.Errorf("expected service=api in LogsQL, got %q", result.LogsQL)
		}
		if !strings.Contains(result.LogsQL, " or ") {
			t.Errorf("expected 'or' in LogsQL, got %q", result.LogsQL)
		}
	})

	t.Run("extracts fields used", func(t *testing.T) {
		result := TranslateToLogsQL(`level = "error" and service = "api"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if len(result.FieldsUsed) != 2 {
			t.Errorf("expected 2 fields used, got %d", len(result.FieldsUsed))
		}

		fieldsMap := make(map[string]bool)
		for _, f := range result.FieldsUsed {
			fieldsMap[f] = true
		}

		if !fieldsMap["level"] {
			t.Error("expected level in fields used")
		}
		if !fieldsMap["service"] {
			t.Error("expected service in fields used")
		}
	})

	t.Run("extracts conditions", func(t *testing.T) {
		result := TranslateToLogsQL(`level = "error" and body ~ "timeout"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if len(result.Conditions) != 2 {
			t.Errorf("expected 2 conditions, got %d", len(result.Conditions))
		}

		if result.Conditions[0].Field != "level" {
			t.Errorf("expected field 'level', got %s", result.Conditions[0].Field)
		}
		if result.Conditions[1].IsRegex != true {
			t.Error("expected second condition to be regex")
		}
	})

	t.Run("invalid query returns error", func(t *testing.T) {
		result := TranslateToLogsQL(`level = `, nil)

		if result.Valid {
			t.Error("expected invalid result for incomplete query")
		}
		if result.Error == nil {
			t.Error("expected error for invalid query")
		}
	})
}

func TestLogsQLAllOperators(t *testing.T) {
	tests := []struct {
		query    string
		contains string
	}{
		{`field = "value"`, "field:=value"},
		{`field != "value"`, "field:!=value"},
		{`field ~ "pattern"`, "field:~"},
		{`field !~ "pattern"`, "field:!~"},
		{`field > 10`, "field:>10"},
		{`field < 10`, "field:<10"},
		{`field >= 10`, "field:>=10"},
		{`field <= 10`, "field:<=10"},
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			result := TranslateToLogsQL(tc.query, nil)
			if !result.Valid {
				t.Fatalf("expected valid result, got error: %v", result.Error)
			}
			if !strings.Contains(result.LogsQL, tc.contains) {
				t.Errorf("expected LogsQL to contain %q, got %q", tc.contains, result.LogsQL)
			}
		})
	}
}

func TestBuildFullLogsQLQuery(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2025, 1, 1, 23, 59, 59, 0, time.UTC)

	t.Run("builds complete query with filter", func(t *testing.T) {
		params := LogsQLQueryBuildParams{
			LogchefQL: `level = "error"`,
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     100,
		}

		query, err := BuildFullLogsQLQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !strings.Contains(query, "level:=error") {
			t.Error("expected filter condition in query")
		}
		if !strings.Contains(query, "_time:[") {
			t.Error("expected time range in query")
		}
		if !strings.Contains(query, "2025-01-01T00:00:00Z") {
			t.Error("expected start time in query")
		}
		if !strings.Contains(query, "2025-01-01T23:59:59Z") {
			t.Error("expected end time in query")
		}
		if !strings.Contains(query, "| sort by (_time desc)") {
			t.Error("expected sort clause in query")
		}
		if !strings.Contains(query, "| limit 100") {
			t.Error("expected limit clause in query")
		}
	})

	t.Run("empty LogchefQL builds query with only time range", func(t *testing.T) {
		params := LogsQLQueryBuildParams{
			LogchefQL: "",
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     50,
		}

		query, err := BuildFullLogsQLQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !strings.HasPrefix(query, "_time:[") {
			t.Errorf("expected query to start with time range, got %q", query)
		}
		if !strings.Contains(query, "| limit 50") {
			t.Error("expected limit clause in query")
		}
	})

	t.Run("no limit when limit is zero", func(t *testing.T) {
		params := LogsQLQueryBuildParams{
			LogchefQL: `service = "api"`,
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     0,
		}

		query, err := BuildFullLogsQLQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if strings.Contains(query, "| limit") {
			t.Error("expected no limit clause when limit is 0")
		}
	})

	t.Run("complex filter query", func(t *testing.T) {
		params := LogsQLQueryBuildParams{
			LogchefQL: `(level = "error" or level = "warn") and service = "api"`,
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     100,
		}

		query, err := BuildFullLogsQLQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !strings.Contains(query, "level:=error") {
			t.Error("expected level=error in query")
		}
		if !strings.Contains(query, "service:=api") {
			t.Error("expected service=api in query")
		}
		if !strings.Contains(query, "_time:[") {
			t.Error("expected time range in query")
		}
	})

	t.Run("invalid LogchefQL returns error", func(t *testing.T) {
		params := LogsQLQueryBuildParams{
			LogchefQL: `level = `,
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     100,
		}

		_, err := BuildFullLogsQLQuery(params)
		if err == nil {
			t.Error("expected error for invalid LogchefQL")
		}
	})

	t.Run("query with regex operator", func(t *testing.T) {
		params := LogsQLQueryBuildParams{
			LogchefQL: `body ~ "error.*timeout"`,
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     100,
		}

		query, err := BuildFullLogsQLQuery(params)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !strings.Contains(query, "body:~") {
			t.Error("expected regex operator in query")
		}
	})
}

func TestLogsQLSelectClause(t *testing.T) {
	t.Run("pipe operator generates select clause", func(t *testing.T) {
		result := TranslateToLogsQL(`level = "error" | timestamp service body`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if result.SelectClause == "" {
			t.Error("expected SelectClause to be set")
		}

		for _, field := range []string{"timestamp", "service", "body"} {
			if !strings.Contains(result.SelectClause, field) {
				t.Errorf("expected %s in SelectClause, got %q", field, result.SelectClause)
			}
		}
	})

	t.Run("query without pipe has no select clause", func(t *testing.T) {
		result := TranslateToLogsQL(`level = "error"`, nil)

		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}

		if result.SelectClause != "" {
			t.Errorf("expected empty SelectClause, got %q", result.SelectClause)
		}
	})
}
