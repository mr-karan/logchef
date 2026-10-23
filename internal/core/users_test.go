package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestAuthorizeAdminTransition(t *testing.T) {
	activeAdmin := models.User{Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	inactiveAdmin := models.User{Role: models.UserRoleAdmin, Status: models.UserStatusInactive}
	member := models.User{Role: models.UserRoleMember, Status: models.UserStatusActive}
	scoped := access.APITokenPrincipal(nil, &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeUsersWrite}})
	for _, tc := range []struct {
		name      string
		principal access.Principal
		before    models.User
		after     models.User
		denied    bool
	}{
		{"scoped create admin", scoped, models.User{}, activeAdmin, true},
		{"scoped create member", scoped, models.User{}, member, false},
		{"scoped promote", scoped, member, activeAdmin, true},
		{"scoped reactivate", scoped, inactiveAdmin, activeAdmin, true},
		{"scoped reactivate and promote", scoped, models.User{Role: models.UserRoleMember, Status: models.UserStatusInactive}, activeAdmin, true},
		{"scoped edit active admin", scoped, activeAdmin, activeAdmin, false},
		{"scoped deactivate admin", scoped, activeAdmin, inactiveAdmin, false},
		{"scoped demote admin", scoped, activeAdmin, member, false},
		{"scoped inactive admin", scoped, member, inactiveAdmin, false},
		{"wildcard reactivate", access.APITokenPrincipal(nil, &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeAll}}), inactiveAdmin, activeAdmin, false},
		{"session reactivate", access.SessionPrincipal(&models.User{ID: 1}), inactiveAdmin, activeAdmin, false},
		{"system reactivate", access.SystemPrincipal(), inactiveAdmin, activeAdmin, false},
		{"missing token", access.APITokenPrincipal(nil, nil), inactiveAdmin, activeAdmin, true},
		{"unknown method", access.Principal{}, inactiveAdmin, activeAdmin, true},
		{"oauth wildcard", access.OAuthPrincipal(nil, 1, "client", []models.TokenScope{models.TokenScopeAll}), inactiveAdmin, activeAdmin, true},
		{"assistant", access.AssistantPrincipal(nil), inactiveAdmin, activeAdmin, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := AuthorizeAdminTransition(tc.principal, tc.before, tc.after)
			if (err != nil) != tc.denied {
				t.Fatalf("authorization error = %v, denied = %t", err, tc.denied)
			}
		})
	}
}

