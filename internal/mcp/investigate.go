package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
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
	Query     string `json:"query,omitempty" jsonschema:"Optional LogchefQL filter (e.g. level=error). Counts only the matching logs. Empty counts every log in the window."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Max values to return (default 20 max 100)"`
}

type GetLogContextParams struct {
	TeamID      int   `json:"team_id" jsonschema:"Team ID"`
	SourceID    int   `json:"source_id" jsonschema:"Source ID"`
	Timestamp   int64 `json:"timestamp" jsonschema:"Target timestamp in milliseconds (from a log entry)"`
	BeforeLimit int   `json:"before_limit,omitempty" jsonschema:"Number of logs before the target (default 10)"`
	AfterLimit  int   `json:"after_limit,omitempty" jsonschema:"Number of logs after the target (default 10)"`
	// Loki keeps context to the row's stream, which only the row's stream
	// labels identify.
	StreamLabels map[string]string `json:"stream_labels,omitempty" jsonschema:"Loki sources only, and required there: the target row's value for every schema field of type label. Rows from a LogchefQL query carry them all"`
}

type ListAlertsParams struct {
	SourceID int `json:"source_id,omitempty" jsonschema:"Optional source ID filter. Omit to list every alert visible to the caller."`
}

type GetAlertHistoryParams struct {
	AlertID int    `json:"alert_id" jsonschema:"Alert ID"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Most episodes to return, newest first: 1 to 100, default 20"`
	Since   string `json:"since,omitempty" jsonschema:"Only episodes that started at or after this RFC3339 time"`
	Status  string `json:"status,omitempty" jsonschema:"Only episodes with this status: triggered, no_data, resolved or error"`
}

// Alert history bounds of the MCP tool.
const (
	defaultHistoryEpisodes = 20
	maxHistoryEpisodes     = 100
	// maxHistoryScan bounds the rows read to apply since and status.
	maxHistoryScan = 500
)

// alertHistoryResult names the rule once, so the episodes need not repeat
// its query.
type alertHistoryResult struct {
	Alert   alertHistoryRule            `json:"alert"`
	History []*models.AlertHistoryEntry `json:"history"`
}

type alertHistoryRule struct {
	ID            models.AlertID       `json:"id"`
	Name          string               `json:"name"`
	QueryLanguage models.QueryLanguage `json:"query_language"`
	Query         string               `json:"query"`
}

// ruleFieldsInPayload are the payload entries that copy the rule, which the
// result states once.
var ruleFieldsInPayload = []string{"query", "condition_json", "query_language", "editor_mode"}

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

// checkFieldFilter checks a field value tool's LogchefQL filter the way the
// LogchefQL query tools check theirs. An empty filter matches every log.
func (t *tools) checkFieldFilter(ctx context.Context, src access.AuthorizedSource, query string) error {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, core.SchemaTimeout)
	defer cancel()
	_, err := core.CompileLogchefQL(ctx, t.deps.Datasources, src, datasource.LogchefQLCompileRequest{Query: query})
	return err
}

// fieldValues reads one field's values for an already authorized source,
// over the logs that query matches. The caller must hold an admission slot
// and have checked query: get_field_values admits each call, and top_values
// runs every field under its one slot.
func (t *tools) fieldValues(ctx context.Context, src access.AuthorizedSource, fieldName, fieldType, query string, start, end time.Time, limit int) (*core.FieldValuesResult, error) {
	ctx, cancel := context.WithTimeout(ctx, core.FieldValuesTimeout)
	defer cancel()
	result, err := core.GetFieldValues(ctx, t.deps.Datasources, src, core.FieldValuesParams{
		FieldName: fieldName,
		FieldType: fieldType,
		Language:  models.QueryLanguageLogchefQL,
		QueryText: query,
		StartTime: start,
		EndTime:   end,
		Timezone:  "UTC",
		Limit:     limit,
	})
	if err != nil {
		return nil, t.queryError("get field values failed", err)
	}
	return result, nil
}

