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

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/auth"

	"github.com/gofiber/fiber/v2"
)

// generateState generates a random state string for OIDC
func generateState() (string, error) {
	// Generate 32 bytes of random data for state
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}

	// Generate 32 bytes for nonce
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Combine state and nonce with timestamp
	timestamp := time.Now().Unix()
	combined := fmt.Sprintf("%s.%s.%d",
		base64.RawURLEncoding.EncodeToString(stateBytes),
		base64.RawURLEncoding.EncodeToString(nonceBytes),
		timestamp,
	)

	return combined, nil
}

// validateState validates the state parameter from OIDC callback
func validateState(state, storedState string) error {
	if state == "" || storedState == "" {
		return errors.New("missing state parameter")
	}

	// Split stored state into components
	parts := strings.Split(storedState, ".")
	if len(parts) != 3 {
		return errors.New("invalid state format")
	}

	// Extract timestamp
	timestamp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp in state: %w", err)
	}

	// Check if state has expired (5 minutes)
	if time.Since(time.Unix(timestamp, 0)) > 5*time.Minute {
		return errors.New("state has expired")
	}

	// Compare full state strings
	if !secureCompare(state, storedState) {
		return errors.New("state mismatch")
	}

	return nil
}

// secureCompare performs a constant-time comparison of two strings
func secureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// redirectToFrontend redirects to the frontend URL with optional error message
func (s *Server) redirectToFrontend(c *fiber.Ctx, path string, err error) error {
	// Build URL with query params if error exists
	if err != nil {
		var errCode string
		switch {
		case errors.Is(err, auth.ErrSessionNotFound):
			errCode = "session_not_found"
		case errors.Is(err, auth.ErrSessionExpired):
			errCode = "session_expired"
		case errors.Is(err, auth.ErrOIDCInvalidToken):
			errCode = "invalid_token"
		case errors.Is(err, auth.ErrOIDCEmailNotVerified):
			errCode = "email_not_verified"
		default:
			errCode = "auth_error"
		}

		path = fmt.Sprintf("/auth/login?error=%s", errCode)
		s.log.Warn("redirecting to frontend login page with error",
			"error_code", errCode,
			"error_message", err.Error(),
			"path", path,
		)
	}

	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Use configured frontend URL if set
	if s.config.Server.FrontendURL != "" {
		s.log.Debug("redirecting to frontend",
			"frontend_url", s.config.Server.FrontendURL,
			"path", path,
		)
		return c.Redirect(s.config.Server.FrontendURL+path, fiber.StatusTemporaryRedirect)
	}

	return c.Redirect(path, fiber.StatusTemporaryRedirect)
}

// handleLogin initiates the OIDC login flow
func (s *Server) handleLogin(c *fiber.Ctx) error {
	// Generate secure state parameter
	state, err := generateState()
	if err != nil {
		s.log.Error("failed to generate state",
			"error", err,
		)
		return s.redirectToFrontend(c, "", fmt.Errorf("%w: %s", auth.ErrOIDCInvalidToken, err.Error()))
	}

	// Store state in secure cookie
	cookie := &fiber.Cookie{
		Name:     "auth_state",
		Value:    state,
		Expires:  time.Now().Add(5 * time.Minute),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "lax",
		Path:     "/",
	}
	c.Cookie(cookie)

	s.log.Debug("initiating OIDC login",
		"state_length", len(state),
	)

	// Get auth URL and redirect to OIDC provider
	authURL := s.auth.GetAuthURL(state)
	return c.Redirect(authURL, fiber.StatusTemporaryRedirect)
}

// handleCallback handles the OIDC callback
func (s *Server) handleCallback(c *fiber.Ctx) error {
	// Get state and code from query params
	state := c.Query("state")
	code := c.Query("code")

	if code == "" {
		s.log.Warn("missing code parameter in callback")
		return s.redirectToFrontend(c, "", fmt.Errorf("%w: missing code parameter", auth.ErrOIDCInvalidToken))
	}

	// Get stored state from cookie
	storedState := c.Cookies("auth_state")

	// Validate state
	if err := validateState(state, storedState); err != nil {
		s.log.Warn("state validation failed",
			"error", err,
			"state_length", len(state),
			"stored_state_length", len(storedState),
		)
		return s.redirectToFrontend(c, "", fmt.Errorf("%w: %s", auth.ErrOIDCInvalidToken, err.Error()))
	}

	// Delete state cookie immediately
	c.Cookie(&fiber.Cookie{
		Name:     "auth_state",
		Value:    "",
		Expires:  time.Now().Add(-24 * time.Hour),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "lax",
		Path:     "/",
	})

	// Handle callback
	_, session, err := s.auth.HandleCallback(c.Context(), code, state)
	if err != nil {
		s.log.Error("failed to handle auth callback",
			"error", err,
		)
		return s.redirectToFrontend(c, "", err)
	}

	// Set session cookie
	c.Cookie(&fiber.Cookie{
		Name:     "session_id",
		Value:    string(session.ID), // Use UUID string directly
		Expires:  session.ExpiresAt,
		HTTPOnly: true,
		Secure:   true,
		SameSite: "lax",
		Path:     "/",
	})

	// Get frontend redirect path from query param, default to /logs/explore if not provided
	redirectPath := c.Query("redirect", "/logs/explore")

	// Redirect to frontend on success
	return s.redirectToFrontend(c, redirectPath, nil)
}

// handleLogout logs out the user
func (s *Server) handleLogout(c *fiber.Ctx) error {
	// Get session ID from cookie
	sessionIDStr := c.Cookies("session_id")
	if sessionIDStr != "" {
		// Revoke session
		sessionID := models.SessionID(sessionIDStr)
		if err := s.auth.RevokeSession(c.Context(), sessionID); err != nil {
			s.log.Error("failed to revoke session", "error", err)
		}
	}

	// Delete session cookie
	c.Cookie(&fiber.Cookie{
		Name:     "session_id",
		Value:    "",
		Expires:  time.Now().Add(-24 * time.Hour),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "lax",
		Path:     "/",
	})

	return SendSuccess(c, fiber.StatusOK, nil)
}

// handleGetSession returns the current session info
func (s *Server) handleGetSession(c *fiber.Ctx) error {
	// Get session ID from cookie
	sessionIDStr := c.Cookies("session_id")
	if sessionIDStr == "" {
		return SendError(c, fiber.StatusUnauthorized, fiber.Map{
			"error": "No session found",
			"code":  "session_not_found",
		})
	}

	// Validate session
	sessionID := models.SessionID(sessionIDStr)
	session, err := s.auth.ValidateSession(c.Context(), sessionID)
	if err != nil {
		var statusCode int
		var errorCode string

		if errors.Is(err, auth.ErrSessionNotFound) || errors.Is(err, auth.ErrSessionExpired) {
			statusCode = fiber.StatusUnauthorized
			errorCode = "session_invalid"

			// Clear the invalid session cookie
			c.Cookie(&fiber.Cookie{
				Name:     "session_id",
				Value:    "",
				Expires:  time.Now().Add(-24 * time.Hour),
				HTTPOnly: true,
				Secure:   true,
				SameSite: "lax",
				Path:     "/",
			})
		} else {
			statusCode = fiber.StatusInternalServerError
			errorCode = "internal_error"
		}

		return SendError(c, statusCode, fiber.Map{
			"error": err.Error(),
			"code":  errorCode,
		})
	}

	// Get user info
	user, err := s.auth.GetUser(c.Context(), session.UserID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to get user information")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"user":    user,
		"session": session,
	})
}
