package source

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
	Err     error // Original error
}

func (e *ValidationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Field, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validator provides validation logic for sources
type Validator struct{}

// NewValidator creates a new source validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateSourceCreation validates source creation parameters
func (v *Validator) ValidateSourceCreation(name string, conn models.ConnectionInfo, description string, ttlDays int, metaTSField string, metaSeverityField string, autoCreateTable bool) error {
	// Validate source name
	if name == "" {
		return &ValidationError{
			Field:   "name",
			Message: "source name is required",
		}
	}

	if !isValidSourceName(name) {
		return &ValidationError{
			Field:   "name",
			Message: "source name must not exceed 50 characters and can only contain letters, numbers, spaces, hyphens, and underscores",
		}
	}

	// Validate table name
	if conn.TableName == "" {
		return &ValidationError{
			Field:   "table_name",
			Message: "table name is required",
		}
	}

	if !isValidTableName(conn.TableName) {
		return &ValidationError{
			Field:   "table_name",
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

	// Severity field is optional, but if provided, it must be valid
	if metaSeverityField != "" && !isValidColumnName(metaSeverityField) {
		return &ValidationError{
			Field:   "MetaSeverityField",
			Message: "meta severity field must start with a letter or underscore and contain only letters, numbers, and underscores",
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

// ValidateConnection validates connection parameters for a connection test
func (v *Validator) ValidateConnection(conn models.ConnectionInfo) error {
	// Validate host
	if conn.Host == "" {
		return &ValidationError{
			Field:   "Host",
			Message: "host is required",
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

	// Validate database name
	if conn.Database == "" {
		return &ValidationError{
			Field:   "Database",
			Message: "database is required",
		}
	}

	// Validate database name contains only valid characters
	if !isValidTableName(conn.Database) {
		return &ValidationError{
			Field:   "Database",
			Message: "database name contains invalid characters",
		}
	}

	// Validate table name if provided
	if conn.TableName != "" && !isValidTableName(conn.TableName) {
		return &ValidationError{
			Field:   "TableName",
			Message: "table name contains invalid characters",
		}
	}

	return nil
}

// ValidateColumnTypes validates that the timestamp and severity columns have the correct types
// This is only used when connecting to an existing table
func (v *Validator) ValidateColumnTypes(ctx context.Context, client *clickhouse.Client, database, tableName, tsField, severityField string) error {
	// Ensure we have a valid client
	if client == nil {
		return &ValidationError{
			Field:   "connection",
			Message: "Invalid database client",
		}
	}

	// First check if the timestamp field exists and has the correct type
	tsQuery := fmt.Sprintf(`
		SELECT type
		FROM system.columns
		WHERE database = '%s' AND table = '%s' AND name = '%s'
	`, database, tableName, tsField)

	tsResult, err := client.Query(ctx, tsQuery)
	if err != nil {
		return &ValidationError{
			Field:   "connection",
			Message: fmt.Sprintf("Failed to query timestamp column type: %s", err.Error()),
		}
	}

	if len(tsResult.Logs) == 0 {
		return &ValidationError{
			Field:   "MetaTSField",
			Message: fmt.Sprintf("Timestamp field '%s' not found in table", tsField),
		}
	}

	// Check if timestamp column is DateTime or DateTime64
	if tsType, ok := tsResult.Logs[0]["type"].(string); ok {
		if !strings.HasPrefix(tsType, "DateTime") {
			return &ValidationError{
				Field:   "MetaTSField",
				Message: fmt.Sprintf("Timestamp field '%s' must be of type DateTime or DateTime64, found %s", tsField, tsType),
			}
		}
	} else {
		return &ValidationError{
			Field:   "MetaTSField",
			Message: fmt.Sprintf("Failed to determine type of timestamp field '%s'", tsField),
		}
	}

	// If severity field is provided, check its type
	if severityField != "" {
		sevQuery := fmt.Sprintf(`
			SELECT type
			FROM system.columns
			WHERE database = '%s' AND table = '%s' AND name = '%s'
		`, database, tableName, severityField)

		sevResult, err := client.Query(ctx, sevQuery)
		if err != nil {
			return &ValidationError{
				Field:   "connection",
				Message: fmt.Sprintf("Failed to query severity column type: %s", err.Error()),
			}
		}

		if len(sevResult.Logs) == 0 {
			// Severity field not found, but it's optional so return a validation error with a clear message
			return &ValidationError{
				Field:   "MetaSeverityField",
				Message: fmt.Sprintf("Severity field '%s' not found in table. Please check the field name or leave it empty if not needed.", severityField),
			}
		}

		// Check if severity column is String or LowCardinality(String)
		if sevType, ok := sevResult.Logs[0]["type"].(string); ok {
			if sevType != "String" && !strings.Contains(sevType, "LowCardinality(String)") {
				return &ValidationError{
					Field:   "MetaSeverityField",
					Message: fmt.Sprintf("Severity field '%s' must be of type String or LowCardinality(String), found %s", severityField, sevType),
				}
			}
		} else {
			return &ValidationError{
				Field:   "MetaSeverityField",
				Message: fmt.Sprintf("Failed to determine type of severity field '%s'", severityField),
			}
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

// isValidSourceName checks if the name is valid for use as a source name
// More relaxed than table name validation - allows spaces, hyphens
func isValidSourceName(name string) bool {
	// Check length constraint
	if len(name) == 0 || len(name) > 50 {
		return false
	}

	// Must not be empty and must not have leading/trailing spaces
	if name == "" || name[0] == ' ' || name[len(name)-1] == ' ' {
		return false
	}

	// Check allowed characters
	for _, r := range name {
		if !isAllowedInSourceName(r) {
			return false
		}
	}

	return true
}

// isAllowedInSourceName checks if a character is allowed in source names
func isAllowedInSourceName(r rune) bool {
	return isLetter(r) || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == ' '
}
