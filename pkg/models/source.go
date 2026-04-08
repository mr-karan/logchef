package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type SourceType string

const (
	SourceTypeClickHouse   SourceType = "clickhouse"
	SourceTypeVictoriaLogs SourceType = "victorialogs"
)

func NormalizeSourceType(sourceType SourceType) SourceType {
	if sourceType == "" {
		return SourceTypeClickHouse
	}
	return sourceType
}

func (t SourceType) Valid() bool {
	switch NormalizeSourceType(t) {
	case SourceTypeClickHouse, SourceTypeVictoriaLogs:
		return true
	default:
		return false
	}
}

func (t SourceType) String() string {
	return string(NormalizeSourceType(t))
}

// ConnectionInfo represents the connection details for a ClickHouse database.
type ConnectionInfo struct {
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Database  string `json:"database"`
	TableName string `json:"table_name"`
}

type VictoriaLogsAuth struct {
	Mode     string `json:"mode,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

type VictoriaLogsTenant struct {
	AccountID string `json:"account_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

type VictoriaLogsScope struct {
	Query string `json:"query,omitempty"`
}

type VictoriaLogsConnectionInfo struct {
	BaseURL string             `json:"base_url"`
	Auth    VictoriaLogsAuth   `json:"auth,omitempty"`
	Tenant  VictoriaLogsTenant `json:"tenant,omitempty"`
	Scope   VictoriaLogsScope  `json:"scope,omitempty"`
	Headers map[string]string  `json:"headers,omitempty"`
	Options map[string]any     `json:"options,omitempty"`
}

// Source represents a datasource in our system.
type Source struct {
	ID                SourceID        `db:"id" json:"id"`
	Name              string          `db:"name" json:"name"`
	MetaIsAutoCreated bool            `db:"_meta_is_auto_created" json:"_meta_is_auto_created"`
	SourceType        SourceType      `db:"source_type" json:"source_type"`
	MetaTSField       string          `db:"_meta_ts_field" json:"_meta_ts_field"`
	MetaSeverityField string          `db:"_meta_severity_field" json:"_meta_severity_field"`
	Connection        ConnectionInfo  `db:"-" json:"connection,omitempty"`
	ConnectionConfig  json.RawMessage `db:"connection_config" json:"-"`
	IdentityKey       string          `db:"identity_key" json:"identity_key,omitempty"`
	Description       string          `db:"description" json:"description,omitempty"`
	TTLDays           int             `db:"ttl_days" json:"ttl_days"`
	Timestamps
	IsConnected bool         `db:"-" json:"is_connected"`
	Schema      string       `db:"-" json:"schema,omitempty"`
	Columns     []ColumnInfo `db:"-" json:"columns,omitempty"`
	// Enhanced schema information
	Engine       string   `db:"-" json:"engine,omitempty"`
	EngineParams []string `db:"-" json:"engine_params,omitempty"`
	SortKeys     []string `db:"-" json:"sort_keys,omitempty"`
	QueryLanguages []QueryLanguage `db:"-" json:"query_languages,omitempty"`
	Capabilities   []string        `db:"-" json:"capabilities,omitempty"`
	// Provisioning
	Managed   bool   `db:"managed" json:"managed"`
	SecretRef string `db:"secret_ref" json:"secret_ref,omitempty"`
}

func BuildClickHouseIdentityKey(conn ConnectionInfo) string {
	host := strings.ToLower(strings.TrimSpace(conn.Host))
	database := strings.ToLower(strings.TrimSpace(conn.Database))
	table := strings.ToLower(strings.TrimSpace(conn.TableName))
	return fmt.Sprintf("%s:%s/%s/%s", SourceTypeClickHouse, host, database, table)
}

func normalizeVictoriaLogsBaseURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return strings.ToLower(strings.TrimRight(trimmed, "/"))
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return strings.TrimRight(parsed.String(), "/")
}

func BuildVictoriaLogsIdentityKey(conn VictoriaLogsConnectionInfo) string {
	return fmt.Sprintf(
		"%s:%s|acct=%s|proj=%s|scope=%s",
		SourceTypeVictoriaLogs,
		normalizeVictoriaLogsBaseURL(conn.BaseURL),
		strings.TrimSpace(conn.Tenant.AccountID),
		strings.TrimSpace(conn.Tenant.ProjectID),
		strings.TrimSpace(conn.Scope.Query),
	)
}

