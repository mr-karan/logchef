package core

// Query preparation shared by every entrypoint that runs a log query. HTTP and
// MCP both prepare through these functions, so limits, timeouts and template
// rules cannot drift between them.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

// HistogramTimeout is the maximum time to wait for a histogram query against
// the configured datasource (ClickHouse or VictoriaLogs) before aborting.
// Bounds both slow ClickHouse queries and VictoriaLogs response bodies that
// may otherwise be read without a deadline.
const HistogramTimeout = 30 * time.Second

// SchemaTimeout is the maximum time to wait for a source schema inspection
// query against the configured datasource before aborting.
const SchemaTimeout = 20 * time.Second

// FieldValuesTimeout is the maximum time to wait for field values queries.
// It propagates to ClickHouse as max_execution_time via the context deadline.
const FieldValuesTimeout = 15 * time.Second

// ErrLogchefQLNotSupported means the source cannot run LogchefQL.
var ErrLogchefQLNotSupported = errors.New("LogchefQL is not supported for this source")

// QueryRequestError is a fault in the caller's query request. Its message is
// safe to return to the caller.
type QueryRequestError struct {
	Message string
}

func (e *QueryRequestError) Error() string { return e.Message }

func queryRequestErrorf(format string, args ...any) error {
	return &QueryRequestError{Message: fmt.Sprintf(format, args...)}
}

// LogchefQLCompileError means the datasource failed to compile a LogchefQL
// query for a reason other than the query itself.
type LogchefQLCompileError struct {
	Err error
}

func (e *LogchefQLCompileError) Error() string { return fmt.Sprintf("compiling LogchefQL: %v", e.Err) }
func (e *LogchefQLCompileError) Unwrap() error { return e.Err }

// PrepareSQLQuery applies the configured timeout and limit policy and template
// substitution to a raw query request. The returned QueryTimeout is never nil.
func PrepareSQLQuery(cfg config.QueryConfig, req models.APIQueryRequest) (datasource.QueryRequest, error) {
	timeout, err := resolveQueryTimeout(cfg, req.QueryTimeout)
	if err != nil {
		return datasource.QueryRequest{}, err
	}

	requiredVars := template.ExtractVariableNames(req.QueryText)
	if len(requiredVars) > 0 && len(req.Variables) == 0 {
		return datasource.QueryRequest{}, queryRequestErrorf("Query contains template variables (%s) but no variables were provided. Please define variable values before executing.", strings.Join(requiredVars, ", "))
	}
	query := req.QueryText
	if len(req.Variables) > 0 {
		query, err = template.SubstituteVariables(req.QueryText, templateVariables(req.Variables))
		if err != nil {
			return datasource.QueryRequest{}, queryRequestErrorf("Variable substitution failed: %v", err)
		}
	}

	params := datasource.QueryRequest{
		RawQuery:         query,
		Timezone:         req.Timezone,
		Limit:            req.Limit,
		DefaultLimit:     cfg.DefaultPreviewLimit,
		MaxLimit:         cfg.MaxPreviewLimit,
		MaxResponseBytes: cfg.MaxResponseBytes,
		QueryTimeout:     timeout,
	}
	if req.StartTime != "" || req.EndTime != "" {
		startTime, endTime, err := parseRFC3339TimeRange(req.StartTime, req.EndTime)
		if err != nil {
			return datasource.QueryRequest{}, &QueryRequestError{Message: err.Error()}
		}
		params.StartTime = startTime
		params.EndTime = endTime
	}
	return params, nil
}

// RunQuery prepares and runs a raw query with no streaming. One deadline,
// the query timeout, covers preparation and execution, because callers such
// as MCP have no client disconnect to cancel the call.
func RunQuery(ctx context.Context, ds *datasource.Service, cfg config.QueryConfig, src access.AuthorizedSource, req models.APIQueryRequest) (*models.QueryResult, error) {
	ctx, cancel, err := withQueryDeadline(ctx, cfg, req.QueryTimeout)
	if err != nil {
		return nil, err
	}
	defer cancel()
	params, err := PrepareSQLQuery(cfg, req)
	if err != nil {
		return nil, err
	}
	return QueryLogs(ctx, ds, src, params)
}

// LogchefQLQueryRequest is a LogchefQL query to run against one source.
type LogchefQLQueryRequest struct {
	Query        string                    `json:"query"`
	StartTime    string                    `json:"start_time"`    // Accepts "2006-01-02 15:04:05" and ISO8601/RFC3339
	EndTime      string                    `json:"end_time"`      // Accepts "2006-01-02 15:04:05" and ISO8601/RFC3339
	Timezone     string                    `json:"timezone"`      // Timezone for time conversion
	Limit        int                       `json:"limit"`         // Result limit
	QueryTimeout *int                      `json:"query_timeout"` // Optional timeout in seconds
	Variables    []models.TemplateVariable `json:"variables,omitempty"`
}

