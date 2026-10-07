package victorialogs

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func (p *Provider) StreamFields(ctx context.Context, source *models.Source) ([]string, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	if conn.Optimizer == nil || !conn.Optimizer.Enabled {
		return nil, datasource.ErrOperationNotSupported
	}
	if len(conn.Optimizer.StreamFields) > 0 {
		return conn.Optimizer.StreamFields, nil
	}
	settings := conn.Optimizer.WithDefaults()
	limiter := p.windowLimiter(source.ID, settings.Concurrency)
	if err := limiter.acquire(ctx); err != nil {
		return nil, err
	}
	defer limiter.release()
	end := time.Now().UTC()
	form := url.Values{"query": {"*"}, "start": {formatAPITime(end.Add(-time.Duration(settings.SidebarLookbackSeconds) * time.Second))}, "end": {formatAPITime(end)}}
	applyScopeFilters(form, conn)
	var result valuesResponse
	if err := p.decodeJSONRequest(ctx, conn, "/select/logsql/stream_field_names", form, &result); err != nil {
		return nil, err
	}
	fields := make([]string, 0, len(result.Values))
	for _, value := range result.Values {
		fields = append(fields, value.Value)
	}
	slices.Sort(fields)
	return fields, nil
}

func (p *Provider) optimizedFieldValues(ctx context.Context, source *models.Source, conn models.VictoriaLogsConnectionInfo, req datasource.FieldValuesRequest, query string) (*datasource.FieldValuesResult, error) {
	settings := conn.Optimizer.WithDefaults()
	limiter := p.windowLimiter(source.ID, settings.Concurrency)
	if err := limiter.acquire(ctx); err != nil {
		return nil, err
	}
	defer limiter.release()
	start := req.EndTime.Add(-time.Duration(settings.SidebarLookbackSeconds) * time.Second)
	if start.Before(req.StartTime) {
		start = req.StartTime
	}
	// Match discovery's ignore_pipes behavior. Grouping returns real frequencies,
	// unlike field_values/stream_field_values when their limit is exceeded.
	query = strings.TrimSpace(splitTopLevelPipes(query)[0])
	field := strconv.Quote(req.FieldName)
	hitsField := "_logchef_hits"
	if req.FieldName == hitsField {
		hitsField += "_"
	}
	form := url.Values{"start": {formatAPITime(start)}, "end": {formatAPITime(req.EndTime)}}
	applyScopeFilters(form, conn)
	if timeout := formatTimeout(req.Timeout); timeout != "" {
		form.Set("timeout", timeout)
	}
	form.Set("query", fmt.Sprintf("%s | stats by (%s) count() as %s | sort by (%s desc) limit %d", query, field, hitsField, hitsField, settings.SidebarValuesCap))
	resp, err := p.doFormRequest(ctx, conn, "/select/logsql/query", form)
	if err != nil {
		return nil, err
	}
	rows, _, _, truncated, readErr := readQueryRows(resp.Body, models.MaxHistogramResponseBytes, settings.SidebarValuesCap)
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if truncated != "" {
		return nil, fmt.Errorf("field values exceed the response byte limit")
	}
	values := make([]datasource.FieldValueInfo, 0, len(rows))
	for _, row := range rows {
		value, ok := row[req.FieldName].(string)
		if !ok {
			return nil, fmt.Errorf("field value is missing")
		}
		hits, err := strconv.ParseInt(fmt.Sprint(row[hitsField]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid field hit count: %w", err)
		}
		values = append(values, datasource.FieldValueInfo{Value: value, Count: hits})
	}
	total := int64(len(values))
	if len(values) == settings.SidebarValuesCap {
		form.Set("query", fmt.Sprintf("%s | stats count_uniq(%s) as total", query, field))
		resp, err := p.doFormRequest(ctx, conn, "/select/logsql/query", form)
		if err != nil {
			return nil, err
		}
		rows, _, _, truncated, err := readQueryRows(resp.Body, 4096, 1)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if truncated != "" || len(rows) != 1 {
			return nil, fmt.Errorf("invalid distinct count response")
		}
		total, err = strconv.ParseInt(fmt.Sprint(rows[0]["total"]), 10, 64)
		if err != nil {
			return nil, err
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	values = values[:min(limit, len(values))]
	fieldType := req.FieldType
	if fieldType == "" {
		fieldType = inferColumnType(source, req.FieldName)
	}
	return &datasource.FieldValuesResult{FieldName: req.FieldName, FieldType: fieldType, Values: values, TotalDistinct: total}, nil
}
