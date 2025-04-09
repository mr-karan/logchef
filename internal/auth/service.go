package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/sqlite"
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
	cfg  *config.Config
	db   *sqlite.DB
	oidc *OIDCProvider
	log  *slog.Logger
}

// New creates a new authentication service
func New(cfg *config.Config, db *sqlite.DB, logger *slog.Logger) (*Service, error) {
	log := logger.With("component", "auth")

	oidc, err := NewOIDCProvider(cfg, db, log)
	if err != nil {
		log.Error("failed to create OIDC provider",
			"error", err,
			"auth_url", cfg.OIDC.AuthURL,
			"token_url", cfg.OIDC.TokenURL,
		)
		return nil, err
	}

	return &Service{
		cfg:  cfg,
		db:   db,
		oidc: oidc,
		log:  log,
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
	session, err := s.db.GetSession(ctx, sessionID)
	if err != nil {
		s.log.Warn("session not found",
			"error", err,
			"session_id", sessionID,
		)
		// Check if the error message contains "session not found"
		if strings.Contains(err.Error(), "session not found") {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("error validating session: %w", err)
	}

	if session == nil {
		s.log.Warn("session is nil",
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
		_ = s.db.DeleteSession(ctx, sessionID)
		return nil, ErrSessionExpired
	}

	return session, nil
}

// RevokeSession deletes a session
func (s *Service) RevokeSession(ctx context.Context, sessionID models.SessionID) error {
	return s.db.DeleteSession(ctx, sessionID)
}

// GetUser retrieves a user by ID
func (s *Service) GetUser(ctx context.Context, userID models.UserID) (*models.User, error) {
	user, err := s.db.GetUser(ctx, userID)
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

	return s.db.UpdateUser(ctx, user)
}

// ListUsers returns all users
func (s *Service) ListUsers(ctx context.Context) ([]*models.User, error) {
	return s.db.ListUsers(ctx)
}

// CreateTeam creates a new team
func (s *Service) CreateTeam(ctx context.Context, team *models.Team) error {
	if team == nil {
		return fmt.Errorf("team cannot be nil")
	}

	return s.db.CreateTeam(ctx, team)
}

// GetTeam retrieves a team by ID
func (s *Service) GetTeam(ctx context.Context, teamID int) (*models.Team, error) {
	team, err := s.db.GetTeam(ctx, models.TeamID(teamID))
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

	return s.db.UpdateTeam(ctx, team)
}

// DeleteTeam deletes a team
func (s *Service) DeleteTeam(ctx context.Context, teamID int) error {
	if teamID <= 0 {
		return fmt.Errorf("team ID must be positive")
	}

	return s.db.DeleteTeam(ctx, models.TeamID(teamID))
}

// ListTeams returns all teams
func (s *Service) ListTeams(ctx context.Context) ([]*models.Team, error) {
	return s.db.ListTeams(ctx)
}

// AddTeamMember adds a user to a team with the specified role
func (s *Service) AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	if teamID <= 0 || userID <= 0 {
		return fmt.Errorf("team ID and user ID must be positive")
	}

	return s.db.AddTeamMember(ctx, teamID, userID, role)
}

// RemoveTeamMember removes a user from a team
func (s *Service) RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error {
	if teamID <= 0 || userID <= 0 {
		return fmt.Errorf("team ID and user ID must be positive")
	}

	return s.db.RemoveTeamMember(ctx, teamID, userID)
}

// ListTeamMembers returns all members of a team
func (s *Service) ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error) {
	if teamID <= 0 {
		return nil, fmt.Errorf("team ID must be positive")
	}

	return s.db.ListTeamMembers(ctx, teamID)
}

// ListUserTeams returns all teams a user is a member of
func (s *Service) ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user ID must be positive")
	}

	return s.db.ListUserTeams(ctx, userID)
}

// AddTeamSource adds a source to a team
func (s *Service) AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	if teamID <= 0 || sourceID <= 0 {
		return fmt.Errorf("team ID and source ID must be positive")
	}

	return s.db.AddTeamSource(ctx, teamID, sourceID)
}

// RemoveTeamSource removes a source from a team
func (s *Service) RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error {
	if teamID <= 0 || sourceID <= 0 {
		return fmt.Errorf("team ID and source ID must be positive")
	}

	return s.db.RemoveTeamSource(ctx, teamID, sourceID)
}

// ListTeamSources returns all sources associated with a team
func (s *Service) ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error) {
	if teamID <= 0 {
		return nil, fmt.Errorf("team ID must be positive")
	}

	return s.db.ListTeamSources(ctx, teamID)
}

// ListSourceTeams returns all teams that have access to a source
func (s *Service) ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error) {
	if sourceID <= 0 {
		return nil, fmt.Errorf("source ID must be positive")
	}

	return s.db.ListSourceTeams(ctx, sourceID)
}
