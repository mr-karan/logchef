package mcp

import (
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
)

// F6-2 decision: an MCP tool call ends at query.mcp_call_timeout_seconds,
// never above query.max_timeout_seconds.
func TestToolCallTimeout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mcp, max int
		want     time.Duration
	}{
		{60, 300, 60 * time.Second},
		{90, 300, 90 * time.Second},
		{60, 30, 30 * time.Second},
		{2, 300, 2 * time.Second},
	} {
		got := toolCallTimeout(config.QueryConfig{MCPCallTimeoutSeconds: tc.mcp, MaxTimeoutSeconds: tc.max})
		if got != tc.want {
			t.Errorf("mcp_call_timeout_seconds=%d max_timeout_seconds=%d: %v, want %v", tc.mcp, tc.max, got, tc.want)
		}
	}
}
