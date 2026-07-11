package core

import (
	"context"
	"sync"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// TestGetOrCreateAutoProvisionedUser_Creates verifies the happy path: a
// first-time auto-provisioned user is created as an active, unmanaged member.
func TestGetOrCreateAutoProvisionedUser_Creates(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	user, err := GetOrCreateAutoProvisionedUser(ctx, db, log, "new-hire@example.com", "New Hire")
	if err != nil {
		t.Fatalf("GetOrCreateAutoProvisionedUser: %v", err)
	}
	if user.Role != models.UserRoleMember {
		t.Errorf("role = %q, want %q", user.Role, models.UserRoleMember)
	}
	if user.Status != models.UserStatusActive {
		t.Errorf("status = %q, want %q", user.Status, models.UserStatusActive)
	}
	if user.AccountType != models.UserAccountTypeHuman {
		t.Errorf("account_type = %q, want %q", user.AccountType, models.UserAccountTypeHuman)
	}
	if managed, err := db.IsUserManaged(ctx, user.ID); err != nil || managed {
		t.Errorf("auto-provisioned user must be unmanaged, managed=%v err=%v", managed, err)
	}
}

// TestGetOrCreateAutoProvisionedUser_ConflictRefetches ensures that when the
// email already exists (e.g. a second first-login racing the first), the
// unique constraint conflict is handled by re-fetching the existing row
// rather than erroring out.
func TestGetOrCreateAutoProvisionedUser_ConflictRefetches(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	existing := newTestUser(t, db, "already-here@example.com", "Already Here")

	got, err := GetOrCreateAutoProvisionedUser(ctx, db, log, "already-here@example.com", "Different Name From Claims")
	if err != nil {
		t.Fatalf("GetOrCreateAutoProvisionedUser: %v", err)
	}
	if got.ID != existing.ID {
		t.Errorf("expected the pre-existing user (id=%d), got id=%d", existing.ID, got.ID)
	}
	// The pre-existing row must not have been clobbered.
	if got.FullName != existing.FullName {
		t.Errorf("full_name = %q, want unchanged %q", got.FullName, existing.FullName)
	}
}

// TestGetOrCreateAutoProvisionedUser_ConcurrentLoginsBothSucceed exercises
// the actual race the spec calls out: two concurrent first logins for the
// same email must both succeed and resolve to the same user row.
func TestGetOrCreateAutoProvisionedUser_ConcurrentLoginsBothSucceed(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	users := make([]*models.User, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			users[i], errs[i] = GetOrCreateAutoProvisionedUser(ctx, db, log, "racer@example.com", "Racer")
		}(i)
	}
	wg.Wait()

	var firstID models.UserID
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: GetOrCreateAutoProvisionedUser: %v", i, errs[i])
		}
		if i == 0 {
			firstID = users[i].ID
			continue
		}
		if users[i].ID != firstID {
			t.Errorf("goroutine %d resolved to user id %d, want %d (all concurrent logins must resolve to the same user)", i, users[i].ID, firstID)
		}
	}
}
