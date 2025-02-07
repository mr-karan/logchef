package models

import (
	"fmt"
	"time"
)

// ConnectionInfo represents the connection details for a Clickhouse database
type ConnectionInfo struct {
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Database  string `json:"database"`
	TableName string `json:"table_name"`
}

// Source represents a Clickhouse data source in our system
type Source struct {
	ID          string         `db:"id" json:"id"`
	SchemaType  string         `db:"schema_type" json:"schema_type"`
	Connection  ConnectionInfo `db:"connection" json:"connection"`
	Description string         `db:"description" json:"description,omitempty"`
	TTLDays     int            `db:"ttl_days" json:"ttl_days"`
	CreatedAt   time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at" json:"updated_at"`
	IsConnected bool           `db:"-" json:"is_connected"`
	Schema      string         `db:"-" json:"schema,omitempty"`
	Columns     []ColumnInfo   `db:"-" json:"columns,omitempty"`
}

const (
	// SchemaTypeManaged represents a managed source with OTEL schema
	SchemaTypeManaged = "managed"
	// SchemaTypeUnmanaged represents an unmanaged source with custom schema
	SchemaTypeUnmanaged = "unmanaged"
)

// GetFullTableName returns the fully qualified table name (database.table)
func (s *Source) GetFullTableName() string {
	return fmt.Sprintf("%s.%s", s.Connection.Database, s.Connection.TableName)
}

// HealthStatus represents the health status of a source
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// SourceHealth represents the health status of a source
type SourceHealth struct {
	SourceID    string       `json:"source_id"`
	Status      HealthStatus `json:"status"`
	Error       string       `json:"error,omitempty"`
	LastChecked time.Time    `json:"last_checked"`
}
