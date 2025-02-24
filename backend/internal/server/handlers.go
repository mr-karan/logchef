package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend-v2/internal/service"
	"backend-v2/pkg/clickhouse"
	autherrors "backend-v2/pkg/errors"
	"backend-v2/pkg/models"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Response represents a standard API response
type Response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}

// generateState generates a random state string for OIDC
func generateState() (string, error) {
	// Generate 32 bytes of random data for state
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}

	// Generate 32 bytes for nonce
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Combine state and nonce with timestamp
	timestamp := time.Now().Unix()
	combined := fmt.Sprintf("%s.%s.%d",
		base64.RawURLEncoding.EncodeToString(stateBytes),
		base64.RawURLEncoding.EncodeToString(nonceBytes),
		timestamp,
	)

	return combined, nil
}

// validateState validates the state parameter from OIDC callback
func validateState(state, storedState string) error {
	if state == "" || storedState == "" {
		return errors.New("missing state parameter")
	}

	// Split stored state into components
	parts := strings.Split(storedState, ".")
	if len(parts) != 3 {
		return errors.New("invalid state format")
	}

	// Extract timestamp
	timestamp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp in state: %w", err)
	}

	// Check if state has expired (5 minutes)
	if time.Since(time.Unix(timestamp, 0)) > 5*time.Minute {
		return errors.New("state has expired")
	}

	// Compare full state strings
	if !secureCompare(state, storedState) {
		return errors.New("state mismatch")
	}

	return nil
}

// secureCompare performs a constant-time comparison of two strings
func secureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// handleHealth handles the health check endpoint
func (s *Server) handleHealth(c *fiber.Ctx) error {
	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"status": "ok",
			"time":   time.Now(),
		},
	})
}

// handleListSources handles listing all sources
func (s *Server) handleListSources(c *fiber.Ctx) error {
	sources, err := s.svc.ListSources(c.Context())
	if err != nil {
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to list sources: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"sources": sources,
		},
	})
}

// handleGetSource handles getting a single source
func (s *Server) handleGetSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	source, err := s.svc.GetSource(c.Context(), id)
	if err != nil {
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get source: %v", err))
	}

	if source == nil {
		return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
	}

	// Get schema information
	// if err := s.svc.ExploreSource(c.Context(), source); err != nil {
	// 	return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get schema: %v", err))
	// }

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"source": source,
		},
	})
}

// CreateSourceRequest represents a request to create a new source
type CreateSourceRequest struct {
	MetaIsAutoCreated bool                  `json:"meta_is_auto_created"`
	MetaTSField       string                `json:"meta_ts_field,omitempty"`
	Connection        models.ConnectionInfo `json:"connection"`
	Description       string                `json:"description"`
	TTLDays           int                   `json:"ttl_days"`
}

// handleCreateSource handles creating a new source
func (s *Server) handleCreateSource(c *fiber.Ctx) error {
	var req CreateSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	// Default meta timestamp field if not provided
	if req.MetaTSField == "" {
		req.MetaTSField = "_timestamp"
	}

	if err := service.ValidateCreateSourceRequest(req.MetaIsAutoCreated, req.Connection, req.Description, req.TTLDays, req.MetaTSField); err != nil {
		return fiber.NewError(http.StatusBadRequest, err.Error())
	}

	created, err := s.svc.CreateSource(c.Context(), req.MetaIsAutoCreated, req.Connection, req.Description, req.TTLDays, req.MetaTSField)
	if err != nil {
		if validationErr, ok := err.(*service.ValidationError); ok {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to create source: %v", err))
	}

	return c.Status(http.StatusCreated).JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"source": created,
		},
	})
}

// handleDeleteSource handles deleting a source
func (s *Server) handleDeleteSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	if err := s.svc.DeleteSource(c.Context(), id); err != nil {
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to delete source: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "Source deleted successfully",
		},
	})
}

