package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// --- Input schemas ---

type QueryLogsParams struct {
	TeamID       int    `json:"team_id" jsonschema:"The ID of the team that has access to the source"`
	SourceID     int    `json:"source_id" jsonschema:"The ID of the source to query logs from"`
	StartTime    string `json:"start_time,omitempty" jsonschema:"Inclusive start time (RFC3339)"`
	EndTime      string `json:"end_time,omitempty" jsonschema:"End time (RFC3339)"`
	RawSQL       string `json:"raw_sql" jsonschema:"Datasource-native query: ClickHouse SQL or VictoriaLogs LogsQL. Use get_source_schema first to understand available columns. Include WHERE clauses with timestamp filters and ORDER BY and LIMIT clauses."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum number of log entries to return (1-100 default 100)"`
	QueryTimeout *int   `json:"query_timeout,omitempty" jsonschema:"Query timeout in seconds (default 30)"`
}

type GetSourceSchemaParams struct {
	TeamID   int `json:"team_id" jsonschema:"The ID of the team that has access to the source"`
	SourceID int `json:"source_id" jsonschema:"The ID of the source to get the schema for"`
}

type GetLogHistogramParams struct {
	TeamID       int    `json:"team_id" jsonschema:"The ID of the team that has access to the source"`
	SourceID     int    `json:"source_id" jsonschema:"The ID of the source to generate histogram for"`
	StartTime    string `json:"start_time,omitempty" jsonschema:"Inclusive start time (RFC3339)"`
	EndTime      string `json:"end_time,omitempty" jsonschema:"End time (RFC3339)"`
	RawSQL       string `json:"raw_sql" jsonschema:"Executable datasource-native query for histogram generation. Use translate_logchefql first; pass full_sql for ClickHouse or generated_query for VictoriaLogs."`
	Window       string `json:"window,omitempty" jsonschema:"Time window for histogram buckets (e.g. 1m 5m 1h 1d). Defaults to 1m."`
	GroupBy      string `json:"group_by,omitempty" jsonschema:"Optional field to group histogram data by (e.g. severity_text or service_name)"`
	Timezone     string `json:"timezone,omitempty" jsonschema:"Timezone for histogram timestamps (default UTC)"`
	QueryTimeout *int   `json:"query_timeout,omitempty" jsonschema:"Query timeout in seconds (default 30)"`
}

type ListSavedQueriesParams struct {
	SourceID int `json:"source_id,omitempty" jsonschema:"Optional source ID to filter by. Omit to list every saved query visible to the caller."`
}

type GetSavedQueryParams struct {
	QueryID int `json:"query_id" jsonschema:"The ID of the saved query to retrieve"`
}

// --- Output schemas ---

type SchemaColumnResult struct {
	Name string `json:"name" jsonschema:"Column name"`
	Type string `json:"type" jsonschema:"ClickHouse column type"`
}

type SavedQueryResult struct {
	ID                int    `json:"id" jsonschema:"Saved query ID"`
	Name              string `json:"name" jsonschema:"Saved query name"`
	Description       string `json:"description" jsonschema:"Saved query description"`
	SourceID          int    `json:"source_id" jsonschema:"ID of the source the query runs against"`
	CreatedFromTeamID *int   `json:"created_from_team_id,omitempty" jsonschema:"Team the query was originally saved from (resolver preference hint, not an ACL)"`
	QueryType         string `json:"query_type" jsonschema:"Either 'logchefql' or 'sql'"`
	QueryContent      string `json:"query_content" jsonschema:"Query payload (JSON envelope)"`
	CreatedBy         *int   `json:"created_by,omitempty" jsonschema:"Creator user ID; null on legacy queries"`
	SourceName        string `json:"source_name,omitempty" jsonschema:"Human-readable source name (when included)"`
	CreatedAt         string `json:"created_at" jsonschema:"Creation timestamp"`
	UpdatedAt         string `json:"updated_at" jsonschema:"Last update timestamp"`
}

// --- Handlers ---

