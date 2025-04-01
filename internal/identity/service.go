package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/sqlite"
)

// Error definitions
var (
	// ErrUserNotFound is returned when a user is not found
	ErrUserNotFound = errors.New("user not found")
	// ErrTeamNotFound is returned when a team is not found
	ErrTeamNotFound = errors.New("team not found")
)

// Service handles operations related to users and teams
type Service struct {
	db        *sqlite.DB
	log       *slog.Logger
	validator *Validator
}

// New creates a new identity service
func New(db *sqlite.DB, log *slog.Logger) *Service {
	return &Service{
		db:        db,
		log:       log.With("component", "identity_service"),
		validator: NewValidator(),
	}
}

// User Management

// ListUsers returns all users
func (s *Service) ListUsers(ctx context.Context) ([]*models.User, error) {
	return s.db.ListUsers(ctx)
}

// GetUser retrieves a user by ID
func (s *Service) GetUser(ctx context.Context, id models.UserID) (*models.User, error) {
	return s.db.GetUser(ctx, id)
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, email, fullName string, role models.UserRole) (*models.User, error) {
	// Check if user already exists
	existingUser, err := s.db.GetUserByEmail(ctx, email)
	if err != nil && err != sql.ErrNoRows {
		s.log.Error("error checking if user exists", "error", err)
		return nil, fmt.Errorf("error checking if user exists: %w", err)
	}

	if existingUser != nil {
		return nil, fmt.Errorf("user with email %s already exists", email)
	}

	// Create new user
	user := &models.User{
		Email:    email,
		FullName: fullName,
		Role:     role,
		Status:   models.UserStatusActive,
		Timestamps: models.Timestamps{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	// Save to database
	if err := s.db.CreateUser(ctx, user); err != nil {
		s.log.Error("error creating user", "error", err)
		return nil, fmt.Errorf("error creating user: %w", err)
	}

	return user, nil
}

// UpdateUser updates a user's information
func (s *Service) UpdateUser(ctx context.Context, user *models.User) error {
	// Validate user exists
	existing, err := s.db.GetUser(ctx, user.ID)
	if err != nil {
		s.log.Error("failed to check existing user", "error", err)
		return fmt.Errorf("error checking existing user: %w", err)
	}

	if existing == nil {
		return ErrUserNotFound
	}

	// Validate role if provided
	if user.Role != "" && user.Role != models.UserRoleAdmin && user.Role != models.UserRoleMember {
		return &ValidationError{
			Field:   "Role",
			Message: "role must be either 'admin' or 'member'",
		}
	}

	// Validate status if provided
	if user.Status != "" && user.Status != models.UserStatusActive && user.Status != models.UserStatusInactive {
		return &ValidationError{
			Field:   "Status",
			Message: "status must be either 'active' or 'inactive'",
		}
	}

	// If changing to inactive and user is admin, ensure there's at least one other active admin
	if user.Status == models.UserStatusInactive && existing.Role == models.UserRoleAdmin {
		count, err := s.db.CountAdminUsers(ctx)
		if err != nil {
			s.log.Error("failed to count admin users", "error", err)
			return fmt.Errorf("error counting admin users: %w", err)
		}

		if count <= 1 {
			return &ValidationError{
				Field:   "Status",
				Message: "cannot deactivate last admin user",
			}
		}
	}

	// Update timestamp
	user.UpdatedAt = time.Now()

	return s.db.UpdateUser(ctx, user)
}

// DeleteUser deletes a user
func (s *Service) DeleteUser(ctx context.Context, id models.UserID) error {
	// Validate user exists
	existing, err := s.db.GetUser(ctx, id)
	if err != nil {
		s.log.Error("failed to check existing user", "error", err)
		return fmt.Errorf("error checking existing user: %w", err)
	}

	if existing == nil {
		return ErrUserNotFound
	}

	// If user is admin, ensure there's at least one other active admin
	if existing.Role == models.UserRoleAdmin {
		count, err := s.db.CountAdminUsers(ctx)
		if err != nil {
			s.log.Error("failed to count admin users", "error", err)
			return fmt.Errorf("error counting admin users: %w", err)
		}

		if count <= 1 {
			return &ValidationError{
				Field:   "ID",
				Message: "cannot delete last admin user",
			}
		}
	}

	return s.db.DeleteUser(ctx, existing.ID)
}

// InitAdminUsers ensures that admin users exist in the system
func (s *Service) InitAdminUsers(ctx context.Context, adminEmails []string) error {
	if len(adminEmails) == 0 {
		s.log.Warn("no admin emails configured")
		return nil
	}

	for _, email := range adminEmails {
		// Check if user already exists
		existing, err := s.db.GetUserByEmail(ctx, email)
		if err != nil {
			// If the error contains "not found", treat it as a non-existing user, not an error
			if strings.Contains(err.Error(), "not found") {
				// User doesn't exist, will create below
				existing = nil
			} else {
				// This is a real error, like DB connection issue
				s.log.Error("failed to check existing admin user", "email", email, "error", err)
				return fmt.Errorf("error checking existing admin user: %w", err)
			}
		}

		if existing != nil {
			// Ensure user is an admin and active
			if existing.Role != models.UserRoleAdmin || existing.Status != models.UserStatusActive {
				existing.Role = models.UserRoleAdmin
				existing.Status = models.UserStatusActive
				existing.Timestamps.UpdatedAt = time.Now()

				if err := s.db.UpdateUser(ctx, existing); err != nil {
					s.log.Error("failed to update admin user", "email", email, "error", err)
					return fmt.Errorf("error updating admin user: %w", err)
				}
				s.log.Info("updated admin user", "email", email)
			}
			continue
		}

		// Create new admin user
		now := time.Now()
		user := &models.User{
			Email:    email,
			FullName: "Admin User",
			Role:     models.UserRoleAdmin,
			Status:   models.UserStatusActive,
			Timestamps: models.Timestamps{
				CreatedAt: now,
				UpdatedAt: now,
			},
		}

		if err := s.db.CreateUser(ctx, user); err != nil {
			s.log.Error("failed to create admin user", "email", email, "error", err)
			return fmt.Errorf("error creating admin user: %w", err)
		}
		s.log.Info("created admin user", "email", email)
	}

	return nil
}

// Team Management

// ListTeams returns all teams
func (s *Service) ListTeams(ctx context.Context) ([]*models.Team, error) {
	teams, err := s.db.ListTeams(ctx)
	if err != nil {
		s.log.Error("failed to list teams", "error", err)
		return nil, fmt.Errorf("error listing teams: %w", err)
	}
	return teams, nil
}

// GetTeam retrieves a team by ID
func (s *Service) GetTeam(ctx context.Context, id models.TeamID) (*models.Team, error) {
	team, err := s.db.GetTeam(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}
	return team, nil
}

// CreateTeam creates a new team
func (s *Service) CreateTeam(ctx context.Context, name, description string) (*models.Team, error) {
	// Check if team already exists
	existingTeam, err := s.db.GetTeamByName(ctx, name)
	if err != nil && err != sql.ErrNoRows {
		s.log.Error("error checking if team exists", "error", err)
		return nil, fmt.Errorf("error checking if team exists: %w", err)
	}

	if existingTeam != nil {
		return nil, fmt.Errorf("team with name %s already exists", name)
	}

	// Create new team
	team := &models.Team{
		Name:        name,
		Description: description,
		Timestamps: models.Timestamps{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	// Save to database
	if err := s.db.CreateTeam(ctx, team); err != nil {
		s.log.Error("error creating team", "error", err)
		return nil, fmt.Errorf("error creating team: %w", err)
	}

	return team, nil
}

// UpdateTeam updates a team
func (s *Service) UpdateTeam(ctx context.Context, team *models.Team) error {
	// Validate team exists
	existing, err := s.db.GetTeam(ctx, team.ID)
	if err != nil {
		return fmt.Errorf("error getting team: %w", err)
	}
	if existing == nil {
		return ErrTeamNotFound
	}

	// Validate team
	if err := s.validator.ValidateCreateTeam(team); err != nil {
		return err
	}

	// Update timestamp
	team.UpdatedAt = time.Now()

	// Update team
	if err := s.db.UpdateTeam(ctx, team); err != nil {
		s.log.Error("failed to update team", "error", err)
		return fmt.Errorf("error updating team: %w", err)
	}

	return nil
}

// DeleteTeam deletes a team
func (s *Service) DeleteTeam(ctx context.Context, teamID models.TeamID) error {
	// Validate team exists
	existing, err := s.db.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error getting team: %w", err)
	}
	if existing == nil {
		return ErrTeamNotFound
	}

	// Delete team
	if err := s.db.DeleteTeam(ctx, teamID); err != nil {
		s.log.Error("failed to delete team", "error", err)
		return fmt.Errorf("error deleting team: %w", err)
	}

	return nil
}

// Team Members

// ListTeamMembers returns all members of a team
func (s *Service) ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	// Validate team exists
	team, err := s.db.GetTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}

	// Get team members
	members, err := s.db.ListTeamMembers(ctx, teamID)
	if err != nil {
		s.log.Error("failed to list team members", "team_id", teamID, "error", err)
		return nil, fmt.Errorf("error listing team members: %w", err)
	}

	return members, nil
}

// ListTeamMembersWithDetails returns all members of a team with user details
func (s *Service) ListTeamMembersWithDetails(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	// Validate team exists
	team, err := s.db.GetTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}

	// Get team members with user details
	members, err := s.db.ListTeamMembersWithDetails(ctx, teamID)
	if err != nil {
		s.log.Error("failed to list team members with details", "team_id", teamID, "error", err)
		return nil, fmt.Errorf("error listing team members with details: %w", err)
	}

	return members, nil
}

