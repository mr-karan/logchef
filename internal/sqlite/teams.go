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
	db.log.Debug("creating team record", "name", team.Name)

	params := sqlc.CreateTeamParams{
		Name:        team.Name,
		Description: sql.NullString{String: team.Description, Valid: team.Description != ""},
	}

	id, err := db.queries.CreateTeam(ctx, params)
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
	teamRow, err := db.queries.GetTeam(ctx, id)
	if err != nil {
		db.log.Error("failed to get newly created team record", "error", err, "assigned_id", id)
		return nil // Continue successfully, but timestamps might be inaccurate.
	}

	// Update input model with DB-generated timestamps.
	team.CreatedAt = teamRow.CreatedAt
	team.UpdatedAt = teamRow.UpdatedAt

	db.log.Debug("team record created successfully", "team_id", team.ID)
	return nil
}

// GetTeam retrieves a single team by its ID.
// Returns core.ErrTeamNotFound if not found.
func (db *DB) GetTeam(ctx context.Context, teamID models.TeamID) (*models.Team, error) {
	db.log.Debug("getting team record", "team_id", teamID)

	teamRow, err := db.queries.GetTeam(ctx, int64(teamID))
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
	db.log.Debug("updating team record", "team_id", team.ID, "name", team.Name)

	params := sqlc.UpdateTeamParams{
		Name:        team.Name,
		Description: sql.NullString{String: team.Description, Valid: team.Description != ""},
		UpdatedAt:   team.UpdatedAt, // Pass current time or let DB handle? Assuming passed in.
		ID:          int64(team.ID),
	}

	err := db.queries.UpdateTeam(ctx, params)
	if err != nil {
		if IsUniqueConstraintError(err) && strings.Contains(err.Error(), "teams.name") {
			return handleUniqueConstraintError(err, "teams", "name", team.Name)
		}
		db.log.Error("failed to update team record in db", "error", err, "team_id", team.ID)
		return fmt.Errorf("error updating team: %w", err)
	}

	db.log.Debug("team record updated successfully", "team_id", team.ID)
	return nil
}

// DeleteTeam removes a team record by ID.
// Associated memberships, source links, and queries should be handled by DB constraints (CASCADE DELETE).
func (db *DB) DeleteTeam(ctx context.Context, teamID models.TeamID) error {
	db.log.Debug("deleting team record", "team_id", teamID)

	err := db.queries.DeleteTeam(ctx, int64(teamID))
	if err != nil {
		db.log.Error("failed to delete team record from db", "error", err, "team_id", teamID)
		return fmt.Errorf("error deleting team: %w", err)
	}

	db.log.Debug("team record deleted successfully", "team_id", teamID)
	return nil
}

// ListTeams retrieves all teams along with their member counts.
func (db *DB) ListTeams(ctx context.Context) ([]*models.Team, error) {
	db.log.Debug("listing team records")

	teamRows, err := db.queries.ListTeams(ctx)
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

	db.log.Debug("team records listed", "count", len(teams))
	return teams, nil
}

// Team member methods

// AddTeamMember creates an association between a user and a team.
// Note: This method currently checks for team/user existence before inserting.
// Consider if this check is strictly necessary or if relying on FK constraints is sufficient.
func (db *DB) AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	db.log.Debug("adding team member record", "team_id", teamID, "user_id", userID, "role", role)

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

	err = db.queries.AddTeamMember(ctx, params)
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

	db.log.Debug("team member record added successfully", "team_id", teamID, "user_id", userID)
	return nil
}

// GetTeamMember retrieves a specific team membership record.
// Returns nil, nil if the user is not a member of the team.
func (db *DB) GetTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error) {
	db.log.Debug("getting team member record", "team_id", teamID, "user_id", userID)

	memberRow, err := db.queries.GetTeamMember(ctx, sqlc.GetTeamMemberParams{
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
	db.log.Debug("updating team member role", "team_id", teamID, "user_id", userID, "new_role", role)

	// Check if the member exists first (optional, UPDATE might handle non-existence gracefully).
	_, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		return fmt.Errorf("failed checking team member existence before update: %w", err)
	}
	if err == nil && false { // Placeholder if GetTeamMember returns nil,nil on not found
		return fmt.Errorf("team member (%d, %d) not found for update", teamID, userID)
	}

	params := sqlc.UpdateTeamMemberRoleParams{
		Role:   string(role),
		TeamID: int64(teamID),
		UserID: int64(userID),
	}
	err = db.queries.UpdateTeamMemberRole(ctx, params)
	if err != nil {
		db.log.Error("failed to update team member role in db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error updating team member role: %w", err)
	}

	db.log.Debug("team member role updated successfully", "team_id", teamID, "user_id", userID)
	return nil
}

// RemoveTeamMember removes a user's membership from a team.
func (db *DB) RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error {
	db.log.Debug("removing team member record", "team_id", teamID, "user_id", userID)

	params := sqlc.RemoveTeamMemberParams{
		TeamID: int64(teamID),
		UserID: int64(userID),
	}
	err := db.queries.RemoveTeamMember(ctx, params)
	if err != nil {
		// DELETE often doesn't error if the row doesn't exist.
		db.log.Error("failed to remove team member record from db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error removing team member: %w", err)
	}

	db.log.Debug("team member record removed successfully", "team_id", teamID, "user_id", userID)
	return nil
}

// ListTeamMembers retrieves basic membership info for all members of a team.
func (db *DB) ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	db.log.Debug("listing team members (basic)", "team_id", teamID)

	memberRows, err := db.queries.ListTeamMembers(ctx, int64(teamID))
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

	db.log.Debug("basic team members listed", "team_id", teamID, "count", len(members))
	return members, nil
}

