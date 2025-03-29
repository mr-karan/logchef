package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/config"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// OIDCProvider handles OIDC authentication
type OIDCProvider struct {
	cfg       *config.Config
	db        *sqlite.DB
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauthConf *oauth2.Config
	log       *slog.Logger
}

// NewOIDCProvider creates a new OIDC provider
func NewOIDCProvider(cfg *config.Config, db *sqlite.DB, log *slog.Logger) (*OIDCProvider, error) {
	// Validate admin emails configuration
	if len(cfg.Auth.AdminEmails) == 0 {
		log.Error("no admin emails configured")
		return nil, ErrAdminNotFound
	}

	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, cfg.OIDC.ProviderURL)
	if err != nil {
		log.Error("failed to create OIDC provider",
			"error", err,
			"provider_url", cfg.OIDC.ProviderURL,
		)
		return nil, fmt.Errorf("%w: %v", ErrOIDCProviderNotConfigured, err)
	}

	oauthConf := &oauth2.Config{
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		RedirectURL:  cfg.OIDC.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.OIDC.Scopes,
	}

	return &OIDCProvider{
		cfg:       cfg,
		db:        db,
		provider:  provider,
		verifier:  provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		oauthConf: oauthConf,
		log:       log,
	}, nil
}

// GetAuthURL returns the URL to redirect the user to for authentication
func (p *OIDCProvider) GetAuthURL(state string) string {
	// Use the OAuth2 config's AuthCodeURL which handles proper URL encoding
	return p.oauthConf.AuthCodeURL(state)
}

// HandleCallback processes the OIDC callback and creates or updates the user
func (p *OIDCProvider) HandleCallback(ctx context.Context, code, state string) (*models.User, *models.Session, error) {
	// Exchange code for token
	oauth2Token, err := p.oauthConf.Exchange(ctx, code)
	if err != nil {
		p.log.Error("failed to exchange code for token",
			"error", err,
		)
		return nil, nil, fmt.Errorf("%w: failed to exchange code for token: %v", ErrOIDCInvalidToken, err)
	}

	// Extract the ID Token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		p.log.Error("no ID token found in OAuth response")
		return nil, nil, ErrOIDCInvalidToken
	}

	// Verify the ID Token
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		p.log.Error("failed to verify ID token",
			"error", err,
		)
		return nil, nil, fmt.Errorf("%w: failed to verify ID token: %v", ErrOIDCInvalidToken, err)
	}

	// Extract claims
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		p.log.Error("failed to parse ID token claims",
			"error", err,
		)
		return nil, nil, fmt.Errorf("%w: failed to parse ID token claims: %v", ErrOIDCInvalidToken, err)
	}

	// Ensure email is verified
	if !claims.EmailVerified {
		p.log.Error("email not verified",
			"email", claims.Email,
		)
		return nil, nil, ErrOIDCEmailNotVerified
	}

	// Check if user exists
	user, err := p.db.GetUserByEmail(ctx, claims.Email)
	if err != nil {
		p.log.Error("failed to lookup user by email",
			"error", err,
			"email", claims.Email,
		)
		return nil, nil, fmt.Errorf("failed to lookup user: %w", err)
	}

	if user == nil {
		// User doesn't exist - log attempt and return error
		p.log.Warn("unauthorized user attempted login",
			"email", claims.Email,
			"name", claims.Name,
		)
		return nil, nil, ErrUnauthorizedUser
	}

	// Check if user is active
	if user.Status == models.UserStatusInactive {
		p.log.Error("inactive user attempted login",
			"user_id", user.ID,
			"email", user.Email,
		)
		return nil, nil, ErrUserInactive
	}

	// Update user's last login
	now := time.Now()
	user.LastLoginAt = &now
	if err := p.db.UpdateUser(ctx, user); err != nil {
		p.log.Error("failed to update user last login",
			"error", err,
			"user_id", user.ID,
		)
		return nil, nil, fmt.Errorf("failed to update user last login: %w", err)
	}

	// Check concurrent sessions
	sessionCount, err := p.db.CountUserSessions(ctx, user.ID)
	if err != nil {
		p.log.Error("failed to count user sessions",
			"error", err,
			"user_id", user.ID,
		)
		return nil, nil, fmt.Errorf("failed to count user sessions: %w", err)
	}

	if sessionCount >= p.cfg.Auth.MaxConcurrentSessions {
		p.log.Info("deleting existing sessions due to max concurrent sessions",
			"user_id", user.ID,
			"max_sessions", p.cfg.Auth.MaxConcurrentSessions,
		)
		// Delete all existing sessions
		if err := p.db.DeleteUserSessions(ctx, user.ID); err != nil {
			p.log.Error("failed to delete user sessions",
				"error", err,
				"user_id", user.ID,
			)
			return nil, nil, fmt.Errorf("failed to delete user sessions: %w", err)
		}
	}

	// Create new session
	sessionID := uuid.New().String()
	session := &models.Session{
		ID:        models.SessionID(sessionID),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(p.cfg.Auth.SessionDuration),
	}

	if err := p.db.CreateSession(ctx, session); err != nil {
		p.log.Error("failed to create session",
			"error", err,
			"user_id", user.ID,
		)
		return nil, nil, fmt.Errorf("failed to create session: %w", err)
	}

	p.log.Info("user logged in successfully",
		"user_id", user.ID,
		"session_id", session.ID,
	)

	return user, session, nil
}
