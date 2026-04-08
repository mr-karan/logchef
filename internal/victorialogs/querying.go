package victorialogs

import (
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
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	defaultDiscoveryLookback = 24 * time.Hour
	defaultQueryLimit        = 1000
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

	form := url.Values{}
	form.Set("query", query)

	limit := effectiveQueryLimit(req.Limit, req.MaxLimit)
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

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	logs := make([]map[string]interface{}, 0, limit)
	columnSet := make(map[string]struct{})
	columnNames := make([]string, 0)

	for {
		var row map[string]interface{}
		if err := decoder.Decode(&row); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode victorialogs query response: %w", err)
		}

		logs = append(logs, row)
		for key := range row {
			if _, ok := columnSet[key]; ok {
				continue
			}
			columnSet[key] = struct{}{}
			columnNames = append(columnNames, key)
		}
	}

	columns := make([]models.ColumnInfo, 0, len(columnNames))
	for _, name := range orderFieldNames(source, columnNames) {
		columns = append(columns, models.ColumnInfo{
			Name: name,
			Type: inferColumnType(source, name),
		})
	}

	return &models.QueryResult{
		Logs:    logs,
		Columns: columns,
		Stats:   statsFromHeaders(resp, len(logs)),
	}, nil
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
	if groupBy := strings.TrimSpace(req.GroupBy); groupBy != "" {
		form.Add("field", groupBy)
	}
	applyScopeFilters(form, conn)

	var result hitsResponse
	if err := p.decodeJSONRequest(ctx, conn, "/select/logsql/hits", form, &result); err != nil {
		return nil, err
	}

	data := make([]datasource.HistogramBucket, 0)
	for _, series := range result.Hits {
		groupValue := ""
		if strings.TrimSpace(req.GroupBy) != "" {
			groupValue = series.Fields[strings.TrimSpace(req.GroupBy)]
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
			})
		}
	}

	return &datasource.HistogramResult{
		Granularity: defaultWindow(req.Window),
		Data:        data,
	}, nil
}

func (p *Provider) LogContext(context.Context, *models.Source, datasource.LogContextRequest) (*datasource.LogContextResult, error) {
	return nil, fmt.Errorf("victorialogs log context requires stream-aware row locators: %w", datasource.ErrOperationNotSupported)
}

func (p *Provider) GetFieldValues(ctx context.Context, source *models.Source, req datasource.FieldValuesRequest) (*datasource.FieldValuesResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}

	query := strings.TrimSpace(req.QueryText)
	if query == "" {
		query = "*"
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

	query := strings.TrimSpace(req.QueryText)
	if query == "" {
		query = "*"
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

func (p *Provider) GetSourceStats(ctx context.Context, source *models.Source) (*datasource.SourceStats, error) {
	columns, err := p.GetSourceSchema(ctx, source)
	if err != nil {
		return nil, err
	}

	return &datasource.SourceStats{
		TableInfo: &datasource.SourceTableInfo{
			Name:    source.Name,
			Engine:  "VictoriaLogs",
			Columns: columns,
		},
	}, nil
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
	if strings.ToLower(result.Status) != "success" {
		return nil, fmt.Errorf("victorialogs stats_query returned status %q", result.Status)
	}

	rows := make([]map[string]interface{}, 0, len(result.Data.Result))
	for _, item := range result.Data.Result {
		row := map[string]interface{}{}
		for key, value := range item.Metric {
			row[key] = value
		}
		if len(item.Value) >= 2 {
			row["value"] = item.Value[1]
		}
		rows = append(rows, row)
	}

	return &models.QueryResult{
		Logs: rows,
		Columns: []models.ColumnInfo{
			{Name: "value", Type: "Float64"},
		},
		Stats: models.QueryStats{
			ExecutionTimeMs: 0,
			RowsRead:        len(rows),
		},
	}, nil
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
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
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

	return reference.In(loc).Format("-07:00")
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

func effectiveQueryLimit(limit, maxLimit int) int {
	if maxLimit <= 0 {
		maxLimit = defaultQueryLimit
	}
	if limit <= 0 {
		return maxLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

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

func defaultDiscoveryWindow() (time.Time, time.Time) {
	end := time.Now().UTC()
	start := end.Add(-defaultDiscoveryLookback)
	return start, end
}
