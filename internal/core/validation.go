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
