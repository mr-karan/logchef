package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// LogchefQL tools: query, translate, validate using Logchef's native search syntax.

type QueryLogchefQLParams struct {
	TeamID       int    `json:"team_id" jsonschema:"Team ID"`
	SourceID     int    `json:"source_id" jsonschema:"Source ID"`
	Query        string `json:"query" jsonschema:"LogchefQL filter expression (e.g. severity_text=ERROR and service=api). Empty string returns all logs."`
	StartTime    string `json:"start_time" jsonschema:"Start time (RFC3339 or YYYY-MM-DD HH:MM:SS)"`
	EndTime      string `json:"end_time" jsonschema:"End time (RFC3339 or YYYY-MM-DD HH:MM:SS)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Max rows to return (1-500 default 100)"`
	Timezone     string `json:"timezone,omitempty" jsonschema:"Timezone (default UTC)"`
	QueryTimeout *int   `json:"query_timeout,omitempty" jsonschema:"Query timeout in seconds (default 60)"`
}

type TranslateLogchefQLParams struct {
	TeamID    int    `json:"team_id" jsonschema:"Team ID"`
	SourceID  int    `json:"source_id" jsonschema:"Source ID"`
	Query     string `json:"query" jsonschema:"LogchefQL filter expression to translate to SQL"`
	StartTime string `json:"start_time" jsonschema:"Start time (RFC3339 or YYYY-MM-DD HH:MM:SS)"`
	EndTime   string `json:"end_time" jsonschema:"End time (RFC3339 or YYYY-MM-DD HH:MM:SS)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Row limit for generated SQL"`
	Timezone  string `json:"timezone,omitempty" jsonschema:"Timezone (default UTC)"`
}

// --- Output schemas ---

type TranslateResult struct {
	SQL                    string                `json:"sql" jsonschema:"ClickHouse filter conditions; not an executable histogram query"`
	FullSQL                string                `json:"full_sql,omitempty"`
	GeneratedQuery         string                `json:"generated_query,omitempty"`
	GeneratedQueryLanguage string                `json:"generated_query_language,omitempty"`
	Error                  *logchefql.ParseError `json:"error,omitempty"`
	Valid                  bool                  `json:"valid" jsonschema:"Whether the LogchefQL expression is valid"`
}

// logchefQLRun is one executed LogchefQL query and its tracked query ID.
type logchefQLRun struct {
	prepared *core.PreparedLogchefQL
	result   *models.QueryResult
	queryID  string
}

// runLogchefQL authorizes, admits and runs one LogchefQL query.
func (t *tools) runLogchefQL(ctx context.Context, teamID, sourceID int, req core.LogchefQLQueryRequest) (logchefQLRun, error) {
	src, err := t.authorizeSource(ctx, teamID, sourceID, models.TokenScopeLogsRead)
	if err != nil {
		return logchefQLRun{}, t.storeError("logchefql query failed", err)
	}
	var run logchefQLRun
	err = t.admit(ctx, QueryClassPreview, src, req.Query, func(ctx context.Context, queryID string) error {
		prepared, result, err := core.RunLogchefQL(ctx, t.deps.Datasources, t.deps.Config.Query, src, req)
		run = logchefQLRun{prepared: prepared, result: result, queryID: queryID}
		return err
	})
	if err != nil {
		return logchefQLRun{}, t.queryError("logchefql query failed", err)
	}
	return run, nil
}

func (t *tools) handleQueryLogchefQL(ctx context.Context, _ mcp.CallToolRequest, params QueryLogchefQLParams) (*mcp.CallToolResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	run, err := t.runLogchefQL(ctx, params.TeamID, params.SourceID, core.LogchefQLQueryRequest{
		Query:        params.Query,
		Limit:        limit,
		StartTime:    params.StartTime,
		EndTime:      params.EndTime,
		Timezone:     params.Timezone,
		QueryTimeout: params.QueryTimeout,
	})
	if err != nil {
		return t.errorResult(err), nil
	}

	compiled := run.prepared.Compiled
	return mcp.NewToolResultStructuredOnly(QueryResult{
		Logs: run.result.Logs, Columns: core.ResultColumns(run.prepared.Source, run.result), Stats: run.result.Stats, QueryID: run.queryID,
		GeneratedSQL: compiled.Query, GeneratedQuery: compiled.Query,
		GeneratedQueryLanguage: string(compiled.Language), Warnings: run.result.Warnings,
		RowCount: len(run.result.Logs), TeamID: params.TeamID, SourceID: params.SourceID,
		StartTime: params.StartTime, EndTime: params.EndTime,
	}), nil
}

