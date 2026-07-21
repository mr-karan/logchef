package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestLoad_DashboardCacheDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	dc := cfg.DashboardCache
	if !dc.Enabled {
		t.Error("dashboard_cache.enabled should default to true")
	}
	if dc.DefaultTTL != 10*time.Minute {
		t.Errorf("default_ttl = %s, want 10m", dc.DefaultTTL)
	}
	if dc.MaxTTL != time.Hour {
		t.Errorf("max_ttl = %s, want 1h", dc.MaxTTL)
	}
	if dc.MaxBytes != 64*1024*1024 {
		t.Errorf("max_bytes = %d, want %d", dc.MaxBytes, 64*1024*1024)
	}
	if dc.MaxEntryBytes != 4*1024*1024 {
		t.Errorf("max_entry_bytes = %d, want %d", dc.MaxEntryBytes, 4*1024*1024)
	}
	if dc.MaxEntries != 1024 {
		t.Errorf("max_entries = %d, want 1024", dc.MaxEntries)
	}
	if dc.MaxConcurrentFills != 8 {
		t.Errorf("max_concurrent_fills = %d, want 8", dc.MaxConcurrentFills)
	}
}

func TestLoad_DashboardCacheOverrides(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[dashboard_cache]
enabled = false
default_ttl = "5m"
max_ttl = "30m"
max_bytes = 1048576
max_entry_bytes = 262144
max_entries = 16
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	dc := cfg.DashboardCache
	if dc.Enabled {
		t.Error("dashboard_cache.enabled = true, want false (explicitly set)")
	}
	if dc.DefaultTTL != 5*time.Minute {
		t.Errorf("default_ttl = %s, want 5m", dc.DefaultTTL)
	}
	if dc.MaxTTL != 30*time.Minute {
		t.Errorf("max_ttl = %s, want 30m", dc.MaxTTL)
	}
	if dc.MaxBytes != 1048576 {
		t.Errorf("max_bytes = %d, want 1048576", dc.MaxBytes)
	}
	if dc.MaxEntryBytes != 262144 {
		t.Errorf("max_entry_bytes = %d, want 262144", dc.MaxEntryBytes)
	}
	if dc.MaxEntries != 16 {
		t.Errorf("max_entries = %d, want 16", dc.MaxEntries)
	}
}

func TestLoad_TrustedProxiesValidAndProxyHeaderDefault(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[server]
trusted_proxies = ["10.20.30.40/32", "192.168.1.1"]
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Server.TrustedProxies) != 2 {
		t.Errorf("trusted_proxies = %v, want 2 entries", cfg.Server.TrustedProxies)
	}
	if cfg.Server.ProxyHeader != "X-Forwarded-For" {
		t.Errorf("proxy_header = %q, want X-Forwarded-For (default)", cfg.Server.ProxyHeader)
	}
}

func TestLoad_TrustedProxiesInvalidFailFast(t *testing.T) {
	for _, bad := range []string{"not-an-ip", "0.0.0.0/0", "::/0", "10.0.0.0/999"} {
		if _, err := Load(writeConfig(t, "[server]\ntrusted_proxies = [\""+bad+"\"]\n")); err == nil {
			t.Errorf("Load with trusted_proxies=%q: want error, got nil", bad)
		}
	}
}

func TestLoad_BedrockRequiresRegionAndModel(t *testing.T) {
	// provider=bedrock with neither region nor model → error (region checked first).
	if _, err := Load(writeConfig(t, "\n[ai]\nenabled = true\nprovider = \"bedrock\"\n")); err == nil {
		t.Fatal("expected error: bedrock provider with no region")
	}
	// region set but model missing → error (gpt-4o default is invalid for bedrock).
	if _, err := Load(writeConfig(t, "\n[ai]\nenabled = true\nprovider = \"bedrock\"\nregion = \"us-east-1\"\n")); err == nil {
		t.Fatal("expected error: bedrock provider with no model")
	}
	// region + model set → ok.
	extra := "\n[ai]\nenabled = true\nprovider = \"bedrock\"\nregion = \"us-east-1\"\nmodel = \"anthropic.claude-3-5-sonnet-20241022-v2:0\"\n"
	if _, err := Load(writeConfig(t, extra)); err != nil {
		t.Fatalf("valid bedrock config should load: %v", err)
	}
}
