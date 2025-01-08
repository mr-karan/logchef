package models

import (
	"fmt"
	"strings"
	"time"
)

// Source represents a Clickhouse data source in our system
type Source struct {
	ID          string    `db:"id" json:"id"`
	Name        string    `db:"name" json:"name"`
	TableName   string    `db:"table_name" json:"table_name"` // Format: database.table_name
	SchemaType  string    `db:"schema_type" json:"schema_type"`
	DSN         string    `db:"dsn" json:"dsn"`
	Description string    `db:"description" json:"description,omitempty"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
	IsConnected bool      `db:"-" json:"is_connected"`
}

// GetDatabaseName returns the database part from TableName
func (s *Source) GetDatabaseName() string {
	parts := strings.Split(s.TableName, ".")
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

// GetTableName returns the table part from TableName
func (s *Source) GetTableName() string {
	parts := strings.Split(s.TableName, ".")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

// Validate checks if the source configuration is valid
func (s *Source) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("source ID is required")
	}
	if s.Name == "" {
		return fmt.Errorf("source name is required")
	}
	if s.TableName == "" {
		return fmt.Errorf("table name is required")
	}
	if !strings.Contains(s.TableName, ".") {
		return fmt.Errorf("table name must be in format database.table_name")
	}
	if s.SchemaType == "" {
		return fmt.Errorf("schema type is required")
	}
	if s.DSN == "" {
		return fmt.Errorf("DSN is required")
	}
	return nil
}

// SourceHealth represents the health status of a source
type SourceHealth struct {
	ID        string    `json:"id"`
	IsHealthy bool      `json:"is_healthy"`
	LastCheck time.Time `json:"last_check"`
	Error     string    `json:"error,omitempty"`
}
