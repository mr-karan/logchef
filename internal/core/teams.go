package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
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
func ListTeams(ctx context.Context, db store.StoreOps) ([]*models.Team, error) {
	teams, err := db.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing teams: %w", err)
	}
	return teams, nil
}

// GetTeam retrieves a specific team by its ID.
func GetTeam(ctx context.Context, db store.StoreOps, id models.TeamID) (*models.Team, error) {
	team, err := db.GetTeam(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrTeamNotFound
		}
		return nil, fmt.Errorf("error getting team from db: %w", err)
	}
	return team, nil
}

// CreateTeam creates a new team in the database.
func CreateTeam(ctx context.Context, db store.StoreOps, log *slog.Logger, name, description string) (*models.Team, error) {
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
	if !models.IsNotFound(err) {
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

// UpdateTeam updates an existing team's mutable fields (name, description)
// and returns the team before and after the update, both read under the row
// lock of the write.
func UpdateTeam(ctx context.Context, db store.Store, log *slog.Logger, teamID models.TeamID, updateData models.Team) (before, after *models.Team, err error) {
	if err := validateTeamUpdate(updateData); err != nil {
		return nil, nil, err
	}

	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		existing, err := tx.GetTeamForUpdate(ctx, teamID)
		if err != nil {
			if models.IsNotFound(err) {
				return ErrTeamNotFound
			}
			return fmt.Errorf("error getting team for update: %w", err)
		}
		prior := *existing
		before, after = &prior, existing

		updated := false
		if updateData.Name != "" && updateData.Name != existing.Name {
			conflictingTeam, err := tx.GetTeamByName(ctx, updateData.Name)
			if err == nil && conflictingTeam.ID != existing.ID {
				return fmt.Errorf("%w: team with name '%s' already exists", ErrTeamAlreadyExists, updateData.Name)
			}
			if err != nil && !models.IsNotFound(err) {
				return fmt.Errorf("error checking for conflicting name: %w", err)
			}
			existing.Name = updateData.Name
			updated = true
		}
		if updateData.Description != existing.Description { // Update description even if empty
			existing.Description = updateData.Description
			updated = true
		}
		if !updated {
			return nil
		}
		if err := tx.UpdateTeam(ctx, existing); err != nil {
			return fmt.Errorf("error updating team: %w", err)
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrTeamNotFound) && !errors.Is(err, ErrTeamAlreadyExists) {
			log.Error("failed to update team", "error", err, "team_id", teamID)
		}
		return nil, nil, err
	}

	log.Info("team updated successfully", "team_id", teamID)
	return before, after, nil
}

// DeleteTeam deletes a team and its associations (members, sources, queries),
// and returns the team as read under the row lock of the delete.
func DeleteTeam(ctx context.Context, db store.Store, log *slog.Logger, teamID models.TeamID) (*models.Team, error) {
	var deleted *models.Team
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.LockMembershipWrites(ctx); err != nil {
			return err
		}
		existing, err := tx.GetTeamForUpdate(ctx, teamID)
		if err != nil {
			if models.IsNotFound(err) {
				return ErrTeamNotFound
			}
			log.Error("failed to get team for deletion check", "error", err, "team_id", teamID)
			return fmt.Errorf("error checking existing team: %w", err)
		}

		log.Warn("deleting team and all associated data", "team_id", teamID)

		if err := tx.DeleteTeam(ctx, teamID); err != nil {
			log.Error("failed to delete team from db", "error", err, "team_id", teamID)
			return fmt.Errorf("error deleting team: %w", err)
		}
		deleted = existing
		return nil
	})
	if err != nil {
		return nil, err
	}

	log.Info("team deleted successfully", "team_id", teamID)
	return deleted, nil
}

// --- Team Member Functions ---

// ListTeamMembers returns all members of a specific team.
func ListTeamMembers(ctx context.Context, db store.StoreOps, teamID models.TeamID) ([]*models.TeamMember, error) {
	// Optional: Validate team exists first
	// _, err := GetTeam(ctx, db, teamID)
	// if err != nil { return nil, err }

	members, err := db.ListTeamMembersWithDetails(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error listing team members: %w", err)
	}
	return members, nil
}

