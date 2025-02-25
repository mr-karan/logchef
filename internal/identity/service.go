package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"logchef/internal/sqlite"
	"logchef/pkg/models"

	"github.com/google/uuid"
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
func (s *Service) GetUser(ctx context.Context, id string) (*models.User, error) {
	return s.db.GetUser(ctx, id)
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, user *models.User) error {
	// Validate user role
	if user.Role != models.UserRoleAdmin && user.Role != models.UserRoleMember {
		return &ValidationError{
			Field:   "Role",
			Message: "role must be either 'admin' or 'member'",
		}
	}

	// Validate user status
	if user.Status != models.UserStatusActive && user.Status != models.UserStatusInactive {
		return &ValidationError{
			Field:   "Status",
			Message: "status must be either 'active' or 'inactive'",
		}
	}

	// Check if user with email already exists
	existing, err := s.db.GetUserByEmail(ctx, user.Email)
	if err != nil {
		s.log.Error("failed to check existing user", "error", err)
		return fmt.Errorf("error checking existing user: %w", err)
	}

	if existing != nil {
		return &ValidationError{
			Field:   "Email",
			Message: "a user with this email already exists",
		}
	}

	// Set ID if not provided
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	return s.db.CreateUser(ctx, user)
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
func (s *Service) DeleteUser(ctx context.Context, id string) error {
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

	return s.db.DeleteUser(ctx, id)
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
			s.log.Error("failed to check existing admin user", "email", email, "error", err)
			return fmt.Errorf("error checking existing admin user: %w", err)
		}

		if existing != nil {
			// Ensure user is an admin and active
			if existing.Role != models.UserRoleAdmin || existing.Status != models.UserStatusActive {
				existing.Role = models.UserRoleAdmin
				existing.Status = models.UserStatusActive
				existing.UpdatedAt = time.Now()

				if err := s.db.UpdateUser(ctx, existing); err != nil {
					s.log.Error("failed to update admin user", "email", email, "error", err)
					return fmt.Errorf("error updating admin user: %w", err)
				}
				s.log.Info("updated admin user", "email", email)
			}
			continue
		}

		// Create new admin user
		user := &models.User{
			ID:        uuid.New().String(),
			Email:     email,
			FullName:  "Admin User",
			Role:      models.UserRoleAdmin,
			Status:    models.UserStatusActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
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
func (s *Service) GetTeam(ctx context.Context, id string) (*models.Team, error) {
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
func (s *Service) CreateTeam(ctx context.Context, team *models.Team) error {
	// Validate team
	if err := s.validator.ValidateCreateTeam(team); err != nil {
		return err
	}

	// Set ID if not provided
	if team.ID == "" {
		team.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	team.CreatedAt = now
	team.UpdatedAt = now

	// Create team
	if err := s.db.CreateTeam(ctx, team); err != nil {
		s.log.Error("failed to create team", "error", err)
		return fmt.Errorf("error creating team: %w", err)
	}

	return nil
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
func (s *Service) DeleteTeam(ctx context.Context, id string) error {
	// Validate team exists
	existing, err := s.db.GetTeam(ctx, id)
	if err != nil {
		return fmt.Errorf("error getting team: %w", err)
	}
	if existing == nil {
		return ErrTeamNotFound
	}

	// Delete team
	if err := s.db.DeleteTeam(ctx, id); err != nil {
		s.log.Error("failed to delete team", "error", err)
		return fmt.Errorf("error deleting team: %w", err)
	}

	return nil
}

// Team Members

// ListTeamMembers returns all members of a team
func (s *Service) ListTeamMembers(ctx context.Context, teamID string) ([]*models.TeamMember, error) {
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

// AddTeamMember adds a user to a team
func (s *Service) AddTeamMember(ctx context.Context, teamID, userID, role string) error {
	// Validate parameters
	if err := s.validator.ValidateTeamMember(teamID, userID, role); err != nil {
		return err
	}

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
func (s *Service) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
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
func (s *Service) ListTeamSources(ctx context.Context, teamID string) ([]*models.Source, error) {
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
func (s *Service) AddTeamSource(ctx context.Context, teamID, sourceID string) error {
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
func (s *Service) RemoveTeamSource(ctx context.Context, teamID, sourceID string) error {
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
