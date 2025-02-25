package server

import (
	"logchef/pkg/models"

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
	if user.Role != "admin" {
		return SendError(c, fiber.StatusForbidden, fiber.Map{
			"error": "Admin access required",
			"code":  "insufficient_permissions",
		})
	}
	return c.Next()
}

// notFoundHandler handles 404 for API routes
func (s *Server) notFoundHandler(c *fiber.Ctx) error {
	return SendError(c, fiber.StatusNotFound, "API route not found")
}
