package server

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// @Summary Health check endpoint
// @Description Returns the current status of the server along with build information
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Server status information"
// @Router /health [get]
// handleHealth responds to the health check endpoint, returning the server status,
// current time, and build information.
func (s *Server) handleHealth(c *fiber.Ctx) error {
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"status":    "ok",
		"time":      time.Now(),
		"buildInfo": s.buildInfo,
	})
}
