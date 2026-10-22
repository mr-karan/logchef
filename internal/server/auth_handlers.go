package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/oauth"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/oauth2"
)

const (
	// Cookie names used for authentication state and session.
	stateCookieName    = "auth_state"
	nonceCookieName    = "auth_nonce"
	verifierCookieName = "auth_pkce"
	sessionCookieName  = "session_id"
	// stateCookieTTL defines how long the OIDC state cookie is valid.
	stateCookieTTL = 5 * time.Minute
)

var (
	errInvalidState = errors.New("invalid login state")
	errAccessDenied = errors.New("access denied by identity provider")
)

// generateState binds random state to a timestamp for cookie expiry checks.
func generateState() (string, error) {
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("failed to generate random state bytes: %w", err)
	}
	entropyBytes := make([]byte, 32)
	if _, err := rand.Read(entropyBytes); err != nil {
		return "", fmt.Errorf("failed to generate state entropy: %w", err)
	}

	timestamp := time.Now().Unix()
	combined := fmt.Sprintf("%s.%s.%d",
		base64.RawURLEncoding.EncodeToString(stateBytes),
		base64.RawURLEncoding.EncodeToString(entropyBytes),
		timestamp,
	)
	return combined, nil
}

func nonceFromSecret(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// validateState checks the OIDC callback state against the stored state cookie.
// It verifies the format, expiration (based on timestamp), and performs a constant-time comparison.
func validateState(state, storedState string) error {
	if state == "" || storedState == "" {
		return fmt.Errorf("%w: missing state parameter or cookie", errInvalidState)
	}

	// Basic format check.
	parts := strings.Split(storedState, ".")
	if len(parts) != 3 {
		return fmt.Errorf("%w: invalid state format", errInvalidState)
	}

	// Check timestamp for expiration.
	timestamp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return fmt.Errorf("%w: invalid timestamp in state: %w", errInvalidState, err)
	}
	if time.Since(time.Unix(timestamp, 0)) > stateCookieTTL {
		return fmt.Errorf("%w: state has expired", errInvalidState)
	}

	// Constant-time comparison to mitigate timing attacks.
	if !secureCompare(state, storedState) {
		return fmt.Errorf("%w: state mismatch", errInvalidState)
	}

	return nil
}