// LogQueryResponse represents the response structure for log queries
type LogQueryResponse struct {
	Logs    interface{}            `json:"logs"`
	Stats   interface{}            `json:"stats"`
	Params  LogQueryResponseParams `json:"params"`
	Columns []models.ColumnInfo    `json:"columns"`
}

// LogQueryResponseParams represents the query parameters used in the response
type LogQueryResponseParams struct {
	SourceID       string              `json:"source_id"`
	RawSQL         string              `json:"raw_sql"`
	Limit          int                 `json:"limit"`
	StartTimestamp int64               `json:"start_timestamp"`
	EndTimestamp   int64               `json:"end_timestamp"`
	Sort           *models.SortOptions `json:"sort,omitempty"`
}

// handleQueryLogs handles POST requests to search/query logs from a source
func (s *Server) handleQueryLogs(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	var req models.LogQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := service.ValidateLogQueryRequest(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, err.Error())
	}

	// Additional validation for timestamps
	if req.StartTimestamp > req.EndTimestamp {
		return fiber.NewError(http.StatusBadRequest, "start_timestamp cannot be greater than end_timestamp")
	}

	// Set default limit if not provided
	if req.Limit == 0 {
		req.Limit = 100 // default limit
	}

	// Convert to query params
	params := clickhouse.LogQueryParams{
		Limit:     req.Limit,
		StartTime: time.UnixMilli(req.StartTimestamp).UTC(),
		EndTime:   time.UnixMilli(req.EndTimestamp).UTC(),
		RawSQL:    req.RawSQL,
	}

	s.log.Debug("querying logs",
		"source_id", id,
		"start_time", params.StartTime,
		"end_time", params.EndTime,
		"limit", params.Limit,
	)

	// Query logs
	result, err := s.svc.QueryLogs(c.Context(), id, params)
	if err != nil {
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
		}
		s.log.Error("failed to query logs",
			"source_id", id,
			"error", err,
		)
		// Pass through ClickHouse errors
		if strings.Contains(err.Error(), "ClickHouse") {
			return fiber.NewError(http.StatusBadRequest, err.Error())
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to query logs: %v", err))
	}

	s.log.Debug("query complete",
		"source_id", id,
		"rows", len(result.Data),
		"execution_time_ms", result.Stats.ExecutionTimeMs,
	)

	response := LogQueryResponse{
		Logs:    result.Data,
		Stats:   result.Stats,
		Columns: result.Columns,
		Params: LogQueryResponseParams{
			SourceID:       id,
			RawSQL:         req.RawSQL,
			Limit:          req.Limit,
			StartTimestamp: req.StartTimestamp,
			EndTimestamp:   req.EndTimestamp,
		},
	}

	return c.JSON(Response{
		Status: "success",
		Data:   response,
	})
}

// TimeSeriesRequest represents the request parameters for time series data
type TimeSeriesRequest struct {
	StartTimestamp int64                 `query:"start_timestamp"`
	EndTimestamp   int64                 `query:"end_timestamp"`
	Window         clickhouse.TimeWindow `query:"window"`
}

// handleGetTimeSeries handles getting time series data for log counts
func (s *Server) handleGetTimeSeries(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	var req TimeSeriesRequest
	if err := c.QueryParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid query parameters: %v", err))
	}

	// Validate time range
	if req.StartTimestamp > req.EndTimestamp {
		return fiber.NewError(http.StatusBadRequest, "start_timestamp cannot be greater than end_timestamp")
	}

	// Convert timestamps to time.Time
	params := clickhouse.TimeSeriesParams{
		StartTime: time.UnixMilli(req.StartTimestamp).UTC(),
		EndTime:   time.UnixMilli(req.EndTimestamp).UTC(),
		Window:    req.Window,
	}

	// If window is not specified, auto-select based on time range
	if params.Window == "" {
		duration := params.EndTime.Sub(params.StartTime)
		switch {
		case duration <= time.Hour:
			params.Window = clickhouse.TimeWindow1m
		case duration <= 6*time.Hour:
			params.Window = clickhouse.TimeWindow5m
		case duration <= 12*time.Hour:
			params.Window = clickhouse.TimeWindow15m
		case duration <= 24*time.Hour:
			params.Window = clickhouse.TimeWindow1h
		case duration <= 7*24*time.Hour:
			params.Window = clickhouse.TimeWindow6h
		default:
			params.Window = clickhouse.TimeWindow24h
		}
	}

	result, err := s.svc.GetTimeSeries(c.Context(), id, params)
	if err != nil {
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
		}
		s.log.Error("failed to get time series data",
			"source_id", id,
			"error", err,
		)
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get time series data: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"buckets": result.Buckets,
		},
	})
}

