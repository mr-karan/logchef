package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// Note: Error definitions (ErrUserNotFound, ErrTeamNotFound, etc.)
// and IsNotFoundError are defined in users.go

// --- Team Validation Functions ---

// validateTeamCreation validates parameters for creating a new team.
func validateTeamCreation(name, description string) error {
	if name == "" {
		return &ValidationError{Field: "name", Message: "team name is required"}
	}
	if len(name) < 2 || len(name) > 50 {
		return &ValidationError{Field: "name", Message: "team name must be between 2 and 50 characters"}
	}
	if !isValidTeamName(name) { // Assumes isValidTeamName is in validation.go
		return &ValidationError{Field: "name", Message: "team name contains invalid characters"}
	}
	if len(description) > 500 {
		return &ValidationError{Field: "description", Message: "description must not exceed 500 characters"}
	}
	return nil
}

// validateTeamUpdate validates parameters for updating a team.
func validateTeamUpdate(updateData models.Team) error {
	// Name validation (if provided)
	if updateData.Name != "" {
		if len(updateData.Name) < 2 || len(updateData.Name) > 50 {
			return &ValidationError{Field: "name", Message: "team name must be between 2 and 50 characters"}
		}
		if !isValidTeamName(updateData.Name) { // Assumes isValidTeamName is in validation.go
			return &ValidationError{Field: "name", Message: "team name contains invalid characters"}
		}
	}
	// Description validation (if provided)
	// Allow empty description, just check length
	if len(updateData.Description) > 500 {
		return &ValidationError{Field: "description", Message: "description must not exceed 500 characters"}
	}
	return nil
}

// validateTeamMember validates parameters for adding/updating a team member.
func validateTeamMember(teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	if teamID <= 0 {
		return &ValidationError{Field: "teamID", Message: "valid team ID is required"}
	}
	if userID <= 0 {
		return &ValidationError{Field: "userID", Message: "valid user ID is required"}
	}
	if role != models.TeamRoleAdmin && role != models.TeamRoleMember && role != models.TeamRoleEditor {
		return &ValidationError{Field: "role", Message: "role must be 'admin', 'editor', or 'member'"}
	}
	return nil
}

// --- Team Management Functions ---

// ListTeams returns all teams from the database.
func ListTeams(ctx context.Context, db *sqlite.DB) ([]*models.Team, error) {
	teams, err := db.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing teams: %w", err)
	}
	return teams, nil
}

// GetTeam retrieves a specific team by its ID.
func GetTeam(ctx context.Context, db *sqlite.DB, id models.TeamID) (*models.Team, error) {
	team, err := db.GetTeam(ctx, id)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsTeamNotFoundError(err) {
			return nil, ErrTeamNotFound
		}
		return nil, fmt.Errorf("error getting team from db: %w", err)
	}
	return team, nil
}

// GetTeamByName retrieves a specific team by its name.
func GetTeamByName(ctx context.Context, db *sqlite.DB, name string) (*models.Team, error) {
	team, err := db.GetTeamByName(ctx, name)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsTeamNotFoundError(err) {
			return nil, ErrTeamNotFound
		}
		return nil, fmt.Errorf("error getting team by name from db: %w", err)
	}
	return team, nil
}

// CreateTeam creates a new team in the database.
func CreateTeam(ctx context.Context, db *sqlite.DB, log *slog.Logger, name, description string) (*models.Team, error) {
	// Validate input parameters
	if err := validateTeamCreation(name, description); err != nil {
		return nil, err
	}

	// Check if team already exists by name
	_, err := db.GetTeamByName(ctx, name)
	if err == nil {
		return nil, fmt.Errorf("%w: team with name '%s'", ErrTeamAlreadyExists, name) // Use error from users.go
	}

	// Only proceed if it's a "not found" error, which is expected
	if !sqlite.IsNotFoundError(err) && !sqlite.IsTeamNotFoundError(err) {
		log.Error("error checking if team exists by name", "error", err, "name", name)
		return nil, fmt.Errorf("error checking if team exists: %w", err)
	}

	// Create new team model
	now := time.Now()
	team := &models.Team{
		Name:        name,
		Description: description,
		Timestamps: models.Timestamps{
			CreatedAt: now,
			UpdatedAt: now, // Set initial UpdatedAt
		},
	}

	// Save to database
	if err := db.CreateTeam(ctx, team); err != nil {
		log.Error("error creating team in db", "error", err, "name", name)
		return nil, fmt.Errorf("error creating team: %w", err)
	}

	log.Info("team created successfully", "team_id", team.ID, "name", team.Name)
	return team, nil
}

