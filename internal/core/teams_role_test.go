package core

import (
	"context"
	"errors"
	"testing"

	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// seedTeamWithMember creates a team and adds the given user with the given role,
// returning the team id. Fails the test on any error so call sites stay flat.
func seedTeamWithMember(t *testing.T, db *sqlite.DB, teamName, ownerEmail string, role models.TeamRole) (models.TeamID, models.UserID) {
	t.Helper()
	log := discardLogger()
	user := newTestUser(t, db, ownerEmail, ownerEmail)
	team, err := CreateTeam(context.Background(), db, log, teamName, "")
	if err != nil {
		t.Fatalf("CreateTeam(%q): %v", teamName, err)
	}
	if err := AddTeamMember(context.Background(), db, log, team.ID, user.ID, role); err != nil {
		t.Fatalf("AddTeamMember(team=%d, user=%d, role=%s): %v", team.ID, user.ID, role, err)
	}
	return team.ID, user.ID
}

func TestIsTeamCollectionMutator(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	adminTeam, adminID := seedTeamWithMember(t, db, "admin-team", "admin@example.com", models.TeamRoleAdmin)
	editorTeam, editorID := seedTeamWithMember(t, db, "editor-team", "editor@example.com", models.TeamRoleEditor)
	memberTeam, memberID := seedTeamWithMember(t, db, "member-team", "member@example.com", models.TeamRoleMember)
	stranger := newTestUser(t, db, "stranger@example.com", "Stranger")

	cases := []struct {
		name   string
		teamID models.TeamID
		userID models.UserID
		want   bool
	}{
		{"admin is mutator", adminTeam, adminID, true},
		{"editor is mutator", editorTeam, editorID, true},
		{"member is not mutator", memberTeam, memberID, false},
		{"non-member is not mutator", adminTeam, stranger.ID, false},
		{"admin in team A not mutator in team B", editorTeam, adminID, false},
		{"editor in team A not mutator in team B", adminTeam, editorID, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsTeamCollectionMutator(ctx, db, tc.teamID, tc.userID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("IsTeamCollectionMutator(team=%d, user=%d) = %v, want %v",
					tc.teamID, tc.userID, got, tc.want)
			}
		})
	}
}

func TestIsAnyTeamCollectionMutator(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	_, adminID := seedTeamWithMember(t, db, "admin-team", "admin@example.com", models.TeamRoleAdmin)
	_, editorID := seedTeamWithMember(t, db, "editor-team", "editor@example.com", models.TeamRoleEditor)
	_, memberID := seedTeamWithMember(t, db, "member-team", "member@example.com", models.TeamRoleMember)
	stranger := newTestUser(t, db, "stranger@example.com", "Stranger")

	cases := []struct {
		name   string
		userID models.UserID
		want   bool
	}{
		{"team admin", adminID, true},
		{"team editor", editorID, true},
		{"team member is not mutator", memberID, false},
		{"user with no team", stranger.ID, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsAnyTeamCollectionMutator(ctx, db, tc.userID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("IsAnyTeamCollectionMutator(user=%d) = %v, want %v",
					tc.userID, got, tc.want)
			}
		})
	}
}

// IsAnyTeamAdmin must NOT count editors. This guards against accidental
// elevation if someone "fixes" IsAnyTeamAdmin to include editors, which would
// silently leak team-admin endpoints (membership management, source linking)
// to editors.
func TestIsAnyTeamAdminExcludesEditors(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	_, editorID := seedTeamWithMember(t, db, "editor-team", "editor@example.com", models.TeamRoleEditor)
	_, adminID := seedTeamWithMember(t, db, "admin-team", "admin@example.com", models.TeamRoleAdmin)

	editorIsAdmin, err := IsAnyTeamAdmin(ctx, db, editorID)
	if err != nil {
		t.Fatalf("IsAnyTeamAdmin(editor): %v", err)
	}
	if editorIsAdmin {
		t.Error("editor was reported as an admin — admin-only routes would leak")
	}

	adminIsAdmin, err := IsAnyTeamAdmin(ctx, db, adminID)
	if err != nil {
		t.Fatalf("IsAnyTeamAdmin(admin): %v", err)
	}
	if !adminIsAdmin {
		t.Error("admin was not recognized as an admin")
	}
}

// IsTeamAdmin must also remain strict: an editor in the team is not the admin
// of that team.
func TestIsTeamAdminStrict(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	team, editorID := seedTeamWithMember(t, db, "team", "editor@example.com", models.TeamRoleEditor)

	got, err := IsTeamAdmin(ctx, db, team, editorID)
	if err != nil {
		t.Fatalf("IsTeamAdmin: %v", err)
	}
	if got {
		t.Error("editor was reported as team admin")
	}
}

