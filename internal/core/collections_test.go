package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// newTestDB spins up a fresh on-disk SQLite DB with all migrations applied.
// The file lives in t.TempDir() so go test cleans up automatically.
func newTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sqlite.New(context.Background(), sqlite.Options{
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

// newTestSource creates a source row with a minimal valid connection config.
// SourceType is left empty, which datasource.NormalizeSourceType treats as
// clickhouse — matching the fakeProvider registered by
// newFakeDatasourceService.
func newTestSource(t *testing.T, db *sqlite.DB, name string) *models.Source {
	t.Helper()
	// Sources are deduped by identity key (host+db+table), not name, so the
	// table name must vary per source to avoid a spurious ErrConflict between
	// unrelated sources in the same test.
	source := &models.Source{
		Name: name,
		Connection: models.ConnectionInfo{
			Host:      "ch:9000",
			Username:  "default",
			Database:  "default",
			TableName: name,
		},
	}
	if err := db.CreateSource(context.Background(), source); err != nil {
		t.Fatalf("CreateSource(%s): %v", name, err)
	}
	return source
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
	savedQuery, err := db.CreateSavedQuery(context.Background(), source.ID, nil, "test", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, `{"version":1,"sourceId":1,"timeRange":null,"limit":100,"content":"SELECT 1"}`, &owner.ID)
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

// Curating a collection's query list is open to any participant, but the member
// roster stays visible to owners only.
func TestCollectionItemParticipationAndRosterVisibility(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "co-owner@example.com", "Owner")
	member := newTestUser(t, db, "co-member@example.com", "Member")

	coll, err := CreateCollection(ctx, db, log, "Team Queries", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, member.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("AddCollectionMember: %v", err)
	}

	// Seed an item directly (bypassing the source-access gate, which is exercised
	// separately) so we can test the removal authorization.
	src := &models.Source{Name: "curate-src", Connection: models.ConnectionInfo{Host: "h:9000", Database: "default", TableName: "logs"}}
	if err := db.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	sq, err := db.CreateSavedQuery(ctx, src.ID, nil, "q", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, "{}", &owner.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery: %v", err)
	}
	if err := db.AddCollectionItem(ctx, coll.ID, sq.ID, 0, &owner.ID); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	// A plain member can remove an item (participation, not ownership).
	if err := RemoveCollectionItem(ctx, db, log, coll.ID, member.ID, sq.ID); err != nil {
		t.Errorf("member should be able to remove a collection item, got %v", err)
	}

	// The member roster is forbidden to a non-owner member, allowed to the owner.
	if _, err := ListCollectionMembers(ctx, db, log, coll.ID, member.ID); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("member listing roster should be forbidden, got %v", err)
	}
	if _, err := ListCollectionMembers(ctx, db, log, coll.ID, owner.ID); err != nil {
		t.Errorf("owner should be able to list members, got %v", err)
	}
}

// TestGetCollectionForUserAdminNoFreePass pins the documented behavior in
// GetCollectionForUser: a global admin with no membership row on the
// collection is treated exactly like any other non-member — 404, not a free
// pass. Admin bypass exists elsewhere (e.g. UserCanDeleteSavedQuery) but not
// here; collection visibility mirrors team-membership-gated source access.
func TestGetCollectionForUserAdminNoFreePass(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "admin-gate-owner@example.com", "Owner")
	admin := newTestUser(t, db, "admin-gate-admin@example.com", "Admin")
	admin.Role = models.UserRoleAdmin
	if err := db.UpdateUser(ctx, admin); err != nil {
		t.Fatalf("UpdateUser(admin): %v", err)
	}

	coll, err := CreateCollection(ctx, db, log, "Owner Only", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	if _, _, err := GetCollectionForUser(ctx, db, log, coll.ID, admin.ID); !errors.Is(err, ErrCollectionNotFound) {
		t.Errorf("GetCollectionForUser(admin, non-member) err = %v, want ErrCollectionNotFound", err)
	}
	// Sanity: the owner themselves can still see it.
	if _, _, err := GetCollectionForUser(ctx, db, log, coll.ID, owner.ID); err != nil {
		t.Errorf("GetCollectionForUser(owner): %v", err)
	}
}

// TestUpdateCollectionAuthorization pins UpdateCollection's owner-only gate
// and the personal-collection-is-immutable rule.
func TestUpdateCollectionAuthorization(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "update-coll-owner@example.com", "Owner")
	member := newTestUser(t, db, "update-coll-member@example.com", "Member")

	coll, err := CreateCollection(ctx, db, log, "Original", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, member.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("AddCollectionMember: %v", err)
	}

	// A plain member cannot rename the collection.
	if _, err := UpdateCollection(ctx, db, log, coll.ID, member.ID, "Hijacked", ""); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("UpdateCollection(member) err = %v, want ErrCollectionForbidden", err)
	}

	// The owner can.
	updated, err := UpdateCollection(ctx, db, log, coll.ID, owner.ID, "Renamed", "new description")
	if err != nil {
		t.Fatalf("UpdateCollection(owner): %v", err)
	}
	if updated.Name != "Renamed" {
		t.Errorf("UpdateCollection name = %q, want %q", updated.Name, "Renamed")
	}

	// A personal collection can never be renamed, even by its owner.
	personal, err := EnsurePersonalCollection(ctx, db, log, owner)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection: %v", err)
	}
	if _, err := UpdateCollection(ctx, db, log, personal.ID, owner.ID, "New Name", ""); !errors.Is(err, ErrPersonalCollectionImmutable) {
		t.Errorf("UpdateCollection(personal) err = %v, want ErrPersonalCollectionImmutable", err)
	}
}

