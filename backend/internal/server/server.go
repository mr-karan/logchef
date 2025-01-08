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
			return c.Status(code).JSON(fiber.Map{
				"error": err.Error(),
			})
		},
	})

	// Add middleware
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${method} ${path}\n",
		TimeFormat: time.RFC3339,
	}))
	app.Use(cors.New())

	server := &Server{
		app:    app,
		config: cfg,
		svc:    svc,
		fs:     fs,
	}

	// Setup routes
	server.setupRoutes()

	return server
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

	// Serve frontend static files
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

// handleHealth handles the health check endpoint
func (s *Server) handleHealth(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": "ok",
		"time":   time.Now(),
	})
}

// handleListSources handles listing all sources
func (s *Server) handleListSources(c *fiber.Ctx) error {
	sources, err := s.svc.ListSources(c.Context())
	if err != nil {
		return fmt.Errorf("error listing sources: %w", err)
	}
	return c.JSON(sources)
}

// handleGetSource handles getting a single source
func (s *Server) handleGetSource(c *fiber.Ctx) error {
	id := c.Params("id")
	source, err := s.svc.GetSource(c.Context(), id)
	if err != nil {
		return fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return fiber.NewError(fiber.StatusNotFound, "source not found")
	}
	return c.JSON(source)
}

// handleCreateSource handles creating a new source
func (s *Server) handleCreateSource(c *fiber.Ctx) error {
	var source struct {
		Name       string `json:"name"`
		TableName  string `json:"table_name"`
		SchemaType string `json:"schema_type"`
		DSN        string `json:"dsn"`
	}

	if err := c.BodyParser(&source); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	created, err := s.svc.CreateSource(c.Context(), source.Name, source.TableName, source.SchemaType, source.DSN)
	if err != nil {
		return fmt.Errorf("error creating source: %w", err)
	}

	return c.Status(fiber.StatusCreated).JSON(created)
}

// handleDeleteSource handles deleting a source
func (s *Server) handleDeleteSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if err := s.svc.DeleteSource(c.Context(), id); err != nil {
		return fmt.Errorf("error deleting source: %w", err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
