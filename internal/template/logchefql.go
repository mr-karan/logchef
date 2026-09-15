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
	values := make(map[string]Variable, len(variables))
	for _, variable := range variables {
		if !validNamePattern.MatchString(variable.Name) {
			return "", fmt.Errorf("invalid variable name: %s", variable.Name)
		}
		values[variable.Name] = variable
	}
	query = ProcessOptionalClauses(query, values)
	var substitutionErr error
	result := logchefQLTemplateToken.ReplaceAllStringFunc(query, func(token string) string {
		quoted := token[0] == '"' || token[0] == '\''
		return variablePattern.ReplaceAllStringFunc(token, func(match string) string {
			name := variablePattern.FindStringSubmatch(match)[1]
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
	case TypeDate:
		formatted, err := formatDate(variable.Value)
		if err != nil {
			return "", err
		}
		value = formatted[1 : len(formatted)-1]
	}
	value = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "'", "\\'", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(value)
	if quoted {
		return value, nil
	}
	return `"` + value + `"`, nil
}
