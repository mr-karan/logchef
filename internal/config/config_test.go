package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestLoad_DemoReadOnlyIsOptIn(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Demo.ReadOnly {
		t.Fatal("demo.read_only should default to false")
	}

	cfg, err = Load(writeConfig(t, "\n[demo]\nread_only = true\n"))
	if err != nil {
		t.Fatalf("Load with demo mode: %v", err)
	}
	if !cfg.Demo.ReadOnly {
		t.Fatal("demo.read_only = false, want true")
	}
}

func TestLoad_DemoLoginCredentialsRequireReadOnlyLocalAuth(t *testing.T) {
	tests := []struct {
		name  string
		extra string
	}{
		{
			name: "read-only disabled",
			extra: `
[auth.local]
enabled = true
admin_email = "demo@example.com"
admin_password = "demo-password"

[demo]
show_login_credentials = true
`,
		},
		{
			name: "local auth disabled",
			extra: `
[demo]
read_only = true
show_login_credentials = true
`,
		},
		{
			name: "local credentials missing",
			extra: `
[auth.local]
enabled = true

[demo]
read_only = true
show_login_credentials = true
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tt.extra)); err == nil {
				t.Fatal("expected invalid demo credential exposure config to be rejected")
			}
		})
	}
}

func TestLoad_DemoLoginCredentialsCanBeEnabledForReadOnlyLocalAuth(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[auth.local]
enabled = true
admin_email = "demo@example.com"
admin_password = "demo-password"

[demo]
read_only = true
show_login_credentials = true
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Demo.ShowLoginCredentials {
		t.Fatal("demo.show_login_credentials = false, want true")
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

func TestServerConfig_BasePathFromFrontendURL(t *testing.T) {
	for _, tc := range []struct {
		frontendURL, wantBase, wantCookie string
	}{
		{"", "/", "/"},
		{"http://localhost:5173", "/", "/"},
		{"https://logs.example.com/", "/", "/"},
		{"https://example.com/logchef", "/logchef/", "/logchef"},
		{"https://example.com/logchef/", "/logchef/", "/logchef"},
		{"https://example.com/tools/logchef//", "/tools/logchef/", "/tools/logchef"},
		{"https://example.com/log chef", "/log%20chef/", "/log%20chef"},
	} {
		s := ServerConfig{FrontendURL: tc.frontendURL}
		if got := s.BasePath(); got != tc.wantBase {
			t.Errorf("BasePath(%q) = %q, want %q", tc.frontendURL, got, tc.wantBase)
		}
		if got := s.CookiePath(); got != tc.wantCookie {
			t.Errorf("CookiePath(%q) = %q, want %q", tc.frontendURL, got, tc.wantCookie)
		}
	}
}

func TestLoad_RejectsUnparseableFrontendURL(t *testing.T) {
	if _, err := Load(writeConfig(t, "[server]\nfrontend_url = \"http://[::1\"\n")); err == nil {
		t.Fatal("Load with an unparseable server.frontend_url: want error, got nil")
	}
}

func TestLoad_OAuthDisabledByDefault(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.OAuth.Enabled || len(cfg.Auth.OAuth.Clients) != 0 {
		t.Fatalf("OAuth = %+v, want disabled with no clients", cfg.Auth.OAuth)
	}
}

func TestLoad_OAuthValidation(t *testing.T) {
	const client = `
[[auth.oauth.clients]]
id = "chatgpt"
name = "ChatGPT"
redirect_uris = ["https://chatgpt.com/connector_platform_oauth_redirect"]
`
	tests := []struct {
		name    string
		extra   string
		wantErr bool
	}{
		{"enabled without public_url", "[auth.oauth]\nenabled = true\n", true},
		{"https public_url", "[server]\npublic_url = \"https://logchef.example.com\"\n[auth.oauth]\nenabled = true\n" + client, false},
		{"base path public_url", "[server]\npublic_url = \"https://example.com/logchef\"\n[auth.oauth]\nenabled = true\n", true},
		{"userinfo public_url", "[server]\npublic_url = \"https://user@logchef.example.com\"\n[auth.oauth]\nenabled = true\n", true},
		{"port public_url", "[server]\npublic_url = \"https://logchef.example.com:8443\"\n[auth.oauth]\nenabled = true\n", false},
		{"loopback http public_url", "[server]\npublic_url = \"http://localhost:8125\"\n[auth.oauth]\nenabled = true\n", false},
		{"browser_url on another host", "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \"https://logchef.example.com\"\n[auth.oauth]\nenabled = true\n", false},
		{"loopback browser_url", "[server]\npublic_url = \"http://127.0.0.1:8125\"\nbrowser_url = \"http://localhost:8125\"\n[auth.oauth]\nenabled = true\n", false},
		{"browser_url with path", "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \"https://logchef.example.com/ui\"\n[auth.oauth]\nenabled = true\n", true},
		{"browser_url trailing slash", "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \"https://logchef.example.com/\"\n[auth.oauth]\nenabled = true\n", true},
		{"non-loopback http browser_url", "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \"http://logchef.example.com\"\n[auth.oauth]\nenabled = true\n", true},
		{"uppercase browser_url host", "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \"https://LOGCHEF.example.com\"\n[auth.oauth]\nenabled = true\n", true},
		{"browser_url default https port", "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \"https://logchef.example.com:443\"\n[auth.oauth]\nenabled = true\n", true},
		{"browser_url default http port", "[server]\npublic_url = \"http://127.0.0.1:8125\"\nbrowser_url = \"http://localhost:80\"\n[auth.oauth]\nenabled = true\n", true},
		{"uppercase browser_url scheme", "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \"HTTPS://logchef.example.com\"\n[auth.oauth]\nenabled = true\n", true},
		{"browser_url non-default port", "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \"https://logchef.example.com:8443\"\n[auth.oauth]\nenabled = true\n", false},
		{"uppercase public_url host", "[server]\npublic_url = \"https://Logchef-API.example.com\"\n[auth.oauth]\nenabled = true\n", true},
		{"public_url default https port", "[server]\npublic_url = \"https://logchef-api.example.com:443\"\n[auth.oauth]\nenabled = true\n", true},
		{"public_url default http port", "[server]\npublic_url = \"http://localhost:80\"\n[auth.oauth]\nenabled = true\n", true},
		{"ipv6 loopback public_url", "[server]\npublic_url = \"http://[::1]:8125\"\n[auth.oauth]\nenabled = true\n", false},
		{"disabled ignores bad browser_url", "[server]\nbrowser_url = \"not a url\"\n", false},
		{"non-loopback http public_url", "[server]\npublic_url = \"http://logchef.example.com\"\n[auth.oauth]\nenabled = true\n", true},
		{"trailing slash", "[server]\npublic_url = \"https://logchef.example.com/\"\n[auth.oauth]\nenabled = true\n", true},
		{"query in public_url", "[server]\npublic_url = \"https://logchef.example.com?x=1\"\n[auth.oauth]\nenabled = true\n", true},
		{"relative public_url", "[server]\npublic_url = \"logchef.example.com\"\n[auth.oauth]\nenabled = true\n", true},
		{"disabled ignores bad block", "[auth.oauth]\nenabled = false\n[[auth.oauth.clients]]\nid = \"logchef-cli\"\n", false},
		{"reserved client id", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\n[[auth.oauth.clients]]\nid = \"logchef-cli\"\nname = \"x\"\nredirect_uris = [\"https://a.example.com/cb\"]\n", true},
		{"reserved MCP client id", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\n[[auth.oauth.clients]]\nid = \"logchef-mcp\"\nname = \"x\"\nredirect_uris = [\"https://a.example.com/cb\"]\n", true},
		{"mcp allowed origin", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\nmcp_allowed_origins = [\"http://localhost:6274\"]\n", false},
		{"mcp allowed origin with path", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\nmcp_allowed_origins = [\"http://localhost:6274/x\"]\n", true},
		{"mcp allowed origin wildcard", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\nmcp_allowed_origins = [\"*\"]\n", true},
		{"duplicate client id", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\n" + client + client, true},
		{"missing name", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\n[[auth.oauth.clients]]\nid = \"a\"\nredirect_uris = [\"https://a.example.com/cb\"]\n", true},
		{"no redirect uris", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\n[[auth.oauth.clients]]\nid = \"a\"\nname = \"A\"\n", true},
		{"http redirect uri", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\n[[auth.oauth.clients]]\nid = \"a\"\nname = \"A\"\nredirect_uris = [\"http://a.example.com/cb\"]\n", true},
		{"relative redirect uri", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\n[[auth.oauth.clients]]\nid = \"a\"\nname = \"A\"\nredirect_uris = [\"/cb\"]\n", true},
		{"fragment redirect uri", "[server]\npublic_url = \"https://l.example.com\"\n[auth.oauth]\nenabled = true\n[[auth.oauth.clients]]\nid = \"a\"\nname = \"A\"\nredirect_uris = [\"https://a.example.com/cb#x\"]\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.extra))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_OAuthClientsFromTOML(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[server]
public_url = "https://logchef.example.com"
[auth.oauth]
enabled = true
[[auth.oauth.clients]]
id = "chatgpt"
name = "ChatGPT"
redirect_uris = ["https://chatgpt.com/connector_platform_oauth_redirect"]
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	clients := cfg.Auth.OAuth.Clients
	if len(clients) != 1 || clients[0].ID != "chatgpt" || clients[0].Name != "ChatGPT" || len(clients[0].RedirectURIs) != 1 {
		t.Fatalf("clients = %+v", clients)
	}
}

// Production configs still set oidc.cli_client_id, which the server no longer
// reads. Unknown keys, from the file or the environment, must not stop
// startup: koanf decodes without mapstructure's ErrorUnused.
func TestLoad_IgnoresRemovedCLIClientID(t *testing.T) {
	t.Setenv("LOGCHEF_OIDC__CLI_CLIENT_ID", "logchef-cli")
	path := filepath.Join(t.TempDir(), "config.toml")
	config := strings.Replace(baseConfig, "[oidc]\n", "[oidc]\ncli_client_id = \"logchef-cli\"\n", 1) + "\n[legacy_section]\nunknown_key = true\n"
	if !strings.Contains(config, "cli_client_id") {
		t.Fatal("test config does not contain cli_client_id")
	}
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load with cli_client_id: %v", err)
	}
	if cfg.OIDC.ClientID != "logchef" || cfg.OIDC.ProviderURL != "http://localhost/dex" {
		t.Fatalf("OIDC config not loaded: %+v", cfg.OIDC)
	}
}

// F6-2: query.mcp_call_timeout_seconds bounds one MCP tool call. It defaults
// to 60, falls back to the default when not positive, is clamped to
// query.max_timeout_seconds, and is overridable from the environment.
func TestLoad_MCPCallTimeoutSeconds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra string
		env   string
		want  int
	}{
		{"default", "", "", 60},
		{"file value", "[query]\nmcp_call_timeout_seconds = 90\n", "", 90},
		{"env override", "[query]\nmcp_call_timeout_seconds = 90\n", "45", 45},
		{"zero falls back to default", "[query]\nmcp_call_timeout_seconds = 0\n", "", 60},
		{"negative falls back to default", "[query]\nmcp_call_timeout_seconds = -5\n", "", 60},
		{"clamped to query maximum", "[query]\nmax_timeout_seconds = 30\nmcp_call_timeout_seconds = 120\n", "", 30},
		{"default clamped to query maximum", "[query]\nmax_timeout_seconds = 20\n", "", 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("LOGCHEF_QUERY__MCP_CALL_TIMEOUT_SECONDS", tc.env)
			}
			cfg, err := Load(writeConfig(t, tc.extra))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Query.MCPCallTimeoutSeconds != tc.want {
				t.Fatalf("mcp_call_timeout_seconds = %d, want %d", cfg.Query.MCPCallTimeoutSeconds, tc.want)
			}
		})
	}
}

// The canonical-form error names the exact value to use.
func TestLoad_NonCanonicalOriginErrorNamesCanonicalForm(t *testing.T) {
	for raw, canonical := range map[string]string{
		"https://LOGCHEF.example.com":     "https://logchef.example.com",
		"https://logchef.example.com:443": "https://logchef.example.com",
		"http://localhost:80":             "http://localhost",
		"HTTPS://Logchef.Example.com:443": "https://logchef.example.com",
	} {
		_, err := Load(writeConfig(t, "[server]\npublic_url = \"https://logchef-api.example.com\"\nbrowser_url = \""+raw+"\"\n[auth.oauth]\nenabled = true\n"))
		if err == nil || !strings.Contains(err.Error(), "server.browser_url") || !strings.Contains(err.Error(), fmt.Sprintf("%q", canonical)) {
			t.Errorf("browser_url %q: err = %v, want it to name %q", raw, err, canonical)
		}
	}
}
