package server

import (
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// requireAuth middleware ensures the request is authenticated
func (s *Server) requireAuth(c *fiber.Ctx) error {
	// Get session ID from cookie
	sessionIDStr := c.Cookies("session_id")
	if sessionIDStr == "" {
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": "Authentication required",
			"code":  "session_not_found",
		})
	}

	// Validate session
	sessionID := models.SessionID(sessionIDStr)
	session, err := s.auth.ValidateSession(c.Context(), sessionID)
	if err != nil {
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": err.Error(),
			"code":  "session_expired",
		})
	}

	// Get user info
	user, err := s.auth.GetUser(c.Context(), session.UserID)
	if err != nil {
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": "User not found",
			"code":  "user_not_found",
		})
	}

	// Store user and session in context
	c.Locals("user", user)
	c.Locals("session", session)

	return c.Next()
}

// requireAdmin middleware ensures the user is an admin
func (s *Server) requireAdmin(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	// Log user role for debugging
	s.log.Debug("requireAdmin check", "user_id", user.ID, "user_role", user.Role)

	if user.Role != models.UserRoleAdmin {
		s.log.Debug("Admin access denied", "user_id", user.ID)
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": "Admin access required",
			"code":  "insufficient_permissions",
		})
	}

	return c.Next()
}

// requireTeamMember middleware ensures the user is a member of the requested team
func (s *Server) requireTeamMember(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	teamID := c.Params("teamID")

	// Log user role and team ID for debugging
	s.log.Debug("requireTeamMember check", "user_id", user.ID, "user_role", user.Role, "team_id", teamID)

	// Admin users bypass team membership check
	if user.Role == models.UserRoleAdmin {
		s.log.Debug("Global admin access granted", "user_id", user.ID)
		c.Locals("isGlobalAdmin", true)
		c.Locals("isTeamMember", true) // Global admins are considered team members for all teams
		return c.Next()
	}

	// Check if user is a team member
	isMember, err := s.identityService.IsTeamMember(c.Context(), teamID, user.ID)
	if err != nil {
		s.log.Error("Failed to verify team membership", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team membership")
	}

	// Store the membership status in context for handlers to use
	c.Locals("isGlobalAdmin", false)
	c.Locals("isTeamMember", isMember)

	if !isMember {
		s.log.Debug("Team membership denied", "user_id", user.ID, "team_id", teamID)
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": "Team membership required",
			"code":  "insufficient_permissions",
		})
	}

	return c.Next()
}

// requireTeamSourceAccess middleware ensures the team has access to the requested source
func (s *Server) requireTeamSourceAccess(c *fiber.Ctx) error {
	teamID := c.Params("teamID")
	sourceID := c.Params("sourceID")

	// Check if team has access to this source
	hasAccess, err := s.identityService.TeamHasSourceAccess(c.Context(), teamID, sourceID)
	if err != nil {
		s.log.Error("Failed to verify source access", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify source access")
	}

	if !hasAccess {
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": "Team does not have access to this source",
			"code":  "insufficient_permissions",
		})
	}

	return c.Next()
}

// requireTeamAdmin middleware ensures the user is an admin of the specified team
func (s *Server) requireTeamAdmin(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	teamID := c.Params("teamID")

	// Log user role and team ID for debugging
	s.log.Debug("requireTeamAdmin check", "user_id", user.ID, "user_role", user.Role, "team_id", teamID)

	// System admins bypass team admin check
	if user.Role == models.UserRoleAdmin {
		s.log.Debug("Global admin access granted", "user_id", user.ID)
		return c.Next()
	}

	// Check if user is a team admin
	isAdmin, err := s.identityService.IsTeamAdmin(c.Context(), teamID, user.ID)
	if err != nil {
		s.log.Error("Failed to verify team admin status", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team admin status")
	}

	if !isAdmin {
		s.log.Debug("Team admin access denied", "user_id", user.ID, "team_id", teamID)
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": "Team admin access required",
			"code":  "insufficient_permissions",
		})
	}

	return c.Next()
}

// requireTeamAdminOrGlobalAdmin is a helper middleware that stores the team admin status in context
// This allows handlers to know if the user is a team admin or a global admin without duplicating checks
func (s *Server) requireTeamAdminOrGlobalAdmin(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	teamID := c.Params("teamID")

	// Log user role and team ID for debugging
	s.log.Debug("requireTeamAdminOrGlobalAdmin check", "user_id", user.ID, "user_role", user.Role, "team_id", teamID)

	// If user is a global admin, store that information and continue
	if user.Role == models.UserRoleAdmin {
		s.log.Debug("Global admin access granted", "user_id", user.ID)
		c.Locals("isGlobalAdmin", true)
		c.Locals("isTeamAdmin", true) // Global admins are considered team admins for all teams
		return c.Next()
	}

	// Otherwise, check if user is a team admin
	isAdmin, err := s.identityService.IsTeamAdmin(c.Context(), teamID, user.ID)
	if err != nil {
		s.log.Error("Failed to verify team admin status", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team admin status")
	}

	// Store the admin status in context for handlers to use
	c.Locals("isGlobalAdmin", false)
	c.Locals("isTeamAdmin", isAdmin)

	if !isAdmin {
		s.log.Debug("Team admin access denied", "user_id", user.ID, "team_id", teamID)
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": "Team admin access required",
			"code":  "insufficient_permissions",
		})
	}

	return c.Next()
}

// notFoundHandler handles 404 for API routes
func (s *Server) notFoundHandler(c *fiber.Ctx) error {
	return SendError(c, fiber.StatusNotFound, "API route not found")
}
