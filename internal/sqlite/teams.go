package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// Team methods

// CreateTeam creates a new team
func (db *DB) CreateTeam(ctx context.Context, team *models.Team) error {
	db.log.Debug("creating team", "name", team.Name)

	id, err := db.queries.CreateTeam(ctx, sqlc.CreateTeamParams{
		Name:        team.Name,
		Description: sql.NullString{String: team.Description, Valid: team.Description != ""},
	})

	if err != nil {
		if isUniqueConstraintError(err, "teams", "name") {
			return fmt.Errorf("team with name %s already exists", team.Name)
		}
		db.log.Error("failed to create team", "error", err)
		return fmt.Errorf("error creating team: %w", err)
	}

	// Set the auto-generated ID
	team.ID = models.TeamID(id)

	// Get the team to fetch the timestamps
	teamRow, err := db.queries.GetTeam(ctx, int64(id))
	if err != nil {
		db.log.Error("failed to get created team", "error", err)
		return fmt.Errorf("error getting created team: %w", err)
	}

	// Set the timestamps from the database
	team.CreatedAt = teamRow.CreatedAt
	team.UpdatedAt = teamRow.UpdatedAt

	db.log.Debug("team created", "team_id", team.ID)
	return nil
}

// GetTeam retrieves a team by ID
func (db *DB) GetTeam(ctx context.Context, teamID models.TeamID) (*models.Team, error) {
	db.log.Debug("getting team", "team_id", teamID)

	teamRow, err := db.queries.GetTeam(ctx, int64(teamID))
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team")
	}

	team := &models.Team{
		ID:          models.TeamID(teamRow.ID),
		Name:        teamRow.Name,
		Description: teamRow.Description.String,
		Timestamps: models.Timestamps{
			CreatedAt: teamRow.CreatedAt,
			UpdatedAt: teamRow.UpdatedAt,
		},
	}
	return team, nil
}

// UpdateTeam updates an existing team
func (db *DB) UpdateTeam(ctx context.Context, team *models.Team) error {
	db.log.Debug("updating team", "team_id", team.ID, "name", team.Name)

	err := db.queries.UpdateTeam(ctx, sqlc.UpdateTeamParams{
		Name:        team.Name,
		Description: sql.NullString{String: team.Description, Valid: team.Description != ""},
		UpdatedAt:   team.UpdatedAt,
		ID:          int64(team.ID),
	})
	if err != nil {
		if isUniqueConstraintError(err, "teams", "name") {
			return fmt.Errorf("team with name %s already exists", team.Name)
		}
		db.log.Error("failed to update team", "error", err, "team_id", team.ID)
		return fmt.Errorf("error updating team: %w", err)
	}

	return nil
}

// DeleteTeam deletes a team by ID
func (db *DB) DeleteTeam(ctx context.Context, teamID models.TeamID) error {
	db.log.Debug("deleting team", "team_id", teamID)

	err := db.queries.DeleteTeam(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to delete team", "error", err, "team_id", teamID)
		return fmt.Errorf("error deleting team: %w", err)
	}

	return nil
}

// ListTeams returns all teams
func (db *DB) ListTeams(ctx context.Context) ([]*models.Team, error) {
	db.log.Debug("listing teams")

	teamRows, err := db.queries.ListTeams(ctx)
	if err != nil {
		db.log.Error("failed to list teams", "error", err)
		return nil, fmt.Errorf("error listing teams: %w", err)
	}

	teams := make([]*models.Team, len(teamRows))
	for i, row := range teamRows {
		teams[i] = &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: row.Description.String,
			MemberCount: int(row.MemberCount),
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		}
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
	err = db.queries.AddTeamMember(ctx, sqlc.AddTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
		Role:   string(role),
	})
	if err != nil {
		if isUniqueConstraintError(err, "team_members", "team_id") {
			return fmt.Errorf("user %d is already a member of team %d", userID, teamID)
		}
		db.log.Error("failed to add team member", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error adding team member: %w", err)
	}

	return nil
}

// GetTeamMember gets a team member
func (db *DB) GetTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error) {
	db.log.Debug("getting team member", "team_id", teamID, "user_id", userID)

	memberRow, err := db.queries.GetTeamMember(ctx, sqlc.GetTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
	})
	if err != nil {
		return nil, handleNotFoundError(err, "error getting team member")
	}

	member := &models.TeamMember{
		TeamID: models.TeamID(memberRow.TeamID),
		UserID: models.UserID(memberRow.UserID),
		Role:   models.TeamRole(memberRow.Role),
	}
	return member, nil
}