// GetTeamMember gets a specific team member
func (s *Service) GetTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error) {
	// Validate team exists
	team, err := s.db.GetTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}

	// Get team member
	member, err := s.db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		s.log.Debug("user is not a member of the team", "team_id", teamID, "user_id", userID)
		return nil, nil // Return nil instead of error for "not found" case
	}

	return member, nil
}

// AddTeamMember adds a user to a team
func (s *Service) AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	// Validate parameters
	if err := s.validator.ValidateTeamMember(teamID, userID, role); err != nil {
		return err
	}

	// Validate team exists
	team, err := s.db.GetTeam(ctx, models.TeamID(teamID))
	if err != nil {
		return fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return ErrTeamNotFound
	}

	// Validate user exists
	user, err := s.db.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("error getting user: %w", err)
	}
	if user == nil {
		return ErrUserNotFound
	}

	// Check if user is already a member
	member, err := s.db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		s.log.Error("failed to check team member", "team_id", teamID, "user_id", userID, "error", err)
		return fmt.Errorf("error checking team member: %w", err)
	}

	if member != nil {
		// Update role if different
		if member.Role != role {
			if err := s.db.UpdateTeamMemberRole(ctx, teamID, userID, role); err != nil {
				s.log.Error("failed to update team member role", "team_id", teamID, "user_id", userID, "error", err)
				return fmt.Errorf("error updating team member role: %w", err)
			}
		}
		return nil
	}

	// Add user to team
	if err := s.db.AddTeamMember(ctx, teamID, userID, role); err != nil {
		s.log.Error("failed to add team member", "team_id", teamID, "user_id", userID, "error", err)
		return fmt.Errorf("error adding team member: %w", err)
	}

	return nil
}

