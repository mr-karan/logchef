package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func newServerTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "test.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newSessionTestServer(t *testing.T, db *sqlite.DB) *fiber.App {
	t.Helper()
	s := &Server{
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		sqlite: db,
		config: &config.Config{},
	}
	app := fiber.New()
	app.Get("/protected", s.requireAuth, func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func reqWithSession(sessionID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/protected", http.NoBody)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	return r
}

// TestSessionRejectedAfterDeactivation is the integration test for #90: an
// active browser session must stop working the moment the account is
// deactivated, rather than remaining valid until the session's natural expiry.
func TestSessionRejectedAfterDeactivation(t *testing.T) {
	t.Parallel()
	db := newServerTestDB(t)
	ctx := context.Background()

	user := &models.User{Email: "victim@example.com", FullName: "Victim", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sess := &models.Session{ID: models.SessionID("live-session-1"), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	app := newSessionTestServer(t, db)

	// Active user + live session => authorized.
	resp, err := app.Test(reqWithSession(string(sess.ID)))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("pre-deactivation status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Deactivate through the core flow (revokes sessions transactionally).
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := core.UpdateUser(ctx, db, log, user.ID, models.User{Status: models.UserStatusInactive}); err != nil {
		t.Fatalf("UpdateUser(deactivate): %v", err)
	}

	// #90: sessions must have been revoked as part of the deactivation.
	if n, err := db.CountUserSessions(ctx, user.ID); err != nil || n != 0 {
		t.Fatalf("CountUserSessions after deactivate = %d / %v, want 0", n, err)
	}

	// Next request with the same cookie => 401.
	resp2, err := app.Test(reqWithSession(string(sess.ID)))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("post-deactivation status = %d, want 401", resp2.StatusCode)
	}
}

// TestSessionRejectedForInactiveUserWithLiveSession isolates the middleware
// status check (#90): even if a session row still exists (status flipped
// directly at the store, no revocation), the middleware must reject it.
func TestSessionRejectedForInactiveUserWithLiveSession(t *testing.T) {
	t.Parallel()
	db := newServerTestDB(t)
	ctx := context.Background()

	user := &models.User{Email: "inactive@example.com", FullName: "Inactive", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sess := &models.Session{ID: models.SessionID("live-session-2"), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Flip status directly at the store so the session row survives.
	user.Status = models.UserStatusInactive
	if err := db.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser(store): %v", err)
	}

	app := newSessionTestServer(t, db)
	resp, err := app.Test(reqWithSession(string(sess.ID)))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for inactive user with live session", resp.StatusCode)
	}
}

// TestSessionRejectedForServiceAccount ensures service accounts cannot ride a
// browser session (#90).
func TestSessionRejectedForServiceAccount(t *testing.T) {
	t.Parallel()
	db := newServerTestDB(t)
	ctx := context.Background()

	user := &models.User{Email: "svc@example.com", FullName: "Service", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeService}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sess := &models.Session{ID: models.SessionID("svc-session-1"), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	app := newSessionTestServer(t, db)
	resp, err := app.Test(reqWithSession(string(sess.ID)))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for service account session", resp.StatusCode)
	}
}