// TestAddTeamMemberIsUpsertNotDuplicate pins the "AddTeamMember on an existing
// member updates their role instead of erroring or duplicating the row" rule.
func TestAddTeamMemberIsUpsertNotDuplicate(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	team, err := CreateTeam(ctx, db, log, "upsert-team", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	user := newTestUser(t, db, "upsert-member@example.com", "Member")

	if err := AddTeamMember(ctx, db, log, team.ID, user.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember(initial): %v", err)
	}
	// Re-adding with a different role must update in place, not error or
	// create a second membership row.
	if err := AddTeamMember(ctx, db, log, team.ID, user.ID, models.TeamRoleAdmin); err != nil {
		t.Fatalf("AddTeamMember(role change): %v", err)
	}

	members, err := ListTeamMembers(ctx, db, team.ID)
	if err != nil {
		t.Fatalf("ListTeamMembers: %v", err)
	}
	matches := 0
	for _, m := range members {
		if m.UserID == user.ID {
			matches++
			if m.Role != models.TeamRoleAdmin {
				t.Errorf("member role = %q, want admin", m.Role)
			}
		}
	}
	if matches != 1 {
		t.Errorf("expected exactly one membership row for user %d, found %d", user.ID, matches)
	}

	// Re-adding with the SAME role again is also a no-op, not an error.
	if err := AddTeamMember(ctx, db, log, team.ID, user.ID, models.TeamRoleAdmin); err != nil {
		t.Errorf("AddTeamMember(same role again): %v", err)
	}
}

// TestAddTeamMemberValidation pins the guard rails: unknown team, unknown
// user, and an invalid role are all rejected before any DB write.
func TestAddTeamMemberValidation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	team, err := CreateTeam(ctx, db, log, "validation-team", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	user := newTestUser(t, db, "validation-member@example.com", "Member")

	if err := AddTeamMember(ctx, db, log, models.TeamID(999999), user.ID, models.TeamRoleMember); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("AddTeamMember(missing team) err = %v, want ErrTeamNotFound", err)
	}
	if err := AddTeamMember(ctx, db, log, team.ID, models.UserID(999999), models.TeamRoleMember); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("AddTeamMember(missing user) err = %v, want ErrUserNotFound", err)
	}
	if err := AddTeamMember(ctx, db, log, team.ID, user.ID, models.TeamRole("owner")); err == nil {
		t.Error("AddTeamMember(invalid role) should have failed")
	}
}

// TestRemoveTeamMemberNotAMemberIsNoOp pins the documented behavior: removing
// a user who was never a member of the team succeeds silently rather than
// erroring — callers shouldn't have to check membership first.
func TestRemoveTeamMemberNotAMemberIsNoOp(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	team, err := CreateTeam(ctx, db, log, "remove-noop-team", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	stranger := newTestUser(t, db, "remove-noop-stranger@example.com", "Stranger")

	if err := RemoveTeamMember(ctx, db, log, team.ID, stranger.ID); err != nil {
		t.Errorf("RemoveTeamMember(never a member) err = %v, want nil", err)
	}
}

// TestRemoveTeamMemberRemovesMembership confirms the member actually leaves
// once removed (and IsTeamMember reflects it).
func TestRemoveTeamMemberRemovesMembership(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	team, userID := seedTeamWithMember(t, db, "remove-team", "remove-member@example.com", models.TeamRoleMember)

	if isMember, err := IsTeamMember(ctx, db, team, userID); err != nil || !isMember {
		t.Fatalf("precondition IsTeamMember = %v / %v, want true", isMember, err)
	}
	if err := RemoveTeamMember(ctx, db, log, team, userID); err != nil {
		t.Fatalf("RemoveTeamMember: %v", err)
	}
	if isMember, err := IsTeamMember(ctx, db, team, userID); err != nil || isMember {
		t.Errorf("IsTeamMember after removal = %v / %v, want false", isMember, err)
	}
}

// TestCreateTeamDuplicateNameRejected pins the unique-team-name rule.
func TestCreateTeamDuplicateNameRejected(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	if _, err := CreateTeam(ctx, db, log, "dup-team", ""); err != nil {
		t.Fatalf("CreateTeam(first): %v", err)
	}
	if _, err := CreateTeam(ctx, db, log, "dup-team", "second description"); !errors.Is(err, ErrTeamAlreadyExists) {
		t.Errorf("CreateTeam(duplicate) err = %v, want ErrTeamAlreadyExists", err)
	}
}

// TestCreateTeamValidation pins the input validation on team creation.
func TestCreateTeamValidation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	cases := []struct {
		name        string
		teamName    string
		description string
		wantErr     bool
	}{
		{"valid name", "Valid Team 1", "", false},
		{"empty name", "", "", true},
		{"too short", "A", "", true},
		{"too long", string(make([]byte, 51)), "", true},
		{"invalid characters", "bad/name!", "", true},
		{"description too long", "Another Team", string(make([]byte, 501)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CreateTeam(ctx, db, log, tc.teamName, tc.description)
			if tc.wantErr && err == nil {
				t.Errorf("CreateTeam(%q) expected error, got nil", tc.teamName)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("CreateTeam(%q) unexpected error: %v", tc.teamName, err)
			}
		})
	}
}

