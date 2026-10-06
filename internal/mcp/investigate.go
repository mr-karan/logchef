package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// Investigation tools: field values, log context, and alerts for incident analysis.

type GetFieldValuesParams struct {
	TeamID    int    `json:"team_id" jsonschema:"Team ID"`
	SourceID  int    `json:"source_id" jsonschema:"Source ID"`
	FieldName string `json:"field_name" jsonschema:"Column name to get distinct values for"`
	FieldType string `json:"field_type" jsonschema:"ClickHouse column type (e.g. String or LowCardinality(String))"`
	StartTime string `json:"start_time" jsonschema:"Start time (RFC3339)"`
	EndTime   string `json:"end_time" jsonschema:"End time (RFC3339)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Max values to return (default 20 max 100)"`
}

type GetLogContextParams struct {
	TeamID      int   `json:"team_id" jsonschema:"Team ID"`
	SourceID    int   `json:"source_id" jsonschema:"Source ID"`
	Timestamp   int64 `json:"timestamp" jsonschema:"Target timestamp in milliseconds (from a log entry)"`
	BeforeLimit int   `json:"before_limit,omitempty" jsonschema:"Number of logs before the target (default 10)"`
	AfterLimit  int   `json:"after_limit,omitempty" jsonschema:"Number of logs after the target (default 10)"`
}

type ListAlertsParams struct {
	SourceID int `json:"source_id,omitempty" jsonschema:"Optional source ID filter. Omit to list every alert visible to the caller."`
}

type GetAlertHistoryParams struct {
	AlertID int `json:"alert_id" jsonschema:"Alert ID"`
}

// --- Handlers ---

// parseTimeRange parses the RFC3339 range that the field value tools take.
func parseTimeRange(startRaw, endRaw string) (start, end time.Time, err error) {
	if startRaw == "" || endRaw == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: start_time and end_time are required", errInvalidArgument)
	}
	start, err = time.Parse(time.RFC3339, startRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: start_time must be RFC3339", errInvalidArgument)
	}
	end, err = time.Parse(time.RFC3339, endRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: end_time must be RFC3339", errInvalidArgument)
	}
	return start, end, nil
}

func (t *tools) fieldValues(ctx context.Context, teamID, sourceID int, fieldName, fieldType, start, end string, limit int) (*core.FieldValuesResult, error) {
	src, err := t.authorizeSource(ctx, teamID, sourceID, models.TokenScopeLogsRead)
	if err != nil {
		return nil, t.storeError("get field values failed", err)
	}
	if fieldName == "" || fieldType == "" {
		return nil, t.storeError("get field values failed", fmt.Errorf("%w: field_name and field_type are required", errInvalidArgument))
	}
	startTime, endTime, err := parseTimeRange(start, end)
	if err != nil {
		return nil, t.storeError("get field values failed", err)
	}
	ctx, cancel := context.WithTimeout(ctx, core.FieldValuesTimeout)
	defer cancel()
	result, err := core.GetFieldValues(ctx, t.deps.Datasources, src, core.FieldValuesParams{
		FieldName: fieldName,
		FieldType: fieldType,
		StartTime: startTime,
		EndTime:   endTime,
		Timezone:  "UTC",
		Limit:     limit,
	})
	if err != nil {
		return nil, t.queryError("get field values failed", err)
	}
	return result, nil
}

// get_field_values returns dynamic field data, so it uses a typed handler.
func (t *tools) handleGetFieldValues(ctx context.Context, _ mcp.CallToolRequest, params GetFieldValuesParams) (*mcp.CallToolResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	result, err := t.fieldValues(ctx, params.TeamID, params.SourceID, params.FieldName, params.FieldType, params.StartTime, params.EndTime, limit)
	if err != nil {
		return t.errorResult(err), nil
	}
	return jsonTextResult(result)
}

// get_log_context returns flexible log data, so it uses a typed handler.
func (t *tools) handleGetLogContext(ctx context.Context, _ mcp.CallToolRequest, params GetLogContextParams) (*mcp.CallToolResult, error) {
	beforeLimit := params.BeforeLimit
	if beforeLimit <= 0 {
		beforeLimit = 10
	}
	if beforeLimit > 100 {
		beforeLimit = 100
	}
	afterLimit := params.AfterLimit
	if afterLimit <= 0 {
		afterLimit = 10
	}
	if afterLimit > 100 {
		afterLimit = 100
	}

	src, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeLogsRead)
	if err != nil {
		return t.errorResult(t.storeError("get log context failed", err)), nil
	}
	if params.Timestamp <= 0 {
		return mcp.NewToolResultError("get log context failed: timestamp is required and must be positive"), nil
	}
	targetTime := time.UnixMilli(params.Timestamp)
	resp, err := core.GetLogContext(ctx, t.deps.Datasources, src, core.LogContextParams{
		TargetTimestamp: params.Timestamp,
		TargetTime:      &targetTime,
		BeforeLimit:     beforeLimit,
		AfterLimit:      afterLimit,
	})
	if err != nil {
		return t.errorResult(t.queryError("get log context failed", err)), nil
	}

	result := map[string]any{
		"target_timestamp": resp.TargetTimestamp,
		"before_logs":      resp.BeforeLogs,
		"target_logs":      resp.TargetLogs,
		"after_logs":       resp.AfterLogs,
		"stats":            resp.Stats,
	}
	return jsonTextResult(result)
}

