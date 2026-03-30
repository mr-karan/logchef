package clickhouse

import (
	"fmt"
	"regexp"
)

var (
	// validIdentifierRe allows column names like "timestamp", "@timestamp", "log.level"
	// Also allows backtick-quoted identifiers and dotted paths for nested fields.
	validIdentifierRe = regexp.MustCompile(`^@?[a-zA-Z_][a-zA-Z0-9_.]*$`)

	// validTimezoneRe allows IANA timezone names (e.g., "UTC", "Asia/Kolkata", "+05:30")
	validTimezoneRe = regexp.MustCompile(`^[A-Za-z0-9_/+:-]+$`)
)

// ValidateIdentifier checks that a string is a safe SQL identifier (column name, field name).
func ValidateIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("identifier cannot be empty")
	}
	if len(name) > 128 {
		return fmt.Errorf("identifier too long: %d characters", len(name))
	}
	if !validIdentifierRe.MatchString(name) {
		return fmt.Errorf("invalid identifier %q: must contain only alphanumeric, underscore, dot, or @ prefix", name)
	}
	return nil
}

// ValidateTimezone checks that a string is a safe timezone identifier for ClickHouse.
func ValidateTimezone(tz string) error {
	if tz == "" {
		return fmt.Errorf("timezone cannot be empty")
	}
	if len(tz) > 64 {
		return fmt.Errorf("timezone too long: %d characters", len(tz))
	}
	if !validTimezoneRe.MatchString(tz) {
		return fmt.Errorf("invalid timezone %q: contains disallowed characters", tz)
	}
	return nil
}
