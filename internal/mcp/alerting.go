package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// AlertRuleParams is an unsaved alert rule, as the alert test route takes it.
type AlertRuleParams struct {
	TeamID            int      `json:"team_id" jsonschema:"ID of a team you belong to that has the source"`
	SourceID          int      `json:"source_id" jsonschema:"ID of the source the rule reads"`
	QueryLanguage     string   `json:"query_language" jsonschema:"logchefql for a condition rule; otherwise the source's own language: clickhouse-sql, logsql or logql"`
	EditorMode        string   `json:"editor_mode,omitempty" jsonschema:"condition for a LogchefQL rule, native for the source's own language. Defaults from query_language"`
	Query             string   `json:"query" jsonschema:"A condition: a LogchefQL filter, one aggregate, at most 4 by keys and no time key, such as level=\"error\" | count() as value by service. Native ClickHouse SQL reads its window from {{__time_filter}} and names its value column value"`
	ConditionJSON     string   `json:"condition_json,omitempty" jsonschema:"The filter of a condition rule. Defaults to the part of query in front of the aggregation pipe"`
	LookbackSeconds   int      `json:"lookback_seconds,omitempty" jsonschema:"The window the query reads, ending at each evaluation. Defaults to the server's alert lookback"`
	ThresholdOperator string   `json:"threshold_operator" jsonschema:"gt gte lt lte eq or neq"`
	ThresholdValue    float64  `json:"threshold_value"`
	GroupBy           []string `json:"group_by,omitempty" jsonschema:"Result columns that split a native rule into groups, at most 4. A condition takes them from its by keys"`
	NoDataPolicy      string   `json:"no_data_policy,omitempty" jsonschema:"What a group without a row means: zero (default) compares 0 with the threshold, no_data reports no data, keep keeps the state"`
}

// BacktestAlertParams is an unsaved alert rule and the past range to replay.
type BacktestAlertParams struct {
	AlertRuleParams
	FrequencySeconds int   `json:"frequency_seconds" jsonschema:"Seconds between evaluations, at least 10"`
	RangeSeconds     int   `json:"range_seconds" jsonschema:"How far back to replay: 3600 to 604800 seconds. The replay runs at most 120 evaluations, so a long range steps by range/120 and reports coverage"`
	NotifyOnResolve  *bool `json:"notify_on_resolve,omitempty" jsonschema:"Count resolved notifications. Defaults to true"`
}

type ListAlertSilencesParams struct {
	SourceID int `json:"source_id" jsonschema:"ID of the source whose silences to list"`
}

type ListCollectionsParams struct{}

type ListDestinationsParams struct{}

func (p AlertRuleParams) request(defaultLookback time.Duration) models.TestAlertQueryRequest {
	lookback := p.LookbackSeconds
	if lookback <= 0 {
		lookback = int(defaultLookback.Seconds())
	}
	return models.TestAlertQueryRequest{
		QueryLanguage:     models.QueryLanguage(p.QueryLanguage),
		EditorMode:        models.AlertEditorMode(p.EditorMode),
		Query:             p.Query,
		ConditionJSON:     p.ConditionJSON,
		LookbackSeconds:   lookback,
		ThresholdOperator: models.AlertThresholdOperator(p.ThresholdOperator),
		ThresholdValue:    p.ThresholdValue,
		GroupBy:           p.GroupBy,
		NoDataPolicy:      models.AlertNoDataPolicy(p.NoDataPolicy),
	}
}

func (t *tools) addAlertingTools(s *server.MCPServer) {
	testTool := mcp.NewTool("test_alert",
		mcp.WithDescription("Run an unsaved alert rule's query once over its lookback, as the alert form's Test does. Returns the value, whether it meets the threshold, up to 20 group samples, no_data and warnings. Saves nothing and sends nothing."),
		mcp.WithInputSchema[AlertRuleParams](),
		mcp.WithTitleAnnotation("Test Alert Rule"),
		readOnlyTool,
	)
	s.AddTool(testTool, mcp.NewTypedToolHandler(t.handleTestAlert))

	backtestTool := mcp.NewTool("backtest_alert",
		mcp.WithDescription("Replay an unsaved alert rule over a past range through the same group, no-data and forget rules as live evaluation, to see when it would have fired. Returns episodes, notifications, firing time, breaching evaluations, coverage, a per-evaluation series and the groups that breached most. Saves nothing, sends nothing and ignores silences. One backtest runs per user at a time. ClickHouse SQL must read its window from {{__time_filter}}."),
		mcp.WithInputSchema[BacktestAlertParams](),
		mcp.WithTitleAnnotation("Backtest Alert Rule"),
		readOnlyTool,
	)
	s.AddTool(backtestTool, mcp.NewTypedToolHandler(t.handleBacktestAlert))

	silencesTool := mcp.NewTool("list_alert_silences",
		mcp.WithDescription("List the alert silences of a source: the rule each one covers (no alert_id means every rule of the source), its label matchers, its once or weekly schedule and whether it is active now. A silence mutes notifications only; rules still evaluate."),
		mcp.WithInputSchema[ListAlertSilencesParams](),
		mcp.WithTitleAnnotation("List Alert Silences"),
		readOnlyTool,
	)
	s.AddTool(silencesTool, mcp.NewTypedToolHandler(t.handleListAlertSilences))

	collectionsTool := mcp.NewTool("list_collections",
		mcp.WithDescription("List the Library collections you take part in: your personal collection and the shared ones, with your role, member, team and item counts. A collection groups saved queries; list_saved_queries lists the queries."),
		mcp.WithInputSchema[ListCollectionsParams](),
		mcp.WithTitleAnnotation("List Collections"),
		readOnlyTool,
	)
	s.AddTool(collectionsTool, mcp.NewTypedToolHandler(t.handleListCollections))

	destinationsTool := mcp.NewTool("list_destinations",
		mcp.WithDescription("List the notification destinations you may use in an alert rule: id, name, type (email, webhook, slack or pagerduty), scope (private, team or global), team and whether it is active. Recipients, receiver URLs and keys are never shown."),
		mcp.WithInputSchema[ListDestinationsParams](),
		mcp.WithTitleAnnotation("List Notification Destinations"),
		readOnlyTool,
	)
	s.AddTool(destinationsTool, mcp.NewTypedToolHandler(t.handleListDestinations))
}

