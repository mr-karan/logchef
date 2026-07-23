package victorialogs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	defaultDiscoveryLookback = 24 * time.Hour
	defaultQueryLimit        = 1000
	// defaultHistogramSeriesLimit caps the number of group-by series returned
	// for a histogram, mirroring the ClickHouse provider's top-10 group cap. A
	// high-cardinality group-by field (e.g. trace_id) would otherwise return
	// one series per distinct value — a server-side memory spike and a
	// multi-MB payload for the browser.
	defaultHistogramSeriesLimit = 10
	// histogramOtherSeriesLabel remains the GroupValue assigned to VL's
	// catch-all aggregate series for backward compatibility. IsOther is the
	// structural identity used by new clients.
	histogramOtherSeriesLabel = "__other__"
)

type valuesResponse struct {
	Values []struct {
		Value string `json:"value"`
		Hits  int64  `json:"hits"`
	} `json:"values"`
}

type facetsResponse struct {
	Facets []struct {
		FieldName string `json:"field_name"`
		Values    []struct {
			FieldValue string `json:"field_value"`
			Hits       int64  `json:"hits"`
		} `json:"values"`
	} `json:"facets"`
}

type hitsResponse struct {
	Hits []struct {
		Fields     map[string]string `json:"fields"`
		Timestamps []string          `json:"timestamps"`
		Values     []int64           `json:"values"`
		Total      int64             `json:"total"`
	} `json:"hits"`
}

type prometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value,omitempty"`
			Values [][]any           `json:"values,omitempty"`
		} `json:"result"`
	} `json:"data"`
}

func (p *Provider) PopulateSourceDetails(ctx context.Context, source *models.Source) error {
	if source == nil {
		return fmt.Errorf("source is required")
	}

	source.Columns = nil
	source.Schema = ""
	source.Engine = "VictoriaLogs"
	source.EngineParams = nil
	source.SortKeys = nil

	columns, err := p.GetSourceSchema(ctx, source)
	if err != nil {
		return err
	}
	source.Columns = columns
	return nil
}

func (p *Provider) QueryLogs(ctx context.Context, source *models.Source, req datasource.QueryRequest) (*models.QueryResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}

	query := strings.TrimSpace(req.RawQuery)
	if query == "" {
		query = "*"
	}
	// Sort explore results newest-first, matching the ClickHouse provider's
	// ORDER BY timestamp DESC. Only this path sorts — alert/stats and
	// field-values queries must not carry a sort pipe.
	query = appendDefaultSort(query)

	form := url.Values{}
	form.Set("query", query)

	limit, limitAdded, limitCapped := resolveQueryLimit(req.Limit, req.DefaultLimit, req.MaxLimit)
	if limit > 0 {
		form.Set("limit", strconv.Itoa(limit))
	}
	if req.StartTime != nil {
		form.Set("start", formatAPITime(*req.StartTime))
	}
	if req.EndTime != nil {
		form.Set("end", formatAPITime(*req.EndTime))
	}
	if timeout := formatTimeout(req.QueryTimeout); timeout != "" {
		form.Set("timeout", timeout)
	}
	applyScopeFilters(form, conn)

	resp, err := p.doFormRequest(ctx, conn, "/select/logsql/query", form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	logs, columnNames, bytesReturned, truncatedReason, err := readQueryRows(resp.Body, req.MaxResponseBytes, limit)
	if err != nil {
		return nil, err
	}

	columns := make([]models.ColumnInfo, 0, len(columnNames))
	for _, name := range orderFieldNames(source, columnNames) {
		columns = append(columns, models.ColumnInfo{
			Name: name,
			Type: inferColumnType(source, name),
		})
	}

	stats := statsFromHeaders(resp, len(logs))
	stats.RowsReturned = len(logs)
	stats.BytesReturned = bytesReturned
	if limit > 0 {
		stats.LimitApplied = limit
	}
	if truncatedReason != "" {
		stats.Truncated = true
		stats.TruncatedReason = truncatedReason
	}

	warnings := make([]models.QueryWarning, 0, 2)
	if limitAdded {
		warnings = append(warnings, models.QueryWarning{
			Code:    "LIMIT_APPLIED",
			Message: fmt.Sprintf("Showing first %d rows. Use download for larger results.", limit),
		})
	}
	if limitCapped {
		warnings = append(warnings, models.QueryWarning{
			Code:    "LIMIT_CAPPED",
			Message: fmt.Sprintf("Result limit capped at %d rows.", limit),
		})
	}

	return &models.QueryResult{
		Logs:     logs,
		Columns:  columns,
		Stats:    stats,
		Warnings: warnings,
	}, nil
}

