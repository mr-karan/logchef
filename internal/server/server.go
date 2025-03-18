package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/identity"
	"github.com/mr-karan/logchef/internal/logs"
	"github.com/mr-karan/logchef/internal/source"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// ServerOptions contains all dependencies needed to create a new Server
type ServerOptions struct {
	Config           *config.Config
	SourceService    *source.Service
	LogsService      *logs.Service
	IdentityService  *identity.Service
	TeamQueryService *logs.TeamQueryService
	Auth             *auth.Service
	FS               http.FileSystem
	Logger           *slog.Logger
	BuildInfo        string
}

// Server represents the HTTP server
type Server struct {
	app    *fiber.App
	config *config.Config

	// Domain-specific services
	sourceService    *source.Service
	logsService      *logs.Service
	identityService  *identity.Service
	teamQueryService *logs.TeamQueryService
	auth             *auth.Service

	fs  http.FileSystem
	log *slog.Logger

	// Build information
	buildInfo string
}

// New creates a new HTTP server
func New(opts ServerOptions) *Server {
	log := opts.Logger.With("component", "server")

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
			log.Error("request error", "path", c.Path(), "method", c.Method(), "error", err.Error())

			// Return JSON error response
			return SendError(c, code, err.Error())
		},
	})

	// Add middleware
	app.Use(recover.New())

	// Create server instance
	s := &Server{
		app:              app,
		config:           opts.Config,
		sourceService:    opts.SourceService,
		logsService:      opts.LogsService,
		identityService:  opts.IdentityService,
		teamQueryService: opts.TeamQueryService,
		auth:             opts.Auth,
		fs:               opts.FS,
		log:              opts.Logger,
		buildInfo:        opts.BuildInfo,
	}

	// Setup routes
	s.setupRoutes()

	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// Group API version 1 endpoints
	api := s.app.Group("/api/v1")

	// Public routes (no auth required)
	api.Get("/health", s.handleHealth)

	// Auth routes
	auth := api.Group("/auth")
	auth.Get("/login", s.handleLogin)
	auth.Get("/callback", s.handleCallback)
	auth.Post("/logout", s.handleLogout)
	auth.Get("/session", s.handleGetSession)

	// Admin-only routes
	admin := api.Group("/admin", s.requireAuth, s.requireAdmin)
	{
		// User management
		admin.Get("/users", s.handleListUsers)
		admin.Post("/users", s.handleCreateUser)
		admin.Get("/users/:userID", s.handleGetUser)
		admin.Put("/users/:userID", s.handleUpdateUser)
		admin.Delete("/users/:userID", s.handleDeleteUser)

		// Team management
		admin.Get("/teams", s.handleListTeams)
		admin.Post("/teams", s.handleCreateTeam)
		admin.Put("/teams/:teamID", s.handleUpdateTeam)
		admin.Delete("/teams/:teamID", s.handleDeleteTeam)

		// Source management
		admin.Get("/sources", s.handleListSources)
		admin.Get("/sources/:sourceID", s.handleGetSource)
		admin.Post("/sources", s.handleCreateSource)
		admin.Post("/sources/validate", s.handleValidateSourceConnection)
		admin.Delete("/sources/:sourceID", s.handleDeleteSource)
		admin.Get("/sources/:sourceID/stats", s.handleGetSourceStats)
	}

	// Personal routes (authenticated user)
	me := api.Group("/users/me", s.requireAuth)
	{
		me.Get("/teams", s.handleListUserTeams)
		me.Get("/queries", s.handleListUserQueries)
	}

	// Team-specific routes
	teams := api.Group("/teams", s.requireAuth)
	{
		teams.Get("/:teamID", s.requireTeamMember, s.handleGetTeam)

		// Team members
		teamMembers := teams.Group("/:teamID/members", s.requireTeamMember)
		{
			teamMembers.Get("/", s.handleListTeamMembers)
			teamMembers.Post("/", s.requireTeamAdminOrGlobalAdmin, s.handleAddTeamMember)
			teamMembers.Delete("/:userID", s.requireTeamAdminOrGlobalAdmin, s.handleRemoveTeamMember)
		}

		// Team sources
		teamSources := teams.Group("/:teamID/sources", s.requireTeamMember)
		{
			teamSources.Get("/", s.handleListTeamSources)
			teamSources.Post("/", s.requireTeamAdminOrGlobalAdmin, s.handleAddTeamSource)
			teamSources.Delete("/:sourceID", s.requireTeamAdminOrGlobalAdmin, s.handleRemoveTeamSource)

			// Team source queries
			teamSources.Get("/:sourceID", s.requireTeamSourceAccess, s.handleGetTeamSource)
			teamSources.Post("/:sourceID/logs/query", s.requireTeamSourceAccess, s.handleQueryTeamSourceLogs)
			teamSources.Get("/:sourceID/schema", s.requireTeamSourceAccess, s.handleGetTeamSourceSchema)
			teamSources.Get("/:sourceID/queries", s.requireTeamSourceAccess, s.handleListTeamSourceQueries)
			teamSources.Post("/:sourceID/queries", s.requireTeamSourceAccess, s.handleCreateTeamSourceQuery)
		}

		// Team queries
		teamQueries := teams.Group("/:teamID/queries", s.requireTeamMember)
		{
			teamQueries.Get("/", s.handleListTeamQueries)
			teamQueries.Post("/", s.handleCreateTeamQuery)
			teamQueries.Get("/:queryID", s.handleGetTeamQuery)
			teamQueries.Put("/:queryID", s.handleUpdateTeamQuery)
			teamQueries.Delete("/:queryID", s.handleDeleteTeamQuery)
		}
	}

	// Handle 404 for API routes
	s.app.Use("/api/*", s.notFoundHandler)

	// Asset handling with proper MIME types
	s.app.Use("/assets/*", filesystem.New(filesystem.Config{
		Root:       s.fs,
		PathPrefix: "assets",
		Browse:     false,
		MaxAge:     86400, // Cache assets for one day
	}))

	// For all other routes, serve the SPA index.html
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
