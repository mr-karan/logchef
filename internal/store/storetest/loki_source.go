package storetest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testLokiSource checks that both stores accept source_type loki (RFC 0011),
// keep its connection_config, and store a logql saved query on it.
func testLokiSource(t *testing.T, ctx context.Context, s store.Store) {
	conn := models.LokiConnectionInfo{
		BaseURL:   "http://loki.test:3100",
		Auth:      models.LokiAuth{Mode: "bearer", Token: "loki-token"},
		TenantID:  "dev",
		Selector:  `{service_name=~".+"}`,
		Principal: "payments-readonly",
		Parser:    "json",
		Headers:   map[string]string{"X-Gateway-Key": "gateway-secret"},
	}
	raw, err := json.Marshal(conn)
	if err != nil {
		t.Fatalf("marshal loki connection: %v", err)
	}
	src := &models.Source{
		Name:              "Loki Demo",
		SourceType:        models.SourceTypeLoki,
		MetaTSField:       "_time",
		MetaSeverityField: "detected_level",
		ConnectionConfig:  raw,
	}
	if err := src.SyncConnectionConfig(); err != nil {
		t.Fatalf("SyncConnectionConfig: %v", err)
	}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource(loki): %v", err)
	}

	byKey, err := s.GetSourceByIdentityKey(ctx, models.BuildLokiIdentityKey(conn))
	if err != nil || byKey.ID != src.ID {
		t.Fatalf("GetSourceByIdentityKey: %v / %+v", err, byKey)
	}
	got, err := s.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSource(loki): %v", err)
	}
	if !got.IsLoki() || got.MetaTSField != "_time" || got.MetaSeverityField != "detected_level" {
		t.Fatalf("GetSource(loki) = type %q ts %q severity %q", got.SourceType, got.MetaTSField, got.MetaSeverityField)
	}
	stored, err := got.LokiConnection()
	if err != nil {
		t.Fatalf("LokiConnection: %v", err)
	}
	if stored.Selector != conn.Selector || stored.TenantID != conn.TenantID || stored.Principal != conn.Principal ||
		stored.Auth.Token != conn.Auth.Token || stored.Headers["X-Gateway-Key"] != "gateway-secret" {
		t.Fatalf("stored connection = %+v, want %+v", stored, conn)
	}

	q, err := s.CreateSavedQuery(ctx, src.ID, nil, "errors", "", models.QueryLanguageLogQL, models.SavedQueryEditorModeNative, `{"content":"{service_name=\"api\"} |= \"error\""}`, nil)
	if err != nil {
		t.Fatalf("CreateSavedQuery(logql): %v", err)
	}
	if q.QueryLanguage != models.QueryLanguageLogQL {
		t.Fatalf("saved query language = %q, want logql", q.QueryLanguage)
	}

	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("DeleteSource(loki): %v", err)
	}
}