// secureCompare performs a constant-time comparison of two strings.
func secureCompare(a, b string) bool {
	// Use crypto/subtle for timing attack resistance.
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// defaultPostLoginPath is where users land after a successful login when no
// (valid) redirect path was requested.
const defaultPostLoginPath = "/logs/explore"

// isSafeLocalPath reports whether path is a safe same-origin redirect target.
// A safe path is a local absolute path: it starts with exactly one '/', is not
// protocol-relative ("//host" or the backslash variant "/\host" that browsers
// normalize to "//host"), and carries no scheme ("://"). This blocks open
// redirects where an attacker-supplied path escapes to an external origin —
// critical because redirectToFrontend collapses an empty frontend_url to "",
// making a "//evil.example" path a fully-qualified protocol-relative URL.
func isSafeLocalPath(path string) bool {
	if path == "" || path[0] != '/' {
		return false
	}
	if strings.HasPrefix(path, "//") || strings.HasPrefix(path, "/\\") {
		return false
	}
	if strings.Contains(path, "://") {
		return false
	}
	return true
}

// redirectToFrontend redirects the user's browser to the configured frontend URL,
// optionally appending an error code as a query parameter if an error occurred.
func (s *Server) redirectToFrontend(c fiber.Ctx, path string, err error) error {
	targetURL := s.config.Server.FrontendURL
	if targetURL == "" {
		targetURL = "/" // Default to root if no frontend URL is configured.
	}

	// Ensure the base URL doesn't end with a slash and the path starts with one.
	targetURL = strings.TrimSuffix(targetURL, "/")
	// Validate the path server-side before it becomes the redirect target. Any
	// path that is not a safe local path (empty, relative, protocol-relative,
	// or scheme-bearing) falls back to root so a crafted "//evil.example" can
	// never be emitted as the Location header.
	if !isSafeLocalPath(path) {
		path = "/"
	}

	finalURL := targetURL + path

	// If an error occurred, map it to a code and append to the redirect URL.
	if err != nil {
		var errCode string
		// Map known auth errors to specific codes for the frontend.
		switch {
		case errors.Is(err, errAccessDenied):
			errCode = "access_denied"
		case errors.Is(err, errInvalidState), errors.Is(err, auth.ErrOIDCInvalidNonce):
			errCode = "session_expired"
		case errors.Is(err, auth.ErrOIDCInvalidToken):
			errCode = "invalid_token"
		case errors.Is(err, auth.ErrOIDCEmailNotVerified):
			errCode = "email_not_verified"
		case errors.Is(err, auth.ErrUnauthorizedUser):
			errCode = "unauthorized"
		case errors.Is(err, auth.ErrUserInactive):
			errCode = "user_inactive"
		default:
			errCode = "auth_error"
		}
		finalURL = fmt.Sprintf("%s/auth/login?error=%s", targetURL, errCode)
		level := slog.LevelWarn
		if errCode == "auth_error" {
			level = slog.LevelError
		}
		s.log.Log(c.Context(), level, "authentication error during redirect",
			"error_code", errCode,
			"error", err,
			"provider_error", c.Query("error"),
			"error_description", c.Query("error_description"),
			"redirect_url", finalURL,
		)
	}

	return c.Redirect().Status(fiber.StatusTemporaryRedirect).To(finalURL)
}

// handleLogin stores state, a nonce secret and a PKCE verifier in cookies and sends state, the secret's hash and the S256 challenge.
func (s *Server) handleLogin(c fiber.Ctx) error {
	if s.oidcProvider == nil {
		s.log.Error("OIDC provider not configured, cannot initiate login")
		return SendError(c, fiber.StatusInternalServerError, "Authentication provider not configured")
	}

	state, err := generateState()
	if err != nil {
		s.log.Error("failed to generate OIDC state", "error", err)
		return s.redirectToFrontend(c, "", fmt.Errorf("state generation failed: %w", err))
	}

	var nonceBytes [32]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		s.log.Error("failed to generate OIDC nonce secret", "error", err)
		return s.redirectToFrontend(c, "", fmt.Errorf("nonce generation failed: %w", err))
	}
	nonceSecret := base64.RawURLEncoding.EncodeToString(nonceBytes[:])
	verifier := oauth2.GenerateVerifier()
	expires := time.Now().Add(stateCookieTTL)

	s.setCookie(c, stateCookieName, state, expires)
	s.setCookie(c, nonceCookieName, nonceSecret, expires)
	s.setCookie(c, verifierCookieName, verifier, expires)

	// Persist the requested redirect path through the OIDC round-trip. Only
	// safe local paths are stored; anything protocol-relative or scheme-bearing
	// is dropped so the callback falls back to the default landing page.
	if redirectPath := c.Query("redirect", ""); isSafeLocalPath(redirectPath) {
		s.setCookie(c, "logchef_redirect", redirectPath, time.Now().Add(stateCookieTTL))
	}

	authURL := s.oidcProvider.GetAuthURL(state, nonceFromSecret(nonceSecret), verifier)
	return c.Redirect().Status(fiber.StatusTemporaryRedirect).To(authURL)
}

