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
	Name              string         `db:"name" json:"name"`
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
	// Enhanced schema information
	Engine       string   `db:"-" json:"engine,omitempty"`
	EngineParams []string `db:"-" json:"engine_params,omitempty"`
	SortKeys     []string `db:"-" json:"sort_keys,omitempty"`
}

// ConnectionInfoResponse represents the connection details for API responses
type ConnectionInfoResponse struct {
	Host      string `json:"host"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Database  string `json:"database"`
	TableName string `json:"table_name"`
}

// SourceResponse represents a Source for API responses, with sensitive information removed
type SourceResponse struct {
	ID                SourceID               `json:"id"`
	Name              string                 `json:"name"`
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
	// Enhanced schema information
	Engine       string   `json:"engine,omitempty"`
	EngineParams []string `json:"engine_params,omitempty"`
	SortKeys     []string `json:"sort_keys,omitempty"`
}

// ToResponse converts a Source to a SourceResponse, removing sensitive information
func (s *Source) ToResponse() *SourceResponse {
	return &SourceResponse{
		ID:                s.ID,
		Name:              s.Name,
		MetaIsAutoCreated: s.MetaIsAutoCreated,
		MetaTSField:       s.MetaTSField,
		MetaSeverityField: s.MetaSeverityField,
		Connection: ConnectionInfoResponse{
			Host:      s.Connection.Host,
			Username:  s.Connection.Username,
			Password:  s.Connection.Password,
			Database:  s.Connection.Database,
			TableName: s.Connection.TableName,
		},
		Description:  s.Description,
		TTLDays:      s.TTLDays,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		IsConnected:  s.IsConnected,
		Schema:       s.Schema,
		Columns:      s.Columns,
		Engine:       s.Engine,
		EngineParams: s.EngineParams,
		SortKeys:     s.SortKeys,
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
	Name              string         `json:"name"`
	MetaIsAutoCreated bool           `json:"meta_is_auto_created"`
	MetaTSField       string         `json:"meta_ts_field"`
	MetaSeverityField string         `json:"meta_severity_field"`
	Connection        ConnectionInfo `json:"connection"`
	Description       string         `json:"description"`
	TTLDays           int            `json:"ttl_days"`
	Schema            string         `json:"schema,omitempty"`
}

// ValidateConnectionRequest represents a request to validate a connection
type ValidateConnectionRequest struct {
	ConnectionInfo
	TimestampField string `json:"timestamp_field"`
	SeverityField  string `json:"severity_field"`
}

// UpdateSourceRequest represents a request to update a data source.
// All fields are pointers to allow partial updates - nil means "don't change".
type UpdateSourceRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	TTLDays     *int    `json:"ttl_days,omitempty"`
	Host        *string `json:"host,omitempty"`
	Username    *string `json:"username,omitempty"`
	Password    *string `json:"password,omitempty"`
	Database    *string `json:"database,omitempty"`
	TableName   *string `json:"table_name,omitempty"`
}

// HasConnectionChanges returns true if any connection-related fields are being updated.
// When connection changes, re-validation is required.
func (r *UpdateSourceRequest) HasConnectionChanges() bool {
	return r.Host != nil || r.Username != nil || r.Password != nil || r.Database != nil || r.TableName != nil
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
	Message string `json:"message"`
}