// PreparedLogchefQL is a LogchefQL query compiled into the source's native
// language, with the datasource request that runs it.
type PreparedLogchefQL struct {
	Source   *models.Source
	Compiled *datasource.CompiledLogchefQL
	// Params.RawQuery is the compiled query. Params.QueryTimeout is never nil.
	Params datasource.QueryRequest
}

// PrepareLogchefQLQuery validates req, applies the configured limit and
// timeout policy, substitutes variables and compiles the query.
func PrepareLogchefQLQuery(ctx context.Context, ds *datasource.Service, cfg config.QueryConfig, src access.AuthorizedSource, req LogchefQLQueryRequest) (*PreparedLogchefQL, error) {
	if req.StartTime == "" || req.EndTime == "" {
		return nil, &QueryRequestError{Message: "start_time and end_time are required"}
	}
	if req.Limit <= 0 {
		req.Limit = cfg.DefaultPreviewLimit
	}
	if req.Limit > cfg.MaxPreviewLimit {
		req.Limit = cfg.MaxPreviewLimit
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	timeout, err := resolveQueryTimeout(cfg, req.QueryTimeout)
	if err != nil {
		return nil, err
	}

	source, err := logchefQLSource(ctx, ds, src)
	if err != nil {
		return nil, err
	}

	query, err := template.SubstituteLogchefQLVariables(req.Query, templateVariables(req.Variables))
	if err != nil {
		return nil, &QueryRequestError{Message: "Variable substitution failed: " + err.Error()}
	}

	// ClickHouse bakes the time range into the SQL. For VictoriaLogs the
	// LogsQL query carries no time range, so the parsed window goes to
	// QueryLogs separately.
	compiled, compileErr := ds.CompileLogchefQL(ctx, src.SourceID(), datasource.LogchefQLCompileRequest{
		Query:     query,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		Timezone:  req.Timezone,
		Limit:     req.Limit,
	})
	if compiled == nil {
		if errors.Is(compileErr, datasource.ErrOperationNotSupported) {
			return nil, ErrLogchefQLNotSupported
		}
		return nil, &LogchefQLCompileError{Err: compileErr}
	}
	if compileErr != nil {
		message := compileErr.Error()
		if compiled.Error != nil {
			message = compiled.Error.Error()
		}
		return nil, &QueryRequestError{Message: message}
	}
	if !compiled.Valid {
		message := "invalid LogchefQL query"
		if compiled.Error != nil {
			message = compiled.Error.Error()
		}
		return nil, &QueryRequestError{Message: message}
	}

	params := datasource.QueryRequest{
		RawQuery:         compiled.Query,
		Timezone:         req.Timezone,
		Limit:            req.Limit,
		DefaultLimit:     cfg.DefaultPreviewLimit,
		MaxLimit:         cfg.MaxPreviewLimit,
		MaxResponseBytes: cfg.MaxResponseBytes,
		QueryTimeout:     timeout,
	}
	if compiled.Language == models.QueryLanguageLogsQL {
		startTime, endTime, err := parseLogchefQLTimeRange(req.StartTime, req.EndTime, req.Timezone)
		if err != nil {
			return nil, &QueryRequestError{Message: err.Error()}
		}
		params.StartTime = startTime
		params.EndTime = endTime
	}
	return &PreparedLogchefQL{Source: source, Compiled: compiled, Params: params}, nil
}

// RunLogchefQL prepares and runs a LogchefQL query with no streaming. One
// deadline, the query timeout, covers preparation (source load, schema fetch,
// compile) and execution.
func RunLogchefQL(ctx context.Context, ds *datasource.Service, cfg config.QueryConfig, src access.AuthorizedSource, req LogchefQLQueryRequest) (*PreparedLogchefQL, *models.QueryResult, error) {
	ctx, cancel, err := withQueryDeadline(ctx, cfg, req.QueryTimeout)
	if err != nil {
		return nil, nil, err
	}
	defer cancel()
	prepared, err := PrepareLogchefQLQuery(ctx, ds, cfg, src, req)
	if err != nil {
		return nil, nil, err
	}
	result, err := QueryLogs(ctx, ds, src, prepared.Params)
	if err != nil {
		return nil, nil, err
	}
	return prepared, result, nil
}

// LogchefQLTranslation is a LogchefQL query compiled for display, in the
// shape the translate endpoints return.
type LogchefQLTranslation struct {
	// SQL is the ClickHouse WHERE-clause-only SQL. Empty for other languages.
	SQL string
	// FullSQL is the complete ClickHouse query with the time range. It is set
	// only when the request had a start, end and timezone.
	FullSQL string
	// GeneratedQuery is FullSQL when set, else SQL for ClickHouse, and the
	// native query for other languages.
	GeneratedQuery string
	Language       models.QueryLanguage
	Valid          bool
	Error          *logchefql.ParseError
	Conditions     []logchefql.FilterCondition
	FieldsUsed     []string
}

// TranslateLogchefQL compiles a LogchefQL query without running it. A query
// that does not parse is not an error: the result has Valid=false and Error
// set, so editors can show it.
func TranslateLogchefQL(ctx context.Context, ds *datasource.Service, src access.AuthorizedSource, req datasource.LogchefQLCompileRequest) (*LogchefQLTranslation, error) {
	if _, err := logchefQLSource(ctx, ds, src); err != nil {
		return nil, err
	}

	compiled, compileErr := ds.CompileLogchefQL(ctx, src.SourceID(), req)
	if compiled == nil {
		if errors.Is(compileErr, datasource.ErrOperationNotSupported) {
			return nil, ErrLogchefQLNotSupported
		}
		return nil, &LogchefQLCompileError{Err: compileErr}
	}

	// A query that parses (compiled.Valid) but fails once the time range is
	// added means the start, end or timezone was rejected.
	hasTimeParams := req.StartTime != "" && req.EndTime != "" && req.Timezone != ""
	if compiled.Valid && hasTimeParams && compileErr != nil {
		message := compileErr.Error()
		if compiled.Error != nil {
			message = compiled.Error.Error()
		}
		return nil, &QueryRequestError{Message: message}
	}

	translation := &LogchefQLTranslation{
		GeneratedQuery: compiled.Query,
		Language:       compiled.Language,
		Valid:          compiled.Valid,
		Error:          compiled.Error,
		Conditions:     compiled.Conditions,
		FieldsUsed:     compiled.FieldsUsed,
	}
	if compiled.Language == models.QueryLanguageClickHouseSQL {
		translation.SQL = compiled.FilterOnly
		translation.GeneratedQuery = compiled.FilterOnly
		if compiled.Valid && hasTimeParams && compileErr == nil {
			translation.FullSQL = compiled.Query
			translation.GeneratedQuery = compiled.Query
		}
	}
	return translation, nil
}

// PrepareHistogram validates a histogram request, substitutes template
// variables and applies the window, timezone and timeout defaults.
func PrepareHistogram(req models.APIHistogramRequest) (HistogramParams, error) {
	if strings.TrimSpace(req.QueryText) == "" {
		return HistogramParams{}, &QueryRequestError{Message: "query_text parameter is required"}
	}

	requiredVars := template.ExtractVariableNames(req.QueryText)
	if len(requiredVars) > 0 && len(req.Variables) == 0 {
		return HistogramParams{}, queryRequestErrorf("Query contains template variables (%s) but no variables were provided. Please define variable values before executing.", strings.Join(requiredVars, ", "))
	}
	query := req.QueryText
	if len(req.Variables) > 0 {
		substituted, err := template.SubstituteVariables(req.QueryText, templateVariables(req.Variables))
		if err != nil {
			return HistogramParams{}, queryRequestErrorf("Variable substitution failed: %v", err)
		}
		query = substituted
	}

	window := req.Window
	if window == "" {
		window = "1m"
	}
	params := HistogramParams{
		Window:   window,
		Query:    query,
		Timezone: req.Timezone,
	}

	startTime, endTime, err := parseHistogramTimeRange(req)
	if err != nil {
		return HistogramParams{}, &QueryRequestError{Message: err.Error()}
	}
	params.StartTime = startTime
	params.EndTime = endTime
	if startTime != nil && endTime.Before(*startTime) {
		return HistogramParams{}, &QueryRequestError{Message: "start_time must not be after end_time"}
	}

	if strings.TrimSpace(req.GroupBy) != "" {
		params.GroupBy = req.GroupBy
	}
	if params.Timezone == "" {
		params.Timezone = "UTC"
	}

	timeout := req.QueryTimeout
	if timeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		timeout = &defaultTimeout
	}
	if err := models.ValidateQueryTimeout(timeout); err != nil {
		return HistogramParams{}, &QueryRequestError{Message: err.Error()}
	}
	params.QueryTimeout = timeout
	return params, nil
}

