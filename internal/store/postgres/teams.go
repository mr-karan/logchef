package postgres

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func teamToModel(r sqlc.Team) *models.Team {
	return &models.Team{
		ID:          models.TeamID(r.ID),
		Name:        r.Name,
		Description: textStr(r.Description),
		Managed:     r.Managed,
		Timestamps:  models.Timestamps{CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time},
	}
}

func teamMemberToModel(r sqlc.TeamMember) *models.TeamMember {
	return &models.TeamMember{
		TeamID:    models.TeamID(r.TeamID),
		UserID:    models.UserID(r.UserID),
		Role:      models.TeamRole(r.Role),
		CreatedAt: r.CreatedAt.Time,
	}
}

// CreateTeam inserts a new team and populates ID + timestamps on success.
func (s *Store) CreateTeam(ctx context.Context, team *models.Team) error {
	id, err := s.q.CreateTeam(ctx, sqlc.CreateTeamParams{
		Name:        team.Name,
		Description: text(team.Description),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: team with name %s already exists", models.ErrConflict, team.Name)
		}
		s.log.Error("failed to create team record in db", "error", err, "name", team.Name)
		return fmt.Errorf("error creating team: %w", err)
	}
	team.ID = models.TeamID(id)
	if row, err := s.q.GetTeam(ctx, id); err == nil {
		team.CreatedAt = row.CreatedAt.Time
		team.UpdatedAt = row.UpdatedAt.Time
	}
	return nil
}

// GetTeam retrieves a team by ID. Returns models.ErrTeamNotFound if absent.
func (s *Store) GetTeam(ctx context.Context, teamID models.TeamID) (*models.Team, error) {
	row, err := s.q.GetTeam(ctx, int64(teamID))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrTeamNotFound
		}
		return nil, fmt.Errorf("getting team id %d: %w", teamID, err)
	}
	return teamToModel(row), nil
}

// GetTeamByName retrieves a team by name. Returns models.ErrTeamNotFound if absent.
func (s *Store) GetTeamByName(ctx context.Context, name string) (*models.Team, error) {
	row, err := s.q.GetTeamByName(ctx, name)
	if err != nil {
		if notFound(err) {
			return nil, models.ErrTeamNotFound
		}
		return nil, fmt.Errorf("getting team name %s: %w", name, err)
	}
	return teamToModel(row), nil
}

// UpdateTeam updates an existing team record.
func (s *Store) UpdateTeam(ctx context.Context, team *models.Team) error {
	err := s.q.UpdateTeam(ctx, sqlc.UpdateTeamParams{
		Name:        team.Name,
		Description: text(team.Description),
		UpdatedAt:   ts(team.UpdatedAt),
		ID:          int64(team.ID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: team with name %s already exists", models.ErrConflict, team.Name)
		}
		s.log.Error("failed to update team record in db", "error", err, "team_id", team.ID)
		return fmt.Errorf("error updating team: %w", err)
	}
	return nil
}

// DeleteTeam removes a team by ID (memberships/links cascade via FKs).
func (s *Store) DeleteTeam(ctx context.Context, teamID models.TeamID) error {
	if err := s.q.DeleteTeam(ctx, int64(teamID)); err != nil {
		s.log.Error("failed to delete team record from db", "error", err, "team_id", teamID)
		return fmt.Errorf("error deleting team: %w", err)
	}
	return nil
}

// ListTeams retrieves all teams with their member counts.
func (s *Store) ListTeams(ctx context.Context) ([]*models.Team, error) {
	rows, err := s.q.ListTeams(ctx)
	if err != nil {
		s.log.Error("failed to list teams from db", "error", err)
		return nil, fmt.Errorf("error listing teams: %w", err)
	}
	teams := make([]*models.Team, 0, len(rows))
	for i := range rows {
		row := rows[i]
		teams = append(teams, &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: textStr(row.Description),
			MemberCount: int(row.MemberCount),
			Timestamps:  models.Timestamps{CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time},
		})
	}
	return teams, nil
}

