package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// addPrompts registers MCP prompts for investigation workflows.
func addPrompts(s *server.MCPServer) {
	// Investigate error spike prompt
	s.AddPrompt(
		mcp.NewPrompt("investigate_error_spike",
			mcp.WithPromptDescription("Guided investigation workflow for diagnosing an error spike in a log source. Walks through schema discovery, error pattern analysis, timeline correlation, and root cause identification."),
			mcp.WithArgument("team_id", mcp.RequiredArgument(), mcp.ArgumentDescription("Team ID that owns the source")),
			mcp.WithArgument("source_id", mcp.RequiredArgument(), mcp.ArgumentDescription("Source ID to investigate")),
			mcp.WithArgument("time_range", mcp.ArgumentDescription("Time range to investigate (e.g. 'last 1h', '2024-01-15 10:00 to 12:00'). Defaults to last 1 hour.")),
		),
		handleInvestigateErrorSpike,
	)

	// Investigate alert prompt. Alerts are not team-scoped, so only alert_id
	// is needed. Source and team are optional hints for schema and log lookups.
	s.AddPrompt(
		mcp.NewPrompt("investigate_alert",
			mcp.WithPromptDescription("Guided investigation workflow for a specific alert. Reviews alert configuration, recent history, matches log patterns, and suggests remediation."),
			mcp.WithArgument("alert_id", mcp.RequiredArgument(), mcp.ArgumentDescription("Alert ID to investigate")),
			mcp.WithArgument("team_id", mcp.ArgumentDescription("Team ID to use for related schema/log lookups (optional)")),
			mcp.WithArgument("source_id", mcp.ArgumentDescription("Source ID the alert is configured on (optional; helps schema/log lookups)")),
		),
		handleInvestigateAlert,
	)
}

func handleInvestigateErrorSpike(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	teamID := request.Params.Arguments["team_id"]
	sourceID := request.Params.Arguments["source_id"]
	timeRange := request.Params.Arguments["time_range"]
	if timeRange == "" {
		timeRange = "last 1 hour"
	}

	instructions := fmt.Sprintf(`You are investigating an error spike in a Logchef log source.

**Context:**
- Team ID: %s
- Source ID: %s
- Time range: %s

**Investigation Steps:**

1. **Discover Schema**: Use the get_source_schema tool (team_id=%s, source_id=%s) to understand available columns and their types.

2. **Assess Error Volume**: Use query_logchefql to count errors over the time range. Start broad:
   - Query: severity_text=ERROR
   - Then narrow with get_log_histogram to visualize the spike pattern, grouping by severity_text.

3. **Identify Error Patterns**: Use get_field_values to explore key dimensions:
   - Check top values for service-related fields (service_name, component, etc.)
   - Check top error messages or error codes if available
   - Identify which services/components are contributing most to the spike

4. **Deep Dive**: Query specific error patterns using query_logchefql with filters identified in step 3.
   - Look for common stack traces, error messages, or request paths
   - Use get_log_context on interesting entries to see surrounding logs

5. **Timeline Correlation**: Use get_log_histogram with different group_by fields to correlate:
   - Did the spike start at a specific time?
   - Does it correlate with a deployment, config change, or external dependency?
   - Are there periodic patterns?

6. **Check Alerts**: Use list_alerts to see if any existing alerts fired during this period. Use get_alert_history for recent triggers on specific alerts.

7. **Summarize Findings**: Present:
   - When the spike started and its duration
   - Which services/components are affected
   - The dominant error patterns
   - Likely root cause
   - Suggested next steps or remediation`, teamID, sourceID, timeRange, teamID, sourceID)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Investigate error spike in source %s (team %s)", sourceID, teamID),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: instructions,
				},
			},
		},
	}, nil
}

func handleInvestigateAlert(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	alertID := request.Params.Arguments["alert_id"]
	teamID := request.Params.Arguments["team_id"]
	sourceID := request.Params.Arguments["source_id"]

	instructions := fmt.Sprintf(`You are investigating a specific alert in a Logchef log source.

**Context:**
- Alert ID: %s
- Team ID (optional, for schema/log lookups): %s
- Source ID (optional, for schema/log lookups): %s

**Investigation Steps:**

1. **Review Alert Configuration**: Use list_alerts (optionally filtered by source_id=%s) to find the alert details — its name, severity, query, threshold, and current state.

2. **Check Alert History**: Use get_alert_history (alert_id=%s) to see recent evaluation results:
   - When did it last fire?
   - How frequently has it been triggering?
   - Has it been flapping (firing/resolving repeatedly)?

3. **Understand the Schema**: Use get_source_schema (team_id=%s, source_id=%s) to understand available columns for deeper investigation.

4. **Reproduce the Alert Query**: Run the alert's query using query_logchefql or query_logs to see the actual matching logs. Examine:
   - Are the logs genuine errors or false positives?
   - Has the volume changed significantly?

5. **Explore Context**: For key log entries found in step 4:
   - Use get_log_context to see surrounding logs
   - Use get_field_values to explore related dimensions
   - Use get_log_histogram to visualize the pattern over time

6. **Correlate**: Check if other alerts on this source are also firing (list_alerts with source_id=%s). Look for common patterns.

7. **Summarize Findings**: Present:
   - Alert details and current state
   - Whether the alert is firing correctly or is a false positive
   - The underlying log pattern causing the alert
   - Recommended actions (acknowledge, tune threshold, fix root cause, etc.)`, alertID, teamID, sourceID, sourceID, alertID, teamID, sourceID, sourceID)

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Investigate alert %s", alertID),
		Messages: []mcp.PromptMessage{
			{
				Role: mcp.RoleUser,
				Content: mcp.TextContent{
					Type: "text",
					Text: instructions,
				},
			},
		},
	}, nil
}
