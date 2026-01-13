package template

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	Value interface{}  `json:"value"`
}

var (
	// variablePattern matches {{variable_name}} with optional whitespace.
	variablePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)
	// validNamePattern validates variable names.
	validNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// SubstituteVariables replaces {{variable}} placeholders with typed values.
// It returns an error if a variable is undefined or has an invalid name.
func SubstituteVariables(sql string, variables []Variable) (string, error) {
	if len(variables) == 0 {
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

	// Find all variable references in the SQL.
	matches := variablePattern.FindAllStringSubmatch(sql, -1)
	if len(matches) == 0 {
		return sql, nil
	}

	// Check that all referenced variables are defined.
	for _, match := range matches {
		varName := match[1]
		if _, exists := varMap[varName]; !exists {
			return "", fmt.Errorf("undefined variable: {{%s}}", varName)
		}
	}

	// Perform substitution.
	var substitutionErr error
	result := variablePattern.ReplaceAllStringFunc(sql, func(match string) string {
		if substitutionErr != nil {
			return match
		}

		submatches := variablePattern.FindStringSubmatch(match)
		if len(submatches) != 2 {
			return match
		}
		varName := submatches[1]

		v := varMap[varName]
		formatted, err := formatValue(v)
		if err != nil {
			substitutionErr = fmt.Errorf("variable %s: %w", varName, err)
			return match
		}
		return formatted
	})

	if substitutionErr != nil {
		return "", substitutionErr
	}

	return result, nil
}

// formatValue converts a variable to its SQL-safe representation.
func formatValue(v Variable) (string, error) {
	// Normalize type (text is alias for string).
	varType := v.Type
	if varType == TypeText {
		varType = TypeString
	}

	switch varType {
	case TypeString:
		return formatString(v.Value)
	case TypeNumber:
		return formatNumber(v.Value)
	case TypeDate:
		return formatDate(v.Value)
	default:
		// Default to string for unknown types.
		return formatString(v.Value)
	}
}

// formatString escapes and quotes a string value.
func formatString(value interface{}) (string, error) {
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
	// Escape single quotes for ClickHouse.
	escaped := strings.ReplaceAll(s, "'", "''")
	return fmt.Sprintf("'%s'", escaped), nil
}

// formatNumber validates and returns a numeric value.
func formatNumber(value interface{}) (string, error) {
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
func formatDate(value interface{}) (string, error) {
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
