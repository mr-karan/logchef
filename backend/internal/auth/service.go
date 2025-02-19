package auth

import (
	"context"
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
	ListUserTeams(ctx context.Context, userID string) ([]*models.Team, error)

	// Team source operations
	AddTeamSource(ctx context.Context, teamID, sourceID string) error
	RemoveTeamSource(ctx context.Context, teamID, sourceID string) error
	ListTeamSources(ctx context.Context, teamID string) ([]*models.Source, error)
	ListSourceTeams(ctx context.Context, sourceID string) ([]*models.Team, error)

	// Team query operations
	CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error
	GetTeamQuery(ctx context.Context, queryID string) (*models.TeamQuery, error)
	UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error
	DeleteTeamQuery(ctx context.Context, queryID string) error
	ListTeamQueries(ctx context.Context, teamID string) ([]*models.TeamQuery, error)
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
	return s.store.AddTeamMember(ctx, teamID, userID, role)
}

// RemoveTeamMember removes a user from a team
func (s *service) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
	return s.store.RemoveTeamMember(ctx, teamID, userID)
}

// ListTeamMembers returns all members of a team
func (s *service) ListTeamMembers(ctx context.Context, teamID string) ([]*models.TeamMember, error) {
	return s.store.ListTeamMembers(ctx, teamID)
}

// GetUserTeams returns all teams a user is a member of
func (s *service) ListUserTeams(ctx context.Context, userID string) ([]*models.Team, error) {
	return s.store.ListUserTeams(ctx, userID)
}

// AddTeamSource adds a source to a team
func (s *service) AddTeamSource(ctx context.Context, teamID, sourceID string) error {
	return s.store.AddTeamSource(ctx, teamID, sourceID)
}

// RemoveTeamSource removes a source from a team
func (s *service) RemoveTeamSource(ctx context.Context, teamID, sourceID string) error {
	return s.store.RemoveTeamSource(ctx, teamID, sourceID)
}

// ListTeamSources returns all sources for a team
func (s *service) ListTeamSources(ctx context.Context, teamID string) ([]*models.Source, error) {
	return s.store.ListTeamSources(ctx, teamID)
}

// ListSourceTeams returns all teams that are members of a source
func (s *service) ListSourceTeams(ctx context.Context, sourceID string) ([]*models.Team, error) {
	return s.store.ListSourceTeams(ctx, sourceID)
}

// CreateTeamQuery creates a new team query
func (s *service) CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	return s.store.CreateTeamQuery(ctx, query)
}

// GetTeamQuery retrieves a team query by ID
func (s *service) GetTeamQuery(ctx context.Context, queryID string) (*models.TeamQuery, error) {
	return s.store.GetTeamQuery(ctx, queryID)
}

// UpdateTeamQuery updates a team query
func (s *service) UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	return s.store.UpdateTeamQuery(ctx, query)
}

// DeleteTeamQuery deletes a team query
func (s *service) DeleteTeamQuery(ctx context.Context, queryID string) error {
	return s.store.DeleteTeamQuery(ctx, queryID)
}

// ListTeamQueries returns all team queries for a team
func (s *service) ListTeamQueries(ctx context.Context, teamID string) ([]*models.TeamQuery, error) {
	return s.store.ListTeamQueries(ctx, teamID)
}

// More method implementations will follow...
