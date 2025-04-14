package core

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidationError represents a validation error, potentially wrapping an original error.
type ValidationError struct {
	Field   string
	Message string
	Err     error // Original error (optional)
}

func (e *ValidationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Field, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// --- Common Validation Helpers ---

// isLetter checks if a character is a letter.
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isAlphanumericOrUnderscore checks if a character is alphanumeric or underscore.
func isAlphanumericOrUnderscore(r rune) bool {
	return isLetter(r) || (r >= '0' && r <= '9') || r == '_'
}

// isValidEmail checks if the email format looks potentially valid (basic check).
func isValidEmail(email string) bool {
	// Basic email validation: non-empty, contains @, domain part contains .
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		return false
	}
	if !strings.Contains(parts[1], ".") {
		return false
	}
	return true
}

// isValidFullName checks if the name contains only valid characters for a person's name.
func isValidFullName(name string) bool {
	// Allow letters, spaces, hyphens, and apostrophes.
	for _, r := range name {
		if !unicode.IsLetter(r) && r != ' ' && r != '-' && r != '\'' {
			return false
		}
	}
	return true
}

// isValidTeamName checks if the team name contains only valid characters.
func isValidTeamName(name string) bool {
	// Allow letters, numbers, spaces, hyphens, and underscores.
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ' && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// isValidTableName checks if the name is valid for use as a database or table name.
func isValidTableName(name string) bool {
	// ClickHouse identifiers: start with letter or _, contain letters, numbers, _
	if len(name) == 0 {
		return false
	}
	firstChar := rune(name[0])
	if !isLetter(firstChar) && firstChar != '_' {
		return false
	}
	for _, r := range name {
		if !isAlphanumericOrUnderscore(r) {
			return false
		}
	}
	return true
}

// isValidColumnName checks if the name is valid for use as a column name.
func isValidColumnName(name string) bool {
	// Same rules as table name for ClickHouse typically.
	return isValidTableName(name)
}

// isValidSourceName checks if the name is valid for use as a LogChef source name.
func isValidSourceName(name string) bool {
	// More relaxed than table name validation - allows spaces, hyphens.
	// Check length constraint
	if len(name) == 0 || len(name) > 50 {
		return false
	}

	// Must not be empty and must not have leading/trailing spaces.
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return false
	}

	// Check allowed characters.
	for _, r := range name {
		if !isAllowedInSourceName(r) {
			return false
		}
	}

	return true
}

// isAllowedInSourceName checks if a character is allowed in source names.
func isAllowedInSourceName(r rune) bool {
	return isLetter(r) || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == ' '
}