// handleCallback handles the redirect back from the OIDC provider.
// It validates the state, exchanges the code for tokens, verifies the ID token,
// processes the user login (checking existence, status), creates a session,
// sets the session cookie, and redirects the user back to the frontend.
func (s *Server) handleCallback(c fiber.Ctx) error {
	if s.oidcProvider == nil {
		s.log.Error("OIDC provider not configured, cannot handle callback")
		return SendError(c, fiber.StatusInternalServerError, "Authentication provider not configured")
	}

	if providerError := c.Query("error"); providerError != "" {
		if providerError == "access_denied" {
			return s.redirectToFrontend(c, "", errAccessDenied)
		}
		return s.redirectToFrontend(c, "", errors.New("identity provider returned an error"))
	}

	state := c.Query("state")
	code := c.Query("code")

	if code == "" {
		return s.redirectToFrontend(c, "", fmt.Errorf("authorization code missing"))
	}

	storedState := c.Cookies(stateCookieName)
	if err := validateState(state, storedState); err != nil {
		return s.redirectToFrontend(c, "", fmt.Errorf("state validation failed: %w", err))
	}

	s.clearCookie(c, stateCookieName)
	s.clearCookie(c, nonceCookieName)
	s.clearCookie(c, verifierCookieName)

	nonceSecret := c.Cookies(nonceCookieName)
	if nonceSecret == "" {
		return s.redirectToFrontend(c, "", fmt.Errorf("%w: missing nonce cookie", auth.ErrOIDCInvalidNonce))
	}

	verifier := c.Cookies(verifierCookieName)
	if verifier == "" {
		return s.redirectToFrontend(c, "", fmt.Errorf("%w: missing PKCE verifier cookie", errInvalidState))
	}

	loginUser, session, err := s.oidcProvider.HandleCallback(c.Context(), s.store, s.log, &s.config.Auth, code, nonceFromSecret(nonceSecret), verifier)
	if err != nil {
		return s.redirectToFrontend(c, "", err)
	}

	s.log.Info("user.login",
		"email", loginUser.Email,
		"user_id", loginUser.ID,
		"name", loginUser.FullName,
		"role", loginUser.Role,
	)

	// Auto-provision (and self-heal) the user's personal collection. Best-effort:
	// a transient failure here must not block login.
	if _, err := core.EnsurePersonalCollection(c.Context(), s.store, s.log, loginUser); err != nil {
		s.log.Warn("failed to ensure personal collection on login", "error", err, "user_id", loginUser.ID)
	}

	// Set the application session cookie.
	s.setCookie(c, sessionCookieName, string(session.ID), session.ExpiresAt)

	// Redirect back to the frontend, restoring the path requested before login.
	// Re-validate the cookie value (defense in depth): a tampered or unsafe
	// path falls back to the default landing page instead of being trusted.
	redirectPath := c.Cookies("logchef_redirect")
	if !isSafeLocalPath(redirectPath) {
		redirectPath = defaultPostLoginPath
	}
	// Clear the redirect cookie
	s.clearCookie(c, "logchef_redirect")
	return s.redirectToFrontend(c, redirectPath, nil)
}

// handleLogout handles user logout requests.
// It revokes the current session (if found) and clears the session cookie.
func (s *Server) handleLogout(c fiber.Ctx) error {
	sessionIDStr := c.Cookies(sessionCookieName)
	if sessionIDStr != "" {
		// Attempt to revoke the session in the database.
		if err := core.RevokeSession(c.Context(), s.store, s.log, models.SessionID(sessionIDStr)); err != nil {
			// Log error but proceed with cookie deletion anyway.
			s.log.Error("failed to revoke session during logout", "error", err, "session_id_prefix", sessionIDStr[:min(len(sessionIDStr), 8)])
		}
	}

	// Clear the session cookie in the browser.
	s.clearCookie(c, sessionCookieName)

	return SendSuccess(c, fiber.StatusOK, nil) // Send simple success response.
}

