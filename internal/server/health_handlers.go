package server

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// handleHealth handles the health check endpoint
func (s *Server) handleHealth(c *fiber.Ctx) error {
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"status":    "ok",
		"time":      time.Now(),
		"buildInfo": s.buildInfo,
	})
}
