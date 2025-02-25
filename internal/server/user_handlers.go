package server

import (
	"errors"
	"fmt"

	"logchef/internal/identity"
	"logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// handleListUsers handles GET /api/v1/users
func (s *Server) handleListUsers(c *fiber.Ctx) error {
	users, err := s.identityService.ListUsers(c.Context())
	if err != nil {
		return fmt.Errorf("error listing users: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, users)
}

// handleGetUser handles GET /api/v1/users/:id
func (s *Server) handleGetUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "user ID is required")
	}

	user, err := s.identityService.GetUser(c.Context(), id)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "user not found")
		}
		return fmt.Errorf("error getting user: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, user)
}

// handleCreateUser handles POST /api/v1/users
func (s *Server) handleCreateUser(c *fiber.Ctx) error {
	var req models.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if req.Email == "" {
		return SendError(c, fiber.StatusBadRequest, "email is required")
	}
	if req.FullName == "" {
		return SendError(c, fiber.StatusBadRequest, "name is required")
	}

	// Get current user ID from context
	userID := c.Locals("userID").(string)

	// Check if current user is admin
	currentUser, err := s.identityService.GetUser(c.Context(), userID)
	if err != nil {
		return fmt.Errorf("error getting current user: %w", err)
	}

	if currentUser.Role != models.UserRoleAdmin {
		return SendError(c, fiber.StatusForbidden, "only admins can create users")
	}

	// Create new user
	newUser := &models.User{
		Email:    req.Email,
		FullName: req.FullName,
		Role:     req.Role,
		Status:   models.UserStatusActive,
	}

	if err := s.identityService.CreateUser(c.Context(), newUser); err != nil {
		var validationErr *identity.ValidationError
		if errors.As(err, &validationErr) {
			return SendError(c, fiber.StatusBadRequest, validationErr.Error())
		}
		return fmt.Errorf("error creating user: %w", err)
	}

	return SendSuccess(c, fiber.StatusCreated, newUser)
}

// handleUpdateUser handles PUT /api/v1/users/:id
func (s *Server) handleUpdateUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "user ID is required")
	}

	var req models.UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Get current user ID from context
	userID := c.Locals("userID").(string)

	// Check if current user is admin or updating their own profile
	currentUser, err := s.identityService.GetUser(c.Context(), userID)
	if err != nil {
		return fmt.Errorf("error getting current user: %w", err)
	}

	// Only admins can update role status
	if req.Role != "" && currentUser.Role != models.UserRoleAdmin {
		return SendError(c, fiber.StatusForbidden, "only admins can update role status")
	}

	// Users can only update their own profile unless they're an admin
	if id != userID && currentUser.Role != models.UserRoleAdmin {
		return SendError(c, fiber.StatusForbidden, "you can only update your own profile")
	}

	// Get existing user
	existingUser, err := s.identityService.GetUser(c.Context(), id)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "user not found")
		}
		return fmt.Errorf("error getting user: %w", err)
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
		return fmt.Errorf("error updating user: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, existingUser)
}

// handleDeleteUser handles DELETE /api/v1/users/:id
func (s *Server) handleDeleteUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "user ID is required")
	}

	// Get current user ID from context
	userID := c.Locals("userID").(string)

	// Check if current user is admin
	currentUser, err := s.identityService.GetUser(c.Context(), userID)
	if err != nil {
		return fmt.Errorf("error getting current user: %w", err)
	}

	if currentUser.Role != models.UserRoleAdmin {
		return SendError(c, fiber.StatusForbidden, "only admins can delete users")
	}

	// Prevent deleting yourself
	if id == userID {
		return SendError(c, fiber.StatusBadRequest, "you cannot delete your own account")
	}

	if err := s.identityService.DeleteUser(c.Context(), id); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return SendError(c, fiber.StatusNotFound, "user not found")
		}
		return fmt.Errorf("error deleting user: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "User deleted successfully"})
}
