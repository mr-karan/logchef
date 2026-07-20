package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
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

// OIDCClaims represents the claims extracted from an OIDC ID token.
// EmailVerified is a *bool to distinguish between a missing/null claim
// (nil) and an explicit false value. This matters for providers like
// Cloudflare Access that omit the claim entirely.
type OIDCClaims struct {
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
}

// CheckEmailVerified validates the email_verified claim according to the
// skip_email_verified_check configuration. The three cases are:
//   - claim is true: always allowed.
//   - claim is missing/null (nil): allowed only when skipCheck is true.
//   - claim is explicitly false: always rejected, even when skipCheck is true.
func CheckEmailVerified(claims OIDCClaims, skipCheck bool, log *slog.Logger, logCtx string) error {
	switch {
	case claims.EmailVerified != nil && *claims.EmailVerified:
		// Email is verified, nothing to do.
		return nil
	case claims.EmailVerified != nil && !*claims.EmailVerified:
		// Provider explicitly says email is NOT verified — always reject.
		log.Warn(logCtx+": email_verified is explicitly false", "email", claims.Email)
		return ErrOIDCEmailNotVerified
	default:
		// Claim is missing/null.
		if skipCheck {
			log.Warn(logCtx+": email_verified claim is missing, proceeding anyway (skip_email_verified_check=true)", "email", claims.Email)
			return nil
		}
		log.Warn(logCtx+": email_verified claim is missing", "email", claims.Email)
		return ErrOIDCEmailNotVerified
	}
}

// OIDCProvider handles OIDC authentication interactions.
type OIDCProvider struct {
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauthConf *oauth2.Config
	log       *slog.Logger
	oidcCfg   *config.OIDCConfig
	// allowedIssuers, when non-empty, is the explicit set of acceptable `iss`
	// claim values. It is populated only when the operator overrides issuer
	// validation via config; otherwise it is nil and the go-oidc verifier
	// enforces the single discovered issuer itself.
	allowedIssuers []string
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

	// Configure ID token verification. Audience (ClientID) is always validated
	// to prevent token-confusion attacks. Issuer validation is enforced too:
	//   - No allowed_issuers override (the default): go-oidc validates the `iss`
	//     claim against the single issuer discovered from provider_url.
	//   - allowed_issuers set: go-oidc's single-issuer check is disabled and we
	//     validate `iss` against the operator-provided allow-list ourselves
	//     (see verify), which is the only way to support multi-realm IdPs that
	//     share one JWKS across issuers.
	allowedIssuers := normalizeIssuers(oidcCfg.AllowedIssuers)
	verifier := provider.Verifier(&oidc.Config{
		ClientID:        oidcCfg.ClientID,
		SkipExpiryCheck: false,
		SkipIssuerCheck: len(allowedIssuers) > 0,
	})
	if len(allowedIssuers) > 0 {
		log.Info("OIDC issuer validation using explicit allow-list", "allowed_issuers", allowedIssuers)
	}

	if oidcCfg.SkipEmailVerifiedCheck {
		log.Warn("OIDC skip_email_verified_check is enabled — logins will succeed when the email_verified claim is missing")
	}

	return &OIDCProvider{
		provider:       provider,
		verifier:       verifier,
		oauthConf:      oauthConf,
		log:            log,
		oidcCfg:        oidcCfg,
		allowedIssuers: allowedIssuers,
	}, nil
}

// normalizeIssuers trims blanks and drops empty entries from the configured
// issuer allow-list.
func normalizeIssuers(issuers []string) []string {
	var out []string
	for _, iss := range issuers {
		if trimmed := strings.TrimSpace(iss); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// verify verifies a raw ID token's signature/audience/expiry and then, when an
// issuer allow-list is configured, rejects any token whose `iss` claim is not
// in the list. With no allow-list, the go-oidc verifier has already enforced
// the single discovered issuer.
func (p *OIDCProvider) verify(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, err
	}
	if len(p.allowedIssuers) > 0 {
		allowed := false
		for _, iss := range p.allowedIssuers {
			if idToken.Issuer == iss {
				allowed = true
				break
			}
		}
		if !allowed {
			p.log.Warn("OIDC token issuer not in allow-list", "issuer", idToken.Issuer)
			return nil, fmt.Errorf("%w: issuer %q not allowed", ErrOIDCInvalidToken, idToken.Issuer)
		}
	}
	return idToken, nil
}

// GetAuthURL returns the URL for the OIDC authorization endpoint with the given state.
func (p *OIDCProvider) GetAuthURL(state string) string {
	return p.oauthConf.AuthCodeURL(state)
}

// VerifyIDToken verifies an ID token string and returns the parsed token.
// Issuer validation follows the configured allow-list (see verify).
func (p *OIDCProvider) VerifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	return p.verify(ctx, rawIDToken)
}

// GetIssuer returns the OIDC issuer URL.
func (p *OIDCProvider) GetIssuer() string {
	return p.oidcCfg.ProviderURL
}

// HandleCallback processes the OIDC callback, exchanges the code for tokens,
// verifies the ID token, looks up or potentially creates the user in the local database,
// and creates a local application session.
func (p *OIDCProvider) HandleCallback(ctx context.Context, db store.Store, log *slog.Logger, authCfg *config.AuthConfig, code, state string) (*models.User, *models.Session, error) {
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
	idToken, err := p.verify(ctx, rawIDToken)
	if err != nil {
		p.log.Error("failed to verify ID token", "error", err)
		return nil, nil, fmt.Errorf("%w: failed to verify ID token: %v", ErrOIDCInvalidToken, err)
	}

	// Extract required claims.
	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		p.log.Error("failed to parse ID token claims", "error", err)
		return nil, nil, fmt.Errorf("%w: failed to parse ID token claims: %v", ErrOIDCInvalidToken, err)
	}

	// Verify email_verified claim.
	if err := CheckEmailVerified(claims, p.oidcCfg.SkipEmailVerifiedCheck, p.log, "OIDC callback"); err != nil {
		return nil, nil, err
	}

	// Look up user in the local database.
	user, err := core.GetUserByEmail(ctx, db, claims.Email)
	if err != nil {
		// User not found: try JIT auto-provisioning (only runs after the
		// email_verified gate above has already passed) before treating this
		// as unauthorized access.
		if errors.Is(err, core.ErrUserNotFound) {
			provisioned, attempted, provErr := autoProvisionUser(ctx, db, log, authCfg.AutoProvision, claims.Email, claims.Name)
			if provErr != nil {
				p.log.Error("failed to auto-provision user", "error", provErr, "email", claims.Email)
				return nil, nil, fmt.Errorf("failed to auto-provision user: %w", provErr)
			}
			if !attempted {
				p.log.Warn("unauthorized user attempted login (user not found in db)", "email", claims.Email, "name", claims.Name)
				return nil, nil, ErrUnauthorizedUser
			}
			user = provisioned
		} else {
			// Log other unexpected DB errors.
			p.log.Error("failed to lookup user by email via core function", "error", err, "email", claims.Email)
			return nil, nil, fmt.Errorf("failed to lookup user: %w", err)
		}
	}
	if user.AccountType == models.UserAccountTypeService || user.Status == models.UserStatusInactive {
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

	return user, session, nil
}
