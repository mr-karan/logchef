package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
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
	if _, err := ListCollectionMembers(ctx, db, log, coll.ID, access.SessionPrincipal(member)); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("member listing roster should be forbidden, got %v", err)
	}
	if _, err := ListCollectionMembers(ctx, db, log, coll.ID, access.SessionPrincipal(owner)); err != nil {
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

// --- Team shares ---

// newTestTeam creates a team and adds each member with the given team role.
func newTestTeam(t *testing.T, db *sqlite.DB, name string, role models.TeamRole, members ...*models.User) *models.Team {
	t.Helper()
	ctx := context.Background()
	team := &models.Team{Name: name}
	if err := db.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam(%s): %v", name, err)
	}
	for _, m := range members {
		if err := db.AddTeamMember(ctx, team.ID, m.ID, role); err != nil {
			t.Fatalf("AddTeamMember(%s, %s): %v", name, m.Email, err)
		}
	}
	return team
}

// listedCollection returns how many times collectionID appears in the user's
// collection list, and the last matching row.
func listedCollection(t *testing.T, db *sqlite.DB, user *models.User, collectionID int) (int, *models.Collection) {
	t.Helper()
	list, err := ListCollectionsForUser(context.Background(), db, discardLogger(), user)
	if err != nil {
		t.Fatalf("ListCollectionsForUser(%s): %v", user.Email, err)
	}
	n := 0
	var found *models.Collection
	for _, c := range list {
		if c.ID == collectionID {
			n++
			found = c
		}
	}
	return n, found
}

func assertNoCollectionAccess(t *testing.T, db *sqlite.DB, user *models.User, collectionID int) {
	t.Helper()
	if _, _, err := GetCollectionForUser(context.Background(), db, discardLogger(), collectionID, user.ID); !errors.Is(err, ErrCollectionNotFound) {
		t.Errorf("GetCollectionForUser(%s) err = %v, want ErrCollectionNotFound", user.Email, err)
	}
	if n, _ := listedCollection(t, db, user, collectionID); n != 0 {
		t.Errorf("collection listed %d times for %s, want 0", n, user.Email)
	}
}

func assertMemberAccess(t *testing.T, db *sqlite.DB, user *models.User, collectionID int, want models.CollectionRole) {
	t.Helper()
	_, role, err := GetCollectionForUser(context.Background(), db, discardLogger(), collectionID, user.ID)
	if err != nil || role != want {
		t.Errorf("GetCollectionForUser(%s) = %q / %v, want %q", user.Email, role, err, want)
	}
	n, c := listedCollection(t, db, user, collectionID)
	if n != 1 || c.CallerRole != want {
		t.Errorf("collection listed %d times for %s (role %v), want once as %q", n, user.Email, c, want)
	}
}

