package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// Team methods

// CreateTeam creates a new team
func (db *DB) CreateTeam(ctx context.Context, team *models.Team) error {
	db.log.Debug("creating team", "name", team.Name)

	var id int64
	err := db.queries.CreateTeam.QueryRowContext(ctx,
		team.Name,
		team.Description,
	).Scan(&id)

	if err != nil {
		if isUniqueConstraintError(err, "teams", "name") {
			return fmt.Errorf("team with name %s already exists", team.Name)
		}
		db.log.Error("failed to create team", "error", err)
		return fmt.Errorf("error creating team: %w", err)
	}

	// Set the auto-generated ID
	team.ID = models.TeamID(id)

	db.log.Debug("team created", "team_id", team.ID)
	return nil
}

// GetTeam retrieves a team by ID
func (db *DB) GetTeam(ctx context.Context, teamID models.TeamID) (*models.Team, error) {
	db.log.Debug("getting team", "team_id", teamID)

	var team models.Team
	err := db.queries.GetTeam.GetContext(ctx, &team, teamID)
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team")
	}
	return &team, nil
}

// UpdateTeam updates an existing team
func (db *DB) UpdateTeam(ctx context.Context, team *models.Team) error {
	db.log.Debug("updating team", "team_id", team.ID, "name", team.Name)

	result, err := db.queries.UpdateTeam.ExecContext(ctx,
		team.Name,
		team.Description,
		team.UpdatedAt,
		team.ID,
	)
	if err != nil {
		if isUniqueConstraintError(err, "teams", "name") {
			return fmt.Errorf("team with name %s already exists", team.Name)
		}
		db.log.Error("failed to update team", "error", err, "team_id", team.ID)
		return fmt.Errorf("error updating team: %w", err)
	}

	if err := checkRowsAffected(result, "update team"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", team.ID)
		return err
	}

	return nil
}

// DeleteTeam deletes a team by ID
func (db *DB) DeleteTeam(ctx context.Context, teamID models.TeamID) error {
	db.log.Debug("deleting team", "team_id", teamID)

	result, err := db.queries.DeleteTeam.ExecContext(ctx, teamID)
	if err != nil {
		db.log.Error("failed to delete team", "error", err, "team_id", teamID)
		return fmt.Errorf("error deleting team: %w", err)
	}

	if err := checkRowsAffected(result, "delete team"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", teamID)
		return err
	}

	return nil
}

// ListTeams returns all teams
func (db *DB) ListTeams(ctx context.Context) ([]*models.Team, error) {
	db.log.Debug("listing teams")

	var teams []*models.Team
	err := db.queries.ListTeams.SelectContext(ctx, &teams)
	if err != nil {
		db.log.Error("failed to list teams", "error", err)
		return nil, fmt.Errorf("error listing teams: %w", err)
	}

	db.log.Debug("teams listed", "count", len(teams))
	return teams, nil
}

// Team member methods

// AddTeamMember adds a member to a team
func (db *DB) AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	db.log.Debug("adding team member", "team_id", teamID, "user_id", userID, "role", role)

	// First check if the team exists
	_, err := db.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error checking team existence: %w", err)
	}

	// Then check if the user exists
	_, err = db.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}

	// Finally add the team member
	result, err := db.queries.AddTeamMember.ExecContext(ctx,
		teamID,
		userID,
		role,
	)
	if err != nil {
		if isUniqueConstraintError(err, "team_members", "team_id") {
			return fmt.Errorf("user %d is already a member of team %d", userID, teamID)
		}
		db.log.Error("failed to add team member", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error adding team member: %w", err)
	}

	if err := checkRowsAffected(result, "add team member"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", teamID, "user_id", userID)
		return err
	}

	return nil
}

// GetTeamMember gets a team member
func (db *DB) GetTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error) {
	db.log.Debug("getting team member", "team_id", teamID, "user_id", userID)

	var member models.TeamMember
	err := db.queries.GetTeamMember.GetContext(ctx, &member, teamID, userID)
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team member")
	}
	return &member, nil
}

// UpdateTeamMemberRole updates a team member's role
func (db *DB) UpdateTeamMemberRole(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	db.log.Debug("updating team member role", "team_id", teamID, "user_id", userID, "role", role)

	// First check if the team member exists
	_, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		return fmt.Errorf("error checking team member existence: %w", err)
	}

	result, err := db.queries.UpdateTeamMemberRole.ExecContext(ctx, role, teamID, userID)
	if err != nil {
		db.log.Error("failed to update team member role", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error updating team member role: %w", err)
	}

	if err := checkRowsAffected(result, "update team member role"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", teamID, "user_id", userID)
		return err
	}

	return nil
}

