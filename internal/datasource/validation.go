package datasource

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/mr-karan/logchef/internal/clickhouse"
)

var ErrSourceAlreadyExists = errors.New("source already exists")

// ValidationError represents a provider validation error.
type ValidationError struct {
	Field   string
	Message string
	Err     error
}

func (e *ValidationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Field, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func IsValidationError(err error) bool {
	var validationErr *ValidationError
	return errors.As(err, &validationErr) || clickhouse.IsValidationError(err)
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isAlphanumericOrUnderscore(r rune) bool {
	return isLetter(r) || (r >= '0' && r <= '9') || r == '_'
}

func IsValidIdentifier(name string) bool {
	if len(name) == 0 {
		return false
	}

	first := rune(name[0])
	if !isLetter(first) && first != '_' {
		return false
	}

	for _, r := range name {
		if !isAlphanumericOrUnderscore(r) {
			return false
		}
	}

	return true
}

func isValidSourceName(name string) bool {
	if len(name) == 0 || len(name) > 50 {
		return false
	}
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return false
	}

	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-' && r != ' ' {
			return false
		}
	}

	return true
}

func ValidateCommonSourceFields(name, description string, ttlDays int) error {
	if name == "" {
		return &ValidationError{Field: "name", Message: "source name is required"}
	}
	if !isValidSourceName(name) {
		return &ValidationError{Field: "name", Message: "source name must not exceed 50 characters and can only contain letters, numbers, spaces, hyphens, and underscores"}
	}
	if len(description) > 500 {
		return &ValidationError{Field: "description", Message: "description must not exceed 500 characters"}
	}
	if ttlDays < -1 {
		return &ValidationError{Field: "ttl_days", Message: "TTL days must be -1 (no TTL) or a non-negative number"}
	}
	return nil
}

func validateColumnName(field, name string) error {
	if strings.TrimSpace(name) == "" {
		return &ValidationError{Field: field, Message: "field is required"}
	}
	if !IsValidIdentifier(name) {
		return &ValidationError{Field: field, Message: "field contains invalid characters"}
	}
	return nil
}

func validateOptionalColumnName(field, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	if !IsValidIdentifier(name) {
		return &ValidationError{Field: field, Message: "field contains invalid characters"}
	}
	return nil
}

func validateClickHouseConnection(connFieldPrefix string, requireTable bool, connHost, connUsername, connPassword, connDatabase, connTable string) error {
	if strings.TrimSpace(connHost) == "" {
		return &ValidationError{Field: connFieldPrefix + "host", Message: "host is required"}
	}

	_, portStr, err := net.SplitHostPort(connHost)
	if err != nil {
		if !strings.Contains(err.Error(), "missing port in address") {
			return &ValidationError{Field: connFieldPrefix + "host", Message: "invalid host format", Err: err}
		}
	} else {
		port, convErr := strconv.Atoi(portStr)
		if convErr != nil || port <= 0 || port > 65535 {
			return &ValidationError{Field: connFieldPrefix + "host", Message: "port must be between 1 and 65535"}
		}
	}

	if strings.TrimSpace(connDatabase) == "" {
		return &ValidationError{Field: connFieldPrefix + "database", Message: "database is required"}
	}
	if !IsValidIdentifier(connDatabase) {
		return &ValidationError{Field: connFieldPrefix + "database", Message: "database name contains invalid characters"}
	}

	if requireTable && strings.TrimSpace(connTable) == "" {
		return &ValidationError{Field: connFieldPrefix + "table_name", Message: "table name is required"}
	}
	if strings.TrimSpace(connTable) != "" && !IsValidIdentifier(connTable) {
		return &ValidationError{Field: connFieldPrefix + "table_name", Message: "table name contains invalid characters"}
	}

	return nil
}

func ValidateVictoriaLogsConnection(connFieldPrefix string, baseURL string) error {
	if strings.TrimSpace(baseURL) == "" {
		return &ValidationError{Field: connFieldPrefix + "base_url", Message: "base_url is required"}
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return &ValidationError{Field: connFieldPrefix + "base_url", Message: "base_url must be a valid URL", Err: err}
	}
	return nil
}
