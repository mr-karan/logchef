package loki

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// metadataMaxBytes bounds the body of a label, field or status call.
	metadataMaxBytes = 16 << 20
	// defaultMaxResponseBytes bounds a query when the request sets no budget.
	defaultMaxResponseBytes = 64 << 20
	// maxErrorBodyBytes and maxErrorMessageChars bound what Logchef reads of
	// a Loki error body and how much of it a user sees.
	maxErrorBodyBytes    = 4096
	maxErrorMessageChars = 500
	statsLookback        = 24 * time.Hour
	detectedFieldsLimit  = 1000
)

type lokiParams = url.Values

// errBodyTooLarge is returned by get when a successful body exceeds its
// budget. Query paging turns it into a byte truncation.
var errBodyTooLarge = errors.New("loki response exceeds the size limit")

// get sends a GET to a fixed Loki path with the source's headers and reads at
// most maxBytes of a successful body. A failed status becomes a typed error:
// a ValidationError with Loki's message for a request Loki rejected, an
// UpstreamError otherwise. A cancelled or expired context is returned as is.
func (p *Provider) get(ctx context.Context, conn models.LokiConnectionInfo, path string, params lokiParams, maxBytes int64) ([]byte, error) {
	endpoint, err := joinBaseURL(conn.BaseURL, path)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create loki request: %w", err)
	}
	applyHeaders(req, conn)

	resp, err := p.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("loki %s: %w", path, ctxErr)
		}
		return nil, &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Message: "Loki could not be reached", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, statusError(resp.StatusCode, redactSecrets(string(body), conn), resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("loki %s: %w", path, ctxErr)
		}
		return nil, &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Status: resp.StatusCode, Message: "Loki's response could not be read", Err: err}
	}
	if int64(len(body)) > maxBytes {
		return nil, errBodyTooLarge
	}
	return body, nil
}

// statusError maps a Loki status to the error the routes answer with.
func statusError(status int, body, contentType string) error {
	message := errorMessage(body, contentType)
	detail := fmt.Errorf("loki returned status %d: %s", status, message)
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		if message == "" {
			message = fmt.Sprintf("Loki rejected the request with status %d", status)
		}
		if strings.Contains(message, "max_entries_limit_per_query") {
			message += ". Lower the source's entries per request"
		}
		return &datasource.ValidationError{Field: "query", Message: message, Err: detail}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &datasource.UpstreamError{Kind: datasource.UpstreamAuth, Status: status, Message: "The source rejected Logchef's credentials", Err: detail}
	case http.StatusTooManyRequests:
		return &datasource.UpstreamError{Kind: datasource.UpstreamBusy, Status: status, Message: "Loki is busy", Err: detail}
	default:
		return &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Status: status, Message: fmt.Sprintf("Loki is unavailable (status %d)", status), Err: detail}
	}
}

// errorMessage returns at most the first 500 characters of a plain-text Loki
// error body on one line. Any other body, such as a gateway's HTML page, is
// not shown.
func errorMessage(body, contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "text/plain" {
		return ""
	}
	text := strings.Join(strings.Fields(strings.ToValidUTF8(body, "")), " ")
	if strings.ContainsFunc(text, unicode.IsControl) {
		return ""
	}
	if runes := []rune(text); len(runes) > maxErrorMessageChars {
		text = string(runes[:maxErrorMessageChars]) + "..."
	}
	return text
}

// redactSecrets blanks every credential and custom header value of the
// source in an upstream error body, because a gateway may echo the request's
// credential and the body reaches users and the server log.
func redactSecrets(body string, conn models.LokiConnectionInfo) string {
	secrets := []string{conn.Auth.Password, conn.Auth.Token}
	if conn.Auth.Username != "" || conn.Auth.Password != "" {
		secrets = append(secrets, base64.StdEncoding.EncodeToString([]byte(conn.Auth.Username+":"+conn.Auth.Password)))
	}
	for _, value := range conn.Headers {
		secrets = append(secrets, value)
	}
	for _, secret := range secrets {
		if strings.TrimSpace(secret) != "" {
			body = strings.ReplaceAll(body, secret, "[redacted]")
		}
	}
	return body
}

// decodeBody parses a successful JSON body. A body that does not parse means
// the server is not a working Loki.
func decodeBody(body []byte, out any) error {
	if err := json.Unmarshal(body, out); err != nil {
		return &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Status: http.StatusOK, Message: "Loki sent a response that could not be read", Err: err}
	}
	return nil
}

