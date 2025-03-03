package models

import (
	"fmt"
	"time"
)

// ConnectionInfo represents the connection details for a ClickHouse database
type ConnectionInfo struct {
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Database  string `json:"database"`
	TableName string `json:"table_name"`
}

// Source represents a ClickHouse data source in our system
type Source struct {
	ID                SourceID       `db:"id" json:"id"`
	MetaIsAutoCreated bool           `db:"_meta_is_auto_created" json:"_meta_is_auto_created"`
	MetaTSField       string         `db:"_meta_ts_field" json:"_meta_ts_field"`
	MetaSeverityField string         `db:"_meta_severity_field" json:"_meta_severity_field"`
	Connection        ConnectionInfo `db:"connection" json:"connection"`
	Description       string         `db:"description" json:"description,omitempty"`
	TTLDays           int            `db:"ttl_days" json:"ttl_days"`
	Timestamps
	IsConnected bool         `db:"-" json:"is_connected"`
	Schema      string       `db:"-" json:"schema,omitempty"`
	Columns     []ColumnInfo `db:"-" json:"columns,omitempty"`
}

// ConnectionInfoResponse represents the connection details for API responses, omitting sensitive fields
type ConnectionInfoResponse struct {
	Host      string `json:"host"`
	Database  string `json:"database"`
	TableName string `json:"table_name"`
}

// SourceResponse represents a Source for API responses, with sensitive information removed
type SourceResponse struct {
	ID                SourceID               `json:"id"`
	MetaIsAutoCreated bool                   `json:"_meta_is_auto_created"`
	MetaTSField       string                 `json:"_meta_ts_field"`
	MetaSeverityField string                 `json:"_meta_severity_field"`
	Connection        ConnectionInfoResponse `json:"connection"`
	Description       string                 `json:"description,omitempty"`
	TTLDays           int                    `json:"ttl_days"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
	IsConnected       bool                   `json:"is_connected"`
	Schema            string                 `json:"schema,omitempty"`
	Columns           []ColumnInfo           `json:"columns,omitempty"`
}

// ToResponse converts a Source to a SourceResponse, removing sensitive information
func (s *Source) ToResponse() *SourceResponse {
	return &SourceResponse{
		ID:                s.ID,
		MetaIsAutoCreated: s.MetaIsAutoCreated,
		MetaTSField:       s.MetaTSField,
		MetaSeverityField: s.MetaSeverityField,
		Connection: ConnectionInfoResponse{
			Host:      s.Connection.Host,
			Database:  s.Connection.Database,
			TableName: s.Connection.TableName,
		},
		Description: s.Description,
		TTLDays:     s.TTLDays,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
		IsConnected: s.IsConnected,
		Schema:      s.Schema,
		Columns:     s.Columns,
	}
}

// GetFullTableName returns the fully qualified table name (database.table)
func (s *Source) GetFullTableName() string {
	return fmt.Sprintf("%s.%s", s.Connection.Database, s.Connection.TableName)
}

// SourceHealth represents the health status of a source
type SourceHealth struct {
	SourceID    SourceID     `json:"source_id"`
	Status      HealthStatus `json:"status"`
	Error       string       `json:"error,omitempty"`
	LastChecked time.Time    `json:"last_checked"`
}

// CreateSourceRequest represents a request to create a new data source
type CreateSourceRequest struct {
	MetaIsAutoCreated bool           `json:"meta_is_auto_created"`
	MetaTSField       string         `json:"meta_ts_field"`
	MetaSeverityField string         `json:"meta_severity_field"`
	Connection        ConnectionInfo `json:"connection"`
	Description       string         `json:"description"`
	TTLDays           int            `json:"ttl_days"`
}

// ValidateConnectionRequest represents a request to validate a connection
type ValidateConnectionRequest struct {
	ConnectionInfo
	TimestampField string `json:"timestamp_field"`
	SeverityField  string `json:"severity_field"`
}

// SourceWithTeams represents a source along with the teams that have access to it
type SourceWithTeams struct {
	Source *SourceResponse `json:"source"`
	Teams  []*Team         `json:"teams"`
}

// TeamGroupedQuery represents a query grouped by team
type TeamGroupedQuery struct {
	TeamID   TeamID            `json:"team_id"`
	TeamName string            `json:"team_name"`
	Queries  []*SavedTeamQuery `json:"queries"`
}

// ConnectionValidationResult represents the result of a connection validation
type ConnectionValidationResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