// TestUpdateTeamNameConflict pins the rename-collision rule: renaming a team
// to another existing team's name is rejected, but renaming to its own
// current name (or a genuinely free name) succeeds.
func TestUpdateTeamNameConflict(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	teamA, err := CreateTeam(ctx, db, log, "team-alpha", "")
	if err != nil {
		t.Fatalf("CreateTeam(alpha): %v", err)
	}
	if _, err := CreateTeam(ctx, db, log, "team-beta", ""); err != nil {
		t.Fatalf("CreateTeam(beta): %v", err)
	}

	if err := UpdateTeam(ctx, db, log, teamA.ID, models.Team{Name: "team-beta"}); !errors.Is(err, ErrTeamAlreadyExists) {
		t.Errorf("UpdateTeam(rename to existing) err = %v, want ErrTeamAlreadyExists", err)
	}

	if err := UpdateTeam(ctx, db, log, teamA.ID, models.Team{Name: "team-alpha-renamed"}); err != nil {
		t.Errorf("UpdateTeam(rename to free name): %v", err)
	}
	updated, err := GetTeam(ctx, db, teamA.ID)
	if err != nil || updated.Name != "team-alpha-renamed" {
		t.Errorf("GetTeam after rename = %+v / %v", updated, err)
	}

	if err := UpdateTeam(ctx, db, log, models.TeamID(999999), models.Team{Name: "whatever"}); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("UpdateTeam(missing team) err = %v, want ErrTeamNotFound", err)
	}
}

// TestUserHasAccessToTeamSourceRequiresBothMembershipAndLink pins the
// two-part access rule: a user must be a member of the team AND the team
// must have the source linked. Either alone is not enough.
func TestUserHasAccessToTeamSourceRequiresBothMembershipAndLink(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	team, memberID := seedTeamWithMember(t, db, "access-team", "access-member@example.com", models.TeamRoleMember)
	src := newTestSource(t, db, "access-src")
	nonMember := newTestUser(t, db, "access-nonmember@example.com", "Non Member")

	// Team member, but source not yet linked to the team: no access.
	has, err := UserHasAccessToTeamSource(ctx, db, log, memberID, team, src.ID)
	if err != nil {
		t.Fatalf("UserHasAccessToTeamSource: %v", err)
	}
	if has {
		t.Error("member should not have access before the source is linked")
	}

	if err := AddTeamSource(ctx, db, log, team, src.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}

	// Now the member has access...
	has, err = UserHasAccessToTeamSource(ctx, db, log, memberID, team, src.ID)
	if err != nil || !has {
		t.Errorf("UserHasAccessToTeamSource(member, linked) = %v / %v, want true", has, err)
	}
	// ...but a non-member of the team still doesn't, even though the source
	// is linked to the team.
	has, err = UserHasAccessToTeamSource(ctx, db, log, nonMember.ID, team, src.ID)
	if err != nil || has {
		t.Errorf("UserHasAccessToTeamSource(non-member, linked) = %v / %v, want false", has, err)
	}
}

// TestListTeamsWithAccessToSourceIntersection pins the intersection logic:
// only teams that both (a) have the source linked and (b) the user belongs to
// are returned.
func TestListTeamsWithAccessToSourceIntersection(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	src := newTestSource(t, db, "intersection-src")

	linkedAndMember, userID := seedTeamWithMember(t, db, "linked-and-member", "intersection-user@example.com", models.TeamRoleMember)
	if err := AddTeamSource(ctx, db, log, linkedAndMember, src.ID); err != nil {
		t.Fatalf("AddTeamSource(linkedAndMember): %v", err)
	}

	// Linked to the source, but the user is not a member of this team.
	linkedNotMember, err := CreateTeam(ctx, db, log, "linked-not-member", "")
	if err != nil {
		t.Fatalf("CreateTeam(linkedNotMember): %v", err)
	}
	if err := AddTeamSource(ctx, db, log, linkedNotMember.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource(linkedNotMember): %v", err)
	}

	// The user is a member, but this team has no access to the source.
	memberNotLinked, err := CreateTeam(ctx, db, log, "member-not-linked", "")
	if err != nil {
		t.Fatalf("CreateTeam(memberNotLinked): %v", err)
	}
	if err := AddTeamMember(ctx, db, log, memberNotLinked.ID, userID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember(memberNotLinked): %v", err)
	}

	accessible, err := ListTeamsWithAccessToSource(ctx, db, log, src.ID, userID)
	if err != nil {
		t.Fatalf("ListTeamsWithAccessToSource: %v", err)
	}
	if len(accessible) != 1 || accessible[0].ID != linkedAndMember {
		t.Errorf("ListTeamsWithAccessToSource = %+v, want exactly [team %d]", accessible, linkedAndMember)
	}
}
