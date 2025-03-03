package server

import (
	"errors"
	"strconv"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/identity"

	"github.com/gofiber/fiber/v2"
)

// handleListUsers handles GET /api/v1/users
func (s *Server) handleListUsers(c *fiber.Ctx) error {
	users, err := s.identityService.ListUsers(c.Context())
	if err != nil {
		s.log.Error("Failed to list users", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "failed to list users")
	}

	return SendSuccess(c, fiber.StatusOK, users)
}

// handleGetUser handles GET /api/v1/users/:id
func (s *Server) handleGetUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "user ID is required")
	}

	// Convert string to int for UserID
	idInt, err := strconv.Atoi(id)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid user ID: "+err.Error())
	}
	userID := models.UserID(idInt)

	user, err := s.identityService.GetUser(c.Context(), userID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "user not found")
		}
		s.log.Error("Failed to get user", "error", err, "userID", userID)
		return SendError(c, fiber.StatusInternalServerError, "failed to get user")
	}

	return SendSuccess(c, fiber.StatusOK, user)
}

// handleCreateUser handles POST /api/v1/users
func (s *Server) handleCreateUser(c *fiber.Ctx) error {
	var req models.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		s.log.Error("Failed to parse request body", "error", err)
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if req.Email == "" {
		return SendError(c, fiber.StatusBadRequest, "email is required")
	}
	if req.FullName == "" {
		return SendError(c, fiber.StatusBadRequest, "name is required")
	}

	// Create new user
	user, err := s.identityService.CreateUser(c.Context(), req.Email, req.FullName, req.Role)
	if err != nil {
		var validationErr *identity.ValidationError
		if errors.As(err, &validationErr) {
			return SendError(c, fiber.StatusBadRequest, validationErr.Error())
		}
		s.log.Error("Failed to create user", "error", err, "email", req.Email)
		return SendError(c, fiber.StatusInternalServerError, "failed to create user")
	}

	return SendSuccess(c, fiber.StatusCreated, user)
}

// handleUpdateUser handles PUT /api/v1/users/:id
func (s *Server) handleUpdateUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "user ID is required")
	}

	// Convert string to int for UserID
	idInt, err := strconv.Atoi(id)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid user ID: "+err.Error())
	}
	userID := models.UserID(idInt)

	var req models.UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		s.log.Error("Failed to parse request body", "error", err, "path", c.Path())
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Get current user from context (set by middleware)
	currentUser := c.Locals("user").(*models.User)

	// Only admins can update role status
	if req.Role != "" && currentUser.Role != models.UserRoleAdmin {
		return SendError(c, fiber.StatusForbidden, "only admins can update role status")
	}

	// Users can only update their own profile unless they're an admin
	if userID != currentUser.ID && currentUser.Role != models.UserRoleAdmin {
		return SendError(c, fiber.StatusForbidden, "you can only update your own profile")
	}

	// Get existing user
	existingUser, err := s.identityService.GetUser(c.Context(), userID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "user not found")
		}
		s.log.Error("Failed to get user", "error", err, "userID", userID)
		return SendError(c, fiber.StatusInternalServerError, "failed to get user")
	}

	// Update user fields
	if req.FullName != "" {
		existingUser.FullName = req.FullName
	}
	if req.Status != "" {
		existingUser.Status = req.Status
	}
	if req.Role != "" {
		existingUser.Role = req.Role
	}

	if err := s.identityService.UpdateUser(c.Context(), existingUser); err != nil {
		var validationErr *identity.ValidationError
		if errors.As(err, &validationErr) {
			return SendError(c, fiber.StatusBadRequest, validationErr.Error())
		}
		if errors.Is(err, identity.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "user not found")
		}
		s.log.Error("Failed to update user", "error", err, "userID", userID)
		return SendError(c, fiber.StatusInternalServerError, "failed to update user")
	}

	return SendSuccess(c, fiber.StatusOK, existingUser)
}

// handleDeleteUser handles DELETE /api/v1/users/:id
func (s *Server) handleDeleteUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "user ID is required")
	}

	// Convert string to int for UserID
	idInt, err := strconv.Atoi(id)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid user ID: "+err.Error())
	}
	userID := models.UserID(idInt)

	// Get current user from context (set by middleware)
	currentUser := c.Locals("user").(*models.User)

	// Prevent deleting yourself
	if userID == currentUser.ID {
		return SendError(c, fiber.StatusBadRequest, "you cannot delete your own account")
	}

	if err := s.identityService.DeleteUser(c.Context(), userID); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "user not found")
		}
		s.log.Error("Failed to delete user", "error", err, "userID", userID)
		return SendError(c, fiber.StatusInternalServerError, "failed to delete user")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "User deleted successfully"})
}
