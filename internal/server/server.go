package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/internal/sqlite"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/swagger" // Swagger handler

	// Import generated docs (will be created after running swag init)
	_ "github.com/mr-karan/logchef/docs"
)

// ServerOptions holds the dependencies required to create a new Server instance.
// This structure reflects the refactored approach using direct dependencies instead of services.
type ServerOptions struct {
	Config        *config.Config
	SQLite        *sqlite.DB
	ClickHouse    *clickhouse.Manager
	AlertsManager *alerts.Manager    // Alerts manager for manual resolution with Alertmanager notification.
	OIDCProvider  *auth.OIDCProvider // OIDC provider for authentication flows.
	FS            http.FileSystem    // Filesystem for serving static assets (frontend).
	Logger        *slog.Logger
	BuildInfo     string
	Version       string
}

// Server represents the core HTTP server, encapsulating the Fiber app instance
// and necessary dependencies like database connections and configuration.
type Server struct {
	app           *fiber.App
	config        *config.Config
	sqlite        *sqlite.DB
	clickhouse    *clickhouse.Manager
	alertsManager *alerts.Manager    // Alerts manager for manual resolution with Alertmanager notification.
	oidcProvider  *auth.OIDCProvider // Handles OIDC authentication logic.
	fs            http.FileSystem
	log           *slog.Logger
	buildInfo     string
	version       string
}

// @title LogChef API
// @version 1.0
// @description Log analytics and exploration platform for collecting, querying, and visualizing log data
// @termsOfService http://example.com/terms/
// @contact.name API Support
// @contact.url https://github.com/mr-karan/logchef
// @contact.email your-email@example.com
// @license.name MIT
// @license.url https://opensource.org/licenses/MIT
// @host localhost:8080
// @BasePath /api/v1
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

// New creates, configures, and returns a new Server instance.
// It initializes the Fiber application, sets up middleware, injects dependencies,
// and registers all application routes.
func New(opts ServerOptions) *Server {
	log := opts.Logger.With("component", "server")

	// Initialize Fiber app with custom error handler.
	app := fiber.New(fiber.Config{
		AppName:               "LogChef API v1",
		DisableStartupMessage: true, // Avoid default Fiber banner.
		ReadTimeout:           opts.Config.Server.HTTPServerTimeout,
		WriteTimeout:          opts.Config.Server.HTTPServerTimeout,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code // Use Fiber's error code if available.
			}
			// Log the internal error details.
			log.Error("request error", "path", c.Path(), "method", c.Method(), "error", err.Error())
			// Return a standardized JSON error response to the client.
			return SendError(c, code, err.Error()) // Assumes SendError is defined elsewhere.
		},
	})

	// Add essential middleware.
	// app.Use(recover.New()) // Recover from panics.
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed, // Prioritize speed over maximum compression
	})) // Compress responses

	// Add metrics middleware
	app.Use(metrics.Middleware())

	// Create the Server instance, injecting dependencies.
	s := &Server{
		app:           app,
		config:        opts.Config,
		sqlite:        opts.SQLite,
		clickhouse:    opts.ClickHouse,
		alertsManager: opts.AlertsManager,
		oidcProvider:  opts.OIDCProvider,
		fs:            opts.FS,
		log:           opts.Logger,
		buildInfo:     opts.BuildInfo,
		version:       opts.Version,
	}

	// Register all application routes.
	s.setupRoutes()

	return s
}

