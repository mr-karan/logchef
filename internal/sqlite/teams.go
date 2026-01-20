package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// Team methods

// CreateTeam inserts a new team record.
// Populates the team ID and timestamps on the input model upon success.
func (db *DB) CreateTeam(ctx context.Context, team *models.Team) error {

	params := sqlc.CreateTeamParams{
		Name:        team.Name,
		Description: sql.NullString{String: team.Description, Valid: team.Description != ""},
	}

	id, err := db.writeQueries.CreateTeam(ctx, params)
	if err != nil {
		if IsUniqueConstraintError(err) && strings.Contains(err.Error(), "teams.name") {
			return handleUniqueConstraintError(err, "teams", "name", team.Name)
		}
		db.log.Error("failed to create team record in db", "error", err, "name", team.Name)
		return fmt.Errorf("error creating team: %w", err)
	}

	// Set auto-generated ID.
	team.ID = models.TeamID(id)

	// Fetch the created record to get accurate timestamps.
	teamRow, err := db.readQueries.GetTeam(ctx, id)
	if err != nil {
		db.log.Error("failed to get newly created team record", "error", err, "assigned_id", id)
		return nil // Continue successfully, but timestamps might be inaccurate.
	}

	// Update input model with DB-generated timestamps.
	team.CreatedAt = teamRow.CreatedAt
	team.UpdatedAt = teamRow.UpdatedAt

	return nil
}

// GetTeam retrieves a single team by its ID.
// Returns core.ErrTeamNotFound if not found.
func (db *DB) GetTeam(ctx context.Context, teamID models.TeamID) (*models.Team, error) {

	teamRow, err := db.readQueries.GetTeam(ctx, int64(teamID))
	if err != nil {
		return nil, handleNotFoundError(err, fmt.Sprintf("getting team id %d", teamID))
	}

	// Map sqlc result to domain model.
	team := &models.Team{
		ID:          models.TeamID(teamRow.ID),
		Name:        teamRow.Name,
		Description: teamRow.Description.String,
		Timestamps: models.Timestamps{
			CreatedAt: teamRow.CreatedAt,
			UpdatedAt: teamRow.UpdatedAt,
		},
		// MemberCount is handled by ListTeams query.
	}
	return team, nil
}

// UpdateTeam updates an existing team record.
// The `updated_at` timestamp is automatically set by the query.
func (db *DB) UpdateTeam(ctx context.Context, team *models.Team) error {

	params := sqlc.UpdateTeamParams{
		Name:        team.Name,
		Description: sql.NullString{String: team.Description, Valid: team.Description != ""},
		UpdatedAt:   team.UpdatedAt, // Pass current time or let DB handle? Assuming passed in.
		ID:          int64(team.ID),
	}

	err := db.writeQueries.UpdateTeam(ctx, params)
	if err != nil {
		if IsUniqueConstraintError(err) && strings.Contains(err.Error(), "teams.name") {
			return handleUniqueConstraintError(err, "teams", "name", team.Name)
		}
		db.log.Error("failed to update team record in db", "error", err, "team_id", team.ID)
		return fmt.Errorf("error updating team: %w", err)
	}

	return nil
}

// DeleteTeam removes a team record by ID.
// Associated memberships, source links, and queries should be handled by DB constraints (CASCADE DELETE).
func (db *DB) DeleteTeam(ctx context.Context, teamID models.TeamID) error {

	err := db.writeQueries.DeleteTeam(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to delete team record from db", "error", err, "team_id", teamID)
		return fmt.Errorf("error deleting team: %w", err)
	}

	return nil
}

// ListTeams retrieves all teams along with their member counts.
func (db *DB) ListTeams(ctx context.Context) ([]*models.Team, error) {

	teamRows, err := db.readQueries.ListTeams(ctx)
	if err != nil {
		db.log.Error("failed to list teams from db", "error", err)
		return nil, fmt.Errorf("error listing teams: %w", err)
	}

	// Map sqlc result rows to domain model slice.
	teams := make([]*models.Team, 0, len(teamRows))
	for _, row := range teamRows {
		teams = append(teams, &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: row.Description.String,
			MemberCount: int(row.MemberCount), // Include member count from query.
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		})
	}

	return teams, nil
}

// Team member methods

