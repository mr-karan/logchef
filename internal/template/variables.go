package template

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/queryescape"
)

// VariableType defines the type of a template variable.
type VariableType string

const (
	// TypeString represents a string variable that will be quoted.
	TypeString VariableType = "string"
	// TypeText is an alias for string (used by frontend).
	TypeText VariableType = "text"
	// TypeNumber represents a numeric variable inserted as-is.
	TypeNumber VariableType = "number"
	// TypeDate represents a date variable formatted as ClickHouse datetime.
	TypeDate VariableType = "date"
)

// Variable represents a template variable with its value.
type Variable struct {
	Name  string       `json:"name"`
	Type  VariableType `json:"type"`
	Value any          `json:"value"`
}

var (
	// variablePattern matches {{variable_name}} with optional whitespace.
	variablePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)
	// validNamePattern validates variable names.
	validNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

func isValueProvided(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	case float64, int, int64:
		return true
	default:
		return true
	}
}

// SubstituteVariables formats template values as ClickHouse SQL literals.
func SubstituteVariables(sql string, variables []Variable) (string, error) {
	return substituteVariables(sql, variables, clickHouseSQL)
}

// SubstituteLogsQLVariables formats template values as LogsQL literals.
func SubstituteLogsQLVariables(query string, variables []Variable) (string, error) {
	return substituteVariables(query, variables, logsQL)
}

type queryDialect uint8

const (
	clickHouseSQL queryDialect = iota
	logsQL
)

func substituteVariables(sql string, variables []Variable, dialect queryDialect) (string, error) {
	if len(variables) == 0 {
		sql = ProcessOptionalClauses(sql, nil)
		return sql, nil
	}

	// Build lookup map and validate names.
	varMap := make(map[string]Variable, len(variables))
	for _, v := range variables {
		if !validNamePattern.MatchString(v.Name) {
			return "", fmt.Errorf("invalid variable name: %s", v.Name)
		}
		varMap[v.Name] = v
	}

	// Process optional clauses first - removes blocks with missing variables
	sql = ProcessOptionalClauses(sql, varMap)

	return substituteTemplateTokens(sql, varMap, dialect)
}

func substituteTemplateTokens(query string, variables map[string]Variable, dialect queryDialect) (string, error) {
	var result strings.Builder
	start := 0
	for i := 0; i < len(query); {
		end := quotedOrCommentEnd(query, i)
		if end == i {
			i++
			continue
		}
		plain, err := substituteVariableTokens(query[start:i], 0, variables, dialect)
		if err != nil {
			return "", err
		}
		result.WriteString(plain)
		token := query[i:end]
		if token[0] == '\'' || token[0] == '"' || token[0] == '`' || token[0] == '$' {
			token, err = substituteVariableTokens(token, token[0], variables, dialect)
			if err != nil {
				return "", err
			}
		}
		result.WriteString(token)
		i, start = end, end
	}
	tail, err := substituteVariableTokens(query[start:], 0, variables, dialect)
	if err != nil {
		return "", err
	}
	result.WriteString(tail)
	return result.String(), nil
}

func substituteVariableTokens(token string, quote byte, variables map[string]Variable, dialect queryDialect) (string, error) {
	if quote != 0 {
		for _, match := range variablePattern.FindAllStringIndex(token, -1) {
			backslashes := 0
			for i := match[0] - 1; i >= 0 && token[i] == '\\'; i-- {
				backslashes++
			}
			if backslashes%2 != 0 {
				return "", fmt.Errorf("template variables cannot follow an unescaped backslash inside quotes")
			}
		}
	}
	var substitutionErr error
	result := variablePattern.ReplaceAllStringFunc(token, func(match string) string {
		name := variablePattern.FindStringSubmatch(match)[1]
		variable, exists := variables[name]
		if !exists {
			substitutionErr = fmt.Errorf("undefined variable: {{%s}}", name)
			return match
		}
		if !isValueProvided(variable.Value) {
			substitutionErr = fmt.Errorf("variable {{%s}} requires a value", name)
			return match
		}
		if quote != 0 && (quote == '`' || quote == '$' || dialect == clickHouseSQL && quote == '"') {
			substitutionErr = fmt.Errorf("variable %s: template values cannot be used inside identifier or raw-string quotes", name)
			return match
		}
		if quote != 0 && isArrayValue(variable.Value) {
			substitutionErr = fmt.Errorf("variable %s: array values must be outside quotes", name)
			return match
		}
		formatted, err := formatValue(variable, dialect)
		if err != nil {
			substitutionErr = fmt.Errorf("variable %s: %w", name, err)
			return match
		}
		if quote != 0 && variable.Type != TypeNumber {
			formatted = formatted[1 : len(formatted)-1]
			if dialect == logsQL && quote == '\'' {
				formatted = strings.NewReplacer(`\"`, `"`, "'", "\\'").Replace(formatted)
			}
		}
		return formatted
	})
	return result, substitutionErr
}

