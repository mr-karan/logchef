package auth

import (
	"context"
	"crypto/subtle"
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
	ErrOIDCInvalidNonce     = errors.New("invalid OIDC nonce")
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

// CheckEmailVerified rejects explicitly false claims and optionally permits missing claims.
func CheckEmailVerified(claims OIDCClaims, skipCheck bool) error {
	if claims.EmailVerified != nil && *claims.EmailVerified {
		return nil
	}
	if claims.EmailVerified == nil && skipCheck {
		return nil
	}
	return ErrOIDCEmailNotVerified
}

func checkOIDCNonce(expected, claim string) error {
	if expected == "" || subtle.ConstantTimeCompare([]byte(claim), []byte(expected)) != 1 {
		return ErrOIDCInvalidNonce
	}
	return nil
}

const oidcRequestTimeout = 10 * time.Second

// OIDCProvider handles OIDC authentication interactions.
type OIDCProvider struct {
	verifier       *oidc.IDTokenVerifier
	oauthConf      *oauth2.Config
	log            *slog.Logger
	oidcCfg        *config.OIDCConfig
	requestTimeout time.Duration
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
	verifierConfig := &oidc.Config{
		ClientID:        oidcCfg.ClientID,
		SkipExpiryCheck: false,
		SkipIssuerCheck: len(allowedIssuers) > 0,
	}
	if len(allowedIssuers) > 0 {
		log.Info("OIDC issuer validation using explicit allow-list", "allowed_issuers", allowedIssuers)
	}

	if oidcCfg.SkipEmailVerifiedCheck {
		log.Warn("OIDC skip_email_verified_check is enabled — logins will succeed when the email_verified claim is missing")
	}

	return &OIDCProvider{
		verifier:       provider.Verifier(verifierConfig),
		oauthConf:      oauthConf,
		log:            log,
		oidcCfg:        oidcCfg,
		requestTimeout: oidcRequestTimeout,
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
		attemptCtx = oidc.ClientContext(attemptCtx, &http.Client{Transport: transport, Timeout: oidcRequestTimeout})
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

// discoveryTransport tracks retryable network and response failures.
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
			return nil, fmt.Errorf("%w: issuer %q not allowed", ErrOIDCInvalidToken, idToken.Issuer)
		}
	}
	return idToken, nil
}

// GetAuthURL returns the OIDC authorization URL with state and nonce binding.
func (p *OIDCProvider) GetAuthURL(state, nonce string) string {
	return p.oauthConf.AuthCodeURL(state, oidc.Nonce(nonce))
}

// GetIssuer returns the OIDC issuer URL.
func (p *OIDCProvider) GetIssuer() string {
	return p.oidcCfg.ProviderURL
}

// HandleCallback processes the OIDC callback, exchanges the code for tokens,
// verifies the ID token, looks up or potentially creates the user in the local database,
// and creates a local application session.
func (p *OIDCProvider) HandleCallback(ctx context.Context, db store.Store, log *slog.Logger, authCfg *config.AuthConfig, code, nonce string) (*models.User, *models.Session, error) {
	idpCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
	defer cancel()

	// Exchange authorization code for OAuth2 tokens.
	oauth2Token, err := p.oauthConf.Exchange(idpCtx, code)
	if err != nil {
		var rejection *oauth2.RetrieveError
		if errors.As(err, &rejection) && rejection.Response.StatusCode == http.StatusBadRequest && rejection.ErrorCode == "invalid_grant" {
			return nil, nil, fmt.Errorf("%w: failed to exchange code for token: %w", ErrOIDCInvalidToken, err)
		}
		return nil, nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}

	// Extract and verify the ID Token.
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, nil, fmt.Errorf("%w: token response has no id_token", ErrOIDCInvalidToken)
	}
	idToken, err := p.verify(idpCtx, rawIDToken)
	if err != nil {
		// go-oidc flattens key-fetch errors with %v at the verifier boundary.
		if idpCtx.Err() != nil || strings.HasPrefix(err.Error(), "failed to verify signature: fetching keys ") {
			return nil, nil, fmt.Errorf("failed to verify ID token: %w", err)
		}
		return nil, nil, fmt.Errorf("%w: failed to verify ID token: %w", ErrOIDCInvalidToken, err)
	}
	if err := checkOIDCNonce(nonce, idToken.Nonce); err != nil {
		return nil, nil, err
	}

	cancel()

	// Extract required claims.
	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, nil, fmt.Errorf("%w: failed to parse ID token claims: %w", ErrOIDCInvalidToken, err)
	}

	// Verify email_verified claim.
	if err := CheckEmailVerified(claims, p.oidcCfg.SkipEmailVerifiedCheck); err != nil {
		return nil, nil, fmt.Errorf("%w: email=%s", err, claims.Email)
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
				return nil, nil, fmt.Errorf("failed to auto-provision user for email=%s: %w", claims.Email, provErr)
			}
			if !attempted {
				return nil, nil, fmt.Errorf("%w: email=%s", ErrUnauthorizedUser, claims.Email)
			}
			user = provisioned
		} else {
			return nil, nil, fmt.Errorf("failed to lookup user for email=%s: %w", claims.Email, err)
		}
	}
	if user.AccountType == models.UserAccountTypeService || user.Status == models.UserStatusInactive {
		return nil, nil, fmt.Errorf("%w: email=%s", ErrUserInactive, claims.Email)
	}

	// Update user's last login time (best effort).
	if err := db.UpdateUserLastLogin(ctx, user.ID, time.Now().UTC()); err != nil {
		// Log failure but don't block login.
		p.log.Error("failed to update user last login", "error", err, "user_id", user.ID)
	}

	// Create a new application session for the user.
	session, err := core.CreateSession(ctx, db, log, user.ID, authCfg.SessionDuration, authCfg.MaxConcurrentSessions)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create session for email=%s: %w", claims.Email, err)
	}

	return user, session, nil
}