// redirectToFrontend redirects to the frontend URL with optional error message
func (s *Server) redirectToFrontend(c *fiber.Ctx, path string, err *autherrors.AuthError) error {
	// Build URL with query params if error exists
	if err != nil {
		path = fmt.Sprintf("/auth/login?error=%s", err.Code)
		s.log.Warn("redirecting to frontend login page with error",
			"error_code", err.Code,
			"error_message", err.Message,
			"path", path,
		)
	}

	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Use configured frontend URL if set
	if s.config.Server.FrontendURL != "" {
		s.log.Debug("redirecting to frontend",
			"frontend_url", s.config.Server.FrontendURL,
			"path", path,
		)
		return c.Redirect(s.config.Server.FrontendURL+path, fiber.StatusTemporaryRedirect)
	}

	return c.Redirect(path, fiber.StatusTemporaryRedirect)
}

// handleCallback handles the OIDC callback
func (s *Server) handleCallback(c *fiber.Ctx) error {
	// Get state and code from query params
	state := c.Query("state")
	code := c.Query("code")

	if code == "" {
		s.log.Warn("missing code parameter in callback")
		return s.redirectToFrontend(c, "", &autherrors.AuthError{
			Code:    autherrors.ErrOIDCInvalidToken,
			Message: "Invalid request: missing code parameter",
		})
	}

	// Get stored state from cookie
	storedState := c.Cookies("auth_state")

	// Validate state
	if err := validateState(state, storedState); err != nil {
		s.log.Warn("state validation failed",
			"error", err,
			"state_length", len(state),
			"stored_state_length", len(storedState),
		)
		return s.redirectToFrontend(c, "", &autherrors.AuthError{
			Code:    autherrors.ErrOIDCInvalidState,
			Message: "Invalid state: " + err.Error(),
		})
	}

	// Delete state cookie immediately
	c.Cookie(&fiber.Cookie{
		Name:     "auth_state",
		Value:    "",
		Expires:  time.Now().Add(-24 * time.Hour),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "lax",
		Path:     "/",
	})

	// Handle callback
	_, session, err := s.auth.HandleCallback(c.Context(), code, state)
	if err != nil {
		var authErr *autherrors.AuthError
		if errors.As(err, &authErr) {
			s.log.Error("failed to handle auth callback",
				"error", err,
				"code", authErr.Code,
			)
			return s.redirectToFrontend(c, "", authErr)
		} else {
			s.log.Error("failed to handle auth callback",
				"error", err,
			)
			return s.redirectToFrontend(c, "", &autherrors.AuthError{
				Code:    autherrors.ErrOIDCInvalidToken,
				Message: "Authentication failed: " + err.Error(),
			})
		}
	}

	// Set session cookie
	c.Cookie(&fiber.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Expires:  session.ExpiresAt,
		HTTPOnly: true,
		Secure:   true,
		SameSite: "lax",
		Path:     "/",
	})

	// Get frontend redirect path from query param, default to /logs/explore if not provided
	redirectPath := c.Query("redirect", "/logs/explore")

	// Redirect to frontend on success
	return s.redirectToFrontend(c, redirectPath, nil)
}