// ListTeamMembersWithDetails retrieves membership info including user email and full name.
func (db *DB) ListTeamMembersWithDetails(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	db.log.Debug("listing team members with details", "team_id", teamID)

	memberRows, err := db.queries.ListTeamMembersWithDetails(ctx, int64(teamID))
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

	db.log.Debug("team members with details listed", "team_id", teamID, "count", len(members))
	return members, nil
}

// ListUserTeams retrieves all teams a specific user is a member of.
func (db *DB) ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	db.log.Debug("listing teams for user", "user_id", userID)

	teamRows, err := db.queries.ListUserTeams(ctx, int64(userID))
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

	db.log.Debug("teams listed for user", "user_id", userID, "count", len(teams))
	return teams, nil
}

// Team source methods

// AddTeamSource creates an association between a team and a data source.
func (db *DB) AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	db.log.Debug("adding team source record", "team_id", teamID, "source_id", sourceID)

	// Existence checks for team/source are optional here, FK constraints handle integrity.
	// _, err := db.GetTeam(ctx, teamID) ...
	// _, err = db.GetSource(ctx, sourceID) ...

	params := sqlc.AddTeamSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	err := db.queries.AddTeamSource(ctx, params)
	if err != nil {
		// Check for primary key violation (association already exists).
		if IsUniqueConstraintError(err) && (strings.Contains(err.Error(), "team_id") || strings.Contains(err.Error(), "source_id")) {
			db.log.Warn("attempted to add existing team-source association", "team_id", teamID, "source_id", sourceID)
			return nil // Treat as success/no-op if association already exists.
		}
		db.log.Error("failed to add team source record", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error adding team source: %w", err)
	}

	db.log.Debug("team source record added successfully", "team_id", teamID, "source_id", sourceID)
	return nil
}

// RemoveTeamSource removes the association between a team and a data source.
func (db *DB) RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	db.log.Debug("removing team source record", "team_id", teamID, "source_id", sourceID)

	params := sqlc.RemoveTeamSourceParams{
		TeamID:   int64(teamID),
		SourceID: int64(sourceID),
	}
	err := db.queries.RemoveTeamSource(ctx, params)
	if err != nil {
		// DELETE often doesn't error if the row doesn't exist.
		db.log.Error("failed to remove team source record from db", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error removing team source: %w", err)
	}

	db.log.Debug("team source record removed successfully", "team_id", teamID, "source_id", sourceID)
	return nil
}

// ListTeamSources retrieves all data sources associated with a specific team.
func (db *DB) ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error) {
	db.log.Debug("listing sources for team", "team_id", teamID)

	sourceRows, err := db.queries.ListTeamSources(ctx, int64(teamID))
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

	db.log.Debug("sources listed for team", "team_id", teamID, "count", len(sources))
	return sources, nil
}

// ListSourceTeams retrieves all teams that have access to a specific data source.
func (db *DB) ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error) {
	db.log.Debug("listing teams for source", "source_id", sourceID)

	teamRows, err := db.queries.ListSourceTeams(ctx, int64(sourceID))
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

	db.log.Debug("teams listed for source", "source_id", sourceID, "count", len(teams))
	return teams, nil
}

// ListSourcesForUser lists all unique sources a user has access to across all their teams.
func (db *DB) ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error) {
	db.log.Debug("listing sources accessible by user", "user_id", userID)

	sourceRows, err := db.queries.ListSourcesForUser(ctx, int64(userID))
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

	db.log.Debug("sources listed for user", "user_id", userID, "count", len(sources))
	return sources, nil
}

// GetTeamByName retrieves a team by its unique name.
func (db *DB) GetTeamByName(ctx context.Context, name string) (*models.Team, error) {
	db.log.Debug("getting team record by name", "name", name)

	teamRow, err := db.queries.GetTeamByName(ctx, name)
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
	db.log.Debug("checking team source access", "team_id", teamID, "source_id", sourceID)

	count, err := db.queries.TeamHasSource(ctx, sqlc.TeamHasSourceParams{
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
	db.log.Debug("checking user source access via teams", "user_id", userID, "source_id", sourceID)

	count, err := db.queries.UserHasSourceAccess(ctx, sqlc.UserHasSourceAccessParams{
		UserID:   int64(userID),
		SourceID: int64(sourceID),
	})
	if err != nil {
		db.log.Error("failed to check user source access in db", "error", err, "user_id", userID, "source_id", sourceID)
		return false, fmt.Errorf("error checking user source access: %w", err)
	}

	return count > 0, nil
}

// ListTeamsForUser retrieves all teams a specific user is a member of.
// This is functionally similar to ListUserTeams but named for consistency.
func (db *DB) ListTeamsForUser(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	db.log.Debug("listing teams for user", "user_id", userID) // Reuses ListUserTeams query

	teamRows, err := db.queries.ListTeamsForUser(ctx, int64(userID))
	if err != nil {
		db.log.Error("failed to list teams for user from db", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing teams for user: %w", err)
	}

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

	db.log.Debug("teams listed for user", "user_id", userID, "count", len(teams))
	return teams, nil
}