// A team share grants team members the Member role dynamically: joining the
// team grants access on the next request, leaving it revokes access, and team
// admins/editors get no more than Member rights.
func TestCollectionTeamShareDynamicMembership(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "ts-owner@example.com", "Owner")
	teamAdmin := newTestUser(t, db, "ts-team-admin@example.com", "Team Admin")
	teamEditor := newTestUser(t, db, "ts-team-editor@example.com", "Team Editor")
	joiner := newTestUser(t, db, "ts-joiner@example.com", "Joiner")
	outsider := newTestUser(t, db, "ts-outsider@example.com", "Outsider")

	team := newTestTeam(t, db, "project-14", models.TeamRoleMember, owner)
	if err := db.AddTeamMember(ctx, team.ID, teamAdmin.ID, models.TeamRoleAdmin); err != nil {
		t.Fatalf("AddTeamMember(admin): %v", err)
	}
	if err := db.AddTeamMember(ctx, team.ID, teamEditor.ID, models.TeamRoleEditor); err != nil {
		t.Fatalf("AddTeamMember(editor): %v", err)
	}

	coll, err := CreateCollection(ctx, db, log, "Project 14 queries", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	assertNoCollectionAccess(t, db, teamAdmin, coll.ID)

	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(owner), team.ID); err != nil {
		t.Fatalf("AddCollectionTeam: %v", err)
	}
	// Duplicate shares are idempotent.
	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(owner), team.ID); err != nil {
		t.Fatalf("AddCollectionTeam(duplicate): %v", err)
	}

	assertMemberAccess(t, db, teamAdmin, coll.ID, models.CollectionRoleMember)
	assertMemberAccess(t, db, teamEditor, coll.ID, models.CollectionRoleMember)
	assertMemberAccess(t, db, owner, coll.ID, models.CollectionRoleOwner)
	assertNoCollectionAccess(t, db, joiner, coll.ID)
	assertNoCollectionAccess(t, db, outsider, coll.ID)

	// Counts: member_count is direct rows only (the owner); teams are separate.
	_, listed := listedCollection(t, db, teamAdmin, coll.ID)
	if listed.MemberCount != 1 || listed.TeamCount != 1 {
		t.Errorf("list counts = members %d teams %d, want 1/1", listed.MemberCount, listed.TeamCount)
	}
	detail, _, err := GetCollectionForUser(ctx, db, log, coll.ID, owner.ID)
	if err != nil || detail.MemberCount != 1 || detail.TeamCount != 1 {
		t.Errorf("detail counts = %+v / %v, want members 1 teams 1", detail, err)
	}

	// Team admins and editors get Member rights only: no collection management.
	for _, u := range []*models.User{teamAdmin, teamEditor} {
		if _, err := UpdateCollection(ctx, db, log, coll.ID, u.ID, "renamed", ""); !errors.Is(err, ErrCollectionForbidden) {
			t.Errorf("UpdateCollection(%s) err = %v, want ErrCollectionForbidden", u.Email, err)
		}
		if err := DeleteCollection(ctx, db, log, coll.ID, u.ID); !errors.Is(err, ErrCollectionForbidden) {
			t.Errorf("DeleteCollection(%s) err = %v, want ErrCollectionForbidden", u.Email, err)
		}
		if err := AddCollectionMember(ctx, db, log, coll.ID, u.ID, outsider.ID, models.CollectionRoleMember); !errors.Is(err, ErrCollectionForbidden) {
			t.Errorf("AddCollectionMember(%s) err = %v, want ErrCollectionForbidden", u.Email, err)
		}
		if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(u), team.ID); !errors.Is(err, ErrCollectionForbidden) {
			t.Errorf("AddCollectionTeam(%s) err = %v, want ErrCollectionForbidden", u.Email, err)
		}
		if err := RemoveCollectionTeam(ctx, db, log, coll.ID, u.ID, team.ID); !errors.Is(err, ErrCollectionForbidden) {
			t.Errorf("RemoveCollectionTeam(%s) err = %v, want ErrCollectionForbidden", u.Email, err)
		}
		if _, err := ListCollectionMembers(ctx, db, log, coll.ID, access.SessionPrincipal(u)); !errors.Is(err, ErrCollectionForbidden) {
			t.Errorf("ListCollectionMembers(%s) err = %v, want ErrCollectionForbidden", u.Email, err)
		}
		if _, err := ListCollectionTeams(ctx, db, log, coll.ID, access.SessionPrincipal(u)); !errors.Is(err, ErrCollectionForbidden) {
			t.Errorf("ListCollectionTeams(%s) err = %v, want ErrCollectionForbidden", u.Email, err)
		}
	}

	// Joining the team grants access on the next request; leaving revokes it.
	if err := db.AddTeamMember(ctx, team.ID, joiner.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember(joiner): %v", err)
	}
	assertMemberAccess(t, db, joiner, coll.ID, models.CollectionRoleMember)
	if err := db.RemoveTeamMember(ctx, team.ID, joiner.ID); err != nil {
		t.Fatalf("RemoveTeamMember(joiner): %v", err)
	}
	assertNoCollectionAccess(t, db, joiner, coll.ID)

	// No team user was copied into direct membership.
	members, err := ListCollectionMembers(ctx, db, log, coll.ID, access.SessionPrincipal(owner))
	if err != nil || len(members) != 1 {
		t.Errorf("ListCollectionMembers = %d / %v, want only the owner", len(members), err)
	}
}

