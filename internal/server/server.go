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
	"github.com/mr-karan/logchef/internal/saved_queries"
	"github.com/mr-karan/logchef/internal/source"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// ServerOptions contains all dependencies needed to create a new Server
type ServerOptions struct {
	Config            *config.Config
	SourceService     *source.Service
	LogsService       *logs.Service
	IdentityService   *identity.Service
	SavedQueryService *saved_queries.Service
	Auth              *auth.Service
	FS                http.FileSystem
	Logger            *slog.Logger
	BuildInfo         string
}

// Server represents the HTTP server
type Server struct {
	app    *fiber.App
	config *config.Config

	// Domain-specific services
	sourceService     *source.Service
	logsService       *logs.Service
	identityService   *identity.Service
	savedQueryService *saved_queries.Service
	auth              *auth.Service

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
		app:               app,
		config:            opts.Config,
		sourceService:     opts.SourceService,
		logsService:       opts.LogsService,
		identityService:   opts.IdentityService,
		savedQueryService: opts.SavedQueryService,
		auth:              opts.Auth,
		fs:                opts.FS,
		log:               opts.Logger,
		buildInfo:         opts.BuildInfo,
	}

	// Setup routes
	s.setupRoutes()

	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// Base path for API version 1
	api := s.app.Group("/api/v1")

	// Public routes
	api.Get("/health", s.handleHealth)

	// Auth routes
	api.Get("/auth/login", s.handleLogin)
	api.Get("/auth/callback", s.handleCallback)
	api.Post("/auth/logout", s.handleLogout)
	api.Get("/auth/session", s.requireAuth, s.handleGetSession)

	// --- Admin Routes ---
	api.Get("/admin/users", s.requireAuth, s.requireAdmin, s.handleListUsers)
	api.Post("/admin/users", s.requireAuth, s.requireAdmin, s.handleCreateUser)
	api.Get("/admin/users/:userID", s.requireAuth, s.requireAdmin, s.handleGetUser)
	api.Put("/admin/users/:userID", s.requireAuth, s.requireAdmin, s.handleUpdateUser)
	api.Delete("/admin/users/:userID", s.requireAuth, s.requireAdmin, s.handleDeleteUser)

	api.Get("/admin/teams", s.requireAuth, s.requireAdmin, s.handleListTeams)
	api.Post("/admin/teams", s.requireAuth, s.requireAdmin, s.handleCreateTeam)
	api.Put("/admin/teams/:teamID", s.requireAuth, s.requireAdmin, s.handleUpdateTeam)
	api.Delete("/admin/teams/:teamID", s.requireAuth, s.requireAdmin, s.handleDeleteTeam)

	api.Get("/admin/sources", s.requireAuth, s.requireAdmin, s.handleListSources)
	api.Get("/admin/sources/:sourceID", s.requireAuth, s.requireAdmin, s.handleGetSource)
	api.Post("/admin/sources", s.requireAuth, s.requireAdmin, s.handleCreateSource)
	api.Post("/admin/sources/validate", s.requireAuth, s.requireAdmin, s.handleValidateSourceConnection)
	api.Delete("/admin/sources/:sourceID", s.requireAuth, s.requireAdmin, s.handleDeleteSource)
	api.Get("/admin/sources/:sourceID/stats", s.requireAuth, s.requireAdmin, s.handleGetSourceStats)

	// --- Personal Routes ---
	api.Get("/users/me/teams", s.requireAuth, s.handleListUserTeams)

	// --- Team Routes ---

	// Team Info
	api.Get("/teams/:teamID", s.requireAuth, s.requireTeamMember, s.handleGetTeam)

	// Team Members
	api.Get("/teams/:teamID/members", s.requireAuth, s.requireTeamMember, s.handleListTeamMembers)
	api.Post("/teams/:teamID/members", s.requireAuth, s.requireTeamMember, s.requireTeamAdminOrGlobalAdmin, s.handleAddTeamMember)
	api.Delete("/teams/:teamID/members/:userID", s.requireAuth, s.requireTeamMember, s.requireTeamAdminOrGlobalAdmin, s.handleRemoveTeamMember)

	// Team Sources
	api.Get("/teams/:teamID/sources", s.requireAuth, s.requireTeamMember, s.handleListTeamSources)
	api.Post("/teams/:teamID/sources", s.requireAuth, s.requireTeamMember, s.requireTeamAdminOrGlobalAdmin, s.handleAddTeamSource)
	api.Delete("/teams/:teamID/sources/:sourceID", s.requireAuth, s.requireTeamMember, s.requireTeamAdminOrGlobalAdmin, s.handleRemoveTeamSource)

	// Team Source Details & Actions
	api.Get("/teams/:teamID/sources/:sourceID", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleGetTeamSource)
	api.Post("/teams/:teamID/sources/:sourceID/logs/query", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleQueryTeamSourceLogs)
	api.Get("/teams/:teamID/sources/:sourceID/schema", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleGetTeamSourceSchema)
	api.Post("/teams/:teamID/sources/:sourceID/logs/histogram", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleGetTeamSourceHistogram)

	// Team Source Queries
	api.Get("/teams/:teamID/sources/:sourceID/queries", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleListTeamSourceQueries)
	api.Post("/teams/:teamID/sources/:sourceID/queries", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleCreateTeamSourceQuery)
	api.Get("/teams/:teamID/sources/:sourceID/queries/:queryID", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleGetTeamSourceQuery)
	api.Put("/teams/:teamID/sources/:sourceID/queries/:queryID", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleUpdateTeamSourceQuery)
	api.Delete("/teams/:teamID/sources/:sourceID/queries/:queryID", s.requireAuth, s.requireTeamMember, s.requireTeamSourceAccess, s.handleDeleteTeamSourceQuery)

	// Handle 404 for API routes
	s.app.Use("/api/*", s.notFoundHandler)

	// Asset handling
	s.app.Use("/assets/*", filesystem.New(filesystem.Config{
		Root:       s.fs,
		PathPrefix: "assets",
		Browse:     false,
		MaxAge:     86400,
	}))

	// SPA index.html handler
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