// RemoveTeamMember removes a member from a team
func (db *DB) RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error {
	db.log.Debug("removing team member", "team_id", teamID, "user_id", userID)

	result, err := db.queries.RemoveTeamMember.ExecContext(ctx, teamID, userID)
	if err != nil {
		db.log.Error("failed to remove team member", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error removing team member: %w", err)
	}

	if err := checkRowsAffected(result, "remove team member"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", teamID, "user_id", userID)
		return err
	}

	return nil
}

// ListTeamMembers lists all members of a team
func (db *DB) ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	db.log.Debug("listing team members", "team_id", teamID)

	var members []*models.TeamMember
	err := db.queries.ListTeamMembers.SelectContext(ctx, &members, teamID)
	if err != nil {
		db.log.Error("failed to list team members", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team members: %w", err)
	}

	db.log.Debug("team members listed", "team_id", teamID, "count", len(members))
	return members, nil
}

// ListTeamMembersWithDetails lists all members of a team with user details
func (db *DB) ListTeamMembersWithDetails(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	db.log.Debug("listing team members with details", "team_id", teamID)

	var members []*models.TeamMember
	err := db.queries.ListTeamMembersWithDetails.SelectContext(ctx, &members, teamID)
	if err != nil {
		db.log.Error("failed to list team members with details", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team members with details: %w", err)
	}

	db.log.Debug("team members with details listed", "team_id", teamID, "count", len(members))
	return members, nil
}

// ListUserTeams lists all teams a user is a member of
func (db *DB) ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	db.log.Debug("listing user teams", "user_id", userID)

	var teams []*models.Team
	err := db.queries.ListUserTeams.SelectContext(ctx, &teams, userID)
	if err != nil {
		db.log.Error("failed to list user teams", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing user teams: %w", err)
	}

	db.log.Debug("user teams listed", "user_id", userID, "count", len(teams))
	return teams, nil
}

// Team source methods

// AddTeamSource associates a source with a team
func (db *DB) AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	db.log.Debug("adding team source", "team_id", teamID, "source_id", sourceID)

	// Verify source exists
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		db.log.Error("failed to verify source exists", "error", err, "source_id", sourceID)
		return fmt.Errorf("error verifying source: %w", err)
	}
	if source == nil {
		return fmt.Errorf("source not found")
	}

	// Add source to team
	result, err := db.queries.AddTeamSource.ExecContext(ctx, teamID, sourceID)
	if err != nil {
		if isUniqueConstraintError(err, "team_sources", "team_id") {
			return nil // Already exists, not an error
		}
		db.log.Error("failed to add team source", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error adding team source: %w", err)
	}

	if err := checkRowsAffected(result, "add team source"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", teamID, "source_id", sourceID)
		return err
	}

	return nil
}

// RemoveTeamSource removes a source from a team
func (db *DB) RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	db.log.Debug("removing team source", "team_id", teamID, "source_id", sourceID)

	result, err := db.queries.RemoveTeamSource.ExecContext(ctx, teamID, sourceID)
	if err != nil {
		db.log.Error("failed to remove team source", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error removing team source: %w", err)
	}

	if err := checkRowsAffected(result, "remove team source"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", teamID, "source_id", sourceID)
		return err
	}

	return nil
}

// ListTeamSources lists all sources for a team
func (db *DB) ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error) {
	db.log.Debug("listing team sources", "team_id", teamID)

	var rows []struct {
		ID          int       `db:"id"`
		Database    string    `db:"database"`
		TableName   string    `db:"table_name"`
		Description string    `db:"description"`
		CreatedAt   time.Time `db:"created_at"`
	}
	err := db.queries.ListTeamSources.SelectContext(ctx, &rows, teamID)
	if err != nil {
		db.log.Error("failed to list team sources", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team sources: %w", err)
	}

	sources := make([]*models.Source, len(rows))
	for i, row := range rows {
		sources[i] = &models.Source{
			ID:          models.SourceID(row.ID),
			Description: row.Description,
			Connection: models.ConnectionInfo{
				Database:  row.Database,
				TableName: row.TableName,
			},
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
			},
		}
	}

	db.log.Debug("team sources listed", "team_id", teamID, "count", len(sources))
	return sources, nil
}