// newTestAdmin creates an active admin user row.
func newTestAdmin(t *testing.T, db *sqlite.DB, email string) *models.User {
	t.Helper()
	u := &models.User{Email: email, FullName: "Admin " + email, Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return u
}

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

// TestDeactivateUserRevokesSessions guards #90: deactivating a user must revoke
// their live sessions in the same operation.
func TestDeactivateUserRevokesSessions(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	user := newTestUser(t, db, "deactivate-me@example.com", "Deactivate Me")
	sess := &models.Session{ID: models.SessionID("to-revoke-1"), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := UpdateUser(ctx, db, log, access.SystemPrincipal(), user.ID, models.User{Status: models.UserStatusInactive}); err != nil {
		t.Fatalf("UpdateUser(deactivate): %v", err)
	}

	if n, err := db.CountUserSessions(ctx, user.ID); err != nil || n != 0 {
		t.Fatalf("CountUserSessions after deactivate = %d / %v, want 0", n, err)
	}
	got, err := db.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Status != models.UserStatusInactive {
		t.Fatalf("status = %q, want inactive", got.Status)
	}
}

// TestDeactivateLastAdminRejected verifies the last-admin guard still fires for
// status changes (#96, sequential case).
func TestDeactivateLastAdminRejected(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	a := newTestAdmin(t, db, "admin-a@example.com")
	b := newTestAdmin(t, db, "admin-b@example.com")

	if err := UpdateUser(ctx, db, log, access.SystemPrincipal(), a.ID, models.User{Status: models.UserStatusInactive}); err != nil {
		t.Fatalf("deactivating first admin should succeed: %v", err)
	}
	if err := UpdateUser(ctx, db, log, access.SystemPrincipal(), b.ID, models.User{Status: models.UserStatusInactive}); !errors.Is(err, ErrCannotDeleteLastAdmin) {
		t.Fatalf("deactivating last admin: err = %v, want ErrCannotDeleteLastAdmin", err)
	}
	if n, err := db.CountAdminUsers(ctx); err != nil || n != 1 {
		t.Fatalf("active admin count = %d / %v, want 1", n, err)
	}
}

// TestDeleteLastAdminRejected verifies the last-admin guard fires for deletes
// (#96, sequential case).
func TestDeleteLastAdminRejected(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	a := newTestAdmin(t, db, "del-admin-a@example.com")
	b := newTestAdmin(t, db, "del-admin-b@example.com")

	if err := DeleteUser(ctx, db, log, a.ID); err != nil {
		t.Fatalf("deleting first admin should succeed: %v", err)
	}
	if err := DeleteUser(ctx, db, log, b.ID); !errors.Is(err, ErrCannotDeleteLastAdmin) {
		t.Fatalf("deleting last admin: err = %v, want ErrCannotDeleteLastAdmin", err)
	}
}

// TestConcurrentAdminDeactivationKeepsOneAdmin is the TOCTOU regression test for
// #96: with exactly two active admins, two concurrent deactivations of the two
// different admins must not both succeed and leave the org with zero admins.
// Because the guard is re-checked inside the write transaction (and SQLite
// serializes writers), exactly one deactivation succeeds and one is rejected.
func TestConcurrentAdminDeactivationKeepsOneAdmin(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	a := newTestAdmin(t, db, "race-admin-a@example.com")
	b := newTestAdmin(t, db, "race-admin-b@example.com")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	targets := []models.UserID{a.ID, b.ID}
	wg.Add(2)
	for i := range targets {
		go func() {
			defer wg.Done()
			errs[i] = UpdateUser(ctx, db, log, access.SystemPrincipal(), targets[i], models.User{Status: models.UserStatusInactive})
		}()
	}
	wg.Wait()

	var success, lastAdmin int
	for _, err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrCannotDeleteLastAdmin):
			lastAdmin++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || lastAdmin != 1 {
		t.Fatalf("outcomes: success=%d lastAdmin=%d, want 1/1", success, lastAdmin)
	}
	if n, err := db.CountAdminUsers(ctx); err != nil || n != 1 {
		t.Fatalf("final active admin count = %d / %v, want 1 (never zero)", n, err)
	}
}

// TestEmailNormalizedOnCreateAndLookup covers #95: emails are stored lowercased
// and looked up case-insensitively.
func TestEmailNormalizedOnCreateAndLookup(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	u := &models.User{Email: "Mixed.Case@Example.COM", FullName: "Mixed Case", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Email != "mixed.case@example.com" {
		t.Fatalf("stored email = %q, want lowercased", u.Email)
	}

	for _, lookup := range []string{"mixed.case@example.com", "Mixed.Case@Example.com", "MIXED.CASE@EXAMPLE.COM", "  mixed.case@example.com  "} {
		got, err := db.GetUserByEmail(ctx, lookup)
		if err != nil {
			t.Fatalf("GetUserByEmail(%q): %v", lookup, err)
		}
		if got.ID != u.ID {
			t.Fatalf("GetUserByEmail(%q) returned id %d, want %d", lookup, got.ID, u.ID)
		}
	}

	// A case-variant create must collide with the existing (lowercased) row.
	dup := &models.User{Email: "MIXED.CASE@example.com", FullName: "Dup", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, dup); !errors.Is(err, models.ErrConflict) {
		t.Fatalf("duplicate case-variant create: err = %v, want ErrConflict", err)
	}
}

func TestDemoteAdmin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		otherAdmin bool
		deactivate bool
		wantErr    error
	}{
		{"sole admin", false, false, ErrCannotDeleteLastAdmin},
		{"one of two admins", true, false, nil},
		{"sole admin demoted and deactivated", false, true, ErrCannotDeleteLastAdmin},
		{"one of two admins demoted and deactivated", true, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			ctx := context.Background()
			admin := newTestAdmin(t, db, "admin@example.com")
			if tc.otherAdmin {
				newTestAdmin(t, db, "other@example.com")
			}
			session := &models.Session{ID: "admin-session", UserID: admin.ID, ExpiresAt: time.Now().Add(time.Hour)}
			if err := db.CreateSession(ctx, session); err != nil {
				t.Fatal(err)
			}
			update := models.User{Role: models.UserRoleMember}
			if tc.deactivate {
				update.Status = models.UserStatusInactive
			}
			err := UpdateUser(ctx, db, discardLogger(), access.SystemPrincipal(), admin.ID, update)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("UpdateUser = %v, want %v", err, tc.wantErr)
			}
			got, err := db.GetUser(ctx, admin.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantRole := models.UserRoleMember
			if tc.wantErr != nil {
				wantRole = models.UserRoleAdmin
			}
			if got.Role != wantRole {
				t.Fatalf("role = %q, want %q", got.Role, wantRole)
			}
			if count, err := db.CountAdminUsers(ctx); err != nil || count != 1 {
				t.Fatalf("active admins = %d, %v, want 1", count, err)
			}
			wantSessions := 1
			if tc.wantErr == nil && tc.deactivate {
				wantSessions = 0
			}
			if count, err := db.CountUserSessions(ctx, admin.ID); err != nil || count != wantSessions {
				t.Fatalf("sessions = %d, %v, want %d", count, err, wantSessions)
			}
		})
	}
}