// UpdateTeam updates an existing team's mutable fields (name, description).
func UpdateTeam(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, updateData models.Team) error {
	// Validate the fields provided for update
	if err := validateTeamUpdate(updateData); err != nil {
		return err
	}

	// Get existing team
	existing, err := db.GetTeam(ctx, teamID)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsTeamNotFoundError(err) {
			return ErrTeamNotFound
		}
		log.Error("failed to get existing team for update", "error", err, "team_id", teamID)
		return fmt.Errorf("error getting team for update: %w", err)
	}

	updated := false
	// Apply updates from updateData
	if updateData.Name != "" && updateData.Name != existing.Name {
		// Check if new name conflicts with another existing team
		conflictingTeam, err := db.GetTeamByName(ctx, updateData.Name)
		if err == nil && conflictingTeam.ID != existing.ID {
			return fmt.Errorf("%w: team with name '%s' already exists", ErrTeamAlreadyExists, updateData.Name) // Use error from users.go
		}
		if err != nil && !sqlite.IsNotFoundError(err) && !sqlite.IsTeamNotFoundError(err) {
			log.Error("error checking conflicting team name during update", "error", err, "new_name", updateData.Name)
			return fmt.Errorf("error checking for conflicting name: %w", err)
		}
		existing.Name = updateData.Name
		updated = true
	}
	if updateData.Description != existing.Description { // Update description even if empty
		existing.Description = updateData.Description
		updated = true
	}

	// If no changes, return early
	if !updated {
		log.Debug("no update needed for team", "team_id", teamID)
		return nil
	}

	// Update timestamp is handled by the DB layer
	if err := db.UpdateTeam(ctx, existing); err != nil {
		log.Error("failed to update team in db", "error", err, "team_id", teamID)
		return fmt.Errorf("error updating team: %w", err)
	}

	log.Info("team updated successfully", "team_id", teamID)
	return nil
}

// DeleteTeam deletes a team and its associations (members, sources, queries).
func DeleteTeam(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID) error {
	// Validate team exists
	_, err := db.GetTeam(ctx, teamID)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsTeamNotFoundError(err) {
			return ErrTeamNotFound
		}
		log.Error("failed to get team for deletion check", "error", err, "team_id", teamID)
		return fmt.Errorf("error checking existing team: %w", err)
	}

	log.Warn("deleting team and all associated data", "team_id", teamID)

	// Delete team (DB constraints should handle cascading deletes for members, sources, queries)
	if err := db.DeleteTeam(ctx, teamID); err != nil {
		log.Error("failed to delete team from db", "error", err, "team_id", teamID)
		return fmt.Errorf("error deleting team: %w", err)
	}

	log.Info("team deleted successfully", "team_id", teamID)
	return nil
}

// --- Team Member Functions ---

// ListTeamMembers returns all members of a specific team.
func ListTeamMembers(ctx context.Context, db *sqlite.DB, teamID models.TeamID) ([]*models.TeamMember, error) {
	// Optional: Validate team exists first
	// _, err := GetTeam(ctx, db, teamID)
	// if err != nil { return nil, err }

	members, err := db.ListTeamMembersWithDetails(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error listing team members: %w", err)
	}
	return members, nil
}

// GetTeamMember retrieves a specific member's details within a team.
func GetTeamMember(ctx context.Context, db *sqlite.DB, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error) {
	member, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Standard practice: return nil, nil if not found, let caller decide if it's an error
			return nil, nil
		}
		return nil, fmt.Errorf("error getting team member from db: %w", err)
	}
	return member, nil
}