func joinBaseURL(baseURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid loki base_url: %w", err)
	}
	// Keep any percent-encoding of the base path: read EscapedPath before
	// Path changes. path is a fixed ASCII literal.
	escapedBase := strings.TrimRight(parsed.EscapedPath(), "/")
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawPath = escapedBase + path
	return parsed.String(), nil
}

func nanos(t time.Time) string {
	return strconv.FormatInt(t.UnixNano(), 10)
}

func (p *Provider) PopulateSourceDetails(ctx context.Context, source *models.Source) error {
	if source == nil {
		return fmt.Errorf("source is required")
	}
	source.Columns = nil
	source.Schema = ""
	source.Engine = "Loki"
	source.EngineParams = nil
	source.SortKeys = nil

	columns, err := p.GetSourceSchema(ctx, source)
	if err != nil {
		return err
	}
	source.Columns = columns
	return nil
}

// Column types of the fixed columns and of stream labels. A detected field
// keeps the type Loki reports.
const (
	timeColumnType   = "DateTime"
	stringColumnType = "String"
	labelColumnType  = "label"
)

type labelsResponse struct {
	Data []string `json:"data"`
}

type detectedFieldsResponse struct {
	Fields []struct {
		Label string `json:"label"`
		Type  string `json:"type"`
		// Parsers is null for structured metadata.
		Parsers []string `json:"parsers"`
	} `json:"fields"`
}

// GetSourceSchema returns _time and _msg, the stream labels of the selector,
// then the detected fields that need no parser or the source's parser.
func (p *Provider) GetSourceSchema(ctx context.Context, source *models.Source) ([]models.ColumnInfo, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	end := time.Now().UTC()
	start := end.Add(-time.Duration(conn.EffectiveLimits().SchemaLookbackSeconds) * time.Second)
	window := lokiParams{"query": {conn.Selector}, "start": {nanos(start)}, "end": {nanos(end)}}

	body, err := p.get(ctx, conn, "/loki/api/v1/labels", window, metadataMaxBytes)
	if err != nil {
		return nil, err
	}
	var labels labelsResponse
	if err := decodeBody(body, &labels); err != nil {
		return nil, err
	}

	fieldParams := lokiParams{"limit": {strconv.Itoa(detectedFieldsLimit)}}
	maps.Copy(fieldParams, window)
	body, err = p.get(ctx, conn, "/loki/api/v1/detected_fields", fieldParams, metadataMaxBytes)
	if err != nil {
		return nil, err
	}
	var detected detectedFieldsResponse
	if err := decodeBody(body, &detected); err != nil {
		return nil, err
	}
	return schemaColumns(labels.Data, detected, conn.Parser), nil
}

func schemaColumns(labels []string, detected detectedFieldsResponse, parser string) []models.ColumnInfo {
	columns := []models.ColumnInfo{{Name: "_time", Type: timeColumnType}, {Name: "_msg", Type: stringColumnType}}
	seen := map[string]bool{"_time": true, "_msg": true}
	slices.Sort(labels)
	for _, name := range labels {
		if seen[name] {
			continue
		}
		seen[name] = true
		columns = append(columns, models.ColumnInfo{Name: name, Type: labelColumnType})
	}
	for _, field := range detected.Fields {
		if seen[field.Label] {
			continue
		}
		if len(field.Parsers) > 0 && (parser == "" || !slices.Contains(field.Parsers, parser)) {
			continue
		}
		seen[field.Label] = true
		columns = append(columns, models.ColumnInfo{Name: field.Label, Type: field.Type})
	}
	return columns
}

// CompileLogchefQL compiles a LogchefQL query into a LogQL log query that
// starts from the source's selector. The time range is not in the query;
// callers pass it to QueryLogs separately.
func (p *Provider) CompileLogchefQL(_ context.Context, source *models.Source, req datasource.LogchefQLCompileRequest) (*datasource.CompiledLogchefQL, error) {
	compiled := &datasource.CompiledLogchefQL{Language: models.QueryLanguageLogQL}
	options, err := p.logQLOptions(source)
	if err != nil {
		return compiled, err
	}

	result := logchefql.TranslateToLogQL(req.Query, options)
	compiled.Valid = result.Valid
	compiled.Error = result.Error
	compiled.Conditions = result.Conditions
	compiled.FieldsUsed = result.FieldsUsed
	compiled.Query = result.Query
	compiled.FilterOnly = result.Query
	if result.Error != nil {
		return compiled, result.Error
	}
	return compiled, nil
}

