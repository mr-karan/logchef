package server

import (
	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/provisioning"
)

// handleExportProvisioning is an admin-only endpoint that exports the current
// database state as a provisioning config JSON.
// GET /api/v1/admin/provisioning/export
func (s *Server) handleExportProvisioning(c *fiber.Ctx) error {
	cfg, err := provisioning.ExportConfig(c.Context(), s.sqlite)
	if err != nil {
		s.log.Error("failed to export provisioning config", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Failed to export provisioning config")
	}
	return SendSuccess(c, fiber.StatusOK, cfg)
}
