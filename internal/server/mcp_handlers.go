package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/core"
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

// registerMCPRoutes mounts /mcp. Only POST is served; the endpoint is
// stateless and does not stream, so GET and DELETE get 405.
func (s *Server) registerMCPRoutes() {
	handler := mcp.NewServer(mcp.Deps{
		DB:                  s.store,
		Datasources:         s.datasources,
		Config:              s.config,
		Version:             s.version,
		Log:                 s.log.With("component", "mcp"),
		Admit:               s.admitMCPQuery,
		StartRead:           s.startToolQueryRun,
		ResourceMetadataURL: s.oauth.MCPResourceMetadataURL(),
	})
	// mcp-go builds each tool context from the request context, which carries
	// the Principal that requireMCPToken stored.
	s.app.Post(MCPPath, s.requireMCPOrigin, s.limitMCPBody, s.requireMCPToken, httpHandlerWithRequestContext(handler))
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
	if origin == "" || origin == s.oauth.IssuerOrigin() || slices.Contains(s.config.Auth.OAuth.MCPAllowedOrigins, origin) {
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
// resource and stores the Principal on the request context. PATs, sessions
// and API-audience tokens get 401 with the challenge that points the host at
// the protected resource metadata.
func (s *Server) requireMCPToken(c fiber.Ctx) error {
	bearer, ok := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	if !ok || bearer == "" {
		return s.mcpUnauthorized(c, "")
	}
	token, err := s.oauth.AuthenticateAccessToken(c.Context(), bearer, oauth.ResourceMCP)
	if err != nil {
		metrics.RecordAuthAttempt("oauth_mcp", false, nil)
		if errors.Is(err, oauth.ErrInvalidAccessToken) {
			return s.mcpUnauthorized(c, "invalid_token")
		}
		s.log.Error("error authenticating MCP access token", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error validating token", models.GeneralErrorType)
	}
	metrics.RecordAuthAttempt("oauth_mcp", true, token.Principal.User)
	ctx := mcp.WithPrincipal(c.Context(), token.Principal)
	c.SetContext(withToolCaller(ctx, toolCaller{
		actor:   oauthActor(token.Principal, models.AuditAuthOAuthMCP),
		request: auditRequest(c),
		mode:    queryRunMCP,
	}))
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
	var trackerClass QueryClass
	switch class {
	case mcp.QueryClassPreview:
		trackerClass = QueryClassPreview
	case mcp.QueryClassHistogram:
		trackerClass = QueryClassHistogram
	case mcp.QueryClassBacktest:
		trackerClass = QueryClassBacktest
	default:
		return "", nil, fmt.Errorf("unknown MCP query class %d", class)
	}
	maxPerUser, maxGlobal := s.admissionLimits(trackerClass)
	queryID, err = queryTracker.StartQuery(trackerClass, src.UserID(), src.SourceID(), src.TeamID(), queryText, cancel, maxPerUser, maxGlobal)
	if err != nil {
		return "", nil, err
	}
	return queryID, func() { queryTracker.RemoveQuery(queryID) }, nil
}

// toolCaller is who a tool call reads logs for: the actor and request that
// its query.run events name, and the mode that tells MCP from Ask Chef.
type toolCaller struct {
	actor   audit.Actor
	request audit.Request
	mode    queryRunMode
}

type toolCallerKey struct{}

func withToolCaller(ctx context.Context, caller toolCaller) context.Context {
	return context.WithValue(ctx, toolCallerKey{}, caller)
}

// startToolQueryRun is the mcp.StartRead of the MCP and Ask Chef tool
// servers. Each tool call that reads logs is one run, recorded like an HTTP
// query and writing no history row.
func (s *Server) startToolQueryRun(ctx context.Context, call mcp.ReadCall) func(mcp.ReadResult) {
	caller, ok := ctx.Value(toolCallerKey{}).(toolCaller)
	if !ok {
		s.log.Error("tool call reads logs without a caller, so no query.run event is recorded", "tool", call.Tool, "source_id", call.Source.SourceID())
		return func(mcp.ReadResult) {}
	}
	run := s.startQueryRun(&queryRun{
		actor:    &caller.actor,
		request:  caller.request,
		teamID:   call.Source.TeamID(),
		sourceID: call.Source.SourceID(),
		query:    call.Query,
		language: call.Language,
		mode:     caller.mode,
		limit:    call.Limit,
		tool:     call.Tool,
		fields:   call.Fields,
		anchor:   call.Anchor,
	})
	// The tool loads the source again when it runs; a source that fails to
	// load here fails the tool too, and the event then has no source name.
	if source, err := core.LoadSource(ctx, s.datasources, run.sourceID); err == nil {
		run.source = source
		if run.language == "" {
			run.language = source.NativeQueryLanguage()
		}
	}
	return func(res mcp.ReadResult) {
		run.from, run.to = res.From, res.To
		// The tool answered over a response that succeeded, whatever the
		// call's outcome.
		s.finishQueryRun(run, queryOutcome{err: res.Err, status: fiber.StatusOK, rows: res.Rows})
	}
}
