package sqlite

import (
	"context"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// Team methods

// CreateTeam creates a new team
func (d *DB) CreateTeam(ctx context.Context, team *models.Team) error {
	result, err := d.queries.CreateTeam.ExecContext(ctx,
		team.ID,
		team.Name,
		team.Description,
	)
	if err != nil {
		if isUniqueConstraintError(err, "teams", "name") {
			return fmt.Errorf("team with name %s already exists", team.Name)
		}
		return fmt.Errorf("error creating team: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// GetTeam retrieves a team by ID
func (d *DB) GetTeam(ctx context.Context, teamID string) (*models.Team, error) {
	var team models.Team
	err := d.queries.GetTeam.GetContext(ctx, &team, teamID)
	if err != nil {
		return nil, fmt.Errorf("error getting team: %w", err)
	}
	return &team, nil
}

// UpdateTeam updates an existing team
func (d *DB) UpdateTeam(ctx context.Context, team *models.Team) error {
	result, err := d.queries.UpdateTeam.ExecContext(ctx,
		team.Name,
		team.Description,
		team.UpdatedAt,
		team.ID,
	)
	if err != nil {
		if isUniqueConstraintError(err, "teams", "name") {
			return fmt.Errorf("team with name %s already exists", team.Name)
		}
		return fmt.Errorf("error updating team: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// DeleteTeam deletes a team by ID
func (d *DB) DeleteTeam(ctx context.Context, teamID string) error {
	result, err := d.queries.DeleteTeam.ExecContext(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error deleting team: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// ListTeams returns all teams
func (d *DB) ListTeams(ctx context.Context) ([]*models.Team, error) {
	var teams []*models.Team
	err := d.queries.ListTeams.SelectContext(ctx, &teams)
	if err != nil {
		return nil, fmt.Errorf("error listing teams: %w", err)
	}
	return teams, nil
}

// Team member methods

// AddTeamMember adds a member to a team
func (d *DB) AddTeamMember(ctx context.Context, teamID, userID, role string) error {
	// First check if the team exists
	_, err := d.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error checking team existence: %w", err)
	}

	// Then check if the user exists
	_, err = d.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}

	// Finally add the team member
	result, err := d.queries.AddTeamMember.ExecContext(ctx,
		teamID,
		userID,
		role,
		time.Now(),
	)
	if err != nil {
		if isUniqueConstraintError(err, "team_members", "team_id") {
			return fmt.Errorf("user %s is already a member of team %s", userID, teamID)
		}
		return fmt.Errorf("error adding team member: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// GetTeamMember gets a team member
func (d *DB) GetTeamMember(ctx context.Context, teamID, userID string) (*models.TeamMember, error) {
	var member models.TeamMember
	err := d.queries.GetTeamMember.GetContext(ctx, &member, teamID, userID)
	if err != nil {
		return nil, fmt.Errorf("error getting team member: %w", err)
	}
	return &member, nil
}

// UpdateTeamMemberRole updates a team member's role
func (d *DB) UpdateTeamMemberRole(ctx context.Context, teamID, userID, role string) error {
	// First check if the team member exists
	_, err := d.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		return fmt.Errorf("error checking team member existence: %w", err)
	}

	result, err := d.queries.UpdateTeamMemberRole.ExecContext(ctx, role, teamID, userID)
	if err != nil {
		return fmt.Errorf("error updating team member role: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// RemoveTeamMember removes a member from a team
func (d *DB) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
	result, err := d.queries.RemoveTeamMember.ExecContext(ctx, teamID, userID)
	if err != nil {
		return fmt.Errorf("error removing team member: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// ListTeamMembers lists all members of a team
func (d *DB) ListTeamMembers(ctx context.Context, teamID string) ([]*models.TeamMember, error) {
	var members []*models.TeamMember
	err := d.queries.ListTeamMembers.SelectContext(ctx, &members, teamID)
	if err != nil {
		return nil, fmt.Errorf("error listing team members: %w", err)
	}
	return members, nil
}

// ListUserTeams lists all teams a user is a member of
func (d *DB) ListUserTeams(ctx context.Context, userID string) ([]*models.Team, error) {
	var teams []*models.Team
	err := d.queries.ListUserTeams.SelectContext(ctx, &teams, userID)
	if err != nil {
		return nil, fmt.Errorf("error listing user teams: %w", err)
	}
	return teams, nil
}