// CompileAlertCondition compiles a LogchefQL alert condition into a LogQL
// metric query whose range is the lookback. EvaluateAlert runs it as an
// instant query at At, so the range ends there.
func (p *Provider) CompileAlertCondition(_ context.Context, source *models.Source, query string, lookbackSeconds int) (*datasource.CompiledLogchefQL, error) {
	options, err := p.logQLOptions(source)
	if err != nil {
		return nil, err
	}
	result := logchefql.TranslateToLogQLMetric(query, options, lookbackSeconds)
	if result.Error != nil {
		return nil, result.Error
	}
	return &datasource.CompiledLogchefQL{
		Language:    models.QueryLanguageLogQL,
		Valid:       true,
		Conditions:  result.Conditions,
		FieldsUsed:  result.FieldsUsed,
		Query:       result.Query,
		Aggregation: result.Aggregation,
	}, nil
}

// logQLOptions are the source's selector, parser and stream labels, which
// every compiled query starts from.
func (p *Provider) logQLOptions(source *models.Source) (logchefql.LogQLTranslateOptions, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return logchefql.LogQLTranslateOptions{}, err
	}
	selector, err := models.ParseLokiSelector(conn.Selector)
	if err != nil {
		return logchefql.LogQLTranslateOptions{}, fmt.Errorf("read the source selector: %w", err)
	}
	options := logchefql.LogQLTranslateOptions{Parser: conn.Parser}
	for _, m := range selector {
		options.Selector = append(options.Selector, logchefql.LogQLMatcher{Name: m.Name, Op: m.Op, Value: m.Value})
	}
	for _, column := range source.Columns {
		if column.Type == labelColumnType {
			options.StreamLabels = append(options.StreamLabels, column.Name)
		}
	}
	return options, nil
}

type indexStatsResponse struct {
	Streams uint64 `json:"streams"`
	Chunks  uint64 `json:"chunks"`
	Entries uint64 `json:"entries"`
	Bytes   uint64 `json:"bytes"`
}

func (p *Provider) InspectSource(ctx context.Context, source *models.Source) (*datasource.SourceInspection, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	version, err := p.buildVersion(ctx, conn)
	if err != nil {
		return nil, err
	}
	end := time.Now().UTC()
	statsParams := lokiParams{"query": {conn.Selector}, "start": {nanos(end.Add(-statsLookback))}, "end": {nanos(end)}}
	body, err := p.get(ctx, conn, "/loki/api/v1/index/stats", statsParams, metadataMaxBytes)
	if err != nil {
		return nil, err
	}
	var stats indexStatsResponse
	if err := decodeBody(body, &stats); err != nil {
		return nil, err
	}
	columns, err := p.GetSourceSchema(ctx, source)
	if err != nil {
		return nil, err
	}

	fields := make([]datasource.SourceSchemaField, len(columns))
	for i, column := range columns {
		fields[i] = datasource.SourceSchemaField{Name: column.Name, Type: column.Type}
	}
	return &datasource.SourceInspection{
		Details: inspectionDetails(source, conn, version),
		Storage: storageMetrics(stats),
		Schema:  &datasource.SourceSchemaInspection{Fields: fields},
	}, nil
}

func inspectionDetails(source *models.Source, conn models.LokiConnectionInfo, version string) []datasource.InspectionDetail {
	parser := conn.Parser
	if parser == "" {
		parser = "none"
	}
	details := []datasource.InspectionDetail{
		{Key: "backend", Label: "Backend", Value: "Loki"},
		{Key: "version", Label: "Loki Version", Value: version, Monospace: true},
		{Key: "base_url", Label: "Base URL", Value: conn.BaseURL, Monospace: true},
	}
	if conn.TenantID != "" {
		details = append(details, datasource.InspectionDetail{Key: "tenant", Label: "Tenant", Value: conn.TenantID, Monospace: true})
	}
	details = append(details,
		datasource.InspectionDetail{Key: "selector", Label: "Default Selector", Value: conn.Selector, Monospace: true, Multiline: true},
		datasource.InspectionDetail{Key: "parser", Label: "Parser", Value: parser, Monospace: true},
		datasource.InspectionDetail{Key: "timestamp_field", Label: "Timestamp Field", Value: source.MetaTSField, Monospace: true},
	)
	if source.MetaSeverityField != "" {
		details = append(details, datasource.InspectionDetail{Key: "severity_field", Label: "Severity Field", Value: source.MetaSeverityField, Monospace: true})
	}
	return details
}

