package logchefql

import (
	"strings"
	"testing"
)

// Test schema based on the TypeScript test schema
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

func TestTokenizer(t *testing.T) {
	t.Run("basic tokenization", func(t *testing.T) {
		result := Tokenize(`severity_text = "error"`)

		if len(result.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", result.Errors)
		}

		if len(result.Tokens) != 3 {
			t.Fatalf("expected 3 tokens, got %d", len(result.Tokens))
		}

		if result.Tokens[0].Type != TokenKey || result.Tokens[0].Value != "severity_text" {
			t.Errorf("expected key token 'severity_text', got %v", result.Tokens[0])
		}

		if result.Tokens[1].Type != TokenOperator || result.Tokens[1].Value != "=" {
			t.Errorf("expected operator token '=', got %v", result.Tokens[1])
		}

		if result.Tokens[2].Type != TokenValue || result.Tokens[2].Value != "error" {
			t.Errorf("expected value token 'error', got %v", result.Tokens[2])
		}
	})

	t.Run("boolean operator tokenization - and", func(t *testing.T) {
		result := Tokenize(`severity_text = "error" and service_name = "api"`)

		if len(result.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", result.Errors)
		}

		boolTokens := filterTokens(result.Tokens, TokenBool)
		if len(boolTokens) != 1 {
			t.Fatalf("expected 1 bool token, got %d", len(boolTokens))
		}
		if boolTokens[0].Value != "and" {
			t.Errorf("expected 'and', got %s", boolTokens[0].Value)
		}
	})

	t.Run("boolean operator tokenization - or", func(t *testing.T) {
		result := Tokenize(`severity_text = "error" or severity_text = "warn"`)

		if len(result.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", result.Errors)
		}

		boolTokens := filterTokens(result.Tokens, TokenBool)
		if len(boolTokens) != 1 {
			t.Fatalf("expected 1 bool token, got %d", len(boolTokens))
		}
		if boolTokens[0].Value != "or" {
			t.Errorf("expected 'or', got %s", boolTokens[0].Value)
		}
	})

	t.Run("should NOT tokenize 'order' as boolean operator", func(t *testing.T) {
		result := Tokenize(`body ~ "order"`)

		boolTokens := filterTokens(result.Tokens, TokenBool)
		if len(boolTokens) != 0 {
			t.Errorf("expected no bool tokens, got %d", len(boolTokens))
		}
	})

	t.Run("should NOT tokenize 'android' as boolean operator", func(t *testing.T) {
		result := Tokenize(`service_name = "android"`)

		boolTokens := filterTokens(result.Tokens, TokenBool)
		if len(boolTokens) != 0 {
			t.Errorf("expected no bool tokens, got %d", len(boolTokens))
		}
	})

	t.Run("case-insensitive boolean operators", func(t *testing.T) {
		result := Tokenize(`severity_text = "ERROR" AND service_name = "API"`)

		boolTokens := filterTokens(result.Tokens, TokenBool)
		if len(boolTokens) != 1 {
			t.Fatalf("expected 1 bool token, got %d", len(boolTokens))
		}
		if boolTokens[0].Value != "and" {
			t.Errorf("expected 'and', got %s", boolTokens[0].Value)
		}
	})

	t.Run("unterminated string literal detection", func(t *testing.T) {
		result := Tokenize(`severity_text = "error`)

		if len(result.Errors) == 0 {
			t.Fatal("expected error for unterminated string")
		}

		if result.Errors[0].Code != ErrUnterminatedString {
			t.Errorf("expected UNTERMINATED_STRING error, got %s", result.Errors[0].Code)
		}
	})

	t.Run("properly terminated strings", func(t *testing.T) {
		result := Tokenize(`severity_text = "error"`)

		if len(result.Errors) != 0 {
			t.Errorf("expected no errors, got %v", result.Errors)
		}
	})

	t.Run("nested field tokenization", func(t *testing.T) {
		result := Tokenize(`log_attributes.level = "error"`)

		if len(result.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", result.Errors)
		}

		keyTokens := filterTokens(result.Tokens, TokenKey)
		if len(keyTokens) != 1 {
			t.Fatalf("expected 1 key token, got %d", len(keyTokens))
		}
		if keyTokens[0].Value != "log_attributes.level" {
			t.Errorf("expected 'log_attributes.level', got %s", keyTokens[0].Value)
		}
	})

	t.Run("multiple operators", func(t *testing.T) {
		tests := []struct {
			query string
			op    string
		}{
			{`field != "value"`, "!="},
			{`field ~ "pattern"`, "~"},
			{`field !~ "pattern"`, "!~"},
			{`field > 10`, ">"},
			{`field < 10`, "<"},
			{`field >= 10`, ">="},
			{`field <= 10`, "<="},
		}

		for _, tc := range tests {
			result := Tokenize(tc.query)
			if len(result.Errors) != 0 {
				t.Errorf("query %q: expected no errors, got %v", tc.query, result.Errors)
				continue
			}

			opTokens := filterTokens(result.Tokens, TokenOperator)
			if len(opTokens) != 1 {
				t.Errorf("query %q: expected 1 operator token, got %d", tc.query, len(opTokens))
				continue
			}
			if opTokens[0].Value != tc.op {
				t.Errorf("query %q: expected operator %q, got %q", tc.query, tc.op, opTokens[0].Value)
			}
		}
	})

	t.Run("parentheses tokenization", func(t *testing.T) {
		result := Tokenize(`(severity_text = "error") and (service_name = "api")`)

		if len(result.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", result.Errors)
		}

		parenTokens := filterTokens(result.Tokens, TokenParen)
		if len(parenTokens) != 4 {
			t.Errorf("expected 4 paren tokens, got %d", len(parenTokens))
		}
	})
}