// handleGetCurrentUser retrieves information about the currently authenticated user.
// It relies on the requireAuth middleware to populate user details in the context.
// Supports both session-based and API token authentication.
func (s *Server) handleGetCurrentUser(c fiber.Ctx) error {
	user := currentUser(c)
	if user == nil {
		s.log.Error("user missing from context in handleGetCurrentUser")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication data unavailable", models.AuthenticationErrorType)
	}

	authMethod := c.Locals("auth_method")
	response := fiber.Map{
		"user":        user,
		"auth_method": authMethod,
	}

	// Include session info if authenticated via session
	if session, ok := c.Locals("session").(*models.Session); ok && session != nil {
		response["session"] = fiber.Map{
			"expires_at": session.ExpiresAt,
		}
	}

	// Include API token info if authenticated via token (without sensitive data)
	if apiToken, ok := c.Locals("api_token").(*models.APIToken); ok && apiToken != nil {
		response["api_token"] = fiber.Map{
			"id":           apiToken.ID,
			"name":         apiToken.Name,
			"prefix":       apiToken.Prefix,
			"last_used_at": apiToken.LastUsedAt,
			"created_at":   apiToken.CreatedAt,
		}
		response["auth"] = fiber.Map{
			"method":     "token",
			"scopes":     apiToken.Scopes,
			"expires_at": apiToken.ExpiresAt,
		}
	}

	// OAuth callers see the access token's scopes, expiry and client.
	if token, ok := c.Locals("oauth_token").(*oauth.AccessToken); ok && token != nil {
		response["auth"] = fiber.Map{
			"method":     "oauth",
			"scopes":     token.Principal.Scopes(),
			"expires_at": token.ExpiresAt,
			"client_id":  token.Principal.ClientID,
		}
	}

	return SendSuccess(c, fiber.StatusOK, response)
}

// handleLocalLogin authenticates a user with email+password (local auth).
// Failures are indistinguishable (unknown email, wrong password, inactive,
// service account) and the endpoint 404s when local auth is disabled.
func (s *Server) handleLocalLogin(c fiber.Ctx) error {
	if !s.config.Auth.Local.Enabled {
		return SendErrorWithType(c, fiber.StatusNotFound, "Local authentication is not enabled", models.NotFoundErrorType)
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind().Body(&req); err != nil || req.Email == "" || req.Password == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "email and password are required", models.ValidationErrorType)
	}

	invalid := func() error {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "invalid email or password", models.AuthenticationErrorType)
	}
	if len(req.Email) > models.MaxEmailLength {
		return invalid()
	}

	if !s.localLoginLimiter.Allow(c.IP(), req.Email) {
		return SendErrorWithType(c, fiber.StatusTooManyRequests, "Too many login attempts, try again shortly", models.ValidationErrorType)
	}

	user, err := s.store.GetUserByEmail(c.Context(), req.Email)
	if err != nil || user == nil {
		// Burn a bcrypt comparison so unknown emails aren't timing-distinguishable.
		auth.VerifyLocalPassword("", req.Password)
		return invalid()
	}
	if user.Status != models.UserStatusActive || user.AccountType != models.UserAccountTypeHuman {
		auth.VerifyLocalPassword("", req.Password)
		return invalid()
	}
	if !auth.VerifyLocalPassword(user.PasswordHash, req.Password) {
		return invalid()
	}

	session, err := core.CreateSession(c.Context(), s.store, s.log, user.ID, s.config.Auth.SessionDuration, s.config.Auth.MaxConcurrentSessions)
	if err != nil {
		s.log.Error("failed to create session for local login", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to create session")
	}

	s.log.Info("user.login", "email", user.Email, "user_id", user.ID, "method", "local")

	if _, err := core.EnsurePersonalCollection(c.Context(), s.store, s.log, user); err != nil {
		s.log.Warn("failed to ensure personal collection on login", "error", err, "user_id", user.ID)
	}

	s.setCookie(c, sessionCookieName, string(session.ID), session.ExpiresAt)

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"user": user})
}

// setCookie writes an HttpOnly, SameSite=Lax cookie under the UI base path.
func (s *Server) setCookie(c fiber.Ctx, name, value string, expires time.Time) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    value,
		Expires:  expires,
		HTTPOnly: true,
		Secure:   s.config.Server.IsSecureCookie(),
		SameSite: fiber.CookieSameSiteLaxMode,
		Path:     s.config.Server.CookiePath(),
	})
}

// clearCookie expires a cookie that setCookie wrote.
func (s *Server) clearCookie(c fiber.Ctx, name string) {
	s.setCookie(c, name, "", time.Now().Add(-1*time.Hour))
}
