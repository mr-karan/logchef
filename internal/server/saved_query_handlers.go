package server

import (
	"errors"
	"strconv"

	core "github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

func parseSavedQueryID(c *fiber.Ctx) (int, error) {
	id, err := parsePositiveIntParam(c, "queryID")
	return int(id), err
}

// loadSavedQueryWithVisibility fetches a saved query and verifies the caller
// has visibility (source access via any team). Returns the query, the caller,
// and a Fiber response if either lookup or authorization fails.
func (s *Server) loadSavedQueryWithVisibility(c *fiber.Ctx) (*models.SavedQuery, *models.User, error) {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return nil, nil, SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}

	queryID, err := parseSavedQueryID(c)
	if err != nil {
		return nil, nil, SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	query, err := core.GetSavedQuery(c.Context(), s.sqlite, s.log, queryID)
	if err != nil {
		if errors.Is(err, core.ErrQueryNotFound) {
			return nil, nil, SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to load saved query", "error", err, "query_id", queryID)
		return nil, nil, SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load saved query", models.GeneralErrorType)
	}

	// Admins do not get a free pass on visibility — they must be a member of a
	// team that has the source. Edit gates (UserCanEditSavedQuery) still let
	// an admin who can SEE a query also edit it.
	hasAccess, accessErr := s.sqlite.UserHasSourceAccess(c.Context(), user.ID, query.SourceID)
	if accessErr != nil {
		s.log.Error("failed to check source access for saved query", "error", accessErr, "user_id", user.ID, "source_id", query.SourceID)
		return nil, nil, SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify access", models.GeneralErrorType)
	}
	if !hasAccess {
		return nil, nil, SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
	}

	return query, user, nil
}

// enrichSavedQueryPermissions populates CanEdit/CanDelete on the query for the
// calling user — UI affordance hints. Best-effort: on error it logs and leaves
// CanEdit nil so the UI falls back to hiding the action.
func (s *Server) enrichSavedQueryPermissions(c *fiber.Ctx, query *models.SavedQuery, user *models.User) {
	if query == nil || user == nil {
		return
	}
	canDelete := core.UserCanDeleteSavedQuery(query, user)
	query.CanDelete = &canDelete
	canEdit, err := core.UserCanEditSavedQuery(c.Context(), s.sqlite, query, user)
	if err != nil {
		s.log.Error("failed to compute can_edit for saved query", "error", err, "query_id", query.ID, "user_id", user.ID)
		return
	}
	query.CanEdit = &canEdit
}

// handleAdminListSavedQueries returns every saved query across all sources — the
// Library "All queries" admin browse surface. Admin authorization is enforced at
// the route (the /admin group's requireAdmin), so no in-handler role check. Each
// row is marked .runnable for the caller; rows for sources the admin can't reach
// are shown locked client-side. The user-facing handleListSavedQueries below is
// deliberately untouched (still source-access-gated; consumed by the explorer
// dropdown and the CLI).
func (s *Server) handleAdminListSavedQueries(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	queries, err := core.ListAllSavedQueries(c.Context(), s.sqlite, s.log)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list saved queries", models.GeneralErrorType)
	}
	// Mark which rows this admin can actually run (source access); the rest are
	// shown locked. Best-effort — a failure just leaves runnable unset.
	if err := core.MarkSavedQueriesRunnable(c.Context(), s.sqlite, user.ID, queries); err != nil {
		s.log.Error("failed to mark saved queries runnable", "error", err, "user_id", user.ID)
	}
	return SendSuccess(c, fiber.StatusOK, queries)
}

// handleListSavedQueries lists saved queries the caller can see. Optional
// ?source_id filter. Source-access-gated; consumed by the explorer dropdown and
// the CLI, so its response shape stays stable.
func (s *Server) handleListSavedQueries(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	if sourceParam := c.Query("source_id"); sourceParam != "" {
		sourceID, err := core.ParseSourceID(sourceParam)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
		}
		queries, err := core.ListSavedQueriesForUserBySource(c.Context(), s.sqlite, s.log, user.ID, sourceID)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list saved queries", models.GeneralErrorType)
		}
		return SendSuccess(c, fiber.StatusOK, queries)
	}

	queries, err := core.ListSavedQueriesForUser(c.Context(), s.sqlite, s.log, user.ID)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list saved queries", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, queries)
}

