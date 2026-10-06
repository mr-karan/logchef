package server

import (
	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
)

// --- Meta Handlers ---

// handleGetMeta returns server metadata including version and configuration
// URL: GET /api/v1/meta
// Public endpoint - no authentication required
// @Summary Get server metadata
// @Description Returns server metadata including version and configuration information
// @Tags meta
// @Accept json
// @Produce json
// @Success 200 {object} core.MetaResponse "Server metadata"
// @Router /meta [get]
func (s *Server) handleGetMeta(c fiber.Ctx) error {
	// Runtime metadata can intentionally include public demo credentials. Never
	// let a browser or intermediary retain them after opt-out or rotation.
	c.Set(fiber.HeaderCacheControl, "no-store, private")

	var oidcIssuer string
	if s.oidcProvider != nil {
		oidcIssuer = s.oidcProvider.GetIssuer()
	}
	return SendSuccess(c, fiber.StatusOK, core.BuildMeta(s.config, s.version, oidcIssuer, s.oidcProvider != nil))
}