// RunHistogram prepares and runs a histogram. HistogramTimeout covers
// preparation and execution.
func RunHistogram(ctx context.Context, ds *datasource.Service, src access.AuthorizedSource, req models.APIHistogramRequest) (*HistogramResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, HistogramTimeout)
	defer cancel()
	params, err := PrepareHistogram(req)
	if err != nil {
		return nil, err
	}
	return GetHistogramData(ctx, ds, src, params)
}

// withQueryDeadline validates the requested timeout and returns a context
// that ends when it expires.
func withQueryDeadline(ctx context.Context, cfg config.QueryConfig, requested *int) (context.Context, context.CancelFunc, error) {
	timeout, err := resolveQueryTimeout(cfg, requested)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(*timeout)*time.Second)
	return ctx, cancel, nil
}

// resolveQueryTimeout applies the default preview timeout and rejects a
// timeout outside the allowed range.
func resolveQueryTimeout(cfg config.QueryConfig, requested *int) (*int, error) {
	timeout := requested
	if timeout == nil {
		defaultTimeout := cfg.DefaultTimeoutSeconds
		timeout = &defaultTimeout
	}
	if err := models.ValidateQueryTimeout(timeout); err != nil {
		return nil, &QueryRequestError{Message: err.Error()}
	}
	if cfg.MaxTimeoutSeconds > 0 && *timeout > cfg.MaxTimeoutSeconds {
		return nil, queryRequestErrorf("Query timeout cannot exceed %d seconds for Run", cfg.MaxTimeoutSeconds)
	}
	return timeout, nil
}