// ListSourceTeams returns all teams that have access to a source
func (db *DB) ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error) {
	db.log.Debug("listing source teams", "source_id", sourceID)

	var teams []*models.Team
	err := db.queries.ListSourceTeams.SelectContext(ctx, &teams, sourceID)
	if err != nil {
		db.log.Error("failed to list source teams", "error", err, "source_id", sourceID)
		return nil, fmt.Errorf("error listing source teams: %w", err)
	}

	db.log.Debug("source teams listed", "source_id", sourceID, "count", len(teams))
	return teams, nil
}

// ListSourcesForUser lists all unique sources a user has access to across all their teams
func (db *DB) ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error) {
	db.log.Debug("listing sources for user", "user_id", userID)

	// We need to get full source information for each source
	var rows []struct {
		ID                int       `db:"id"`
		MetaIsAutoCreated int       `db:"_meta_is_auto_created"`
		MetaTSField       string    `db:"_meta_ts_field"`
		MetaSeverityField string    `db:"_meta_severity_field"`
		Host              string    `db:"host"`
		Username          string    `db:"username"`
		Password          string    `db:"password"`
		Database          string    `db:"database"`
		TableName         string    `db:"table_name"`
		Description       string    `db:"description"`
		TTLDays           int       `db:"ttl_days"`
		CreatedAt         time.Time `db:"created_at"`
		UpdatedAt         time.Time `db:"updated_at"`
	}

	err := db.conn.SelectContext(ctx, &rows, `
		SELECT DISTINCT s.* FROM sources s
		JOIN team_sources ts ON s.id = ts.source_id
		JOIN team_members tm ON ts.team_id = tm.team_id
		WHERE tm.user_id = ?
		ORDER BY s.created_at DESC
	`, userID)

	if err != nil {
		db.log.Error("failed to list sources for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing sources for user: %w", err)
	}

	sources := make([]*models.Source, len(rows))
	for i, row := range rows {
		sources[i] = &models.Source{
			ID:                models.SourceID(row.ID),
			MetaIsAutoCreated: row.MetaIsAutoCreated == 1,
			MetaTSField:       row.MetaTSField,
			MetaSeverityField: row.MetaSeverityField,
			Description:       row.Description,
			TTLDays:           row.TTLDays,
			Connection: models.ConnectionInfo{
				Host:      row.Host,
				Username:  row.Username,
				Password:  row.Password,
				Database:  row.Database,
				TableName: row.TableName,
			},
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		}
	}

	db.log.Debug("sources listed for user", "user_id", userID, "count", len(sources))
	return sources, nil
}

// Team query methods are implemented in team_queries.go

// GetTeamByName retrieves a team by its name
func (db *DB) GetTeamByName(ctx context.Context, name string) (*models.Team, error) {
	db.log.Debug("getting team by name", "name", name)

	var team models.Team
	err := db.queries.GetTeamByName.GetContext(ctx, &team, name)
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team by name")
	}

	return &team, nil
}

// TeamHasSource checks if a team has access to a source
func (db *DB) TeamHasSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) (bool, error) {
	db.log.Debug("checking if team has source access", "team_id", teamID, "source_id", sourceID)

	var count int
	err := db.queries.TeamHasSource.GetContext(ctx, &count, teamID, sourceID)
	if err != nil {
		db.log.Error("failed to check team source access", "error", err, "team_id", teamID, "source_id", sourceID)
		return false, fmt.Errorf("error checking team source access: %w", err)
	}

	return count > 0, nil
}

// UserHasSourceAccess checks if a user has access to a source through any team
func (db *DB) UserHasSourceAccess(ctx context.Context, userID models.UserID, sourceID models.SourceID) (bool, error) {
	db.log.Debug("checking if user has source access", "user_id", userID, "source_id", sourceID)

	var count int
	err := db.queries.UserHasSourceAccess.GetContext(ctx, &count, userID, sourceID)
	if err != nil {
		db.log.Error("failed to check user source access", "error", err, "user_id", userID, "source_id", sourceID)
		return false, fmt.Errorf("error checking user source access: %w", err)
	}

	return count > 0, nil
}

// ListTeamsForUser returns all teams a user is a member of
func (db *DB) ListTeamsForUser(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	db.log.Debug("listing teams for user", "user_id", userID)

	var teams []*models.Team
	err := db.queries.ListUserTeams.SelectContext(ctx, &teams, userID)
	if err != nil {
		db.log.Error("failed to list teams for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing teams for user: %w", err)
	}

	db.log.Debug("listed teams for user", "user_id", userID, "count", len(teams))
	return teams, nil
}