// RemoveTeamMember removes a user from a team
func (s *Service) RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error {
	// Validate team exists
	team, err := s.db.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return ErrTeamNotFound
	}

	// Validate user exists
	user, err := s.db.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("error getting user: %w", err)
	}
	if user == nil {
		return ErrUserNotFound
	}

	// Check if user is a member
	member, err := s.db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		s.log.Error("failed to check team member", "team_id", teamID, "user_id", userID, "error", err)
		return fmt.Errorf("error checking team member: %w", err)
	}

	if member == nil {
		// User is not a member, nothing to do
		return nil
	}

	// Remove user from team
	if err := s.db.RemoveTeamMember(ctx, teamID, userID); err != nil {
		s.log.Error("failed to remove team member", "team_id", teamID, "user_id", userID, "error", err)
		return fmt.Errorf("error removing team member: %w", err)
	}

	return nil
}

// Team Sources

// ListTeamSources returns all sources associated with a team
func (s *Service) ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error) {
	// Validate team exists
	team, err := s.db.GetTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}

	// Get team sources
	sources, err := s.db.ListTeamSources(ctx, teamID)
	if err != nil {
		s.log.Error("failed to list team sources", "team_id", teamID, "error", err)
		return nil, fmt.Errorf("error listing team sources: %w", err)
	}

	return sources, nil
}

// AddTeamSource associates a source with a team
func (s *Service) AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	// Validate team exists
	team, err := s.db.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return ErrTeamNotFound
	}

	// Validate source exists
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return fmt.Errorf("source not found")
	}

	// Add source to team
	if err := s.db.AddTeamSource(ctx, teamID, sourceID); err != nil {
		s.log.Error("failed to add team source", "team_id", teamID, "source_id", sourceID, "error", err)
		return fmt.Errorf("error adding team source: %w", err)
	}

	return nil
}

// RemoveTeamSource removes a source association from a team
func (s *Service) RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	// Validate team exists
	team, err := s.db.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return ErrTeamNotFound
	}

	// Validate source exists
	source, err := s.db.GetSource(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return fmt.Errorf("source not found")
	}

	// Remove source from team
	if err := s.db.RemoveTeamSource(ctx, teamID, sourceID); err != nil {
		s.log.Error("failed to remove team source", "team_id", teamID, "source_id", sourceID, "error", err)
		return fmt.Errorf("error removing team source: %w", err)
	}

	return nil
}

