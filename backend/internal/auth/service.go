package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"backend-v2/internal/config"
	"backend-v2/pkg/errors"
	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"
)

// Service defines the interface for authentication and authorization operations
type Service interface {
	// OIDC operations
	GetAuthURL(state string) string
	HandleCallback(ctx context.Context, code, state string) (*models.User, *models.Session, error)

	// Session operations
	ValidateSession(ctx context.Context, sessionID string) (*models.Session, error)
	RevokeSession(ctx context.Context, sessionID string) error

	// User operations
	GetUser(ctx context.Context, userID string) (*models.User, error)
	UpdateUser(ctx context.Context, user *models.User) error
	ListUsers(ctx context.Context) ([]*models.User, error)

	// Team operations
	CreateTeam(ctx context.Context, team *models.Team) error
	GetTeam(ctx context.Context, teamID string) (*models.Team, error)
	UpdateTeam(ctx context.Context, team *models.Team) error
	DeleteTeam(ctx context.Context, teamID string) error
	ListTeams(ctx context.Context) ([]*models.Team, error)

	// Team member operations
	AddTeamMember(ctx context.Context, teamID, userID, role string) error
	RemoveTeamMember(ctx context.Context, teamID, userID string) error
	ListTeamMembers(ctx context.Context, teamID string) ([]*models.TeamMember, error)

	// Space operations
	CreateSpace(ctx context.Context, space *models.Space) error
	GetSpace(ctx context.Context, spaceID string) (*models.Space, error)
	UpdateSpace(ctx context.Context, space *models.Space) error
	DeleteSpace(ctx context.Context, spaceID string) error
	ListSpaces(ctx context.Context) ([]*models.Space, error)

	// Space access operations
	GrantTeamAccess(ctx context.Context, spaceID, teamID, permission string) error
	RevokeTeamAccess(ctx context.Context, spaceID, teamID string) error
	ListSpaceAccess(ctx context.Context, spaceID string) ([]*models.SpaceTeamAccess, error)

	// Permission checking
	CanAccessSpace(ctx context.Context, userID, spaceID, requiredPermission string) (bool, error)
	GetUserSpaces(ctx context.Context, userID string) ([]*models.Space, error)
}

// service implements the Service interface
type service struct {
	cfg   *config.Config
	store Store
	oidc  *OIDCProvider
	log   *slog.Logger
}

// NewService creates a new authentication service
func NewService(cfg *config.Config, store Store) (Service, error) {
	log := logger.Default().With("component", "auth")

	oidc, err := NewOIDCProvider(cfg, store)
	if err != nil {
		log.Error("failed to create OIDC provider",
			"error", err,
			"provider_url", cfg.OIDC.ProviderURL,
		)
		return nil, err
	}

	return &service{
		cfg:   cfg,
		store: store,
		oidc:  oidc,
		log:   log,
	}, nil
}

// GetAuthURL returns the URL to redirect the user to for authentication
func (s *service) GetAuthURL(state string) string {
	return s.oidc.GetAuthURL(state)
}

// HandleCallback processes the OIDC callback and creates or updates the user
func (s *service) HandleCallback(ctx context.Context, code, state string) (*models.User, *models.Session, error) {
	return s.oidc.HandleCallback(ctx, code, state)
}

// ValidateSession checks if a session is valid and not expired
func (s *service) ValidateSession(ctx context.Context, sessionID string) (*models.Session, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		s.log.Warn("session not found",
			"error", err,
			"session_id", sessionID,
		)
		return nil, errors.NewAuthError(errors.ErrSessionNotFound, "Session not found", map[string]interface{}{
			"session_id": sessionID,
		})
	}

	if time.Now().After(session.ExpiresAt) {
		s.log.Info("session expired",
			"session_id", sessionID,
			"expires_at", session.ExpiresAt,
		)
		// Delete expired session
		_ = s.store.DeleteSession(ctx, sessionID)
		return nil, errors.NewAuthError(errors.ErrSessionExpired, "Session has expired", map[string]interface{}{
			"session_id": sessionID,
		})
	}

	return session, nil
}

// RevokeSession deletes a session
func (s *service) RevokeSession(ctx context.Context, sessionID string) error {
	return s.store.DeleteSession(ctx, sessionID)
}

// GetUser retrieves a user by ID
func (s *service) GetUser(ctx context.Context, userID string) (*models.User, error) {
	return s.store.GetUser(ctx, userID)
}

// UpdateUser updates a user's information
func (s *service) UpdateUser(ctx context.Context, user *models.User) error {
	return s.store.UpdateUser(ctx, user)
}

// ListUsers returns all users
func (s *service) ListUsers(ctx context.Context) ([]*models.User, error) {
	return s.store.ListUsers(ctx)
}

