package server

import (
	"strconv"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// handleListTeams handles GET /api/v1/teams
func (s *Server) handleListTeams(c *fiber.Ctx) error {
	teams, err := s.identityService.ListTeams(c.Context())
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error listing teams: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, teams)
}

// handleGetTeam handles GET /api/v1/teams/:teamID
func (s *Server) handleGetTeam(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Team ID is required", models.ValidationErrorType)
	}

	// Convert to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID", models.ValidationErrorType)
	}

	// Get team from database
	team, err := s.identityService.GetTeam(c.Context(), models.TeamID(id))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusNotFound, "Team not found", models.NotFoundErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, team)
}

// handleCreateTeam handles POST /api/v1/teams
func (s *Server) handleCreateTeam(c *fiber.Ctx) error {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Validate request
	if req.Name == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Name is required", models.ValidationErrorType)
	}

	// Create team
	team, err := s.identityService.CreateTeam(c.Context(), req.Name, req.Description)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error creating team: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusCreated, team)
}

// handleUpdateTeam handles PUT /api/v1/admin/teams/:teamID
func (s *Server) handleUpdateTeam(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID is required")
	}

	// Convert to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID")
	}

	// Parse request body
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Get team from database
	team, err := s.identityService.GetTeam(c.Context(), models.TeamID(id))
	if err != nil {
		return SendError(c, fiber.StatusNotFound, "Team not found")
	}

	// Update team fields
	team.Name = req.Name
	team.Description = req.Description

	// Update team in database
	if err := s.identityService.UpdateTeam(c.Context(), team); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to update team")
	}

	return SendSuccess(c, fiber.StatusOK, team)
}

// handleDeleteTeam handles DELETE /api/v1/admin/teams/:teamID
func (s *Server) handleDeleteTeam(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID is required")
	}

	// Convert to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID")
	}

	// Delete team from database
	if err := s.identityService.DeleteTeam(c.Context(), models.TeamID(id)); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to delete team")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "Team deleted successfully",
	})
}

// handleListTeamMembers handles GET /api/v1/teams/:teamID/members
func (s *Server) handleListTeamMembers(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID is required")
	}

	// Convert to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID")
	}

	// Get team members from database with user details
	members, err := s.identityService.ListTeamMembersWithDetails(c.Context(), models.TeamID(id))
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to list team members")
	}

	return SendSuccess(c, fiber.StatusOK, members)
}

// handleAddTeamMember handles POST /api/v1/teams/:teamID/members
func (s *Server) handleAddTeamMember(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID is required")
	}

	// Convert to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID")
	}

	// Parse request body
	var req struct {
		UserID models.UserID   `json:"user_id"`
		Role   models.TeamRole `json:"role"`
	}

	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Add team member
	if err := s.identityService.AddTeamMember(c.Context(), models.TeamID(id), req.UserID, req.Role); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to add team member")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "Team member added successfully",
	})
}

// handleRemoveTeamMember handles DELETE /api/v1/teams/:teamID/members/:userID
func (s *Server) handleRemoveTeamMember(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID is required")
	}

	memberIDStr := c.Params("userID")
	if memberIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "User ID is required")
	}

	// Convert to integers
	teamID, err := strconv.Atoi(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID")
	}

	userID, err := strconv.Atoi(memberIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	// Remove team member
	if err := s.identityService.RemoveTeamMember(c.Context(), models.TeamID(teamID), models.UserID(userID)); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to remove team member")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "Team member removed successfully",
	})
}

// handleListTeamSources handles GET /api/v1/teams/:teamID/sources
func (s *Server) handleListTeamSources(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID is required")
	}

	// Convert to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID")
	}

	// Get team sources from database
	sources, err := s.identityService.ListTeamSources(c.Context(), models.TeamID(id))
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to list team sources")
	}

	// Enrich sources with live connection status
	sourceResponses := make([]*models.SourceResponse, len(sources))
	for i, src := range sources {
		// Check connection status using the source service
		src.IsConnected = s.sourceService.CheckSourceConnectionStatus(c.Context(), src)
		sourceResponses[i] = src.ToResponse() // Convert to response object *after* checking status
	}

	return SendSuccess(c, fiber.StatusOK, sourceResponses)
}

// handleAddTeamSource handles POST /api/v1/teams/:teamID/sources
func (s *Server) handleAddTeamSource(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID is required")
	}

	// Parse request body
	var req struct {
		SourceID models.SourceID `json:"source_id"`
	}

	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Convert to integer
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID")
	}

	// Add team source
	if err := s.identityService.AddTeamSource(c.Context(), models.TeamID(id), req.SourceID); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to add team source")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "Source added to team successfully",
	})
}

// handleRemoveTeamSource handles DELETE /api/v1/teams/:teamID/sources/:sourceID
func (s *Server) handleRemoveTeamSource(c *fiber.Ctx) error {
	idStr := c.Params("teamID")
	if idStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID is required")
	}

	sourceIDStr := c.Params("sourceID")
	if sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Source ID is required")
	}

	// Convert to integers
	teamID, err := strconv.Atoi(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID")
	}

	sourceID, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid source ID")
	}

	// Remove team source
	if err := s.identityService.RemoveTeamSource(c.Context(), models.TeamID(teamID), models.SourceID(sourceID)); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to remove team source")
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "Source removed from team successfully",
	})
}