func formatValue(v Variable, dialect queryDialect) (string, error) {
	varType := v.Type
	if varType == TypeText {
		varType = TypeString
	}

	if isArrayValue(v.Value) {
		return formatArray(v.Value, varType, dialect)
	}

	switch varType {
	case TypeString:
		return formatString(v.Value, dialect)
	case TypeNumber:
		return formatNumber(v.Value)
	case TypeDate:
		return formatDialectDate(v.Value, dialect)
	default:
		return formatString(v.Value, dialect)
	}
}

func isArrayValue(value any) bool {
	switch value.(type) {
	case []any, []string:
		return true
	default:
		return false
	}
}

func formatArray(value any, varType VariableType, dialect queryDialect) (string, error) {
	var items []string

	switch arr := value.(type) {
	case []any:
		for _, item := range arr {
			formatted, err := formatSingleValue(item, varType, dialect)
			if err != nil {
				return "", err
			}
			items = append(items, formatted)
		}
	case []string:
		for _, item := range arr {
			formatted, err := formatSingleValue(item, varType, dialect)
			if err != nil {
				return "", err
			}
			items = append(items, formatted)
		}
	default:
		return "", fmt.Errorf("unsupported array type: %T", value)
	}

	if len(items) == 0 {
		return "", fmt.Errorf("empty array value")
	}

	return strings.Join(items, ", "), nil
}

func formatSingleValue(value any, varType VariableType, dialect queryDialect) (string, error) {
	switch varType {
	case TypeString, TypeText:
		return formatString(value, dialect)
	case TypeNumber:
		return formatNumber(value)
	case TypeDate:
		return formatDialectDate(value, dialect)
	default:
		return formatString(value, dialect)
	}
}

// formatString escapes and quotes a string value.
func formatString(value any, dialect queryDialect) (string, error) {
	var s string
	switch val := value.(type) {
	case string:
		s = val
	case float64:
		s = strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		s = strconv.Itoa(val)
	case int64:
		s = strconv.FormatInt(val, 10)
	default:
		s = fmt.Sprintf("%v", val)
	}
	if dialect == logsQL {
		return strconv.Quote(s), nil
	}
	return "'" + queryescape.ClickHouseString(s) + "'", nil
}

// formatNumber validates and returns a numeric value.
func formatNumber(value any) (string, error) {
	switch val := value.(type) {
	case float64:
		// Check if it's actually an integer.
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10), nil
		}
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(val), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case string:
		// Validate it's actually numeric.
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			return "", fmt.Errorf("invalid number: %s", val)
		}
		return val, nil
	default:
		return "", fmt.Errorf("unsupported number type: %T", val)
	}
}

// formatDate parses and formats a date value as ClickHouse datetime.
func formatDate(value any) (string, error) {
	switch val := value.(type) {
	case string:
		// Try various date formats, including those from HTML datetime-local inputs.
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05",
			"2006-01-02T15:04",
			"2006-01-02 15:04:05",
			"2006-01-02 15:04",
			"2006-01-02",
		}

		var t time.Time
		var err error
		for _, format := range formats {
			t, err = time.Parse(format, val)
			if err == nil {
				break
			}
		}
		if err != nil {
			return "", fmt.Errorf("invalid date format: %s (expected ISO 8601 format)", val)
		}

		// Format as ClickHouse datetime string.
		return fmt.Sprintf("'%s'", t.Format("2006-01-02 15:04:05")), nil

	case time.Time:
		return fmt.Sprintf("'%s'", val.Format("2006-01-02 15:04:05")), nil

	case float64:
		// Assume Unix timestamp in milliseconds (common from JavaScript).
		t := time.UnixMilli(int64(val))
		return fmt.Sprintf("'%s'", t.UTC().Format("2006-01-02 15:04:05")), nil

	case int64:
		// Assume Unix timestamp in milliseconds.
		t := time.UnixMilli(val)
		return fmt.Sprintf("'%s'", t.UTC().Format("2006-01-02 15:04:05")), nil

	default:
		return "", fmt.Errorf("unsupported date type: %T", val)
	}
}

// ExtractVariableNames returns all unique variable names found in the SQL.
func ExtractVariableNames(sql string) []string {
	matches := variablePattern.FindAllStringSubmatch(sql, -1)
	seen := make(map[string]bool)
	names := make([]string, 0, len(matches))

	for _, m := range matches {
		if len(m) == 2 && !seen[m[1]] {
			names = append(names, m[1])
			seen[m[1]] = true
		}
	}
	return names
}

func formatDialectDate(value any, dialect queryDialect) (string, error) {
	formatted, err := formatDate(value)
	if err != nil {
		return "", err
	}
	if dialect == logsQL {
		return strconv.Quote(formatted[1 : len(formatted)-1]), nil
	}
	return formatted, nil
}
