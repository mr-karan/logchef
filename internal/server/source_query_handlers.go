package server

import (
	"github.com/mr-karan/logchef/pkg/models"

	"strconv"

	"github.com/gofiber/fiber/v2"
)

// handleListSourceQueries handles GET /api/v1/sources/:sourceId/queries
func (s *Server) handleListSourceQueries(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceId")
	if sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

	// Convert string to int for SourceID
	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Check if groupByTeam parameter is true
	groupByTeam := c.Query("groupByTeam") == "true"

	// Check if teamId parameter is provided
	teamIDStr := c.Query("team_id")

	var queries []*models.SavedTeamQuery
	var queryErr error

	if teamIDStr != "" {
		// Convert string to int for TeamID
		teamIDInt, err := strconv.Atoi(teamIDStr)
		if err != nil {
			return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
		}
		teamID := models.TeamID(teamIDInt)

		// Filter queries by team ID if provided
		queries, queryErr = s.teamQueryService.ListQueriesForUserBySourceAndTeam(c.Context(), userID, sourceID, teamID)
	} else {
		// Get all saved queries for this source that the user has access to
		queries, queryErr = s.teamQueryService.ListQueriesForUserBySource(c.Context(), userID, sourceID)
	}

	if queryErr != nil {
		return SendError(c, fiber.StatusInternalServerError, "error listing source queries: "+queryErr.Error())
	}

	if !groupByTeam {
		// Return flat list of queries
		return SendSuccess(c, fiber.StatusOK, queries)
	}

	// Group queries by team
	teamMap := make(map[models.TeamID]*models.TeamGroupedQuery)

	// Process each query
	for _, query := range queries {
		teamID := models.TeamID(query.TeamID)

		// If this team hasn't been seen yet, get the team info
		if _, exists := teamMap[teamID]; !exists {
			team, err := s.identityService.GetTeam(c.Context(), teamID)
			if err != nil {
				s.log.Error("error getting team for query",
					"team_id", teamID,
					"error", err)
				// Use a placeholder name if team can't be found
				teamMap[teamID] = &models.TeamGroupedQuery{
					TeamID:   teamID,
					TeamName: "Unknown Team",
					Queries:  make([]*models.SavedTeamQuery, 0),
				}
			} else {
				teamMap[teamID] = &models.TeamGroupedQuery{
					TeamID:   teamID,
					TeamName: team.Name,
					Queries:  make([]*models.SavedTeamQuery, 0),
				}
			}
		}

		// Add this query to the appropriate team group
		teamMap[teamID].Queries = append(teamMap[teamID].Queries, query)
	}

	// Convert map to slice for response
	teamGroups := make([]*models.TeamGroupedQuery, 0, len(teamMap))
	for _, group := range teamMap {
		teamGroups = append(teamGroups, group)
	}

	return SendSuccess(c, fiber.StatusOK, teamGroups)
}

// handleListUserSources handles GET /api/v1/user/sources
func (s *Server) handleListUserSources(c *fiber.Ctx) error {
	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Get all unique sources the user has access to across all teams
	sources, err := s.identityService.ListSourcesForUser(c.Context(), userID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error listing user sources: "+err.Error())
	}

	// For each source, get the teams that provide access
	sourcesWithTeams := make([]*models.SourceWithTeams, 0, len(sources))
	for _, source := range sources {
		// Convert source to response object to hide sensitive data
		sourceResponse := source.ToResponse()

		// Get teams that have access to this source
		teams, err := s.identityService.ListSourceTeams(c.Context(), source.ID)
		if err != nil {
			s.log.Error("error getting teams for source",
				"source_id", source.ID,
				"error", err)
			// Continue to next source rather than failing the entire request
			continue
		}

		// Filter to only include teams the user is a member of
		userTeams := make([]*models.Team, 0)
		for _, team := range teams {
			// Check if user is a member of this team
			member, err := s.identityService.GetTeamMember(c.Context(), team.ID, userID)
			if err == nil && member != nil {
				userTeams = append(userTeams, team)
			}
		}

		sourcesWithTeams = append(sourcesWithTeams, &models.SourceWithTeams{
			Source: sourceResponse,
			Teams:  userTeams,
		})
	}

	return SendSuccess(c, fiber.StatusOK, sourcesWithTeams)
}

// handleListUserQueries handles GET /api/v1/user/queries
func (s *Server) handleListUserQueries(c *fiber.Ctx) error {
	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Optional source filter
	sourceIDStr := c.Query("source_id")

	var queries []*models.SavedTeamQuery
	var err error

	if sourceIDStr != "" {
		// Convert string to int for SourceID
		sourceIDInt, err := strconv.Atoi(sourceIDStr)
		if err != nil {
			return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
		}
		sourceID := models.SourceID(sourceIDInt)

		// Filter by source if provided
		queries, err = s.teamQueryService.ListQueriesForUserBySource(c.Context(), userID, sourceID)
	} else {
		// Get all queries the user has access to
		queries, err = s.teamQueryService.ListQueriesForUser(c.Context(), userID)
	}

	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error listing user queries: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, queries)
}

// handleCreateSourceQuery handles POST /api/v1/sources/:sourceId/queries
func (s *Server) handleCreateSourceQuery(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceId")
	if sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

	// Convert string to int for SourceID
	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	// Get user from context
	user := c.Locals("user").(*models.User)
	userID := user.ID

	// Parse request body
	var req struct {
		Name         string `json:"name" validate:"required"`
		Description  string `json:"description"`
		QueryContent string `json:"query_content" validate:"required"`
		TeamID       string `json:"team_id" validate:"required"`
	}

	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body: "+err.Error())
	}

	// Validate required fields
	if req.Name == "" {
		return SendError(c, fiber.StatusBadRequest, "name is required")
	}
	if req.QueryContent == "" {
		return SendError(c, fiber.StatusBadRequest, "query_content is required")
	}
	if req.TeamID == "" {
		return SendError(c, fiber.StatusBadRequest, "team_id is required")
	}

	// Convert string to int for TeamID
	teamIDInt, err := strconv.Atoi(req.TeamID)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	// Check if user is a member of the selected team
	member, err := s.identityService.GetTeamMember(c.Context(), teamID, userID)
	if err != nil || member == nil {
		return SendError(c, fiber.StatusForbidden, "you are not a member of the selected team")
	}

	// Check if the team has access to the source
	teamSources, err := s.identityService.ListTeamSources(c.Context(), teamID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error checking team sources: "+err.Error())
	}

	hasAccess := false
	for _, src := range teamSources {
		if src.ID == sourceID {
			hasAccess = true
			break
		}
	}

	if !hasAccess {
		return SendError(c, fiber.StatusForbidden, "the selected team does not have access to this source")
	}

	// Create the query
	createReq := models.CreateTeamQueryRequest{
		Name:         req.Name,
		Description:  req.Description,
		SourceID:     sourceID,
		QueryContent: req.QueryContent,
	}

	query, err := s.teamQueryService.CreateTeamQuery(c.Context(), teamID, createReq)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error creating query: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusCreated, query)
}