// AddTeamMember adds a user to a team with a specified role.
// If the user is already a member, it updates their role.
func AddTeamMember(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	// Validate parameters
	if err := validateTeamMember(teamID, userID, role); err != nil {
		return err
	}

	// Validate team exists
	if _, err := GetTeam(ctx, db, teamID); err != nil {
		return err // Propagate ErrTeamNotFound or other DB errors
	}

	// Validate user exists (call function from users.go)
	if _, err := GetUser(ctx, db, userID); err != nil {
		return err // Propagate ErrUserNotFound or other DB errors
	}

	// Check if user is already a member
	existingMember, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Error("failed to check existing team member", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error checking team member: %w", err)
	}

	if existingMember != nil {
		// User is already a member, update role if different
		if existingMember.Role != role {
			log.Info("updating team member role", "team_id", teamID, "user_id", userID, "old_role", existingMember.Role, "new_role", role)
			if err := db.UpdateTeamMemberRole(ctx, teamID, userID, role); err != nil {
				log.Error("failed to update team member role in db", "error", err, "team_id", teamID, "user_id", userID)
				return fmt.Errorf("error updating team member role: %w", err)
			}
		} else {
			log.Debug("user already team member with correct role", "team_id", teamID, "user_id", userID, "role", role)
		}
		return nil // Indicate success (or no-op)
	}

	// Add user to team
	log.Info("adding new team member", "team_id", teamID, "user_id", userID, "role", role)
	if err := db.AddTeamMember(ctx, teamID, userID, role); err != nil {
		log.Error("failed to add team member to db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error adding team member: %w", err)
	}

	return nil
}

// UpdateTeamMemberRole changes the role of an existing team member.
func UpdateTeamMemberRole(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, userID models.UserID, newRole models.TeamRole) error {
	// Validate Role
	if err := validateTeamMember(teamID, userID, newRole); err != nil {
		// Adjust error message slightly if needed
		return err
	}

	// Check if the member exists first
	existingMember, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("user %d is not a member of team %d", userID, teamID) // Specific error
		}
		log.Error("failed to get team member for role update check", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error checking team member: %w", err)
	}

	// If role is the same, do nothing
	if existingMember.Role == newRole {
		log.Debug("team member role already correct", "team_id", teamID, "user_id", userID, "role", newRole)
		return nil
	}

	// Update role in the database
	log.Info("updating team member role", "team_id", teamID, "user_id", userID, "old_role", existingMember.Role, "new_role", newRole)
	if err := db.UpdateTeamMemberRole(ctx, teamID, userID, newRole); err != nil {
		log.Error("failed to update team member role in db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error updating team member role: %w", err)
	}

	return nil
}

// RemoveTeamMember removes a user from a team.
func RemoveTeamMember(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, userID models.UserID) error {
	// Optional: Validate team and user exist first
	// _, err := GetTeam(ctx, db, teamID)
	// if err != nil { return err }
	// _, err = GetUser(ctx, db, userID) // Use function from users.go
	// if err != nil { return err }

	// Check if user is actually a member before attempting delete
	_, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Debug("user not a member of team, nothing to remove", "team_id", teamID, "user_id", userID)
			return nil // Not an error, just nothing to do
		}
		log.Error("failed to check team member before removal", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error checking team member before removal: %w", err)
	}

	// Remove user from team
	log.Info("removing team member", "team_id", teamID, "user_id", userID)
	if err := db.RemoveTeamMember(ctx, teamID, userID); err != nil {
		log.Error("failed to remove team member from db", "error", err, "team_id", teamID, "user_id", userID)
		return fmt.Errorf("error removing team member: %w", err)
	}

	return nil
}

// --- Team Source Functions ---

// ListTeamSources returns basic information for all sources associated with a specific team,
// including their connection status fetched from the ClickHouse manager's cache.
func ListTeamSources(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, teamID models.TeamID) ([]*models.Source, error) {
	// First, validate the team exists
	_, err := GetTeam(ctx, db, teamID) // Use existing GetTeam function
	if err != nil {
		if errors.Is(err, ErrTeamNotFound) {
			// Return specific error if team not found, allows handler to differentiate
			return nil, ErrTeamNotFound
		}
		// Return other DB errors
		return nil, fmt.Errorf("error checking team existence before listing sources: %w", err)
	}

	// Get the basic source info from SQLite
	sources, err := db.ListTeamSources(ctx, teamID)
	if err != nil {
		// If the error is sql.ErrNoRows (or equivalent), it means no sources are linked.
		// Return an empty slice and no error in this case.
		if errors.Is(err, sql.ErrNoRows) || sqlite.IsNotFoundError(err) { // Check for standard and custom not found
			return []*models.Source{}, nil
		}
		// Return other database errors
		return nil, fmt.Errorf("error listing team sources from db: %w", err)
	}

	// Iterate and populate connection status from the manager's cache
	for _, source := range sources {
		if source == nil { // Safety check
			continue
		}
		// Get the cached health status from the manager
		health := chDB.GetCachedHealth(source.ID)
		// Update the IsConnected field based on the cached status
		source.IsConnected = (health.Status == models.HealthStatusHealthy)

		// Optionally log if status is unhealthy for debugging
		if !source.IsConnected {
			log.Debug("retrieved unhealthy status from cache for source",
				"source_id", source.ID,
				"team_id", teamID,
				"cached_status", health.Status,
				"last_checked", health.LastChecked,
				"error", health.Error,
			)
		}
	}

	return sources, nil
}