// handleLogin initiates the OIDC login flow
func (s *Server) handleLogin(c *fiber.Ctx) error {
	// Generate secure state parameter
	state, err := generateState()
	if err != nil {
		s.log.Error("failed to generate state",
			"error", err,
		)
		return s.redirectToFrontend(c, "", &autherrors.AuthError{
			Code:    autherrors.ErrOIDCInvalidState,
			Message: "Failed to generate state: " + err.Error(),
		})
	}

	// Store state in secure cookie
	cookie := &fiber.Cookie{
		Name:     "auth_state",
		Value:    state,
		Expires:  time.Now().Add(5 * time.Minute),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "lax",
		Path:     "/",
	}
	c.Cookie(cookie)

	s.log.Debug("initiating OIDC login",
		"state_length", len(state),
	)

	// Get auth URL and redirect to OIDC provider
	authURL := s.auth.GetAuthURL(state)
	return c.Redirect(authURL, fiber.StatusTemporaryRedirect)
}

// handleLogout logs out the user
func (s *Server) handleLogout(c *fiber.Ctx) error {
	// Get session ID from cookie
	sessionID := c.Cookies("session_id")
	if sessionID != "" {
		// Revoke session
		if err := s.auth.RevokeSession(c.Context(), sessionID); err != nil {
			s.log.Error("failed to revoke session", "error", err)
		}
	}

	// Delete session cookie
	c.Cookie(&fiber.Cookie{
		Name:     "session_id",
		Value:    "",
		Expires:  time.Now().Add(-24 * time.Hour),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "lax",
	})

	return c.JSON(Response{
		Status: "success",
		Data:   nil,
	})
}

// handleGetSession returns the current session info
func (s *Server) handleGetSession(ctx *fiber.Ctx) error {
	// Get session ID from cookie
	sessionID := ctx.Cookies("session_id")
	if sessionID == "" {
		return ctx.Status(fiber.StatusUnauthorized).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "No session found",
				"code":  autherrors.ErrSessionNotFound,
			},
		})
	}

	// Validate session
	session, err := s.auth.ValidateSession(ctx.Context(), sessionID)
	if err != nil {
		var authErr *autherrors.AuthError
		if errors.As(err, &authErr) {
			return ctx.Status(fiber.StatusUnauthorized).JSON(Response{
				Status: "error",
				Data: fiber.Map{
					"error": authErr.Message,
					"code":  authErr.Code,
				},
			})
		}
		return ctx.Status(fiber.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "Internal server error",
			},
		})
	}

	// Get user info
	user, err := s.auth.GetUser(ctx.Context(), session.UserID)
	if err != nil {
		return ctx.Status(fiber.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "Failed to get user information",
			},
		})
	}

	return ctx.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"user":    user,
			"session": session,
		},
	})
}

// requireAuth middleware ensures the request is authenticated
func (s *Server) requireAuth(c *fiber.Ctx) error {
	// Get session ID from cookie
	sessionID := c.Cookies("session_id")
	if sessionID == "" {
		return c.Status(fiber.StatusForbidden).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "Authentication required",
				"code":  autherrors.ErrSessionNotFound,
			},
		})
	}

	// Validate session
	session, err := s.auth.ValidateSession(c.Context(), sessionID)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": err.Error(),
				"code":  autherrors.ErrSessionExpired,
			},
		})
	}

	// Get user info
	user, err := s.auth.GetUser(c.Context(), session.UserID)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "User not found",
				"code":  autherrors.ErrUserNotFound,
			},
		})
	}

	// Store user and session in context
	c.Locals("user", user)
	c.Locals("session", session)

	return c.Next()
}

// requireAdmin middleware ensures the user is an admin
func (s *Server) requireAdmin(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	if user.Role != "admin" {
		return autherrors.NewAuthError(autherrors.ErrInsufficientPermissions, "Admin access required", nil)
	}
	return c.Next()
}

// Team handlers

// CreateTeamRequest represents a request to create a new team
type CreateTeamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleListTeams handles listing all teams
func (s *Server) handleListTeams(c *fiber.Ctx) error {
	teams, err := s.svc.ListTeams(c.Context())
	if err != nil {
		s.log.Error("failed to list teams",
			"error", err,
		)
		return fiber.NewError(http.StatusInternalServerError, "Failed to retrieve teams. Please try again later.")
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"teams": teams,
		},
	})
}

