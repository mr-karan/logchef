package server

import (
	"errors"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v3"
)

// --- Team Management Handlers ---

// handleListTeams lists all teams.
// URL: GET /api/v1/admin/teams
// Requires: Admin privileges (requireAdmin middleware)
func (s *Server) handleListTeams(c fiber.Ctx) error {
	teams, err := core.ListTeams(c.Context(), s.store)
	if err != nil {
		s.log.Error("failed to list teams", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error listing teams")
	}
	return SendSuccess(c, fiber.StatusOK, teams)
}

// handleGetTeam retrieves details for a specific team.
// URL: GET /api/v1/teams/:teamID
// Requires: Team membership (requireTeamMember middleware)
func (s *Server) handleGetTeam(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}

	team, err := core.GetTeam(c.Context(), s.store, teamID)
	if err != nil {
		if errors.Is(err, core.ErrTeamNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Team not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get team", "error", err, "team_id", teamID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get team", models.DatabaseErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, team)
}

// handleCreateTeam creates a new team.
// URL: POST /api/v1/admin/teams
// Requires: Admin privileges (requireAdmin middleware)
func (s *Server) handleCreateTeam(c fiber.Ctx) error {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	team, err := core.CreateTeam(c.Context(), s.store, s.log, req.Name, req.Description)
	if err != nil {
		if errors.Is(err, core.ErrTeamAlreadyExists) {
			return SendErrorWithType(c, fiber.StatusConflict, err.Error(), models.ConflictErrorType)
		}
		if validationErr, ok := errors.AsType[*core.ValidationError](err); ok {
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Message, models.ValidationErrorType)
		}
		s.log.Error("failed to create team", "error", err, "name", req.Name)
		return SendError(c, fiber.StatusInternalServerError, "Error creating team")
	}
	if actor := currentUser(c); actor != nil {
		s.log.Info("team.create", "actor", actor.Email, "team", team.Name, "team_id", team.ID)
	}
	auditSet(c, audit.TeamTarget(team), nil, audit.TeamFields(team))
	return SendSuccess(c, fiber.StatusCreated, team)
}

// handleUpdateTeam updates an existing team's details.
// URL: PUT /api/v1/teams/:teamID
// Requires: Team admin or global admin (requireTeamAdminOrGlobalAdmin middleware)
func (s *Server) handleUpdateTeam(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Construct update DTO.
	updateData := models.Team{}
	if req.Name != nil {
		updateData.Name = *req.Name
	}
	if req.Description != nil {
		updateData.Description = *req.Description
	}

	// Call core update function.
	before, after, err := core.UpdateTeam(c.Context(), s.store, s.log, teamID, updateData)
	if err != nil {
		if errors.Is(err, core.ErrTeamNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Team not found", models.NotFoundErrorType)
		}
		if errors.Is(err, core.ErrTeamAlreadyExists) {
			return SendErrorWithType(c, fiber.StatusConflict, err.Error(), models.ConflictErrorType)
		}
		if validationErr, ok := errors.AsType[*core.ValidationError](err); ok {
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Message, models.ValidationErrorType)
		}
		s.log.Error("failed to update team", "error", err, "team_id", teamID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to update team")
	}

	// Fetch and return updated team.
	auditSet(c, audit.TeamTarget(after), audit.TeamFields(before), audit.TeamFields(after))
	updatedTeam, err := core.GetTeam(c.Context(), s.store, teamID)
	if err != nil {
		s.log.Error("failed to fetch updated team after successful update", "error", err, "team_id", teamID)
		return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Team updated successfully, but failed to fetch result"})
	}
	return SendSuccess(c, fiber.StatusOK, updatedTeam)
}

// handleDeleteTeam deletes a team.
// URL: DELETE /api/v1/admin/teams/:teamID
// Requires: Admin privileges (requireAdmin middleware)
func (s *Server) handleDeleteTeam(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	deleted, err := core.DeleteTeam(c.Context(), s.store, s.log, teamID)
	if err != nil {
		if errors.Is(err, core.ErrTeamNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Team not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to delete team", "error", err, "team_id", teamID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to delete team")
	}
	auditSet(c, audit.TeamTarget(deleted), audit.TeamFields(deleted), nil)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Team deleted successfully"})
}

