package provisioning

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			{
				Name: "src1",
				Connection: map[string]any{
					"host":       "host:9000",
					"database":   "db",
					"table_name": "tbl",
					"password":   "pass",
				},
			},
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

func TestValidateConfig_LegacyFlatSourceFormat(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{
				// Pre-v2.0 flat format: connection fields at the top level,
				// no [sources.connection] block and no source_type.
				Name:      "Production Logs",
				Host:      "clickhouse.internal:9000",
				Database:  "logs",
				TableName: "otel_logs",
			},
		},
	}
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("flat pre-v2.0 source format should fail validation")
	}
	msg := err.Error()
	if !strings.Contains(msg, "flat provisioning format") || !strings.Contains(msg, "[sources.connection]") {
		t.Errorf("error should mention the flat format and [sources.connection]; got: %v", err)
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

func TestValidateConfig_InvalidSQLIdentifiers(t *testing.T) {
	chSource := func(database, table, tsField, sevField string) config.ProvisionSource {
		return config.ProvisionSource{
			Name:              "s",
			SourceType:        "clickhouse",
			MetaTSField:       tsField,
			MetaSeverityField: sevField,
			Connection: map[string]any{
				"host":       "h:9000",
				"database":   database,
				"table_name": table,
				"password":   "p",
			},
		}
	}
	cases := []struct {
		name string
		src  config.ProvisionSource
	}{
		{"bad database", chSource("db; DROP TABLE x", "tbl", "", "")},
		{"bad table", chSource("db", "tbl`)--", "", "")},
		{"bad ts field", chSource("db", "tbl", "ts field", "")},
		{"bad severity field", chSource("db", "tbl", "", "sev-field")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{tc.src}}
			if err := ValidateConfig(cfg); err == nil {
				t.Errorf("%s should fail identifier validation", tc.name)
			}
		})
	}
}

func TestValidateConfig_ClickHouseQuerySettings(t *testing.T) {
	withSettings := func(settings map[string]any) config.ProvisionSource {
		return config.ProvisionSource{
			Name:       "s",
			SourceType: "clickhouse",
			Connection: map[string]any{
				"host":       "h:9000",
				"database":   "db",
				"table_name": "tbl",
				"password":   "p",
				"settings":   settings,
			},
		}
	}
	cases := []struct {
		name     string
		settings map[string]any
		wantErr  bool
	}{
		{"valid settings accepted", map[string]any{"max_result_rows": 1000, "readonly": 2, "result_overflow_mode": "break"}, false},
		{"negative cap rejected", map[string]any{"max_result_rows": -5}, true},
		{"bad readonly rejected", map[string]any{"readonly": 3}, true},
		{"bad overflow mode rejected", map[string]any{"result_overflow_mode": "halt"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{withSettings(tc.settings)}}
			err := ValidateConfig(cfg)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateConfig_DuplicateSourceNames(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{Name: "src1", Connection: map[string]any{"host": "host:9000", "database": "db", "table_name": "tbl1", "password": "pass"}},
			{Name: "src1", Connection: map[string]any{"host": "host:9000", "database": "db", "table_name": "tbl2", "password": "pass"}},
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
			{Name: "", Connection: map[string]any{"host": "host:9000", "database": "db", "table_name": "tbl"}},
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
			{Name: "src1", Connection: map[string]any{"host": "host:9000", "database": "db", "table_name": "tbl", "password": "pass"}},
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
			{Name: "src1", Connection: map[string]any{"host": "host:9000", "database": "db", "table_name": "tbl", "password": "pass"}},
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

func TestValidateConfig_VictoriaLogsSchemaLookbackFromTOML(t *testing.T) {
	dir := t.TempDir()
	mainConfig := `
[auth]
admin_emails = ["admin@example.com"]
api_token_secret = "0123456789abcdef0123456789abcdef"

[oidc]
provider_url = "http://localhost/dex"
auth_url = "http://localhost/dex/auth"
token_url = "http://localhost/dex/token"
client_id = "logchef"
redirect_url = "http://localhost/callback"

[provisioning]
file = "provisioning.toml"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(mainConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		lookback int
		valid    bool
	}{{900, true}, {59, false}, {86401, false}} {
		provisioning := fmt.Sprintf(`
manage_sources = true

[[sources]]
name = "payments"
source_type = "victorialogs"

[sources.connection]
base_url = "https://logs.example.com"

[sources.connection.optimizer]
enabled = true
schema_lookback_seconds = %d
`, tc.lookback)
		if err := os.WriteFile(filepath.Join(dir, "provisioning.toml"), []byte(provisioning), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(filepath.Join(dir, "config.toml"))
		if err != nil {
			t.Fatal(err)
		}
		err = ValidateConfig(&cfg.Provisioning)
		if !tc.valid {
			if err == nil || !strings.Contains(err.Error(), "schema_lookback_seconds") {
				t.Fatalf("schema_lookback_seconds = %d: got %v", tc.lookback, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		conn, err := cfg.Provisioning.Sources[0].VictoriaLogsConnection()
		if err != nil {
			t.Fatal(err)
		}
		if conn.Optimizer == nil || conn.Optimizer.SchemaLookbackSeconds != tc.lookback {
			t.Fatalf("decoded optimizer %+v", conn.Optimizer)
		}
	}
}