// TestDeleteCollectionAuthorization pins DeleteCollection's owner-only gate
// and the personal-collection-is-immutable rule.
func TestDeleteCollectionAuthorization(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "delete-coll-owner@example.com", "Owner")
	member := newTestUser(t, db, "delete-coll-member@example.com", "Member")

	coll, err := CreateCollection(ctx, db, log, "Doomed", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, member.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("AddCollectionMember: %v", err)
	}

	if err := DeleteCollection(ctx, db, log, coll.ID, member.ID); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("DeleteCollection(member) err = %v, want ErrCollectionForbidden", err)
	}

	personal, err := EnsurePersonalCollection(ctx, db, log, owner)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection: %v", err)
	}
	if err := DeleteCollection(ctx, db, log, personal.ID, owner.ID); !errors.Is(err, ErrPersonalCollectionImmutable) {
		t.Errorf("DeleteCollection(personal) err = %v, want ErrPersonalCollectionImmutable", err)
	}

	if err := DeleteCollection(ctx, db, log, coll.ID, owner.ID); err != nil {
		t.Errorf("DeleteCollection(owner): %v", err)
	}
}

// TestAddCollectionMemberAuthorization pins AddCollectionMember's owner-only
// gate, invalid-role rejection, and the personal-collection-is-immutable rule.
func TestAddCollectionMemberAuthorization(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "add-member-owner@example.com", "Owner")
	member := newTestUser(t, db, "add-member-member@example.com", "Member")
	target := newTestUser(t, db, "add-member-target@example.com", "Target")

	coll, err := CreateCollection(ctx, db, log, "Team Coll", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, member.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("AddCollectionMember(seed): %v", err)
	}

	// A non-owner member cannot invite anyone.
	if err := AddCollectionMember(ctx, db, log, coll.ID, member.ID, target.ID, models.CollectionRoleMember); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("AddCollectionMember(non-owner) err = %v, want ErrCollectionForbidden", err)
	}

	// An unknown role is rejected before the ownership check even matters.
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, target.ID, models.CollectionRole("bogus")); !errors.Is(err, ErrInvalidCollectionRole) {
		t.Errorf("AddCollectionMember(bogus role) err = %v, want ErrInvalidCollectionRole", err)
	}

	// The owner can add a valid member.
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, target.ID, models.CollectionRoleEditor); err != nil {
		t.Errorf("AddCollectionMember(owner, editor): %v", err)
	}

	// Personal collections reject membership changes outright, even by their owner.
	personal, err := EnsurePersonalCollection(ctx, db, log, owner)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection: %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, personal.ID, owner.ID, target.ID, models.CollectionRoleMember); !errors.Is(err, ErrPersonalCollectionImmutable) {
		t.Errorf("AddCollectionMember(personal) err = %v, want ErrPersonalCollectionImmutable", err)
	}
}
