package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// LogchefQL tools: query, translate, validate using Logchef's native search syntax.

type QueryLogchefQLParams struct {
	TeamID       int    `json:"team_id" jsonschema:"Team ID"`
	SourceID     int    `json:"source_id" jsonschema:"Source ID"`
	Query        string `json:"query" jsonschema:"LogchefQL filter expression (e.g. severity_text=ERROR and service=api) with an optional aggregation pipe (e.g. severity_text=ERROR | count() by service). Empty string returns all logs."`
	StartTime    string `json:"start_time" jsonschema:"Start time (RFC3339 or YYYY-MM-DD HH:MM:SS)"`
	EndTime      string `json:"end_time" jsonschema:"End time (RFC3339 or YYYY-MM-DD HH:MM:SS)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Max rows to return (1-500 default 100)"`
	Timezone     string `json:"timezone,omitempty" jsonschema:"Timezone (default UTC)"`
	QueryTimeout *int   `json:"query_timeout,omitempty" jsonschema:"Query timeout in seconds (default 60)"`
	Interval     string `json:"interval,omitempty" jsonschema:"Bucket size of a by-time aggregation key with no window of its own: 1s 5s 10s 15s 30s 1m 5m 10m 15m 30m 1h 2h 3h 6h 12h or 24h"`
}

type TranslateLogchefQLParams struct {
	TeamID    int    `json:"team_id" jsonschema:"Team ID"`
	SourceID  int    `json:"source_id" jsonschema:"Source ID"`
	Query     string `json:"query" jsonschema:"LogchefQL filter expression to translate to SQL"`
	StartTime string `json:"start_time" jsonschema:"Start time (RFC3339 or YYYY-MM-DD HH:MM:SS)"`
	EndTime   string `json:"end_time" jsonschema:"End time (RFC3339 or YYYY-MM-DD HH:MM:SS)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Row limit for generated SQL"`
	Timezone  string `json:"timezone,omitempty" jsonschema:"Timezone (default UTC)"`
	Interval  string `json:"interval,omitempty" jsonschema:"Bucket size of a by-time aggregation key with no window of its own: 1s 5s 10s 15s 30s 1m 5m 10m 15m 30m 1h 2h 3h 6h 12h or 24h"`
}

// --- Output schemas ---

type TranslateResult struct {
	SQL                    string                `json:"sql" jsonschema:"ClickHouse filter conditions; not an executable histogram query"`
	FullSQL                string                `json:"full_sql,omitempty"`
	GeneratedQuery         string                `json:"generated_query,omitempty"`
	GeneratedQueryLanguage string                `json:"generated_query_language,omitempty"`
	Error                  *logchefql.ParseError `json:"error,omitempty"`
	Valid                  bool                  `json:"valid" jsonschema:"Whether the LogchefQL expression is valid"`
	// Aggregation is absent for a query that returns log rows.
	Aggregation *logchefql.AggregationInfo `json:"aggregation,omitempty" jsonschema:"The result shape and columns when the query aggregates. full_sql and generated_query are then the aggregated query and sql stays the filter"`
	Warnings    []string                   `json:"warnings,omitempty" jsonschema:"Notes about a query that translates but may not match what it says, such as a JSON number read as text from a String column"`
}

// logchefQLRun is one executed LogchefQL query and its tracked query ID.
type logchefQLRun struct {
	prepared *core.PreparedLogchefQL
	result   *models.QueryResult
	queryID  string
}

// runLogchefQL admits and runs one LogchefQL query on an authorized source.
func (t *tools) runLogchefQL(ctx context.Context, src access.AuthorizedSource, req core.LogchefQLQueryRequest) (logchefQLRun, error) {
	var run logchefQLRun
	err := t.admit(ctx, QueryClassPreview, src, req.Query, func(ctx context.Context, queryID string) error {
		prepared, result, err := core.RunLogchefQL(ctx, t.deps.Datasources, t.deps.Config.Query, src, req)
		run = logchefQLRun{prepared: prepared, result: result, queryID: queryID}
		return err
	})
	return run, err
}

