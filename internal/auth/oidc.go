package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Define OIDC/Auth specific errors.
var (
	ErrSessionNotFound           = errors.New("session not found") // Referenced in HandleCallback error mapping logic? (Keep for now, though defined in core/session)
	ErrSessionExpired            = errors.New("session expired")   // Referenced in HandleCallback error mapping logic? (Keep for now, though defined in core/session)
	ErrUserNotFound              = errors.New("user not found")    // Referenced in HandleCallback error mapping logic? (Keep for now, though defined in core/users)
	ErrTeamNotFound              = errors.New("team not found")    // Referenced in HandleCallback error mapping logic? (Keep for now, though defined in core/users)
	ErrUnauthorizedUser          = errors.New("unauthorized user")
	ErrUserInactive              = errors.New("user inactive")
	ErrOIDCProviderNotConfigured = errors.New("OIDC provider not configured")
	ErrOIDCInvalidToken          = errors.New("invalid OIDC token")
	ErrOIDCEmailNotVerified      = errors.New("email not verified")
	ErrAdminNotFound             = errors.New("admin not found") // May not be needed if admin check moves to core
)

// OIDCProvider handles OIDC authentication interactions.
type OIDCProvider struct {
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauthConf *oauth2.Config
	log       *slog.Logger
	oidcCfg   *config.OIDCConfig
}

// NewOIDCProvider initializes an OIDCProvider based on the provided configuration.
// It requires explicit AuthURL and TokenURL, but uses ProviderURL for discovery
// to set up the ID token verifier.
func NewOIDCProvider(ctx context.Context, oidcCfg *config.OIDCConfig, log *slog.Logger) (*OIDCProvider, error) {

	var provider *oidc.Provider
	var err error
	var endpoint oauth2.Endpoint

	if oidcCfg.AuthURL != "" && oidcCfg.TokenURL != "" {
		log.Info("using explicit OIDC endpoints", "auth_url", oidcCfg.AuthURL, "token_url", oidcCfg.TokenURL)
		endpoint = oauth2.Endpoint{AuthURL: oidcCfg.AuthURL, TokenURL: oidcCfg.TokenURL}
		// ProviderURL is still needed for discovery to set up the verifier.
		if oidcCfg.ProviderURL == "" {
			log.Error("provider_url is required for OIDC discovery even with explicit endpoints")
			return nil, fmt.Errorf("%w: provider_url is required", ErrOIDCProviderNotConfigured)
		}
		log.Info("using provider URL for OIDC discovery", "provider_url", oidcCfg.ProviderURL)
		provider, err = oidc.NewProvider(ctx, oidcCfg.ProviderURL)
		if err != nil {
			log.Error("failed to create OIDC provider for verification", "error", err, "provider_url", oidcCfg.ProviderURL)
			return nil, fmt.Errorf("%w: %v", ErrOIDCProviderNotConfigured, err)
		}
	} else {
		// Explicit endpoints are required.
		log.Error("missing required OIDC configuration: auth_url and token_url")
		return nil, ErrOIDCProviderNotConfigured
	}

	oauthConf := &oauth2.Config{
		ClientID:     oidcCfg.ClientID,
		ClientSecret: oidcCfg.ClientSecret,
		RedirectURL:  oidcCfg.RedirectURL,
		Endpoint:     endpoint,
		Scopes:       oidcCfg.Scopes,
	}

	// Configure ID token verification.
	// Skip issuer validation to allow flexibility in provider URLs (e.g., internal vs external).
	verifier := provider.Verifier(&oidc.Config{
		ClientID:          oidcCfg.ClientID,
		SkipClientIDCheck: true, // Client ID is implicitly checked by the OAuth2 exchange.
		SkipExpiryCheck:   false,
		SkipIssuerCheck:   true,
	})

	return &OIDCProvider{
		provider:  provider,
		verifier:  verifier,
		oauthConf: oauthConf,
		log:       log,
		oidcCfg:   oidcCfg,
	}, nil
}

// GetAuthURL returns the URL for the OIDC authorization endpoint with the given state.
func (p *OIDCProvider) GetAuthURL(state string) string {
	return p.oauthConf.AuthCodeURL(state)
}

// HandleCallback processes the OIDC callback, exchanges the code for tokens,
// verifies the ID token, looks up or potentially creates the user in the local database,
// and creates a local application session.
func (p *OIDCProvider) HandleCallback(ctx context.Context, db *sqlite.DB, log *slog.Logger, authCfg *config.AuthConfig, code, state string) (*models.User, *models.Session, error) {
	// Exchange authorization code for OAuth2 tokens.
	oauth2Token, err := p.oauthConf.Exchange(ctx, code)
	if err != nil {
		p.log.Error("failed to exchange code for token", "error", err)
		return nil, nil, fmt.Errorf("%w: failed to exchange code for token: %v", ErrOIDCInvalidToken, err)
	}

	// Extract and verify the ID Token.
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		p.log.Error("no id_token field in oauth2 token")
		return nil, nil, ErrOIDCInvalidToken
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		p.log.Error("failed to verify ID token", "error", err)
		return nil, nil, fmt.Errorf("%w: failed to verify ID token: %v", ErrOIDCInvalidToken, err)
	}

	// Extract required claims.
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		p.log.Error("failed to parse ID token claims", "error", err)
		return nil, nil, fmt.Errorf("%w: failed to parse ID token claims: %v", ErrOIDCInvalidToken, err)
	}

	// Ensure email is verified by the OIDC provider.
	if !claims.EmailVerified {
		p.log.Warn("OIDC login attempt with unverified email", "email", claims.Email)
		return nil, nil, ErrOIDCEmailNotVerified
	}

	// Look up user in the local database.
	user, err := core.GetUserByEmail(ctx, db, claims.Email)
	if err != nil {
		// User not found is treated as unauthorized access.
		if errors.Is(err, core.ErrUserNotFound) {
			p.log.Warn("unauthorized user attempted login (user not found in db)", "email", claims.Email, "name", claims.Name)
			return nil, nil, ErrUnauthorizedUser
		}
		// Log other unexpected DB errors.
		p.log.Error("failed to lookup user by email via core function", "error", err, "email", claims.Email)
		return nil, nil, fmt.Errorf("failed to lookup user: %w", err)
	}
	// User exists, check status.
	if user.Status == models.UserStatusInactive {
		p.log.Warn("inactive user attempted login", "user_id", user.ID, "email", user.Email)
		return nil, nil, ErrUserInactive
	}

	// Update user's last login time (best effort).
	now := time.Now()
	updateData := models.User{LastLoginAt: &now}
	if err := core.UpdateUser(ctx, db, log, user.ID, updateData); err != nil {
		// Log failure but don't block login.
		p.log.Error("failed to update user last login via core function", "error", err, "user_id", user.ID)
	}

	// Create a new application session for the user.
	session, err := core.CreateSession(ctx, db, log, user.ID, authCfg.SessionDuration, authCfg.MaxConcurrentSessions)
	if err != nil {
		// Error should have been logged within core.CreateSession.
		return nil, nil, fmt.Errorf("failed to create session: %w", err)
	}

	p.log.Info("user logged in successfully via OIDC callback", "user_id", user.ID, "session_id", session.ID)
	return user, session, nil
}
