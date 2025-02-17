package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"backend-v2/internal/auth"
	"backend-v2/internal/config"
	"backend-v2/internal/service"
	pkglogger "backend-v2/pkg/logger"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// Server represents the HTTP server
type Server struct {
	app    *fiber.App
	config *config.Config
	svc    *service.Service
	auth   auth.Service
	fs     http.FileSystem
	log    *slog.Logger
}

// New creates a new HTTP server
func New(cfg *config.Config, svc *service.Service, auth auth.Service, fs http.FileSystem) *Server {
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

	s := &Server{
		app:    app,
		config: cfg,
		svc:    svc,
		auth:   auth,
		fs:     fs,
		log:    pkglogger.Default().With("component", "server"),
	}

	// Setup routes
	s.setupRoutes()

	return s
}

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

	// Space routes
	s.app.Get("/api/v1/spaces", s.requireAuth, s.handleListSpaces)
	s.app.Post("/api/v1/spaces", s.requireAuth, s.handleCreateSpace)
	s.app.Get("/api/v1/spaces/:id", s.requireAuth, s.handleGetSpace)
	s.app.Put("/api/v1/spaces/:id", s.requireAuth, s.handleUpdateSpace)
	s.app.Delete("/api/v1/spaces/:id", s.requireAuth, s.handleDeleteSpace)
	s.app.Get("/api/v1/spaces/:id/teams", s.requireAuth, s.handleListSpaceTeams)
	s.app.Put("/api/v1/spaces/:id/teams/:teamId", s.requireAuth, s.handleUpdateSpaceTeamAccess)
	s.app.Delete("/api/v1/spaces/:id/teams/:teamId", s.requireAuth, s.handleRevokeSpaceTeamAccess)

	// Handle 404 for API routes
	s.app.Use("/api/*", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "API route not found",
			},
		})
	})

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