// AddTeamMember creates an association between a user and a team.
// Note: This method currently checks for team/user existence before inserting.
// Consider if this check is strictly necessary or if relying on FK constraints is sufficient.
func (db *DB) AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {

	// Check team existence (optional, FK constraint might handle).
	_, err := db.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("failed checking team existence: %w", err) // Could be ErrTeamNotFound
	}

	// Check user existence (optional, FK constraint might handle).
	_, err = db.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed checking user existence: %w", err) // Could be ErrUserNotFound
	}

	params := sqlc.AddTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
		Role:   string(role),
	}

	err = db.writeQueries.AddTeamMember(ctx, params)
	if err != nil {
		// Check for primary key violation (user already member of team).
		if IsUniqueConstraintError(err) && (strings.Contains(err.Error(), "team_id") || strings.Contains(err.Error(), "user_id")) {
			// This is often not a fatal error; could indicate an attempt to re-add.
			// Consider returning nil or a specific "already exists" error depending on desired behavior.
			db.log.Warn("attempted to add existing team member", "team_id", teamID, "user_id", userID)
			return nil // Or return fmt.Errorf("user %d is already a member of team %d", userID, teamID)
		}
		db.log.Error("failed to add team member record", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error adding team member: %w", err)
	}

	return nil
}

// GetTeamMember retrieves a specific team membership record.
// Returns nil, nil if the user is not a member of the team.
func (db *DB) GetTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error) {

	memberRow, err := db.readQueries.GetTeamMember(ctx, sqlc.GetTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
	})
	if err != nil {
		// Map ErrNoRows to nil, nil for "not found".
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		db.log.Error("failed to get team member record from db", "error", err, "team_id", teamID, "user_id", userID)
		return nil, fmt.Errorf("error getting team member: %w", err)
	}

	// Map sqlc result to domain model.
	member := &models.TeamMember{
		TeamID:    models.TeamID(memberRow.TeamID),
		UserID:    models.UserID(memberRow.UserID),
		Role:      models.TeamRole(memberRow.Role),
		CreatedAt: memberRow.CreatedAt,
	}
	return member, nil
}

// UpdateTeamMemberRole updates the role of an existing team member.
// Note: This method currently checks for member existence first.
func (db *DB) UpdateTeamMemberRole(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {

	// Check if the member exists first (optional, UPDATE might handle non-existence gracefully).
	_, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		return fmt.Errorf("failed checking team member existence before update: %w", err)
	}

	params := sqlc.UpdateTeamMemberRoleParams{
		Role:   string(role),
		TeamID: int64(teamID),
		UserID: int64(userID),
	}
	err = db.writeQueries.UpdateTeamMemberRole(ctx, params)
	if err != nil {
		db.log.Error("failed to update team member role in db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error updating team member role: %w", err)
	}

	return nil
}

// RemoveTeamMember removes a user's membership from a team.
func (db *DB) RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error {

	params := sqlc.RemoveTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
	}
	err := db.writeQueries.RemoveTeamMember(ctx, params)
	if err != nil {
		// DELETE often doesn't error if the row doesn't exist.
		db.log.Error("failed to remove team member record from db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error removing team member: %w", err)
	}

	return nil
}

// ListTeamMembers retrieves basic membership info for all members of a team.
func (db *DB) ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {

	memberRows, err := db.readQueries.ListTeamMembers(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list team members from db", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team members: %w", err)
	}

	// Map results.
	members := make([]*models.TeamMember, 0, len(memberRows))
	for _, row := range memberRows {
		members = append(members, &models.TeamMember{
			TeamID:    models.TeamID(row.TeamID),
			UserID:    models.UserID(row.UserID),
			Role:      models.TeamRole(row.Role),
			CreatedAt: row.CreatedAt,
		})
	}

	return members, nil
}

// ListTeamMembersWithDetails retrieves membership info including user email and full name.
func (db *DB) ListTeamMembersWithDetails(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {

	memberRows, err := db.readQueries.ListTeamMembersWithDetails(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list team members with details from db", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team members with details: %w", err)
	}

	// Map results including joined user fields.
	members := make([]*models.TeamMember, 0, len(memberRows))
	for _, row := range memberRows {
		members = append(members, &models.TeamMember{
			TeamID:    models.TeamID(row.TeamID),
			UserID:    models.UserID(row.UserID),
			Role:      models.TeamRole(row.Role),
			Email:     row.Email,    // From joined users table
			FullName:  row.FullName, // From joined users table
			CreatedAt: row.CreatedAt,
		})
	}

	return members, nil
}

// ListUserTeams retrieves all teams a specific user is a member of.
func (db *DB) ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error) {

	teamRows, err := db.readQueries.ListUserTeams(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list teams for user from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing teams for user: %w", err)
	}

	// Map results to domain model.
	teams := make([]*models.Team, 0, len(teamRows))
	for _, row := range teamRows {
		teams = append(teams, &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: row.Description.String,
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
			// MemberCount not included in this query.
		})
	}

	return teams, nil
}

// Team source methods

