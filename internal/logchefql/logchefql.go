package logchefql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Translate parses a LogchefQL query and returns the SQL translation with metadata.
func Translate(query string, schema *Schema) *TranslateResult {
	result := &TranslateResult{
		Valid:      false,
		Conditions: []FilterCondition{},
		FieldsUsed: []string{},
	}

	if query == "" || strings.TrimSpace(query) == "" {
		result.Valid = true
		result.SQL = ""
		return result
	}

	pq, err := ParseLogchefQL(query)
	if err != nil {
		result.Error = convertParticipleError(err)
		return result
	}

	ast := ConvertToAST(pq)

	generator := NewSQLGenerator(schema)
	sql := generator.Generate(ast)

	var selectClause string
	if queryNode, ok := ast.(*QueryNode); ok && len(queryNode.Select) > 0 {
		selectClause = generator.GenerateSelectClause(queryNode.Select, "")
	}

	fieldsUsed := extractFieldsFromAST(ast)
	conditions := extractConditionsFromAST(ast)

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

	if query == "" || strings.TrimSpace(query) == "" {
		result.Valid = true
		return result
	}

	_, err := ParseLogchefQL(query)
	if err != nil {
		result.Error = convertParticipleError(err)
		return result
	}

	result.Valid = true
	return result
}

// ValidateWithDetails is an alias for Validate that returns detailed error information.
func ValidateWithDetails(query string) *ValidateResult {
	return Validate(query)
}

func convertParticipleError(err error) *ParseError {
	if err == nil {
		return nil
	}

	msg := err.Error()
	lowerMsg := strings.ToLower(msg)

	code := ErrUnexpectedToken
	if strings.Contains(lowerMsg, "unterminated") {
		code = ErrUnterminatedString
	} else if strings.Contains(lowerMsg, "unexpected") && strings.Contains(lowerMsg, "ident") {
		code = ErrMissingBooleanOperator
	}

	pos := extractPositionFromError(msg)

	return &ParseError{
		Code:     code,
		Message:  msg,
		Position: pos,
	}
}

func extractPositionFromError(msg string) *Position {
	idx := strings.Index(msg, ":")
	if idx == -1 {
		return nil
	}

	lineStr := msg[:idx]
	line, err := strconv.Atoi(lineStr)
	if err != nil {
		return nil
	}

	rest := msg[idx+1:]
	idx2 := strings.Index(rest, ":")
	if idx2 == -1 {
		return nil
	}

	colStr := rest[:idx2]
	col, err := strconv.Atoi(colStr)
	if err != nil {
		return nil
	}

	return &Position{Line: line, Column: col}
}

func extractFieldsFromAST(node ASTNode) []string {
	seen := make(map[string]bool)
	var fields []string

	var walk func(n ASTNode)
	walk = func(n ASTNode) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *ExpressionNode:
			fieldName := getFieldName(v.Key)
			if idx := strings.Index(fieldName, "."); idx > 0 {
				fieldName = fieldName[:idx]
			}
			if fieldName != "" && !seen[fieldName] {
				seen[fieldName] = true
				fields = append(fields, fieldName)
			}
		case *LogicalNode:
			for _, child := range v.Children {
				walk(child)
			}
		case *GroupNode:
			for _, child := range v.Children {
				walk(child)
			}
		case *QueryNode:
			walk(v.Where)
		}
	}

	walk(node)
	return fields
}

func extractConditionsFromAST(node ASTNode) []FilterCondition {
	var conditions []FilterCondition

	var walk func(n ASTNode)
	walk = func(n ASTNode) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *ExpressionNode:
			field := getFieldName(v.Key)
			op := string(v.Operator)
			value := formatConditionValue(v.Value)
			isRegex := op == "~" || op == "!~"

			conditions = append(conditions, FilterCondition{
				Field:    field,
				Operator: op,
				Value:    value,
				IsRegex:  isRegex,
			})
		case *LogicalNode:
			for _, child := range v.Children {
				walk(child)
			}
		case *GroupNode:
			for _, child := range v.Children {
				walk(child)
			}
		case *QueryNode:
			walk(v.Where)
		}
	}

	walk(node)
	return conditions
}