// AddTeamMember associates a user with a team. Adding an existing member is a no-op.
func (s *Store) AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	if _, err := s.GetTeam(ctx, teamID); err != nil {
		return fmt.Errorf("failed checking team existence: %w", err)
	}
	if _, err := s.GetUser(ctx, userID); err != nil {
		return fmt.Errorf("failed checking user existence: %w", err)
	}

	err := s.q.AddTeamMember(ctx, sqlc.AddTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
		Role:   string(role),
	})
	if err != nil {
		if isUniqueViolation(err) {
			s.log.Warn("attempted to add existing team member", "team_id", teamID, "user_id", userID)
			return nil
		}
		s.log.Error("failed to add team member record", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error adding team member: %w", err)
	}
	return nil
}

// GetTeamMember retrieves a membership. Returns (nil, nil) if not a member.
func (s *Store) GetTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error) {
	row, err := s.q.GetTeamMember(ctx, sqlc.GetTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
	})
	if err != nil {
		if notFound(err) {
			return nil, nil
		}
		s.log.Error("failed to get team member record from db", "error", err, "team_id", teamID, "user_id", userID)
		return nil, fmt.Errorf("error getting team member: %w", err)
	}
	return teamMemberToModel(row), nil
}

// UpdateTeamMemberRole updates the role of an existing team member.
func (s *Store) UpdateTeamMemberRole(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	if _, err := s.GetTeamMember(ctx, teamID, userID); err != nil {
		return fmt.Errorf("failed checking team member existence before update: %w", err)
	}
	err := s.q.UpdateTeamMemberRole(ctx, sqlc.UpdateTeamMemberRoleParams{
		Role:   string(role),
		TeamID: int64(teamID),
		UserID: int64(userID),
	})
	if err != nil {
		s.log.Error("failed to update team member role in db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error updating team member role: %w", err)
	}
	return nil
}

// RemoveTeamMember removes a user's membership from a team.
func (s *Store) RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error {
	err := s.q.RemoveTeamMember(ctx, sqlc.RemoveTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
	})
	if err != nil {
		s.log.Error("failed to remove team member record from db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error removing team member: %w", err)
	}
	return nil
}

// ListTeamMembers retrieves basic membership info for a team.
func (s *Store) ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	rows, err := s.q.ListTeamMembers(ctx, int64(teamID))
	if err != nil {
		s.log.Error("failed to list team members from db", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team members: %w", err)
	}
	members := make([]*models.TeamMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, teamMemberToModel(row))
	}
	return members, nil
}

// ListTeamMembersWithDetails retrieves membership info plus user email/full name.
func (s *Store) ListTeamMembersWithDetails(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	rows, err := s.q.ListTeamMembersWithDetails(ctx, int64(teamID))
	if err != nil {
		s.log.Error("failed to list team members with details from db", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team members with details: %w", err)
	}
	members := make([]*models.TeamMember, 0, len(rows))
	for _, row := range rows {
		accountType := models.UserAccountType(row.AccountType)
		if accountType == "" {
			accountType = models.UserAccountTypeHuman
		}
		members = append(members, &models.TeamMember{
			TeamID:      models.TeamID(row.TeamID),
			UserID:      models.UserID(row.UserID),
			Role:        models.TeamRole(row.Role),
			Email:       row.Email,
			FullName:    row.FullName,
			AccountType: accountType,
			CreatedAt:   row.CreatedAt.Time,
		})
	}
	return members, nil
}

// ListUserTeams retrieves all teams a user is a member of.
func (s *Store) ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	rows, err := s.q.ListUserTeams(ctx, int64(userID))
	if err != nil {
		s.log.Error("failed to list teams for user from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing teams for user: %w", err)
	}
	teams := make([]*models.Team, 0, len(rows))
	for _, row := range rows {
		teams = append(teams, teamToModel(row))
	}
	return teams, nil
}

