package server

import (
	"errors"
	"strconv"

	"github.com/mr-karan/logchef/internal/logs"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// handleListTeamQueries handles GET /api/v1/teams/:teamId/queries
func (s *Server) handleListTeamQueries(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamId")
	if teamIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	// Convert string to int for TeamID
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Get saved queries for this team that the user has access to
	queries, err := s.teamQueryService.ListQueriesForUserAndTeam(c.Context(), userID, teamID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error listing team queries: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, queries)
}

// handleGetTeamQuery handles GET /api/v1/teams/:teamId/queries/:queryId
func (s *Server) handleGetTeamQuery(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamId")
	queryIDStr := c.Params("queryId")
	if teamIDStr == "" || queryIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID and query ID are required")
	}

	// Convert string to int for TeamID
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	// Convert string to int for queryID
	queryIDInt, err := strconv.Atoi(queryIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid query ID: "+err.Error())
	}

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Get saved query with access check
	query, err := s.teamQueryService.GetTeamQueryWithAccess(c.Context(), queryIDInt, userID)
	if err != nil {
		if errors.Is(err, logs.ErrTeamQueryNotFound) {
			return SendError(c, fiber.StatusNotFound, "team query not found or access denied")
		}
		return SendError(c, fiber.StatusInternalServerError, "error getting team query: "+err.Error())
	}

	// Verify the query belongs to the specified team
	if query.TeamID != teamID {
		return SendError(c, fiber.StatusForbidden, "query does not belong to the specified team")
	}

	return SendSuccess(c, fiber.StatusOK, query)
}

// handleCreateTeamQuery handles POST /api/v1/teams/:teamId/queries
func (s *Server) handleCreateTeamQuery(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamId")
	if teamIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID is required")
	}

	// Convert string to int for TeamID
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	// Get user from context
	user := c.Locals("user").(*models.User)

	// Check if user is a member of the team
	members, err := s.identityService.ListTeamMembers(c.Context(), teamID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error checking team membership: "+err.Error())
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

	// Parse request body
	var req models.CreateTeamQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body: "+err.Error())
	}

	// Validate request
	if req.Name == "" {
		return SendError(c, fiber.StatusBadRequest, "name is required")
	}
	if req.SourceID == 0 {
		return SendError(c, fiber.StatusBadRequest, "source_id is required")
	}
	if req.QueryContent == "" {
		return SendError(c, fiber.StatusBadRequest, "query content is required")
	}

	// Validate that the team has access to the source
	sourceID := models.SourceID(req.SourceID)
	teamSources, err := s.identityService.ListTeamSources(c.Context(), teamID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error checking team sources: "+err.Error())
	}

	hasAccess := false
	for _, source := range teamSources {
		if source.ID == sourceID {
			hasAccess = true
			break
		}
	}

	if !hasAccess {
		return SendError(c, fiber.StatusForbidden, "this team does not have access to the specified source")
	}

	// Create the saved query
	query, err := s.teamQueryService.CreateTeamQuery(c.Context(), teamID, req)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error creating team query: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusCreated, query)
}

// handleUpdateTeamQuery handles PUT /api/v1/teams/:teamId/queries/:queryId
func (s *Server) handleUpdateTeamQuery(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamId")
	queryIDStr := c.Params("queryId")
	if teamIDStr == "" || queryIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID and query ID are required")
	}

	// Convert string to int for TeamID
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	// Convert string to int for queryID
	queryIDInt, err := strconv.Atoi(queryIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid query ID: "+err.Error())
	}

	// Parse request body
	var req models.UpdateTeamQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body: "+err.Error())
	}

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Get existing query with access check
	existingQuery, err := s.teamQueryService.GetTeamQueryWithAccess(c.Context(), queryIDInt, userID)
	if err != nil {
		if errors.Is(err, logs.ErrTeamQueryNotFound) {
			return SendError(c, fiber.StatusNotFound, "team query not found or access denied")
		}
		return SendError(c, fiber.StatusInternalServerError, "error getting team query: "+err.Error())
	}

	// Verify the query belongs to the specified team
	if existingQuery.TeamID != teamID {
		return SendError(c, fiber.StatusForbidden, "query does not belong to the specified team")
	}

	// If source ID is provided, validate that the team has access to it
	if req.SourceID != 0 {
		sourceID := req.SourceID
		teamSources, err := s.identityService.ListTeamSources(c.Context(), teamID)
		if err != nil {
			return SendError(c, fiber.StatusInternalServerError, "error checking team sources: "+err.Error())
		}

		hasAccess := false
		for _, source := range teamSources {
			if source.ID == sourceID {
				hasAccess = true
				break
			}
		}

		if !hasAccess {
			return SendError(c, fiber.StatusForbidden, "team does not have access to the specified source")
		}
	}

	// Update the query
	updatedQuery, err := s.teamQueryService.UpdateTeamQuery(c.Context(), queryIDInt, req)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error updating team query: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, updatedQuery)
}

// handleDeleteTeamQuery handles DELETE /api/v1/teams/:teamId/queries/:queryId
func (s *Server) handleDeleteTeamQuery(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamId")
	queryIDStr := c.Params("queryId")
	if teamIDStr == "" || queryIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "team ID and query ID are required")
	}

	// Convert string to int for TeamID
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	// Convert string to int for queryID
	queryIDInt, err := strconv.Atoi(queryIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid query ID: "+err.Error())
	}

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Get existing query with access check
	existingQuery, err := s.teamQueryService.GetTeamQueryWithAccess(c.Context(), queryIDInt, userID)
	if err != nil {
		if errors.Is(err, logs.ErrTeamQueryNotFound) {
			return SendError(c, fiber.StatusNotFound, "team query not found or access denied")
		}
		return SendError(c, fiber.StatusInternalServerError, "error getting team query: "+err.Error())
	}

	// Verify the query belongs to the specified team
	if existingQuery.TeamID != teamID {
		return SendError(c, fiber.StatusForbidden, "query does not belong to the specified team")
	}

	// Delete the query
	if err := s.teamQueryService.DeleteTeamQuery(c.Context(), queryIDInt); err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error deleting team query: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Query deleted successfully"})
}