// logchefQLSource loads the source and confirms it accepts LogchefQL.
func logchefQLSource(ctx context.Context, ds *datasource.Service, src access.AuthorizedSource) (*models.Source, error) {
	source, err := GetSource(ctx, ds, src.SourceID())
	if err != nil {
		return nil, err
	}
	if !source.SupportsQueryLanguage(models.QueryLanguageLogchefQL) {
		return nil, ErrLogchefQLNotSupported
	}
	return source, nil
}

func templateVariables(vars []models.TemplateVariable) []template.Variable {
	converted := make([]template.Variable, len(vars))
	for i, v := range vars {
		converted[i] = template.Variable{
			Name:  v.Name,
			Type:  template.VariableType(v.Type),
			Value: v.Value,
		}
	}
	return converted
}

func parseRFC3339TimeRange(startTimeRaw, endTimeRaw string) (startPtr, endPtr *time.Time, err error) {
	startTimeRaw = strings.TrimSpace(startTimeRaw)
	endTimeRaw = strings.TrimSpace(endTimeRaw)

	if startTimeRaw == "" && endTimeRaw == "" {
		return nil, nil, nil
	}
	if startTimeRaw == "" || endTimeRaw == "" {
		return nil, nil, fmt.Errorf("start_time and end_time must both be provided")
	}

	startTime, err := time.Parse(time.RFC3339, startTimeRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid start_time format (use ISO8601/RFC3339)")
	}
	endTime, err := time.Parse(time.RFC3339, endTimeRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid end_time format (use ISO8601/RFC3339)")
	}
	return &startTime, &endTime, nil
}

func parseHistogramTimeRange(req models.APIHistogramRequest) (startPtr, endPtr *time.Time, err error) {
	if strings.TrimSpace(req.StartTime) != "" || strings.TrimSpace(req.EndTime) != "" {
		return parseRFC3339TimeRange(req.StartTime, req.EndTime)
	}

	if req.StartTimestamp == 0 && req.EndTimestamp == 0 {
		return nil, nil, nil
	}
	if req.StartTimestamp == 0 || req.EndTimestamp == 0 {
		return nil, nil, fmt.Errorf("start_timestamp and end_timestamp must both be provided")
	}

	startTime := time.UnixMilli(req.StartTimestamp)
	endTime := time.UnixMilli(req.EndTimestamp)
	return &startTime, &endTime, nil
}

func parseLogchefQLTimeRange(startTime, endTime, timezone string) (startPtr, endPtr *time.Time, err error) {
	locationName := timezone
	if locationName == "" {
		locationName = "UTC"
	}

	loc, err := time.LoadLocation(locationName)
	if err != nil {
		return nil, nil, err
	}

	start, err := parseLogchefQLTimeValue(startTime, loc)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid start_time: %w", err)
	}
	end, err := parseLogchefQLTimeValue(endTime, loc)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid end_time: %w", err)
	}

	return &start, &end, nil
}

func parseLogchefQLTimeValue(value string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		var (
			parsed time.Time
			err    error
		)

		switch layout {
		case time.RFC3339Nano, time.RFC3339:
			parsed, err = time.Parse(layout, value)
		default:
			parsed, err = time.ParseInLocation(layout, value, loc)
		}
		if err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
}