// AddTeamMember adds a user to a team with a specified role. If the user is
// already a member, it updates their role. It returns the membership before
// (nil for a new member) and after the write, both read under the row lock of
// the write. It takes no LockMembershipWrites: provisioning copes with
// concurrent membership writes and must not wait for them.
func AddTeamMember(ctx context.Context, db store.Store, log *slog.Logger, teamID models.TeamID, userID models.UserID, role models.TeamRole) (before, after *models.TeamMember, err error) {
	if err := validateTeamMember(teamID, userID, role); err != nil {
		return nil, nil, err
	}

	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		if _, err := GetTeam(ctx, tx, teamID); err != nil {
			return err
		}
		if _, err := GetUser(ctx, tx, userID); err != nil {
			return err
		}

		existing, err := tx.GetTeamMemberForUpdate(ctx, teamID, userID)
		if err != nil {
			return fmt.Errorf("error checking team member: %w", err)
		}
		if existing == nil {
			log.Info("adding new team member", "team_id", teamID, "user_id", userID, "role", role)
			inserted, err := tx.InsertTeamMember(ctx, teamID, userID, role)
			if err != nil {
				return fmt.Errorf("error adding team member: %w", err)
			}
			if !inserted {
				// A concurrent add won the insert: its committed row is the
				// state this call changes, if it changes anything.
				if existing, err = tx.GetTeamMemberForUpdate(ctx, teamID, userID); err != nil {
					return fmt.Errorf("error reading concurrently added team member: %w", err)
				}
				if existing == nil {
					return fmt.Errorf("team member %d of team %d was added and removed concurrently", userID, teamID)
				}
			}
		}
		switch {
		case existing == nil: // this call inserted the member
		case existing.Role != role:
			log.Info("updating team member role", "team_id", teamID, "user_id", userID, "old_role", existing.Role, "new_role", role)
			if err := tx.UpdateTeamMemberRole(ctx, teamID, userID, role); err != nil {
				return fmt.Errorf("error updating team member role: %w", err)
			}
		default:
			log.Debug("user already team member with correct role", "team_id", teamID, "user_id", userID, "role", role)
		}
		before = existing
		after, err = tx.GetTeamMember(ctx, teamID, userID)
		return err
	})
	if err != nil {
		if !errors.Is(err, ErrTeamNotFound) && !errors.Is(err, ErrUserNotFound) {
			log.Error("failed to add team member", "error", err, "team_id", teamID, "user_id", userID)
		}
		return nil, nil, err
	}
	return before, after, nil
}

// RemoveTeamMember removes a user from a team and returns the membership it
// removed, read under the row lock of the delete. Removing a non-member is a
// no-op that returns a nil membership.
func RemoveTeamMember(ctx context.Context, db store.Store, log *slog.Logger, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error) {
	var removed *models.TeamMember
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		existing, err := tx.GetTeamMemberForUpdate(ctx, teamID, userID)
		if err != nil {
			return fmt.Errorf("error checking team member before removal: %w", err)
		}
		if existing == nil {
			log.Debug("user not a member of team, nothing to remove", "team_id", teamID, "user_id", userID)
			return nil
		}
		log.Info("removing team member", "team_id", teamID, "user_id", userID)
		if err := tx.RemoveTeamMember(ctx, teamID, userID); err != nil {
			return fmt.Errorf("error removing team member: %w", err)
		}
		removed = existing
		return nil
	})
	if err != nil {
		log.Error("failed to remove team member", "error", err, "team_id", teamID, "user_id", userID)
		return nil, err
	}
	return removed, nil
}

// --- Team Source Functions ---

// ListTeamSources returns basic information for all sources associated with a specific team,
// including provider-derived feature metadata and cached connection status.
func ListTeamSources(ctx context.Context, db store.StoreOps, ds *datasource.Service, log *slog.Logger, teamID models.TeamID) ([]*models.Source, error) {
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
		if models.IsNotFound(err) { // Check for standard and custom not found
			return []*models.Source{}, nil
		}
		// Return other database errors
		return nil, fmt.Errorf("error listing team sources from db: %w", err)
	}

	if err := populateSourceStatuses(ds, sources); err != nil {
		return nil, err
	}
	for _, source := range sources {
		if source != nil && !source.IsConnected {
			log.Debug("source reported unhealthy during team source listing",
				"source_id", source.ID,
				"team_id", teamID,
			)
		}
	}

	return sources, nil
}