// UpdateTeamMemberRole updates a team member's role
func (db *DB) UpdateTeamMemberRole(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	db.log.Debug("updating team member role", "team_id", teamID, "user_id", userID, "role", role)

	// First check if the team member exists
	_, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		return fmt.Errorf("error checking team member existence: %w", err)
	}

	err = db.queries.UpdateTeamMemberRole(ctx, sqlc.UpdateTeamMemberRoleParams{
		Role:   string(role),
		TeamID: int64(teamID),
		UserID: int64(userID),
	})
	if err != nil {
		db.log.Error("failed to update team member role", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error updating team member role: %w", err)
	}

	return nil
}

// RemoveTeamMember removes a member from a team
func (db *DB) RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error {
	db.log.Debug("removing team member", "team_id", teamID, "user_id", userID)

	err := db.queries.RemoveTeamMember(ctx, sqlc.RemoveTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
	})
	if err != nil {
		db.log.Error("failed to remove team member", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error removing team member: %w", err)
	}

	return nil
}

// ListTeamMembers lists all members of a team
func (db *DB) ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	db.log.Debug("listing team members", "team_id", teamID)

	memberRows, err := db.queries.ListTeamMembers(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list team members", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team members: %w", err)
	}

	members := make([]*models.TeamMember, len(memberRows))
	for i, row := range memberRows {
		members[i] = &models.TeamMember{
			TeamID: models.TeamID(row.TeamID),
			UserID: models.UserID(row.UserID),
			Role:   models.TeamRole(row.Role),
		}
	}

	db.log.Debug("team members listed", "team_id", teamID, "count", len(members))
	return members, nil
}

// ListTeamMembersWithDetails lists all members of a team with additional details
func (db *DB) ListTeamMembersWithDetails(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	db.log.Debug("listing team members with details", "team_id", teamID)

	memberRows, err := db.queries.ListTeamMembersWithDetails(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list team members with details", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team members with details: %w", err)
	}

	members := make([]*models.TeamMember, len(memberRows))
	for i, row := range memberRows {
		members[i] = &models.TeamMember{
			TeamID:    models.TeamID(row.TeamID),
			UserID:    models.UserID(row.UserID),
			Role:      models.TeamRole(row.Role),
			Email:     row.Email,
			FullName:  row.FullName,
			CreatedAt: row.CreatedAt,
		}
	}

	db.log.Debug("team members with details listed", "team_id", teamID, "count", len(members))
	return members, nil
}

// ListUserTeams lists all teams a user is a member of
func (db *DB) ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	db.log.Debug("listing user teams", "user_id", userID)

	teamRows, err := db.queries.ListUserTeams(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list user teams", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing user teams: %w", err)
	}

	teams := make([]*models.Team, len(teamRows))
	for i, row := range teamRows {
		teams[i] = &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: row.Description.String,
		}
	}

	db.log.Debug("user teams listed", "user_id", userID, "count", len(teams))
	return teams, nil
}

// Team source methods

// AddTeamSource adds a source to a team
func (db *DB) AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	db.log.Debug("adding team source", "team_id", teamID, "source_id", sourceID)

	// First check if the team exists
	_, err := db.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error checking team existence: %w", err)
	}

	// Then check if the source exists
	_, err = db.GetSource(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("error checking source existence: %w", err)
	}

	// Finally add the team source
	err = db.queries.AddTeamSource(ctx, sqlc.AddTeamSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		if isUniqueConstraintError(err, "team_sources", "team_id") {
			return fmt.Errorf("source %d is already associated with team %d", sourceID, teamID)
		}
		db.log.Error("failed to add team source", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error adding team source: %w", err)
	}

	return nil
}

// RemoveTeamSource removes a source from a team
func (db *DB) RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	db.log.Debug("removing team source", "team_id", teamID, "source_id", sourceID)

	err := db.queries.RemoveTeamSource(ctx, sqlc.RemoveTeamSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to remove team source", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error removing team source: %w", err)
	}

	return nil
}

