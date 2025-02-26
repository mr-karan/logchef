package server

import (
	"errors"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/identity"

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

// handleGetTeam handles GET /api/v1/teams/:id
func (s *Server) handleGetTeam(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	team, err := s.identityService.GetTeam(c.Context(), id)
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error getting team: "+err.Error())
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
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if req.Name == "" {
		return SendError(c, fiber.StatusBadRequest, "name is required")
	}

	// Get user from context
	user := c.Locals("user").(*models.User)

	// Create team
	team := &models.Team{
		Name:        req.Name,
		Description: req.Description,
	}

	if err := s.identityService.CreateTeam(c.Context(), team); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error creating team: "+err.Error())
	}

	// Add creator as admin
	if err := s.identityService.AddTeamMember(c.Context(), team.ID, user.ID, "admin"); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error adding team member: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusCreated, team)
}

// handleUpdateTeam handles PUT /api/v1/teams/:id
func (s *Server) handleUpdateTeam(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Get existing team
	team, err := s.identityService.GetTeam(c.Context(), id)
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error getting team: "+err.Error())
	}

	// Check if user is team admin
	members, err := s.identityService.ListTeamMembers(c.Context(), models.TeamID(id))
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error listing team members: "+err.Error())
	}

	isAdmin := false
	for _, member := range members {
		if member.UserID == userID && member.Role == models.TeamRoleAdmin {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return SendError(c, fiber.StatusForbidden, "you are not an admin of this team")
	}

	// Update team
	if req.Name != "" {
		team.Name = req.Name
	}
	if req.Description != "" {
		team.Description = req.Description
	}

	if err := s.identityService.UpdateTeam(c.Context(), team); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error updating team: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, team)
}

// handleDeleteTeam handles DELETE /api/v1/teams/:id
func (s *Server) handleDeleteTeam(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	// Get user from context
	user := c.Locals("user").(*models.User)

	// Check if user is team admin
	members, err := s.identityService.ListTeamMembers(c.Context(), models.TeamID(id))
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error listing team members: "+err.Error())
	}

	isAdmin := false
	for _, member := range members {
		if member.UserID == user.ID && member.Role == models.TeamRoleAdmin {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return SendError(c, fiber.StatusForbidden, "you are not an admin of this team")
	}

	if err := s.identityService.DeleteTeam(c.Context(), id); err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error deleting team: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Team deleted successfully"})
}

// handleListTeamMembers handles GET /api/v1/teams/:id/members
func (s *Server) handleListTeamMembers(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	// Get user from context
	user := c.Locals("user").(*models.User)

	// Check if user is team member
	members, err := s.identityService.ListTeamMembers(c.Context(), models.TeamID(id))
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error listing team members: "+err.Error())
	}

	isMember := false
	for _, member := range members {
		if member.UserID == user.ID {
			isMember = true
			break
		}
	}

	if !isMember {
		return SendError(c, fiber.StatusForbidden, "you are not a member of this team")
	}

	return SendSuccess(c, fiber.StatusOK, members)
}

// handleAddTeamMember handles POST /api/v1/teams/:id/members
func (s *Server) handleAddTeamMember(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	var req struct {
		UserID models.UserID   `json:"user_id"`
		Role   models.TeamRole `json:"role"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if req.UserID == "" {
		return SendError(c, fiber.StatusBadRequest, "user ID is required")
	}
	if req.Role == "" {
		req.Role = models.TeamRoleMember // Default role
	}

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Check if user is team admin
	members, err := s.identityService.ListTeamMembers(c.Context(), models.TeamID(id))
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error listing team members: "+err.Error())
	}

	isAdmin := false
	for _, member := range members {
		if member.UserID == userID && member.Role == models.TeamRoleAdmin {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return SendError(c, fiber.StatusForbidden, "you are not an admin of this team")
	}

	if err := s.identityService.AddTeamMember(c.Context(), models.TeamID(id), req.UserID, req.Role); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error adding team member: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Member added successfully"})
}

// handleRemoveTeamMember handles DELETE /api/v1/teams/:id/members/:userId
func (s *Server) handleRemoveTeamMember(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	memberID := c.Params("userId")
	if memberID == "" {
		return SendError(c, fiber.StatusBadRequest, "member ID is required")
	}

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Check if user is team admin
	members, err := s.identityService.ListTeamMembers(c.Context(), models.TeamID(id))
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error listing team members: "+err.Error())
	}

	isAdmin := false
	for _, member := range members {
		if member.UserID == userID && member.Role == models.TeamRoleAdmin {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return SendError(c, fiber.StatusForbidden, "you are not an admin of this team")
	}

	if err := s.identityService.RemoveTeamMember(c.Context(), models.TeamID(id), models.UserID(memberID)); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error removing team member: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Member removed successfully"})
}

// handleListTeamSources handles GET /api/v1/teams/:id/sources
func (s *Server) handleListTeamSources(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	// Get user from context
	user := c.Locals("user").(*models.User)

	// Check if user is team member
	members, err := s.identityService.ListTeamMembers(c.Context(), models.TeamID(id))
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error listing team members: "+err.Error())
	}

	isMember := false
	for _, member := range members {
		if member.UserID == user.ID {
			isMember = true
			break
		}
	}

	if !isMember {
		return SendError(c, fiber.StatusForbidden, "you are not a member of this team")
	}

	sources, err := s.identityService.ListTeamSources(c.Context(), models.TeamID(id))
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error listing team sources: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, sources)
}

// handleAddTeamSource handles POST /api/v1/teams/:id/sources
func (s *Server) handleAddTeamSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	var req struct {
		SourceID models.SourceID `json:"source_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if req.SourceID == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

	// Get user from context
	user := c.Locals("user").(*models.User)

	// Check if user is team admin
	members, err := s.identityService.ListTeamMembers(c.Context(), models.TeamID(id))
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error listing team members: "+err.Error())
	}

	isAdmin := false
	for _, member := range members {
		if member.UserID == user.ID && member.Role == models.TeamRoleAdmin {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return SendError(c, fiber.StatusForbidden, "you are not an admin of this team")
	}

	if err := s.identityService.AddTeamSource(c.Context(), models.TeamID(id), req.SourceID); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error adding team source: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Source added successfully"})
}

// handleRemoveTeamSource handles DELETE /api/v1/teams/:id/sources/:sourceId
func (s *Server) handleRemoveTeamSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	sourceID := c.Params("sourceId")
	if sourceID == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

	// Get user from context
	user := c.Locals("user").(*models.User)

	// Check if user is team admin
	members, err := s.identityService.ListTeamMembers(c.Context(), models.TeamID(id))
	if err != nil {
		if errors.Is(err, identity.ErrTeamNotFound) {
			return SendError(c, fiber.StatusNotFound, "team not found")
		}
		return SendError(c, fiber.StatusInternalServerError, "error listing team members: "+err.Error())
	}

	isAdmin := false
	for _, member := range members {
		if member.UserID == user.ID && member.Role == models.TeamRoleAdmin {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return SendError(c, fiber.StatusForbidden, "you are not an admin of this team")
	}

	if err := s.identityService.RemoveTeamSource(c.Context(), models.TeamID(id), models.SourceID(sourceID)); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error removing team source: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Source removed successfully"})
}