// AddTeamSource associates a team with a source. An existing link is a no-op.
func (s *Store) AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	err := s.q.AddTeamSource(ctx, sqlc.AddTeamSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			s.log.Warn("attempted to add existing team-source association", "team_id", teamID, "source_id", sourceID)
			return nil
		}
		s.log.Error("failed to add team source record", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error adding team source: %w", err)
	}
	return nil
}

// RemoveTeamSource removes the association between a team and a source.
func (s *Store) RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	err := s.q.RemoveTeamSource(ctx, sqlc.RemoveTeamSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		s.log.Error("failed to remove team source record from db", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error removing team source: %w", err)
	}
	return nil
}

// ListTeamSources retrieves all sources associated with a team.
func (s *Store) ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error) {
	rows, err := s.q.ListTeamSources(ctx, int64(teamID))
	if err != nil {
		s.log.Error("failed to list team sources from db", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team sources: %w", err)
	}
	sources := make([]*models.Source, 0, len(rows))
	for i := range rows {
		r := rows[i]
		sources = append(sources, sourceToModel(r))
	}
	return sources, nil
}

// ListSourceTeams retrieves all teams that have access to a source.
func (s *Store) ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error) {
	rows, err := s.q.ListSourceTeams(ctx, int64(sourceID))
	if err != nil {
		s.log.Error("failed to list source teams from db", "error", err, "source_id", sourceID)
		return nil, fmt.Errorf("error listing source teams: %w", err)
	}
	teams := make([]*models.Team, 0, len(rows))
	for _, r := range rows {
		teams = append(teams, teamToModel(r))
	}
	return teams, nil
}

// ListSourcesForUser lists unique sources a user can reach across all teams.
func (s *Store) ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error) {
	rows, err := s.q.ListSourcesForUser(ctx, int64(userID))
	if err != nil {
		s.log.Error("failed to list sources for user from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing sources for user: %w", err)
	}
	sources := make([]*models.Source, 0, len(rows))
	for i := range rows {
		r := rows[i]
		sources = append(sources, sourceToModel(r))
	}
	return sources, nil
}

// TeamHasSource reports whether a team has been granted access to a source.
func (s *Store) TeamHasSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) (bool, error) {
	count, err := s.q.TeamHasSource(ctx, sqlc.TeamHasSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		s.log.Error("failed to check team source access in db", "error", err, "team_id", teamID, "source_id", sourceID)
		return false, fmt.Errorf("error checking team source access: %w", err)
	}
	return count > 0, nil
}

// UserHasSourceAccess reports whether a user can reach a source via any team.
func (s *Store) UserHasSourceAccess(ctx context.Context, userID models.UserID, sourceID models.SourceID) (bool, error) {
	count, err := s.q.UserHasSourceAccess(ctx, sqlc.UserHasSourceAccessParams{
		UserID:   int64(userID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		s.log.Error("failed to check user source access in db", "error", err, "user_id", userID, "source_id", sourceID)
		return false, fmt.Errorf("error checking user source access: %w", err)
	}
	return count > 0, nil
}

// ListTeamsForUser retrieves a user's teams with their role and member count.
func (s *Store) ListTeamsForUser(ctx context.Context, userID models.UserID) ([]*models.UserTeamDetails, error) {
	rows, err := s.q.ListTeamsForUser(ctx, int64(userID))
	if err != nil {
		s.log.Error("failed to list teams for user (with role and count) from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing teams for user (with role and count): %w", err)
	}
	out := make([]*models.UserTeamDetails, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, &models.UserTeamDetails{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: textStr(row.Description),
			CreatedAt:   row.CreatedAt.Time,
			UpdatedAt:   row.UpdatedAt.Time,
			MemberCount: int(row.MemberCount),
			Role:        models.TeamRole(row.Role),
		})
	}
	return out, nil
}