// handleGetTeam handles getting a single team
func (s *Server) handleGetTeam(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}

	team, err := s.svc.GetTeam(c.Context(), id)
	if err != nil {
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("team %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get team: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"team": team,
		},
	})
}

// handleCreateTeam handles creating a new team
func (s *Server) handleCreateTeam(c *fiber.Ctx) error {
	var req CreateTeamRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	team := &models.Team{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
	}

	// Validate team
	if err := service.ValidateCreateTeamRequest(team); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return err
	}

	if err := s.svc.CreateTeam(c.Context(), team); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return fiber.NewError(http.StatusInternalServerError, "Failed to create team. Please try again later.")
	}

	return c.Status(http.StatusCreated).JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"team": team,
		},
	})
}

// handleUpdateTeam handles updating a team
func (s *Server) handleUpdateTeam(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}

	var req CreateTeamRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	// Get existing team
	team, err := s.svc.GetTeam(c.Context(), id)
	if err != nil {
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("team %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get team: %v", err))
	}

	// Update fields
	team.Name = req.Name
	team.Description = req.Description

	if err := s.svc.UpdateTeam(c.Context(), team); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to update team: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"team": team,
		},
	})
}

// handleDeleteTeam handles deleting a team
func (s *Server) handleDeleteTeam(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}

	if err := s.svc.DeleteTeam(c.Context(), id); err != nil {
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("team %s not found", id))
		}
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to delete team: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "Team deleted successfully",
		},
	})
}

// Team member handlers

// AddTeamMemberRequest represents a request to add a member to a team
type AddTeamMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// handleListTeamMembers handles listing all members of a team
func (s *Server) handleListTeamMembers(c *fiber.Ctx) error {
	teamID := c.Params("id")
	if teamID == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}

	members, err := s.svc.ListTeamMembers(c.Context(), teamID)
	if err != nil {
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("team %s not found", teamID))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to list team members: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"members": members,
		},
	})
}

// handleAddTeamMember handles adding a member to a team
func (s *Server) handleAddTeamMember(c *fiber.Ctx) error {
	teamID := c.Params("id")
	if teamID == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}

	var req AddTeamMemberRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	// Validate request
	if err := service.ValidateTeamMemberRequest(teamID, req.UserID, req.Role); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return err
	}

	if err := s.svc.AddTeamMember(c.Context(), teamID, req.UserID, req.Role); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("team %s not found", teamID))
		}
		if err == service.ErrUserNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("user %s not found", req.UserID))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to add team member: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "Team member added successfully",
		},
	})
}

// handleRemoveTeamMember handles removing a member from a team
func (s *Server) handleRemoveTeamMember(c *fiber.Ctx) error {
	teamID := c.Params("id")
	userID := c.Params("userId")

	if teamID == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}
	if userID == "" {
		return fiber.NewError(http.StatusBadRequest, "user id is required")
	}

	if err := s.svc.RemoveTeamMember(c.Context(), teamID, userID); err != nil {
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("team %s not found", teamID))
		}
		if err == service.ErrUserNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("user %s not found", userID))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to remove team member: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "Team member removed successfully",
		},
	})
}

// User handlers

// handleListUsers handles listing all users
func (s *Server) handleListUsers(c *fiber.Ctx) error {
	users, err := s.auth.ListUsers(c.Context())
	if err != nil {
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to list users: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"users": users,
		},
	})
}

// handleGetUser handles getting a single user
func (s *Server) handleGetUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "user id is required")
	}

	user, err := s.svc.GetUser(c.Context(), id)
	if err != nil {
		if err == service.ErrUserNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("user %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get user: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"user": user,
		},
	})
}

// handleCreateUser handles creating a new user
func (s *Server) handleCreateUser(c *fiber.Ctx) error {
	var req models.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	// Validate request
	if err := service.ValidateCreateUserRequest(&req); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return err
	}

	user := &models.User{
		Email:    req.Email,
		FullName: req.FullName,
		Role:     req.Role,
		Status:   models.UserStatusActive, // New users are active by default
	}

	if err := s.svc.CreateUser(c.Context(), user); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to create user: %v", err))
	}

	return c.Status(http.StatusCreated).JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"user": user,
		},
	})
}

