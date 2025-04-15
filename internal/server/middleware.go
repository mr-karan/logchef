package server

import (
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"errors"

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

// requireAuth is middleware that ensures the request includes a valid, non-expired session cookie.
// It validates the session using core logic, retrieves the associated user, and stores
// both the user and session information in the request context (c.Locals) for subsequent handlers.
func (s *Server) requireAuth(c *fiber.Ctx) error {
	// Retrieve session ID from cookie.
	sessionIDStr := c.Cookies(sessionCookieName)
	if sessionIDStr == "" {
		return SendErrorWithType(c, fiber.StatusForbidden, "Authentication required", models.AuthenticationErrorType)
	}
	sessionID := models.SessionID(sessionIDStr)

	// Validate the session exists and is not expired.
	session, err := core.ValidateSession(c.Context(), s.sqlite, s.log, sessionID)
	if err != nil {
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

	// Store user and session in request context for downstream handlers.
	c.Locals("user", user)
	c.Locals("session", session)

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

	s.log.Debug("requireAdmin check", "user_id", user.ID, "user_role", user.Role)
	if user.Role != models.UserRoleAdmin {
		s.log.Debug("Admin access denied", "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusForbidden, "Admin access required", models.AuthorizationErrorType)
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

	s.log.Debug("requireTeamMember check", "user_id", user.ID, "user_role", user.Role, "team_id", teamIDStr)

	// Global admins bypass specific team membership checks.
	if user.Role == models.UserRoleAdmin {
		s.log.Debug("Global admin granting team member access", "user_id", user.ID, "team_id", teamIDStr)
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

// notFoundHandler returns a standardized 404 Not Found error for API routes.
func (s *Server) notFoundHandler(c *fiber.Ctx) error {
	return SendErrorWithType(c, fiber.StatusNotFound, "API route not found", models.NotFoundErrorType)
}
