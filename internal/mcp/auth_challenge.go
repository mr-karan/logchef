package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core/access"
)

// wwwAuthenticateMetaKey carries an OAuth challenge inside a tool result, so a
// host such as ChatGPT can start a new authorization from a tool call.
const wwwAuthenticateMetaKey = "mcp/www_authenticate"

// challengeScopes is every scope some tool needs: the scopes a host should ask
// for when a call fails for lack of scope.
var challengeScopes = func() string {
	var scopes []string
	for _, needed := range toolScopes {
		for _, scope := range needed {
			if !slices.Contains(scopes, string(scope)) {
				scopes = append(scopes, string(scope))
			}
		}
	}
	slices.Sort(scopes)
	return strings.Join(scopes, " ")
}()

// authChallenge returns the WWW-Authenticate value for an authentication or
// scope failure, and false for any other error.
func (t *tools) authChallenge(err error) (string, bool) {
	var challenge string
	switch {
	case errors.Is(err, errNoPrincipal):
		challenge = `Bearer error="invalid_token"`
	case errors.Is(err, access.ErrInsufficientScope):
		challenge = fmt.Sprintf(`Bearer error="insufficient_scope", scope=%q`, challengeScopes)
	default:
		return "", false
	}
	if t.deps.ResourceMetadataURL != "" {
		challenge += fmt.Sprintf(`, resource_metadata=%q`, t.deps.ResourceMetadataURL)
	}
	return challenge, true
}

// errorResult is the tool result for err. An authentication or scope failure
// also carries the OAuth challenge in _meta.
func (t *tools) errorResult(err error) *mcp.CallToolResult {
	result := mcp.NewToolResultError(err.Error())
	if challenge, ok := t.authChallenge(err); ok {
		result.Meta = &mcp.Meta{AdditionalFields: map[string]any{wwwAuthenticateMetaKey: challenge}}
	}
	return result
}

// authErrors turns an authentication or scope error that a handler returns
// into a tool result with the OAuth challenge. Other errors pass through.
func (t *tools) authErrors(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := next(ctx, request)
		if err != nil {
			if _, ok := t.authChallenge(err); ok {
				return t.errorResult(err), nil
			}
		}
		return result, err
	}
}