// ListTeamSources lists all sources associated with a team
func (db *DB) ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error) {
	db.log.Debug("listing team sources", "team_id", teamID)

	sourceRows, err := db.queries.ListTeamSources(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list team sources", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team sources: %w", err)
	}

	sources := make([]*models.Source, len(sourceRows))
	for i, row := range sourceRows {
		sources[i] = &models.Source{
			ID:   models.SourceID(row.ID),
			Name: row.Name,
			Connection: models.ConnectionInfo{
				Database:  row.Database,
				TableName: row.TableName,
			},
			Description: row.Description.String,
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		}
	}

	db.log.Debug("team sources listed", "team_id", teamID, "count", len(sources))
	return sources, nil
}

// ListSourceTeams lists all teams that have access to a source
func (db *DB) ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error) {
	db.log.Debug("listing source teams", "source_id", sourceID)

	teamRows, err := db.queries.ListSourceTeams(ctx, int64(sourceID))
	if err != nil {
		db.log.Error("failed to list source teams", "error", err, "source_id", sourceID)
		return nil, fmt.Errorf("error listing source teams: %w", err)
	}

	teams := make([]*models.Team, len(teamRows))
	for i, row := range teamRows {
		teams[i] = &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: row.Description.String,
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		}
	}

	db.log.Debug("source teams listed", "source_id", sourceID, "count", len(teams))
	return teams, nil
}

// ListSourcesForUser lists all unique sources a user has access to across all their teams
func (db *DB) ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error) {
	db.log.Debug("listing sources for user", "user_id", userID)

	sourceRows, err := db.queries.ListSourcesForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list sources for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing sources for user: %w", err)
	}

	sources := make([]*models.Source, len(sourceRows))
	for i, row := range sourceRows {
		metaSeverityField := ""
		if row.MetaSeverityField.Valid {
			metaSeverityField = row.MetaSeverityField.String
		}

		sources[i] = &models.Source{
			ID:                models.SourceID(row.ID),
			Name:              row.Name,
			MetaIsAutoCreated: row.MetaIsAutoCreated == 1,
			MetaTSField:       row.MetaTsField,
			MetaSeverityField: metaSeverityField,
			Description:       row.Description.String,
			TTLDays:           int(row.TtlDays),
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

// GetTeamByName gets a team by name
func (db *DB) GetTeamByName(ctx context.Context, name string) (*models.Team, error) {
	db.log.Debug("getting team by name", "name", name)

	teamRow, err := db.queries.GetTeamByName(ctx, name)
	if err != nil {
		return nil, handleNotFoundError(err, "failed to get team by name")
	}

	team := &models.Team{
		ID:          models.TeamID(teamRow.ID),
		Name:        teamRow.Name,
		Description: teamRow.Description.String,
		Timestamps: models.Timestamps{
			CreatedAt: teamRow.CreatedAt,
			UpdatedAt: teamRow.UpdatedAt,
		},
	}
	return team, nil
}

// TeamHasSource checks if a team has access to a source
func (db *DB) TeamHasSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) (bool, error) {
	db.log.Debug("checking team source access", "team_id", teamID, "source_id", sourceID)

	count, err := db.queries.TeamHasSource(ctx, sqlc.TeamHasSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to check team source access", "error", err, "team_id", teamID, "source_id", sourceID)
		return false, fmt.Errorf("error checking team source access: %w", err)
	}

	return count > 0, nil
}

// UserHasSourceAccess checks if a user has access to a source through any team
func (db *DB) UserHasSourceAccess(ctx context.Context, userID models.UserID, sourceID models.SourceID) (bool, error) {
	db.log.Debug("checking user source access", "user_id", userID, "source_id", sourceID)

	count, err := db.queries.UserHasSourceAccess(ctx, sqlc.UserHasSourceAccessParams{
		UserID:   int64(userID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to check user source access", "error", err, "user_id", userID, "source_id", sourceID)
		return false, fmt.Errorf("error checking user source access: %w", err)
	}

	return count > 0, nil
}

// ListTeamsForUser lists all teams a user is a member of
func (db *DB) ListTeamsForUser(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	db.log.Debug("listing teams for user", "user_id", userID)

	teamRows, err := db.queries.ListUserTeams(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list teams for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing teams for user: %w", err)
	}

	teams := make([]*models.Team, len(teamRows))
	for i, row := range teamRows {
		teams[i] = &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: row.Description.String,
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		}
	}

	db.log.Debug("teams listed for user", "user_id", userID, "count", len(teams))
	return teams, nil
}