// query_logs returns flexible log data, so it uses a typed handler.
func (t *tools) handleQueryLogs(ctx context.Context, _ mcp.CallToolRequest, args QueryLogsParams) (*mcp.CallToolResult, error) {
	if args.Limit < 0 {
		args.Limit = 0
	}
	if args.Limit > 100 {
		args.Limit = 100
	}

	src, err := t.authorizeSource(ctx, args.TeamID, args.SourceID, models.TokenScopeLogsRead)
	if err != nil {
		return t.errorResult(t.storeError("query logs", err)), nil
	}
	var result *models.QueryResult
	var queryID string
	err = t.admit(ctx, QueryClassPreview, src, args.RawSQL, func(ctx context.Context, id string) error {
		queryID = id
		var runErr error
		result, runErr = core.RunQuery(ctx, t.deps.Datasources, t.deps.Config.Query, src, models.APIQueryRequest{
			QueryText:    args.RawSQL,
			Limit:        args.Limit,
			QueryTimeout: args.QueryTimeout,
			StartTime:    args.StartTime, EndTime: args.EndTime,
		})
		return runErr
	})
	if err != nil {
		return t.errorResult(t.queryError("query logs", err)), nil
	}

	return mcp.NewToolResultStructuredOnly(QueryResult{
		Logs: result.Logs, Columns: core.ResultColumns(nil, result), Stats: result.Stats, QueryID: queryID,
		RowCount: len(result.Logs), TeamID: args.TeamID, SourceID: args.SourceID,
		StartTime: args.StartTime, EndTime: args.EndTime, Warnings: result.Warnings,
	}), nil
}

func (t *tools) handleGetSourceSchema(ctx context.Context, _ mcp.CallToolRequest, args GetSourceSchemaParams) ([]SchemaColumnResult, error) {
	schema, err := t.sourceSchema(ctx, args.TeamID, args.SourceID)
	if err != nil {
		return nil, err
	}

	result := make([]SchemaColumnResult, len(schema))
	for i, col := range schema {
		result[i] = SchemaColumnResult{Name: col.Name, Type: col.Type}
	}
	return result, nil
}

func (t *tools) sourceSchema(ctx context.Context, teamID, sourceID int) ([]models.ColumnInfo, error) {
	src, err := t.authorizeSource(ctx, teamID, sourceID, models.TokenScopeSourcesRead)
	if err != nil {
		return nil, t.storeError("get source schema", err)
	}
	ctx, cancel := context.WithTimeout(ctx, core.SchemaTimeout)
	defer cancel()
	schema, err := core.GetSourceSchema(ctx, t.deps.Datasources, src)
	if err != nil {
		return nil, t.queryError("get source schema", err)
	}
	return schema, nil
}

// get_log_histogram returns flexible histogram data, so it uses a typed handler.
func (t *tools) handleGetLogHistogram(ctx context.Context, _ mcp.CallToolRequest, args GetLogHistogramParams) (*mcp.CallToolResult, error) {
	src, err := t.authorizeSource(ctx, args.TeamID, args.SourceID, models.TokenScopeLogsRead)
	if err != nil {
		return t.errorResult(t.storeError("get log histogram", err)), nil
	}
	var histogram *core.HistogramResponse
	err = t.admit(ctx, QueryClassHistogram, src, args.RawSQL, func(ctx context.Context, _ string) error {
		var runErr error
		histogram, runErr = core.RunHistogram(ctx, t.deps.Datasources, src, models.APIHistogramRequest{
			QueryText:    args.RawSQL,
			Window:       args.Window,
			GroupBy:      args.GroupBy,
			Timezone:     args.Timezone,
			QueryTimeout: args.QueryTimeout,
			StartTime:    args.StartTime, EndTime: args.EndTime,
		})
		return runErr
	})
	if err != nil {
		return t.errorResult(t.queryError("get log histogram", err)), nil
	}

	return mcp.NewToolResultStructuredOnly(HistogramResult{Granularity: histogram.Granularity, Data: histogram.Data, Notice: histogram.Notice}), nil
}

func (t *tools) handleListSavedQueries(ctx context.Context, _ mcp.CallToolRequest, args ListSavedQueriesParams) ([]SavedQueryResult, error) {
	queries, err := t.listSavedQueries(ctx, args.SourceID)
	if err != nil {
		return nil, t.storeError("list saved queries", err)
	}

	result := make([]SavedQueryResult, len(queries))
	for i, q := range queries {
		result[i] = savedQueryToResult(q)
	}
	return result, nil
}

// listSavedQueries returns the saved queries the principal can see. The
// store query joins the user's teams, so a source the user cannot reach
// yields no rows.
func (t *tools) listSavedQueries(ctx context.Context, sourceID int) ([]*models.SavedQuery, error) {
	p, err := principalFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.Require(models.TokenScopeSavedQueriesRead); err != nil {
		return nil, err
	}
	if sourceID != 0 {
		return core.ListSavedQueriesForUserBySource(ctx, t.deps.DB, t.deps.Log, p.User.ID, models.SourceID(sourceID))
	}
	return core.ListSavedQueriesForUser(ctx, t.deps.DB, t.deps.Log, p.User.ID)
}