// readQueryRows reads VL's newline-delimited JSON query response (one row
// object per line) and enforces the response byte budget while reading, so a
// single very large row cannot allocate beyond the budget. Byte accounting uses
// the raw wire length (the escaped JSON actually transferred), not the decoded
// string length. It returns the decoded rows, the column names in first-seen
// order, the bytes accounted, and a truncation reason ("" if none).
func readQueryRows(body io.Reader, maxResponseBytes, limitHint int) (logs []map[string]interface{}, columnNames []string, bytesReturned int, truncatedReason string, err error) {
	logs = make([]map[string]interface{}, 0, limitHint)
	columnSet := make(map[string]struct{})
	columnNames = make([]string, 0)

	reader := body
	if maxResponseBytes > 0 {
		// Cap the bytes pulled off the socket at the budget so even an oversized
		// row cannot be buffered in full. The +1 byte of headroom lets the
		// budget check below distinguish a row that exactly meets the budget
		// from one that overflows it, and guarantees any line the LimitReader
		// truncates trips that check rather than being decoded as partial JSON.
		reader = io.LimitReader(body, int64(maxResponseBytes)+1)
	}
	lineReader := bufio.NewReader(reader)
	for {
		line, readErr := lineReader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			lineSize := len(line)
			if maxResponseBytes > 0 && bytesReturned+lineSize > maxResponseBytes {
				return logs, columnNames, bytesReturned, "byte_limit", nil
			}

			var row map[string]interface{}
			dec := json.NewDecoder(bytes.NewReader(trimmed))
			dec.UseNumber()
			if decErr := dec.Decode(&row); decErr != nil {
				return nil, nil, 0, "", fmt.Errorf("decode victorialogs query response: %w", decErr)
			}

			// Count bytes unconditionally (even when unbounded) so
			// Stats.BytesReturned reflects the real payload size.
			bytesReturned += lineSize
			logs = append(logs, row)
			for key := range row {
				if _, ok := columnSet[key]; ok {
					continue
				}
				columnSet[key] = struct{}{}
				columnNames = append(columnNames, key)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				return logs, columnNames, bytesReturned, truncatedReason, nil
			}
			return nil, nil, 0, "", fmt.Errorf("read victorialogs query response: %w", readErr)
		}
	}
}

// translateLogchefQLToLogsQL is the single point where LogchefQL is compiled
// into LogsQL for this provider. Both the field-values call sites (via
// compileQueryForVictoriaLogs) and the LogchefQLCompiler interface method wrap
// it.
func translateLogchefQLToLogsQL(queryText string, source *models.Source) *logchefql.LogsQLTranslateResult {
	return logchefql.TranslateToLogsQL(queryText, &logchefql.LogsQLTranslateOptions{
		DefaultTimestampField: source.MetaTSField,
	})
}

func compileQueryForVictoriaLogs(queryText string, language models.QueryLanguage, source *models.Source) (string, error) {
	normalizedLanguage := models.NormalizeQueryLanguage(language)
	switch normalizedLanguage {
	case "", models.QueryLanguageLogsQL:
		query := strings.TrimSpace(queryText)
		if query == "" {
			return "*", nil
		}
		return query, nil
	case models.QueryLanguageLogchefQL:
		result := translateLogchefQLToLogsQL(queryText, source)
		if !result.Valid {
			if result.Error != nil {
				return "", result.Error
			}
			return "", fmt.Errorf("invalid LogchefQL query")
		}
		return result.Query, nil
	default:
		return "", fmt.Errorf("victorialogs does not support query language %q", normalizedLanguage)
	}
}

// CompileLogchefQL compiles a LogchefQL query into a LogsQL query. The time
// range is not baked into the query; callers pass it to QueryLogs separately.
func (p *Provider) CompileLogchefQL(_ context.Context, source *models.Source, req datasource.LogchefQLCompileRequest) (*datasource.CompiledLogchefQL, error) {
	result := translateLogchefQLToLogsQL(req.Query, source)
	compiled := &datasource.CompiledLogchefQL{
		Language:   models.QueryLanguageLogsQL,
		Valid:      result.Valid,
		Error:      result.Error,
		Conditions: result.Conditions,
		FieldsUsed: result.FieldsUsed,
		Query:      result.Query,
		FilterOnly: result.Query,
	}
	if !result.Valid {
		if result.Error != nil {
			return compiled, result.Error
		}
		return compiled, fmt.Errorf("invalid LogchefQL query")
	}
	return compiled, nil
}

func (p *Provider) GetSourceSchema(ctx context.Context, source *models.Source) ([]models.ColumnInfo, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}

	start, end := defaultDiscoveryWindow()
	form := url.Values{}
	form.Set("query", "*")
	form.Set("start", formatAPITime(start))
	form.Set("end", formatAPITime(end))
	form.Set("ignore_pipes", "1")
	applyScopeFilters(form, conn)

	var result valuesResponse
	if err := p.decodeJSONRequest(ctx, conn, "/select/logsql/field_names", form, &result); err != nil {
		return nil, err
	}

	fieldNames := make([]string, 0, len(result.Values)+2)
	for _, value := range result.Values {
		if strings.TrimSpace(value.Value) == "" {
			continue
		}
		fieldNames = append(fieldNames, value.Value)
	}
	fieldNames = ensureFieldNames(fieldNames, source.MetaTSField, source.MetaSeverityField)

	columns := make([]models.ColumnInfo, 0, len(fieldNames))
	for _, name := range orderFieldNames(source, fieldNames) {
		columns = append(columns, models.ColumnInfo{
			Name: name,
			Type: inferColumnType(source, name),
		})
	}

	return columns, nil
}

