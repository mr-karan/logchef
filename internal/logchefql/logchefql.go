package logchefql

import (
	"fmt"
	"strings"
)

// Translate parses a LogchefQL query and returns the SQL translation with metadata.
// This is the main entry point for query translation.
func Translate(query string, schema *Schema) *TranslateResult {
	result := &TranslateResult{
		Valid:      false,
		Conditions: []FilterCondition{},
		FieldsUsed: []string{},
	}

	// Handle empty query
	if query == "" || strings.TrimSpace(query) == "" {
		result.Valid = true
		result.SQL = ""
		return result
	}

	// Tokenize
	tokenResult := Tokenize(query)
	if len(tokenResult.Errors) > 0 {
		result.Error = &tokenResult.Errors[0]
		return result
	}

	// Check for missing boolean operators before parsing
	if missingOpErr := DetectMissingBooleanOperators(tokenResult.Tokens); missingOpErr != nil {
		result.Error = missingOpErr
		return result
	}

	// Parse
	parser := NewParser(tokenResult.Tokens)
	parseResult := parser.Parse()
	if len(parseResult.Errors) > 0 {
		result.Error = &parseResult.Errors[0]
		return result
	}

	// Generate SQL
	generator := NewSQLGenerator(schema)
	sql := generator.Generate(parseResult.AST)

	// Check if query has SELECT clause (pipe operator)
	var selectClause string
	if queryNode, ok := parseResult.AST.(*QueryNode); ok && len(queryNode.Select) > 0 {
		// Generate SELECT clause - pass empty string for timestamp field since
		// the caller will prepend it if needed
		selectClause = generator.GenerateSelectClause(queryNode.Select, "")
	}

	// Extract metadata
	fieldsUsed := extractFieldsUsed(tokenResult.Tokens)
	conditions := extractConditions(tokenResult.Tokens)

	result.Valid = true
	result.SQL = sql
	result.SelectClause = selectClause
	result.FieldsUsed = fieldsUsed
	result.Conditions = conditions

	return result
}

// Validate checks if a LogchefQL query is syntactically valid.
func Validate(query string) *ValidateResult {
	result := &ValidateResult{Valid: false}

	// Handle empty query
	if query == "" || strings.TrimSpace(query) == "" {
		result.Valid = true
		return result
	}

	// Tokenize
	tokenResult := Tokenize(query)
	if len(tokenResult.Errors) > 0 {
		result.Error = &tokenResult.Errors[0]
		return result
	}

	// Check for missing boolean operators
	if missingOpErr := DetectMissingBooleanOperators(tokenResult.Tokens); missingOpErr != nil {
		result.Error = missingOpErr
		return result
	}

	// Parse
	parser := NewParser(tokenResult.Tokens)
	parseResult := parser.Parse()
	if len(parseResult.Errors) > 0 {
		result.Error = &parseResult.Errors[0]
		return result
	}

	result.Valid = true
	return result
}

// ValidateWithDetails is an alias for Validate that returns detailed error information.
func ValidateWithDetails(query string) *ValidateResult {
	return Validate(query)
}

// extractFieldsUsed extracts unique field names from tokens
func extractFieldsUsed(tokens []Token) []string {
	seen := make(map[string]bool)
	var fields []string

	for _, token := range tokens {
		if token.Type == TokenKey {
			// Extract base field name (handle nested fields)
			fieldName := token.Value
			if idx := strings.Index(fieldName, "."); idx > 0 {
				fieldName = fieldName[:idx]
			}

			if !seen[fieldName] {
				seen[fieldName] = true
				fields = append(fields, fieldName)
			}
		}
	}

	return fields
}

