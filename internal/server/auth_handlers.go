package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

const (
	// Cookie names used for authentication state and session.
	stateCookieName   = "auth_state"
	sessionCookieName = "session_id"
	// stateCookieTTL defines how long the OIDC state cookie is valid.
	stateCookieTTL = 5 * time.Minute
)

// Define Auth/OIDC specific errors used within handlers, especially for redirect mapping.
var (
	// Note: Some errors might overlap with core definitions but are kept here
	// for mapping purposes within redirectToFrontend.
	ErrSessionNotFound           = errors.New("session not found")
	ErrSessionExpired            = errors.New("session expired")
	ErrUserNotFound              = errors.New("user not found")
	ErrTeamNotFound              = errors.New("team not found")
	ErrUnauthorizedUser          = errors.New("unauthorized user")
	ErrUserInactive              = errors.New("user inactive")
	ErrOIDCProviderNotConfigured = errors.New("OIDC provider not configured") // This one might belong in auth/oidc.go
	ErrOIDCInvalidToken          = errors.New("invalid OIDC token")
	ErrOIDCEmailNotVerified      = errors.New("email not verified")
)

// generateState generates a cryptographically secure random string used for the OIDC state parameter.
// It includes a nonce and timestamp to prevent replay attacks and ensure freshness.
func generateState() (string, error) {
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("failed to generate random state bytes: %w", err)
	}
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("failed to generate nonce bytes: %w", err)
	}

	// Combine state, nonce, and timestamp for validation.
	timestamp := time.Now().Unix()
	combined := fmt.Sprintf("%s.%s.%d",
		base64.RawURLEncoding.EncodeToString(stateBytes),
		base64.RawURLEncoding.EncodeToString(nonceBytes),
		timestamp,
	)
	return combined, nil
}

// validateState checks the OIDC callback state against the stored state cookie.
// It verifies the format, expiration (based on timestamp), and performs a constant-time comparison.
func validateState(state, storedState string) error {
	if state == "" || storedState == "" {
		return errors.New("state validation failed: missing state parameter")
	}

	// Basic format check.
	parts := strings.Split(storedState, ".")
	if len(parts) != 3 {
		return errors.New("state validation failed: invalid state format")
	}

	// Check timestamp for expiration.
	timestamp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return fmt.Errorf("state validation failed: invalid timestamp in state: %w", err)
	}
	if time.Since(time.Unix(timestamp, 0)) > stateCookieTTL {
		return errors.New("state validation failed: state has expired")
	}

	// Constant-time comparison to mitigate timing attacks.
	if !secureCompare(state, storedState) {
		return errors.New("state validation failed: state mismatch")
	}

	return nil
}