// AddTeamSource creates an association between a team and a data source.
func (db *DB) AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {

	// Existence checks for team/source are optional here, FK constraints handle integrity.
	// _, err := db.GetTeam(ctx, teamID) ...
	// _, err = db.GetSource(ctx, sourceID) ...

	params := sqlc.AddTeamSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	err := db.writeQueries.AddTeamSource(ctx, params)
	if err != nil {
		// Check for primary key violation (association already exists).
		if IsUniqueConstraintError(err) && (strings.Contains(err.Error(), "team_id") || strings.Contains(err.Error(), "source_id")) {
			db.log.Warn("attempted to add existing team-source association", "team_id", teamID, "source_id", sourceID)
			return nil // Treat as success/no-op if association already exists.
		}
		db.log.Error("failed to add team source record", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error adding team source: %w", err)
	}

	return nil
}

// RemoveTeamSource removes the association between a team and a data source.
func (db *DB) RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {

	params := sqlc.RemoveTeamSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	err := db.writeQueries.RemoveTeamSource(ctx, params)
	if err != nil {
		// DELETE often doesn't error if the row doesn't exist.
		db.log.Error("failed to remove team source record from db", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error removing team source: %w", err)
	}

	return nil
}

// ListTeamSources retrieves all data sources associated with a specific team.
func (db *DB) ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error) {

	sourceRows, err := db.readQueries.ListTeamSources(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to list team sources from db", "error", err, "team_id", teamID)
		return nil, fmt.Errorf("error listing team sources: %w", err)
	}

	// Map results. Note: mapSourceRowToModel is in utility.go.
	sources := make([]*models.Source, 0, len(sourceRows))
	for i := range sourceRows {
		mappedSource := mapSourceRowToModel(&sourceRows[i])
		if mappedSource != nil {
			sources = append(sources, mappedSource)
		}
	}

	return sources, nil
}

// ListSourceTeams retrieves all teams that have access to a specific data source.
func (db *DB) ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error) {

	teamRows, err := db.readQueries.ListSourceTeams(ctx, int64(sourceID))
	if err != nil {
		db.log.Error("failed to list source teams from db", "error", err, "source_id", sourceID)
		return nil, fmt.Errorf("error listing source teams: %w", err)
	}

	// Map results.
	teams := make([]*models.Team, 0, len(teamRows))
	for _, row := range teamRows {
		teams = append(teams, &models.Team{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: row.Description.String,
			Timestamps: models.Timestamps{
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		})
	}

	return teams, nil
}

// ListSourcesForUser lists all unique sources a user has access to across all their teams.
func (db *DB) ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error) {

	sourceRows, err := db.readQueries.ListSourcesForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list sources for user from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing sources for user: %w", err)
	}

	// Map results using the shared mapper.
	sources := make([]*models.Source, 0, len(sourceRows))
	for i := range sourceRows {
		mappedSource := mapSourceRowToModel(&sourceRows[i])
		if mappedSource != nil {
			sources = append(sources, mappedSource)
		}
	}

	return sources, nil
}

// GetTeamByName retrieves a team by its unique name.
func (db *DB) GetTeamByName(ctx context.Context, name string) (*models.Team, error) {

	teamRow, err := db.readQueries.GetTeamByName(ctx, name)
	if err != nil {
		// Use handleNotFoundError for consistent not-found mapping.
		return nil, handleNotFoundError(err, fmt.Sprintf("getting team name %s", name))
	}

	// Map result.
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

// TeamHasSource checks if a specific team has been granted access to a specific source.
func (db *DB) TeamHasSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) (bool, error) {

	count, err := db.readQueries.TeamHasSource(ctx, sqlc.TeamHasSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to check team source access in db", "error", err, "team_id", teamID, "source_id", sourceID)
		return false, fmt.Errorf("error checking team source access: %w", err)
	}

	return count > 0, nil
}

// UserHasSourceAccess checks if a user can access a specific source through any of their team memberships.
func (db *DB) UserHasSourceAccess(ctx context.Context, userID models.UserID, sourceID models.SourceID) (bool, error) {

	count, err := db.readQueries.UserHasSourceAccess(ctx, sqlc.UserHasSourceAccessParams{
		UserID:   int64(userID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to check user source access in db", "error", err, "user_id", userID, "source_id", sourceID)
		return false, fmt.Errorf("error checking user source access: %w", err)
	}

	return count > 0, nil
}

// ListTeamsForUser retrieves all teams a specific user is a member of, along with their role and team member count.
// This function now returns the raw sqlc-generated rows, and mapping is handled in the core layer.
func (db *DB) ListTeamsForUser(ctx context.Context, userID models.UserID) ([]sqlc.ListTeamsForUserRow, error) {

	// This calls the sqlc generated function for the modified "ListTeamsForUser" query
	teamRows, err := db.readQueries.ListTeamsForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list teams for user (with role and count) from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing teams for user (with role and count): %w", err)
	}

	// No mapping here; return the direct sqlc result.
	// The core layer (core.ListTeamsForUser) will handle mapping to models.UserTeamDetails.
	return teamRows, nil
}