func getFieldName(key interface{}) string {
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

func formatConditionValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
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

var (
	timeFormatRegex     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)
	timezoneAllowedChar = regexp.MustCompile(`^[A-Za-z0-9_/+:-]+$`)
	// Allows @ prefix for ELK-style @timestamp fields
	validIdentifier = regexp.MustCompile(`^@?[a-zA-Z_][a-zA-Z0-9_]*$`)
	// Requires database.table format (ClickHouse best practice)
	validTableName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*$`)
)

func validateTimeFormat(t string) *ParseError {
	if !timeFormatRegex.MatchString(t) {
		return &ParseError{
			Code:    ErrInvalidTimeFormat,
			Message: fmt.Sprintf("invalid time format: expected 'YYYY-MM-DD HH:MM:SS', got '%s'", t),
		}
	}
	if _, err := time.Parse("2006-01-02 15:04:05", t); err != nil {
		return &ParseError{
			Code:    ErrInvalidTimeFormat,
			Message: fmt.Sprintf("invalid time value: %s", t),
		}
	}
	return nil
}

func validateTimezone(tz string) *ParseError {
	if tz == "" {
		return &ParseError{
			Code:    ErrInvalidTimezone,
			Message: "timezone cannot be empty",
		}
	}
	if !timezoneAllowedChar.MatchString(tz) {
		return &ParseError{
			Code:    ErrInvalidTimezone,
			Message: "invalid timezone: contains disallowed characters",
		}
	}
	if len(tz) > 64 {
		return &ParseError{
			Code:    ErrInvalidTimezone,
			Message: "invalid timezone: too long",
		}
	}
	return nil
}

func validateIdentifier(name, fieldName string) *ParseError {
	if !validIdentifier.MatchString(name) {
		return &ParseError{
			Code:    ErrInvalidIdentifier,
			Message: fmt.Sprintf("invalid %s '%s': must start with letter or underscore, contain only alphanumeric and underscore", fieldName, name),
		}
	}
	return nil
}

func validateTableName(name string) *ParseError {
	if !validTableName.MatchString(name) {
		return &ParseError{
			Code:    ErrInvalidIdentifier,
			Message: fmt.Sprintf("invalid table name '%s': expected format 'database.table' with valid identifiers", name),
		}
	}
	return nil
}

// BuildFullQuery builds a complete SQL query from LogchefQL with time range and other parameters.
// This is used when executing the query against ClickHouse.
func BuildFullQuery(params QueryBuildParams) (string, error) {
	if err := validateTimeFormat(params.StartTime); err != nil {
		return "", err
	}
	if err := validateTimeFormat(params.EndTime); err != nil {
		return "", err
	}
	if err := validateTimezone(params.Timezone); err != nil {
		return "", err
	}
	if err := validateTableName(params.TableName); err != nil {
		return "", err
	}
	if err := validateIdentifier(params.TimestampField, "timestamp field"); err != nil {
		return "", err
	}

	translateResult := Translate(params.LogchefQL, params.Schema)
	if !translateResult.Valid {
		if translateResult.Error != nil {
			return "", translateResult.Error
		}
		return "", &ParseError{Code: ErrUnexpectedToken, Message: "invalid LogchefQL query"}
	}

	var query strings.Builder

	query.WriteString("SELECT ")
	if translateResult.SelectClause != "" {
		timestampInSelect := strings.Contains(translateResult.SelectClause, "`"+params.TimestampField+"`")
		if params.TimestampField != "" && !timestampInSelect {
			query.WriteString(fmt.Sprintf("`%s`, ", params.TimestampField))
		}
		query.WriteString(translateResult.SelectClause)
	} else {
		query.WriteString("*")
	}
	query.WriteString("\n")

	// FROM clause
	query.WriteString("FROM ")
	query.WriteString(params.TableName)
	query.WriteString("\n")

	// WHERE clause with time range
	query.WriteString("WHERE `")
	query.WriteString(params.TimestampField)
	query.WriteString("` BETWEEN toDateTime('")
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
	query.WriteString("ORDER BY `")
	query.WriteString(params.TimestampField)
	query.WriteString("` DESC\n")

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
