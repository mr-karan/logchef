package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/mr-karan/logchef/internal/core/access"
)

// §5.6: authentication and scope failures carry the OAuth challenge in the
// tool result's _meta; data-access refusals do not.
func TestToolResultAuthChallenge(t *testing.T) {
	t.Parallel()
	tl := &tools{deps: Deps{ResourceMetadataURL: "https://logchef.test/.well-known/oauth-protected-resource/mcp"}}
	challenge := func(r *mcp.CallToolResult) string {
		if r.Meta == nil {
			return ""
		}
		s, _ := r.Meta.AdditionalFields[wwwAuthenticateMetaKey].(string)
		return s
	}

	scope := challenge(tl.errorResult(fmt.Errorf("query logs: %w", access.ErrInsufficientScope)))
	if !strings.Contains(scope, `error="insufficient_scope"`) || !strings.Contains(scope, `resource_metadata="https://logchef.test/.well-known/oauth-protected-resource/mcp"`) ||
		!strings.Contains(scope, "logs:read") || strings.Contains(scope, "offline_access") {
		t.Fatalf("insufficient scope challenge %q", scope)
	}
	if got := challenge(tl.errorResult(fmt.Errorf("get profile: %w", errNoPrincipal))); !strings.Contains(got, `error="invalid_token"`) {
		t.Fatalf("missing principal challenge %q", got)
	}
	for _, err := range []error{access.ErrNotTeamMember, access.ErrSourceNotInTeam, fmt.Errorf("boom")} {
		if got := challenge(tl.errorResult(err)); got != "" {
			t.Fatalf("%v carried a challenge %q", err, got)
		}
	}

	failing := tl.authErrors(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, fmt.Errorf("get teams: %w", access.ErrInsufficientScope)
	})
	result, err := failing(context.Background(), mcp.CallToolRequest{})
	if err != nil || result == nil || !result.IsError || challenge(result) == "" {
		t.Fatalf("middleware result %+v err %v", result, err)
	}
	other := tl.authErrors(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, fmt.Errorf("unrelated")
	})
	if _, err := other(context.Background(), mcp.CallToolRequest{}); err == nil {
		t.Fatal("middleware swallowed a non-auth error")
	}
}
