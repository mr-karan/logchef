package helm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

type manifest struct {
	Kind     string `yaml:"kind" json:"kind"`
	Metadata struct {
		Name   string            `yaml:"name" json:"name"`
		Labels map[string]string `yaml:"labels" json:"labels"`
	} `yaml:"metadata" json:"metadata"`
	Spec       map[string]any    `yaml:"spec" json:"spec"`
	StringData map[string]string `yaml:"stringData" json:"stringData"`
}

func helmCommand(t *testing.T, args []string, values string) ([]byte, error) {
	t.Helper()
	binary := os.Getenv("HELM_BIN")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("helm")
		if err != nil {
			t.Skip("Helm is required; set HELM_BIN or install Helm")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = strings.NewReader(values)
	return cmd.CombinedOutput()
}

func render(t *testing.T, release, values string) []manifest {
	t.Helper()
	args := []string{"template", release, ".", "--set", "logchef.auth.existingSecret=test-api-token", "--set", "dex.enabled=false", "-f", "-"}
	output, err := helmCommand(t, args, values)
	if err != nil {
		t.Fatalf("render chart: %v\n%s", err, output)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(output))
	var manifests []manifest
	for {
		var item manifest
		if err := decoder.Decode(&item); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode chart: %v", err)
		}
		if item.Kind != "" {
			manifests = append(manifests, item)
		}
	}
	return manifests
}

func find(t *testing.T, manifests []manifest, kind, component string) manifest {
	t.Helper()
	for _, item := range manifests {
		if item.Kind == kind && item.Metadata.Labels["app.kubernetes.io/component"] == component {
			return item
		}
	}
	t.Fatalf("missing %s for %s", kind, component)
	return manifest{}
}

