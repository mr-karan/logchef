package server

import (
	"errors"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	// "github.com/mr-karan/logchef/internal/identity" // Removed

	"github.com/gofiber/fiber/v2"
)

// --- Admin User Management Handlers ---

// handleListUsers lists all users in the system.
// URL: GET /api/v1/admin/users
// Requires: Admin privileges (requireAdmin middleware)
func (s *Server) handleListUsers(c *fiber.Ctx) error {
	users, err := core.ListUsers(c.Context(), s.sqlite)
	if err != nil {
		s.log.Error("failed to list users", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error listing users")
	}
	return SendSuccess(c, fiber.StatusOK, users)
}

// handleGetUser gets a specific user by ID.
// URL: GET /api/v1/admin/users/:userID
// Requires: Admin privileges (requireAdmin middleware)
func (s *Server) handleGetUser(c *fiber.Ctx) error {
	userIDStr := c.Params("userID")
	if userIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "User ID is required")
	}

	userID, err := core.ParseUserID(userIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid user ID format")
	}

	user, err := core.GetUser(c.Context(), s.sqlite, userID)
	if err != nil {
		if errors.Is(err, core.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "User not found")
		}
		s.log.Error("failed to get user", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error getting user")
	}

	return SendSuccess(c, fiber.StatusOK, user)
}

// handleCreateUser creates a new user in the system.
// URL: POST /api/v1/admin/users
// Requires: Admin privileges (requireAdmin middleware)
func (s *Server) handleCreateUser(c *fiber.Ctx) error {
	var req struct {
		Email    string `json:"email"`
		FullName string `json:"full_name"`
		Role     string `json:"role"`
		Status   string `json:"status"`
	}

	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Convert string values to proper enum types
	role := models.UserRole(req.Role)
	if role == "" {
		role = models.UserRoleMember // Default role
	}

	status := models.UserStatus(req.Status)
	if status == "" {
		status = models.UserStatusActive // Default status
	}

	user, err := core.CreateUser(c.Context(), s.sqlite, s.log, req.Email, req.FullName, role, status)
	if err != nil {
		// Handle specific error types from core
		if errors.Is(err, core.ErrUserAlreadyExists) {
			return SendError(c, fiber.StatusConflict, err.Error())
		}
		if valErr, ok := err.(*core.ValidationError); ok {
			return SendError(c, fiber.StatusBadRequest, valErr.Error())
		}

		s.log.Error("failed to create user", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error creating user")
	}

	return SendSuccess(c, fiber.StatusCreated, user)
}

// handleUpdateUser updates an existing user.
// URL: PUT /api/v1/admin/users/:userID
// Requires: Admin privileges (requireAdmin middleware)
func (s *Server) handleUpdateUser(c *fiber.Ctx) error {
	userIDStr := c.Params("userID")
	if userIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "User ID is required")
	}

	userID, err := core.ParseUserID(userIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid user ID format")
	}

	var req struct {
		Email    *string `json:"email"`
		FullName *string `json:"full_name"`
		Role     *string `json:"role"`
		Status   *string `json:"status"`
	}

	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Construct update DTO
	updateData := models.User{}
	if req.Email != nil {
		updateData.Email = *req.Email
	}
	if req.FullName != nil {
		updateData.FullName = *req.FullName
	}
	if req.Role != nil {
		updateData.Role = models.UserRole(*req.Role)
	}
	if req.Status != nil {
		updateData.Status = models.UserStatus(*req.Status)
	}

	if err := core.UpdateUser(c.Context(), s.sqlite, s.log, userID, updateData); err != nil {
		// Handle specific error types from core
		if errors.Is(err, core.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "User not found")
		}
		if errors.Is(err, core.ErrUserAlreadyExists) {
			return SendError(c, fiber.StatusConflict, err.Error())
		}
		if valErr, ok := err.(*core.ValidationError); ok {
			return SendError(c, fiber.StatusBadRequest, valErr.Error())
		}

		s.log.Error("failed to update user", "error", err, "user_id", userID)
		return SendError(c, fiber.StatusInternalServerError, "Error updating user")
	}

	// Fetch updated user
	updatedUser, err := core.GetUser(c.Context(), s.sqlite, userID)
	if err != nil {
		s.log.Error("failed to get updated user", "error", err, "user_id", userID)
		return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "User updated successfully, but failed to fetch result"})
	}

	return SendSuccess(c, fiber.StatusOK, updatedUser)
}

// handleDeleteUser deletes a user.
// URL: DELETE /api/v1/admin/users/:userID
// Requires: Admin privileges (requireAdmin middleware)
func (s *Server) handleDeleteUser(c *fiber.Ctx) error {
	userIDStr := c.Params("userID")
	if userIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "User ID is required")
	}

	userID, err := core.ParseUserID(userIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid user ID format")
	}

	if err := core.DeleteUser(c.Context(), s.sqlite, s.log, userID); err != nil {
		if errors.Is(err, core.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "User not found")
		}
		s.log.Error("failed to delete user", "error", err, "user_id", userID)
		return SendError(c, fiber.StatusInternalServerError, "Error deleting user")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "User deleted successfully"})
}

// --- Current User Team Handlers ---

// handleListCurrentUserTeams lists teams that the authenticated user belongs to.
// URL: GET /api/v1/me/teams
// Requires: User authentication (requireAuth middleware)
func (s *Server) handleListCurrentUserTeams(c *fiber.Ctx) error {
	// User should be in context from auth middleware
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	// Get teams user belongs to
	teams, err := core.ListTeamsForUser(c.Context(), s.sqlite, user.ID)
	if err != nil {
		s.log.Error("failed to list teams for user", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Error listing user teams")
	}

	// Enhanced response with additional info for each team
	type TeamResponse struct {
		*models.Team
		MemberCount int  `json:"member_count"`
		IsAdmin     bool `json:"is_admin"`
	}

	teamResponses := make([]TeamResponse, 0, len(teams))
	for _, team := range teams {
		// Get member count
		members, err := core.ListTeamMembers(c.Context(), s.sqlite, team.ID)
		if err != nil {
			s.log.Warn("failed to get member count for team", "error", err, "team_id", team.ID)
			continue
		}

		// Check if user is admin of this team
		isAdmin, err := core.IsTeamAdmin(c.Context(), s.sqlite, team.ID, user.ID)
		if err != nil {
			s.log.Warn("failed to check if user is team admin", "error", err, "team_id", team.ID, "user_id", user.ID)
			continue
		}

		teamResponses = append(teamResponses, TeamResponse{
			Team:        team,
			MemberCount: len(members),
			IsAdmin:     isAdmin,
		})
	}

	return SendSuccess(c, fiber.StatusOK, teamResponses)
}
