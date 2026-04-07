package clickhouse

import (
	"errors"
	"fmt"
	"regexp"
)

// ValidationError is returned for invalid inputs (field names, timezones).
// Callers can use errors.As to distinguish validation failures from DB errors.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// IsValidationError checks if an error (or any in its chain) is a ValidationError.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

var (
	// validIdentifierRe allows column names like "timestamp", "@timestamp", "log.level", "user-identifier"
	// Also allows backtick-quoted identifiers, dotted paths, and hyphenated names.
	validIdentifierRe = regexp.MustCompile(`^@?[a-zA-Z_][a-zA-Z0-9_.\-]*$`)

	// validTimezoneRe allows IANA timezone names (e.g., "UTC", "Asia/Kolkata", "+05:30")
	validTimezoneRe = regexp.MustCompile(`^[A-Za-z0-9_/+:-]+$`)
)

// ValidateIdentifier checks that a string is a safe SQL identifier (column name, field name).
func ValidateIdentifier(name string) error {
	if name == "" {
		return &ValidationError{Message: "identifier cannot be empty"}
	}
	if len(name) > 128 {
		return &ValidationError{Message: fmt.Sprintf("identifier too long: %d characters", len(name))}
	}
	if !validIdentifierRe.MatchString(name) {
		return &ValidationError{Message: fmt.Sprintf("invalid identifier %q: must contain only alphanumeric, underscore, dot, hyphen, or @ prefix", name)}
	}
	return nil
}

// ValidateTimezone checks that a string is a safe timezone identifier for ClickHouse.
func ValidateTimezone(tz string) error {
	if tz == "" {
		return &ValidationError{Message: "timezone cannot be empty"}
	}
	if len(tz) > 64 {
		return &ValidationError{Message: fmt.Sprintf("timezone too long: %d characters", len(tz))}
	}
	if !validTimezoneRe.MatchString(tz) {
		return &ValidationError{Message: fmt.Sprintf("invalid timezone %q: contains disallowed characters", tz)}
	}
	return nil
}