// AddTeamSource associates a source with a team.
func AddTeamSource(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID) error {
	// Validate team exists
	if _, err := GetTeam(ctx, db, teamID); err != nil {
		return err // Propagate ErrTeamNotFound or other DB errors
	}

	// Validate source exists
	if _, err := db.GetSource(ctx, sourceID); err != nil { // Check DB directly for now
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrNotFound) {
			return ErrSourceNotFound // Use error from source.go
		}
		return fmt.Errorf("error getting source: %w", err)
	}

	// Add source to team (DB handles potential duplicates gracefully or via error)
	log.Info("adding source to team", "team_id", teamID, "source_id", sourceID)
	if err := db.AddTeamSource(ctx, teamID, sourceID); err != nil {
		log.Error("failed to add team source in db", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error adding team source: %w", err)
	}

	return nil
}

// RemoveTeamSource removes the association between a source and a team.
func RemoveTeamSource(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID) error {
	// Optional: Validate team and source exist first
	// _, err := GetTeam(ctx, db, teamID)
	// if err != nil { return err }
	// _, err = db.GetSource(ctx, sourceID) // Check DB directly
	// if err != nil { ... handle ErrSourceNotFound ... }

	// Remove source from team
	log.Info("removing source from team", "team_id", teamID, "source_id", sourceID)
	if err := db.RemoveTeamSource(ctx, teamID, sourceID); err != nil {
		log.Error("failed to remove team source from db", "error", err, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("error removing team source: %w", err)
	}

	return nil
}

// --- Authorization/Access Check Functions ---

// ListSourcesForUser returns all unique sources a user has access to across all teams.
func ListSourcesForUser(ctx context.Context, db *sqlite.DB, userID models.UserID) ([]*models.Source, error) {
	// Optional: Validate user exists first
	// _, err := GetUser(ctx, db, userID)
	// if err != nil { return nil, err }

	sources, err := db.ListSourcesForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error listing sources for user: %w", err)
	}
	return sources, nil
}

// ListSourceTeams returns all teams that have access to a specific source.
func ListSourceTeams(ctx context.Context, db *sqlite.DB, sourceID models.SourceID) ([]*models.Team, error) {
	// Optional: Validate source exists first
	// _, err := db.GetSource(ctx, sourceID)
	// if err != nil { ... handle ErrSourceNotFound ... }

	teams, err := db.ListSourceTeams(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error listing teams for source: %w", err)
	}
	return teams, nil
}

// IsTeamMember checks if a user is a member of a specific team.
func IsTeamMember(ctx context.Context, db *sqlite.DB, teamID models.TeamID, userID models.UserID) (bool, error) {
	member, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil // Not found is not an error for a check
		}
		return false, fmt.Errorf("error checking team membership: %w", err)
	}
	return member != nil, nil
}

// IsTeamAdmin checks if a user is an admin of a specific team.
func IsTeamAdmin(ctx context.Context, db *sqlite.DB, teamID models.TeamID, userID models.UserID) (bool, error) {
	member, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil // Not found is not an error for a check
		}
		return false, fmt.Errorf("error checking team admin status: %w", err)
	}
	return member != nil && member.Role == models.TeamRoleAdmin, nil
}

// TeamHasSourceAccess checks if a specific team has access to a specific source.
func TeamHasSourceAccess(ctx context.Context, db *sqlite.DB, teamID models.TeamID, sourceID models.SourceID) (bool, error) {
	hasAccess, err := db.TeamHasSource(ctx, teamID, sourceID)
	if err != nil {
		return false, fmt.Errorf("error checking team source access: %w", err)
	}
	return hasAccess, nil
}

