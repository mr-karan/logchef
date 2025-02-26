package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	pkglogger "github.com/mr-karan/logchef/pkg/logger"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/identity"
	"github.com/mr-karan/logchef/internal/logs"
	"github.com/mr-karan/logchef/internal/source"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// Server represents the HTTP server
type Server struct {
	app    *fiber.App
	config *config.Config

	// Domain-specific services
	sourceService   *source.Service
	logsService     *logs.Service
	identityService *identity.Service

	auth *auth.Service
	fs   http.FileSystem
	log  *slog.Logger

	// Build information
	buildInfo string
}

// New creates a new HTTP server
func New(cfg *config.Config, sourceService *source.Service, logsService *logs.Service,
	identityService *identity.Service, auth *auth.Service, fs http.FileSystem,
	buildInfo string) *Server {

	// Initialize Fiber app with configuration
	app := fiber.New(fiber.Config{
		AppName:               "LogChef API v1",
		DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			// Default 500 status code
			code := fiber.StatusInternalServerError

			// Check if it's a Fiber error
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}

			// Log the error
			logger := pkglogger.Default().With("component", "error_handler")
			logger.Error("request error", "path", c.Path(), "method", c.Method(), "error", err.Error())

			// Return JSON error response
			return SendError(c, code, err.Error())
		},
	})

	// Add middleware
	app.Use(recover.New())

	// Create server instance
	s := &Server{
		app:             app,
		config:          cfg,
		sourceService:   sourceService,
		logsService:     logsService,
		identityService: identityService,
		auth:            auth,
		fs:              fs,
		log:             pkglogger.Default().With("component", "server"),
		buildInfo:       buildInfo,
	}

	// Setup routes
	s.setupRoutes()

	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// Health check
	s.app.Get("/api/v1/health", s.handleHealth)

	// Auth routes (public)
	s.app.Get("/api/v1/auth/login", s.handleLogin)
	s.app.Get("/api/v1/auth/callback", s.handleCallback)
	s.app.Post("/api/v1/auth/logout", s.handleLogout)
	s.app.Get("/api/v1/auth/session", s.handleGetSession)

	// All routes below require authentication
	// Source routes
	s.app.Get("/api/v1/sources", s.requireAuth, s.handleListSources)
	s.app.Post("/api/v1/sources", s.requireAuth, s.handleCreateSource)
	s.app.Get("/api/v1/sources/:id", s.requireAuth, s.handleGetSource)
	s.app.Delete("/api/v1/sources/:id", s.requireAuth, s.handleDeleteSource)
	s.app.Post("/api/v1/sources/:id/logs/search", s.requireAuth, s.handleQueryLogs)
	s.app.Get("/api/v1/sources/:id/logs/timeseries", s.requireAuth, s.handleGetTimeSeries)
	s.app.Post("/api/v1/sources/:id/logs/context", s.requireAuth, s.handleGetLogContext)

	// User routes (admin only)
	s.app.Get("/api/v1/users", s.requireAuth, s.requireAdmin, s.handleListUsers)
	s.app.Post("/api/v1/users", s.requireAuth, s.requireAdmin, s.handleCreateUser)
	s.app.Get("/api/v1/users/:id", s.requireAuth, s.requireAdmin, s.handleGetUser)
	s.app.Put("/api/v1/users/:id", s.requireAuth, s.requireAdmin, s.handleUpdateUser)
	s.app.Delete("/api/v1/users/:id", s.requireAuth, s.requireAdmin, s.handleDeleteUser)

	// Team routes
	s.app.Get("/api/v1/teams", s.requireAuth, s.handleListTeams)
	s.app.Post("/api/v1/teams", s.requireAuth, s.handleCreateTeam)
	s.app.Get("/api/v1/teams/:id", s.requireAuth, s.handleGetTeam)
	s.app.Put("/api/v1/teams/:id", s.requireAuth, s.handleUpdateTeam)
	s.app.Delete("/api/v1/teams/:id", s.requireAuth, s.handleDeleteTeam)
	s.app.Get("/api/v1/teams/:id/members", s.requireAuth, s.handleListTeamMembers)
	s.app.Post("/api/v1/teams/:id/members", s.requireAuth, s.handleAddTeamMember)
	s.app.Delete("/api/v1/teams/:id/members/:userId", s.requireAuth, s.handleRemoveTeamMember)

	// Team source routes
	s.app.Get("/api/v1/teams/:id/sources", s.requireAuth, s.handleListTeamSources)
	s.app.Post("/api/v1/teams/:id/sources", s.requireAuth, s.handleAddTeamSource)
	s.app.Delete("/api/v1/teams/:id/sources/:sourceId", s.requireAuth, s.handleRemoveTeamSource)

	// Handle 404 for API routes
	s.app.Use("/api/*", s.notFoundHandler)

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
	s.log.Info("starting http server", "address", addr)
	return s.app.Listen(addr)
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("shutting down http server")
	return s.app.ShutdownWithContext(ctx)
}
