package template

import (
	"regexp"
	"strings"
)

var heredocDelimiter = regexp.MustCompile(`^\$[A-Za-z0-9_]*\$`)

func processOptionalClauses(query string, variables map[string]Variable, dialect Dialect) string {
	var result strings.Builder
	arrayDepth := 0
	for i := 0; i < len(query); {
		if end := quotedOrCommentEnd(query, i, dialect); end > i {
			result.WriteString(query[i:end])
			i = end
			continue
		}
		if (dialect != ClickHouseSQL || arrayDepth == 0) && strings.HasPrefix(query[i:], "[[") {
			end := optionalClauseEnd(query, i+2, dialect)
			if end >= 0 {
				content := query[i+2 : end]
				names, optional := optionalVariables(content, dialect)
				if optional {
					provided := true
					for _, name := range names {
						variable, exists := variables[name]
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

func optionalClauseEnd(query string, start int, dialect Dialect) int {
	depth := 0
	for i := start; i < len(query); i++ {
		if end := quotedOrCommentEnd(query, i, dialect); end > i {
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

func optionalVariables(content string, dialect Dialect) ([]string, bool) {
	var names []string
	for i := 0; i < len(content); {
		if end := quotedOrCommentEnd(content, i, dialect); end > i {
			if strings.ContainsRune("'\"`$", rune(content[i])) {
				for _, match := range variablePattern.FindAllStringSubmatch(content[i:end], -1) {
					names = append(names, match[1])
				}
			}
			i = end
			continue
		}
		if strings.HasPrefix(content[i:], "{{") {
			match := variablePattern.FindStringSubmatchIndex(content[i:])
			if len(match) > 0 && match[0] == 0 {
				names = append(names, content[i+match[2]:i+match[3]])
				i += match[1]
				continue
			}
		}
		i++
	}
	return names, len(names) > 0
}

func quotedOrCommentEnd(query string, start int, dialect Dialect) int {
	if dialect == ClickHouseSQL && query[start] == '$' {
		delimiter := heredocDelimiter.FindString(query[start:])
		if delimiter != "" {
			contentStart := start + len(delimiter)
			if end := strings.Index(query[contentStart:], delimiter); end >= 0 {
				return contentStart + end + len(delimiter)
			}
			return len(query)
		}
	}

	if dialect == ClickHouseSQL && strings.HasPrefix(query[start:], "--") || dialect != LogchefQL && query[start] == '#' {
		if end := strings.IndexAny(query[start:], "\r\n"); end >= 0 {
			return start + end
		}
		return len(query)
	}
	if dialect == ClickHouseSQL && strings.HasPrefix(query[start:], "/*") {
		return blockCommentEnd(query, start)
	}

	return quotedEnd(query, start, dialect)
}

func quotedEnd(query string, start int, dialect Dialect) int {
	quote := query[start]
	if quote != '\'' && quote != '"' && (quote != '`' || dialect == LogchefQL) {
		return start
	}
	for i := start + 1; i < len(query); i++ {
		if query[i] == '\\' && (quote != '`' || dialect == ClickHouseSQL) {
			i++
			continue
		}
		if query[i] == quote {
			if dialect == ClickHouseSQL && i+1 < len(query) && query[i+1] == quote {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(query)
}

func blockCommentEnd(query string, start int) int {
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
