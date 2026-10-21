package config

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files in testdata/golden")

// clearLogchefEnv hides any LOGCHEF_ variables of the host so a golden run
// only sees the config file under test.
func clearLogchefEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, envPrefix) {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatalf("unset %s: %v", name, err)
			}
		}
	}
}

func assertGolden(t *testing.T, name string, cfg *Config) {
	t.Helper()
	got, err := json.MarshalIndent(cfg, "", "  ") //nolint:musttag // Go field names are the golden schema
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", name+".json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("resolved config differs from %s\n got: %s\nwant: %s", path, got, want)
	}
}

// writeRawConfig writes body as the whole config file, so a case controls
// every table header itself.
func writeRawConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

const (
	goldenAuthKeys = `admin_emails = ["admin@example.com"]
api_token_secret = "0123456789abcdef0123456789abcdef"
`
	goldenOIDCKeys = `provider_url = "http://localhost/dex"
auth_url = "http://localhost/dex/auth"
token_url = "http://localhost/dex/token"
client_id = "logchef"
redirect_url = "http://localhost/callback"
`
)

func TestLoad_Golden(t *testing.T) {
	const zeros = `
[server]
port = 0
host = ""
http_server_timeout = "0s"
secure_cookie = false
proxy_header = "  "

[sqlite]
path = ""

[logging]
level = ""

[postgres]
max_open_conns = 0
max_idle_conns = 0
conn_max_lifetime = "0s"

[alerts]
enabled = false
evaluation_interval = "0s"
default_lookback = "0s"
history_limit = 0

[ai]
enabled = false
base_url = ""
max_tokens = 0
temperature = 0.0

[auth]
` + goldenAuthKeys + `session_duration = "0s"
max_concurrent_sessions = 0
default_token_expiry = "0s"

[query]
default_preview_limit = 0
max_preview_limit = 0
max_response_bytes = 0
default_timeout_seconds = 0
max_timeout_seconds = 0
mcp_call_timeout_seconds = 0
max_concurrent_per_user = 0
max_concurrent_global = 0

[export]
max_rows = 0
default_timeout_seconds = 0
max_timeout_seconds = 0
max_concurrent_per_user = 0
max_concurrent_global = 0
artifact_ttl = "0s"

[tail]
poll_interval = "0s"
max_per_user = 0
max_global = 0
session_ttl = "0s"
max_rows_per_sec = 0
lookback_margin = "0s"

[shares]
default_ttl = "0s"
max_query_text_bytes = 0

[rate_limit]
enabled = true
auth_per_ip_per_minute = 0
auth_global_per_minute = 0
query_per_user_per_minute = 0

[dashboard_cache]
enabled = false
default_ttl = "0s"
max_ttl = "0s"
max_bytes = 0
max_entry_bytes = 0
max_entries = 0
max_concurrent_fills = 0

[oidc]
` + goldenOIDCKeys + `
`
	const negatives = `
[query]
max_limit = 500
default_preview_limit = -1
max_timeout_seconds = -1
default_timeout_seconds = 9999
mcp_call_timeout_seconds = 9999

[export]
max_rows = -1
formats = []
default_timeout_seconds = 9999

[tail]
lookback_margin = "-1s"
max_global = -3

[rate_limit]
auth_global_per_minute = -1

[oidc]
` + goldenOIDCKeys + `scopes = []

[auth]
` + goldenAuthKeys + `
`
	const legacyAlias = `
[query]
max_limit = 5000
default_preview_limit = 20000
`
	const customized = `
[server]
port = 9000
secure_cookie = true
proxy_header = "X-Real-IP"

[oidc]
` + goldenOIDCKeys + `scopes = ["openid"]

[export]
formats = ["csv"]

[ai]
provider = "bedrock"
region = "us-east-1"
model = "example.model-v1:0"

[auth]
` + goldenAuthKeys + `
`

	const capitalized = `
[Server]
Port = 9001
Secure_Cookie = false

[AI]
Model = ""

[Query]
Max_Limit = 700
Max_Preview_Limit = 0

[OIDC]
` + goldenOIDCKeys + `Scopes = ["openid"]

[auth]
` + goldenAuthKeys + `
`

	tests := []struct {
		name string
		path func(t *testing.T) string
	}{
		{"repo_config", func(t *testing.T) string { return filepath.Join("..", "..", "config.toml") }},
		{"required_only", func(t *testing.T) string { return writeConfig(t, "") }},
		{"explicit_zeros", func(t *testing.T) string { return writeRawConfig(t, zeros) }},
		{"negatives_and_empties", func(t *testing.T) string { return writeRawConfig(t, negatives) }},
		{"legacy_max_limit_alias", func(t *testing.T) string { return writeConfig(t, legacyAlias) }},
		{"customized", func(t *testing.T) string { return writeRawConfig(t, customized) }},
		{"capitalized_keys", func(t *testing.T) string { return writeRawConfig(t, capitalized) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearLogchefEnv(t)
			cfg, err := Load(tt.path(t))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			assertGolden(t, tt.name, cfg)
		})
	}
}

func TestLoad_GoldenEnvOnly(t *testing.T) {
	clearLogchefEnv(t)
	for k, v := range map[string]string{
		"LOGCHEF_AUTH__ADMIN_EMAILS":                 "admin@example.com",
		"LOGCHEF_AUTH__API_TOKEN_SECRET":             "0123456789abcdef0123456789abcdef",
		"LOGCHEF_AUTH__SESSION_DURATION":             "0s",
		"LOGCHEF_OIDC__PROVIDER_URL":                 "http://localhost/dex",
		"LOGCHEF_OIDC__AUTH_URL":                     "http://localhost/dex/auth",
		"LOGCHEF_OIDC__TOKEN_URL":                    "http://localhost/dex/token",
		"LOGCHEF_OIDC__CLIENT_ID":                    "logchef",
		"LOGCHEF_OIDC__REDIRECT_URL":                 "http://localhost/callback",
		"LOGCHEF_SERVER__PORT":                       "0",
		"LOGCHEF_SERVER__SECURE_COOKIE":              "false",
		"LOGCHEF_SQLITE__PATH":                       "",
		"LOGCHEF_ALERTS__ENABLED":                    "false",
		"LOGCHEF_ALERTS__HISTORY_LIMIT":              "0",
		"LOGCHEF_AI__MODEL":                          "",
		"LOGCHEF_QUERY__MAX_PREVIEW_LIMIT":           "0",
		"LOGCHEF_QUERY__DEFAULT_TIMEOUT_SECONDS":     "45",
		"LOGCHEF_RATE_LIMIT__ENABLED":                "false",
		"LOGCHEF_RATE_LIMIT__AUTH_GLOBAL_PER_MINUTE": "0",
		"LOGCHEF_DASHBOARD_CACHE__ENABLED":           "false",
		"LOGCHEF_TAIL__LOOKBACK_MARGIN":              "0s",
	} {
		t.Setenv(k, v)
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertGolden(t, "env_only", cfg)
}
