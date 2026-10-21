package template

import (
	"regexp"
	"strings"
)

var heredocDelimiter = regexp.MustCompile(`^\$[A-Za-z0-9_]*\$`)

// ProcessOptionalClauses removes clauses whose template values are missing.
func ProcessOptionalClauses(query string, variables map[string]Variable) string {
	return processOptionalClauses(query, variables, true)
}

func processOptionalClauses(query string, variables map[string]Variable, preserveArrays bool) string {
	var result strings.Builder
	arrayDepth := 0
	for i := 0; i < len(query); {
		if end := quotedOrCommentEnd(query, i); end > i {
			result.WriteString(query[i:end])
			i = end
			continue
		}
		if arrayDepth == 0 && strings.HasPrefix(query[i:], "[[") {
			end := optionalClauseEnd(query, i+2)
			if end >= 0 && (!preserveArrays || isOptionalClause(query[i+2:end])) {
				content := query[i+2 : end]
				provided := true
				for _, match := range variablePattern.FindAllStringSubmatch(content, -1) {
					variable, exists := variables[match[1]]
					if !exists || !isValueProvided(variable.Value) {
						provided = false
						break
					}
				}
				if provided {
					result.WriteString(content)
				}
				i = end + 2
				continue
			}
		}
		switch query[i] {
		case '[':
			arrayDepth++
		case ']':
			if arrayDepth > 0 {
				arrayDepth--
			}
		}
		result.WriteByte(query[i])
		i++
	}
	return result.String()
}

func optionalClauseEnd(query string, start int) int {
	depth := 0
	for i := start; i < len(query); i++ {
		if end := quotedOrCommentEnd(query, i); end > i {
			i = end - 1
			continue
		}
		switch query[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
				continue
			}
			if strings.HasPrefix(query[i:], "]]") {
				return i
			}
			return -1
		}
	}
	return -1
}

func isOptionalClause(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	if content[0] == '|' {
		return true
	}
	end := 0
	for end < len(content) && ((content[end] >= 'a' && content[end] <= 'z') || (content[end] >= 'A' && content[end] <= 'Z')) {
		end++
	}
	// Clause keywords distinguish optional blocks from nested array expressions.
	switch strings.ToUpper(content[:end]) {
	case "AND", "OR", "WHERE", "PREWHERE", "HAVING", "GROUP", "ORDER", "LIMIT", "OFFSET", "SETTINGS", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "UNION":
		return end == len(content) || content[end] == ' ' || content[end] == '\t' || content[end] == '\n' || content[end] == '\r' || content[end] == '('
	default:
		return false
	}
}

func quotedOrCommentEnd(query string, start int) int {
	if query[start] == '$' {
		delimiter := heredocDelimiter.FindString(query[start:])
		if delimiter != "" {
			contentStart := start + len(delimiter)
			if end := strings.Index(query[contentStart:], delimiter); end >= 0 {
				return contentStart + end + len(delimiter)
			}
			return len(query)
		}
	}

	if strings.HasPrefix(query[start:], "--") || query[start] == '#' {
		if end := strings.IndexAny(query[start:], "\r\n"); end >= 0 {
			return start + end
		}
		return len(query)
	}
	if strings.HasPrefix(query[start:], "/*") {
		depth := 1
		for i := start + 2; i < len(query); i++ {
			switch {
			case strings.HasPrefix(query[i:], "/*"):
				depth++
				i++
			case strings.HasPrefix(query[i:], "*/"):
				depth--
				i++
				if depth == 0 {
					return i + 1
				}
			}
		}
		return len(query)
	}

	quote := query[start]
	if quote != '\'' && quote != '"' && quote != '`' {
		return start
	}
	for i := start + 1; i < len(query); i++ {
		if query[i] == '\\' {
			i++
			continue
		}
		if query[i] == quote {
			if i+1 < len(query) && query[i+1] == quote {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(query)
}