// list_alerts and get_alert_history return the API JSON as text.
func (t *tools) handleListAlerts(ctx context.Context, _ mcp.CallToolRequest, params ListAlertsParams) (*mcp.CallToolResult, error) {
	alerts, err := t.listAlerts(ctx, params.SourceID)
	if err != nil {
		return t.errorResult(t.storeError("list alerts failed", err)), nil
	}
	return jsonTextResult(alerts)
}

func (t *tools) listAlerts(ctx context.Context, sourceID int) ([]*models.Alert, error) {
	p, err := t.alertsPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if sourceID != 0 {
		return core.ListAlertsBySourceForPrincipal(ctx, t.deps.DB, p, models.SourceID(sourceID))
	}
	return core.ListAlertsForUser(ctx, t.deps.DB, p.User.ID)
}

func (t *tools) handleGetAlertHistory(ctx context.Context, _ mcp.CallToolRequest, params GetAlertHistoryParams) (*mcp.CallToolResult, error) {
	p, err := t.alertsPrincipal(ctx)
	if err != nil {
		return t.errorResult(t.storeError("get alert history failed", err)), nil
	}
	limit := t.deps.Config.Alerts.HistoryLimit
	if limit <= 0 {
		limit = models.DefaultAlertHistoryLimit
	}
	history, err := core.ListAlertHistoryForPrincipal(ctx, t.deps.DB, t.deps.Log, p, models.AlertID(params.AlertID), limit)
	if err != nil {
		return t.errorResult(t.storeError("get alert history failed", err)), nil
	}
	return jsonTextResult(history)
}

// alertsPrincipal returns the principal when alerting is enabled and the
// principal holds alerts:read, as the HTTP alert routes require.
func (t *tools) alertsPrincipal(ctx context.Context) (access.Principal, error) {
	if !t.deps.Config.Alerts.Enabled {
		return access.Principal{}, errAlertsDisabled
	}
	p, err := principalFrom(ctx)
	if err != nil {
		return access.Principal{}, err
	}
	if err := p.Require(models.TokenScopeAlertsRead); err != nil {
		return access.Principal{}, err
	}
	return p, nil
}

func jsonTextResult(value any) (*mcp.CallToolResult, error) {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding tool result: %w", err)
	}
	return mcp.NewToolResultText(string(out)), nil
}

func (t *tools) addInvestigateTools(s *server.MCPServer) {
	fieldValuesTool := mcp.NewTool("get_field_values",
		mcp.WithDescription("Get the top distinct values for a specific field in a time range. Useful for exploring dimensions (e.g. top severity levels, service names, status codes) before writing queries."),
		mcp.WithInputSchema[GetFieldValuesParams](),
		mcp.WithTitleAnnotation("Get Field Values"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(fieldValuesTool, mcp.NewTypedToolHandler(t.handleGetFieldValues))

	logContextTool := mcp.NewTool("get_log_context",
		mcp.WithDescription("Get surrounding log entries (before and after) a specific timestamp. Useful for investigating what happened around a particular event."),
		mcp.WithInputSchema[GetLogContextParams](),
		mcp.WithTitleAnnotation("Get Log Context"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(logContextTool, mcp.NewTypedToolHandler(t.handleGetLogContext))

	listAlertsTool := mcp.NewTool("list_alerts",
		mcp.WithDescription("List all alert rules configured for a source. Shows name, severity, active status, query mode, and last state (firing/resolved)."),
		mcp.WithInputSchema[ListAlertsParams](),
		mcp.WithTitleAnnotation("List Alerts"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(listAlertsTool, mcp.NewTypedToolHandler(t.handleListAlerts))

	alertHistoryTool := mcp.NewTool("get_alert_history",
		mcp.WithDescription("Get the evaluation history for a specific alert. Shows when it triggered, resolved, or errored, with the actual metric values."),
		mcp.WithInputSchema[GetAlertHistoryParams](),
		mcp.WithTitleAnnotation("Get Alert History"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(alertHistoryTool, mcp.NewTypedToolHandler(t.handleGetAlertHistory))
}
