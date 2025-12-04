// Package models defines the core data structures used throughout LogChef.
// It includes database entity models, API request/response structures, and
// validation rules for ensuring data integrity across the application.
package models

import "time"

// ID types for improved type safety
type (
	// UserID represents a unique user identifier
	UserID int

	// TeamID represents a unique team identifier
	TeamID int

	// SourceID represents a unique data source identifier
	SourceID int

	// SessionID represents a unique session identifier
	SessionID string

	// AlertID represents a unique alert identifier
	AlertID int64
)

// Timestamps provides common timestamp fields used across multiple models
type Timestamps struct {
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// HealthStatus represents the health status of a resource
type HealthStatus string

const (
	// HealthStatusHealthy indicates the resource is healthy
	HealthStatusHealthy HealthStatus = "healthy"

	// HealthStatusUnhealthy indicates the resource is unhealthy
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// QueryStats represents statistics about query execution
type QueryStats struct {
	ExecutionTimeMs float64 `json:"execution_time_ms"`
	RowsRead        int     `json:"rows_read"`
	BytesRead       int     `json:"bytes_read,omitempty"`
}

// ColumnInfo represents column metadata from ClickHouse
type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
