package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// addResourceTemplates registers MCP resource templates for Logchef entities.
func (t *tools) addResourceTemplates(s *server.MCPServer) {
	// Source schema resource template
	s.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"logchef://team/{team_id}/source/{source_id}/schema",
			"Source Schema",
			mcp.WithTemplateDescription("ClickHouse table schema (column names and types) for a log source. Use this to understand available fields before writing queries."),
			mcp.WithTemplateMIMEType("application/json"),
		),
		t.handleSourceSchemaResource,
	)

	// Saved queries are source-scoped, not team-scoped.
	s.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"logchef://source/{source_id}/saved-queries",
			"Saved Queries",
			mcp.WithTemplateDescription("Saved queries pinned to a log source. Each entry has a query_type (logchefql or sql), query_content (JSON envelope), and source metadata."),
			mcp.WithTemplateMIMEType("application/json"),
		),
		t.handleSavedQueriesListResource,
	)

	// Single saved query by its global ID.
	s.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"logchef://saved-query/{query_id}",
			"Saved Query",
			mcp.WithTemplateDescription("A single saved query with its name, description, query_type, query_content, and source metadata."),
			mcp.WithTemplateMIMEType("application/json"),
		),
		t.handleSavedQueryResource,
	)
}

func (t *tools) handleSourceSchemaResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	teamID, sourceID, err := parseTeamSourceURI(request.Params.URI)
	if err != nil {
		return nil, err
	}
	schema, err := t.sourceSchema(ctx, teamID, sourceID)
	if err != nil {
		return nil, err
	}
	return jsonResource(request.Params.URI, schema)
}

func (t *tools) handleSavedQueriesListResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	sourceID, err := parseSourceURI(request.Params.URI)
	if err != nil {
		return nil, err
	}
	queries, err := t.listSavedQueries(ctx, sourceID)
	if err != nil {
		return nil, t.storeError("list saved queries", err)
	}
	return jsonResource(request.Params.URI, queries)
}

func (t *tools) handleSavedQueryResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	queryID, err := parseSavedQueryURI(request.Params.URI)
	if err != nil {
		return nil, err
	}
	got, err := t.getSavedQuery(ctx, queryID)
	if err != nil {
		return nil, t.storeError("get saved query", err)
	}
	return jsonResource(request.Params.URI, got)
}

func jsonResource(uri string, value any) ([]mcp.ResourceContents, error) {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding resource: %w", err)
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(out),
		},
	}, nil
}

// parseTeamSourceURI extracts team_id and source_id from URIs like
// logchef://team/{team_id}/source/{source_id}/...
func parseTeamSourceURI(uri string) (teamID, sourceID int, err error) {
	parts := strings.Split(strings.TrimPrefix(uri, "logchef://"), "/")
	if len(parts) < 4 || parts[0] != "team" || parts[2] != "source" {
		return 0, 0, fmt.Errorf("invalid URI format: %s", uri)
	}

	teamID, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid team_id in URI: %s", parts[1])
	}

	sourceID, err = strconv.Atoi(parts[3])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid source_id in URI: %s", parts[3])
	}

	return teamID, sourceID, nil
}

// parseSourceURI extracts source_id from URIs like
// logchef://source/{source_id}/saved-queries
func parseSourceURI(uri string) (int, error) {
	parts := strings.Split(strings.TrimPrefix(uri, "logchef://"), "/")
	if len(parts) < 2 || parts[0] != "source" {
		return 0, fmt.Errorf("invalid source URI format: %s", uri)
	}

	sourceID, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid source_id in URI: %s", parts[1])
	}

	return sourceID, nil
}

// parseSavedQueryURI extracts query_id from URIs like
// logchef://saved-query/{query_id}
func parseSavedQueryURI(uri string) (int, error) {
	parts := strings.Split(strings.TrimPrefix(uri, "logchef://"), "/")
	if len(parts) < 2 || parts[0] != "saved-query" {
		return 0, fmt.Errorf("invalid saved-query URI format: %s", uri)
	}

	queryID, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid query_id in URI: %s", parts[1])
	}

	return queryID, nil
}