// handleUpdateUser handles updating a user
func (s *Server) handleUpdateUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "user id is required")
	}

	var req models.UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	// Validate request
	if err := service.ValidateUpdateUserRequest(&req); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return err
	}

	// Get existing user
	user, err := s.svc.GetUser(c.Context(), id)
	if err != nil {
		if err == service.ErrUserNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("user %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get user: %v", err))
	}

	// Update fields if provided
	if req.FullName != "" {
		user.FullName = req.FullName
	}
	if req.Role != "" {
		user.Role = req.Role
	}
	if req.Status != "" {
		user.Status = req.Status
	}

	if err := s.svc.UpdateUser(c.Context(), user); err != nil {
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to update user: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"user": user,
		},
	})
}

// handleDeleteUser handles deleting a user
func (s *Server) handleDeleteUser(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "user id is required")
	}

	if err := s.svc.DeleteUser(c.Context(), id); err != nil {
		if err == service.ErrUserNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("user %s not found", id))
		}
		var validationErr *service.ValidationError
		if errors.As(err, &validationErr) {
			return fiber.NewError(http.StatusBadRequest, validationErr.Error())
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to delete user: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "User deleted successfully",
		},
	})
}

// Team source handlers

// AddTeamSourceRequest represents a request to add a source to a team
type AddTeamSourceRequest struct {
	SourceID string `json:"source_id"`
}

// handleListTeamSources handles listing all sources in a team
func (s *Server) handleListTeamSources(c *fiber.Ctx) error {
	teamID := c.Params("id")
	if teamID == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}

	sources, err := s.svc.ListTeamSources(c.Context(), teamID)
	if err != nil {
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, "Team not found")
		}
		s.log.Error("failed to list team sources",
			"error", err,
			"team_id", teamID,
		)
		return fiber.NewError(http.StatusInternalServerError, "Failed to retrieve team sources. Please try again later.")
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"sources": sources,
		},
	})
}

// handleAddTeamSource handles adding a source to a team
func (s *Server) handleAddTeamSource(c *fiber.Ctx) error {
	teamID := c.Params("id")
	if teamID == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}

	var req AddTeamSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, "Invalid request body")
	}

	if req.SourceID == "" {
		return fiber.NewError(http.StatusBadRequest, "source_id is required")
	}

	if err := s.svc.AddTeamSource(c.Context(), teamID, req.SourceID); err != nil {
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, "Team not found")
		}
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, "Source not found")
		}
		if strings.Contains(err.Error(), "already exists") {
			return fiber.NewError(http.StatusBadRequest, "Source is already added to this team")
		}
		s.log.Error("failed to add team source",
			"error", err,
			"team_id", teamID,
			"source_id", req.SourceID,
		)
		return fiber.NewError(http.StatusInternalServerError, "Failed to add source to team. Please try again later.")
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "Source added to team successfully",
		},
	})
}

// handleRemoveTeamSource handles removing a source from a team
func (s *Server) handleRemoveTeamSource(c *fiber.Ctx) error {
	teamID := c.Params("id")
	sourceID := c.Params("sourceId")

	if teamID == "" {
		return fiber.NewError(http.StatusBadRequest, "team id is required")
	}
	if sourceID == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	if err := s.svc.RemoveTeamSource(c.Context(), teamID, sourceID); err != nil {
		if err == service.ErrTeamNotFound {
			return fiber.NewError(http.StatusNotFound, "Team not found")
		}
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, "Source not found")
		}
		s.log.Error("failed to remove team source",
			"error", err,
			"team_id", teamID,
			"source_id", sourceID,
		)
		return fiber.NewError(http.StatusInternalServerError, "Failed to remove source from team. Please try again later.")
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "Source removed from team successfully",
		},
	})
}