func TestParser(t *testing.T) {
	t.Run("simple expression", func(t *testing.T) {
		result := Tokenize(`severity_text = "error"`)
		parser := NewParser(result.Tokens)
		parseResult := parser.Parse()

		if len(parseResult.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", parseResult.Errors)
		}

		if parseResult.AST == nil {
			t.Fatal("expected AST, got nil")
		}

		expr, ok := parseResult.AST.(*ExpressionNode)
		if !ok {
			t.Fatalf("expected ExpressionNode, got %T", parseResult.AST)
		}

		if expr.Key != "severity_text" {
			t.Errorf("expected key 'severity_text', got %v", expr.Key)
		}
		if expr.Operator != OpEquals {
			t.Errorf("expected operator '=', got %v", expr.Operator)
		}
		if expr.Value != "error" {
			t.Errorf("expected value 'error', got %v", expr.Value)
		}
	})

	t.Run("logical AND expression", func(t *testing.T) {
		result := Tokenize(`severity_text = "error" and service_name = "api"`)
		parser := NewParser(result.Tokens)
		parseResult := parser.Parse()

		if len(parseResult.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", parseResult.Errors)
		}

		logical, ok := parseResult.AST.(*LogicalNode)
		if !ok {
			t.Fatalf("expected LogicalNode, got %T", parseResult.AST)
		}

		if logical.Operator != BoolAnd {
			t.Errorf("expected AND operator, got %v", logical.Operator)
		}
		if len(logical.Children) != 2 {
			t.Errorf("expected 2 children, got %d", len(logical.Children))
		}
	})

	t.Run("grouped expression", func(t *testing.T) {
		result := Tokenize(`(severity_text = "error")`)
		parser := NewParser(result.Tokens)
		parseResult := parser.Parse()

		if len(parseResult.Errors) != 0 {
			t.Fatalf("expected no errors, got %v", parseResult.Errors)
		}

		group, ok := parseResult.AST.(*GroupNode)
		if !ok {
			t.Fatalf("expected GroupNode, got %T", parseResult.AST)
		}

		if len(group.Children) != 1 {
			t.Errorf("expected 1 child, got %d", len(group.Children))
		}
	})

	t.Run("missing boolean operator detection", func(t *testing.T) {
		result := Tokenize(`severity_text = "error" service_name = "api"`)

		err := DetectMissingBooleanOperators(result.Tokens)
		if err == nil {
			t.Fatal("expected error for missing boolean operator")
		}

		if err.Code != ErrMissingBooleanOperator {
			t.Errorf("expected MISSING_BOOLEAN_OPERATOR, got %s", err.Code)
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
			{`severity_number > 3`, "> '3'"},
			{`severity_number < 5`, "< '5'"},
			{`severity_number >= 3`, ">= '3'"},
			{`severity_number <= 5`, "<= '5'"},
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
		if result.Error.Code != ErrUnterminatedString {
			t.Errorf("expected UNTERMINATED_STRING error, got %s", result.Error.Code)
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
		if result.Error.Code != ErrMissingBooleanOperator {
			t.Errorf("expected MISSING_BOOLEAN_OPERATOR error, got %s", result.Error.Code)
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
		if !strings.Contains(sql, "WHERE timestamp BETWEEN") {
			t.Error("expected WHERE clause with time range in query")
		}
		if !strings.Contains(sql, "severity_text") {
			t.Error("expected condition in query")
		}
		if !strings.Contains(sql, "ORDER BY timestamp DESC") {
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
}

// Helper function to filter tokens by type
func filterTokens(tokens []Token, tokenType TokenType) []Token {
	var result []Token
	for _, t := range tokens {
		if t.Type == tokenType {
			result = append(result, t)
		}
	}
	return result
}