func (p *Provider) Histogram(ctx context.Context, source *models.Source, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = "*"
	}

	form := url.Values{}
	form.Set("query", query)
	form.Set("step", defaultWindow(req.Window))
	form.Set("ignore_pipes", "1")
	if req.StartTime != nil {
		form.Set("start", formatAPITime(*req.StartTime))
	}
	if req.EndTime != nil {
		form.Set("end", formatAPITime(*req.EndTime))
	}
	if offset := formatTimezoneOffset(req.Timezone, req.StartTime, req.EndTime); offset != "" {
		form.Set("offset", offset)
	}
	if timeout := formatTimeout(req.QueryTimeout); timeout != "" {
		form.Set("timeout", timeout)
	}
	groupBy := strings.TrimSpace(req.GroupBy)
	if groupBy != "" {
		form.Add("field", groupBy)
		// Cap the number of returned series so a high-cardinality group-by can't
		// blow up server memory or the response payload.
		form.Set("fields_limit", strconv.Itoa(defaultHistogramSeriesLimit))
	}
	applyScopeFilters(form, conn)

	var result hitsResponse
	if err := p.decodeJSONRequest(ctx, conn, "/select/logsql/hits", form, &result); err != nil {
		return nil, err
	}

	data := make([]datasource.HistogramBucket, 0)
	// truncated tracks whether VL returned its catch-all aggregate series, which
	// it emits only when the real group count exceeded fields_limit (see
	// getTopHitsSeries in VL app/vlselect/logsql/logsql.go).
	truncated := false
	for _, series := range result.Hits {
		groupValue := ""
		isOther := false
		if groupBy != "" {
			value, hasGroupField := series.Fields[groupBy]
			if !hasGroupField {
				// VL's top-hits cap folds every series beyond fields_limit into a
				// single catch-all series keyed "{}", serialized with an empty
				// `fields` object. It lacks the group-by field, so it must not be
				// rendered as a genuine empty-value group; retain the legacy
				// marker while identifying it structurally as synthetic Other.
				groupValue = histogramOtherSeriesLabel
				isOther = true
				truncated = true
			} else {
				groupValue = value
			}
		}
		for i, timestampRaw := range series.Timestamps {
			if i >= len(series.Values) {
				break
			}
			bucket, err := time.Parse(time.RFC3339, timestampRaw)
			if err != nil {
				return nil, fmt.Errorf("parse victorialogs histogram timestamp %q: %w", timestampRaw, err)
			}
			data = append(data, datasource.HistogramBucket{
				Bucket:     bucket,
				LogCount:   int(series.Values[i]),
				GroupValue: groupValue,
				IsOther:    isOther,
			})
		}
	}

	// VL returns one series at a time, so a group-by result interleaves buckets
	// across groups. Sort globally by bucket time (the activity/ClickHouse paths
	// also emit time-ordered buckets) so the chart renders coherently.
	slices.SortStableFunc(data, func(a, b datasource.HistogramBucket) int {
		return a.Bucket.Compare(b.Bucket)
	})

	// Only warn when VL actually truncated (i.e. it returned the catch-all
	// aggregate). Exactly defaultHistogramSeriesLimit real series is NOT a
	// truncation. The remaining series' counts are still shown, aggregated into
	// the "other" bucket, so the notice says so rather than claiming they are
	// hidden.
	notice := ""
	if groupBy != "" && truncated {
		notice = fmt.Sprintf("Showing top %d series by %q; the rest are aggregated into an \"other\" bucket.", defaultHistogramSeriesLimit, groupBy)
	}

	return &datasource.HistogramResult{
		Granularity: defaultWindow(req.Window),
		Data:        data,
		Notice:      notice,
	}, nil
}

func (p *Provider) GetFieldValues(ctx context.Context, source *models.Source, req datasource.FieldValuesRequest) (*datasource.FieldValuesResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}

	query, err := compileQueryForVictoriaLogs(req.QueryText, req.Language, source)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("query", query)
	form.Set("field", req.FieldName)
	form.Set("start", formatAPITime(req.StartTime))
	form.Set("end", formatAPITime(req.EndTime))
	form.Set("ignore_pipes", "1")
	if req.Limit > 0 {
		form.Set("limit", strconv.Itoa(req.Limit))
	}
	if timeout := formatTimeout(req.Timeout); timeout != "" {
		form.Set("timeout", timeout)
	}
	applyScopeFilters(form, conn)

	var result valuesResponse
	if err := p.decodeJSONRequest(ctx, conn, "/select/logsql/field_values", form, &result); err != nil {
		return nil, err
	}

	values := make([]datasource.FieldValueInfo, 0, len(result.Values))
	for _, value := range result.Values {
		values = append(values, datasource.FieldValueInfo{
			Value: value.Value,
			Count: value.Hits,
		})
	}

	fieldType := req.FieldType
	if strings.TrimSpace(fieldType) == "" {
		fieldType = inferColumnType(source, req.FieldName)
	}

	return &datasource.FieldValuesResult{
		FieldName:        req.FieldName,
		FieldType:        fieldType,
		IsLowCardinality: false,
		Values:           values,
		TotalDistinct:    int64(len(values)),
	}, nil
}