// handleCreateSavedQuery creates a new saved query bound to the supplied source.
// The caller must have source access via any of their teams; the resulting query
// is owned by the caller (created_by = user.ID).
func (s *Server) handleCreateSavedQuery(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req models.CreateSavedQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if req.Name == "" || req.SourceID == 0 || req.QueryContent == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Missing required fields (name, source_id, query_content)", models.ValidationErrorType)
	}
	if req.QueryLanguage == "" && req.EditorMode == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "query_language or editor_mode is required", models.ValidationErrorType)
	}

	hasAccess, err := s.sqlite.UserHasSourceAccess(c.Context(), user.ID, req.SourceID)
	if err != nil {
		s.log.Error("failed to check source access for saved query create", "error", err, "user_id", user.ID, "source_id", req.SourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify access", models.GeneralErrorType)
	}
	if !hasAccess {
		return SendErrorWithType(c, fiber.StatusForbidden, "No team you belong to has access to this source", models.AuthorizationErrorType)
	}

	if req.CreatedFromTeamID != nil {
		isMember, memberErr := core.IsTeamMember(c.Context(), s.sqlite, *req.CreatedFromTeamID, user.ID)
		if memberErr != nil {
			s.log.Error("failed to check saved query team membership", "error", memberErr, "user_id", user.ID, "team_id", *req.CreatedFromTeamID)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify team access", models.GeneralErrorType)
		}
		if !isMember {
			return SendErrorWithType(c, fiber.StatusForbidden, "You are not a member of the selected team", models.AuthorizationErrorType)
		}

		teamHasSource, teamSourceErr := core.TeamHasSourceAccess(c.Context(), s.sqlite, *req.CreatedFromTeamID, req.SourceID)
		if teamSourceErr != nil {
			s.log.Error("failed to check saved query team source access", "error", teamSourceErr, "team_id", *req.CreatedFromTeamID, "source_id", req.SourceID)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify source access", models.GeneralErrorType)
		}
		if !teamHasSource {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Selected team does not have access to this source", models.ValidationErrorType)
		}
	}

	created, err := core.CreateSavedQuery(c.Context(), s.sqlite, s.datasources, s.log, req.SourceID, req.CreatedFromTeamID, req.Name, req.Description, req.QueryContent, req.QueryLanguage, req.EditorMode, user.ID)
	if err != nil {
		if errors.Is(err, core.ErrQueryLanguageRequired) || errors.Is(err, core.ErrInvalidQueryDefinition) || errors.Is(err, core.ErrUnsupportedSavedQueryDefinition) || errors.Is(err, core.ErrInvalidQueryContent) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create saved query", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, created)
}

// handleGetSavedQuery returns a single saved query.
func (s *Server) handleGetSavedQuery(c *fiber.Ctx) error {
	query, user, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	s.enrichSavedQueryPermissions(c, query, user)
	return SendSuccess(c, fiber.StatusOK, query)
}

// handleUpdateSavedQuery updates a saved query. Allowed only for the creator
// or a global admin; legacy queries (created_by IS NULL) require global admin.
func (s *Server) handleUpdateSavedQuery(c *fiber.Ctx) error {
	query, user, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	canEdit, editErr := core.UserCanEditSavedQuery(c.Context(), s.sqlite, query, user)
	if editErr != nil {
		s.log.Error("failed to check saved query edit access", "error", editErr, "query_id", query.ID, "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify edit access", models.GeneralErrorType)
	}
	if !canEdit {
		return SendErrorWithType(c, fiber.StatusForbidden, "You don't have permission to edit this query. You must be its creator, a global admin, or an owner/editor of a collection it belongs to.", models.AuthorizationErrorType)
	}

	var req struct {
		Name          *string                      `json:"name"`
		Description   *string                      `json:"description"`
		QueryLanguage *models.QueryLanguage        `json:"query_language"`
		EditorMode    *models.SavedQueryEditorMode `json:"editor_mode"`
		QueryContent  *string                      `json:"query_content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	name := query.Name
	if req.Name != nil {
		name = *req.Name
	}
	description := query.Description
	if req.Description != nil {
		description = *req.Description
	}
	queryLanguage := query.QueryLanguage
	if req.QueryLanguage != nil {
		queryLanguage = *req.QueryLanguage
	}
	editorMode := query.EditorMode
	if req.EditorMode != nil {
		editorMode = *req.EditorMode
	}
	queryContent := query.QueryContent
	if req.QueryContent != nil {
		queryContent = *req.QueryContent
	}

	updated, updateErr := core.UpdateSavedQuery(c.Context(), s.sqlite, s.datasources, s.log, query.ID, name, description, queryContent, queryLanguage, editorMode)
	if updateErr != nil {
		if errors.Is(updateErr, core.ErrQueryNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
		}
		if errors.Is(updateErr, core.ErrInvalidQueryDefinition) || errors.Is(updateErr, core.ErrUnsupportedSavedQueryDefinition) || errors.Is(updateErr, core.ErrInvalidQueryContent) {
			return SendErrorWithType(c, fiber.StatusBadRequest, updateErr.Error(), models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update saved query", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, updated)
}

// handleDeleteSavedQuery removes a saved query (creator + global admin only).
func (s *Server) handleDeleteSavedQuery(c *fiber.Ctx) error {
	query, user, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	if !core.UserCanDeleteSavedQuery(query, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can delete this query", models.AuthorizationErrorType)
	}

	if delErr := core.DeleteSavedQuery(c.Context(), s.sqlite, s.log, query.ID); delErr != nil {
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete saved query", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Saved query deleted successfully"})
}

// handleResolveSavedQuery returns the full saved-query struct for the explorer
// to hydrate without round-tripping through URL params.
func (s *Server) handleResolveSavedQuery(c *fiber.Ctx) error {
	query, user, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	if query == nil || user == nil {
		s.log.Error("saved query resolver loaded invalid context", "query_nil", query == nil, "user_nil", user == nil)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to resolve saved query context", models.GeneralErrorType)
	}

	teams, err := core.ListTeamsWithAccessToSource(c.Context(), s.sqlite, s.log, query.SourceID, user.ID)
	if err != nil {
		s.log.Error("failed to resolve saved query team", "error", err, "query_id", query.ID, "source_id", query.SourceID, "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to resolve saved query context", models.GeneralErrorType)
	}
	if len(teams) == 0 {
		return SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
	}

	resolvedTeamID := teams[0].ID
	hasAccessibleTeam := func(teamID models.TeamID) bool {
		for _, team := range teams {
			if team.ID == teamID {
				return true
			}
		}
		return false
	}

	usedPreferredTeam := false
	if preferredTeam := c.Query("team_id"); preferredTeam != "" {
		if parsed, parseErr := strconv.ParseInt(preferredTeam, 10, 64); parseErr == nil {
			preferredTeamID := models.TeamID(parsed)
			if hasAccessibleTeam(preferredTeamID) {
				resolvedTeamID = preferredTeamID
				usedPreferredTeam = true
			}
		}
	}
	if !usedPreferredTeam && query.CreatedFromTeamID != nil {
		if hasAccessibleTeam(*query.CreatedFromTeamID) {
			resolvedTeamID = *query.CreatedFromTeamID
		}
	}

	return SendSuccess(c, fiber.StatusOK, &models.ResolvedSavedQuery{
		SavedQuery:     *query,
		ResolvedTeamID: resolvedTeamID,
	})
}
