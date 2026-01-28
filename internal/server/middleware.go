package server

import (
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"

	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// getUserIDFromContext extracts the user ID from the context
func getUserIDFromContext(c *fiber.Ctx) models.UserID {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return 0
	}
	return user.ID
}

// isUserAdmin checks if the user in context has admin role
func isUserAdmin(c *fiber.Ctx) bool {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return false
	}
	return user.Role == models.UserRoleAdmin
}

// requireAuth is middleware that ensures the request includes valid authentication.
// It supports both API token authentication (Authorization: Bearer <token>) and
// session-based authentication (session cookie). It validates the authentication,
// retrieves the associated user, and stores the user information in the request
// context (c.Locals) for subsequent handlers.
func (s *Server) requireAuth(c *fiber.Ctx) error {
	// Try API token authentication first
	authHeader := c.Get("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		return s.authenticateWithToken(c, authHeader)
	}

	// Fall back to session-based authentication
	return s.authenticateWithSession(c)
}

// authenticateWithToken handles API token authentication
func (s *Server) authenticateWithToken(c *fiber.Ctx, authHeader string) error {
	// Extract token from "Bearer <token>"
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader || token == "" {
		metrics.RecordAuthAttempt("token", false, nil)
		return SendErrorWithType(c, fiber.StatusForbidden, "Invalid Authorization header format", models.AuthenticationErrorType)
	}

	// Authenticate token and get associated user
	user, apiToken, err := core.AuthenticateAPIToken(c.Context(), s.sqlite, s.log, &s.config.Auth, token)
	if err != nil {
		metrics.RecordAuthAttempt("token", false, nil)

		// Handle specific token errors
		if errors.Is(err, core.ErrInvalidToken) || errors.Is(err, core.ErrTokenExpired) {
			return SendErrorWithType(c, fiber.StatusForbidden, "Invalid or expired token", models.AuthenticationErrorType)
		}

		s.log.Error("error authenticating API token", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error validating token", models.GeneralErrorType)
	}

	// Record successful token authentication with user context
	metrics.RecordAuthAttempt("token", true, user)

	// Store user and token info in request context
	c.Locals("user", user)
	c.Locals("api_token", apiToken)
	c.Locals("auth_method", "token")

	return c.Next()
}

// authenticateWithSession handles session-based authentication (existing logic)
func (s *Server) authenticateWithSession(c *fiber.Ctx) error {
	// Retrieve session ID from cookie.
	sessionIDStr := c.Cookies(sessionCookieName)
	if sessionIDStr == "" {
		metrics.RecordSessionOperation("validate", false, nil)
		return SendErrorWithType(c, fiber.StatusForbidden, "Authentication required", models.AuthenticationErrorType)
	}
	sessionID := models.SessionID(sessionIDStr)

	// Validate the session exists and is not expired.
	session, err := core.ValidateSession(c.Context(), s.sqlite, s.log, sessionID)
	if err != nil {
		metrics.RecordSessionOperation("validate", false, nil)

		// Handle specific session errors by returning 403.
		if errors.Is(err, core.ErrSessionNotFound) || errors.Is(err, core.ErrSessionExpired) {
			return SendErrorWithType(c, fiber.StatusForbidden, err.Error(), models.AuthenticationErrorType)
		}
		// Log unexpected errors during validation.
		s.log.Error("error validating session via core function", "error", err, "session_id", sessionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error validating session", models.GeneralErrorType)
	}

	// Retrieve associated user information.
	user, err := core.GetUser(c.Context(), s.sqlite, session.UserID)
	if err != nil {
		// If user not found for a valid session, treat as an auth issue or internal error.
		if errors.Is(err, core.ErrUserNotFound) {
			return SendErrorWithType(c, fiber.StatusForbidden, "User associated with session not found", models.AuthenticationErrorType)
		}
		s.log.Error("error getting user for session via core function", "error", err, "user_id", session.UserID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error retrieving user data", models.GeneralErrorType)
	}

	// Record successful session validation with user context
	metrics.RecordSessionOperation("validate", true, user)

	// Store user and session in request context for downstream handlers.
	c.Locals("user", user)
	c.Locals("session", session)
	c.Locals("auth_method", "session")

	return c.Next()
}

// requireAdmin is middleware that ensures the authenticated user has the global 'admin' role.
// It assumes requireAuth has already run and placed the user in the context.
func (s *Server) requireAdmin(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context for admin check")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}

	if user.Role != models.UserRoleAdmin {

		// Record authorization failure
		metrics.RecordAuthorizationFailure(c.Route().Path, user, "insufficient_role")

		return SendErrorWithType(c, fiber.StatusForbidden, "Admin access required", models.AuthorizationErrorType)
	}

	return c.Next()
}

// requireAnyTeamAdmin is middleware that ensures the authenticated user is an admin of at least one team,
// or is a global admin. This is used for endpoints that should be accessible to team admins
// without requiring a specific team context (e.g., listing users to add to teams).
// It assumes requireAuth has already run.
func (s *Server) requireAnyTeamAdmin(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context for any team admin check")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}

	// Global admins bypass specific team admin checks.
	if user.Role == models.UserRoleAdmin {
		return c.Next()
	}

	// Check if the user is an admin of any team.
	isAnyAdmin, err := core.IsAnyTeamAdmin(c.Context(), s.sqlite, user.ID)
	if err != nil {
		s.log.Error("failed to check if user is any team admin", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team admin status")
	}

	if !isAnyAdmin {
		s.log.Warn("User is not an admin of any team", "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusForbidden, "Team admin privileges required", models.AuthorizationErrorType)
	}

	return c.Next()
}

// requireTeamMember is middleware that ensures the authenticated user is a member of the team
// specified by the ':teamID' path parameter, or is a global admin.
// It assumes requireAuth has already run.
func (s *Server) requireTeamMember(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context for team member check")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}
	teamIDStr := c.Params("teamID")

	// Global admins bypass specific team membership checks.
	if user.Role == models.UserRoleAdmin {
		c.Locals("isGlobalAdmin", true)
		c.Locals("isTeamMember", true)
		return c.Next()
	}

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}

	// Check membership using core function.
	isMember, err := core.IsTeamMember(c.Context(), s.sqlite, teamID, user.ID)
	if err != nil {
		s.log.Error("failed to verify team membership", "error", err, "team_id", teamID, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team membership")
	}

	// Store status for potential use in handlers (though check ensures access).
	c.Locals("isGlobalAdmin", false)
	c.Locals("isTeamMember", isMember)

	if !isMember {
		s.log.Warn("Team membership denied", "user_id", user.ID, "team_id", teamID)
		return SendErrorWithType(c, fiber.StatusForbidden, "Team membership required", models.AuthorizationErrorType)
	}

	return c.Next()
}

// requireTeamAdminOrGlobalAdmin checks if a user is either an admin of the requested team or a global admin
func (s *Server) requireTeamAdminOrGlobalAdmin(c *fiber.Ctx) error {
	userID := getUserIDFromContext(c)
	if userID == 0 {
		return SendError(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	// Get the team ID from the request parameters
	teamIDStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	// Check if the user is a global admin
	if isUserAdmin(c) {
		return c.Next() // Allow global admins unconditionally
	}

	// Check if the user is a team admin
	isTeamAdmin, err := core.IsTeamAdmin(c.Context(), s.sqlite, teamID, userID)
	if err != nil {
		s.log.Error("Error checking team admin status", "error", err, "team_id", teamID, "user_id", userID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team admin status")
	}

	if !isTeamAdmin {
		s.log.Warn("User is not a team admin", "team_id", teamID, "user_id", userID)
		return SendError(c, fiber.StatusForbidden, "Admin team privileges required")
	}

	// User is a team admin, continue with the request
	return c.Next()
}

// requireTeamHasSource is a middleware that verifies if the requested team has access to the specified source.
// This must be used after requireTeamMember to ensure team membership is already verified.
func (s *Server) requireTeamHasSource(c *fiber.Ctx) error {
	// Extract path parameters
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")

	// Parse IDs
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid source ID: "+err.Error())
	}

	// Check if the team has access to the source
	hasAccess, err := core.TeamHasSourceAccess(c.Context(), s.sqlite, teamID, sourceID)
	if err != nil {
		s.log.Error("Error checking team-source access", "error", err, "team_id", teamID, "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team source access")
	}

	if !hasAccess {
		s.log.Warn("Team does not have access to source", "team_id", teamID, "source_id", sourceID)
		return SendError(c, fiber.StatusForbidden, "Team does not have access to this source")
	}

	// Team has access to the source, continue with the request
	return c.Next()
}

// requireCollectionManagement checks if a user has privileges to manage collections for the requested team.
// This includes Team Editors, Team Admins, or Global Admins.
// Assumes requireAuth has already run.
func (s *Server) requireCollectionManagement(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context for collection management check")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}

	teamIDStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}

	// Global admins bypass specific team role checks.
	if user.Role == models.UserRoleAdmin {
		return c.Next()
	}

	// Fetch the user's specific role within this team using the core function.
	teamMember, err := core.GetTeamMember(c.Context(), s.sqlite, teamID, user.ID)
	if err != nil {
		// This error means something unexpected happened during DB interaction, not "not found".
		s.log.Error("failed to get team member details for collection management check", "error", err, "team_id", teamID, "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify team role", models.GeneralErrorType)
	}

	if teamMember == nil {
		// core.GetTeamMember returns (nil, nil) if the user is not found in the team.
		s.log.Warn("User not a member of the team for collection management check", "user_id", user.ID, "team_id", teamID)
		return SendErrorWithType(c, fiber.StatusForbidden, "Team membership required for this action", models.AuthorizationErrorType)
	}

	// Check if the team member is an Admin or Editor.
	if teamMember.Role == models.TeamRoleAdmin || teamMember.Role == models.TeamRoleEditor {
		return c.Next()
	}

	s.log.Warn("Collection management privileges required, but user has role", "user_id", user.ID, "team_id", teamID, "team_role", teamMember.Role)
	return SendErrorWithType(c, fiber.StatusForbidden, "Collection management privileges required", models.AuthorizationErrorType)
}

// notFoundHandler returns a standardized 404 Not Found error for API routes.
func (s *Server) notFoundHandler(c *fiber.Ctx) error {
	return SendErrorWithType(c, fiber.StatusNotFound, "API route not found", models.NotFoundErrorType)
}