// ListSourcesForUser returns all unique sources a user has access to across all teams
func (s *Service) ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error) {
	// Validate user exists
	user, err := s.db.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error getting user: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	// Get sources accessible to the user
	sources, err := s.db.ListSourcesForUser(ctx, userID)
	if err != nil {
		s.log.Error("failed to list sources for user", "user_id", userID, "error", err)
		return nil, fmt.Errorf("error listing sources for user: %w", err)
	}

	return sources, nil
}

// ListSourceTeams returns all teams that have access to a source
func (s *Service) ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error) {
	// Validate source exists (we're using source service for this normally, but to avoid circular deps)
	// Just validate through the source access attempt

	// Get teams that have access to the source
	teams, err := s.db.ListSourceTeams(ctx, sourceID)
	if err != nil {
		s.log.Error("failed to list teams for source", "source_id", sourceID, "error", err)
		return nil, fmt.Errorf("error listing teams for source: %w", err)
	}

	return teams, nil
}

// IsTeamMember checks if a user is a member of a team
func (s *Service) IsTeamMember(ctx context.Context, teamIDStr string, userID models.UserID) (bool, error) {
	// Convert teamID to integer
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return false, fmt.Errorf("invalid team ID: %w", err)
	}
	teamID := models.TeamID(teamIDInt)

	// Check if user is a member of this team
	member, err := s.db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		// Don't log as error since this is often just checking membership
		s.log.Debug("failed to get team member", "team_id", teamID, "user_id", userID, "error", err)
		return false, nil
	}

	return member != nil, nil
}

// IsTeamAdmin checks if a user is an admin of a team
func (s *Service) IsTeamAdmin(ctx context.Context, teamIDStr string, userID models.UserID) (bool, error) {
	// Convert teamID to integer
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return false, fmt.Errorf("invalid team ID: %w", err)
	}
	teamID := models.TeamID(teamIDInt)

	// Check if user is an admin of this team
	member, err := s.db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		// Don't log as error since this is often just checking membership
		s.log.Debug("failed to get team member", "team_id", teamID, "user_id", userID, "error", err)
		return false, nil
	}

	return member != nil && member.Role == models.TeamRoleAdmin, nil
}

// TeamHasSourceAccess checks if a team has access to a source
func (s *Service) TeamHasSourceAccess(ctx context.Context, teamIDStr string, sourceIDStr string) (bool, error) {
	// Convert IDs to integers
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return false, fmt.Errorf("invalid team ID: %w", err)
	}
	teamID := models.TeamID(teamIDInt)

	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return false, fmt.Errorf("invalid source ID: %w", err)
	}
	sourceID := models.SourceID(sourceIDInt)

	// Check if team has source
	return s.db.TeamHasSource(ctx, teamID, sourceID)
}

// UserHasSourceAccess checks if a user has access to a source via any team
func (s *Service) UserHasSourceAccess(ctx context.Context, userID models.UserID, sourceIDStr string) (bool, error) {
	// Convert sourceID to integer
	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return false, fmt.Errorf("invalid source ID: %w", err)
	}
	sourceID := models.SourceID(sourceIDInt)

	// Check if user has source access through any team
	hasAccess, err := s.db.UserHasSourceAccess(ctx, userID, sourceID)
	if err != nil {
		s.log.Error("failed to check user source access", "user_id", userID, "source_id", sourceID, "error", err)
		return false, err
	}

	return hasAccess, nil
}

// ListTeamsForUser returns all teams a user is a member of
func (s *Service) ListTeamsForUser(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	// Validate user exists
	user, err := s.db.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error getting user: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	// Get teams for user
	teams, err := s.db.ListTeamsForUser(ctx, userID)
	if err != nil {
		s.log.Error("failed to list teams for user", "user_id", userID, "error", err)
		return nil, fmt.Errorf("error listing teams for user: %w", err)
	}

	return teams, nil
}

// ListTeamsWithAccessToSource returns all teams that have access to a source for a specific user
func (s *Service) ListTeamsWithAccessToSource(ctx context.Context, sourceIDStr string, userID models.UserID) ([]*models.Team, error) {
	// Convert sourceID to integer
	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid source ID: %w", err)
	}
	sourceID := models.SourceID(sourceIDInt)

	// Get all teams for the source
	sourceTeams, err := s.db.ListSourceTeams(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	// Get all teams for the user
	userTeams, err := s.db.ListTeamsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Find intersection of sourceTeams and userTeams
	var accessibleTeams []*models.Team
	for _, sourceTeam := range sourceTeams {
		for _, userTeam := range userTeams {
			if sourceTeam.ID == userTeam.ID {
				accessibleTeams = append(accessibleTeams, sourceTeam)
				break
			}
		}
	}

	return accessibleTeams, nil
}
