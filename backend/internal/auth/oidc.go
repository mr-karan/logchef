package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"backend-v2/internal/config"
	"backend-v2/pkg/errors"
	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// OIDCProvider handles OIDC authentication
type OIDCProvider struct {
	cfg       *config.Config
	store     Store
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauthConf *oauth2.Config
	log       *slog.Logger
}

// NewOIDCProvider creates a new OIDC provider
func NewOIDCProvider(cfg *config.Config, store Store) (*OIDCProvider, error) {
	log := logger.Default().With("component", "oidc")

	// Validate admin emails configuration
	if len(cfg.Auth.AdminEmails) == 0 {
		log.Error("no admin emails configured")
		return nil, errors.NewAuthError(
			errors.ErrAdminNotFound,
			"No admin emails configured. At least one admin email must be configured.",
			nil,
		)
	}

	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, cfg.OIDC.ProviderURL)
	if err != nil {
		log.Error("failed to create OIDC provider",
			"error", err,
			"provider_url", cfg.OIDC.ProviderURL,
		)
		return nil, errors.NewAuthError(errors.ErrOIDCProviderNotConfigured, "Failed to create OIDC provider", map[string]interface{}{
			"provider_url": cfg.OIDC.ProviderURL,
			"error":        err.Error(),
		})
	}

	oauthConf := &oauth2.Config{
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		RedirectURL:  cfg.OIDC.RedirectURI,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.OIDC.Scopes,
	}

	return &OIDCProvider{
		cfg:       cfg,
		store:     store,
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
		return nil, nil, errors.NewAuthError(errors.ErrOIDCInvalidToken, "Failed to exchange code for token", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Extract the ID Token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		p.log.Error("no ID token found in OAuth response")
		return nil, nil, errors.NewAuthError(errors.ErrOIDCInvalidToken, "No ID token found in OAuth response", nil)
	}

	// Verify the ID Token
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		p.log.Error("failed to verify ID token",
			"error", err,
		)
		return nil, nil, errors.NewAuthError(errors.ErrOIDCInvalidToken, "Failed to verify ID token", map[string]interface{}{
			"error": err.Error(),
		})
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
		return nil, nil, errors.NewAuthError(errors.ErrOIDCInvalidToken, "Failed to parse ID token claims", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// Ensure email is verified
	if !claims.EmailVerified {
		p.log.Error("email not verified",
			"email", claims.Email,
		)
		return nil, nil, errors.NewAuthError(errors.ErrOIDCEmailNotVerified, "Email not verified", map[string]interface{}{
			"email": claims.Email,
		})
	}

	// Check if user exists
	user, err := p.store.GetUserByEmail(ctx, claims.Email)
	if err != nil {
		// If user doesn't exist, create new user
		isAdmin := false
		for _, adminEmail := range p.cfg.Auth.AdminEmails {
			if strings.EqualFold(claims.Email, adminEmail) {
				isAdmin = true
				break
			}
		}

		user = &models.User{
			ID:       uuid.New().String(),
			Email:    claims.Email,
			FullName: claims.Name,
			Role:     "member",
		}
		if isAdmin {
			user.Role = "admin"
		}

		if err := p.store.CreateUser(ctx, user); err != nil {
			p.log.Error("failed to create user",
				"error", err,
				"email", claims.Email,
			)
			return nil, nil, fmt.Errorf("failed to create user: %w", err)
		}

		p.log.Info("created new user",
			"user_id", user.ID,
			"email", user.Email,
			"role", user.Role,
		)
	}

	// Update user's last login
	now := time.Now()
	user.LastLoginAt = &now
	if err := p.store.UpdateUser(ctx, user); err != nil {
		p.log.Error("failed to update user last login",
			"error", err,
			"user_id", user.ID,
		)
		return nil, nil, fmt.Errorf("failed to update user last login: %w", err)
	}

	// Check concurrent sessions
	sessionCount, err := p.store.CountUserSessions(ctx, user.ID)
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
		if err := p.store.DeleteUserSessions(ctx, user.ID); err != nil {
			p.log.Error("failed to delete user sessions",
				"error", err,
				"user_id", user.ID,
			)
			return nil, nil, fmt.Errorf("failed to delete user sessions: %w", err)
		}
	}

	// Create new session
	session := &models.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(p.cfg.Auth.SessionDuration),
	}

	if err := p.store.CreateSession(ctx, session); err != nil {
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
