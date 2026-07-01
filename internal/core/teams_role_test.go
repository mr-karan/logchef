package core

import (
	"context"
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
