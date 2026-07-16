package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/swagger" // Swagger handler

	// Import generated docs (will be created after running swag init)
	_ "github.com/mr-karan/logchef/docs"
)

// ServerOptions holds the dependencies required to create a new Server instance.
// This structure reflects the refactored approach using direct dependencies instead of services.
type ServerOptions struct {
	Config        *config.Config
	SQLite        store.Store
	ClickHouse    *clickhouse.Manager
	Datasources   *datasource.Service
	AlertsManager *alerts.Manager    // Alerts manager for manual resolution and notifications.
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
	sqlite        store.Store
	clickhouse    *clickhouse.Manager
	datasources   *datasource.Service
	alertsManager *alerts.Manager    // Alerts manager for manual resolution and notifications.
	oidcProvider  *auth.OIDCProvider // Handles OIDC authentication logic.
	fs            http.FileSystem
	log           *slog.Logger
	buildInfo     string
	version       string

	stop chan struct{} // closed by Shutdown to stop background maintenance loops
	wg   sync.WaitGroup
}

// @title Logchef API
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
		AppName:               "Logchef API v1",
		DisableStartupMessage: true, // Avoid default Fiber banner.
		ReadTimeout:           opts.Config.Server.HTTPServerTimeout,
		WriteTimeout:          opts.Config.Server.HTTPServerTimeout,
		IdleTimeout:           30 * time.Second, // Free idle keep-alive connection buffers quickly
		// Request bodies here are queries and small JSON payloads, never bulk
		// data, so a few MB is generous. This is a coarse transport-level
		// backstop; the LogchefQL parser additionally enforces its own
		// (much smaller) query length/nesting limits regardless of this cap.
		BodyLimit: 4 * 1024 * 1024, // 4MB
		// Client-IP resolution behind a reverse proxy. The check is always on:
		// with an empty TrustedProxies list Fiber returns the direct peer IP
		// (default/current behavior); it reads ProxyHeader only when the direct
		// peer is a configured trusted proxy, so an untrusted caller can't spoof
		// it. EnableIPValidation makes c.IP() return the first *valid* header
		// entry (falling back to the peer) rather than the raw header string.
		EnableTrustedProxyCheck: true,
		TrustedProxies:          opts.Config.Server.TrustedProxies,
		ProxyHeader:             opts.Config.Server.ProxyHeader,
		EnableIPValidation:      true,
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
	app.Use(recoverMiddleware(log))
	app.Use(compress.New(compress.Config{
		// SSE streams (live tail) must not be buffered/compressed: the compressor
		// holds the whole body, which never completes for an open stream.
		Next: func(c *fiber.Ctx) bool {
			return strings.HasSuffix(c.Path(), "/logs/tail")
		},
		Level: compress.LevelBestSpeed, // Prioritize speed over maximum compression
	})) // Compress responses

	// Add metrics middleware
	app.Use(metrics.Middleware())

	// Add request logging middleware
	app.Use(requestLogger(log))

	// Create the Server instance, injecting dependencies.
	s := &Server{
		app:           app,
		config:        opts.Config,
		sqlite:        opts.SQLite,
		clickhouse:    opts.ClickHouse,
		datasources:   opts.Datasources,
		alertsManager: opts.AlertsManager,
		oidcProvider:  opts.OIDCProvider,
		fs:            opts.FS,
		log:           opts.Logger,
		buildInfo:     opts.BuildInfo,
		version:       opts.Version,
		stop:          make(chan struct{}),
	}

	// Register all application routes.
	s.setupRoutes()
	s.startBackgroundCleanup()

	return s
}

func recoverMiddleware(log *slog.Logger) fiber.Handler {
	return fiberrecover.New(fiberrecover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, panicValue interface{}) {
			log.Error("panic recovered",
				"path", c.Path(),
				"method", c.Method(),
				"panic", panicValue,
				"stack", string(debug.Stack()),
			)
		},
	})
}

