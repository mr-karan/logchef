package server

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// savedViewCaller builds the caller for the team and source that
// requireTeamHasSource authorized. ok=false means a response was written.
func (s *Server) savedViewCaller(c fiber.Ctx) (core.SavedViewCaller, bool) {
	src, ok := s.authorizedSource(c)
	if !ok {
		return core.SavedViewCaller{}, false
	}
	caller, err := core.NewSavedViewCaller(c.Context(), s.store, principalFromLocals(c), src)
	if err != nil {
		s.log.Error("failed to read team role for saved views", "error", err, "team_id", src.TeamID())
		_ = SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify team role", models.GeneralErrorType)
		return core.SavedViewCaller{}, false
	}
	return caller, true
}

func parseSavedViewID(c fiber.Ctx) (models.SavedViewID, error) {
	id, err := parsePositiveIntParam(c, "viewID")
	return models.SavedViewID(id), err
}

// sendSavedViewError maps a core saved view error to its status.
func (s *Server) sendSavedViewError(c fiber.Ctx, err error, failure string) error {
	switch {
	case errors.Is(err, core.ErrInvalidSavedView), errors.Is(err, models.ErrInvalidSavedViewDefinition):
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	case errors.Is(err, core.ErrSavedViewNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, "View not found", models.NotFoundErrorType)
	case errors.Is(err, core.ErrSavedViewForbidden), errors.Is(err, core.ErrSavedViewManaged), errors.Is(err, core.ErrSavedViewManagedDefault):
		return SendErrorWithType(c, fiber.StatusForbidden, err.Error(), models.AuthorizationErrorType)
	case errors.Is(err, core.ErrSavedViewStale):
		return SendErrorWithType(c, fiber.StatusConflict, "This view changed. Reload it and apply your changes again.", models.ConflictErrorType)
	case errors.Is(err, core.ErrSavedViewNameTaken), errors.Is(err, core.ErrSavedViewLimit), errors.Is(err, core.ErrSavedViewDefaultConflict):
		return SendErrorWithType(c, fiber.StatusConflict, err.Error(), models.ConflictErrorType)
	default:
		s.log.Error(failure, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, failure, models.GeneralErrorType)
	}
}

func (s *Server) handleListSavedViews(c fiber.Ctx) error {
	caller, ok := s.savedViewCaller(c)
	if !ok {
		return nil
	}
	list, err := core.ListSavedViews(c.Context(), s.store, caller)
	if err != nil {
		return s.sendSavedViewError(c, err, "Failed to list views")
	}
	return SendSuccess(c, fiber.StatusOK, list)
}

func (s *Server) handleGetSavedView(c fiber.Ctx) error {
	caller, ok := s.savedViewCaller(c)
	if !ok {
		return nil
	}
	id, err := parseSavedViewID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	view, err := core.GetSavedView(c.Context(), s.store, caller, id)
	if err != nil {
		return s.sendSavedViewError(c, err, "Failed to load view")
	}
	return SendSuccess(c, fiber.StatusOK, view)
}

func (s *Server) handleCreateSavedView(c fiber.Ctx) error {
	caller, ok := s.savedViewCaller(c)
	if !ok {
		return nil
	}
	var req models.CreateSavedViewRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	view, err := core.CreateSavedView(c.Context(), s.store, s.datasources, caller, &req)
	if err != nil {
		return s.sendSavedViewError(c, err, "Failed to create view")
	}
	auditSet(c, audit.SavedViewTarget(view), nil, audit.SavedViewFields(view))
	return SendSuccess(c, fiber.StatusCreated, view)
}

func (s *Server) handleUpdateSavedView(c fiber.Ctx) error {
	caller, ok := s.savedViewCaller(c)
	if !ok {
		return nil
	}
	id, err := parseSavedViewID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var req models.UpdateSavedViewRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	before, after, err := core.UpdateSavedView(c.Context(), s.store, s.datasources, caller, id, &req)
	if err != nil {
		return s.sendSavedViewError(c, err, "Failed to update view")
	}
	auditSet(c, audit.SavedViewTarget(after), audit.SavedViewFields(before), audit.SavedViewFields(after))
	return SendSuccess(c, fiber.StatusOK, after)
}

func (s *Server) handleDeleteSavedView(c fiber.Ctx) error {
	caller, ok := s.savedViewCaller(c)
	if !ok {
		return nil
	}
	id, err := parseSavedViewID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	view, err := core.DeleteSavedView(c.Context(), s.store, caller, id)
	if err != nil {
		return s.sendSavedViewError(c, err, "Failed to delete view")
	}
	auditSet(c, audit.SavedViewTarget(view), audit.SavedViewFields(view), nil)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "View deleted"})
}

func (s *Server) handleSetSavedViewDefault(c fiber.Ctx) error {
	caller, ok := s.savedViewCaller(c)
	if !ok {
		return nil
	}
	id, err := parseSavedViewID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	before, after, clearedID, err := core.SetSavedViewDefault(c.Context(), s.store, caller, id)
	if err != nil {
		return s.sendSavedViewError(c, err, "Failed to set the default view")
	}
	auditSet(c, audit.SavedViewTarget(after), audit.SavedViewFields(before), audit.SavedViewFields(after))
	auditDetail(c, "scope", after.Scope)
	auditDetail(c, "cleared_view_id", clearedID)
	return SendSuccess(c, fiber.StatusOK, after)
}

func (s *Server) handleClearSavedViewDefault(c fiber.Ctx) error {
	caller, ok := s.savedViewCaller(c)
	if !ok {
		return nil
	}
	id, err := parseSavedViewID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	before, after, err := core.ClearSavedViewDefault(c.Context(), s.store, caller, id)
	if err != nil {
		return s.sendSavedViewError(c, err, "Failed to clear the default view")
	}
	auditSet(c, audit.SavedViewTarget(after), audit.SavedViewFields(before), audit.SavedViewFields(after))
	auditDetail(c, "scope", after.Scope)
	return SendSuccess(c, fiber.StatusOK, after)
}
