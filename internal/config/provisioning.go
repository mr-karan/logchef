package config

// ProvisioningConfig declares the desired state for teams, sources, and access control.
// When absent from config.toml, provisioning is disabled and LogChef operates in UI-only mode.
type ProvisioningConfig struct {
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

	// Sources declares ClickHouse data sources to manage.
	// Each source is identified by its Name (must be unique).
	Sources []ProvisionSource `koanf:"sources"`

	// Teams declares teams with their memberships and source access.
	// Each team is identified by its Name (must be unique).
	Teams []ProvisionTeam `koanf:"teams"`
}

// Enabled returns true if any provisioning management is configured.
func (c *ProvisioningConfig) Enabled() bool {
	return c.ManageSources || c.ManageTeams
}

// ProvisionSource declares a ClickHouse data source.
type ProvisionSource struct {
	// Name is the unique identifier and display name for this source.
	Name string `koanf:"name"`

	// ClickHouse connection details.
	Host     string `koanf:"host"`
	Username string `koanf:"username"`
	Password string `koanf:"password"`
	Database string `koanf:"database"`
	TableName string `koanf:"table_name"`

	// SecretRef stores the environment variable or file path that provided the password.
	// Used by the export command to generate round-trippable config (passwords are never exported).
	// If set and Password is empty, the value is resolved from the environment at startup.
	SecretRef string `koanf:"secret_ref"`

	Description       string `koanf:"description"`
	TTLDays           int    `koanf:"ttl_days"`
	MetaTSField       string `koanf:"meta_ts_field"`
	MetaSeverityField string `koanf:"meta_severity_field"`
}

// ResolvedPassword returns the password, resolving from SecretRef env var if needed.
func (s *ProvisionSource) ResolvedPassword() string {
	if s.Password != "" {
		return s.Password
	}
	return "" // caller must resolve from env using SecretRef
}

// ProvisionTeam declares a team with members and source links.
type ProvisionTeam struct {
	// Name is the unique identifier and display name for this team.
	Name        string `koanf:"name"`
	Description string `koanf:"description"`

	// Sources lists source Names that this team should have access to.
	Sources []string `koanf:"sources"`

	// Members declares the team membership with roles.
	Members []ProvisionMember `koanf:"members"`
}

// ProvisionMember declares a team member by email with a role.
type ProvisionMember struct {
	Email string `koanf:"email"`
	// Role is the team-level role: "admin", "editor", or "member".
	Role string `koanf:"role"`
}
