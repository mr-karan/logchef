package storetest

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testSourceHeaderSecretRefs checks that both stores keep the header
// references of a source through create, list and update, and clear them.
func testSourceHeaderSecretRefs(t *testing.T, ctx context.Context, s store.Store) {
	refs := map[string]string{"x-api-key": "PROXY_KEY", "x-org": "ORG_KEY"}
	src := &models.Source{
		Name:             "VL Header Refs",
		SourceType:       models.SourceTypeVictoriaLogs,
		MetaTSField:      "_time",
		ConnectionConfig: []byte(`{"base_url":"http://vl.test:9428","headers":{"X-Api-Key":"k","X-Org":"o"}}`),
		HeaderSecretRefs: refs,
	}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteSource(ctx, src.ID) })

	got, err := s.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if !maps.Equal(got.HeaderSecretRefs, refs) {
		t.Fatalf("GetSource header refs = %v, want %v", got.HeaderSecretRefs, refs)
	}
	listed, err := s.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	i := slices.IndexFunc(listed, func(l *models.Source) bool { return l.ID == src.ID })
	if i < 0 {
		t.Fatalf("ListSources omits source %d", src.ID)
	}
	if !maps.Equal(listed[i].HeaderSecretRefs, refs) {
		t.Fatalf("ListSources header refs = %v, want %v", listed[i].HeaderSecretRefs, refs)
	}

	got.HeaderSecretRefs = map[string]string{"x-api-key": "PROXY_KEY_V2"}
	if err := s.UpdateSource(ctx, got); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	if again, err := s.GetSource(ctx, src.ID); err != nil || !maps.Equal(again.HeaderSecretRefs, got.HeaderSecretRefs) {
		t.Fatalf("after update: %v, header refs %v, want %v", err, again.HeaderSecretRefs, got.HeaderSecretRefs)
	}

	got.HeaderSecretRefs = nil
	if err := s.UpdateSource(ctx, got); err != nil {
		t.Fatalf("UpdateSource(clear): %v", err)
	}
	if cleared, err := s.GetSource(ctx, src.ID); err != nil || cleared.HeaderSecretRefs != nil {
		t.Fatalf("after clearing: %v, header refs %v, want none", err, cleared.HeaderSecretRefs)
	}
}
