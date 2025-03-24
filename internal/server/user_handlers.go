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
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list users", models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, users)
}

// handleGetUser handles GET /api/v1/admin/users/:userID
func (s *Server) handleGetUser(c *fiber.Ctx) error {
	// Get user ID from params
	id := c.Params("userID")
	if id == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "User ID is required", models.ValidationErrorType)
	}

	// Convert to integer
	userID, err := strconv.Atoi(id)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid user ID", models.ValidationErrorType)
	}

	// Get user from database
	user, err := s.identityService.GetUser(c.Context(), models.UserID(userID))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusNotFound, "User not found", models.NotFoundErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, user)
}

// handleCreateUser handles POST /api/v1/users
func (s *Server) handleCreateUser(c *fiber.Ctx) error {
	var req models.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		s.log.Error("Failed to parse request body", "error", err)
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Validate request
	if req.Email == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Email is required", models.ValidationErrorType)
	}
	if req.FullName == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Name is required", models.ValidationErrorType)
	}

	// Create new user
	user, err := s.identityService.CreateUser(c.Context(), req.Email, req.FullName, req.Role)
	if err != nil {
		var validationErr *identity.ValidationError
		if errors.As(err, &validationErr) {
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Error(), models.ValidationErrorType)
		}
		s.log.Error("Failed to create user", "error", err, "email", req.Email)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create user", models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusCreated, user)
}

// handleUpdateUser handles PUT /api/v1/admin/users/:userID
func (s *Server) handleUpdateUser(c *fiber.Ctx) error {
	// Get user ID from params
	id := c.Params("userID")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "User ID is required")
	}

	// Convert to integer
	userID, err := strconv.Atoi(id)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	// Parse request body
	var req struct {
		FullName *string `json:"full_name"`
		Email    *string `json:"email"`
		Role     *string `json:"role"`
		Status   *string `json:"status"`
	}

	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Get user from database
	user, err := s.identityService.GetUser(c.Context(), models.UserID(userID))
	if err != nil {
		return SendError(c, fiber.StatusNotFound, "User not found")
	}

	// Update user fields if provided
	if req.FullName != nil {
		user.FullName = *req.FullName
	}

	if req.Email != nil {
		// Validate email format
		if *req.Email == "" {
			return SendError(c, fiber.StatusBadRequest, "Email cannot be empty")
		}
		user.Email = *req.Email
	}

	if req.Role != nil {
		role := models.UserRole(*req.Role)
		// Validate role
		if role != models.UserRoleAdmin && role != models.UserRoleMember {
			return SendError(c, fiber.StatusBadRequest, "Invalid role")
		}
		user.Role = role
	}

	if req.Status != nil {
		status := models.UserStatus(*req.Status)
		// Validate status
		if status != models.UserStatusActive && status != models.UserStatusInactive {
			return SendError(c, fiber.StatusBadRequest, "Invalid status")
		}
		user.Status = status
	}

	// Update user in database
	if err := s.identityService.UpdateUser(c.Context(), user); err != nil {
		var validationErr *identity.ValidationError
		if errors.As(err, &validationErr) {
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Error(), models.ValidationErrorType)
		}
		s.log.Error("Failed to update user", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to update user")
	}

	return SendSuccess(c, fiber.StatusOK, user)
}

// handleDeleteUser handles DELETE /api/v1/admin/users/:userID
func (s *Server) handleDeleteUser(c *fiber.Ctx) error {
	// Get user ID from params
	id := c.Params("userID")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "User ID is required")
	}

	// Convert to integer
	userID, err := strconv.Atoi(id)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	// Delete user from database
	if err := s.identityService.DeleteUser(c.Context(), models.UserID(userID)); err != nil {
		if validationErr, ok := err.(*identity.ValidationError); ok {
			return SendError(c, fiber.StatusBadRequest, validationErr.Message)
		}
		return SendError(c, fiber.StatusInternalServerError, "Failed to delete user")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "User deleted successfully",
	})
}

// handleListUserTeams handles GET /api/v1/users/me/teams
func (s *Server) handleListUserTeams(c *fiber.Ctx) error {
	// Get user from context
	user := c.Locals("user").(*models.User)

	// Get teams for user
	teams, err := s.identityService.ListTeamsForUser(c.Context(), user.ID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to list teams")
	}

	// For each team, get additional information like member count
	type TeamResponse struct {
		*models.Team
		MemberCount int  `json:"member_count"`
		IsAdmin     bool `json:"is_admin"`
	}

	teamResponses := make([]TeamResponse, 0, len(teams))
	for _, team := range teams {
		// Get team members to count them
		members, err := s.identityService.ListTeamMembers(c.Context(), team.ID)
		if err != nil {
			s.log.Error("Failed to get team members", "team_id", team.ID, "error", err)
			continue
		}

		// Check if user is admin of this team
		isAdmin := false
		for _, member := range members {
			if member.UserID == user.ID && member.Role == models.TeamRoleAdmin {
				isAdmin = true
				break
			}
		}

		teamResponses = append(teamResponses, TeamResponse{
			Team:        team,
			MemberCount: len(members),
			IsAdmin:     isAdmin,
		})
	}

	return SendSuccess(c, fiber.StatusOK, teamResponses)
}

// handleGetTeamSource handles GET /api/v1/teams/:teamID/sources/:sourceID
func (s *Server) handleGetTeamSource(c *fiber.Ctx) error {
	// Get team ID and source ID from params
	teamID := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	if teamID == "" || sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID and Source ID are required")
	}

	// Convert source ID to integer
	sourceID, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid source ID")
	}

	// Get source (middleware already checked team membership and source access)
	source, err := s.sourceService.GetSource(c.Context(), models.SourceID(sourceID))
	if err != nil {
		return SendError(c, fiber.StatusNotFound, "Source not found")
	}

	return SendSuccess(c, fiber.StatusOK, source)
}