func BuildIdentityKey(sourceType SourceType, connectionConfig json.RawMessage) (string, error) {
	switch NormalizeSourceType(sourceType) {
	case SourceTypeClickHouse:
		var conn ConnectionInfo
		if err := json.Unmarshal(connectionConfig, &conn); err != nil {
			return "", fmt.Errorf("unmarshal clickhouse connection config: %w", err)
		}
		return BuildClickHouseIdentityKey(conn), nil
	case SourceTypeVictoriaLogs:
		var conn VictoriaLogsConnectionInfo
		if err := json.Unmarshal(connectionConfig, &conn); err != nil {
			return "", fmt.Errorf("unmarshal victorialogs connection config: %w", err)
		}
		return BuildVictoriaLogsIdentityKey(conn), nil
	default:
		return "", fmt.Errorf("unsupported source type %q", sourceType)
	}
}

func (s *Source) SyncConnectionConfig() error {
	s.SourceType = NormalizeSourceType(s.SourceType)

	switch s.SourceType {
	case SourceTypeClickHouse:
		payload, err := json.Marshal(s.Connection)
		if err != nil {
			return fmt.Errorf("marshal clickhouse connection config: %w", err)
		}
		s.ConnectionConfig = payload
	case SourceTypeVictoriaLogs:
		if len(s.ConnectionConfig) == 0 {
			return fmt.Errorf("victorialogs sources require connection_config")
		}
	default:
		return fmt.Errorf("unsupported source type %q", s.SourceType)
	}

	identityKey, err := BuildIdentityKey(s.SourceType, s.ConnectionConfig)
	if err != nil {
		return err
	}
	s.IdentityKey = identityKey
	return nil
}

func (s *Source) HydrateConnection() error {
	s.SourceType = NormalizeSourceType(s.SourceType)

	if len(s.ConnectionConfig) == 0 && s.SourceType == SourceTypeClickHouse {
		return s.SyncConnectionConfig()
	}

	switch s.SourceType {
	case SourceTypeClickHouse:
		if err := json.Unmarshal(s.ConnectionConfig, &s.Connection); err != nil {
			return fmt.Errorf("unmarshal clickhouse connection config: %w", err)
		}
		if s.IdentityKey == "" {
			s.IdentityKey = BuildClickHouseIdentityKey(s.Connection)
		}
	case SourceTypeVictoriaLogs:
		if s.IdentityKey == "" {
			identityKey, err := BuildIdentityKey(s.SourceType, s.ConnectionConfig)
			if err != nil {
				return err
			}
			s.IdentityKey = identityKey
		}
	default:
		return fmt.Errorf("unsupported source type %q", s.SourceType)
	}

	return nil
}

func (s *Source) IsClickHouse() bool {
	return NormalizeSourceType(s.SourceType) == SourceTypeClickHouse
}

func (s *Source) IsVictoriaLogs() bool {
	return NormalizeSourceType(s.SourceType) == SourceTypeVictoriaLogs
}

func (s *Source) SupportsQueryLanguage(language QueryLanguage) bool {
	normalized := NormalizeQueryLanguage(language)
	for _, candidate := range s.QueryLanguages {
		if NormalizeQueryLanguage(candidate) == normalized {
			return true
		}
	}
	return false
}

func (s *Source) HasCapability(capability string) bool {
	needle := strings.TrimSpace(capability)
	if needle == "" {
		return false
	}
	for _, candidate := range s.Capabilities {
		if strings.TrimSpace(candidate) == needle {
			return true
		}
	}
	return false
}

func (s *Source) VictoriaLogsConnection() (VictoriaLogsConnectionInfo, error) {
	var conn VictoriaLogsConnectionInfo
	if !s.IsVictoriaLogs() {
		return conn, fmt.Errorf("source %d is not a victorialogs source", s.ID)
	}
	if err := json.Unmarshal(s.ConnectionConfig, &conn); err != nil {
		return conn, fmt.Errorf("unmarshal victorialogs connection config: %w", err)
	}
	return conn, nil
}

func (s *Source) RedactedConnectionConfig() json.RawMessage {
	switch NormalizeSourceType(s.SourceType) {
	case SourceTypeClickHouse:
		payload, err := json.Marshal(ConnectionInfoResponse{
			Host:      s.Connection.Host,
			Username:  s.Connection.Username,
			Password:  s.Connection.Password,
			Database:  s.Connection.Database,
			TableName: s.Connection.TableName,
		})
		if err != nil {
			return json.RawMessage(`{}`)
		}
		return payload
	case SourceTypeVictoriaLogs:
		conn, err := s.VictoriaLogsConnection()
		if err != nil {
			return json.RawMessage(`{}`)
		}
		conn.Auth.Password = ""
		conn.Auth.Token = ""
		payload, err := json.Marshal(conn)
		if err != nil {
			return json.RawMessage(`{}`)
		}
		return payload
	default:
		return json.RawMessage(`{}`)
	}
}

