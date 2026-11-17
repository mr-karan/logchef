package provisioning

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// vlHeaderSource is a VictoriaLogs source on an unreachable port, so the
// best-effort connection check fails fast.
func vlHeaderSource(name string, headers map[string]any) config.ProvisionSource {
	return config.ProvisionSource{
		Name:       name,
		SourceType: models.SourceTypeVictoriaLogs,
		Connection: map[string]any{
			"base_url": "http://127.0.0.1:1",
			"headers":  headers,
		},
	}
}

func lokiHeaderSource(name string, headers map[string]any) config.ProvisionSource {
	src := lokiProvisionSource(name, "payments-readonly", nil)
	src.Connection["base_url"] = "http://127.0.0.1:1"
	src.Connection["headers"] = headers
	return src
}

func TestExportImportHeaderSecretsSQLite(t *testing.T) {
	runHeaderSecretExportImport(t, newReconcileTestDB(t))
}

func TestExportImportHeaderSecretsPostgres(t *testing.T) {
	db, _, _ := newProvisioningPostgres(t)
	runHeaderSecretExportImport(t, db)
}

// Plain header values import, export as references, and importing the export
// with the referenced variables set changes nothing.
func runHeaderSecretExportImport(t *testing.T, db store.Store) {
	t.Setenv("VL_PROD_TOKEN", "vl-token")
	w := viewWorld{t: t, db: db, ds: datasourcesFor(db)}

	vl := vlHeaderSource("VL Prod", map[string]any{"X-Api-Key": "vl-key-1", "X-Team": "payments"})
	vl.Connection["auth"] = map[string]any{"mode": "bearer"}
	vl.SecretRef = "VL_PROD_TOKEN"
	loki := lokiHeaderSource("loki-gw", map[string]any{"X-Gateway-Key": "loki-key-1"})
	w.reconcile(&config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{vl, loki}})
	before := map[string]*models.Source{"VL Prod": w.source("VL Prod"), "loki-gw": w.source("loki-gw")}

	exported, err := ExportConfig(t.Context(), db)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	wantRefs := map[string]map[string]string{
		"VL Prod": {"X-Api-Key": "LOGCHEF_SOURCE_VL_PROD_HEADER_X_API_KEY", "X-Team": "LOGCHEF_SOURCE_VL_PROD_HEADER_X_TEAM"},
		"loki-gw": {"X-Gateway-Key": "LOGCHEF_SOURCE_LOKI_GW_HEADER_X_GATEWAY_KEY"},
	}
	for i := range exported.Sources {
		src := &exported.Sources[i]
		if !maps.Equal(src.HeaderSecretRefs, wantRefs[src.Name]) {
			t.Errorf("source %q exported header_secret_refs %v, want %v", src.Name, src.HeaderSecretRefs, wantRefs[src.Name])
		}
		if _, ok := src.Connection["headers"]; ok {
			t.Errorf("source %q exported connection.headers %v, want none", src.Name, src.Connection["headers"])
		}
	}

	t.Setenv("LOGCHEF_SOURCE_VL_PROD_HEADER_X_API_KEY", "vl-key-1")
	t.Setenv("LOGCHEF_SOURCE_VL_PROD_HEADER_X_TEAM", "payments")
	t.Setenv("LOGCHEF_SOURCE_LOKI_GW_HEADER_X_GATEWAY_KEY", "loki-key-1")
	w.reconcile(exported)
	for name, b := range before {
		a := w.source(name)
		if a.ID != b.ID || string(a.ConnectionConfig) != string(b.ConnectionConfig) || a.IdentityKey != b.IdentityKey ||
			a.SecretRef != b.SecretRef || !a.UpdatedAt.Equal(b.UpdatedAt) {
			t.Errorf("source %q changed on import:\nbefore %+v\n after %+v", name, b, a)
		}
	}

	// A new header value in the environment updates the Loki source in place:
	// headers are not part of its identity. Reconcile resolved the first
	// export in place, so export again.
	if exported, err = ExportConfig(t.Context(), db); err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	t.Setenv("LOGCHEF_SOURCE_LOKI_GW_HEADER_X_GATEWAY_KEY", "loki-key-2")
	w.reconcile(exported)
	updated := w.source("loki-gw")
	conn, err := updated.LokiConnection()
	if err != nil {
		t.Fatal(err)
	}
	if conn.Headers["X-Gateway-Key"] != "loki-key-2" || updated.IdentityKey != before["loki-gw"].IdentityKey {
		t.Errorf("after a header change: headers %v identity %q, want loki-key-2 and %q",
			conn.Headers, updated.IdentityKey, before["loki-gw"].IdentityKey)
	}
}

