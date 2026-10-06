package server

import (
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"

	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// getUserIDFromContext extracts the user ID from the context
func getUserIDFromContext(c fiber.Ctx) models.UserID {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return 0
	}
	return user.ID
}

// isUserAdmin checks if the user in context has admin role
func isUserAdmin(c fiber.Ctx) bool {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return false
	}
	return user.Role == models.UserRoleAdmin
}

// requireAuth is middleware that ensures the request includes valid authentication.
// It supports both API token authentication (Authorization: Bearer <token>) and
// session-based authentication (session cookie). It validates the authentication,
// retrieves the associated user, and stores the user information in the request
// context (c.Locals) for subsequent handlers.
func (s *Server) requireAuth(c fiber.Ctx) error {
	// Try API token authentication first
	authHeader := c.Get("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		return s.authenticateWithToken(c, authHeader)
	}

	// Fall back to session-based authentication
	return s.authenticateWithSession(c)
}

// authenticateWithToken handles API token authentication
func (s *Server) authenticateWithToken(c fiber.Ctx, authHeader string) error {
	// Extract token from "Bearer <token>"
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader || token == "" {
		metrics.RecordAuthAttempt("token", false, nil)
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Invalid Authorization header format", models.AuthenticationErrorType)
	}

	// Authenticate token and get associated user
	user, apiToken, err := core.AuthenticateAPIToken(c.RequestCtx(), s.sqlite, s.log, &s.config.Auth, token)
	if err != nil {
		metrics.RecordAuthAttempt("token", false, nil)

		// Handle specific token errors
		if errors.Is(err, core.ErrInvalidToken) || errors.Is(err, core.ErrTokenExpired) {
			return SendErrorWithType(c, fiber.StatusUnauthorized, "Invalid or expired token", models.AuthenticationErrorType)
		}

		s.log.Error("error authenticating API token", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error validating token", models.GeneralErrorType)
	}

	// Record successful token authentication with user context
	metrics.RecordAuthAttempt("token", true, user)

	// Store user and token info in request context
	c.Locals("user", user)
	c.Locals("api_token", apiToken)
	c.Locals("auth_method", "token")

	return c.Next()
}

// authenticateWithSession handles session-based authentication (existing logic)
func (s *Server) authenticateWithSession(c fiber.Ctx) error {
	// Retrieve session ID from cookie.
	sessionIDStr := c.Cookies(sessionCookieName)
	if sessionIDStr == "" {
		metrics.RecordSessionOperation("validate", false, nil)
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication required", models.AuthenticationErrorType)
	}
	sessionID := models.SessionID(sessionIDStr)

	// Validate the session exists and is not expired.
	session, err := core.ValidateSession(c.RequestCtx(), s.sqlite, s.log, sessionID)
	if err != nil {
		metrics.RecordSessionOperation("validate", false, nil)

		// Handle specific session errors by returning 401 (not authenticated).
		if errors.Is(err, core.ErrSessionNotFound) || errors.Is(err, core.ErrSessionExpired) {
			return SendErrorWithType(c, fiber.StatusUnauthorized, err.Error(), models.AuthenticationErrorType)
		}
		// Log unexpected errors during validation.
		s.log.Error("error validating session via core function", "error", err, "session_id", sessionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error validating session", models.GeneralErrorType)
	}

	// Retrieve associated user information.
	user, err := core.GetUser(c.RequestCtx(), s.sqlite, session.UserID)
	if err != nil {
		// If user not found for a valid session, treat as an auth issue.
		if errors.Is(err, core.ErrUserNotFound) {
			return SendErrorWithType(c, fiber.StatusUnauthorized, "User associated with session not found", models.AuthenticationErrorType)
		}
		s.log.Error("error getting user for session via core function", "error", err, "user_id", session.UserID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error retrieving user data", models.GeneralErrorType)
	}

	// Re-validate the account's right to authenticate on every request, mirroring
	// the API-token path (core.AuthenticateAPIToken). A session cookie must never
	// outlive the account: deactivating a user (or a service account, which must
	// not hold a browser session) is rejected immediately here rather than
	// remaining valid until the session's natural expiry.
	if user.Status != models.UserStatusActive || user.AccountType == models.UserAccountTypeService {
		metrics.RecordSessionOperation("validate", false, nil)
		s.log.Warn("session rejected for inactive or non-interactive account", "user_id", user.ID, "status", user.Status, "account_type", user.AccountType)
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User account is inactive", models.AuthenticationErrorType)
	}

	// Record successful session validation with user context
	metrics.RecordSessionOperation("validate", true, user)

	// Store user and session in request context for downstream handlers.
	c.Locals("user", user)
	c.Locals("session", session)
	c.Locals("auth_method", "session")

	return c.Next()
}

// requireAdmin is middleware that ensures the authenticated user has the global 'admin' role.
// It assumes requireAuth has already run and placed the user in the context.
func (s *Server) requireAdmin(c fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context for admin check")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}

	if user.Role != models.UserRoleAdmin {

		// Record authorization failure
		metrics.RecordAuthorizationFailure(c.Route().Path, user, "insufficient_role")

		return SendErrorWithType(c, fiber.StatusForbidden, "Admin access required", models.AuthorizationErrorType)
	}

	return c.Next()
}

// principalFromLocals builds the access principal from what requireAuth stored.
// A request without a known auth method gets the zero Principal, which every
// scope check denies.
func principalFromLocals(c fiber.Ctx) access.Principal {
	user, _ := c.Locals("user").(*models.User)
	switch c.Locals("auth_method") {
	case "session":
		return access.SessionPrincipal(user)
	case "token":
		apiToken, _ := c.Locals("api_token").(*models.APIToken)
		return access.APITokenPrincipal(user, apiToken)
	}
	return access.Principal{User: user}
}

func (s *Server) requireTokenScope(scope models.TokenScope) fiber.Handler {
	return func(c fiber.Ctx) error {
		p := principalFromLocals(c)
		if err := p.Require(scope); err != nil {
			return sendInsufficientScope(c, p.User)
		}
		return c.Next()
	}
}

func sendInsufficientScope(c fiber.Ctx, user *models.User) error {
	metrics.RecordAuthorizationFailure(c.Route().Path, user, "insufficient_token_scope")
	return SendErrorWithType(c, fiber.StatusForbidden, "API token does not have the required scope", models.AuthorizationErrorType)
}

// requireSourceNotManaged rejects mutations on config-managed sources.
func (s *Server) requireSourceNotManaged(c fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	if sourceIDStr == "" {
		return c.Next()
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return c.Next() // let handler deal with bad ID
	}
	managed, err := s.sqlite.IsSourceManaged(c.RequestCtx(), sourceID)
	if err == nil && managed {
		return SendErrorWithType(c, fiber.StatusForbidden,
			"This source is managed by provisioning config and cannot be modified via API",
			models.ManagedResourceErrorType)
	}
	return c.Next()
}

// requireTeamNotManaged rejects mutations on config-managed teams.
func (s *Server) requireTeamNotManaged(c fiber.Ctx) error {
	teamIDStr := c.Params("teamID")
	if teamIDStr == "" {
		return c.Next()
	}
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return c.Next()
	}
	managed, err := s.sqlite.IsTeamManaged(c.RequestCtx(), teamID)
	if err == nil && managed {
		return SendErrorWithType(c, fiber.StatusForbidden,
			"This team is managed by provisioning config and cannot be modified via API",
			models.ManagedResourceErrorType)
	}
	return c.Next()
}

// requireAnyTeamAdmin is middleware that ensures the authenticated user is an admin of at least one team,
// or is a global admin. This is used for endpoints that should be accessible to team admins
// without requiring a specific team context (e.g., listing users to add to teams).
// It assumes requireAuth has already run.
func (s *Server) requireAnyTeamAdmin(c fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context for any team admin check")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}

	// Global admins bypass specific team admin checks.
	if user.Role == models.UserRoleAdmin {
		return c.Next()
	}

	// Check if the user is an admin of any team.
	isAnyAdmin, err := core.IsAnyTeamAdmin(c.RequestCtx(), s.sqlite, user.ID)
	if err != nil {
		s.log.Error("failed to check if user is any team admin", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team admin status")
	}

	if !isAnyAdmin {
		s.log.Warn("User is not an admin of any team", "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusForbidden, "Team admin privileges required", models.AuthorizationErrorType)
	}

	return c.Next()
}

// requireTeamMember is middleware that ensures the authenticated user is a member of the team
// specified by the ':teamID' path parameter, or is a global admin.
// It assumes requireAuth has already run.
func (s *Server) requireTeamMember(c fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.log.Error("user not found in context for team member check")
		return SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}
	teamIDStr := c.Params("teamID")

	// Global admins bypass specific team membership checks.
	if user.Role == models.UserRoleAdmin {
		return c.Next()
	}

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}

	// Check membership using core function.
	isMember, err := core.IsTeamMember(c.RequestCtx(), s.sqlite, teamID, user.ID)
	if err != nil {
		s.log.Error("failed to verify team membership", "error", err, "team_id", teamID, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team membership")
	}

	if !isMember {
		s.log.Warn("Team membership denied", "user_id", user.ID, "team_id", teamID)
		return SendErrorWithType(c, fiber.StatusForbidden, "Team membership required", models.AuthorizationErrorType)
	}

	return c.Next()
}

// requireTeamAdminOrGlobalAdmin checks if a user is either an admin of the requested team or a global admin
func (s *Server) requireTeamAdminOrGlobalAdmin(c fiber.Ctx) error {
	userID := getUserIDFromContext(c)
	if userID == 0 {
		return SendError(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	// Get the team ID from the request parameters
	teamIDStr := c.Params("teamID")
	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
	}

	// Check if the user is a global admin
	if isUserAdmin(c) {
		return c.Next() // Allow global admins unconditionally
	}

	// Check if the user is a team admin
	isTeamAdmin, err := core.IsTeamAdmin(c.RequestCtx(), s.sqlite, teamID, userID)
	if err != nil {
		s.log.Error("Error checking team admin status", "error", err, "team_id", teamID, "user_id", userID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to verify team admin status")
	}

	if !isTeamAdmin {
		s.log.Warn("User is not a team admin", "team_id", teamID, "user_id", userID)
		return SendError(c, fiber.StatusForbidden, "Admin team privileges required")
	}

	// User is a team admin, continue with the request
	return c.Next()
}

const authorizedSourceKey = "authorized_source"

// requireTeamHasSource authorizes the caller for the team and source in the
// path and the route's scope, then stores the access.AuthorizedSource for the
// handler. It runs after requireTeamMember, which answers non-members first.
func (s *Server) requireTeamHasSource(scope models.TokenScope) fiber.Handler {
	return func(c fiber.Ctx) error {
		teamID, err := core.ParseTeamID(c.Params("teamID"))
		if err != nil {
			return SendError(c, fiber.StatusBadRequest, "Invalid team ID: "+err.Error())
		}
		sourceID, err := core.ParseSourceID(c.Params("sourceID"))
		if err != nil {
			return SendError(c, fiber.StatusBadRequest, "Invalid source ID: "+err.Error())
		}

		p := principalFromLocals(c)
		src, err := access.AuthorizeTeamSource(c.RequestCtx(), s.sqlite, p, teamID, sourceID, scope)
		switch {
		case err == nil:
		case errors.Is(err, access.ErrNotTeamMember):
			return SendErrorWithType(c, fiber.StatusForbidden, "Team membership required", models.AuthorizationErrorType)
		case errors.Is(err, access.ErrSourceNotInTeam):
			s.log.Warn("Team does not have access to source", "team_id", teamID, "source_id", sourceID)
			return SendError(c, fiber.StatusForbidden, "Team does not have access to this source")
		case errors.Is(err, access.ErrInsufficientScope):
			return sendInsufficientScope(c, p.User)
		default:
			s.log.Error("Error checking team-source access", "error", err, "team_id", teamID, "source_id", sourceID)
			return SendError(c, fiber.StatusInternalServerError, "Failed to verify team source access")
		}

		c.Locals(authorizedSourceKey, src)
		return c.Next()
	}
}

// authorizedSource returns the source that requireTeamHasSource authorized.
// When it is missing, it writes a 500 and returns ok=false: the route is
// registered without the middleware, which is a server bug.
func (s *Server) authorizedSource(c fiber.Ctx) (src access.AuthorizedSource, ok bool) {
	src, ok = c.Locals(authorizedSourceKey).(access.AuthorizedSource)
	if !ok {
		s.log.Error("route reached handler without source authorization", "path", c.Route().Path)
		_ = SendErrorWithType(c, fiber.StatusInternalServerError, "Authorization context missing", models.GeneralErrorType)
	}
	return src, ok
}

// notFoundHandler returns a standardized 404 Not Found error for API routes.
func (s *Server) notFoundHandler(c fiber.Ctx) error {
	return SendErrorWithType(c, fiber.StatusNotFound, "API route not found", models.NotFoundErrorType)
}
