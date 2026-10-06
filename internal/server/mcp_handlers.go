package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/mcp"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/internal/oauth"
	"github.com/mr-karan/logchef/pkg/models"
)

// MCPPath is the MCP endpoint. It exists only when OAuth is enabled.
const MCPPath = "/mcp"

// mcpMaxBodyBytes bounds one MCP request body (planner section 5.2).
const mcpMaxBodyBytes = 1 << 20

type mcpPrincipalKey struct{}

// registerMCPRoutes mounts /mcp. Only POST is served; the endpoint is
// stateless and does not stream, so GET and DELETE get 405.
func (s *Server) registerMCPRoutes() {
	handler := mcp.NewServer(mcp.Deps{
		DB:                  s.sqlite,
		Datasources:         s.datasources,
		Config:              s.config,
		Version:             s.version,
		Log:                 s.log.With("component", "mcp"),
		Admit:               s.admitMCPQuery,
		ResourceMetadataURL: s.oauth.MCPResourceMetadataURL(),
	})
	// mcp-go builds each tool context from the request context, so copy the
	// Principal that requireMCPToken stored in the Fiber context onto it.
	withPrincipal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, _ := adaptor.LocalContextFromHTTPRequest(r)
		if local != nil {
			if p, ok := local.Value(mcpPrincipalKey{}).(access.Principal); ok {
				r = r.WithContext(mcp.WithPrincipal(r.Context(), p))
			}
		}
		handler.ServeHTTP(w, r)
	})
	s.app.Post(MCPPath, s.requireMCPOrigin, s.limitMCPBody, s.requireMCPToken, adaptor.HTTPHandlerWithContext(withPrincipal))
	methodNotAllowed := func(c fiber.Ctx) error {
		c.Set(fiber.HeaderAllow, fiber.MethodPost)
		return c.SendStatus(fiber.StatusMethodNotAllowed)
	}
	s.app.Get(MCPPath, methodNotAllowed)
	s.app.Delete(MCPPath, methodNotAllowed)
}

// requireMCPOrigin defends against DNS rebinding: a browser request must come
// from public_url's origin or a configured MCP origin. A request without an
// Origin header (a native MCP host) passes.
func (s *Server) requireMCPOrigin(c fiber.Ctx) error {
	origin := c.Get(fiber.HeaderOrigin)
	if origin == "" || origin == s.oauth.Origin() || slices.Contains(s.config.Auth.OAuth.MCPAllowedOrigins, origin) {
		return c.Next()
	}
	return SendErrorWithType(c, fiber.StatusForbidden, "Request origin is not allowed", models.AuthorizationErrorType)
}

func (s *Server) limitMCPBody(c fiber.Ctx) error {
	if c.Request().Header.ContentLength() > mcpMaxBodyBytes || len(c.Body()) > mcpMaxBodyBytes {
		return SendErrorWithType(c, fiber.StatusRequestEntityTooLarge, "Request body is larger than 1 MiB", models.ValidationErrorType)
	}
	return c.Next()
}

// requireMCPToken accepts only OAuth access tokens issued for the MCP
// resource. PATs, sessions and API-audience tokens get 401 with the
// challenge that points the host at the protected resource metadata.
func (s *Server) requireMCPToken(c fiber.Ctx) error {
	bearer, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	if !ok || bearer == "" {
		return s.mcpUnauthorized(c, "")
	}
	token, err := s.oauth.AuthenticateAccessToken(c.RequestCtx(), bearer, oauth.ResourceMCP)
	if err != nil {
		metrics.RecordAuthAttempt("oauth_mcp", false, nil)
		if errors.Is(err, oauth.ErrInvalidAccessToken) {
			return s.mcpUnauthorized(c, "invalid_token")
		}
		s.log.Error("error authenticating MCP access token", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error validating token", models.GeneralErrorType)
	}
	metrics.RecordAuthAttempt("oauth_mcp", true, token.Principal.User)
	c.SetContext(context.WithValue(c.Context(), mcpPrincipalKey{}, token.Principal))
	return c.Next()
}

func (s *Server) mcpUnauthorized(c fiber.Ctx, oauthError string) error {
	challenge := fmt.Sprintf(`Bearer resource_metadata=%q, scope=%q`, s.oauth.MCPResourceMetadataURL(), s.oauth.MCPChallengeScopes())
	if oauthError != "" {
		challenge = fmt.Sprintf(`Bearer error=%q, resource_metadata=%q, scope=%q`, oauthError, s.oauth.MCPResourceMetadataURL(), s.oauth.MCPChallengeScopes())
	}
	c.Set(fiber.HeaderWWWAuthenticate, challenge)
	return SendErrorWithType(c, fiber.StatusUnauthorized, "An OAuth access token for this MCP server is required", models.AuthenticationErrorType)
}

// admitMCPQuery admits an MCP log query under the same per-user and global
// caps as the HTTP API.
func (s *Server) admitMCPQuery(class mcp.QueryClass, src access.AuthorizedSource, queryText string, cancel context.CancelFunc) (queryID string, release func(), err error) {
	trackerClass := QueryClassPreview
	if class == mcp.QueryClassHistogram {
		trackerClass = QueryClassHistogram
	}
	maxPerUser, maxGlobal := s.admissionLimits(trackerClass)
	queryID, err = queryTracker.StartQuery(trackerClass, src.UserID(), src.SourceID(), src.TeamID(), queryText, cancel, maxPerUser, maxGlobal)
	if err != nil {
		return "", nil, err
	}
	return queryID, func() { queryTracker.RemoveQuery(queryID) }, nil
}