// CanAccessSpace checks if a user has the required permission for a space
func (s *service) CanAccessSpace(ctx context.Context, userID, spaceID, requiredPermission string) (bool, error) {
	// Get user's teams
	teams, err := s.store.GetUserTeams(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("failed to get user teams: %w", err)
	}

	// Check each team's access to the space
	for _, team := range teams {
		access, err := s.store.GetTeamSpaceAccess(ctx, team.ID, spaceID)
		if err != nil {
			continue // Skip if team doesn't have access
		}

		// Check if team's permission is sufficient
		switch requiredPermission {
		case "read":
			// Any permission level is sufficient for read
			return true, nil
		case "write":
			// Write or admin permission is sufficient
			if access.Permission == "write" || access.Permission == "admin" {
				return true, nil
			}
		case "admin":
			// Only admin permission is sufficient
			if access.Permission == "admin" {
				return true, nil
			}
		}
	}

	return false, nil
}

// GetUserSpaces returns all spaces a user has access to
func (s *service) GetUserSpaces(ctx context.Context, userID string) ([]*models.Space, error) {
	// Get user's teams
	teams, err := s.store.GetUserTeams(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user teams: %w", err)
	}

	// Get all spaces and filter by team access
	spaces := make(map[string]*models.Space)
	for _, team := range teams {
		accesses, err := s.store.ListSpaceAccess(ctx, team.ID)
		if err != nil {
			continue
		}

		for _, access := range accesses {
			space, err := s.store.GetSpace(ctx, access.SpaceID)
			if err != nil {
				continue
			}
			spaces[space.ID] = space
		}
	}

	// Convert map to slice
	result := make([]*models.Space, 0, len(spaces))
	for _, space := range spaces {
		result = append(result, space)
	}

	return result, nil
}

// CreateTeam creates a new team
func (s *service) CreateTeam(ctx context.Context, team *models.Team) error {
	return s.store.CreateTeam(ctx, team)
}

// GetTeam retrieves a team by ID
func (s *service) GetTeam(ctx context.Context, teamID string) (*models.Team, error) {
	return s.store.GetTeam(ctx, teamID)
}

// UpdateTeam updates a team's information
func (s *service) UpdateTeam(ctx context.Context, team *models.Team) error {
	return s.store.UpdateTeam(ctx, team)
}

// DeleteTeam deletes a team
func (s *service) DeleteTeam(ctx context.Context, teamID string) error {
	return s.store.DeleteTeam(ctx, teamID)
}

// ListTeams returns all teams
func (s *service) ListTeams(ctx context.Context) ([]*models.Team, error) {
	return s.store.ListTeams(ctx)
}

// AddTeamMember adds a user to a team with the specified role
func (s *service) AddTeamMember(ctx context.Context, teamID, userID, role string) error {
	member := &models.TeamMember{
		TeamID: teamID,
		UserID: userID,
		Role:   role,
	}
	return s.store.AddTeamMember(ctx, member)
}

// RemoveTeamMember removes a user from a team
func (s *service) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
	return s.store.RemoveTeamMember(ctx, teamID, userID)
}

// ListTeamMembers returns all members of a team
func (s *service) ListTeamMembers(ctx context.Context, teamID string) ([]*models.TeamMember, error) {
	return s.store.ListTeamMembers(ctx, teamID)
}

// CreateSpace creates a new space
func (s *service) CreateSpace(ctx context.Context, space *models.Space) error {
	return s.store.CreateSpace(ctx, space)
}

// GetSpace retrieves a space by ID
func (s *service) GetSpace(ctx context.Context, spaceID string) (*models.Space, error) {
	return s.store.GetSpace(ctx, spaceID)
}

// UpdateSpace updates a space's information
func (s *service) UpdateSpace(ctx context.Context, space *models.Space) error {
	return s.store.UpdateSpace(ctx, space)
}

// DeleteSpace deletes a space
func (s *service) DeleteSpace(ctx context.Context, spaceID string) error {
	return s.store.DeleteSpace(ctx, spaceID)
}

// ListSpaces returns all spaces
func (s *service) ListSpaces(ctx context.Context) ([]*models.Space, error) {
	return s.store.ListSpaces(ctx)
}

// GrantTeamAccess grants a team access to a space with the specified permission
func (s *service) GrantTeamAccess(ctx context.Context, spaceID, teamID, permission string) error {
	access := &models.SpaceTeamAccess{
		SpaceID:    spaceID,
		TeamID:     teamID,
		Permission: permission,
	}
	return s.store.GrantTeamAccess(ctx, access)
}

// RevokeTeamAccess revokes a team's access to a space
func (s *service) RevokeTeamAccess(ctx context.Context, spaceID, teamID string) error {
	return s.store.RevokeTeamAccess(ctx, spaceID, teamID)
}

// ListSpaceAccess returns all team access records for a space
func (s *service) ListSpaceAccess(ctx context.Context, spaceID string) ([]*models.SpaceTeamAccess, error) {
	return s.store.ListSpaceAccess(ctx, spaceID)
}

// More method implementations will follow...