func (p *Provider) GetAllFieldValues(ctx context.Context, source *models.Source, req datasource.AllFieldValuesRequest) (datasource.AllFieldValuesResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}

	query, err := compileQueryForVictoriaLogs(req.QueryText, req.Language, source)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("query", query)
	form.Set("start", formatAPITime(req.StartTime))
	form.Set("end", formatAPITime(req.EndTime))
	form.Set("ignore_pipes", "1")
	form.Set("keep_const_fields", "1")
	if req.Limit > 0 {
		form.Set("limit", strconv.Itoa(req.Limit))
	}
	if timeout := formatTimeout(req.Timeout); timeout != "" {
		form.Set("timeout", timeout)
	}
	applyScopeFilters(form, conn)

	var result facetsResponse
	if err := p.decodeJSONRequest(ctx, conn, "/select/logsql/facets", form, &result); err != nil {
		return nil, err
	}

	allValues := make(datasource.AllFieldValuesResult, len(result.Facets))
	for _, facet := range result.Facets {
		values := make([]datasource.FieldValueInfo, 0, len(facet.Values))
		for _, value := range facet.Values {
			values = append(values, datasource.FieldValueInfo{
				Value: value.FieldValue,
				Count: value.Hits,
			})
		}
		allValues[facet.FieldName] = &datasource.FieldValuesResult{
			FieldName:        facet.FieldName,
			FieldType:        inferColumnType(source, facet.FieldName),
			IsLowCardinality: false,
			Values:           values,
			TotalDistinct:    int64(len(values)),
		}
	}

	return allValues, nil
}

func (p *Provider) InspectSource(ctx context.Context, source *models.Source) (*datasource.SourceInspection, error) {
	columns, err := p.GetSourceSchema(ctx, source)
	if err != nil {
		return nil, err
	}

	return &datasource.SourceInspection{
		Details: buildVictoriaLogsInspectionDetails(source),
		Schema: &datasource.SourceSchemaInspection{
			Fields: mapVictoriaLogsSchemaFields(columns),
		},
	}, nil
}

func (p *Provider) InspectSourceActivity(ctx context.Context, source *models.Source) (*datasource.SourceActivity, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	hourlyBuckets, err := p.fetchActivityBuckets(ctx, conn, now.Add(-24*time.Hour), now, "1h")
	if err != nil {
		return nil, err
	}
	return &datasource.SourceActivity{
		Rows1h:        sumBucketsSince(hourlyBuckets, now.Add(-time.Hour)),
		Rows24h:       sumBuckets(hourlyBuckets),
		HourlyBuckets: hourlyBuckets,
	}, nil
}

func (p *Provider) fetchActivityBuckets(
	ctx context.Context,
	conn models.VictoriaLogsConnectionInfo,
	start time.Time,
	end time.Time,
	step string,
) ([]datasource.IngestionBucket, error) {
	form := url.Values{}
	form.Set("query", "*")
	form.Set("start", formatAPITime(start))
	form.Set("end", formatAPITime(end))
	form.Set("step", step)
	form.Set("ignore_pipes", "1")
	applyScopeFilters(form, conn)

	var result hitsResponse
	if err := p.decodeJSONRequest(ctx, conn, "/select/logsql/hits", form, &result); err != nil {
		return nil, err
	}

	bucketsByTime := make(map[time.Time]uint64)
	for _, series := range result.Hits {
		for index, timestampRaw := range series.Timestamps {
			if index >= len(series.Values) {
				break
			}
			bucket, err := time.Parse(time.RFC3339, timestampRaw)
			if err != nil {
				return nil, fmt.Errorf("parse victorialogs activity bucket %q: %w", timestampRaw, err)
			}
			bucketsByTime[bucket] += uint64(max(series.Values[index], 0))
		}
	}

	buckets := make([]datasource.IngestionBucket, 0, len(bucketsByTime))
	for bucket, rows := range bucketsByTime {
		buckets = append(buckets, datasource.IngestionBucket{
			Bucket: bucket,
			Rows:   rows,
		})
	}
	slices.SortFunc(buckets, func(a, b datasource.IngestionBucket) int {
		return a.Bucket.Compare(b.Bucket)
	})

	return buckets, nil
}

func sumBuckets(buckets []datasource.IngestionBucket) uint64 {
	var total uint64
	for _, bucket := range buckets {
		total += bucket.Rows
	}
	return total
}

func sumBucketsSince(buckets []datasource.IngestionBucket, since time.Time) uint64 {
	var total uint64
	for _, bucket := range buckets {
		if bucket.Bucket.Before(since) {
			continue
		}
		total += bucket.Rows
	}
	return total
}

