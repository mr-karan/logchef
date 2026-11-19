package server

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/provisioning"
	"github.com/mr-karan/logchef/pkg/models"
)

// handleExportProvisioning is an admin-only endpoint that exports the current
// database state as a provisioning config JSON.
// GET /api/v1/admin/provisioning/export
func (s *Server) handleExportProvisioning(c fiber.Ctx) error {
	cfg, err := provisioning.ExportConfig(c.Context(), s.store)
	if conflict, ok := errors.AsType[*provisioning.SecretRefConflictError](err); ok {
		return SendErrorWithType(c, fiber.StatusConflict, conflict.Error(), models.ConflictErrorType)
	}
	if err != nil {
		s.log.Error("failed to export provisioning config", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Failed to export provisioning config")
	}
	return SendSuccess(c, fiber.StatusOK, cfg)
}
