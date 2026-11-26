package server

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// destinationTestTimeout bounds a test send to every target of a destination.
const destinationTestTimeout = 30 * time.Second

// sendDestinationError maps a core destination error to its status.
func (s *Server) sendDestinationError(c fiber.Ctx, err error, action string) error {
	switch {
	case errors.Is(err, core.ErrInvalidDestination), errors.Is(err, core.ErrInvalidAlertConfiguration):
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	case errors.Is(err, core.ErrDestinationForbidden):
		return SendErrorWithType(c, fiber.StatusForbidden, err.Error(), models.AuthorizationErrorType)
	case errors.Is(err, core.ErrDestinationNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, "Notification destination not found", models.NotFoundErrorType)
	case errors.Is(err, core.ErrDestinationInUse):
		return SendErrorWithType(c, fiber.StatusConflict, err.Error(), models.ConflictErrorType)
	case errors.Is(err, access.ErrInsufficientScope):
		return sendInsufficientScope(c, principalFromLocals(c).User, err)
	default:
		s.log.Error("notification destination request failed", "action", action, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to "+action+" notification destination", models.GeneralErrorType)
	}
}

func parseDestinationID(c fiber.Ctx) (models.DestinationID, error) {
	id, err := parsePositiveIntParam(c, "destinationID")
	return models.DestinationID(id), err
}

// handleListDestinations lists the destinations the caller may use. An OAuth
// client gets summaries: no recipient address, user ID or receiver host.
func (s *Server) handleListDestinations(c fiber.Ctx) error {
	if p := principalFromLocals(c); p.Method == access.AuthOAuth {
		summaries, err := core.ListDestinationSummariesForPrincipal(c.Context(), s.store, p)
		if err != nil {
			return s.sendDestinationError(c, err, "list")
		}
		return SendSuccess(c, fiber.StatusOK, summaries)
	}
	list, err := core.ListDestinationsForUser(c.Context(), s.store, currentUser(c))
	if err != nil {
		return s.sendDestinationError(c, err, "list")
	}
	return SendSuccess(c, fiber.StatusOK, list)
}

func (s *Server) handleCreateDestination(c fiber.Ctx) error {
	var req models.DestinationRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	d, err := core.CreateDestination(c.Context(), s.store, currentUser(c), &req)
	if err != nil {
		return s.sendDestinationError(c, err, "create")
	}
	auditSet(c, audit.NotificationDestinationTarget(d), nil, audit.NotificationDestinationFields(d))
	return SendSuccess(c, fiber.StatusCreated, d)
}

func (s *Server) handleUpdateDestination(c fiber.Ctx) error {
	id, err := parseDestinationID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var req models.DestinationRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	before, after, err := core.UpdateDestination(c.Context(), s.store, currentUser(c), id, &req)
	if err != nil {
		return s.sendDestinationError(c, err, "update")
	}
	auditSet(c, audit.NotificationDestinationTarget(after), audit.NotificationDestinationFields(before), audit.NotificationDestinationFields(after))
	return SendSuccess(c, fiber.StatusOK, after)
}

func (s *Server) handleDeleteDestination(c fiber.Ctx) error {
	id, err := parseDestinationID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	d, err := core.DeleteDestination(c.Context(), s.store, currentUser(c), id)
	if err != nil {
		return s.sendDestinationError(c, err, "delete")
	}
	auditSet(c, audit.NotificationDestinationTarget(d), audit.NotificationDestinationFields(d), nil)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Notification destination deleted"})
}

// handleTestDestination sends a test message to a saved destination, or to
// an unsaved definition, and returns one result per target.
func (s *Server) handleTestDestination(c fiber.Ctx) error {
	var req models.TestDestinationRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	d, err := core.DestinationForTest(c.Context(), s.store, currentUser(c), &req)
	if err != nil {
		return s.sendDestinationError(c, err, "test")
	}
	if d.ID != 0 {
		auditTarget(c, audit.NotificationDestinationTarget(d))
	} else {
		auditTarget(c, audit.NotificationDestinationTypeTarget(d.Type))
	}
	ctx, cancel := context.WithTimeout(c.Context(), destinationTestTimeout)
	defer cancel()
	results := s.alertsManager.TestDestination(ctx, d)
	ok := !slices.ContainsFunc(results, func(r models.DestinationTestResult) bool { return !r.OK })
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"ok": ok, "results": results})
}

// legacyAlertTargetKey returns the first alert field in body that
// destinations replaced. A request that names one is refused even with an
// empty value, so a script that clears targets with [] cannot look like it
// succeeded. A body that is not a JSON object has none; binding reports it.
func legacyAlertTargetKey(body []byte) string {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(body, &keys); err != nil {
		return ""
	}
	for _, key := range []string{"recipient_user_ids", "webhook_urls"} {
		if _, ok := keys[key]; ok {
			return key
		}
	}
	return ""
}

func sendLegacyAlertTargetError(c fiber.Ctx, key string) error {
	return SendErrorWithType(c, fiber.StatusBadRequest,
		key+" is no longer accepted: alert targets are notification destinations. Create a destination and pass its id in destination_ids.",
		models.ValidationErrorType)
}
