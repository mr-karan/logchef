package config

import (
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/pkg/models"
)

// ProvisioningConfig declares the desired state for teams, sources, and access control.
// When absent from config.toml, provisioning is disabled and Logchef operates in UI-only mode.
type ProvisioningConfig struct {
	// File is an optional path to a separate provisioning.toml file.
	// If relative, resolved against the main config.toml directory.
	// When set, the provisioning config is loaded from this file instead of inline.
	File string `koanf:"file"`

	// ManageSources enables declarative management of ClickHouse data sources.
	// When true, sources listed in Sources are created/updated/adopted and marked managed.
	ManageSources bool `koanf:"manage_sources"`

	// ManageTeams enables declarative management of teams, memberships, and source links.
	// When true, teams listed in Teams are created/updated/adopted and marked managed.
	ManageTeams bool `koanf:"manage_teams"`

	// Prune removes managed resources that are no longer declared in config.
	// WARNING: Pruning a team/source cascades to saved queries and alerts via FK constraints.
	// Default: false (safe mode — orphaned managed resources are logged but not deleted).
	Prune bool `koanf:"prune"`

	// DryRun logs all reconciliation actions without applying them.
	// The transaction is rolled back after computing the diff.
	DryRun bool `koanf:"dry_run"`

	// Sources declares datasource definitions to manage.
	// Each source is identified by its Name (must be unique).
	Sources []ProvisionSource `koanf:"sources" json:"sources,omitempty"`

	// Teams declares teams with their memberships and source access.
	// Each team is identified by its Name (must be unique).
	Teams []ProvisionTeam `koanf:"teams" json:"teams,omitempty"`
}

// Enabled returns true if any provisioning management is configured.
func (c *ProvisioningConfig) Enabled() bool {
	return c.ManageSources || c.ManageTeams
}

// ProvisionSource declares a datasource to manage. New configs should use
// source_type + connection.
type ProvisionSource struct {
	// Name is the unique identifier and display name for this source.
	Name string `koanf:"name" json:"name"`

	// SourceType identifies the datasource provider. Defaults to clickhouse.
	SourceType models.SourceType `koanf:"source_type" json:"source_type,omitempty"`

	// Connection stores provider-specific connection details.
	Connection map[string]any `koanf:"connection" json:"connection,omitempty"`

	// SecretRef stores the environment variable or file path that provided the password.
	// Used by the export command to generate round-trippable config (passwords are never exported).
	// If set and Password is empty, the value is resolved from the environment at startup.
	SecretRef string `koanf:"secret_ref" json:"secret_ref,omitempty"`

	Description       string `koanf:"description" json:"description,omitempty"`
	TTLDays           int    `koanf:"ttl_days" json:"ttl_days,omitempty"`
	MetaTSField       string `koanf:"meta_ts_field" json:"meta_ts_field,omitempty"`
	MetaSeverityField string `koanf:"meta_severity_field" json:"meta_severity_field,omitempty"`
}

func (s *ProvisionSource) NormalizedSourceType() models.SourceType {
	return models.NormalizeSourceType(s.SourceType)
}

func (s *ProvisionSource) ClickHouseConnection() (models.ConnectionInfo, error) {
	var conn models.ConnectionInfo
	if len(s.Connection) == 0 {
		return conn, fmt.Errorf("clickhouse source %q requires a connection block", s.Name)
	}

	payload, err := json.Marshal(s.Connection)
	if err != nil {
		return conn, fmt.Errorf("marshal clickhouse connection config: %w", err)
	}
	if err := json.Unmarshal(payload, &conn); err != nil {
		return conn, fmt.Errorf("unmarshal clickhouse connection config: %w", err)
	}
	return conn, nil
}

func (s *ProvisionSource) SetConnectionConfig(connection any) error {
	payload, err := json.Marshal(connection)
	if err != nil {
		return fmt.Errorf("marshal connection config: %w", err)
	}

	var connectionMap map[string]any
	if err := json.Unmarshal(payload, &connectionMap); err != nil {
		return fmt.Errorf("unmarshal connection config into map: %w", err)
	}

	s.Connection = connectionMap
	return nil
}

func (s *ProvisionSource) ConnectionPayload() (json.RawMessage, error) {
	if len(s.Connection) > 0 {
		payload, err := json.Marshal(s.Connection)
		if err != nil {
			return nil, fmt.Errorf("marshal connection config: %w", err)
		}
		return payload, nil
	}

	switch s.NormalizedSourceType() {
	case models.SourceTypeClickHouse:
		conn, err := s.ClickHouseConnection()
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(conn)
		if err != nil {
			return nil, fmt.Errorf("marshal clickhouse connection config: %w", err)
		}
		return payload, nil
	case models.SourceTypeVictoriaLogs:
		return nil, fmt.Errorf("victorialogs source %q requires a connection block", s.Name)
	default:
		return nil, fmt.Errorf("unsupported source type %q", s.SourceType)
	}
}

func (s *ProvisionSource) VictoriaLogsConnection() (models.VictoriaLogsConnectionInfo, error) {
	var conn models.VictoriaLogsConnectionInfo
	payload, err := s.ConnectionPayload()
	if err != nil {
		return conn, err
	}

	if err := json.Unmarshal(payload, &conn); err != nil {
		return conn, fmt.Errorf("unmarshal victorialogs connection config: %w", err)
	}
	return conn, nil
}

// ProvisionTeam declares a team with members and source links.
type ProvisionTeam struct {
	// Name is the unique identifier and display name for this team.
	Name        string `koanf:"name" json:"name"`
	Description string `koanf:"description" json:"description,omitempty"`

	// Sources lists source Names that this team should have access to.
	Sources []string `koanf:"sources" json:"sources,omitempty"`

	// Members declares the team membership with roles.
	Members []ProvisionMember `koanf:"members" json:"members,omitempty"`
}

// ProvisionMember declares a team member by email with a role.
type ProvisionMember struct {
	Email string `koanf:"email" json:"email"`
	// Role is the team-level role: "admin", "editor", or "member".
	Role string `koanf:"role" json:"role,omitempty"`
}