func (t *tools) handleGetSavedQuery(ctx context.Context, _ mcp.CallToolRequest, args GetSavedQueryParams) (SavedQueryResult, error) {
	got, err := t.getSavedQuery(ctx, args.QueryID)
	if err != nil {
		return SavedQueryResult{}, t.storeError("get saved query", err)
	}
	return savedQueryToResult(got), nil
}

func (t *tools) getSavedQuery(ctx context.Context, queryID int) (*models.SavedQuery, error) {
	p, err := principalFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.Require(models.TokenScopeSavedQueriesRead); err != nil {
		return nil, err
	}
	return core.GetSavedQueryForPrincipal(ctx, t.deps.DB, t.deps.Log, p, queryID)
}

// legacyQueryType is the query_type value of the removed column, derived the
// same way as migration 000027's down path.
func legacyQueryType(language models.QueryLanguage) string {
	if language == models.QueryLanguageLogchefQL {
		return "logchefql"
	}
	return "sql"
}

func savedQueryToResult(q *models.SavedQuery) SavedQueryResult {
	result := SavedQueryResult{
		ID:           q.ID,
		Name:         q.Name,
		Description:  q.Description,
		SourceID:     int(q.SourceID),
		QueryType:    legacyQueryType(q.QueryLanguage),
		QueryContent: q.QueryContent,
		SourceName:   q.SourceName,
		CreatedAt:    formatTime(q.CreatedAt),
		UpdatedAt:    formatTime(q.UpdatedAt),
	}
	if q.CreatedFromTeamID != nil {
		teamID := int(*q.CreatedFromTeamID)
		result.CreatedFromTeamID = &teamID
	}
	if q.CreatedBy != nil {
		createdBy := int(*q.CreatedBy)
		result.CreatedBy = &createdBy
	}
	return result
}

func (t *tools) addLogsTools(s *server.MCPServer) {
	queryLogsTool := mcp.NewTool("query_logs",
		mcp.WithDescription("Execute a datasource-native query against a specific log source within a team. Use get_source_schema first to understand available columns. The query should include proper WHERE clauses with timestamp filters, ORDER BY, and LIMIT. Maximum 100 results per query."),
		mcp.WithInputSchema[QueryLogsParams](),
		mcp.WithOutputSchema[QueryResult](),
		mcp.WithTitleAnnotation("Query Logs (SQL)"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(queryLogsTool, mcp.NewTypedToolHandler(t.handleQueryLogs))

	schemaTool := mcp.NewTool("get_source_schema",
		mcp.WithDescription("Get the source schema (column names and types) for a specific log source within a team. Use this before querying logs to understand what fields are available."),
		mcp.WithInputSchema[GetSourceSchemaParams](),
		mcp.WithOutputSchema[[]SchemaColumnResult](),
		mcp.WithTitleAnnotation("Get Source Schema"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(schemaTool, mcp.NewStructuredToolHandler(t.handleGetSourceSchema))

	// get_log_histogram returns flexible histogram data, so it uses a typed handler.
	histogramTool := mcp.NewTool("get_log_histogram",
		mcp.WithDescription("Generate time-based histogram data for log analysis. Creates a time series showing log volume over specified time windows, with optional grouping by fields like severity or service. Useful for identifying traffic patterns, spikes, and trends."),
		mcp.WithInputSchema[GetLogHistogramParams](),
		mcp.WithOutputSchema[HistogramResult](),
		mcp.WithTitleAnnotation("Get Log Histogram"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(histogramTool, mcp.NewTypedToolHandler(t.handleGetLogHistogram))

	listSavedQueriesTool := mcp.NewTool("list_saved_queries",
		mcp.WithDescription("List saved queries visible to the caller. Optionally filter by source_id. Saved queries are reusable LogchefQL or SQL queries pinned to a specific source; any user with source access via any team can see them."),
		mcp.WithInputSchema[ListSavedQueriesParams](),
		mcp.WithOutputSchema[[]SavedQueryResult](),
		mcp.WithTitleAnnotation("List Saved Queries"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(listSavedQueriesTool, mcp.NewStructuredToolHandler(t.handleListSavedQueries))

	getSavedQueryTool := mcp.NewTool("get_saved_query",
		mcp.WithDescription("Get a single saved query by ID. Returns name, description, query_type, query_content, source, and metadata."),
		mcp.WithInputSchema[GetSavedQueryParams](),
		mcp.WithOutputSchema[SavedQueryResult](),
		mcp.WithTitleAnnotation("Get Saved Query"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(getSavedQueryTool, mcp.NewStructuredToolHandler(t.handleGetSavedQuery))
}