// get_field_values returns dynamic field data, so it uses a typed handler.
func (t *tools) handleGetFieldValues(ctx context.Context, req mcp.CallToolRequest, params GetFieldValuesParams) (*mcp.CallToolResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	src, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeLogsRead)
	if err != nil {
		return t.errorResult(t.storeError("get field values failed", err)), nil
	}
	read := t.startRead(ctx, req, ReadCall{Source: src, Query: params.Query, Language: models.QueryLanguageLogchefQL, Limit: limit, Fields: []string{params.FieldName}})
	defer func() { read.end(recover()) }()
	if params.FieldName == "" || params.FieldType == "" {
		read.Err = fmt.Errorf("%w: field_name and field_type are required", errInvalidArgument)
		return t.errorResult(t.storeError("get field values failed", read.Err)), nil
	}
	start, end, err := parseTimeRange(params.StartTime, params.EndTime)
	if err != nil {
		read.Err = err
		return t.errorResult(t.storeError("get field values failed", err)), nil
	}
	read.From, read.To = &start, &end
	var result *core.FieldValuesResult
	err = t.admit(ctx, QueryClassPreview, src, "get_field_values: "+params.FieldName, func(ctx context.Context, _ string) error {
		if err := t.checkFieldFilter(ctx, src, params.Query); err != nil {
			return t.queryError("get field values failed", err)
		}
		var err error
		result, err = t.fieldValues(ctx, src, params.FieldName, params.FieldType, params.Query, start, end, limit)
		return err
	})
	if err != nil {
		read.Err = err
		if _, ok := errors.AsType[*admissionError](err); ok {
			return t.errorResult(t.queryError("get field values failed", err)), nil
		}
		return t.errorResult(err), nil
	}
	read.Rows = int64(len(result.Values))
	return jsonTextResult(result)
}

// get_log_context returns flexible log data, so it uses a typed handler.
func (t *tools) handleGetLogContext(ctx context.Context, req mcp.CallToolRequest, params GetLogContextParams) (*mcp.CallToolResult, error) {
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
	read := t.startRead(ctx, req, ReadCall{Source: src, Limit: beforeLimit + afterLimit, Anchor: &targetTime})
	defer func() { read.end(recover()) }()
	var resp *models.LogContextResponse
	err = t.admit(ctx, QueryClassPreview, src, "get_log_context", func(ctx context.Context, _ string) error {
		var err error
		resp, err = core.GetLogContext(ctx, t.deps.Datasources, src, core.LogContextParams{
			TargetTimestamp: params.Timestamp,
			TargetTime:      &targetTime,
			BeforeLimit:     beforeLimit,
			AfterLimit:      afterLimit,
			StreamLabels:    params.StreamLabels,
		})
		return err
	})
	if err != nil {
		read.Err = err
		return t.errorResult(t.queryError("get log context failed", err)), nil
	}
	read.Rows = int64(len(resp.BeforeLogs) + len(resp.TargetLogs) + len(resp.AfterLogs))

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
	const op = "get alert history failed"
	limit := params.Limit
	switch {
	case limit == 0:
		limit = defaultHistoryEpisodes
	case limit < 0 || limit > maxHistoryEpisodes:
		return t.errorResult(fmt.Errorf("%s: %w: limit must be 1 to %d", op, errInvalidArgument, maxHistoryEpisodes)), nil
	}
	var since time.Time
	if params.Since != "" {
		parsed, err := time.Parse(time.RFC3339, params.Since)
		if err != nil {
			return t.errorResult(fmt.Errorf("%s: %w: since must be an RFC3339 time: %w", op, errInvalidArgument, err)), nil
		}
		since = parsed
	}
	status := models.AlertStatus(params.Status)
	switch status {
	case "", models.AlertStatusTriggered, models.AlertStatusNoData, models.AlertStatusResolved, models.AlertStatusError:
	default:
		return t.errorResult(fmt.Errorf("%s: %w: status must be triggered, no_data, resolved or error", op, errInvalidArgument)), nil
	}

	p, err := t.alertsPrincipal(ctx)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	alert, err := core.GetAlertForPrincipal(ctx, t.deps.DB, t.deps.Log, p, models.AlertID(params.AlertID))
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	scan := limit
	if !since.IsZero() || status != "" {
		scan = maxHistoryScan
	}
	history, err := core.ListAlertHistory(ctx, t.deps.DB, alert.ID, scan)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	kept := make([]*models.AlertHistoryEntry, 0, min(limit, len(history)))
	for _, entry := range history {
		if len(kept) == limit {
			break
		}
		if entry.TriggeredAt.Before(since) || (status != "" && entry.Status != status) {
			continue
		}
		kept = append(kept, withoutRuleFields(entry))
	}
	return jsonTextResult(alertHistoryResult{
		Alert:   alertHistoryRule{ID: alert.ID, Name: alert.Name, QueryLanguage: alert.QueryLanguage, Query: alert.Query},
		History: kept,
	})
}

