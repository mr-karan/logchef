package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// strictAuditStore fails the test on a success event whose handler staged no
// target, or no fields for a Staged action.
type strictAuditStore struct {
	store.AuditStore
	t testing.TB
}

func (s strictAuditStore) InsertAuditEvent(ctx context.Context, e *models.AuditEvent) error {
	var detail map[string]any
	if err := json.Unmarshal(e.Detail, &detail); err != nil {
		s.t.Errorf("audit event %s has detail %s: %v", e.Action, e.Detail, err)
	}
	if detail["unstaged"] == true {
		s.t.Errorf("audit event %s on %s %s: the handler staged no target or fields", e.Action, e.HTTPMethod, e.Route)
	}
	return s.AuditStore.InsertAuditEvent(ctx, e)
}

func testAuditRecorder(t testing.TB, st store.AuditStore) *audit.Recorder {
	return audit.New(audit.Options{
		Store:  strictAuditStore{AuditStore: st, t: t},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}