// setupRoutes configures all API endpoints, applying necessary middleware.
func (s *Server) setupRoutes() {
	// Swagger documentation route
	s.app.Get("/swagger/*", swagger.HandlerDefault)

	// Metrics endpoint
	s.app.Get("/metrics", metrics.MetricsHandler())

	api := s.app.Group("/api/v1")

	// --- Public Routes ---
	api.Get("/health", s.handleHealth)
	api.Get("/meta", s.handleGetMeta)

	// --- Authentication Routes ---
	api.Get("/auth/login", s.handleLogin)
	api.Get("/auth/callback", s.handleCallback)
	api.Post("/auth/logout", s.handleLogout)

	// --- Current User ("Me") Routes ---
	api.Get("/me", s.requireAuth, s.handleGetCurrentUser)
	api.Get("/me/teams", s.requireAuth, s.handleListCurrentUserTeams)

	// API Token Management for current user
	api.Get("/me/tokens", s.requireAuth, s.handleListAPITokens)
	api.Post("/me/tokens", s.requireAuth, s.handleCreateAPIToken)
	api.Delete("/me/tokens/:tokenID", s.requireAuth, s.handleDeleteAPIToken)

	// --- Admin Routes ---
	// These endpoints are only accessible to admin users for global management
	admin := api.Group("/admin", s.requireAuth, s.requireAdmin)
	{
		// User Management
		admin.Get("/users", s.handleListUsers)
		admin.Post("/users", s.handleCreateUser)
		admin.Get("/users/:userID", s.handleGetUser)
		admin.Put("/users/:userID", s.handleUpdateUser)
		admin.Delete("/users/:userID", s.handleDeleteUser)

		// Global Team Management
		admin.Get("/teams", s.handleListTeams)
		admin.Post("/teams", s.handleCreateTeam)
		admin.Delete("/teams/:teamID", s.handleDeleteTeam)

		// Global Source Management
		admin.Get("/sources", s.handleListSources) // Admin endpoint for listing all sources
		admin.Post("/sources", s.handleCreateSource)
		admin.Post("/sources/validate", s.handleValidateSourceConnection)
		admin.Delete("/sources/:sourceID", s.handleDeleteSource)
		admin.Get("/sources/:sourceID/stats", s.handleGetSourceStats) // Admin-only source stats

		// System Settings Management
		admin.Get("/settings", s.handleListSettings)
		admin.Get("/settings/category/:category", s.handleListSettingsByCategory)
		admin.Get("/settings/:key", s.handleGetSetting)
		admin.Put("/settings/:key", s.handleUpdateSetting)
		admin.Delete("/settings/:key", s.handleDeleteSetting)
		admin.Post("/settings/test-alertmanager", s.handleTestAlertmanagerConnection)
	}

	// --- Team Routes (Access controlled by team membership) ---
	// Regular users can view teams they belong to, team admins can manage membership and linked sources

	// Team details and members (requires team membership)
	api.Get("/teams/:teamID", s.requireAuth, s.requireTeamMember, s.handleGetTeam)

	// Team member management (requires team admin or global admin)
	teamMembers := api.Group("/teams/:teamID/members", s.requireAuth, s.requireTeamMember)
	{
		teamMembers.Get("/", s.handleListTeamMembers) // Any team member can view
		// Only team admins can add/remove members
		teamMembers.Post("/", s.requireTeamAdminOrGlobalAdmin, s.handleAddTeamMember)
		teamMembers.Delete("/:userID", s.requireTeamAdminOrGlobalAdmin, s.handleRemoveTeamMember)
	}

	// Team settings (requires team admin or global admin)
	api.Put("/teams/:teamID", s.requireAuth, s.requireTeamAdminOrGlobalAdmin, s.handleUpdateTeam)

	// Team Source Management (linking/unlinking)
	teamSources := api.Group("/teams/:teamID/sources", s.requireAuth, s.requireTeamMember)
	{
		teamSources.Get("/", s.handleListTeamSources) // Any team member can view team sources (basic info)

		// Only team admins can link/unlink sources
		teamSources.Post("/", s.requireTeamAdminOrGlobalAdmin, s.handleLinkSourceToTeam)
		teamSources.Delete("/:sourceID", s.requireTeamAdminOrGlobalAdmin, s.handleUnlinkSourceFromTeam)
	}

	// --- Team Source Operations (requires team membership) ---
	// These endpoints allow team members to interact with a specific source linked to their team
	teamSourceOps := api.Group("/teams/:teamID/sources/:sourceID", s.requireAuth, s.requireTeamMember, s.requireTeamHasSource)
	{
		// Get detailed source info including connection status and schema
		teamSourceOps.Get("/", s.handleGetTeamSource)
		teamSourceOps.Get("/stats", s.handleGetTeamSourceStats)

		// Query and explore logs
		teamSourceOps.Post("/logs/query", s.handleQueryLogs)
		teamSourceOps.Post("/logs/query/:queryID/cancel", s.handleCancelQuery)
		teamSourceOps.Get("/schema", s.handleGetSourceSchema)
		teamSourceOps.Post("/logs/histogram", s.handleGetHistogram)
		teamSourceOps.Post("/logs/context", s.handleGetLogContext)
		teamSourceOps.Post("/generate-sql", s.handleGenerateAISQL)

		// LogchefQL endpoints - query language parsing and translation
		teamSourceOps.Post("/logchefql/translate", s.handleLogchefQLTranslate) // Translate LogchefQL to SQL
		teamSourceOps.Post("/logchefql/validate", s.handleLogchefQLValidate)   // Validate LogchefQL syntax
		teamSourceOps.Post("/logchefql/query", s.handleLogchefQLQuery)         // Execute LogchefQL query directly

		// Field value exploration for sidebar
		teamSourceOps.Get("/fields/values", s.handleGetAllFieldValues)         // Get all LowCardinality field values
		teamSourceOps.Get("/fields/:fieldName/values", s.handleGetFieldValues) // Get values for a specific field

		// Collections (Saved Queries) scoped to Team & Source
		// Regular team members can view and use collections
		collections := teamSourceOps.Group("/collections")
		{
			collections.Get("/", s.handleListTeamSourceCollections)
			collections.Get("/:collectionID", s.handleGetTeamSourceCollection)

			// Only team editors, team admins, or global admins can manage collections
			collections.Post("/", s.requireCollectionManagement, s.handleCreateTeamSourceCollection)
			collections.Put("/:collectionID", s.requireCollectionManagement, s.handleUpdateTeamSourceCollection)
			collections.Delete("/:collectionID", s.requireCollectionManagement, s.handleDeleteTeamSourceCollection)
			collections.Patch("/:collectionID/bookmark", s.requireCollectionManagement, s.handleToggleQueryBookmark)
		}

		alerts := teamSourceOps.Group("/alerts")
		{
			alerts.Get("/", s.handleListAlerts)
			alerts.Get("/:alertID", s.handleGetAlert)
			alerts.Get("/:alertID/history", s.handleListAlertHistory)
			alerts.Post("/test", s.handleTestAlertQuery) // Test query endpoint (accessible to all team members)
			alerts.Post("/", s.requireTeamAdminOrGlobalAdmin, s.handleCreateAlert)
			alerts.Put("/:alertID", s.requireTeamAdminOrGlobalAdmin, s.handleUpdateAlert)
			alerts.Delete("/:alertID", s.requireTeamAdminOrGlobalAdmin, s.handleDeleteAlert)
			alerts.Post("/:alertID/resolve", s.requireTeamAdminOrGlobalAdmin, s.handleResolveAlert)
		}
	}

	// --- Static Asset and SPA Handling ---
	s.app.Use("/api/*", s.notFoundHandler) // Catch-all for API 404s
	s.app.Use("/assets", filesystem.New(filesystem.Config{
		Root:       s.fs,
		PathPrefix: "assets",
		Browse:     false,
		MaxAge:     86400,
	}))
	s.app.Use("/", filesystem.New(filesystem.Config{
		Root:         s.fs,
		Browse:       false,
		Index:        "index.html",
		NotFoundFile: "index.html",
	}))
}

// Start binds the server to the configured host and port and begins listening.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
	s.log.Info("starting http server", "address", addr)
	return s.app.Listen(addr)
}

// Shutdown gracefully shuts down the Fiber server within the given context timeout.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("shutting down http server")
	return s.app.ShutdownWithContext(ctx)
}