func (t *tools) handleTestAlert(ctx context.Context, _ mcp.CallToolRequest, params AlertRuleParams) (*mcp.CallToolResult, error) {
	const op = "test alert failed"
	p, err := t.alertsPrincipal(ctx)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	src, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeAlertsRead)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	req := params.request(t.deps.Config.Alerts.DefaultLookback)
	var result *models.TestAlertQueryResponse
	err = t.admit(ctx, QueryClassPreview, src, req.Query, func(ctx context.Context, _ string) error {
		var runErr error
		result, runErr = core.TestAlertQueryForPrincipal(ctx, t.deps.DB, t.deps.Datasources, p, src.SourceID(), &req)
		return runErr
	})
	if err != nil {
		return t.errorResult(t.queryError(op, err)), nil
	}
	return jsonTextResult(result)
}

func (t *tools) handleBacktestAlert(ctx context.Context, _ mcp.CallToolRequest, params BacktestAlertParams) (*mcp.CallToolResult, error) {
	const op = "backtest alert failed"
	p, err := t.alertsPrincipal(ctx)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	src, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeAlertsRead)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	req := models.BacktestAlertRequest{
		TestAlertQueryRequest: params.request(t.deps.Config.Alerts.DefaultLookback),
		FrequencySeconds:      params.FrequencySeconds,
		RangeSeconds:          params.RangeSeconds,
		NotifyOnResolve:       params.NotifyOnResolve,
	}
	var result *models.BacktestResult
	err = t.admit(ctx, QueryClassBacktest, src, req.Query, func(ctx context.Context, _ string) error {
		var runErr error
		result, runErr = core.BacktestAlertForPrincipal(ctx, t.deps.DB, t.deps.Datasources, p, src.SourceID(), &req)
		return runErr
	})
	if err != nil {
		return t.errorResult(t.queryError(op, err)), nil
	}
	if result.FirstErr != nil {
		result.FirstError = t.queryError("alert evaluation", result.FirstErr).Error()
	}
	return jsonTextResult(result)
}

func (t *tools) handleListAlertSilences(ctx context.Context, _ mcp.CallToolRequest, params ListAlertSilencesParams) (*mcp.CallToolResult, error) {
	const op = "list alert silences failed"
	if params.SourceID <= 0 {
		return t.errorResult(fmt.Errorf("%s: %w: source_id is required", op, errInvalidArgument)), nil
	}
	p, err := t.alertsPrincipal(ctx)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	silences, err := core.ListSilencesForPrincipal(ctx, t.deps.DB, t.deps.Log, p, models.SourceID(params.SourceID), time.Now())
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	return jsonTextResult(silences)
}

func (t *tools) handleListCollections(ctx context.Context, _ mcp.CallToolRequest, _ ListCollectionsParams) (*mcp.CallToolResult, error) {
	const op = "list collections failed"
	p, err := principalFrom(ctx)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	if err := p.Require(models.TokenScopeCollectionsRead); err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	collections, err := t.deps.DB.ListCollectionsForUser(ctx, p.User.ID)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	return jsonTextResult(collections)
}

func (t *tools) handleListDestinations(ctx context.Context, _ mcp.CallToolRequest, _ ListDestinationsParams) (*mcp.CallToolResult, error) {
	const op = "list destinations failed"
	p, err := principalFrom(ctx)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	destinations, err := core.ListDestinationSummariesForPrincipal(ctx, t.deps.DB, p)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	return jsonTextResult(destinations)
}
