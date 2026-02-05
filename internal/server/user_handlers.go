package server

import (
	"errors"
	"strconv"
	"time"

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
		// Handle the specific error for deleting the last admin
		if errors.Is(err, core.ErrCannotDeleteLastAdmin) {
			return SendError(c, fiber.StatusBadRequest, core.ErrCannotDeleteLastAdmin.Error())
		}
		s.log.Error("failed to delete user", "error", err, "user_id", userID)
		return SendError(c, fiber.StatusInternalServerError, "Error deleting user")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "User deleted successfully"})
}

// --- Current User Team Handlers ---

// handleListCurrentUserTeams lists teams that the authenticated user belongs to,
// including their role in each team and the team's member count.
// URL: GET /api/v1/me/teams
// Requires: User authentication (requireAuth middleware)
func (s *Server) handleListCurrentUserTeams(c *fiber.Ctx) error {
	// User should be in context from auth middleware
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	// Get teams user belongs to, now with role and member count included
	userTeamDetails, err := core.ListTeamsForUser(c.Context(), s.sqlite, user.ID)
	if err != nil {
		s.log.Error("failed to list teams for user with details", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Error listing user teams")
	}

	if userTeamDetails == nil {
		// Return empty list if user has no teams, to be consistent.
		return SendSuccess(c, fiber.StatusOK, []*models.UserTeamDetails{})
	}

	return SendSuccess(c, fiber.StatusOK, userTeamDetails)
}

// --- API Token Management Handlers ---

// handleListAPITokens lists all API tokens for the authenticated user.
// URL: GET /api/v1/me/tokens
// Requires: User authentication (requireAuth middleware)
func (s *Server) handleListAPITokens(c *fiber.Ctx) error {
	// User should be in context from auth middleware
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	tokens, err := core.ListAPITokensForUser(c.Context(), s.sqlite, user.ID)
	if err != nil {
		s.log.Error("failed to list API tokens for user", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Error listing API tokens")
	}

	return SendSuccess(c, fiber.StatusOK, tokens)
}

// handleCreateAPIToken creates a new API token for the authenticated user.
// URL: POST /api/v1/me/tokens
// Requires: User authentication (requireAuth middleware)
func (s *Server) handleCreateAPIToken(c *fiber.Ctx) error {
	// User should be in context from auth middleware
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	var req struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}

	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	response, err := core.CreateAPIToken(c.Context(), s.sqlite, s.log, &s.config.Auth, user.ID, req.Name, req.ExpiresAt)
	if err != nil {
		// Handle specific error types from core
		if valErr, ok := err.(*core.ValidationError); ok {
			return SendError(c, fiber.StatusBadRequest, valErr.Error())
		}

		s.log.Error("failed to create API token", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Error creating API token")
	}

	return SendSuccess(c, fiber.StatusCreated, response)
}

// handleDeleteAPIToken deletes an API token owned by the authenticated user.
// URL: DELETE /api/v1/me/tokens/:tokenID
// Requires: User authentication (requireAuth middleware)
func (s *Server) handleDeleteAPIToken(c *fiber.Ctx) error {
	// User should be in context from auth middleware
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	tokenIDStr := c.Params("tokenID")
	if tokenIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Token ID is required")
	}

	tokenID, err := strconv.Atoi(tokenIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid token ID format")
	}

	if err := core.DeleteAPIToken(c.Context(), s.sqlite, s.log, user.ID, tokenID); err != nil {
		if errors.Is(err, core.ErrAPITokenNotFound) {
			return SendError(c, fiber.StatusNotFound, "API token not found")
		}

		s.log.Error("failed to delete API token", "error", err, "token_id", tokenID, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Error deleting API token")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "API token deleted successfully"})
}

// --- Current User Preferences Handlers ---

// UserPreferencesResponse represents user preferences in API responses.
type UserPreferencesResponse struct {
	Preferences models.UserPreferences `json:"preferences"`
	IsDefault   bool                   `json:"is_default"`
}

// handleGetUserPreferences retrieves preferences for the authenticated user.
// URL: GET /api/v1/me/preferences
// Requires: User authentication (requireAuth middleware)
func (s *Server) handleGetUserPreferences(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	preferences, isDefault, err := core.GetUserPreferences(c.Context(), s.sqlite, user.ID)
	if err != nil {
		s.log.Error("failed to get user preferences", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user preferences")
	}

	return SendSuccess(c, fiber.StatusOK, UserPreferencesResponse{
		Preferences: preferences,
		IsDefault:   isDefault,
	})
}

// handleUpdateUserPreferences updates preferences for the authenticated user.
// URL: PUT /api/v1/me/preferences
// Requires: User authentication (requireAuth middleware)
func (s *Server) handleUpdateUserPreferences(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context despite requireAuth middleware")
		return SendError(c, fiber.StatusInternalServerError, "Error retrieving user context")
	}

	var req models.UpdateUserPreferencesRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	preferences, err := core.UpdateUserPreferences(c.Context(), s.sqlite, user.ID, req)
	if err != nil {
		if valErr, ok := err.(*core.ValidationError); ok {
			return SendError(c, fiber.StatusBadRequest, valErr.Error())
		}

		s.log.Error("failed to update user preferences", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Error updating user preferences")
	}

	return SendSuccess(c, fiber.StatusOK, UserPreferencesResponse{
		Preferences: preferences,
		IsDefault:   false,
	})
}
