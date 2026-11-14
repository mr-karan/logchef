package template

import (
	"fmt"
	"regexp"
	"strings"
)

// Match complete literals first so substitution preserves their quote context.
var logchefQLTemplateToken = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\{\{\s*[a-zA-Z_][a-zA-Z0-9_]*\s*\}\}`)

// SubstituteLogchefQLVariables formats values using LogchefQL's backslash escapes.
func SubstituteLogchefQLVariables(query string, variables []Variable) (string, error) {
	values, err := variableMap(variables)
	if err != nil {
		return "", err
	}
	query = processOptionalClauses(query, values, LogchefQL)
	var substitutionErr error
	result := logchefQLTemplateToken.ReplaceAllStringFunc(query, func(token string) string {
		quoted := token[0] == '"' || token[0] == '\''
		return variablePattern.ReplaceAllStringFunc(token, func(match string) string {
			name := variablePattern.FindStringSubmatch(match)[1]
			if IsBuiltin(name) {
				_, substitutionErr = expandMacro(name, 0, Builtins{}, LogchefQL)
				return match
			}
			variable, exists := values[name]
			if !exists || !isValueProvided(variable.Value) {
				substitutionErr = fmt.Errorf("variable {{%s}} requires a value", name)
				return match
			}
			value, err := formatLogchefQLValue(variable, quoted)
			if err != nil {
				substitutionErr = fmt.Errorf("variable %s: %w", name, err)
				return match
			}
			return value
		})
	})
	if substitutionErr != nil {
		return "", substitutionErr
	}
	return result, nil
}

func formatLogchefQLValue(variable Variable, quoted bool) (string, error) {
	if isArrayValue(variable.Value) {
		return "", fmt.Errorf("LogchefQL requires a single value")
	}
	value := fmt.Sprint(variable.Value)
	switch variable.Type {
	case TypeNumber:
		formatted, err := formatNumber(variable.Value)
		if err != nil || !quoted {
			return formatted, err
		}
		value = formatted
	case TypeDay:
		formatted, err := formatDay(variable.Value, LogchefQL)
		if err != nil {
			return "", err
		}
		value = formatted[1 : len(formatted)-1]
	case TypeDate:
		formatted, err := formatDate(variable.Value)
		if err != nil {
			return "", err
		}
		value = formatted[1 : len(formatted)-1]
	case TypeString, TypeText:
		// Used as-is: value is already fmt.Sprint(variable.Value).
	}
	value = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "'", "\\'", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(value)
	if quoted {
		return value, nil
	}
	return `"` + value + `"`, nil
}