func jsonSpec(t *testing.T, item manifest) string {
	t.Helper()
	data, err := json.Marshal(item.Spec)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHelmPortsConfigAndStrategies(t *testing.T) {
	values := `logchef:
  service:
    port: 8080
  config:
    server:
      public_url: https://logs.example.com
      secure_cookie: false
      trusted_proxies: [10.0.0.0/8]
    query:
      timeout: 7s
    auth:
      oauth:
        enabled: true
        clients:
          - id: test
            name: 'Quotes " and Unicode café'
            redirect_uris: [https://example.com/callback]
`
	manifests := render(t, "ports", values)
	deployment := jsonSpec(t, find(t, manifests, "Deployment", "logchef"))
	for _, expected := range []string{`"type":"Recreate"`, `"containerPort":8125`} {
		if !strings.Contains(deployment, expected) {
			t.Errorf("deployment lacks %s", expected)
		}
	}
	service := jsonSpec(t, find(t, manifests, "Service", "logchef"))
	if !strings.Contains(service, `"port":8080`) || !strings.Contains(service, `"targetPort":"http"`) {
		t.Error("service port override did not preserve the named listener target")
	}
	var config struct {
		Server struct {
			PublicURL      string   `toml:"public_url"`
			SecureCookie   bool     `toml:"secure_cookie"`
			TrustedProxies []string `toml:"trusted_proxies"`
		}
		Query struct{ Timeout string }
		Auth  struct {
			OAuth struct {
				Enabled bool
				Clients []struct {
					ID           string
					Name         string
					RedirectURIs []string `toml:"redirect_uris"`
				}
			}
		}
	}
	if err := toml.Unmarshal([]byte(find(t, manifests, "Secret", "logchef").StringData["config.toml"]), &config); err != nil {
		t.Fatal(err)
	}
	if config.Server.PublicURL != "https://logs.example.com" || config.Server.SecureCookie || len(config.Server.TrustedProxies) != 1 || config.Query.Timeout != "7s" {
		t.Errorf("configuration overrides lost: %+v", config)
	}
	if !config.Auth.OAuth.Enabled || len(config.Auth.OAuth.Clients) != 1 || config.Auth.OAuth.Clients[0].Name != `Quotes " and Unicode café` {
		t.Error("nested tables or escaped strings did not round-trip")
	}
	postgres := render(t, "postgres", "logchef:\n  replicaCount: 2\n  persistence:\n    enabled: false\n  config:\n    database:\n      driver: postgres\n    postgres:\n      dsn: postgres://example.invalid/logchef\n")
	if !strings.Contains(jsonSpec(t, find(t, postgres, "Deployment", "logchef")), `"type":"RollingUpdate"`) {
		t.Error("Postgres should use RollingUpdate")
	}
}

func TestHelmClickHouseNetworksAndNames(t *testing.T) {
	values := "fullnameOverride: " + strings.Repeat("x", 63) + "\nclickhouse:\n  serviceName: custom-clickhouse\n  allowFrom: [10.0.0.0/8, 192.168.0.0/16]\n"
	manifests := render(t, "names", values)
	seen := map[string]bool{}
	for _, item := range manifests {
		key := item.Kind + "/" + item.Metadata.Name
		if seen[key] {
			t.Errorf("duplicate resource %s", key)
		}
		seen[key] = true
		if item.Kind == "Service" && len(item.Metadata.Name) > 63 {
			t.Errorf("Service name too long: %s", item.Metadata.Name)
		}
	}
	chi := jsonSpec(t, find(t, manifests, "ClickHouseInstallation", "clickhouse"))
	for _, expected := range []string{"10.0.0.0/8", "192.168.0.0/16", `"generateName":"custom-clickhouse"`} {
		if !strings.Contains(chi, expected) {
			t.Errorf("ClickHouse lacks %s", expected)
		}
	}
	job := find(t, manifests, "Job", "clickhouse")
	if !strings.Contains(jsonSpec(t, job), `"value":"custom-clickhouse"`) {
		t.Error("schema Job targets a different Service")
	}
	updated := render(t, "names", values+"  schemaJobRevision: '999'\n")
	if job.Metadata.Name == find(t, updated, "Job", "clickhouse").Metadata.Name {
		t.Error("schema revision did not change the Job name")
	}
	if find(t, render(t, "first", ""), "ClickHouseInstallation", "clickhouse").Metadata.Name == find(t, render(t, "second", ""), "ClickHouseInstallation", "clickhouse").Metadata.Name {
		t.Error("different releases share a ClickHouseInstallation")
	}
}

func TestHelmDexPersistentAndSharedStorage(t *testing.T) {
	// --set takes precedence over values files, so invoke Helm directly for Dex.
	values := `logchef:
  auth:
    existingSecret: test-api-token
  oidc:
    existingSecret: test-oidc-client
dex:
  connectors:
    - type: oidc
      id: example
      name: Example
      config:
        issuer: https://idp.example.com
        clientID: test
`
	for _, storage := range []string{"", "  replicaCount: 2\n  storage:\n    type: postgres\n    config:\n      host: database.example.com\n"} {
		output, err := helmCommand(t, []string{"template", "dex", ".", "-f", "-"}, values+storage)
		if err != nil {
			t.Fatalf("Dex render: %v\n%s", err, output)
		}
		if storage == "" {
			if !bytes.Contains(output, []byte("kind: PersistentVolumeClaim")) || !bytes.Contains(output, []byte("claimName: dex-logchef-dex-data")) || bytes.Contains(output, []byte("emptyDir")) {
				t.Error("Dex SQLite must use persistent storage")
			}
		} else if bytes.Contains(output, []byte("name: dex-logchef-dex-data")) {
			t.Error("external Dex storage should not create a SQLite PVC")
		}
		if !bytes.Contains(output, []byte("secretEnv: LOGCHEF_CLIENT_SECRET")) || !bytes.Contains(output, []byte("name: LOGCHEF_OIDC__CLIENT_SECRET")) {
			t.Error("Dex and Logchef must use the same external OIDC client credential")
		}
	}
	for _, invalid := range []string{"  replicaCount: 2\n", "  persistence:\n    enabled: false\n"} {
		if _, err := helmCommand(t, []string{"template", "dex", ".", "-f", "-"}, values+invalid); err == nil {
			t.Error("Dex accepted unsafe SQLite storage settings")
		}
	}
	output, err := helmCommand(t, []string{"template", "dex", ".", "-f", "-"}, values+"fullnameOverride: "+strings.Repeat("x", 63)+"\n")
	if err != nil {
		t.Fatalf("long Dex name: %v", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(output))
	services := map[string]bool{}
	for {
		var item manifest
		if err := decoder.Decode(&item); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if item.Kind == "Service" {
			if services[item.Metadata.Name] || len(item.Metadata.Name) > 63 {
				t.Errorf("invalid or duplicate Service name: %s", item.Metadata.Name)
			}
			services[item.Metadata.Name] = true
		}
	}
}

func TestHelmValidationAndSecurityContexts(t *testing.T) {
	for name, values := range map[string]string{
		"SQLite replicas": "logchef:\n  replicaCount: 2\n",
		"empty networks":  "clickhouse:\n  allowFrom: []\n",
		"invalid port":    "logchef:\n  service:\n    port: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			output, err := helmCommand(t, []string{"template", "invalid", ".", "--set", "logchef.auth.existingSecret=test-api-token", "--set", "dex.enabled=false", "-f", "-"}, values)
			if err == nil {
				t.Fatalf("invalid values accepted: %s", output)
			}
		})
	}
	output, err := helmCommand(t, []string{"template", "missing-secret", ".", "--set", "dex.enabled=false"}, "")
	if err == nil || !bytes.Contains(output, []byte("logchef.auth.existingSecret")) {
		t.Error("missing persistent API-token secret should fail with configuration guidance")
	}
	values := "logchef:\n  dataPermsInit:\n    enabled: false\n  podSecurityContext:\n    runAsNonRoot: true\n    seccompProfile:\n      type: RuntimeDefault\n  securityContext:\n    allowPrivilegeEscalation: false\n    capabilities:\n      drop: [ALL]\n"
	deployment := jsonSpec(t, find(t, render(t, "restricted", values), "Deployment", "logchef"))
	for _, expected := range []string{`"runAsNonRoot":true`, `"type":"RuntimeDefault"`, `"allowPrivilegeEscalation":false`, `"drop":["ALL"]`} {
		if !strings.Contains(deployment, expected) {
			t.Errorf("restricted settings lack %s", expected)
		}
	}
	if strings.Contains(deployment, "initContainers") {
		t.Error("root init container was not disabled")
	}
	zeroGroup := render(t, "zero-group", "logchef:\n  podSecurityContext:\n    fsGroup: 0\n")
	if !strings.Contains(jsonSpec(t, find(t, zeroGroup, "Deployment", "logchef")), "chown -R 0:0 /data") {
		t.Error("permissions init container ignored fsGroup zero")
	}
	output, err = helmCommand(t, []string{"template", "no-demo", ".", "--set", "logchef.auth.existingSecret=test-api-token"}, "")
	if err == nil || !bytes.Contains(output, []byte("No demo accounts are installed")) {
		t.Error("Dex should require an explicitly configured login provider")
	}
	first := render(t, "stable", "")
	second := render(t, "stable", "")
	if len(first) != len(second) {
		t.Fatal("offline render changed the resource count")
	}
	for i := range first {
		if first[i].Kind == "Secret" && first[i].Metadata.Name == "test-api-token" {
			t.Error("chart should not manage an externally supplied API-token Secret")
		}
		left, err := json.Marshal(first[i])
		if err != nil {
			t.Fatal(err)
		}
		right, err := json.Marshal(second[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, right) {
			t.Errorf("offline render changed %s", first[i].Metadata.Name)
		}
	}
}
