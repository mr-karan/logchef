package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/mcp/ui"
)

const InvestigationURI = "ui://logchef/investigation.html"
const appMIMEType = "text/html;profile=mcp-app"

type OpenInvestigationParams struct {
	TeamID    int    `json:"team_id,omitempty" jsonschema:"Optional team ID. Supply together with source_id."`
	SourceID  int    `json:"source_id,omitempty" jsonschema:"Optional source ID. Supply together with team_id."`
	Query     string `json:"query,omitempty" jsonschema:"LogchefQL filter. Empty selects all logs."`
	StartTime string `json:"start_time,omitempty" jsonschema:"Inclusive RFC3339 start, default one hour ago."`
	EndTime   string `json:"end_time,omitempty" jsonschema:"RFC3339 end, default now. Maximum range is seven days."`
}

type InvestigationState struct {
	TeamID    int    `json:"team_id,omitempty"`
	SourceID  int    `json:"source_id,omitempty"`
	Query     string `json:"query"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Timezone  string `json:"timezone"`
}

func handleOpenInvestigation(_ context.Context, _ mcp.CallToolRequest, params OpenInvestigationParams) (InvestigationState, error) {
	if params.TeamID < 0 || params.SourceID < 0 || (params.TeamID == 0) != (params.SourceID == 0) {
		return InvestigationState{}, fmt.Errorf("supply positive team_id and source_id together, or omit both")
	}
	end := time.Now().UTC()
	var err error
	if params.EndTime != "" {
		end, err = time.Parse(time.RFC3339, params.EndTime)
		if err != nil {
			return InvestigationState{}, fmt.Errorf("end_time must be RFC3339")
		}
	}
	start := end.Add(-time.Hour)
	if params.StartTime != "" {
		start, err = time.Parse(time.RFC3339, params.StartTime)
		if err != nil {
			return InvestigationState{}, fmt.Errorf("start_time must be RFC3339")
		}
	}
	if !start.Before(end) || end.Sub(start) > 7*24*time.Hour {
		return InvestigationState{}, fmt.Errorf("time range must be positive and at most seven days")
	}
	return InvestigationState{TeamID: params.TeamID, SourceID: params.SourceID, Query: params.Query, StartTime: start.UTC().Format(time.RFC3339), EndTime: end.UTC().Format(time.RFC3339), Timezone: "UTC"}, nil
}

func addInvestigationApp(s *server.MCPServer) {
	tool := mcp.NewTool("open_investigation",
		mcp.WithToolTitle("Investigate logs"),
		mcp.WithDescription("Open the interactive Logchef investigation panel. Browse sources, run bounded LogchefQL queries, select histogram intervals or log rows, and attach evidence to the conversation. The panel runs the requested query when it opens, and a later call replaces the query in an open panel."),
		mcp.WithInputSchema[OpenInvestigationParams](), mcp.WithOutputSchema[InvestigationState](),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	tool.Meta = mcp.NewMetaFromMap(map[string]any{
		"ui":        map[string]any{"resourceUri": InvestigationURI},
		"openai/ui": map[string]any{"entrypoints": []map[string]string{{"type": "global"}, {"type": "thread"}}},
	})
	s.AddTool(tool, mcp.NewStructuredToolHandler(handleOpenInvestigation))
	resource := mcp.NewResource(InvestigationURI, "Logchef investigation", mcp.WithMIMEType(appMIMEType))
	s.AddResource(resource, func(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return []mcp.ResourceContents{mcp.TextResourceContents{URI: InvestigationURI, MIMEType: appMIMEType, Text: ui.InvestigationHTML,
			Meta: map[string]any{
				"openai/ui":                map[string]any{"availableDisplayModes": []string{"inline", "fullscreen"}},
				"openai/widgetDescription": "Inspect bounded log queries, select evidence, and compare time windows without leaving the conversation.",
				"ui": map[string]any{
					"prefersBorder": true,
					"csp":           map[string]any{"connectDomains": []string{}, "resourceDomains": []string{}},
				},
			},
		}}, nil
	})
}
