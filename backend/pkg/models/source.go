package models

import (
	"fmt"
	"strings"
	"time"
)

// Source represents a Clickhouse data source in our system
type Source struct {
	ID          string    `db:"id" json:"id"`
	TableName   string    `db:"table_name" json:"table_name"`
	SchemaType  string    `db:"schema_type" json:"schema_type"`
	DSN         string    `db:"dsn" json:"dsn"`
	Description string    `db:"description" json:"description,omitempty"`
	TTLDays     int       `db:"ttl_days" json:"ttl_days"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
	IsConnected bool      `db:"-" json:"is_connected"`
}

// GetDatabaseName returns the database part from DSN
func (s *Source) GetDatabaseName() string {
	// DSN format: tcp://[username:password@]host:port?database=dbname
	if s.DSN == "" {
		return ""
	}

	// Find the database parameter in the query string
	if idx := strings.Index(s.DSN, "?database="); idx >= 0 {
		dbPart := s.DSN[idx+len("?database="):]
		// If there are other query parameters, stop at &
		if andIdx := strings.Index(dbPart, "&"); andIdx >= 0 {
			return dbPart[:andIdx]
		}
		return dbPart
	}

	return ""
}

// Validate checks if the source configuration is valid
func (s *Source) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("source ID is required")
	}
	if s.TableName == "" {
		return fmt.Errorf("table name is required")
	}
	if !isValidTableName(s.TableName) {
		return fmt.Errorf("table name must start with a letter and contain only letters, numbers, and underscores")
	}
	if s.SchemaType == "" {
		return fmt.Errorf("schema type is required")
	}
	if s.DSN == "" {
		return fmt.Errorf("DSN is required")
	}
	if s.TTLDays < -1 {
		return fmt.Errorf("TTL days must be -1 (no TTL) or a positive number")
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

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isAlphanumericOrUnderscore(r rune) bool {
	return isLetter(r) || (r >= '0' && r <= '9') || r == '_'
}

// SourceHealth represents the health status of a source
type SourceHealth struct {
	ID        string    `json:"id"`
	IsHealthy bool      `json:"is_healthy"`
	LastCheck time.Time `json:"last_check"`
	Error     string    `json:"error,omitempty"`
}
