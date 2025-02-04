package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"backend-v2/internal/config"
	"backend-v2/internal/service"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// Server represents the HTTP server
type Server struct {
	app    *fiber.App
	config *config.Config
	svc    *service.Service
	fs     http.FileSystem
}

// New creates a new HTTP server
func New(cfg *config.Config, svc *service.Service, fs http.FileSystem) *Server {
	app := fiber.New(fiber.Config{
		AppName:               "LogChef API v1",
		DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			resp := Response{
				Status: "error",
				Data: fiber.Map{
					"error": err.Error(),
				},
			}
			return c.Status(code).JSON(resp)
		},
	})

	// Add middleware
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${method} ${path}\n",
		TimeFormat: time.RFC3339,
	}))
	app.Use(cors.New())

	s := &Server{
		app:    app,
		config: cfg,
		svc:    svc,
		fs:     fs,
	}

	// Setup routes
	s.setupRoutes()

	return s
}

func (s *Server) setupRoutes() {
	// API v1 routes
	v1 := s.app.Group("/api/v1")

	// Health check
	v1.Get("/health", s.handleHealth)

	// Sources
	sources := v1.Group("/sources")
	sources.Get("/", s.handleListSources)
	sources.Post("/", s.handleCreateSource)
	sources.Get("/:id", s.handleGetSource)
	sources.Delete("/:id", s.handleDeleteSource)
	sources.Post("/:id/logs/search", s.handleQueryLogs)

	// Handle 404 for all API routes
	s.app.Use("/api/*", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "API route not found",
			},
		})
	})

	// Serve frontend static files for all other routes
	s.app.Use("/", filesystem.New(filesystem.Config{
		Root:         s.fs,
		Browse:       false,
		Index:        "index.html",
		NotFoundFile: "index.html",
	}))
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
	return s.app.Listen(addr)
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.app.ShutdownWithContext(ctx)
}