func (p *Provider) EvaluateAlert(ctx context.Context, source *models.Source, req datasource.AlertQueryRequest) (*models.QueryResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	if language := models.NormalizeQueryLanguage(req.Language); language != "" && language != models.QueryLanguageLogsQL {
		return nil, fmt.Errorf("victorialogs alerts require %q, got %q", models.QueryLanguageLogsQL, language)
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("alert query is required")
	}
	query = applyAlertLookback(query, req.LookbackSeconds)

	form := url.Values{}
	form.Set("query", query)
	form.Set("time", formatAPITime(time.Now().UTC()))
	if timeout := formatTimeout(req.QueryTimeout); timeout != "" {
		form.Set("timeout", timeout)
	}
	applyScopeFilters(form, conn)

	var result prometheusResponse
	if err := p.decodeJSONRequest(ctx, conn, "/select/logsql/stats_query", form, &result); err != nil {
		return nil, err
	}
	if !strings.EqualFold(result.Status, "success") {
		return nil, fmt.Errorf("victorialogs stats_query returned status %q", result.Status)
	}

	rows := make([]map[string]interface{}, 0, len(result.Data.Result))
	labelSet := make(map[string]struct{})
	for _, item := range result.Data.Result {
		row := map[string]interface{}{}
		for key, value := range item.Metric {
			row[key] = value
			labelSet[key] = struct{}{}
		}
		if len(item.Value) >= 2 {
			row["value"] = item.Value[1]
		}
		rows = append(rows, row)
	}

	// Declare a column per metric label (sorted for determinism) plus the
	// numeric value, so the schema matches the rows. Hardcoding only "value"
	// dropped the group-by labels that the rows actually carry.
	labelNames := make([]string, 0, len(labelSet))
	for name := range labelSet {
		labelNames = append(labelNames, name)
	}
	slices.Sort(labelNames)

	columns := make([]models.ColumnInfo, 0, len(labelNames)+1)
	for _, name := range labelNames {
		columns = append(columns, models.ColumnInfo{Name: name, Type: "String"})
	}
	columns = append(columns, models.ColumnInfo{Name: "value", Type: "Float64"})

	return &models.QueryResult{
		Logs:    rows,
		Columns: columns,
		Stats: models.QueryStats{
			ExecutionTimeMs: 0,
			RowsRead:        len(rows),
		},
	}, nil
}

func applyAlertLookback(query string, lookbackSeconds int) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || lookbackSeconds <= 0 {
		return trimmed
	}

	lookbackFilter := fmt.Sprintf("_time:%s", formatLookbackDuration(lookbackSeconds))

	prefix := ""
	rest := trimmed
	if strings.HasPrefix(trimmed, "options(") {
		// Split off the options(...) prefix at its MATCHING close paren, not the
		// first ")": a real option value such as
		// options(global_filter=(service:="api")) contains nested parens.
		open := strings.IndexByte(trimmed, '(')
		if end := matchingParen(trimmed, open); end >= 0 {
			prefix = strings.TrimSpace(trimmed[:end+1])
			rest = strings.TrimSpace(trimmed[end+1:])
		}
	}

	// A native LogsQL alert query may already scope _time itself. LogsQL ANDs
	// multiple _time filters, so prepending another would silently narrow (or
	// zero, if disjoint) the window. Leave the query's own window intact only
	// when it is genuinely bounded — see isAlreadyTimeBounded.
	if isAlreadyTimeBounded(rest) {
		return trimmed
	}

	// The injected filter binds by AND, which is tighter than OR. If the query
	// has a top-level OR (e.g. `_time:5m OR level:critical`), a bare prepend
	// would bind only to the first branch and leave the rest unbounded, so wrap
	// the query in parens to bound every branch.
	toBound := rest
	if hasTopLevelOr(rest) {
		toBound = "(" + rest + ")"
	}

	if prefix != "" {
		if toBound == "" {
			return fmt.Sprintf("%s %s", prefix, lookbackFilter)
		}
		return fmt.Sprintf("%s %s %s", prefix, lookbackFilter, toBound)
	}

	return fmt.Sprintf("%s %s", lookbackFilter, toBound)
}

// isAlreadyTimeBounded reports whether query is already constrained by a
// top-level, non-negated `_time:` filter, in which case alert lookback must not
// be re-injected. It is a deliberate structural heuristic, NOT a full LogsQL
// parser. Rules:
//   - A `_time:` is only counted at paren depth 0 (a `_time:` inside a group is
//     not guaranteed to bound the whole query).
//   - A `_time:` directly under NOT (e.g. `NOT _time:1h`) does not bound the
//     window, so it is ignored. (A `-_time:` negation is also ignored, since
//     `-` is a field-name glue byte and breaks the field boundary below.)
//   - A top-level OR means no single `_time:` bounds every branch, so the query
//     is treated as unbounded.
//
// Known conservative limits: a `_time:` inside a quoted string literal is a
// false match at the field-boundary level but is only a conservative skip
// (lookback still injected). Compound field names ending in `_time` (e.g.
// `custom._time:`) are correctly NOT matched because glue bytes extend the
// field boundary.
func isAlreadyTimeBounded(query string) bool {
	if hasTopLevelOr(query) {
		return false
	}

	const token = "_time:"
	depth := 0
	var quote byte
	escaped := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			switch c {
			case '\\':
				escaped = true
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
			continue
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth != 0 || c != '_' || !strings.HasPrefix(query[i:], token) {
			continue
		}
		if i > 0 && isFieldNameByte(query[i-1]) {
			continue // part of a longer compound field name
		}
		if precededByNegation(query, i) {
			continue
		}
		return true
	}
	return false
}

