package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
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
	ErrUnauthorizedUser     = errors.New("unauthorized user")
	ErrUserInactive         = errors.New("user inactive")
	ErrOIDCInvalidToken     = errors.New("invalid OIDC token")
	ErrOIDCEmailNotVerified = errors.New("email not verified")
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

// discoveryRetry bounds how long startup waits for the IdP discovery endpoint.
type discoveryRetry struct {
	attemptTimeout time.Duration
	maxWait        time.Duration
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// defaultDiscoveryRetry rides out an IdP that starts a little after Logchef
// (host reboot, compose ordering) without hanging startup indefinitely.
var defaultDiscoveryRetry = discoveryRetry{
	attemptTimeout: 10 * time.Second,
	maxWait:        60 * time.Second,
	initialBackoff: time.Second,
	maxBackoff:     10 * time.Second,
}

// NewOIDCProvider initializes an OIDCProvider from a validated OIDC config.
// It uses the explicit AuthURL and TokenURL for the OAuth flow and discovers
// ProviderURL to set up the ID token verifier, retrying while the IdP is
// unreachable.
func NewOIDCProvider(ctx context.Context, oidcCfg *config.OIDCConfig, log *slog.Logger) (*OIDCProvider, error) {
	log.Info("using explicit OIDC endpoints", "auth_url", oidcCfg.AuthURL, "token_url", oidcCfg.TokenURL)
	endpoint := oauth2.Endpoint{AuthURL: oidcCfg.AuthURL, TokenURL: oidcCfg.TokenURL}

	provider, err := discoverProvider(ctx, oidcCfg.ProviderURL, defaultDiscoveryRetry, log)
	if err != nil {
		return nil, err
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

// discoverProvider fetches the OIDC discovery document, retrying with backoff
// until retry.maxWait elapses. Failures that cannot recover by waiting (bad
// URL, most 4xx, invalid metadata, issuer mismatch) return at once.
func discoverProvider(ctx context.Context, providerURL string, retry discoveryRetry, log *slog.Logger) (*oidc.Provider, error) {
	ctx, cancel := context.WithTimeout(ctx, retry.maxWait)
	defer cancel()

	backoff := retry.initialBackoff
	for attempt := 1; ; attempt++ {
		transport := &discoveryTransport{}
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, retry.attemptTimeout)
		attemptCtx = oidc.ClientContext(attemptCtx, &http.Client{Transport: transport})
		provider, err := oidc.NewProvider(attemptCtx, providerURL)
		cancelAttempt()
		if err == nil {
			return provider, nil
		}

		if !transport.retryable() {
			return nil, fmt.Errorf("error discovering OIDC provider %s: %w", providerURL, err)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("error discovering OIDC provider %s after %d attempts: %w", providerURL, attempt, err)
		}

		log.Warn("OIDC discovery failed, retrying", "provider_url", providerURL, "attempt", attempt, "retry_in", backoff.String(), "error", err)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("error discovering OIDC provider %s after %d attempts: %w", providerURL, attempt, err)
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, retry.maxBackoff)
	}
}

// discoveryTransport records whether the discovery request went out, its
// response status, and whether reading the body failed, so a booting IdP can
// be told apart from a broken config. go-oidc keeps this client for later JWKS
// fetches, hence the atomics.
type discoveryTransport struct {
	sent       atomic.Bool
	status     atomic.Int64
	readFailed atomic.Bool
}

func (t *discoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.sent.Store(true)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if resp != nil {
		t.status.Store(int64(resp.StatusCode))
		resp.Body = &discoveryBody{ReadCloser: resp.Body, failed: &t.readFailed}
	}
	return resp, err
}

// retryable reports whether a failed attempt may succeed later: no response
// or a body cut off (connection, TLS, timeout), 5xx, 408, 429, or a 404 from a
// reverse proxy whose route to the IdP is not registered yet.
func (t *discoveryTransport) retryable() bool {
	if !t.sent.Load() {
		return false
	}
	if t.readFailed.Load() {
		return true
	}
	switch status := t.status.Load(); {
	case status == 0, status >= 500:
		return true
	case status == http.StatusNotFound, status == http.StatusRequestTimeout, status == http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

// discoveryBody flags a body read that fails before EOF. go-oidc formats that
// error with %v, so the error chain cannot tell it apart from bad metadata.
type discoveryBody struct {
	io.ReadCloser
	failed *atomic.Bool
}

func (b *discoveryBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		b.failed.Store(true)
	}
	return n, err
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
		allowed := slices.Contains(p.allowedIssuers, idToken.Issuer)
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
		return nil, nil, fmt.Errorf("%w: failed to exchange code for token: %w", ErrOIDCInvalidToken, err)
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
		return nil, nil, fmt.Errorf("%w: failed to verify ID token: %w", ErrOIDCInvalidToken, err)
	}

	// Extract required claims.
	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		p.log.Error("failed to parse ID token claims", "error", err)
		return nil, nil, fmt.Errorf("%w: failed to parse ID token claims: %w", ErrOIDCInvalidToken, err)
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