// extractConditions extracts filter conditions from tokens
func extractConditions(tokens []Token) []FilterCondition {
	var conditions []FilterCondition

	i := 0
	for i < len(tokens) {
		// Look for pattern: key -> operator -> value
		if i+2 < len(tokens) &&
			tokens[i].Type == TokenKey &&
			tokens[i+1].Type == TokenOperator &&
			(tokens[i+2].Type == TokenValue || tokens[i+2].Type == TokenKey) {

			field := tokens[i].Value
			operator := tokens[i+1].Value
			value := tokens[i+2].Value
			isRegex := operator == "~" || operator == "!~"

			conditions = append(conditions, FilterCondition{
				Field:    field,
				Operator: operator,
				Value:    value,
				IsRegex:  isRegex,
			})

			i += 3
		} else {
			i++
		}
	}

	return conditions
}

// TranslateToSQLConditions is a convenience function that returns just the SQL string.
// Returns empty string on error.
func TranslateToSQLConditions(query string, schema *Schema) string {
	result := Translate(query, schema)
	if !result.Valid {
		return ""
	}
	return result.SQL
}

// BuildFullQuery builds a complete SQL query from LogchefQL with time range and other parameters.
// This is used when executing the query against ClickHouse.
func BuildFullQuery(params QueryBuildParams) (string, error) {
	// Translate LogchefQL to SQL conditions
	translateResult := Translate(params.LogchefQL, params.Schema)
	if !translateResult.Valid {
		if translateResult.Error != nil {
			return "", fmt.Errorf("invalid LogchefQL: %s", translateResult.Error.Message)
		}
		return "", fmt.Errorf("invalid LogchefQL query")
	}

	// Build the full query
	var query strings.Builder

	// SELECT clause - prefer SelectClause from pipe operator, then params.SelectFields, then *
	query.WriteString("SELECT ")
	if translateResult.SelectClause != "" {
		// Pipe operator used - include timestamp field first, then selected fields
		if params.TimestampField != "" {
			query.WriteString(fmt.Sprintf("`%s`, ", params.TimestampField))
		}
		query.WriteString(translateResult.SelectClause)
	} else if params.SelectFields != "" {
		query.WriteString(params.SelectFields)
	} else {
		query.WriteString("*")
	}
	query.WriteString("\n")

	// FROM clause
	query.WriteString("FROM ")
	query.WriteString(params.TableName)
	query.WriteString("\n")

	// WHERE clause with time range
	query.WriteString("WHERE ")
	query.WriteString(params.TimestampField)
	query.WriteString(" BETWEEN toDateTime('")
	query.WriteString(params.StartTime)
	query.WriteString("', '")
	query.WriteString(params.Timezone)
	query.WriteString("') AND toDateTime('")
	query.WriteString(params.EndTime)
	query.WriteString("', '")
	query.WriteString(params.Timezone)
	query.WriteString("')")

	// Add LogchefQL conditions if present
	if translateResult.SQL != "" {
		query.WriteString("\n  AND (")
		query.WriteString(translateResult.SQL)
		query.WriteString(")")
	}
	query.WriteString("\n")

	// ORDER BY clause
	query.WriteString("ORDER BY ")
	query.WriteString(params.TimestampField)
	query.WriteString(" DESC\n")

	// LIMIT clause
	if params.Limit > 0 {
		query.WriteString(fmt.Sprintf("LIMIT %d", params.Limit))
	}

	return query.String(), nil
}

// QueryBuildParams contains parameters for building a full SQL query
type QueryBuildParams struct {
	LogchefQL      string  // The LogchefQL query string
	Schema         *Schema // Optional schema for type-aware SQL generation
	TableName      string  // Fully qualified table name (database.table)
	TimestampField string  // Name of the timestamp column
	StartTime      string  // Start time in format "2006-01-02 15:04:05"
	EndTime        string  // End time in format "2006-01-02 15:04:05"
	Timezone       string  // Timezone for time conversion
	Limit          int     // Result limit
	SelectFields   string  // Optional SELECT clause (defaults to *)
}

// GetConditionsFromQuery extracts filter conditions from a LogchefQL query.
// This is useful for the field sidebar feature.
func GetConditionsFromQuery(query string) []FilterCondition {
	result := Translate(query, nil)
	if !result.Valid {
		return nil
	}
	return result.Conditions
}