// secureCompare performs a constant-time comparison of two strings.
func secureCompare(a, b string) bool {
	// Use crypto/subtle for timing attack resistance.
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// redirectToFrontend redirects the user's browser to the configured frontend URL,
// optionally appending an error code as a query parameter if an error occurred.
func (s *Server) redirectToFrontend(c *fiber.Ctx, path string, err error) error {
	targetURL := s.config.Server.FrontendURL
	if targetURL == "" {
		targetURL = "/" // Default to root if no frontend URL is configured.
	}

	// Ensure the base URL doesn't end with a slash and the path starts with one.
	targetURL = strings.TrimSuffix(targetURL, "/")
	if path == "" {
		path = "/" // Default path if none provided.
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	finalURL := targetURL + path

	// If an error occurred, map it to a code and append to the redirect URL.
	if err != nil {
		var errCode string
		// Map known auth errors to specific codes for the frontend.
		switch {
		// Use errors defined in this file first for mapping
		case errors.Is(err, ErrSessionNotFound), errors.Is(err, core.ErrSessionNotFound):
			errCode = "session_not_found"
		case errors.Is(err, ErrSessionExpired), errors.Is(err, core.ErrSessionExpired):
			errCode = "session_expired"
		case errors.Is(err, ErrOIDCInvalidToken):
			errCode = "invalid_token"
		case errors.Is(err, ErrOIDCEmailNotVerified):
			errCode = "email_not_verified"
		case errors.Is(err, ErrUnauthorizedUser):
			errCode = "unauthorized"
		case errors.Is(err, ErrUserInactive):
			errCode = "user_inactive"
		// Add mappings for other relevant core errors if needed
		case errors.Is(err, core.ErrUserNotFound):
			errCode = "user_not_found" // Example mapping
		default:
			errCode = "auth_error" // Generic code for other errors.
		}
		finalURL = fmt.Sprintf("%s/auth/login?error=%s", targetURL, errCode)
		s.log.Warn("authentication error during redirect",
			"error_code", errCode,
			"original_error", err.Error(),
			"redirect_url", finalURL,
		)
	}

	s.log.Debug("redirecting to frontend", "url", finalURL)
	return c.Redirect(finalURL, fiber.StatusTemporaryRedirect)
}

// handleLogin initiates the OIDC authentication flow.
// It generates a state parameter, stores it in a cookie, and redirects the user
// to the OIDC provider's authorization endpoint.
func (s *Server) handleLogin(c *fiber.Ctx) error {
	if s.oidcProvider == nil {
		s.log.Error("OIDC provider not configured, cannot initiate login")
		return SendError(c, fiber.StatusInternalServerError, "Authentication provider not configured")
	}

	state, err := generateState()
	if err != nil {
		s.log.Error("failed to generate OIDC state", "error", err)
		return s.redirectToFrontend(c, "", fmt.Errorf("state generation failed: %w", err))
	}

	// Store state in a short-lived, secure, HTTP-only cookie.
	c.Cookie(&fiber.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Expires:  time.Now().Add(stateCookieTTL),
		HTTPOnly: true,
		Secure:   true,                        // Assumes HTTPS
		SameSite: fiber.CookieSameSiteLaxMode, // Correct constant
		Path:     "/",
	})

	s.log.Debug("initiating OIDC login flow")
	authURL := s.oidcProvider.GetAuthURL(state)
	return c.Redirect(authURL, fiber.StatusTemporaryRedirect)
}

// handleCallback handles the redirect back from the OIDC provider.
// It validates the state, exchanges the code for tokens, verifies the ID token,
// processes the user login (checking existence, status), creates a session,
// sets the session cookie, and redirects the user back to the frontend.
func (s *Server) handleCallback(c *fiber.Ctx) error {
	if s.oidcProvider == nil {
		s.log.Error("OIDC provider not configured, cannot handle callback")
		return SendError(c, fiber.StatusInternalServerError, "Authentication provider not configured")
	}

	state := c.Query("state")
	code := c.Query("code")

	if code == "" {
		s.log.Warn("OIDC callback missing 'code' parameter")
		return s.redirectToFrontend(c, "", fmt.Errorf("authorization code missing"))
	}

	// Validate state against the value stored in the cookie.
	storedState := c.Cookies(stateCookieName)
	if err := validateState(state, storedState); err != nil {
		s.log.Warn("OIDC state validation failed", "error", err)
		return s.redirectToFrontend(c, "", fmt.Errorf("state validation failed: %w", err))
	}

	// State is validated, clear the cookie immediately.
	c.Cookie(&fiber.Cookie{Name: stateCookieName, Expires: time.Now().Add(-1 * time.Hour), HTTPOnly: true, Secure: true, SameSite: fiber.CookieSameSiteLaxMode, Path: "/"}) // Correct constant

	// Process the OIDC callback using the provider and core functions.
	_, session, err := s.oidcProvider.HandleCallback(c.Context(), s.sqlite, s.log, &s.config.Auth, code, state)
	if err != nil {
		// HandleCallback logs internal errors; map to frontend redirect error.
		s.log.Error("OIDC callback handling failed", "error", err)
		return s.redirectToFrontend(c, "", err)
	}

	// Set the application session cookie.
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Value:    string(session.ID),
		Expires:  session.ExpiresAt,
		HTTPOnly: true,
		Secure:   true,                        // Assumes HTTPS
		SameSite: fiber.CookieSameSiteLaxMode, // Correct constant
		Path:     "/",
	})

	// Redirect back to the frontend, possibly to a specific path requested before login.
	redirectPath := c.Query("redirect", "/logs/explore") // Default redirect
	return s.redirectToFrontend(c, redirectPath, nil)
}

// handleLogout handles user logout requests.
// It revokes the current session (if found) and clears the session cookie.
func (s *Server) handleLogout(c *fiber.Ctx) error {
	sessionIDStr := c.Cookies(sessionCookieName)
	if sessionIDStr != "" {
		// Attempt to revoke the session in the database.
		if err := core.RevokeSession(c.Context(), s.sqlite, s.log, models.SessionID(sessionIDStr)); err != nil {
			// Log error but proceed with cookie deletion anyway.
			s.log.Error("failed to revoke session during logout", "error", err, "session_id_prefix", sessionIDStr[:min(len(sessionIDStr), 8)])
		}
	}

	// Clear the session cookie in the browser.
	c.Cookie(&fiber.Cookie{Name: sessionCookieName, Value: "", Expires: time.Now().Add(-1 * time.Hour), HTTPOnly: true, Secure: true, SameSite: fiber.CookieSameSiteLaxMode, Path: "/"})

	return SendSuccess(c, fiber.StatusOK, nil) // Send simple success response.
}

// handleGetCurrentUser retrieves information about the currently authenticated user and their session.
// It relies on the requireAuth middleware to populate user and session details in the context.
func (s *Server) handleGetCurrentUser(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	session := c.Locals("session").(*models.Session)

	if user == nil || session == nil {
		s.log.Error("user or session missing from context in handleGetCurrentUser")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Session data unavailable", models.AuthenticationErrorType)
	}

	// Return relevant user and session info (e.g., exclude sensitive session ID).
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"user": user,
		"session": fiber.Map{
			"expires_at": session.ExpiresAt,
		},
	})
}

// min is a helper, consider moving to a utility package if used elsewhere.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
