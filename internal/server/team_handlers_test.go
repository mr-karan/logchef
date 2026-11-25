package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestUpdateTeamMemberRoleHTTP(t *testing.T) {
	ctx := context.Background()
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.store, "admin@example.com", models.UserRoleAdmin)
	teamAdmin := mkTestUser(t, s.store, "lead@example.com", models.UserRoleMember)
	member := mkTestUser(t, s.store, "member@example.com", models.UserRoleMember)
	outsider := mkTestUser(t, s.store, "outsider@example.com", models.UserRoleMember)

	team := &models.Team{Name: "Crux"}
	other := &models.Team{Name: "Nomad"}
	for _, tm := range []*models.Team{team, other} {
		if err := s.store.CreateTeam(ctx, tm); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []struct {
		team *models.Team
		user *models.User
		role models.TeamRole
	}{
		{team, teamAdmin, models.TeamRoleAdmin},
		{team, member, models.TeamRoleMember},
		{other, teamAdmin, models.TeamRoleMember},
		{other, member, models.TeamRoleMember},
	} {
		if err := s.store.AddTeamMember(ctx, m.team.ID, m.user.ID, m.role); err != nil {
			t.Fatal(err)
		}
	}
	path := func(tm *models.Team, u *models.User) string {
		return fmt.Sprintf("/api/v1/teams/%d/members/%d", tm.ID, u.ID)
	}
	roleOf := func(tm *models.Team, u *models.User) models.TeamRole {
		t.Helper()
		m, err := s.store.GetTeamMember(ctx, tm.ID, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if m == nil {
			return ""
		}
		return m.Role
	}
	adminSession := withSession(sessionCookie(t, s.store, admin))
	teamAdminSession := withSession(sessionCookie(t, s.store, teamAdmin))

	t.Run("team admin changes a member's role", func(t *testing.T) {
		resp := doAuthzRequest(t, s, http.MethodPut, path(team, member), `{"role":"editor"}`, teamAdminSession)
		if resp.status != http.StatusOK {
			t.Fatalf("status = %d (%q), want 200", resp.status, resp.message)
		}
		if got := roleOf(team, member); got != models.TeamRoleEditor {
			t.Fatalf("role = %q, want editor", got)
		}
	})

	t.Run("does not re-add a user who is not a member", func(t *testing.T) {
		resp := doAuthzRequest(t, s, http.MethodPut, path(team, outsider), `{"role":"admin"}`, adminSession)
		if resp.status != http.StatusNotFound {
			t.Fatalf("status = %d (%q), want 404", resp.status, resp.message)
		}
		if got := roleOf(team, outsider); got != "" {
			t.Fatalf("outsider became a %q member", got)
		}
	})

	t.Run("rejects an unknown role", func(t *testing.T) {
		resp := doAuthzRequest(t, s, http.MethodPut, path(team, member), `{"role":"owner"}`, adminSession)
		if resp.status != http.StatusBadRequest {
			t.Fatalf("status = %d (%q), want 400", resp.status, resp.message)
		}
		if got := roleOf(team, member); got != models.TeamRoleEditor {
			t.Fatalf("role = %q, want editor unchanged", got)
		}
	})

	t.Run("rejects a missing team", func(t *testing.T) {
		resp := doAuthzRequest(t, s, http.MethodPut, fmt.Sprintf("/api/v1/teams/999999/members/%d", member.ID), `{"role":"member"}`, adminSession)
		if resp.status != http.StatusNotFound {
			t.Fatalf("status = %d (%q), want 404", resp.status, resp.message)
		}
	})

	t.Run("a plain member of the team cannot change roles", func(t *testing.T) {
		resp := doAuthzRequest(t, s, http.MethodPut, path(other, teamAdmin), `{"role":"admin"}`, teamAdminSession)
		if resp.status != http.StatusForbidden {
			t.Fatalf("status = %d (%q), want 403", resp.status, resp.message)
		}
		if got := roleOf(other, teamAdmin); got != models.TeamRoleMember {
			t.Fatalf("role = %q, want member unchanged", got)
		}
	})

	t.Run("token needs teams:write", func(t *testing.T) {
		readOnly := doAuthzRequest(t, s, http.MethodPut, path(team, member), `{"role":"member"}`, withBearer(apiToken(t, s, admin, models.TokenScopeTeamsRead)))
		if readOnly.status != http.StatusForbidden {
			t.Errorf("teams:read status = %d, want 403", readOnly.status)
		}
		writer := doAuthzRequest(t, s, http.MethodPut, path(team, member), `{"role":"member"}`, withBearer(apiToken(t, s, admin, models.TokenScopeTeamsWrite)))
		if writer.status != http.StatusOK {
			t.Errorf("teams:write status = %d (%q), want 200", writer.status, writer.message)
		}
	})
}
