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

func lokiProvisionSource(name, principal string, auth map[string]any) config.ProvisionSource {
	return config.ProvisionSource{
		Name:       name,
		SourceType: "loki",
		Connection: map[string]any{
			"base_url":  "http://127.0.0.1:3100",
			"tenant_id": "dev",
			"selector":  `{service_name=~".+"}`,
			"principal": principal,
			"auth":      auth,
		},
	}
}

// The RFC 0011 example, read through config.Load so the TOML decoding of the
// selector quotes and the inline auth table is covered too.
func TestValidateConfig_ValidLokiSourceFromTOML(t *testing.T) {
	t.Setenv("LOGCHEF_SOURCE_LOKI_DEMO_TOKEN", "tok-123")
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
	provisioningTOML := `
manage_sources = true

[[sources]]
name = "Loki Demo"
source_type = "loki"
secret_ref = "LOGCHEF_SOURCE_LOKI_DEMO_TOKEN"

[sources.connection]
base_url = "http://127.0.0.1:3100"
tenant_id = "dev"
selector = '{service_name=~".+"}'
parser = "json"
principal = "payments-readonly"
auth = { mode = "bearer" }
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(mainConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provisioning.toml"), []byte(provisioningTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ResolveConfig(&cfg.Provisioning)
	if err := ValidateConfig(&cfg.Provisioning); err != nil {
		t.Fatalf("valid loki source should pass: %v", err)
	}

	src := cfg.Provisioning.Sources[0]
	conn, err := src.LokiConnection()
	if err != nil {
		t.Fatal(err)
	}
	if conn.Selector != `{service_name=~".+"}` || conn.Principal != "payments-readonly" || conn.Parser != "json" || conn.TenantID != "dev" {
		t.Errorf("decoded connection %+v", conn)
	}
	if conn.Auth.Token != "tok-123" {
		t.Errorf("secret_ref should fill auth.token in bearer mode, got %q", conn.Auth.Token)
	}
	if conn.Auth.Password != "" {
		t.Errorf("secret_ref must not fill auth.password in bearer mode, got %q", conn.Auth.Password)
	}
	if src.MetaTSField != "_time" || src.MetaSeverityField != "detected_level" {
		t.Errorf("defaults: meta_ts_field=%q meta_severity_field=%q, want _time and detected_level", src.MetaTSField, src.MetaSeverityField)
	}
}

func TestResolveConfig_LokiSecretRefBasicFillsPassword(t *testing.T) {
	t.Setenv("LOKI_GATEWAY_PASSWORD", "pw-456")
	src := lokiProvisionSource("loki", "", map[string]any{"mode": "basic", "username": "reader"})
	src.SecretRef = "LOKI_GATEWAY_PASSWORD"
	cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{src}}
	ResolveConfig(cfg)
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("basic auth with secret_ref should pass: %v", err)
	}
	conn, err := cfg.Sources[0].LokiConnection()
	if err != nil {
		t.Fatal(err)
	}
	if conn.Auth.Password != "pw-456" || conn.Auth.Token != "" {
		t.Errorf("basic mode: password=%q token=%q, want the secret in password only", conn.Auth.Password, conn.Auth.Token)
	}
}

func TestValidateConfig_InvalidLokiSources(t *testing.T) {
	missingSelector := lokiProvisionSource("no-selector", "", nil)
	delete(missingSelector.Connection, "selector")

	unsetSecret := lokiProvisionSource("unset-secret", "", map[string]any{"mode": "bearer"})
	unsetSecret.SecretRef = "LOGCHEF_TEST_UNSET_LOKI_TOKEN"

	badPrincipal := lokiProvisionSource("bad-principal", "has space", nil)

	reservedHeader := lokiProvisionSource("reserved-header", "", nil)
	reservedHeader.Connection["headers"] = map[string]any{"X-Scope-OrgID": "other"}

	tests := []struct {
		name    string
		src     config.ProvisionSource
		want    string
		notWant string
	}{
		{name: "missing selector", src: missingSelector, want: "loki connection.selector: selector is required"},
		{name: "bearer without token or secret_ref", src: lokiProvisionSource("no-token", "", map[string]any{"mode": "bearer"}), want: "loki connection.auth.token: token is required for bearer auth; set it or secret_ref"},
		{name: "secret_ref names an unset variable", src: unsetSecret, want: `secret_ref "LOGCHEF_TEST_UNSET_LOKI_TOKEN" env var is empty or not set`, notWant: "auth.token"},
		{name: "invalid principal", src: badPrincipal, want: "loki connection.principal"},
		{name: "reserved header", src: reservedHeader, want: "loki connection.headers"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{tc.src}}
			ResolveConfig(cfg)
			err := ValidateConfig(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateConfig = %v, want an error containing %q", err, tc.want)
			}
			if tc.notWant != "" && strings.Contains(err.Error(), tc.notWant) {
				t.Errorf("error %q should not mention %q", err, tc.notWant)
			}
		})
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

func TestValidateConfig_ClickHouseActivitySettings(t *testing.T) {
	withActivity := func(activity map[string]any) config.ProvisionSource {
		return config.ProvisionSource{
			Name:       "s",
			SourceType: "clickhouse",
			Connection: map[string]any{
				"host":       "h:9000",
				"database":   "db",
				"table_name": "logs_view",
				"password":   "p",
				"activity":   activity,
			},
		}
	}
	cases := []struct {
		name     string
		activity map[string]any
		wantErr  bool
	}{
		{"allow_view with default caps accepted", map[string]any{"allow_view": true}, false},
		{"allow_view with positive caps accepted", map[string]any{"allow_view": true, "max_rows_to_read": 5000000, "max_bytes_to_read": 104857600}, false},
		{"maximum caps accepted", map[string]any{"max_rows_to_read": int64(10_000_000_000), "max_bytes_to_read": int64(1_099_511_627_776)}, false},
		{"rows above maximum rejected", map[string]any{"max_rows_to_read": int64(10_000_000_001)}, true},
		{"bytes above maximum rejected", map[string]any{"max_bytes_to_read": int64(1_099_511_627_777)}, true},
		{"zero max_rows_to_read rejected", map[string]any{"allow_view": true, "max_rows_to_read": 0}, true},
		{"negative max_bytes_to_read rejected", map[string]any{"allow_view": true, "max_bytes_to_read": -10}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{withActivity(tc.activity)}}
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

func TestResolveConfig_DefaultMetaTSField(t *testing.T) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			{Name: "src1", MetaTSField: ""},
		},
	}
	ResolveConfig(cfg)
	if cfg.Sources[0].MetaTSField != "timestamp" {
		t.Errorf("expected default MetaTSField 'timestamp', got %q", cfg.Sources[0].MetaTSField)
	}
}

func TestResolveConfig_DefaultVictoriaLogsMetaTSField(t *testing.T) {
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
	ResolveConfig(cfg)
	if cfg.Sources[0].MetaTSField != "_time" {
		t.Errorf("expected default MetaTSField '_time', got %q", cfg.Sources[0].MetaTSField)
	}
}

func TestResolveConfig_DefaultMemberRole(t *testing.T) {
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
	ResolveConfig(cfg)
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

func TestValidateConfig_DuplicateTeamSources(t *testing.T) {
	for _, manageSources := range []bool{false, true} {
		t.Run(fmt.Sprintf("manage_sources=%t", manageSources), func(t *testing.T) {
			cfg := &config.ProvisioningConfig{
				ManageTeams:   true,
				ManageSources: manageSources,
				Sources:       []config.ProvisionSource{{Name: "logs", Connection: map[string]any{"host": "localhost:9000", "database": "default", "table_name": "logs"}}},
				Teams:         []config.ProvisionTeam{{Name: "team", Sources: []string{"logs", "logs"}}},
			}
			if err := ValidateConfig(cfg); err == nil || !strings.Contains(err.Error(), "duplicate source") {
				t.Fatalf("expected duplicate source validation error, got %v", err)
			}
		})
	}
}