func TestConcurrentAdminDemotionKeepsOneAdmin(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	admins := []*models.User{
		newTestAdmin(t, db, "admin-a@example.com"),
		newTestAdmin(t, db, "admin-b@example.com"),
	}
	var wg sync.WaitGroup
	errs := make([]error, len(admins))
	wg.Add(len(admins))
	for i, admin := range admins {
		go func() {
			defer wg.Done()
			errs[i] = UpdateUser(ctx, db, discardLogger(), access.SystemPrincipal(), admin.ID, models.User{Role: models.UserRoleMember})
		}()
	}
	wg.Wait()
	var success, rejected int
	for _, err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrCannotDeleteLastAdmin):
			rejected++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d, want 1/1", success, rejected)
	}
	if count, err := db.CountAdminUsers(ctx); err != nil || count != 1 {
		t.Fatalf("active admins = %d, %v, want 1", count, err)
	}
}

func TestCreateUserAdminGrant(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principal access.Principal
		role      models.UserRole
		status    models.UserStatus
		denied    bool
	}{
		{"system admin", access.SystemPrincipal(), models.UserRoleAdmin, "", false},
		{"wildcard admin", access.APITokenPrincipal(nil, &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeAll}}), models.UserRoleAdmin, "", false},
		{"session admin", access.SessionPrincipal(&models.User{ID: 1}), models.UserRoleAdmin, "", false},
		{"unknown admin", access.Principal{}, models.UserRoleAdmin, "", true},
		{"scoped admin defaults active", access.APITokenPrincipal(nil, &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeUsersWrite}}), models.UserRoleAdmin, "", true},
		{"scoped inactive admin", access.APITokenPrincipal(nil, &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeUsersWrite}}), models.UserRoleAdmin, models.UserStatusInactive, false},
		{"scoped default member", access.APITokenPrincipal(nil, &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeUsersWrite}}), "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			email := "created@example.com"
			user, err := CreateUser(context.Background(), db, discardLogger(), tc.principal, email, "Created User", tc.role, tc.status)
			if tc.denied {
				if !errors.Is(err, ErrAdminGrantForbidden) {
					t.Fatalf("CreateUser error = %v, want ErrAdminGrantForbidden", err)
				}
				users, err := db.ListUsers(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if len(users) != 0 {
					t.Fatalf("denied creation persisted %d users", len(users))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := db.GetUser(context.Background(), user.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantRole, wantStatus := tc.role, tc.status
			if wantRole == "" {
				wantRole = models.UserRoleMember
			}
			if wantStatus == "" {
				wantStatus = models.UserStatusActive
			}
			if stored.Role != wantRole || stored.Status != wantStatus {
				t.Fatalf("stored role/status = %s/%s, want %s/%s", stored.Role, stored.Status, wantRole, wantStatus)
			}
		})
	}
}