func (t *tools) handleQueryLogchefQL(ctx context.Context, req mcp.CallToolRequest, params QueryLogchefQLParams) (*mcp.CallToolResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	timezone := params.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	src, err := t.authorizeSource(ctx, params.TeamID, params.SourceID, models.TokenScopeLogsRead)
	if err != nil {
		return t.errorResult(t.storeError("logchefql query failed", err)), nil
	}
	read := t.startRead(ctx, req, ReadCall{Source: src, Query: params.Query, Language: models.QueryLanguageLogchefQL, Limit: limit})
	defer func() { read.end(recover()) }()
	read.From, read.To = logchefQLWindow(params.StartTime, params.EndTime, timezone)
	run, err := t.runLogchefQL(ctx, src, core.LogchefQLQueryRequest{
		Query:        params.Query,
		Limit:        limit,
		StartTime:    params.StartTime,
		EndTime:      params.EndTime,
		Timezone:     timezone,
		QueryTimeout: params.QueryTimeout,
		Interval:     params.Interval,
		// The tool returns whatever the query computes, so it runs an
		// aggregation as well as a filter.
		AllowAggregation: true,
	})
	if err != nil {
		read.Err = err
		return t.errorResult(t.queryError("logchefql query failed", err)), nil
	}
	read.Rows = int64(len(run.result.Logs))

	compiled := run.prepared.Compiled
	return mcp.NewToolResultStructuredOnly(QueryResult{
		Logs: run.result.Logs, Columns: core.ResultColumns(run.prepared.Source, run.result), Stats: run.result.Stats, QueryID: run.queryID,
		GeneratedSQL: compiled.Query, GeneratedQuery: compiled.Query,
		GeneratedQueryLanguage: string(compiled.Language), Warnings: run.result.Warnings,
		Aggregation: compiled.Aggregation,
		RowCount:    len(run.result.Logs), TeamID: params.TeamID, SourceID: params.SourceID,
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
	// ClickHouse builds full_sql only with a timezone, so an empty one
	// would return the bare filter.
	timezone := params.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	translation, err := core.TranslateLogchefQL(ctx, t.deps.Datasources, src, datasource.LogchefQLCompileRequest{
		Query:     params.Query,
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		Timezone:  timezone,
		Limit:     limit,
		Interval:  params.Interval,
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
		Aggregation:            translation.Aggregation,
		Warnings:               translation.Warnings,
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
		mcp.WithDescription("Execute a LogchefQL query against a log source. LogchefQL is a filter syntax (e.g. 'severity_text=ERROR and service=api') with an optional aggregation pipe that counts and summarizes the matching logs instead of returning them: 'severity_text=ERROR | count() by service' or 'service=api | p95(duration_ms), avg(duration_ms) by time(5m)'. Functions: count, count_distinct, sum, avg, min, max, p50, p75, p90, p95, p99. 'by' takes up to 5 fields and one time bucket; bare 'time' needs the interval parameter. Optional '| sort <column> desc' and '| limit N' follow the aggregation. Time range is specified separately. Returns logs (the result rows when the query aggregates), columns, stats, the generated SQL, and an aggregation object that describes an aggregated result."),
		mcp.WithInputSchema[QueryLogchefQLParams](),
		mcp.WithOutputSchema[QueryResult](),
		mcp.WithTitleAnnotation("Query LogchefQL"),
		readOnlyTool,
	)
	s.AddTool(queryTool, mcp.NewTypedToolHandler(t.handleQueryLogchefQL))

	translateTool := mcp.NewTool("translate_logchefql",
		mcp.WithDescription("Translate LogchefQL to the source native query language without executing it. warnings lists queries that translate but may not match what they say. sql contains ClickHouse filter conditions. For get_log_histogram, translate the filter without an aggregation pipe and pass its full_sql (ClickHouse) or generated_query (VictoriaLogs); the full_sql of an aggregated query is not a histogram query."),
		mcp.WithInputSchema[TranslateLogchefQLParams](),
		mcp.WithOutputSchema[TranslateResult](),
		mcp.WithTitleAnnotation("Translate LogchefQL to SQL"),
		readOnlyTool,
	)
	s.AddTool(translateTool, mcp.NewStructuredToolHandler(t.handleTranslateLogchefQL))

	validateTool := mcp.NewTool("validate_logchefql",
		mcp.WithDescription("Validate a LogchefQL expression for syntax errors without executing it. Returns whether the expression is valid and any error details. Use this before executing queries to catch syntax issues early. It does not read the source's column types: translate_logchefql does, and its warnings name a query that translates but may not match, such as a JSON number read as text from a String column."),
		mcp.WithInputSchema[ValidateLogchefQLParams](),
		mcp.WithOutputSchema[ValidateResult](),
		mcp.WithTitleAnnotation("Validate LogchefQL"),
		readOnlyTool,
	)
	s.AddTool(validateTool, mcp.NewStructuredToolHandler(t.handleValidateLogchefQL))
}