// Pinning is participation (allowed for team-derived members with source
// access), while editing a curated saved query needs a direct Owner/Editor row.
// Collection visibility never grants source access.
func TestCollectionTeamShareSourceSeparation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "sep-owner@example.com", "Owner")
	infraMember := newTestUser(t, db, "sep-infra@example.com", "Infra Member")
	plainMember := newTestUser(t, db, "sep-plain@example.com", "Plain Member")

	shared := newTestTeam(t, db, "sep-project-14", models.TeamRoleAdmin, owner, infraMember, plainMember)
	infra := newTestTeam(t, db, "sep-infrastructure", models.TeamRoleMember, owner, infraMember)
	src := newTestSource(t, db, "sep_infra_logs")
	if err := db.AddTeamSource(ctx, infra.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	sq, err := db.CreateSavedQuery(ctx, src.ID, nil, "infra errors", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, "{}", &owner.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery: %v", err)
	}
	coll, err := CreateCollection(ctx, db, log, "Shared", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := AddCollectionItem(ctx, db, log, coll.ID, owner.ID, sq.ID, 0); err != nil {
		t.Fatalf("AddCollectionItem(owner): %v", err)
	}
	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(owner), shared.ID); err != nil {
		t.Fatalf("AddCollectionTeam: %v", err)
	}

	runnable := func(u *models.User) bool {
		t.Helper()
		items, err := ListCollectionItems(ctx, db, log, coll.ID, u.ID)
		if err != nil || len(items) != 1 {
			t.Fatalf("ListCollectionItems(%s) = %d / %v, want 1 item", u.Email, len(items), err)
		}
		return items[0].Runnable
	}
	if runnable(plainMember) {
		t.Error("team-derived member without source access sees a runnable item, want locked")
	}
	if !runnable(infraMember) {
		t.Error("team-derived member with source access sees a locked item, want runnable")
	}
	if has, err := db.UserHasSourceAccess(ctx, plainMember.ID, src.ID); err != nil || has {
		t.Errorf("collection share granted source access: %v / %v", has, err)
	}

	// Pinning: allowed with source access, denied without it.
	if err := RemoveCollectionItem(ctx, db, log, coll.ID, infraMember.ID, sq.ID); err != nil {
		t.Errorf("team-derived member should unpin, got %v", err)
	}
	if err := AddCollectionItem(ctx, db, log, coll.ID, infraMember.ID, sq.ID, 0); err != nil {
		t.Errorf("team-derived member with source access should pin, got %v", err)
	}
	if err := AddCollectionItem(ctx, db, log, coll.ID, plainMember.ID, sq.ID, 0); err == nil {
		t.Error("team-derived member without source access pinned a query, want error")
	}

	// Editing: a team share (even from a team admin) does not delegate edit rights.
	canEdit, err := UserCanEditSavedQuery(ctx, db, sq, infraMember)
	if err != nil || canEdit {
		t.Errorf("UserCanEditSavedQuery(team-derived) = %v / %v, want false", canEdit, err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, infraMember.ID, models.CollectionRoleEditor); err != nil {
		t.Fatalf("AddCollectionMember(editor): %v", err)
	}
	canEdit, err = UserCanEditSavedQuery(ctx, db, sq, infraMember)
	if err != nil || !canEdit {
		t.Errorf("UserCanEditSavedQuery(direct editor) = %v / %v, want true", canEdit, err)
	}
}

// Overlapping direct and team access lists the collection once; the direct role
// wins. Removing direct membership (including self-removal) removes only that
// row, and removing one team share keeps the other shares and direct members.
func TestCollectionTeamShareOverlapAndRemoval(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "ov-owner@example.com", "Owner")
	both := newTestUser(t, db, "ov-both@example.com", "Both")
	directOnly := newTestUser(t, db, "ov-direct@example.com", "Direct")

	teamA := newTestTeam(t, db, "ov-team-a", models.TeamRoleMember, owner, both)
	teamB := newTestTeam(t, db, "ov-team-b", models.TeamRoleMember, owner, both)

	coll, err := CreateCollection(ctx, db, log, "Overlap", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	for _, team := range []*models.Team{teamA, teamB} {
		if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(owner), team.ID); err != nil {
			t.Fatalf("AddCollectionTeam(%s): %v", team.Name, err)
		}
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, both.ID, models.CollectionRoleEditor); err != nil {
		t.Fatalf("AddCollectionMember(both): %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, directOnly.ID, models.CollectionRoleMember); err != nil {
		t.Fatalf("AddCollectionMember(directOnly): %v", err)
	}

	// Two team shares plus a direct Editor row: listed once, as editor.
	assertMemberAccess(t, db, both, coll.ID, models.CollectionRoleEditor)
	_, listed := listedCollection(t, db, owner, coll.ID)
	if listed.MemberCount != 3 || listed.TeamCount != 2 {
		t.Errorf("list counts = members %d teams %d, want 3/2", listed.MemberCount, listed.TeamCount)
	}

	// Direct self-removal drops only the direct row; team access remains.
	if err := RemoveCollectionMember(ctx, db, log, coll.ID, both.ID, both.ID); err != nil {
		t.Fatalf("self-removal: %v", err)
	}
	assertMemberAccess(t, db, both, coll.ID, models.CollectionRoleMember)
	if _, listed := listedCollection(t, db, owner, coll.ID); listed.MemberCount != 2 {
		t.Errorf("member_count after self-removal = %d, want 2", listed.MemberCount)
	}
	// Self-removal by a team-only participant is a no-op, not an exclusion.
	if err := RemoveCollectionMember(ctx, db, log, coll.ID, both.ID, both.ID); err != nil {
		t.Fatalf("team-only self-removal: %v", err)
	}
	assertMemberAccess(t, db, both, coll.ID, models.CollectionRoleMember)

	// Removing one share keeps the other share and direct members.
	if err := RemoveCollectionTeam(ctx, db, log, coll.ID, owner.ID, teamA.ID); err != nil {
		t.Fatalf("RemoveCollectionTeam(A): %v", err)
	}
	assertMemberAccess(t, db, both, coll.ID, models.CollectionRoleMember)
	assertMemberAccess(t, db, directOnly, coll.ID, models.CollectionRoleMember)
	teams, err := ListCollectionTeams(ctx, db, log, coll.ID, access.SessionPrincipal(owner))
	if err != nil || len(teams) != 1 || teams[0].TeamID != teamB.ID || teams[0].TeamName != teamB.Name {
		t.Fatalf("ListCollectionTeams after removal = %+v / %v, want only team B", teams, err)
	}

	// Deleting the last shared team cleans up the share and revokes access.
	if err := DeleteTeam(ctx, db, log, teamB.ID); err != nil {
		t.Fatalf("DeleteTeam(B): %v", err)
	}
	assertNoCollectionAccess(t, db, both, coll.ID)
	assertMemberAccess(t, db, directOnly, coll.ID, models.CollectionRoleMember)
	if teams, err := ListCollectionTeams(ctx, db, log, coll.ID, access.SessionPrincipal(owner)); err != nil || len(teams) != 0 {
		t.Errorf("ListCollectionTeams after team deletion = %d / %v, want 0", len(teams), err)
	}
}

// Adding a share requires direct collection ownership plus membership in the
// target team (global admins may pick any existing team). Removing a share
// needs ownership only, so owners can revoke after leaving the team.
func TestCollectionTeamShareAuthorization(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "auth-owner@example.com", "Owner")
	editor := newTestUser(t, db, "auth-editor@example.com", "Editor")
	admin := newTestAdmin(t, db, "auth-admin@example.com")
	adminOwner := newTestAdmin(t, db, "auth-admin-owner@example.com")

	ownTeam := newTestTeam(t, db, "auth-own", models.TeamRoleMember, owner, editor, admin)
	foreignTeam := newTestTeam(t, db, "auth-foreign", models.TeamRoleMember)

	coll, err := CreateCollection(ctx, db, log, "Auth", "", owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := AddCollectionMember(ctx, db, log, coll.ID, owner.ID, editor.ID, models.CollectionRoleEditor); err != nil {
		t.Fatalf("AddCollectionMember(editor): %v", err)
	}

	// Non-member owners cannot share with teams they do not belong to, and
	// cannot tell a missing team from a foreign one.
	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(owner), foreignTeam.ID); !errors.Is(err, ErrCollectionTeamNotMember) {
		t.Errorf("AddCollectionTeam(foreign) err = %v, want ErrCollectionTeamNotMember", err)
	}
	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(owner), 999999); !errors.Is(err, ErrCollectionTeamNotMember) {
		t.Errorf("AddCollectionTeam(missing, non-admin) err = %v, want ErrCollectionTeamNotMember", err)
	}
	// Direct editors are not owners.
	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(editor), ownTeam.ID); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("AddCollectionTeam(editor) err = %v, want ErrCollectionForbidden", err)
	}
	// A global admin with no participation cannot see or mutate the collection.
	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(admin), ownTeam.ID); !errors.Is(err, ErrCollectionNotFound) {
		t.Errorf("AddCollectionTeam(non-participant admin) err = %v, want ErrCollectionNotFound", err)
	}

	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(owner), ownTeam.ID); err != nil {
		t.Fatalf("AddCollectionTeam(own): %v", err)
	}

	// A global admin participating through the team can read the rosters but
	// cannot mutate shares without collection ownership.
	if _, err := ListCollectionTeams(ctx, db, log, coll.ID, access.SessionPrincipal(admin)); err != nil {
		t.Errorf("ListCollectionTeams(participating admin): %v", err)
	}
	if _, err := ListCollectionMembers(ctx, db, log, coll.ID, access.SessionPrincipal(admin)); err != nil {
		t.Errorf("ListCollectionMembers(participating admin): %v", err)
	}
	if err := RemoveCollectionTeam(ctx, db, log, coll.ID, admin.ID, ownTeam.ID); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("RemoveCollectionTeam(participating admin) err = %v, want ErrCollectionForbidden", err)
	}
	if err := AddCollectionTeam(ctx, db, log, coll.ID, access.SessionPrincipal(admin), foreignTeam.ID); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("AddCollectionTeam(participating admin) err = %v, want ErrCollectionForbidden", err)
	}
	// Through OAuth the admin role does not apply, so the roster stays hidden.
	adminOAuth := access.OAuthPrincipal(admin, 1, "test-client", []models.TokenScope{models.TokenScopeCollectionsRead})
	if _, err := ListCollectionTeams(ctx, db, log, coll.ID, adminOAuth); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("ListCollectionTeams(participating admin via OAuth) err = %v, want ErrCollectionForbidden", err)
	}
	if _, err := ListCollectionMembers(ctx, db, log, coll.ID, adminOAuth); !errors.Is(err, ErrCollectionForbidden) {
		t.Errorf("ListCollectionMembers(participating admin via OAuth) err = %v, want ErrCollectionForbidden", err)
	}

	// An owner who left the team can still revoke the share.
	if err := db.RemoveTeamMember(ctx, ownTeam.ID, owner.ID); err != nil {
		t.Fatalf("RemoveTeamMember(owner): %v", err)
	}
	if teams, err := ListCollectionTeams(ctx, db, log, coll.ID, access.SessionPrincipal(owner)); err != nil || len(teams) != 1 {
		t.Errorf("ListCollectionTeams(owner after leaving) = %d / %v, want 1", len(teams), err)
	}
	if err := RemoveCollectionTeam(ctx, db, log, coll.ID, owner.ID, ownTeam.ID); err != nil {
		t.Errorf("RemoveCollectionTeam(owner after leaving): %v", err)
	}
	assertNoCollectionAccess(t, db, admin, coll.ID)

	// A global admin who owns a collection may share with any existing team.
	adminColl, err := CreateCollection(ctx, db, log, "Admin owned", "", adminOwner.ID)
	if err != nil {
		t.Fatalf("CreateCollection(admin): %v", err)
	}
	if err := AddCollectionTeam(ctx, db, log, adminColl.ID, access.SessionPrincipal(adminOwner), foreignTeam.ID); err != nil {
		t.Errorf("AddCollectionTeam(admin owner, foreign team): %v", err)
	}
	if err := AddCollectionTeam(ctx, db, log, adminColl.ID, access.SessionPrincipal(adminOwner), 999999); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("AddCollectionTeam(admin owner, missing team) err = %v, want ErrTeamNotFound", err)
	}
	// Through OAuth an admin owner is limited to their own teams, like any owner.
	adminOwnerOAuth := access.OAuthPrincipal(adminOwner, 1, "test-client", []models.TokenScope{models.TokenScopeCollectionsWrite})
	if err := AddCollectionTeam(ctx, db, log, adminColl.ID, adminOwnerOAuth, ownTeam.ID); !errors.Is(err, ErrCollectionTeamNotMember) {
		t.Errorf("AddCollectionTeam(admin owner via OAuth, foreign team) err = %v, want ErrCollectionTeamNotMember", err)
	}

	// Personal collections never accept team shares.
	personal, err := EnsurePersonalCollection(ctx, db, log, owner)
	if err != nil {
		t.Fatalf("EnsurePersonalCollection: %v", err)
	}
	if err := db.AddTeamMember(ctx, ownTeam.ID, owner.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("re-add owner to team: %v", err)
	}
	if err := AddCollectionTeam(ctx, db, log, personal.ID, access.SessionPrincipal(owner), ownTeam.ID); !errors.Is(err, ErrPersonalCollectionImmutable) {
		t.Errorf("AddCollectionTeam(personal) err = %v, want ErrPersonalCollectionImmutable", err)
	}
	if err := RemoveCollectionTeam(ctx, db, log, personal.ID, owner.ID, ownTeam.ID); !errors.Is(err, ErrPersonalCollectionImmutable) {
		t.Errorf("RemoveCollectionTeam(personal) err = %v, want ErrPersonalCollectionImmutable", err)
	}
}