// hasTopLevelOr reports whether query contains an `OR` boolean operator at paren
// depth 0, outside any quoted literal. LogsQL requires whitespace/paren
// boundaries around the keyword, so `error` (containing "or") is not matched.
func hasTopLevelOr(query string) bool {
	depth := 0
	var quote byte
	escaped := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			switch c {
			case '\\':
				escaped = true
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && (c == 'o' || c == 'O') &&
				i+2 <= len(query) && strings.EqualFold(query[i:i+2], "or") &&
				isKeywordBoundary(query, i-1) && isKeywordBoundary(query, i+2) {
				return true
			}
		}
	}
	return false
}

// isKeywordBoundary reports whether the byte at index i is a boundary for a
// LogsQL boolean keyword: the start/end of the string, whitespace, or a paren.
func isKeywordBoundary(query string, i int) bool {
	if i < 0 || i >= len(query) {
		return true
	}
	switch query[i] {
	case ' ', '\t', '\n', '\r', '(', ')':
		return true
	}
	return false
}

// precededByNegation reports whether the token immediately before pos is the
// LogsQL `NOT` keyword.
func precededByNegation(query string, pos int) bool {
	j := pos - 1
	for j >= 0 && (query[j] == ' ' || query[j] == '\t') {
		j--
	}
	end := j + 1
	for j >= 0 && isKeywordLetter(query[j]) {
		j--
	}
	return strings.EqualFold(query[j+1:end], "not")
}

func isKeywordLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// matchingParen returns the index of the `)` matching the `(` at index open,
// or -1 if there is none. It counts paren depth while skipping quoted string
// literals so a paren inside a quoted value is not miscounted.
func matchingParen(s string, open int) int {
	if open < 0 || open >= len(s) || s[open] != '(' {
		return -1
	}
	depth := 0
	var quote byte
	escaped := false
	for i := open; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			switch c {
			case '\\':
				escaped = true
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// isFieldNameByte reports whether b can appear inside an unquoted LogsQL field
// name. Beyond alphanumerics and `_`, VictoriaLogs glues compound tokens on
// `. - / : $ +` (see glueCompoundTokens in VL lib/logstorage/parser.go), so a
// name like `custom._time` or `custom-_time` is a single field — and must not
// be detected as the `_time` filter.
func isFieldNameByte(b byte) bool {
	switch b {
	case '_', '.', '-', '/', ':', '$', '+':
		return true
	}
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

func formatLookbackDuration(seconds int) string {
	duration := time.Duration(seconds) * time.Second
	if duration%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(duration/(24*time.Hour)))
	}
	if duration%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	}
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	}
	return fmt.Sprintf("%ds", seconds)
}

