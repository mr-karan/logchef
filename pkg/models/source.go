package models

import (
	"fmt"
	"time"
)

type BackendType string

const (
	BackendClickHouse   BackendType = "clickhouse"
	BackendVictoriaLogs BackendType = "victorialogs"
)

func (b BackendType) String() string {
	return string(b)
}

func (b BackendType) IsValid() bool {
	switch b {
	case BackendClickHouse, BackendVictoriaLogs:
		return true
	default:
		return false
	}
}

type ConnectionInfo struct {
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Database  string `json:"database"`
	TableName string `json:"table_name"`
}

type VictoriaLogsConnectionInfo struct {
	URL          string            `json:"url"`
	AccountID    string            `json:"account_id,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	StreamLabels map[string]string `json:"stream_labels,omitempty"`
}

type Source struct {
	ID                SourceID    `db:"id" json:"id"`
	Name              string      `db:"name" json:"name"`
	BackendType       BackendType `db:"backend_type" json:"backend_type"`
	MetaIsAutoCreated bool        `db:"_meta_is_auto_created" json:"_meta_is_auto_created"`
	MetaTSField       string      `db:"_meta_ts_field" json:"_meta_ts_field"`
	MetaSeverityField string      `db:"_meta_severity_field" json:"_meta_severity_field"`
	Description       string      `db:"description" json:"description,omitempty"`
	TTLDays           int         `db:"ttl_days" json:"ttl_days"`

	Connection             ConnectionInfo              `db:"connection" json:"connection"`
	VictoriaLogsConnection *VictoriaLogsConnectionInfo `db:"victorialogs_connection" json:"victorialogs_connection,omitempty"`

	Timestamps
	IsConnected  bool         `db:"-" json:"is_connected"`
	Schema       string       `db:"-" json:"schema,omitempty"`
	Columns      []ColumnInfo `db:"-" json:"columns,omitempty"`
	Engine       string       `db:"-" json:"engine,omitempty"`
	EngineParams []string     `db:"-" json:"engine_params,omitempty"`
	SortKeys     []string     `db:"-" json:"sort_keys,omitempty"`
}

type ConnectionInfoResponse struct {
	Host      string `json:"host"`
	Database  string `json:"database"`
	TableName string `json:"table_name"`
}

type VictoriaLogsConnectionInfoResponse struct {
	URL          string            `json:"url"`
	AccountID    string            `json:"account_id,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	StreamLabels map[string]string `json:"stream_labels,omitempty"`
}

type SourceResponse struct {
	ID                     SourceID                            `json:"id"`
	Name                   string                              `json:"name"`
	BackendType            BackendType                         `json:"backend_type"`
	MetaIsAutoCreated      bool                                `json:"_meta_is_auto_created"`
	MetaTSField            string                              `json:"_meta_ts_field"`
	MetaSeverityField      string                              `json:"_meta_severity_field"`
	Connection             ConnectionInfoResponse              `json:"connection"`
	VictoriaLogsConnection *VictoriaLogsConnectionInfoResponse `json:"victorialogs_connection,omitempty"`
	Description            string                              `json:"description,omitempty"`
	TTLDays                int                                 `json:"ttl_days"`
	CreatedAt              time.Time                           `json:"created_at"`
	UpdatedAt              time.Time                           `json:"updated_at"`
	IsConnected            bool                                `json:"is_connected"`
	Schema                 string                              `json:"schema,omitempty"`
	Columns                []ColumnInfo                        `json:"columns,omitempty"`
	Engine                 string                              `json:"engine,omitempty"`
	EngineParams           []string                            `json:"engine_params,omitempty"`
	SortKeys               []string                            `json:"sort_keys,omitempty"`
}

func (s *Source) ToResponse() *SourceResponse {
	resp := &SourceResponse{
		ID:                s.ID,
		Name:              s.Name,
		BackendType:       s.BackendType,
		MetaIsAutoCreated: s.MetaIsAutoCreated,
		MetaTSField:       s.MetaTSField,
		MetaSeverityField: s.MetaSeverityField,
		Connection: ConnectionInfoResponse{
			Host:      s.Connection.Host,
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

	if s.VictoriaLogsConnection != nil {
		resp.VictoriaLogsConnection = &VictoriaLogsConnectionInfoResponse{
			URL:          s.VictoriaLogsConnection.URL,
			AccountID:    s.VictoriaLogsConnection.AccountID,
			ProjectID:    s.VictoriaLogsConnection.ProjectID,
			StreamLabels: s.VictoriaLogsConnection.StreamLabels,
		}
	}

	return resp
}

func (s *Source) GetFullTableName() string {
	return fmt.Sprintf("%s.%s", s.Connection.Database, s.Connection.TableName)
}

func (s *Source) GetEffectiveBackendType() BackendType {
	if s.BackendType == "" {
		return BackendClickHouse
	}
	return s.BackendType
}

func (s *Source) IsClickHouse() bool {
	return s.GetEffectiveBackendType() == BackendClickHouse
}

func (s *Source) IsVictoriaLogs() bool {
	return s.GetEffectiveBackendType() == BackendVictoriaLogs
}

// SourceHealth represents the health status of a source
type SourceHealth struct {
	SourceID    SourceID     `json:"source_id"`
	Status      HealthStatus `json:"status"`
	Error       string       `json:"error,omitempty"`
	LastChecked time.Time    `json:"last_checked"`
}

type CreateSourceRequest struct {
	Name                   string                      `json:"name"`
	BackendType            BackendType                 `json:"backend_type"`
	MetaIsAutoCreated      bool                        `json:"meta_is_auto_created"`
	MetaTSField            string                      `json:"meta_ts_field"`
	MetaSeverityField      string                      `json:"meta_severity_field"`
	Connection             ConnectionInfo              `json:"connection"`
	VictoriaLogsConnection *VictoriaLogsConnectionInfo `json:"victorialogs_connection,omitempty"`
	Description            string                      `json:"description"`
	TTLDays                int                         `json:"ttl_days"`
	Schema                 string                      `json:"schema,omitempty"`
}

type ValidateConnectionRequest struct {
	BackendType            BackendType                 `json:"backend_type"`
	ConnectionInfo         ConnectionInfo              `json:"connection"`
	VictoriaLogsConnection *VictoriaLogsConnectionInfo `json:"victorialogs_connection,omitempty"`
	TimestampField         string                      `json:"timestamp_field"`
	SeverityField          string                      `json:"severity_field"`
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
