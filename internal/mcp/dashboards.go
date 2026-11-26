package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

type ListDashboardsParams struct{}

type GetDashboardParams struct {
	DashboardID int `json:"dashboard_id" jsonschema:"Dashboard ID, from list_dashboards"`
}

func (t *tools) addDashboardTools(s *server.MCPServer) {
	listTool := mcp.NewTool("list_dashboards",
		mcp.WithDescription("List the dashboards you can see: those you created and those with a panel on a team you belong to. Each entry has the id, name, description, panel count, panel kinds, tab count and revision, and no panels; get_dashboard reads them."),
		mcp.WithInputSchema[ListDashboardsParams](),
		mcp.WithTitleAnnotation("List Dashboards"),
		readOnlyTool,
	)
	s.AddTool(listTool, mcp.NewTypedToolHandler(t.handleListDashboards))

	getTool := mcp.NewTool("get_dashboard",
		mcp.WithDescription("Get one dashboard with its schema 2 panels: tabs and their layouts, and each panel's team, source, query and visualization. A panel on a source you cannot reach comes back locked, without its query."),
		mcp.WithInputSchema[GetDashboardParams](),
		mcp.WithTitleAnnotation("Get Dashboard"),
		readOnlyTool,
	)
	s.AddTool(getTool, mcp.NewTypedToolHandler(t.handleGetDashboard))
}

func (t *tools) handleListDashboards(ctx context.Context, _ mcp.CallToolRequest, _ ListDashboardsParams) (*mcp.CallToolResult, error) {
	const op = "list dashboards failed"
	p, err := principalFrom(ctx)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	summaries, err := core.ListDashboards(ctx, t.deps.DB, t.deps.Log, core.DashboardViewer(p))
	if errors.Is(err, core.ErrDashboardListUnavailable) {
		return t.errorResult(fmt.Errorf("%s: the dashboard list is being repaired; retry shortly", op)), nil
	}
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	return jsonTextResult(summaries)
}

func (t *tools) handleGetDashboard(ctx context.Context, _ mcp.CallToolRequest, params GetDashboardParams) (*mcp.CallToolResult, error) {
	const op = "get dashboard failed"
	p, err := principalFrom(ctx)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	viewer := core.DashboardViewer(p)
	dashboard, err := core.GetDashboard(ctx, t.deps.DB, t.deps.Log, params.DashboardID)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	visible, err := core.UserCanViewDashboard(ctx, t.deps.DB, viewer, dashboard)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	if !visible {
		// Not found, so the answer does not confirm that the dashboard exists.
		return t.errorResult(fmt.Errorf("%s: %w", op, core.ErrDashboardNotFound)), nil
	}
	schema := "2"
	client, err := models.ParseDashboardClient(&schema, strings.Join(models.KnownDashboardFeatures(), ","))
	if err != nil {
		return nil, fmt.Errorf("building the dashboard client: %w", err)
	}
	resp, err := core.BuildDashboardResponse(ctx, t.deps.DB, t.deps.Log, viewer, client, dashboard)
	if err != nil {
		return t.errorResult(t.storeError(op, err)), nil
	}
	return jsonTextResult(resp)
}
