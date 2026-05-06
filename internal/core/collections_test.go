package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// newTestDB spins up a fresh on-disk SQLite DB with all migrations applied.
// The file lives in t.TempDir() so go test cleans up automatically.
func newTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlite.New(sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: dbPath},
	})
	if err != nil {
		t.Fatalf("sqlite.New failed: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("warning: db.Close failed: %v", err)
		}
	})
	return db
}

// newTestUser creates a user row and returns the populated *models.User.
func newTestUser(t *testing.T, db *sqlite.DB, email, fullName string) *models.User {
	t.Helper()
	user := &models.User{Email: email, FullName: fullName, Role: models.UserRoleMember, Status: "active"}
	if err := db.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return user
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEnsurePersonalCollectionIdempotent(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	user := newTestUser(t, db, "ada@example.com", "Ada Lovelace")

	// First call creates the row.
	first, err := EnsurePersonalCollection(context.Background(), db, log, user)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if !first.IsPersonal {
		t.Errorf("expected is_personal=true, got false")
	}
	if first.CallerRole != models.CollectionRoleOwner {
		t.Errorf("expected caller_role=owner, got %q", first.CallerRole)
	}

	// Second call returns the same row, doesn't create another.
	second, err := EnsurePersonalCollection(context.Background(), db, log, user)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("idempotency broken: first.ID=%d, second.ID=%d", first.ID, second.ID)
	}

	// And the unique partial index is real — only one personal collection exists.
	collections, err := db.ListCollectionsForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("ListCollectionsForUser failed: %v", err)
	}
	personalCount := 0
	for _, c := range collections {
		if c.IsPersonal {
			personalCount++
		}
	}
	if personalCount != 1 {
		t.Errorf("expected exactly one personal collection, found %d", personalCount)
	}
}

func TestEnsurePersonalCollectionRaceRecovery(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	user := newTestUser(t, db, "grace@example.com", "Grace Hopper")

	// Simulate the race: another goroutine has already created the row before
	// our EnsurePersonalCollection call would naturally hit GetPersonalCollection.
	// We do this by inserting directly via the DB layer — by the time the next
	// call runs, the row exists and the function should just return it.
	preexisting, err := db.CreateCollection(context.Background(), "My Collection", "", true, user.ID)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
	if err := db.AddCollectionMember(context.Background(), preexisting.ID, user.ID, models.CollectionRoleOwner, &user.ID); err != nil {
		t.Fatalf("AddCollectionMember failed: %v", err)
	}

	got, err := EnsurePersonalCollection(context.Background(), db, log, user)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection on pre-existing row failed: %v", err)
	}
	if got.ID != preexisting.ID {
		t.Errorf("expected to return the pre-existing collection (id %d), got id %d",
			preexisting.ID, got.ID)
	}
}

func TestRemoveCollectionMemberLastOwnerGuard(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	owner := newTestUser(t, db, "owner@example.com", "Owner")
	memberUser := newTestUser(t, db, "member@example.com", "Member")

	collection, err := CreateCollection(context.Background(), db, log, "Shared", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
	// Add a regular member via core (idempotent + owner-only enforcement).
	if err := AddCollectionMember(context.Background(), db, log, collection.ID, owner.ID, memberUser.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("AddCollectionMember failed: %v", err)
	}

	// Case 1: owner removes a non-owner member — allowed.
	if err := RemoveCollectionMember(context.Background(), db, log, collection.ID, owner.ID, memberUser.ID); err != nil {
		t.Fatalf("removing non-owner member by owner failed: %v", err)
	}

	// Re-add the member to set up the next cases.
	if err := AddCollectionMember(context.Background(), db, log, collection.ID, owner.ID, memberUser.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("re-add member failed: %v", err)
	}

	// Case 2: a member self-leaves — allowed.
	if err := RemoveCollectionMember(context.Background(), db, log, collection.ID, memberUser.ID, memberUser.ID); err != nil {
		t.Fatalf("self-leave by member failed: %v", err)
	}

	// Case 3: the only owner tries to self-remove — last-owner guard fires.
	err = RemoveCollectionMember(context.Background(), db, log, collection.ID, owner.ID, owner.ID)
	if !errors.Is(err, ErrLastOwnerRemoval) {
		t.Errorf("expected ErrLastOwnerRemoval when removing the only owner, got %v", err)
	}

	// Case 4: a member tries to remove someone else — forbidden.
	if err := AddCollectionMember(context.Background(), db, log, collection.ID, owner.ID, memberUser.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("re-add member for case 4 failed: %v", err)
	}
	other := newTestUser(t, db, "other@example.com", "Other")
	if err := AddCollectionMember(context.Background(), db, log, collection.ID, owner.ID, other.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("add other member failed: %v", err)
	}
	err = RemoveCollectionMember(context.Background(), db, log, collection.ID, memberUser.ID, other.ID)
	if !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("expected ErrCollectionForbidden when non-owner removes someone else, got %v", err)
	}
}

func TestAddCollectionItemSourceAccessGate(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()

	owner := newTestUser(t, db, "owner@example.com", "Owner")

	// Create a source the owner does NOT have team access to (no team_sources row).
	source := &models.Source{
		Name: "lonely-source",
		Connection: models.ConnectionInfo{
			Host:      "ch:9000",
			Username:  "default",
			Password:  "",
			Database:  "default",
			TableName: "logs",
		},
	}
	if err := db.CreateSource(context.Background(), source); err != nil {
		t.Fatalf("CreateSource failed: %v", err)
	}

	// Save a query against that source.
	savedQuery, err := db.CreateSavedQuery(context.Background(), source.ID, "test", "", "sql", `{"version":1,"sourceId":1,"timeRange":null,"limit":100,"content":"SELECT 1"}`, &owner.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery failed: %v", err)
	}

	// Owner has a personal collection.
	personal, err := EnsurePersonalCollection(context.Background(), db, log, owner)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection failed: %v", err)
	}

	// Without source access, adding the saved-query as an item should fail.
	err = AddCollectionItem(context.Background(), db, log, personal.ID, owner.ID, savedQuery.ID, 0)
	if err == nil {
		t.Error("expected AddCollectionItem to fail without source access, got nil")
	}
}