// withoutRuleFields copies entry without the payload entries that repeat the
// rule, at the top level and in the annotations.
func withoutRuleFields(entry *models.AlertHistoryEntry) *models.AlertHistoryEntry {
	trimmed := *entry
	if entry.Payload == nil {
		return &trimmed
	}
	trimmed.Payload = maps.Clone(entry.Payload)
	for _, key := range ruleFieldsInPayload {
		delete(trimmed.Payload, key)
	}
	switch annotations := trimmed.Payload["annotations"].(type) {
	case map[string]any:
		kept := maps.Clone(annotations)
		for _, key := range ruleFieldsInPayload {
			delete(kept, key)
		}
		trimmed.Payload["annotations"] = kept
	case map[string]string:
		kept := maps.Clone(annotations)
		for _, key := range ruleFieldsInPayload {
			delete(kept, key)
		}
		trimmed.Payload["annotations"] = kept
	}
	return &trimmed
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
		mcp.WithDescription("Get the top distinct values for a specific field in a time range, with counts. Counts every log in the window unless query filters them. Useful for exploring dimensions (e.g. top severity levels, service names, status codes) before writing queries."),
		mcp.WithInputSchema[GetFieldValuesParams](),
		mcp.WithTitleAnnotation("Get Field Values"),
		readOnlyTool,
	)
	s.AddTool(fieldValuesTool, mcp.NewTypedToolHandler(t.handleGetFieldValues))

	logContextTool := mcp.NewTool("get_log_context",
		mcp.WithDescription("Get surrounding log entries (before and after) a specific timestamp. Useful for investigating what happened around a particular event."),
		mcp.WithInputSchema[GetLogContextParams](),
		mcp.WithTitleAnnotation("Get Log Context"),
		readOnlyTool,
	)
	s.AddTool(logContextTool, mcp.NewTypedToolHandler(t.handleGetLogContext))

	listAlertsTool := mcp.NewTool("list_alerts",
		mcp.WithDescription("List alert rules, optionally for one source. Shows name, severity, active status, query and threshold, group_by, no_data_policy, last_state (firing, no_data or resolved) and instance_counts (groups firing, with no data, and ok)."),
		mcp.WithInputSchema[ListAlertsParams](),
		mcp.WithTitleAnnotation("List Alerts"),
		readOnlyTool,
	)
	s.AddTool(listAlertsTool, mcp.NewTypedToolHandler(t.handleListAlerts))

	alertHistoryTool := mcp.NewTool("get_alert_history",
		mcp.WithDescription("Get the history of a specific alert, newest first: 20 episodes unless limit says otherwise (at most 100), optionally since a time and of one status. Each entry is one episode of one group (group_key, group) with status triggered, no_data, resolved or error, the value, its times and the state of each notification delivery. silenced_by in the payload means a silence muted the episode. The rule's query is given once, as alert.query, not in each entry."),

		mcp.WithInputSchema[GetAlertHistoryParams](),
		mcp.WithTitleAnnotation("Get Alert History"),
		readOnlyTool,
	)
	s.AddTool(alertHistoryTool, mcp.NewTypedToolHandler(t.handleGetAlertHistory))
}