func (p *Provider) decodeJSONRequest(ctx context.Context, conn models.VictoriaLogsConnectionInfo, path string, form url.Values, out interface{}) error {
	resp, err := p.doFormRequest(ctx, conn, path, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}

func (p *Provider) doFormRequest(ctx context.Context, conn models.VictoriaLogsConnectionInfo, path string, form url.Values) (*http.Response, error) {
	endpoint, err := joinBaseURL(conn.BaseURL, path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create victorialogs request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applyHeaders(req, conn)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("victorialogs request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("victorialogs request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func joinBaseURL(baseURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid victorialogs base_url: %w", err)
	}
	// Preserve any percent-encoding in the configured base path (e.g. an
	// encoded %2F segment) by updating RawPath alongside Path. Mutating only
	// Path lets url.String() re-derive the escaped form from the decoded Path
	// and silently drop the encoding. EscapedPath() must be read BEFORE Path is
	// mutated so it reflects the original encoding. path is a fixed ASCII
	// literal, so appending it verbatim needs no escaping.
	escapedBase := strings.TrimRight(parsed.EscapedPath(), "/")
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawPath = escapedBase + path
	return parsed.String(), nil
}

func applyScopeFilters(form url.Values, conn models.VictoriaLogsConnectionInfo) {
	scopeQuery := strings.TrimSpace(conn.Scope.Query)
	if scopeQuery == "" {
		return
	}

	if strings.HasPrefix(scopeQuery, "{") && strings.HasSuffix(scopeQuery, "}") {
		form.Add("extra_stream_filters", scopeQuery)
		return
	}
	form.Add("extra_filters", scopeQuery)
}

func buildVictoriaLogsInspectionDetails(source *models.Source) []datasource.InspectionDetail {
	if source == nil {
		return nil
	}

	details := []datasource.InspectionDetail{
		{Key: "backend", Label: "Backend", Value: "VictoriaLogs"},
		{Key: "timestamp_field", Label: "Timestamp Field", Value: source.MetaTSField, Monospace: true},
	}

	conn, err := source.VictoriaLogsConnection()
	if err != nil {
		return details
	}

	if conn.BaseURL != "" {
		details = append(details, datasource.InspectionDetail{
			Key: "base_url", Label: "Base URL", Value: conn.BaseURL, Monospace: true,
		})
	}
	if source.MetaSeverityField != "" {
		details = append(details, datasource.InspectionDetail{
			Key: "severity_field", Label: "Severity Field", Value: source.MetaSeverityField, Monospace: true,
		})
	}
	if conn.Tenant.AccountID != "" || conn.Tenant.ProjectID != "" {
		tenantValue := fmt.Sprintf("account=%s project=%s", emptyFallback(conn.Tenant.AccountID, "-"), emptyFallback(conn.Tenant.ProjectID, "-"))
		details = append(details, datasource.InspectionDetail{
			Key: "tenant", Label: "Tenant", Value: tenantValue, Monospace: true,
		})
	}
	if scope := strings.TrimSpace(conn.Scope.Query); scope != "" {
		details = append(details, datasource.InspectionDetail{
			Key: "scope", Label: "Immutable Scope", Value: scope, Monospace: true, Multiline: true,
		})
	}

	return details
}

func mapVictoriaLogsSchemaFields(columns []models.ColumnInfo) []datasource.SourceSchemaField {
	fields := make([]datasource.SourceSchemaField, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, datasource.SourceSchemaField{
			Name: column.Name,
			Type: column.Type,
		})
	}
	return fields
}

func emptyFallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultWindow(window string) string {
	if strings.TrimSpace(window) == "" {
		return "1m"
	}
	return strings.TrimSpace(window)
}

func formatTimeout(timeout *int) string {
	if timeout == nil || *timeout <= 0 {
		return ""
	}
	return fmt.Sprintf("%ds", *timeout)
}

func formatAPITime(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339)
}

// formatTimezoneOffset returns a VictoriaLogs-compatible duration string for
// the `offset` param on /select/logsql/hits, which shifts histogram bucket
// boundaries so they align to the given timezone's day/hour boundaries.
//
// VL's `offset` expects a duration (e.g. "19800s" / "5h30m"), not a clock
// offset string like "+05:30". The sign matches the zone's seconds-east-of-UTC
// value: verified empirically against a running VictoriaLogs instance that
// offset=19800s (UTC+5:30, Asia/Kolkata) aligns daily buckets to IST midnight
// (00:00 IST == 18:30 UTC), and offset=-9000s (UTC-2:30, America/St_Johns)
// aligns buckets to NDT midnight (00:00 NDT == 02:30 UTC) — i.e. the offset
// value is simply the zone's UTC offset in seconds.
func formatTimezoneOffset(timezone string, start, end *time.Time) string {
	locationName := strings.TrimSpace(timezone)
	if locationName == "" || strings.EqualFold(locationName, "UTC") {
		return ""
	}

	loc, err := time.LoadLocation(locationName)
	if err != nil {
		return ""
	}

	reference := time.Now().UTC()
	if start != nil {
		reference = *start
	} else if end != nil {
		reference = *end
	}

	_, offsetSeconds := reference.In(loc).Zone()
	if offsetSeconds == 0 {
		return ""
	}

	return fmt.Sprintf("%ds", offsetSeconds)
}

func statsFromHeaders(resp *http.Response, rowCount int) models.QueryStats {
	stats := models.QueryStats{RowsRead: rowCount}
	if resp == nil {
		return stats
	}

	durationRaw := strings.TrimSpace(resp.Header.Get("VL-Request-Duration-Seconds"))
	if durationRaw == "" {
		return stats
	}

	durationSeconds, err := strconv.ParseFloat(durationRaw, 64)
	if err != nil {
		return stats
	}
	stats.ExecutionTimeMs = durationSeconds * 1000
	return stats
}

// appendDefaultSort makes explore results newest-first. A pipe-free query
// (including the bare `*` default and plain filters — which is what
// LogchefQL-translated queries without projection look like) gets
// `| sort by (_time desc)` appended. A query whose only pipe stages are
// `fields` projections (LogchefQL's pipe operator) gets the sort inserted
// BEFORE the first projection, since projecting away `_time` first would
// break the sort. Anything with other pipe stages (stats, existing sort,
// limit, ...) is power-user territory and passes through untouched.
//
// The explicit sort is deliberate: we do NOT rely on VictoriaLogs' implicit
// ordering. A bare LogsQL query with a limit returns some N matching rows, not
// necessarily the newest N in time order, so dropping this would break the
// explorer's newest-first contract and the tests that encode it. Kept per #107.
func appendDefaultSort(query string) string {
	stages := splitTopLevelPipes(query)
	for _, stage := range stages[1:] {
		if !isProjectionStage(stage) {
			return query
		}
	}
	out := strings.TrimSpace(stages[0]) + " | sort by (_time desc)"
	for _, stage := range stages[1:] {
		out += " | " + strings.TrimSpace(stage)
	}
	return out
}

// isProjectionStage reports whether a pipe stage is a field projection, i.e.
// the `fields` pipe or its `keep` alias (VL pipe.go maps `keep` →
// parsePipeFields). Matching is case-insensitive since LogsQL pipe names are
// not case-sensitive.
func isProjectionStage(stage string) bool {
	trimmed := strings.TrimSpace(stage)
	name := trimmed
	if idx := strings.IndexAny(trimmed, " \t("); idx >= 0 {
		name = trimmed[:idx]
	}
	name = strings.ToLower(name)
	return name == "fields" || name == "keep"
}

// splitTopLevelPipes splits a LogsQL query on `|` pipe stages. The scan is
// quote-aware across all three LogsQL quote styles (double, single, and
// backtick), so a `|` inside any quoted string literal (e.g. `_msg:"a|b"`,
// `_msg:'a|b'`) is not treated as a stage boundary.
func splitTopLevelPipes(query string) []string {
	var stages []string
	var current strings.Builder
	var quote rune
	escaped := false
	for _, r := range query {
		if escaped {
			escaped = false
			current.WriteRune(r)
			continue
		}
		switch {
		case r == '\\':
			if quote != 0 {
				escaped = true
			}
			current.WriteRune(r)
		case quote != 0:
			if r == quote {
				quote = 0
			}
			current.WriteRune(r)
		case r == '"' || r == '\'' || r == '`':
			quote = r
			current.WriteRune(r)
		case r == '|':
			stages = append(stages, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	stages = append(stages, current.String())
	return stages
}

// resolveQueryLimit mirrors the ClickHouse limit policy: a limit-less call
// falls back to DefaultLimit (not MaxLimit), and any limit is capped at
// MaxLimit. It reports whether a default was injected (LIMIT_APPLIED) or the
// caller's limit was capped (LIMIT_CAPPED) so callers can surface a warning.
func resolveQueryLimit(limit, defaultLimit, maxLimit int) (applied int, added, capped bool) {
	if defaultLimit <= 0 {
		defaultLimit = defaultQueryLimit
	}
	if maxLimit <= 0 {
		maxLimit = defaultLimit
	}

	if limit <= 0 {
		applied = defaultLimit
		added = true
	} else {
		applied = limit
	}
	if applied > maxLimit {
		applied = maxLimit
		capped = true
	}
	return applied, added, capped
}

// inferColumnType maps a field name to a display type. VictoriaLogs is
// schemaless — the field discovery API returns names only, with no per-field
// type — so this uses name heuristics rather than the returned values. Value
// inference was considered but rejected: the same field can appear with
// different value shapes across rows/queries, which would make a column's type
// flap between requests. A name-based guess is stable and deterministic.
func inferColumnType(source *models.Source, name string) string {
	if name == "" {
		return "String"
	}
	if source != nil {
		if name == source.MetaTSField {
			return "DateTime"
		}
		if source.MetaSeverityField != "" && name == source.MetaSeverityField {
			return "String"
		}
	}

	lowerName := strings.ToLower(name)
	switch {
	case name == "_time":
		return "DateTime"
	case strings.Contains(lowerName, "timestamp"), strings.HasSuffix(lowerName, "_time"), strings.HasSuffix(lowerName, "_ts"):
		return "DateTime"
	case strings.HasPrefix(lowerName, "duration"), strings.HasSuffix(lowerName, "_ms"), strings.HasSuffix(lowerName, "_seconds"), strings.HasSuffix(lowerName, "_count"):
		return "Float64"
	default:
		return "String"
	}
}

func ensureFieldNames(fieldNames []string, extra ...string) []string {
	seen := make(map[string]struct{}, len(fieldNames)+len(extra))
	result := make([]string, 0, len(fieldNames)+len(extra))
	for _, name := range fieldNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	for _, name := range extra {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func orderFieldNames(source *models.Source, fieldNames []string) []string {
	ordered := ensureFieldNames(fieldNames)
	slices.Sort(ordered)

	if source == nil {
		return ordered
	}

	priority := []string{
		source.MetaTSField,
		"_time",
		source.MetaSeverityField,
		"_msg",
		"_stream_id",
		"_stream",
	}

	out := make([]string, 0, len(ordered))
	seen := make(map[string]struct{}, len(ordered))
	for _, name := range priority {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for _, candidate := range ordered {
			if candidate != name {
				continue
			}
			if _, ok := seen[candidate]; ok {
				break
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
			break
		}
	}
	for _, name := range ordered {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func defaultDiscoveryWindow() (start, end time.Time) {
	end = time.Now().UTC()
	start = end.Add(-defaultDiscoveryLookback)
	return start, end
}