// ListTeamsForUser returns all teams a user is a member of, including their role and team member count.
func ListTeamsForUser(ctx context.Context, db *sqlite.DB, userID models.UserID) ([]*models.UserTeamDetails, error) {
	// Optional: Validate user exists first
	// _, err := GetUser(ctx, db, userID) // Assuming GetUser exists and is appropriate here
	// if err != nil { return nil, err }

	// After sqlc generate, db.ListTeamsForUser returns the new sqlc-generated row struct.
	// Let's assume it's []sqlc.ListTeamsForUserRow (the actual name might vary based on sqlc config/version)
	teamRows, err := db.ListTeamsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error listing teams for user from db: %w", err)
	}

	userTeams := make([]*models.UserTeamDetails, 0, len(teamRows))
	for _, row := range teamRows {
		// The fields of 'row' are now directly from the SQL query via sqlc's generated struct.
		// e.g., row.ID, row.Name, row.Description, row.CreatedAt, row.UpdatedAt, row.Role, row.MemberCount
		desc := ""
		if row.Description.Valid { // Check if sql.NullString is valid
			desc = row.Description.String
		}

		userTeams = append(userTeams, &models.UserTeamDetails{
			ID:          models.TeamID(row.ID),
			Name:        row.Name,
			Description: desc,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
			MemberCount: int(row.MemberCount),      // MemberCount from SQL query
			Role:        models.TeamRole(row.Role), // Role from SQL query
		})
	}

	return userTeams, nil
}

// --- Utility Functions ---

// UserHasAccessToTeamSource checks if a user has access to a specific source through a specific team.
// This is the proper way to check access - requiring both team membership and team-source linkage.
func UserHasAccessToTeamSource(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID, teamID models.TeamID, sourceID models.SourceID) (bool, error) {
	// First check if user is a team member
	isMember, err := IsTeamMember(ctx, db, teamID, userID)
	if err != nil {
		return false, fmt.Errorf("error checking team membership: %w", err)
	}
	if !isMember {
		log.Debug("user is not a member of the team", "user_id", userID, "team_id", teamID)
		return false, nil
	}

	// Then check if team has access to source
	hasAccess, err := TeamHasSourceAccess(ctx, db, teamID, sourceID)
	if err != nil {
		return false, fmt.Errorf("error checking team source access: %w", err)
	}
	if !hasAccess {
		log.Debug("team does not have access to source", "team_id", teamID, "source_id", sourceID)
		return false, nil
	}

	return true, nil
}

// ListTeamsWithAccessToSource returns teams accessible by a user for a given source.
// This involves multiple DB calls and might be better placed closer to the handler.
func ListTeamsWithAccessToSource(ctx context.Context, db *sqlite.DB, log *slog.Logger, sourceID models.SourceID, userID models.UserID) ([]*models.Team, error) {
	// Get all teams for the source
	sourceTeams, err := ListSourceTeams(ctx, db, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error listing source teams: %w", err)
	}

	// Get all teams for the user
	userTeams, err := ListTeamsForUser(ctx, db, userID)
	if err != nil {
		return nil, fmt.Errorf("error listing user teams: %w", err)
	}

	// Find intersection of sourceTeams and userTeams
	userTeamMap := make(map[models.TeamID]bool)
	for _, userTeam := range userTeams {
		userTeamMap[userTeam.ID] = true
	}

	var accessibleTeams []*models.Team
	for _, sourceTeam := range sourceTeams {
		if userTeamMap[sourceTeam.ID] {
			accessibleTeams = append(accessibleTeams, sourceTeam)
		}
	}

	log.Debug("found accessible teams for user/source", "user_id", userID, "source_id", sourceID, "count", len(accessibleTeams))
	return accessibleTeams, nil
}

// ParseTeamID converts a string team ID to models.TeamID.
// Utility function often needed in handlers.
func ParseTeamID(teamIDStr string) (models.TeamID, error) {
	if teamIDStr == "" {
		return 0, fmt.Errorf("team ID cannot be empty")
	}
	id, err := strconv.ParseInt(teamIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid team ID format: %w", err)
	}
	return models.TeamID(id), nil
}

// ParseUserID converts a string user ID to models.UserID.
// Utility function often needed in handlers.
func ParseUserID(userIDStr string) (models.UserID, error) {
	if userIDStr == "" {
		return 0, fmt.Errorf("user ID cannot be empty")
	}
	id, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid user ID format: %w", err)
	}
	return models.UserID(id), nil
}

// ParseSourceID converts a string source ID to models.SourceID.
// Utility function often needed in handlers.
func ParseSourceID(sourceIDStr string) (models.SourceID, error) {
	if sourceIDStr == "" {
		return 0, fmt.Errorf("source ID cannot be empty")
	}
	id, err := strconv.ParseInt(sourceIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid source ID format: %w", err)
	}
	return models.SourceID(id), nil
}