// setupRoutes configures all API endpoints, applying necessary middleware.
func (s *Server) setupRoutes() {
	// Swagger documentation route
	s.app.Get("/swagger/*", swagger.HandlerDefault)

	// Metrics endpoint
	s.app.Get("/metrics", metrics.MetricsHandler())

	api := s.app.Group("/api/v1")

	// Build rate-limit middleware once so limiter state persists across
	// requests. When disabled, both helpers below register no middleware.
	var authLimiter, queryLimiter fiber.Handler
	if s.config.RateLimit.Enabled {
		authLimiter = authRateLimitMiddleware(s.config.RateLimit.AuthPerIPPerMinute, s.config.RateLimit.AuthGlobalPerMinute)
		queryLimiter = queryRateLimitMiddleware(s.config.RateLimit.QueryPerUserPerMinute)
	}
	// withAuthLimit / withQueryLimit prepend the relevant limiter to a route's
	// handler chain when limiting is enabled, and are no-ops otherwise. The
	// query limiter runs after the group-level requireAuth, so the user context
	// is populated when it computes its per-user key.
	withAuthLimit := func(handlers ...fiber.Handler) []fiber.Handler {
		if authLimiter != nil {
			return append([]fiber.Handler{authLimiter}, handlers...)
		}
		return handlers
	}
	withQueryLimit := func(handlers ...fiber.Handler) []fiber.Handler {
		if queryLimiter != nil {
			return append([]fiber.Handler{queryLimiter}, handlers...)
		}
		return handlers
	}

	// --- Public Routes ---
	api.Get("/health", s.handleHealth)
	api.Get("/meta", s.handleGetMeta)

	// --- Authentication Routes ---
	// The unauthenticated auth/token endpoints are rate-limited per client IP
	// (plus an optional global cap) to blunt credential-stuffing / brute force.
	api.Get("/auth/login", withAuthLimit(s.handleLogin)...)
	api.Post("/auth/local/login", withAuthLimit(s.handleLocalLogin)...)
	api.Get("/auth/callback", withAuthLimit(s.handleCallback)...)
	api.Post("/auth/logout", s.handleLogout)

	// --- CLI Authentication ---
	api.Post("/cli/token", withAuthLimit(s.handleCLITokenExchange)...)

	// --- Current User ("Me") Routes ---
	api.Get("/me", s.requireAuth, s.requireTokenScope(models.TokenScopeProfileRead), s.handleGetCurrentUser)
	api.Get("/me/teams", s.requireAuth, s.requireTokenScope(models.TokenScopeTeamsRead), s.handleListCurrentUserTeams)
	api.Get("/me/preferences", s.requireAuth, s.requireTokenScope(models.TokenScopeProfileRead), s.handleGetUserPreferences)
	api.Put("/me/preferences", s.requireAuth, s.requireTokenScope(models.TokenScopeProfileWrite), s.handleUpdateUserPreferences)
	api.Get("/me/query-history", s.requireAuth, s.requireTokenScope(models.TokenScopeLogsRead), s.handleListQueryHistory)

	// Share links for ad hoc queries. Share payload access is still scoped by
	// team membership and source linkage in the handler.
	api.Get("/query-shares/:token", s.requireAuth, s.requireTokenScope(models.TokenScopeQuerySharesRead), s.handleGetQueryShare)
	api.Delete("/query-shares/:token", s.requireAuth, s.requireTokenScope(models.TokenScopeQuerySharesWrite), s.handleDeleteQueryShare)

	// API Token Management for current user
	api.Get("/me/tokens", s.requireAuth, s.requireTokenScope(models.TokenScopeTokensRead), s.handleListAPITokens)
	api.Post("/me/tokens", s.requireAuth, s.requireTokenScope(models.TokenScopeTokensWrite), s.handleCreateAPIToken)
	api.Delete("/me/tokens/:tokenID", s.requireAuth, s.requireTokenScope(models.TokenScopeTokensWrite), s.handleDeleteAPIToken)

	// --- User Listing (for team admins to add members) ---
	// This endpoint is accessible to team admins (users who are admin of at least one team)
	// or global admins, to allow them to select users when adding members to their teams.
	api.Get("/users", s.requireAuth, s.requireTokenScope(models.TokenScopeUsersRead), s.requireAnyTeamAdmin, s.handleListUsers)

	// --- Admin Routes ---
	// These endpoints are only accessible to admin users for global management
	admin := api.Group("/admin", s.requireAuth, s.requireAdmin)
	// User Management
	admin.Get("/users", s.requireTokenScope(models.TokenScopeUsersRead), s.handleListUsers)
	admin.Post("/users", s.requireTokenScope(models.TokenScopeUsersWrite), s.handleCreateUser)
	admin.Get("/users/:userID", s.requireTokenScope(models.TokenScopeUsersRead), s.handleGetUser)
	admin.Put("/users/:userID", s.requireTokenScope(models.TokenScopeUsersWrite), s.handleUpdateUser)
	admin.Delete("/users/:userID", s.requireTokenScope(models.TokenScopeUsersWrite), s.handleDeleteUser)
	admin.Get("/service-accounts", s.requireTokenScope(models.TokenScopeUsersRead), s.handleListServiceAccounts)
	admin.Post("/service-accounts", s.requireTokenScope(models.TokenScopeUsersWrite), s.handleCreateServiceAccount)
	admin.Delete("/service-accounts/:userID", s.requireTokenScope(models.TokenScopeUsersWrite), s.handleDeleteServiceAccount)
	admin.Get("/service-accounts/:userID/tokens", s.requireTokenScope(models.TokenScopeTokensRead), s.handleListServiceAccountTokens)
	admin.Post("/service-accounts/:userID/tokens", s.requireTokenScope(models.TokenScopeTokensWrite), s.handleCreateServiceAccountToken)
	admin.Delete("/service-accounts/:userID/tokens/:tokenID", s.requireTokenScope(models.TokenScopeTokensWrite), s.handleDeleteServiceAccountToken)
	admin.Get("/service-accounts/:userID/teams", s.requireTokenScope(models.TokenScopeTeamsRead), s.handleListServiceAccountTeams)
	admin.Post("/service-accounts/:userID/teams", s.requireTokenScope(models.TokenScopeTeamsWrite), s.handleAddServiceAccountToTeam)
	admin.Delete("/service-accounts/:userID/teams/:teamID", s.requireTokenScope(models.TokenScopeTeamsWrite), s.handleRemoveServiceAccountFromTeam)

	// Saved Queries (admin browse): every saved query across all sources, each
	// marked runnable for the caller. The user-facing /saved-queries stays
	// source-access-gated and is untouched.
	admin.Get("/saved-queries", s.requireTokenScope(models.TokenScopeSavedQueriesRead), s.handleAdminListSavedQueries)

	// Global Team Management
	admin.Get("/teams", s.requireTokenScope(models.TokenScopeTeamsRead), s.handleListTeams)
	admin.Post("/teams", s.requireTokenScope(models.TokenScopeTeamsWrite), s.handleCreateTeam)
	admin.Delete("/teams/:teamID", s.requireTokenScope(models.TokenScopeTeamsWrite), s.requireTeamNotManaged, s.handleDeleteTeam)

	// Global Source Management
	admin.Get("/sources", s.requireTokenScope(models.TokenScopeSourcesRead), s.handleListSources) // Admin endpoint for listing all sources
	admin.Post("/sources", s.requireTokenScope(models.TokenScopeSourcesWrite), s.handleCreateSource)
	admin.Post("/sources/validate", s.requireTokenScope(models.TokenScopeSourcesWrite), s.handleValidateSourceConnection)
	admin.Put("/sources/:sourceID", s.requireTokenScope(models.TokenScopeSourcesWrite), s.requireSourceNotManaged, s.handleUpdateSource)
	admin.Delete("/sources/:sourceID", s.requireTokenScope(models.TokenScopeSourcesWrite), s.requireSourceNotManaged, s.handleDeleteSource)
	admin.Get("/sources/:sourceID/stats", s.requireTokenScope(models.TokenScopeSourcesRead), s.handleGetSourceStats) // Admin-only source stats

	// Provisioning Export
	admin.Get("/provisioning/export", s.requireTokenScope(models.TokenScopeSettingsRead), s.handleExportProvisioning)

	// System Settings Management
	admin.Get("/settings", s.requireTokenScope(models.TokenScopeSettingsRead), s.handleListSettings)
	admin.Get("/settings/category/:category", s.requireTokenScope(models.TokenScopeSettingsRead), s.handleListSettingsByCategory)
	admin.Get("/settings/:key", s.requireTokenScope(models.TokenScopeSettingsRead), s.handleGetSetting)
	admin.Put("/settings/:key", s.requireTokenScope(models.TokenScopeSettingsWrite), s.handleUpdateSetting)
	admin.Delete("/settings/:key", s.requireTokenScope(models.TokenScopeSettingsWrite), s.handleDeleteSetting)
	admin.Post("/settings/test-email", s.requireTokenScope(models.TokenScopeSettingsWrite), s.requireAlertsEnabled, s.handleTestEmail)
	admin.Post("/settings/test-webhook", s.requireTokenScope(models.TokenScopeSettingsWrite), s.requireAlertsEnabled, s.handleTestWebhook)

	// --- Team Routes (Access controlled by team membership) ---
	// Regular users can view teams they belong to, team admins can manage membership and linked sources

	// Team details and members (requires team membership)
	api.Get("/teams/:teamID", s.requireAuth, s.requireTokenScope(models.TokenScopeTeamsRead), s.requireTeamMember, s.handleGetTeam)

	// Team member management (requires team admin or global admin)
	teamMembers := api.Group("/teams/:teamID/members", s.requireAuth, s.requireTeamMember)
	teamMembers.Get("/", s.requireTokenScope(models.TokenScopeTeamsRead), s.handleListTeamMembers) // Any team member can view
	// Team admins can add/remove members even on managed teams (day-to-day operations)
	teamMembers.Post("/", s.requireTokenScope(models.TokenScopeTeamsWrite), s.requireTeamAdminOrGlobalAdmin, s.handleAddTeamMember)
	teamMembers.Delete("/:userID", s.requireTokenScope(models.TokenScopeTeamsWrite), s.requireTeamAdminOrGlobalAdmin, s.handleRemoveTeamMember)

	// Team settings — managed guard only on structural changes (rename/description)
	api.Put("/teams/:teamID", s.requireAuth, s.requireTokenScope(models.TokenScopeTeamsWrite), s.requireTeamNotManaged, s.requireTeamAdminOrGlobalAdmin, s.handleUpdateTeam)

	// Collections (cross-team curation lists). Each user gets an auto-created
	// personal collection on first GET /api/v1/collections. Other collections
	// are invite-only with two roles: owner (full control) and member (read).
	collections := api.Group("/collections", s.requireAuth)
	collections.Get("/", s.requireTokenScope(models.TokenScopeCollectionsRead), s.handleListCollections)
	collections.Get("/:collectionID", s.requireTokenScope(models.TokenScopeCollectionsRead), s.handleGetCollection)
	collections.Get("/:collectionID/members", s.requireTokenScope(models.TokenScopeCollectionsRead), s.handleListCollectionMembers)
	collections.Get("/:collectionID/items", s.requireTokenScope(models.TokenScopeCollectionsRead), s.handleListCollectionItems)
	// Ownership-based: any authenticated user can create a collection; all
	// per-collection mutations are gated on the caller's collection role inside
	// core/collections.go (owner manages members/items + delete; editor curates).
	// Collection membership never grants source access — that stays the hard gate.
	collections.Post("/", s.requireTokenScope(models.TokenScopeCollectionsWrite), s.handleCreateCollection)
	collections.Put("/:collectionID", s.requireTokenScope(models.TokenScopeCollectionsWrite), s.handleUpdateCollection)
	collections.Delete("/:collectionID", s.requireTokenScope(models.TokenScopeCollectionsWrite), s.handleDeleteCollection)
	collections.Post("/:collectionID/members", s.requireTokenScope(models.TokenScopeCollectionsWrite), s.handleAddCollectionMember)
	collections.Delete("/:collectionID/members/:userID", s.requireTokenScope(models.TokenScopeCollectionsWrite), s.handleRemoveCollectionMember)
	collections.Post("/:collectionID/items", s.requireTokenScope(models.TokenScopeCollectionsWrite), s.handleAddCollectionItem)
	collections.Delete("/:collectionID/items/:queryID", s.requireTokenScope(models.TokenScopeCollectionsWrite), s.handleRemoveCollectionItem)

	// Saved Queries (cross-team, source-scoped). Visibility: any user with source
	// access via any team. Edit/delete: creator + global admin (legacy queries
	// without created_by are global-admin-only).
	savedQueries := api.Group("/saved-queries", s.requireAuth)
	savedQueries.Get("/", s.requireTokenScope(models.TokenScopeSavedQueriesRead), s.handleListSavedQueries)
	savedQueries.Post("/", s.requireTokenScope(models.TokenScopeSavedQueriesWrite), s.handleCreateSavedQuery)
	savedQueries.Get("/:queryID", s.requireTokenScope(models.TokenScopeSavedQueriesRead), s.handleGetSavedQuery)
	savedQueries.Put("/:queryID", s.requireTokenScope(models.TokenScopeSavedQueriesWrite), s.handleUpdateSavedQuery)
	savedQueries.Delete("/:queryID", s.requireTokenScope(models.TokenScopeSavedQueriesWrite), s.handleDeleteSavedQuery)
	savedQueries.Get("/:queryID/resolve", s.requireTokenScope(models.TokenScopeSavedQueriesRead), s.handleResolveSavedQuery)

	// Team Source Management (linking/unlinking)
	teamSources := api.Group("/teams/:teamID/sources", s.requireAuth, s.requireTeamMember)
	teamSources.Get("/", s.requireTokenScope(models.TokenScopeSourcesRead), s.handleListTeamSources)

	// Only team admins can link/unlink sources
	teamSources.Post("/", s.requireTokenScope(models.TokenScopeTeamsWrite), s.requireTeamAdminOrGlobalAdmin, s.handleLinkSourceToTeam)
	teamSources.Delete("/:sourceID", s.requireTokenScope(models.TokenScopeTeamsWrite), s.requireTeamAdminOrGlobalAdmin, s.handleUnlinkSourceFromTeam)

	// --- Team Source Operations (requires team membership) ---
	// These endpoints allow team members to interact with a specific source linked to their team
	teamSourceOps := api.Group("/teams/:teamID/sources/:sourceID", s.requireAuth, s.requireTeamMember, s.requireTeamHasSource)
	// Get detailed source info including connection status and schema
	teamSourceOps.Get("/", s.requireTokenScope(models.TokenScopeSourcesRead), s.handleGetTeamSource)
	teamSourceOps.Get("/stats", s.requireTokenScope(models.TokenScopeSourcesRead), s.handleGetTeamSourceStats)

	// Query and explore logs. The heavy query/exploration endpoints are
	// rate-limited per authenticated user (queryLimiter runs after the group's
	// requireAuth, so the user context is available).
	teamSourceOps.Post("/logs/query", withQueryLimit(s.requireTokenScope(models.TokenScopeLogsRead), s.handleQueryLogs)...)
	teamSourceOps.Get("/logs/tail", s.requireTokenScope(models.TokenScopeLogsRead), s.handleTailLogs)
	teamSourceOps.Post("/logs/export", s.requireTokenScope(models.TokenScopeLogsRead), s.handleExportLogs)
	teamSourceOps.Post("/logs/query/:queryID/cancel", s.requireTokenScope(models.TokenScopeLogsRead), s.handleCancelQuery)
	teamSourceOps.Post("/exports", s.requireTokenScope(models.TokenScopeLogsRead), s.handleCreateExportJob)
	teamSourceOps.Get("/exports/:exportID", s.requireTokenScope(models.TokenScopeLogsRead), s.handleGetExportJob)
	teamSourceOps.Get("/exports/:exportID/download", s.requireTokenScope(models.TokenScopeLogsRead), s.handleDownloadExportJob)
	teamSourceOps.Get("/schema", s.requireTokenScope(models.TokenScopeSourcesRead), s.handleGetSourceSchema)
	teamSourceOps.Post("/logs/histogram", withQueryLimit(s.requireTokenScope(models.TokenScopeLogsRead), s.handleGetHistogram)...)
	teamSourceOps.Post("/logs/context", s.requireTokenScope(models.TokenScopeLogsRead), s.handleGetLogContext)
	teamSourceOps.Post("/generate-sql", s.requireTokenScope(models.TokenScopeLogsRead), s.handleGenerateAISQL)
	teamSourceOps.Post("/query-shares", s.requireTokenScope(models.TokenScopeQuerySharesWrite), s.handleCreateQueryShare)

	// LogchefQL endpoints - query language parsing and translation
	teamSourceOps.Post("/logchefql/translate", s.requireTokenScope(models.TokenScopeLogsRead), s.handleLogchefQLTranslate) // Translate LogchefQL to SQL
	teamSourceOps.Post("/logchefql/validate", s.requireTokenScope(models.TokenScopeLogsRead), s.handleLogchefQLValidate)   // Validate LogchefQL syntax
	teamSourceOps.Post("/logchefql/query", s.requireTokenScope(models.TokenScopeLogsRead), s.handleLogchefQLQuery)         // Execute LogchefQL query directly

	// Field value exploration for sidebar
	teamSourceOps.Get("/fields/values", withQueryLimit(s.requireTokenScope(models.TokenScopeLogsRead), s.handleGetAllFieldValues)...)         // Get all LowCardinality field values
	teamSourceOps.Get("/fields/:fieldName/values", withQueryLimit(s.requireTokenScope(models.TokenScopeLogsRead), s.handleGetFieldValues)...) // Get values for a specific field

	// Alerts (cross-team, source-scoped). Visibility: any user with source
	// access via any team. Edit/delete/resolve: creator + global admin
	// (legacy alerts without created_by are global-admin-only).
	alertRoutes := api.Group("/alerts", s.requireAuth, s.requireAlertsEnabled)
	alertRoutes.Get("/", s.requireTokenScope(models.TokenScopeAlertsRead), s.handleListAlerts)
	alertRoutes.Post("/", s.requireTokenScope(models.TokenScopeAlertsWrite), s.handleCreateAlert)
	alertRoutes.Post("/test", s.requireTokenScope(models.TokenScopeAlertsWrite), s.handleTestAlertQuery)
	alertRoutes.Get("/:alertID", s.requireTokenScope(models.TokenScopeAlertsRead), s.handleGetAlert)
	alertRoutes.Put("/:alertID", s.requireTokenScope(models.TokenScopeAlertsWrite), s.handleUpdateAlert)
	alertRoutes.Delete("/:alertID", s.requireTokenScope(models.TokenScopeAlertsWrite), s.handleDeleteAlert)
	alertRoutes.Get("/:alertID/history", s.requireTokenScope(models.TokenScopeAlertsRead), s.handleListAlertHistory)
	alertRoutes.Post("/:alertID/resolve", s.requireTokenScope(models.TokenScopeAlertsWrite), s.handleResolveAlert)

	// Dashboards (saved grids of visualization panels). Visibility: any
	// authenticated user can list/view. Edit/delete: creator + global admin
	// (dashboards whose author was deleted are global-admin-only). Panel data is
	// fetched by the frontend through the existing team-scoped log endpoints, so
	// there is no source-access gate here.
	dashboardRoutes := api.Group("/dashboards", s.requireAuth)
	dashboardRoutes.Get("/", s.requireTokenScope(models.TokenScopeDashboardsRead), s.handleListDashboards)
	dashboardRoutes.Post("/", s.requireTokenScope(models.TokenScopeDashboardsWrite), s.handleCreateDashboard)
	dashboardRoutes.Get("/:dashboardID", s.requireTokenScope(models.TokenScopeDashboardsRead), s.handleGetDashboard)
	dashboardRoutes.Put("/:dashboardID", s.requireTokenScope(models.TokenScopeDashboardsWrite), s.handleUpdateDashboard)
	dashboardRoutes.Delete("/:dashboardID", s.requireTokenScope(models.TokenScopeDashboardsWrite), s.handleDeleteDashboard)

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
// It also stops background maintenance loops (e.g. the expired-session/export-job
// sweeper) and waits for them to exit before returning.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("shutting down http server")
	close(s.stop)
	s.wg.Wait()
	return s.app.ShutdownWithContext(ctx)
}
