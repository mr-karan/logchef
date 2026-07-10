package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// TestDeleteUserRemovesSessions guards the fix for #53: a deleted user must
// not retain a live session. DeleteUser deletes the user row and the user's
// session rows in the same transaction.
func TestDeleteUserRemovesSessions(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	user := newTestUser(t, db, "sessions-victim@example.com", "Sessions Victim")

	sess := &models.Session{ID: models.SessionID("victim-session-1"), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if n, err := db.CountUserSessions(ctx, user.ID); err != nil || n != 1 {
		t.Fatalf("precondition: CountUserSessions = %d / %v, want 1", n, err)
	}

	if err := DeleteUser(ctx, db, log, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := db.GetSession(ctx, sess.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("session should be deleted along with the user, got err: %v", err)
	}
	if _, err := db.GetUser(ctx, user.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("user should be deleted, got err: %v", err)
	}
}

// TestDeleteUserOfOtherUsersSessionsUnaffected ensures the session sweep is
// scoped to the deleted user: a bystander's session must survive.
func TestDeleteUserOfOtherUsersSessionsUnaffected(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	victim := newTestUser(t, db, "victim2@example.com", "Victim Two")
	bystander := newTestUser(t, db, "bystander@example.com", "Bystander")

	bystanderSession := &models.Session{ID: models.SessionID("bystander-session-1"), UserID: bystander.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.CreateSession(ctx, bystanderSession); err != nil {
		t.Fatalf("CreateSession(bystander): %v", err)
	}

	if err := DeleteUser(ctx, db, log, victim.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := db.GetSession(ctx, bystanderSession.ID); err != nil {
		t.Errorf("bystander session should survive, got err: %v", err)
	}
}
