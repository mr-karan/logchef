package provisioning

import (
	"testing"

	"github.com/mr-karan/logchef/internal/config"
)

func TestValidateConfig_Empty(t *testing.T) {
	cfg := &config.ProvisioningConfig{}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("empty config should be valid: %v", err)
	}
}

func TestValidateConfig_ValidSources(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{Name: "src1", Host: "host:9000", Database: "db", TableName: "tbl", Password: "pass"},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("valid source config should pass: %v", err)
	}
}

func TestValidateConfig_ValidSourceWithNestedClickHouseConnection(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{
				Name:       "src1",
				SourceType: "clickhouse",
				Connection: map[string]any{
					"host":       "host:9000",
					"database":   "db",
					"table_name": "tbl",
					"username":   "default",
					"password":   "pass",
				},
			},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("valid nested clickhouse source config should pass: %v", err)
	}
}

func TestValidateConfig_ValidVictoriaLogsSource(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{
				Name:       "payments",
				SourceType: "victorialogs",
				Connection: map[string]any{
					"base_url": "https://logs.example.com",
					"auth": map[string]any{
						"mode":  "bearer",
						"token": "secret",
					},
					"tenant": map[string]any{
						"account_id": "12",
						"project_id": "34",
					},
				},
			},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("valid victorialogs source config should pass: %v", err)
	}
}

func TestValidateConfig_DuplicateSourceNames(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{Name: "src1", Host: "host:9000", Database: "db", TableName: "tbl1", Password: "pass"},
			{Name: "src1", Host: "host:9000", Database: "db", TableName: "tbl2", Password: "pass"},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("duplicate source names should fail validation")
	}
}

func TestValidateConfig_MissingSourceFields(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{Name: "src1"},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("source missing host/database/table should fail")
	}
}

func TestValidateConfig_EmptySourceName(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{Name: "", Host: "host:9000", Database: "db", TableName: "tbl"},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("empty source name should fail")
	}
}

func TestValidateConfig_ValidTeams(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		ManageTeams:   true,
		Sources: []config.ProvisionSource{
			{Name: "src1", Host: "host:9000", Database: "db", TableName: "tbl", Password: "pass"},
		},
		Teams: []config.ProvisionTeam{
			{
				Name:    "team1",
				Sources: []string{"src1"},
				Members: []config.ProvisionMember{
					{Email: "alice@co", Role: "admin"},
					{Email: "bob@co", Role: "member"},
				},
			},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("valid team config should pass: %v", err)
	}
}

func TestValidateConfig_DuplicateTeamNames(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{
			{Name: "team1"},
			{Name: "team1"},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("duplicate team names should fail validation")
	}
}

func TestValidateConfig_InvalidSourceRef(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		ManageTeams:   true,
		Sources: []config.ProvisionSource{
			{Name: "src1", Host: "host:9000", Database: "db", TableName: "tbl", Password: "pass"},
		},
		Teams: []config.ProvisionTeam{
			{Name: "team1", Sources: []string{"nonexistent"}},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("team referencing nonexistent source should fail")
	}
}

func TestValidateConfig_InvalidMemberRole(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{
			{
				Name: "team1",
				Members: []config.ProvisionMember{
					{Email: "alice@co", Role: "superadmin"},
				},
			},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("invalid member role should fail validation")
	}
}

func TestValidateConfig_ValidEditorRole(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{
			{
				Name: "team1",
				Members: []config.ProvisionMember{
					{Email: "alice@co", Role: "editor"},
				},
			},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("editor role should be valid: %v", err)
	}
}

func TestValidateConfig_DuplicateMemberEmails(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{
			{
				Name: "team1",
				Members: []config.ProvisionMember{
					{Email: "alice@co", Role: "admin"},
					{Email: "alice@co", Role: "member"},
				},
			},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("duplicate member emails should fail validation")
	}
}

func TestValidateConfig_EmptyTeamName(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{
			{Name: ""},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("empty team name should fail")
	}
}

func TestValidateConfig_EmptyMemberEmail(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{
			{
				Name: "team1",
				Members: []config.ProvisionMember{
					{Email: "", Role: "member"},
				},
			},
		},
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("empty member email should fail")
	}
}

func TestValidateConfig_DefaultMemberRole(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{
			{
				Name: "team1",
				Members: []config.ProvisionMember{
					{Email: "alice@co", Role: ""},
				},
			},
		},
	}
	// Empty role defaults to "member" — should pass
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("empty role (defaults to member) should pass: %v", err)
	}
}

func TestValidateConfig_SourcesNotManagedTeamRefsIgnored(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: false, // sources not managed
		ManageTeams:   true,
		Teams: []config.ProvisionTeam{
			{Name: "team1", Sources: []string{"anything"}},
		},
	}
	// When manage_sources=false, source refs in teams aren't validated against config
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("source refs should be ignored when manage_sources=false: %v", err)
	}
}

func TestResolveSecrets_DefaultMetaTSField(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{Name: "src1", MetaTSField: ""},
		},
	}
	ResolveSecrets(cfg)
	if cfg.Sources[0].MetaTSField != "timestamp" {
		t.Errorf("expected default MetaTSField 'timestamp', got %q", cfg.Sources[0].MetaTSField)
	}
}

func TestResolveSecrets_DefaultVictoriaLogsMetaTSField(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{
				Name:       "payments",
				SourceType: "victorialogs",
				Connection: map[string]any{
					"base_url": "https://logs.example.com",
				},
			},
		},
	}
	ResolveSecrets(cfg)
	if cfg.Sources[0].MetaTSField != "_time" {
		t.Errorf("expected default MetaTSField '_time', got %q", cfg.Sources[0].MetaTSField)
	}
}

func TestResolveSecrets_DefaultMemberRole(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageTeams: true,
		Teams: []config.ProvisionTeam{
			{
				Name: "team1",
				Members: []config.ProvisionMember{
					{Email: "alice@co", Role: ""},
				},
			},
		},
	}
	ResolveSecrets(cfg)
	if cfg.Teams[0].Members[0].Role != "member" {
		t.Errorf("expected default role 'member', got %q", cfg.Teams[0].Members[0].Role)
	}
}