func storageMetrics(stats indexStatsResponse) []datasource.InspectionMetric {
	const hint = "From the index. Excludes data not yet flushed"
	format := func(n uint64) string { return strconv.FormatUint(n, 10) }
	return []datasource.InspectionMetric{
		{Key: "streams", Label: "Streams (24h)", Value: format(stats.Streams), Hint: hint},
		{Key: "chunks", Label: "Chunks (24h)", Value: format(stats.Chunks), Hint: hint},
		{Key: "entries", Label: "Entries (24h)", Value: format(stats.Entries), Hint: hint},
		{Key: "bytes", Label: "Bytes (24h)", Value: format(stats.Bytes), Hint: hint},
	}
}

// logQueryAlertMessage names what a Loki alert needs. Loki answers an
// instant log query with a 400 that says "log queries are not supported as an
// instant query type".
const logQueryAlertMessage = `A Loki alert needs a metric query, such as sum(count_over_time({app="api"} |= "error" [5m]))`

type lokiSample struct {
	Metric map[string]string `json:"metric"`
	// Value is a [unix seconds, sample as a string] pair.
	Value [2]json.RawMessage `json:"value"`
}

// EvaluateAlert runs a LogQL metric query as an instant query at req.At. The
// query keeps its own range, such as [5m], so the window is that range ending
// at At. Each vector sample becomes a row of value, then the series labels.
func (p *Provider) EvaluateAlert(ctx context.Context, source *models.Source, req datasource.AlertQueryRequest) (*models.QueryResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	if language := models.NormalizeQueryLanguage(req.Language); language != models.QueryLanguageLogQL {
		return nil, fmt.Errorf("loki alerts require %q, got %q", models.QueryLanguageLogQL, language)
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("alert query is required")
	}
	if req.At.IsZero() {
		return nil, fmt.Errorf("alert evaluation needs the instant it stands for")
	}
	// Loki has no per-request timeout, so the query timeout is the deadline.
	if req.QueryTimeout != nil && *req.QueryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*req.QueryTimeout)*time.Second)
		defer cancel()
	}

	params := lokiParams{"query": {query}, "time": {nanos(req.At)}}
	body, err := p.get(ctx, conn, "/loki/api/v1/query", params, defaultMaxResponseBytes)
	if errors.Is(err, errBodyTooLarge) {
		return nil, &datasource.ValidationError{Field: "query", Message: fmt.Sprintf("Loki's alert result exceeds %d MiB. Aggregate it to fewer series, such as with sum by (label)", defaultMaxResponseBytes>>20)}
	}
	if validation, ok := errors.AsType[*datasource.ValidationError](err); ok && strings.Contains(validation.Message, "not supported as an instant query") {
		return nil, &datasource.ValidationError{Field: "query", Message: logQueryAlertMessage, Err: validation}
	}
	if err != nil {
		return nil, err
	}
	// An instant query answers in the shape of a range query.
	var resp queryRangeResponse
	if err := decodeBody(body, &resp); err != nil {
		return nil, err
	}
	if resp.Data.ResultType != "vector" {
		return nil, &datasource.ValidationError{Field: "query", Message: fmt.Sprintf("Loki returned a %q result. %s", resp.Data.ResultType, logQueryAlertMessage)}
	}
	var samples []lokiSample
	if err := decodeBody(resp.Data.Result, &samples); err != nil {
		return nil, err
	}
	return vectorResult(samples, resp.Data.Stats.Summary), nil
}

// vectorResult orders the series by their labels, so an ungrouped rule that
// reads the first row reads the same series every time.
func vectorResult(samples []lokiSample, summary lokiSummary) *models.QueryResult {
	slices.SortFunc(samples, func(a, b lokiSample) int { return strings.Compare(labelString(a.Metric), labelString(b.Metric)) })
	names := map[string]struct{}{}
	rows := make([]map[string]any, 0, len(samples))
	for _, s := range samples {
		row := make(map[string]any, len(s.Metric)+1)
		for name, value := range s.Metric {
			row[name] = value
			names[name] = struct{}{}
		}
		// A series label named value loses to the sample.
		row["value"] = sampleValue(s.Value[1])
		rows = append(rows, row)
	}
	columns := append([]models.ColumnInfo{{Name: "value", Type: stringColumnType}}, labelColumns(names, "value")...)
	return &models.QueryResult{
		Logs:    rows,
		Columns: columns,
		Stats: models.QueryStats{
			ExecutionTimeMs: summary.ExecTime * 1000,
			RowsRead:        int(summary.TotalLinesProcessed),
			BytesRead:       int(summary.TotalBytesProcessed),
			RowsReturned:    len(rows),
		},
	}
}
