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
	db.log.Debug("creating team", "team_id", team.ID, "name", team.Name)

	result, err := db.queries.CreateTeam.ExecContext(ctx,
		team.ID,
		team.Name,
		team.Description,
	)
	if err != nil {
		if isUniqueConstraintError(err, "teams", "name") {
			return fmt.Errorf("team with name %s already exists", team.Name)
		}
		db.log.Error("failed to create team", "error", err, "team_id", team.ID)
		return fmt.Errorf("error creating team: %w", err)
	}

	if err := checkRowsAffected(result, "create team"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", team.ID)
		return err
	}

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
		time.Now(),
	)
	if err != nil {
		if isUniqueConstraintError(err, "team_members", "team_id") {
			return fmt.Errorf("user %s is already a member of team %s", userID, teamID)
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
	result, err := db.queries.AddTeamSource.ExecContext(ctx, teamID, sourceID, time.Now())
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
		ID          string    `db:"id"`
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

// Team query methods

// CreateTeamQuery creates a new team query
func (db *DB) CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	db.log.Debug("creating team query", "team_id", query.TeamID, "query_id", query.ID, "name", query.Name)

	now := time.Now()
	query.CreatedAt = now
	query.UpdatedAt = now

	result, err := db.queries.CreateTeamQuery.ExecContext(ctx,
		query.ID,
		query.TeamID,
		query.Name,
		query.Description,
		query.QueryContent,
		query.CreatedAt,
		query.UpdatedAt,
	)
	if err != nil {
		db.log.Error("failed to create team query", "error", err, "team_id", query.TeamID, "query_id", query.ID)
		return fmt.Errorf("error creating team query: %w", err)
	}

	if err := checkRowsAffected(result, "create team query"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "team_id", query.TeamID, "query_id", query.ID)
		return err
	}

	return nil
}

// GetTeamQuery gets a team query by ID
func (db *DB) GetTeamQuery(ctx context.Context, queryID string) (*models.TeamQuery, error) {
	db.log.Debug("getting team query", "query_id", queryID)

	var query models.TeamQuery
	err := db.queries.GetTeamQuery.GetContext(ctx, &query, queryID)
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team query")
	}
	return &query, nil
}

// UpdateTeamQuery updates a team query
func (db *DB) UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	db.log.Debug("updating team query", "query_id", query.ID, "team_id", query.TeamID, "name", query.Name)

	query.UpdatedAt = time.Now()

	result, err := db.queries.UpdateTeamQuery.ExecContext(ctx,
		query.Name,
		query.Description,
		query.QueryContent,
		query.UpdatedAt,
		query.ID,
	)
	if err != nil {
		db.log.Error("failed to update team query", "error", err, "query_id", query.ID, "team_id", query.TeamID)
		return fmt.Errorf("error updating team query: %w", err)
	}

	if err := checkRowsAffected(result, "update team query"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "query_id", query.ID, "team_id", query.TeamID)
		return err
	}

	return nil
}

// DeleteTeamQuery deletes a team query
func (db *DB) DeleteTeamQuery(ctx context.Context, queryID string) error {
	db.log.Debug("deleting team query", "query_id", queryID)

	result, err := db.queries.DeleteTeamQuery.ExecContext(ctx, queryID)
	if err != nil {
		db.log.Error("failed to delete team query", "error", err, "query_id", queryID)
		return fmt.Errorf("error deleting team query: %w", err)
	}

	if err := checkRowsAffected(result, "delete team query"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "query_id", queryID)
		return err
	}

	return nil
}

// ListTeamQueries lists all queries for a team
func (db *DB) ListTeamQueries(ctx context.Context, teamID models.TeamID) ([]*models.TeamQuery, error) {
	db.log.Debug("listing team queries", "team_id", teamID)

	var queries []*models.TeamQuery
	err := db.queries.ListTeamQueries.SelectContext(ctx, &queries, teamID)
	if err != nil {
		db.log.Error("failed to list team queries", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team queries: %w", err)
	}

	db.log.Debug("team queries listed", "team_id", teamID, "count", len(queries))
	return queries, nil
}
