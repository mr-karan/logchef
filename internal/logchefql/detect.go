package logchefql

import (
	"regexp"
	"strings"
)

var (
	sqlPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*SELECT\s+`),
		regexp.MustCompile(`(?i)^\s*WITH\s+`),
		regexp.MustCompile(`(?i)\s+FROM\s+`),
		regexp.MustCompile(`(?i)\s+GROUP\s+BY\s+`),
		regexp.MustCompile(`(?i)\s+ORDER\s+BY\s+`),
	}

	logchefqlPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\w+\s*=\s*"`),
		regexp.MustCompile(`\w+\s*!=\s*"`),
		regexp.MustCompile(`\w+\s*~\s*"`),
		regexp.MustCompile(`\|\s*\w+`),
	}
)

func DetectQueryType(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "logchefql"
	}

	for _, pattern := range sqlPatterns {
		if pattern.MatchString(query) {
			return "sql"
		}
	}

	for _, pattern := range logchefqlPatterns {
		if pattern.MatchString(query) {
			return "logchefql"
		}
	}

	return "logchefql"
}
