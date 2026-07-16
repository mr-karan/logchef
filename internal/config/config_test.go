package config

import (
	"os"
	"path/filepath"
	"testing"
)

// baseConfig is a minimal config that passes all the non-database validation,
// so individual tests can append a [database]/[postgres] section and assert on
// just the backend-selection behavior.
const baseConfig = `
[auth]
admin_emails = ["admin@example.com"]
api_token_secret = "0123456789abcdef0123456789abcdef"

[oidc]
provider_url = "http://localhost/dex"
auth_url = "http://localhost/dex/auth"
token_url = "http://localhost/dex/token"
client_id = "logchef"
redirect_url = "http://localhost/callback"
`

func writeConfig(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(baseConfig+extra), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoad_DefaultsToSQLite(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("driver = %q, want sqlite", cfg.Database.Driver)
	}
	if cfg.SQLite.Path != defaultSQLitePath {
		t.Errorf("sqlite path = %q, want default", cfg.SQLite.Path)
	}
}

func TestLoad_PostgresRequiresDSN(t *testing.T) {
	_, err := Load(writeConfig(t, "\n[database]\ndriver = \"postgres\"\n"))
	if err == nil {
		t.Fatal("expected error when postgres driver has no DSN")
	}
}

func TestLoad_PostgresWithDSN(t *testing.T) {
	extra := "\n[database]\ndriver = \"postgres\"\n\n[postgres]\ndsn = \"postgres://logchef:logchef@localhost:5432/logchef?sslmode=disable\"\n"
	cfg, err := Load(writeConfig(t, extra))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("driver = %q, want postgres", cfg.Database.Driver)
	}
	if cfg.Postgres.MaxOpenConns != defaultPostgresMaxOpenConns {
		t.Errorf("max_open_conns = %d, want default %d", cfg.Postgres.MaxOpenConns, defaultPostgresMaxOpenConns)
	}
}

func TestLoad_RejectsUnknownDriver(t *testing.T) {
	_, err := Load(writeConfig(t, "\n[database]\ndriver = \"mysql\"\n"))
	if err == nil {
		t.Fatal("expected error for unknown driver")
	}
}

func TestLoad_AutoProvisionEnabledRequiresAllowedDomains(t *testing.T) {
	_, err := Load(writeConfig(t, "\n[auth.auto_provision]\nenabled = true\n"))
	if err == nil {
		t.Fatal("expected error when auto_provision.enabled=true with no allowed_domains")
	}
}

func TestLoad_AutoProvisionEnabledWithAllowedDomains(t *testing.T) {
	extra := "\n[auth.auto_provision]\nenabled = true\nallowed_domains = [\"example.com\"]\ndefault_team_ids = [1, 2]\n"
	cfg, err := Load(writeConfig(t, extra))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Auth.AutoProvision.Enabled {
		t.Error("auto_provision.enabled = false, want true")
	}
	if got := cfg.Auth.AutoProvision.AllowedDomains; len(got) != 1 || got[0] != "example.com" {
		t.Errorf("allowed_domains = %v, want [example.com]", got)
	}
	if got := cfg.Auth.AutoProvision.DefaultTeamIDs; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("default_team_ids = %v, want [1 2]", got)
	}
}

func TestLoad_AutoProvisionDisabledByDefault(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.AutoProvision.Enabled {
		t.Error("auto_provision.enabled should default to false")
	}
}

func TestLoad_RateLimitDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rl := cfg.RateLimit
	if rl.Enabled {
		t.Error("rate_limit.enabled should default to false (opt-in; per-IP needs trusted-proxy config)")
	}
	if rl.AuthPerIPPerMinute != 20 {
		t.Errorf("auth_per_ip_per_minute = %d, want 20", rl.AuthPerIPPerMinute)
	}
	if rl.AuthGlobalPerMinute != 300 {
		t.Errorf("auth_global_per_minute = %d, want 300", rl.AuthGlobalPerMinute)
	}
	if rl.QueryPerUserPerMinute != 120 {
		t.Errorf("query_per_user_per_minute = %d, want 120", rl.QueryPerUserPerMinute)
	}
}

func TestLoad_RateLimitOverrides(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[rate_limit]
enabled = false
auth_per_ip_per_minute = 5
auth_global_per_minute = 0
query_per_user_per_minute = 50
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rl := cfg.RateLimit
	if rl.Enabled {
		t.Error("rate_limit.enabled = true, want false (explicitly set)")
	}
	if rl.AuthPerIPPerMinute != 5 {
		t.Errorf("auth_per_ip_per_minute = %d, want 5", rl.AuthPerIPPerMinute)
	}
	// 0 is a valid value meaning "no global cap" and must be preserved.
	if rl.AuthGlobalPerMinute != 0 {
		t.Errorf("auth_global_per_minute = %d, want 0 (global cap disabled)", rl.AuthGlobalPerMinute)
	}
	if rl.QueryPerUserPerMinute != 50 {
		t.Errorf("query_per_user_per_minute = %d, want 50", rl.QueryPerUserPerMinute)
	}
}