// No header value of any source reaches the exported sources, the part of the
// export endpoint's output that holds connection data.
func TestExportConfig_HeaderValuesNeverLeak(t *testing.T) {
	db := newReconcileTestDB(t)
	w := viewWorld{t: t, db: db, ds: datasourcesFor(db)}
	secrets := []string{"vl-hdr-secret-7f3a91", "vl-hdr-secret-c0ffee", "loki-hdr-secret-5e2d77", "loki-org-secret-9b41aa"}
	w.reconcile(&config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{
		vlHeaderSource("vl", map[string]any{"Authorization-Proxy": secrets[0], "X-Api-Key": secrets[1]}),
		lokiHeaderSource("loki", map[string]any{"X-Gateway-Key": secrets[2], "X-Org-Token": secrets[3]}),
	}})

	exported, err := ExportConfig(t.Context(), db)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	out, err := json.Marshal(exported.Sources)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(out), secret) {
			t.Errorf("export holds header value %q:\n%s", secret, out)
		}
	}
}

// A header reference to an unset variable is a startup error that names the
// source and header; nothing is saved with an empty header.
func TestReconcile_MissingHeaderSecretFails(t *testing.T) {
	cases := map[string]config.ProvisionSource{
		"victorialogs": vlHeaderSource("vl", nil),
		"loki":         lokiHeaderSource("loki", nil),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			db := newReconcileTestDB(t)
			src.HeaderSecretRefs = map[string]string{"X-Api-Key": "LOGCHEF_TEST_UNSET_HEADER"}
			cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{src}}
			err := Reconcile(t.Context(), cfg, db, datasourcesFor(db), testRecorder(db), quietLogger(), nil)
			want := `sources[0] ("` + src.Name + `"): header "X-Api-Key": header_secret_refs env var "LOGCHEF_TEST_UNSET_HEADER" is empty or not set`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Reconcile = %v, want an error containing %q", err, want)
			}
			if got := findSource(t, db, src.Name); got != nil {
				t.Fatalf("source saved despite the error: %+v", got)
			}
		})
	}
}

func TestValidateConfig_HeaderSecretRefsOnClickHouse(t *testing.T) {
	src := viewSource("ch")
	src.HeaderSecretRefs = map[string]string{"X-Api-Key": "SOME_VAR"}
	cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{src}}
	ResolveConfig(cfg)
	err := ValidateConfig(cfg, testDatasources())
	want := `sources[0] ("ch"): header_secret_refs applies to victorialogs and loki sources only`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateConfig = %v, want an error containing %q", err, want)
	}
}

// Read through config.Load so the header name keeps its case from the TOML.
func TestResolveConfig_HeaderSecretRefsFromTOML(t *testing.T) {
	t.Setenv("VL_API_KEY", "key-from-env")
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
name = "vl"
source_type = "victorialogs"

[sources.connection]
base_url = "http://127.0.0.1:9428"
headers = { X-Team = "payments" }

[sources.header_secret_refs]
X-Api-Key = "VL_API_KEY"
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
	if err := ValidateConfig(&cfg.Provisioning, testDatasources()); err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	conn, err := cfg.Provisioning.Sources[0].VictoriaLogsConnection()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"X-Api-Key": "key-from-env", "X-Team": "payments"}
	if !maps.Equal(conn.Headers, want) {
		t.Fatalf("headers %v, want %v", conn.Headers, want)
	}
}

// Two references to one header in different case would race for its value
// and let one of them go unchecked.
func TestValidateConfig_HeaderSecretRefsCaseDuplicate(t *testing.T) {
	t.Setenv("LOGCHEF_TEST_KEY_A", "a")
	src := vlHeaderSource("vl", nil)
	src.HeaderSecretRefs = map[string]string{"X-Api-Key": "LOGCHEF_TEST_KEY_A", "x-api-key": "LOGCHEF_TEST_UNSET_KEY_B"}
	cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{src}}
	ResolveConfig(cfg)
	err := ValidateConfig(cfg, testDatasources())
	want := `sources[0] ("vl"): header_secret_refs names header "x-api-key" twice, as "X-Api-Key" and "x-api-key"; keep one`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("ValidateConfig = %v, want an error containing %q", err, want)
	}
}

// Source names that sanitize alike would export one variable for two
// credentials, so importing would hand one source the other's secret.
func TestExportConfig_HeaderRefCollisionAcrossSources(t *testing.T) {
	db := newReconcileTestDB(t)
	w := viewWorld{t: t, db: db, ds: datasourcesFor(db)}
	dash := vlHeaderSource("prod-eu", map[string]any{"X-Api-Key": "key-dash"})
	underscore := vlHeaderSource("prod_eu", map[string]any{"X-Api-Key": "key-underscore"})
	underscore.Connection["tenant"] = map[string]any{"account_id": "2"}
	w.reconcile(&config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{dash, underscore}})

	_, err := ExportConfig(t.Context(), db)
	if err == nil || !strings.Contains(err.Error(), "both export as LOGCHEF_SOURCE_PROD_EU_HEADER_X_API_KEY") ||
		!strings.Contains(err.Error(), `"prod-eu"`) || !strings.Contains(err.Error(), `"prod_eu"`) {
		t.Fatalf("ExportConfig = %v, want a collision error naming both sources", err)
	}
}
