package storetest

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// LoggerOpener opens a fresh, migrated store that logs to logger.
type LoggerOpener func(t *testing.T, logger *slog.Logger) store.Store

// RunShareLogRedaction asserts the query-share store methods never write a full
// share token to the log, on the success path and on database failures.
func RunShareLogRedaction(t *testing.T, open LoggerOpener) {
	ctx := context.Background()
	var logs bytes.Buffer
	s := open(t, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	owner := mkUser(t, ctx, s, "share-logs@test.dev")
	src := mkSource(t, ctx, s, "share_logs")
	const token = "s3cr3tshare-0123456789abcdef"
	share := &models.QueryShare{
		Token:     token,
		SourceID:  src.ID,
		CreatedBy: owner.ID,
		Payload:   []byte(`{}`),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}
	if err := s.CreateQueryShare(ctx, share); err != nil {
		t.Fatalf("CreateQueryShare: %v", err)
	}
	if _, err := s.GetQueryShare(ctx, token); err != nil {
		t.Fatalf("GetQueryShare: %v", err)
	}
	if err := s.TouchQueryShare(ctx, token, time.Now().UTC()); err != nil {
		t.Fatalf("TouchQueryShare: %v", err)
	}
	if err := s.DeleteQueryShare(ctx, token); err != nil {
		t.Fatalf("DeleteQueryShare: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.GetQueryShare(ctx, token); err == nil {
		t.Error("GetQueryShare on closed store succeeded")
	}
	if err := s.TouchQueryShare(ctx, token, time.Now().UTC()); err == nil {
		t.Error("TouchQueryShare on closed store succeeded")
	}
	if err := s.DeleteQueryShare(ctx, token); err == nil {
		t.Error("DeleteQueryShare on closed store succeeded")
	}

	out := logs.String()
	if !strings.Contains(out, "failed to touch query share") {
		t.Fatalf("no error log captured, test would pass vacuously:\n%s", out)
	}
	if strings.Contains(out, token) {
		t.Errorf("log output contains the full share token:\n%s", out)
	}
}