func (t *tools) handleTranslateLogchefQL(ctx context.Context, _ mcp.CallToolRequest, params TranslateLogchefQLParams) (TranslateResult, error) {
	src, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeLogsRead)
	if err != nil {
		return TranslateResult{}, t.storeError("logchefql translate failed", err)
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}

	translation, err := core.TranslateLogchefQL(ctx, t.deps.Datasources, src, datasource.LogchefQLCompileRequest{
		Query:     params.Query,
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		Timezone:  params.Timezone,
		Limit:     limit,
	})
	if err != nil {
		return TranslateResult{}, t.queryError("logchefql translate failed", err)
	}

	return TranslateResult{
		SQL:                    translation.SQL,
		FullSQL:                translation.FullSQL,
		GeneratedQuery:         translation.GeneratedQuery,
		GeneratedQueryLanguage: string(translation.Language),
		Error:                  translation.Error,
		Valid:                  translation.Valid,
	}, nil
}

type ValidateLogchefQLParams struct {
	TeamID   int    `json:"team_id" jsonschema:"Team ID"`
	SourceID int    `json:"source_id" jsonschema:"Source ID"`
	Query    string `json:"query" jsonschema:"LogchefQL expression to validate"`
}

type ValidateResult struct {
	Valid bool                  `json:"valid" jsonschema:"Whether the LogchefQL expression is syntactically valid"`
	Error *logchefql.ParseError `json:"error,omitempty" jsonschema:"Structured syntax error if invalid"`
}

// handleValidateLogchefQL authorizes like the HTTP route, although the check
// itself reads no data, so both entrypoints give the same verdicts.
func (t *tools) handleValidateLogchefQL(ctx context.Context, _ mcp.CallToolRequest, params ValidateLogchefQLParams) (ValidateResult, error) {
	if _, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeLogsRead); err != nil {
		return ValidateResult{}, t.storeError("logchefql validate failed", err)
	}
	result := logchefql.Validate(params.Query)
	return ValidateResult{
		Valid: result.Valid,
		Error: result.Error,
	}, nil
}

func (t *tools) addLogchefQLTools(s *server.MCPServer) {
	queryTool := mcp.NewTool("query_logchefql",
		mcp.WithDescription("Execute a LogchefQL query against a log source. LogchefQL is a simple filter syntax (e.g. 'severity_text=ERROR and service=api'). Time range is specified separately. Returns logs, columns, stats, and the generated SQL."),
		mcp.WithInputSchema[QueryLogchefQLParams](),
		mcp.WithOutputSchema[QueryResult](),
		mcp.WithTitleAnnotation("Query LogchefQL"),
		readOnlyTool,
	)
	s.AddTool(queryTool, mcp.NewTypedToolHandler(t.handleQueryLogchefQL))

	translateTool := mcp.NewTool("translate_logchefql",
		mcp.WithDescription("Translate LogchefQL to the source native query language without executing it. sql contains ClickHouse filter conditions; use full_sql or generated_query for histogram queries."),
		mcp.WithInputSchema[TranslateLogchefQLParams](),
		mcp.WithOutputSchema[TranslateResult](),
		mcp.WithTitleAnnotation("Translate LogchefQL to SQL"),
		readOnlyTool,
	)
	s.AddTool(translateTool, mcp.NewStructuredToolHandler(t.handleTranslateLogchefQL))

	validateTool := mcp.NewTool("validate_logchefql",
		mcp.WithDescription("Validate a LogchefQL expression for syntax errors without executing it. Returns whether the expression is valid and any error details. Use this before executing queries to catch syntax issues early."),
		mcp.WithInputSchema[ValidateLogchefQLParams](),
		mcp.WithOutputSchema[ValidateResult](),
		mcp.WithTitleAnnotation("Validate LogchefQL"),
		readOnlyTool,
	)
	s.AddTool(validateTool, mcp.NewStructuredToolHandler(t.handleValidateLogchefQL))
}
