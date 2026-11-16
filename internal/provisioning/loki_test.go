package provisioning

import (
	"context"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

// Two sources that reach one URL, tenant and selector through different
// gateway credentials differ only in principal, and both must load.
func TestReconcile_LokiSourcesDifferingOnlyInPrincipal(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()

	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources: []config.ProvisionSource{
			lokiProvisionSource("payments", "payments-readonly", nil),
			lokiProvisionSource("everything", "platform-admin", nil),
		},
	}
	if err := Reconcile(ctx, cfg, db, newTestDatasourceService(db), quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	payments := findSource(t, db, "payments")
	everything := findSource(t, db, "everything")
	if payments == nil || everything == nil {
		t.Fatalf("both sources should exist: payments=%v everything=%v", payments, everything)
	}
	if payments.SourceType != models.SourceTypeLoki || everything.SourceType != models.SourceTypeLoki {
		t.Fatalf("source types %q and %q, want loki", payments.SourceType, everything.SourceType)
	}
	if payments.IdentityKey == everything.IdentityKey {
		t.Fatalf("identity keys should differ, both are %q", payments.IdentityKey)
	}
}

// Export then import keeps the principal, writes the secret as a secret_ref
// rather than its value, and recreates the same source.
func TestExportConfig_LokiRoundTrip(t *testing.T) {
	t.Setenv("LOKI_DEMO_TOKEN", "tok-123")
	ctx := context.Background()

	src := lokiProvisionSource("Loki Demo", "payments-readonly", map[string]any{"mode": "bearer"})
	src.SecretRef = "LOKI_DEMO_TOKEN"
	src.Connection["parser"] = "json"
	src.Description = "dev loki"

	first := newReconcileTestDB(t)
	cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{src}}
	if err := Reconcile(ctx, cfg, first, newTestDatasourceService(first), quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	original := findSource(t, first, "Loki Demo")
	if original == nil {
		t.Fatal("source should have been created")
	}

	exported, err := ExportConfig(ctx, first)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if len(exported.Sources) != 1 {
		t.Fatalf("exported %d sources, want 1", len(exported.Sources))
	}
	out := exported.Sources[0]
	if out.SourceType != models.SourceTypeLoki || out.SecretRef != "LOKI_DEMO_TOKEN" {
		t.Errorf("exported source_type=%q secret_ref=%q", out.SourceType, out.SecretRef)
	}
	conn, err := out.LokiConnection()
	if err != nil {
		t.Fatal(err)
	}
	if conn.Principal != "payments-readonly" {
		t.Errorf("exported principal %q, want payments-readonly", conn.Principal)
	}
	if conn.Auth.Token != "" || conn.Auth.Password != "" {
		t.Errorf("export must not hold the secret: %+v", conn.Auth)
	}

	second := newReconcileTestDB(t)
	exported.ManageTeams = false
	if err := Reconcile(ctx, exported, second, newTestDatasourceService(second), quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile exported config: %v", err)
	}
	imported := findSource(t, second, "Loki Demo")
	if imported == nil {
		t.Fatal("imported source should exist")
	}
	if imported.IdentityKey != original.IdentityKey {
		t.Errorf("identity key %q after round trip, want %q", imported.IdentityKey, original.IdentityKey)
	}
	if string(imported.ConnectionConfig) != string(original.ConnectionConfig) {
		t.Errorf("connection config after round trip:\n got %s\nwant %s", imported.ConnectionConfig, original.ConnectionConfig)
	}
	if imported.MetaTSField != original.MetaTSField || imported.MetaSeverityField != original.MetaSeverityField ||
		imported.Description != original.Description || imported.SecretRef != original.SecretRef {
		t.Errorf("source fields after round trip: %+v, want %+v", imported, original)
	}
}

func TestReconcile_LokiIdentityIsImmutable(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	reconcile := func(src config.ProvisionSource) error {
		cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{src}}
		return Reconcile(ctx, cfg, db, newTestDatasourceService(db), quietLogger(), nil)
	}

	if err := reconcile(lokiProvisionSource("payments", "payments-readonly", nil)); err != nil {
		t.Fatalf("create: %v", err)
	}
	created := findSource(t, db, "payments")

	parserChange := lokiProvisionSource("payments", "payments-readonly", nil)
	parserChange.Connection["parser"] = "logfmt"
	if err := reconcile(parserChange); err != nil {
		t.Fatalf("a parser change should update the source: %v", err)
	}
	conn, err := findSource(t, db, "payments").LokiConnection()
	if err != nil {
		t.Fatal(err)
	}
	if conn.Parser != "logfmt" {
		t.Fatalf("parser = %q after update, want logfmt", conn.Parser)
	}

	tenantChange := lokiProvisionSource("payments", "payments-readonly", nil)
	tenantChange.Connection["tenant_id"] = "platform"
	err = reconcile(tenantChange)
	if err == nil || !strings.Contains(err.Error(), "cannot change after creation") {
		t.Fatalf("a tenant change = %v, want the immutability error", err)
	}
	if got := findSource(t, db, "payments").IdentityKey; got != created.IdentityKey {
		t.Fatalf("identity key changed to %q, want %q", got, created.IdentityKey)
	}
}

func TestReconcile_LokiAdoptionKeepsIdentity(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()

	existing := &models.Source{
		Name:             "payments",
		SourceType:       models.SourceTypeLoki,
		MetaTSField:      "_time",
		ConnectionConfig: []byte(`{"base_url":"http://127.0.0.1:3100","tenant_id":"dev","selector":"{namespace=\"payments\"}"}`),
	}
	if err := existing.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(ctx, existing); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		Sources:       []config.ProvisionSource{lokiProvisionSource("payments", "", nil)},
	}
	err := Reconcile(ctx, cfg, db, newTestDatasourceService(db), quietLogger(), nil)
	if err == nil || !strings.Contains(err.Error(), "cannot change after creation") {
		t.Fatalf("adopting with another selector = %v, want the immutability error", err)
	}
}
