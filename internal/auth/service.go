package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/pkg/logger"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/config"
)

// Define standard errors at package level
var (
	ErrSessionNotFound           = fmt.Errorf("session not found")
	ErrSessionExpired            = fmt.Errorf("session expired")
	ErrUserNotFound              = fmt.Errorf("user not found")
	ErrTeamNotFound              = fmt.Errorf("team not found")
	ErrUnauthorizedUser          = fmt.Errorf("unauthorized user")
	ErrUserInactive              = fmt.Errorf("user inactive")
	ErrOIDCProviderNotConfigured = fmt.Errorf("OIDC provider not configured")
	ErrOIDCInvalidToken          = fmt.Errorf("invalid OIDC token")
	ErrOIDCEmailNotVerified      = fmt.Errorf("email not verified")
	ErrAdminNotFound             = fmt.Errorf("admin not found")
)

// Service handles authentication and authorization operations
type Service struct {
	cfg   *config.Config
	store Store
	oidc  *OIDCProvider
	log   *slog.Logger
}

// New creates a new authentication service
func New(cfg *config.Config, store Store) (*Service, error) {
	log := logger.Default().With("component", "auth")

	oidc, err := NewOIDCProvider(cfg, store)
	if err != nil {
		log.Error("failed to create OIDC provider",
			"error", err,
			"provider_url", cfg.OIDC.ProviderURL,
		)
		return nil, err
	}

	return &Service{
		cfg:   cfg,
		store: store,
		oidc:  oidc,
		log:   log,
	}, nil
}

// GetAuthURL returns the URL to redirect the user to for authentication
func (s *Service) GetAuthURL(state string) string {
	return s.oidc.GetAuthURL(state)
}

// HandleCallback processes the OIDC callback and creates or updates the user
func (s *Service) HandleCallback(ctx context.Context, code, state string) (*models.User, *models.Session, error) {
	return s.oidc.HandleCallback(ctx, code, state)
}

// ValidateSession checks if a session is valid and not expired
func (s *Service) ValidateSession(ctx context.Context, sessionID models.SessionID) (*models.Session, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		s.log.Warn("session not found",
			"error", err,
			"session_id", sessionID,
		)
		return nil, ErrSessionNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		s.log.Info("session expired",
			"session_id", sessionID,
			"expires_at", session.ExpiresAt,
		)
		// Delete expired session
		_ = s.store.DeleteSession(ctx, sessionID)
		return nil, ErrSessionExpired
	}

	return session, nil
}

// RevokeSession deletes a session
func (s *Service) RevokeSession(ctx context.Context, sessionID models.SessionID) error {
	return s.store.DeleteSession(ctx, sessionID)
}

// GetUser retrieves a user by ID
func (s *Service) GetUser(ctx context.Context, userID models.UserID) (*models.User, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error getting user: %w", err)
	}

	if user == nil {
		return nil, ErrUserNotFound
	}

	return user, nil
}

// UpdateUser updates a user's information
func (s *Service) UpdateUser(ctx context.Context, user *models.User) error {
	if user == nil {
		return fmt.Errorf("user cannot be nil")
	}

	return s.store.UpdateUser(ctx, user)
}

// ListUsers returns all users
func (s *Service) ListUsers(ctx context.Context) ([]*models.User, error) {
	return s.store.ListUsers(ctx)
}

// CreateTeam creates a new team
func (s *Service) CreateTeam(ctx context.Context, team *models.Team) error {
	if team == nil {
		return fmt.Errorf("team cannot be nil")
	}

	return s.store.CreateTeam(ctx, team)
}

// GetTeam retrieves a team by ID
func (s *Service) GetTeam(ctx context.Context, teamID string) (*models.Team, error) {
	team, err := s.store.GetTeam(ctx, models.TeamID(teamID))
	if err != nil {
		return nil, fmt.Errorf("error getting team: %w", err)
	}

	if team == nil {
		return nil, ErrTeamNotFound
	}

	return team, nil
}

// UpdateTeam updates a team's information
func (s *Service) UpdateTeam(ctx context.Context, team *models.Team) error {
	if team == nil {
		return fmt.Errorf("team cannot be nil")
	}

	return s.store.UpdateTeam(ctx, team)
}

