package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// Discovery tools: bulk field exploration.

// --- Input schemas ---

type GetAllFieldDimensionsParams struct {
	TeamID    int    `json:"team_id" jsonschema:"Team ID"`
	SourceID  int    `json:"source_id" jsonschema:"Source ID"`
	StartTime string `json:"start_time" jsonschema:"Start time (RFC3339)"`
	EndTime   string `json:"end_time" jsonschema:"End time (RFC3339)"`
	Timezone  string `json:"timezone,omitempty" jsonschema:"Timezone (default UTC)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Max values per field (default 10 max 100)"`
}

// --- Handlers ---

// get_all_field_dimensions returns dynamic data, so it uses a typed handler.
func (t *tools) handleGetAllFieldDimensions(ctx context.Context, _ mcp.CallToolRequest, params GetAllFieldDimensionsParams) (*mcp.CallToolResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	timezone := params.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	src, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeLogsRead)
	if err != nil {
		return t.errorResult(t.storeError("get all field dimensions failed", err)), nil
	}
	startTime, endTime, err := parseTimeRange(params.StartTime, params.EndTime)
	if err != nil {
		return t.errorResult(t.storeError("get all field dimensions failed", err)), nil
	}
	var result map[string]*core.FieldValuesResult
	err = t.admit(ctx, QueryClassPreview, src, "get_all_field_dimensions", func(ctx context.Context, _ string) error {
		ctx, cancel := context.WithTimeout(ctx, core.FieldValuesTimeout)
		defer cancel()
		var err error
		result, err = core.GetAllFieldValues(ctx, t.deps.Datasources, src, core.AllFieldValuesParams{
			StartTime: startTime,
			EndTime:   endTime,
			Timezone:  timezone,
			Limit:     limit,
		})
		return err
	})
	if err != nil {
		return t.errorResult(t.queryError("get all field dimensions failed", err)), nil
	}
	return jsonTextResult(result)
}

func (t *tools) addDiscoverTools(s *server.MCPServer) {
	dimensionsTool := mcp.NewTool("get_all_field_dimensions",
		mcp.WithDescription("Get top values for all LowCardinality fields in one call. Returns a map of field names to their top values with counts. Much faster than calling get_field_values for each field individually. Use this for initial source exploration to understand what dimensions exist."),
		mcp.WithInputSchema[GetAllFieldDimensionsParams](),
		mcp.WithTitleAnnotation("Get All Field Dimensions"),
		readOnlyTool,
	)
	s.AddTool(dimensionsTool, mcp.NewTypedToolHandler(t.handleGetAllFieldDimensions))
}