// --- Team Member Handlers ---

// handleListTeamMembers lists members of a specific team.
// URL: GET /api/v1/teams/:teamID/members
// Requires: Team membership (requireTeamMember middleware)
func (s *Server) handleListTeamMembers(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	members, err := core.ListTeamMembers(c.Context(), s.store, teamID)
	if err != nil {
		s.log.Error("failed to list team members", "error", err, "team_id", teamID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to list team members")
	}
	return SendSuccess(c, fiber.StatusOK, members)
}

// handleAddTeamMember adds a user to a team.
// URL: POST /api/v1/teams/:teamID/members
// Requires: Team admin or global admin (requireTeamAdminOrGlobalAdmin middleware)
func (s *Server) handleAddTeamMember(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	var req struct {
		UserID models.UserID   `json:"user_id"`
		Role   models.TeamRole `json:"role"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	before, after, err := core.AddTeamMember(c.Context(), s.store, s.log, teamID, req.UserID, req.Role)
	if err != nil {
		if errors.Is(err, core.ErrTeamNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Team not found", models.NotFoundErrorType)
		}
		if errors.Is(err, core.ErrUserNotFound) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "User specified not found", models.ValidationErrorType)
		}
		if validationErr, ok := errors.AsType[*core.ValidationError](err); ok {
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Message, models.ValidationErrorType)
		}
		if errors.Is(err, core.ErrInvalidRole) {
			return SendErrorWithType(c, fiber.StatusBadRequest, core.ErrInvalidRole.Error(), models.ValidationErrorType)
		}
		// Handle potential already-exists non-error case implicitly (core function returns nil)
		s.log.Error("failed to add team member", "error", err, "team_id", teamID, "user_id", req.UserID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to add team member")
	}
	if actor := currentUser(c); actor != nil {
		s.log.Info("team.member.add", "actor", actor.Email, "team_id", teamID, "member_id", req.UserID, "role", req.Role)
	}
	auditTeamMemberAdd(c, before, after)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Team member added successfully"})
}

// handleRemoveTeamMember removes a user from a team.
// URL: DELETE /api/v1/teams/:teamID/members/:userID
// Requires: Team admin or global admin (requireTeamAdminOrGlobalAdmin middleware)
func (s *Server) handleRemoveTeamMember(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	memberIDStr := c.Params("userID")

	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}
	userID, err := core.ParseUserID(memberIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid user ID: "+err.Error())
	}

	removed, err := core.RemoveTeamMember(c.Context(), s.store, s.log, teamID, userID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to remove team member")
	}
	auditTeamMemberRemove(c, teamID, userID, removed)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Team member removed successfully"})
}

// --- Team Source Handlers ---

// handleListTeamSources lists sources linked to a specific team, including their connection status.
// URL: GET /api/v1/teams/:teamID/sources
// Requires: Team membership (requireTeamMember middleware)
func (s *Server) handleListTeamSources(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	// Get source info, including connection status, linked to the team using the updated core function.
	sources, err := core.ListTeamSources(c.Context(), s.store, s.datasources, s.log, teamID)
	if err != nil {
		if errors.Is(err, core.ErrTeamNotFound) {
			// Team not found is a valid case, return empty list.
			return SendSuccess(c, fiber.StatusOK, []*models.SourceResponse{})
		}
		s.log.Error("failed to list team sources", "error", err, "team_id", teamID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to list team sources")
	}

	// Convert sources (which now include IsConnected) to response objects.
	sourceResponses := make([]*models.SourceResponse, 0, len(sources))
	for _, src := range sources {
		// Check for nil just in case (core function should prevent this)
		if src != nil {
			sourceResponses = append(sourceResponses, src.ToResponse())
		}
	}
	return SendSuccess(c, fiber.StatusOK, sourceResponses)
}

// handleGetTeamSource retrieves detailed information for a specific source within a team context.
// URL: GET /api/v1/teams/:teamID/sources/:sourceID
// Requires: Team membership (requireTeamMember middleware)
func (s *Server) handleGetTeamSource(c fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}

	// Middleware (requireTeamMember) already verifies user has access to the team.
	// Now check that the source is linked to this team (access control).
	teamIDStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}

	hasAccess, err := core.TeamHasSourceAccess(c.Context(), s.store, teamID, sourceID)
	if err != nil {
		s.log.Error("failed to check team source access", "error", err, "team_id", teamID, "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Error checking source access")
	}
	if !hasAccess {
		return SendErrorWithType(c, fiber.StatusForbidden, "Source not linked to this team", models.AuthorizationErrorType)
	}

	// Use the core.GetSource which fetches details (connection, schema).
	sourceDetails, err := core.GetSource(c.Context(), s.datasources, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source details", "error", err, "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to get source details")
	}

	// Convert to response object.
	return SendSuccess(c, fiber.StatusOK, sourceDetails.ToResponse())
}

// handleLinkSourceToTeam links an existing source to a team.
// URL: POST /api/v1/teams/:teamID/sources
// Requires: Team admin or global admin (requireTeamAdminOrGlobalAdmin middleware)
func (s *Server) handleLinkSourceToTeam(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	var req struct {
		SourceID models.SourceID `json:"source_id"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if req.SourceID <= 0 {
		return SendError(c, fiber.StatusBadRequest, "Invalid source ID in request body")
	}

	// Call core function to create the link.
	existed, err := core.AddTeamSource(c.Context(), s.store, s.log, principalFromLocals(c), teamID, req.SourceID)
	if err != nil {
		if errors.Is(err, core.ErrTeamNotFound) || errors.Is(err, core.ErrSourceNotFound) {
			// Return 404 if either team or source doesn't exist.
			return SendErrorWithType(c, fiber.StatusNotFound, err.Error(), models.NotFoundErrorType)
		}
		// Core function returns nil if link already exists.
		s.log.Error("failed to add team source", "error", err, "team_id", teamID, "source_id", req.SourceID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to link source to team")
	}
	link := audit.TeamSourceFields(teamID, req.SourceID)
	var before *audit.Fields
	if existed {
		before = link
	}
	auditSet(c, audit.TeamSourceTarget(teamID, req.SourceID), before, link)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Source linked to team successfully"})
}

// handleUnlinkSourceFromTeam removes the link between a source and a team.
// URL: DELETE /api/v1/teams/:teamID/sources/:sourceID
// Requires: Team admin or global admin (requireTeamAdminOrGlobalAdmin middleware)
func (s *Server) handleUnlinkSourceFromTeam(c fiber.Ctx) error {
	idStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")

	teamID, err := core.ParseTeamID(idStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid source ID: "+err.Error())
	}

	// Call core function to remove the link.
	removed, err := core.RemoveTeamSource(c.Context(), s.store, s.log, teamID, sourceID)
	if err != nil {
		s.log.Error("failed to remove team source", "error", err, "team_id", teamID, "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to remove team source link")
	}
	if removed {
		auditSet(c, audit.TeamSourceTarget(teamID, sourceID), audit.TeamSourceFields(teamID, sourceID), nil)
	} else {
		auditNoChange(c)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Source unlinked from team successfully"})
}

// auditTeamMemberAdd stages a member add, which is a role change when the
// user was already a member.
func auditTeamMemberAdd(c fiber.Ctx, before, after *models.TeamMember) {
	var beforeFields *audit.Fields
	if before != nil {
		beforeFields = audit.TeamMemberFields(before)
	}
	auditSet(c, audit.TeamMemberTarget(after.TeamID, after.UserID), beforeFields, audit.TeamMemberFields(after))
}

// auditTeamMemberRemove stages a member removal, or no event when the user was
// not a member.
func auditTeamMemberRemove(c fiber.Ctx, teamID models.TeamID, userID models.UserID, removed *models.TeamMember) {
	if removed == nil {
		auditNoChange(c)
		return
	}
	auditSet(c, audit.TeamMemberTarget(teamID, userID), audit.TeamMemberFields(removed), nil)
}