// DeleteTeam deletes a team
func (s *Service) DeleteTeam(ctx context.Context, teamID string) error {
	if teamID == "" {
		return fmt.Errorf("team ID cannot be empty")
	}

	return s.store.DeleteTeam(ctx, models.TeamID(teamID))
}

// ListTeams returns all teams
func (s *Service) ListTeams(ctx context.Context) ([]*models.Team, error) {
	return s.store.ListTeams(ctx)
}

// AddTeamMember adds a user to a team with the specified role
func (s *Service) AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	if teamID == "" || userID == "" {
		return fmt.Errorf("team ID and user ID cannot be empty")
	}

	return s.store.AddTeamMember(ctx, teamID, userID, role)
}

// RemoveTeamMember removes a user from a team
func (s *Service) RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error {
	if teamID == "" || userID == "" {
		return fmt.Errorf("team ID and user ID cannot be empty")
	}

	return s.store.RemoveTeamMember(ctx, teamID, userID)
}

// ListTeamMembers returns all members of a team
func (s *Service) ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	if teamID == "" {
		return nil, fmt.Errorf("team ID cannot be empty")
	}

	members, err := s.store.ListTeamMembers(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error listing team members: %w", err)
	}

	return members, nil
}

// ListUserTeams returns all teams a user is a member of
func (s *Service) ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID cannot be empty")
	}

	teams, err := s.store.ListUserTeams(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error listing user teams: %w", err)
	}

	return teams, nil
}

// AddTeamSource adds a source to a team
func (s *Service) AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	if teamID == "" || sourceID == "" {
		return fmt.Errorf("team ID and source ID cannot be empty")
	}

	return s.store.AddTeamSource(ctx, teamID, sourceID)
}

// RemoveTeamSource removes a source from a team
func (s *Service) RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	if teamID == "" || sourceID == "" {
		return fmt.Errorf("team ID and source ID cannot be empty")
	}

	return s.store.RemoveTeamSource(ctx, teamID, sourceID)
}

// ListTeamSources returns all sources for a team
func (s *Service) ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error) {
	if teamID == "" {
		return nil, fmt.Errorf("team ID cannot be empty")
	}

	sources, err := s.store.ListTeamSources(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error listing team sources: %w", err)
	}

	return sources, nil
}

// ListSourceTeams returns all teams that are members of a source
func (s *Service) ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("source ID cannot be empty")
	}

	teams, err := s.store.ListSourceTeams(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error listing source teams: %w", err)
	}

	return teams, nil
}

// CreateTeamQuery creates a new team query
func (s *Service) CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	if query == nil {
		return fmt.Errorf("query cannot be nil")
	}

	return s.store.CreateTeamQuery(ctx, query)
}

// GetTeamQuery retrieves a team query by ID
func (s *Service) GetTeamQuery(ctx context.Context, queryID string) (*models.TeamQuery, error) {
	if queryID == "" {
		return nil, fmt.Errorf("query ID cannot be empty")
	}

	query, err := s.store.GetTeamQuery(ctx, queryID)
	if err != nil {
		return nil, fmt.Errorf("error getting team query: %w", err)
	}

	return query, nil
}

// UpdateTeamQuery updates a team query
func (s *Service) UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error {
	if query == nil {
		return fmt.Errorf("query cannot be nil")
	}

	return s.store.UpdateTeamQuery(ctx, query)
}

// DeleteTeamQuery deletes a team query
func (s *Service) DeleteTeamQuery(ctx context.Context, queryID string) error {
	if queryID == "" {
		return fmt.Errorf("query ID cannot be empty")
	}

	return s.store.DeleteTeamQuery(ctx, queryID)
}

// ListTeamQueries returns all team queries for a team
func (s *Service) ListTeamQueries(ctx context.Context, teamID models.TeamID) ([]*models.TeamQuery, error) {
	if teamID == "" {
		return nil, fmt.Errorf("team ID cannot be empty")
	}

	queries, err := s.store.ListTeamQueries(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error listing team queries: %w", err)
	}

	return queries, nil
}