// SourceResponse represents a Source for API responses, with sensitive information removed.
type SourceResponse struct {
	ID                SourceID        `json:"id"`
	Name              string          `json:"name"`
	MetaIsAutoCreated bool            `json:"_meta_is_auto_created"`
	SourceType        SourceType      `json:"source_type"`
	MetaTSField       string          `json:"_meta_ts_field"`
	MetaSeverityField string          `json:"_meta_severity_field"`
	Connection        json.RawMessage `json:"connection"`
	IdentityKey       string          `json:"identity_key,omitempty"`
	Description       string          `json:"description,omitempty"`
	TTLDays           int             `json:"ttl_days"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	IsConnected       bool            `json:"is_connected"`
	Schema            string          `json:"schema,omitempty"`
	Columns           []ColumnInfo    `json:"columns,omitempty"`
	// Enhanced schema information
	Engine       string   `json:"engine,omitempty"`
	EngineParams []string `json:"engine_params,omitempty"`
	SortKeys     []string `json:"sort_keys,omitempty"`
	QueryLanguages []QueryLanguage `json:"query_languages,omitempty"`
	Capabilities   []string        `json:"capabilities,omitempty"`
}

// ToResponse converts a Source to a SourceResponse, removing sensitive information.
func (s *Source) ToResponse() *SourceResponse {
	return &SourceResponse{
		ID:                s.ID,
		Name:              s.Name,
		MetaIsAutoCreated: s.MetaIsAutoCreated,
		SourceType:        NormalizeSourceType(s.SourceType),
		MetaTSField:       s.MetaTSField,
		MetaSeverityField: s.MetaSeverityField,
		Connection:        s.RedactedConnectionConfig(),
		IdentityKey:       s.IdentityKey,
		Description:       s.Description,
		TTLDays:           s.TTLDays,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
		IsConnected:       s.IsConnected,
		Schema:            s.Schema,
		Columns:           s.Columns,
		Engine:            s.Engine,
		EngineParams:      s.EngineParams,
		SortKeys:          s.SortKeys,
		QueryLanguages:    s.QueryLanguages,
		Capabilities:      s.Capabilities,
	}
}

// GetFullTableName returns the fully qualified table name (database.table).
func (s *Source) GetFullTableName() string {
	return fmt.Sprintf("%s.%s", s.Connection.Database, s.Connection.TableName)
}

// SourceHealth represents the health status of a source.
type SourceHealth struct {
	SourceID    SourceID     `json:"source_id"`
	Status      HealthStatus `json:"status"`
	Error       string       `json:"error,omitempty"`
	LastChecked time.Time    `json:"last_checked"`
}

// CreateSourceRequest represents a request to create a new data source.
type CreateSourceRequest struct {
	Name              string          `json:"name"`
	MetaIsAutoCreated bool            `json:"meta_is_auto_created"`
	SourceType        SourceType      `json:"source_type"`
	MetaTSField       string          `json:"meta_ts_field"`
	MetaSeverityField string          `json:"meta_severity_field"`
	Connection        json.RawMessage `json:"connection"`
	Description       string          `json:"description"`
	TTLDays           int             `json:"ttl_days"`
	Schema            string          `json:"schema,omitempty"`
}

// ValidateConnectionRequest represents a request to validate a connection.
type ValidateConnectionRequest struct {
	SourceType     SourceType      `json:"source_type"`
	Connection     json.RawMessage `json:"connection"`
	TimestampField string          `json:"timestamp_field"`
	SeverityField  string          `json:"severity_field"`
}

// UpdateSourceRequest represents a request to update a data source.
// All fields are pointers to allow partial updates - nil means "don't change".
type UpdateSourceRequest struct {
	Name              *string         `json:"name,omitempty"`
	Description       *string         `json:"description,omitempty"`
	TTLDays           *int            `json:"ttl_days,omitempty"`
	MetaTSField       *string         `json:"meta_ts_field,omitempty"`
	MetaSeverityField *string         `json:"meta_severity_field,omitempty"`
	Connection        json.RawMessage `json:"connection,omitempty"`
}

// HasConnectionChanges returns true if any connection-related fields are being updated.
// When connection changes, re-validation is required.
func (r *UpdateSourceRequest) HasConnectionChanges() bool {
	return len(bytes.TrimSpace(r.Connection)) > 0
}

// SourceWithTeams represents a source along with the teams that have access to it.
type SourceWithTeams struct {
	Source *SourceResponse `json:"source"`
	Teams  []*Team         `json:"teams"`
}

// TeamGroupedQuery represents a query grouped by team.
type TeamGroupedQuery struct {
	TeamID   TeamID            `json:"team_id"`
	TeamName string            `json:"team_name"`
	Queries  []*SavedTeamQuery `json:"queries"`
}

// ConnectionValidationResult represents the result of a connection validation.
type ConnectionValidationResult struct {
	Message string `json:"message"`
}

// ConnectionInfoResponse represents the connection details for API responses.
type ConnectionInfoResponse struct {
	Host      string `json:"host"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Database  string `json:"database"`
	TableName string `json:"table_name"`
}
