package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSessionLogsDoNotLeakCredentials(t *testing.T) {
	db := newTestDB(t)
	user := newTestUser(t, db, "session-log@example.com", "Session User")
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()
	session, err := CreateSession(ctx, db, log, user.ID, time.Hour, 5)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), string(session.ID)) {
		t.Error("creation log contains full session ID")
	}
	logs.Reset()
	if err := RevokeSession(ctx, db, log, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateSession(ctx, db, log, session.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session error = %v", err)
	}
	if strings.Contains(logs.String(), string(session.ID)) {
		t.Error("revocation or missing-session log contains full session ID")
	}
	logs.Reset()
	session.ExpiresAt = time.Now().Add(-time.Hour)
	if err := db.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateSession(ctx, db, log, session.ID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired session error = %v", err)
	}
	if strings.Contains(logs.String(), string(session.ID)) {
		t.Error("expiration log contains full session ID")
	}
	logs.Reset()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateSession(ctx, db, log, session.ID); err == nil {
		t.Fatal("closed database did not fail validation")
	}
	if err := RevokeSession(ctx, db, log, session.ID); err == nil {
		t.Fatal("closed database did not fail revocation")
	}
	if strings.Contains(logs.String(), string(session.ID)) {
		t.Error("database-error log contains full session ID")
	}
}
