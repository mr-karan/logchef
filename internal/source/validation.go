package source

import (
	"fmt"
	"net"
	"strconv"

	"github.com/mr-karan/logchef/pkg/models"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validator provides validation logic for sources
type Validator struct{}

// NewValidator creates a new source validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateSourceCreation validates source creation parameters
func (v *Validator) ValidateSourceCreation(conn models.ConnectionInfo, description string, ttlDays int, metaTSField string, autoCreateTable bool) error {
	// Validate table name
	if conn.TableName == "" {
		return &ValidationError{
			Field:   "TableName",
			Message: "table name is required",
		}
	}

	if !isValidTableName(conn.TableName) {
		return &ValidationError{
			Field:   "TableName",
			Message: "table name must start with a letter and contain only letters, numbers, and underscores",
		}
	}

	// Parse host and port
	_, portStr, err := net.SplitHostPort(conn.Host)
	if err != nil {
		return &ValidationError{
			Field:   "Host",
			Message: "host must include a port (e.g., 'localhost:9000')",
		}
	}

	// Validate port is a number
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return &ValidationError{
			Field:   "Host",
			Message: "port must be between 1 and 65535",
		}
	}

	// Username and Password are optional for Clickhouse, but if username is provided
	// then password must also be provided
	if conn.Username != "" && conn.Password == "" {
		return &ValidationError{
			Field:   "Password",
			Message: "password is required when username is provided",
		}
	}

	if conn.Database == "" {
		return &ValidationError{
			Field:   "Database",
			Message: "database is required",
		}
	}

	if len(description) > 50 {
		return &ValidationError{
			Field:   "Description",
			Message: "description must not exceed 50 characters",
		}
	}

	if ttlDays < -1 {
		return &ValidationError{
			Field:   "TTLDays",
			Message: "TTL days must be -1 (no TTL) or a non-negative number",
		}
	}

	if metaTSField == "" {
		return &ValidationError{
			Field:   "MetaTSField",
			Message: "meta timestamp field is required",
		}
	}

	if !isValidColumnName(metaTSField) {
		return &ValidationError{
			Field:   "MetaTSField",
			Message: "meta timestamp field must start with a letter or underscore and contain only letters, numbers, and underscores",
		}
	}

	return nil
}

// ValidateSourceUpdate validates source update parameters
func (v *Validator) ValidateSourceUpdate(description string, ttlDays int) error {
	if description == "" {
		return &ValidationError{
			Field:   "Description",
			Message: "description is required",
		}
	}

	if len(description) > 50 {
		return &ValidationError{
			Field:   "Description",
			Message: "description must not exceed 50 characters",
		}
	}

	if ttlDays < -1 {
		return &ValidationError{
			Field:   "TTLDays",
			Message: "TTL days must be -1 (no TTL) or a non-negative number",
		}
	}

	return nil
}

// isValidTableName checks if the name is valid for use as a table name
func isValidTableName(name string) bool {
	// Only allow alphanumeric and underscore, must start with a letter
	if len(name) == 0 || !isLetter(rune(name[0])) {
		return false
	}
	for _, r := range name {
		if !isAlphanumericOrUnderscore(r) {
			return false
		}
	}
	return true
}

// isValidColumnName checks if the name is valid for use as a column name
func isValidColumnName(name string) bool {
	if len(name) == 0 {
		return false
	}
	// Must start with a letter or underscore
	if !isLetter(rune(name[0])) && name[0] != '_' {
		return false
	}
	// Rest can be alphanumeric or underscore
	for _, r := range name[1:] {
		if !isAlphanumericOrUnderscore(r) {
			return false
		}
	}
	return true
}

// isLetter checks if a character is a letter
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isAlphanumericOrUnderscore checks if a character is alphanumeric or underscore
func isAlphanumericOrUnderscore(r rune) bool {
	return isLetter(r) || (r >= '0' && r <= '9') || r == '_'
}