// AddTeamSource associates a source with a team. It reports whether the link
// existed before, read under LockMembershipWrites; linking an already linked
// source is a no-op.
func AddTeamSource(ctx context.Context, db store.Store, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID) (existed bool, err error) {
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.LockMembershipWrites(ctx); err != nil {
			return err
		}
		if _, err := GetTeam(ctx, tx, teamID); err != nil {
			return err
		}
		if _, err := tx.GetSource(ctx, sourceID); err != nil {
			if models.IsNotFound(err) {
				return ErrSourceNotFound
			}
			return fmt.Errorf("error getting source: %w", err)
		}
		if existed, err = tx.TeamHasSource(ctx, teamID, sourceID); err != nil {
			return fmt.Errorf("error checking team source: %w", err)
		}

		log.Info("adding source to team", "team_id", teamID, "source_id", sourceID)
		if err := tx.AddTeamSource(ctx, teamID, sourceID); err != nil {
			log.Error("failed to add team source in db", "error", err, "team_id", teamID, "source_id", sourceID)
			return fmt.Errorf("error adding team source: %w", err)
		}
		return nil
	})
	return existed, err
}

// RemoveTeamSource removes the association between a source and a team, and
// reports whether a link existed, read under LockMembershipWrites. Unlinking
// a source that is not linked is a no-op.
func RemoveTeamSource(ctx context.Context, db store.Store, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID) (removed bool, err error) {
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.LockMembershipWrites(ctx); err != nil {
			return err
		}
		if removed, err = tx.TeamHasSource(ctx, teamID, sourceID); err != nil {
			return fmt.Errorf("error checking team source: %w", err)
		}
		if !removed {
			return nil
		}
		log.Info("removing source from team", "team_id", teamID, "source_id", sourceID)
		if err := tx.RemoveTeamSource(ctx, teamID, sourceID); err != nil {
			log.Error("failed to remove team source from db", "error", err, "team_id", teamID, "source_id", sourceID)
			return fmt.Errorf("error removing team source: %w", err)
		}
		return nil
	})
	return removed, err
}

// --- Authorization/Access Check Functions ---

// ListSourceTeams returns all teams that have access to a specific source.
func ListSourceTeams(ctx context.Context, db store.StoreOps, sourceID models.SourceID) ([]*models.Team, error) {
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
func IsTeamMember(ctx context.Context, db store.StoreOps, teamID models.TeamID, userID models.UserID) (bool, error) {
	member, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrNotFound) {
			return false, nil // Not found is not an error for a check
		}
		return false, fmt.Errorf("error checking team membership: %w", err)
	}
	return member != nil, nil
}

// IsTeamAdmin checks if a user is an admin of a specific team.
func IsTeamAdmin(ctx context.Context, db store.StoreOps, teamID models.TeamID, userID models.UserID) (bool, error) {
	member, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrNotFound) {
			return false, nil // Not found is not an error for a check
		}
		return false, fmt.Errorf("error checking team admin status: %w", err)
	}
	return member != nil && member.Role == models.TeamRoleAdmin, nil
}

// IsAnyTeamAdmin checks if a user is an admin of ANY team.
// This is used to determine if a user should have access to team management features.
func IsAnyTeamAdmin(ctx context.Context, db store.StoreOps, userID models.UserID) (bool, error) {
	teams, err := db.ListTeamsForUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("error listing teams for user: %w", err)
	}
	for _, team := range teams {
		if team.Role == models.TeamRoleAdmin {
			return true, nil
		}
	}
	return false, nil
}

// TeamHasSourceAccess checks if a specific team has access to a specific source.
func TeamHasSourceAccess(ctx context.Context, db store.StoreOps, teamID models.TeamID, sourceID models.SourceID) (bool, error) {
	hasAccess, err := db.TeamHasSource(ctx, teamID, sourceID)
	if err != nil {
		return false, fmt.Errorf("error checking team source access: %w", err)
	}
	return hasAccess, nil
}

// ListTeamsForUser returns all teams a user is a member of, including their role and team member count.
func ListTeamsForUser(ctx context.Context, db store.StoreOps, userID models.UserID) ([]*models.UserTeamDetails, error) {
	// The adapter maps to models.UserTeamDetails (role + member count); this
	// wrapper exists so callers depend on core, not the store directly.
	return db.ListTeamsForUser(ctx, userID)
}

// --- Utility Functions ---

// UserHasAccessToTeamSource checks if a user has access to a specific source through a specific team.
// This is the proper way to check access - requiring both team membership and team-source linkage.
func UserHasAccessToTeamSource(ctx context.Context, db store.StoreOps, log *slog.Logger, userID models.UserID, teamID models.TeamID, sourceID models.SourceID) (bool, error) {
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
func ListTeamsWithAccessToSource(ctx context.Context, db store.StoreOps, log *slog.Logger, sourceID models.SourceID, userID models.UserID) ([]*models.Team, error) {
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
