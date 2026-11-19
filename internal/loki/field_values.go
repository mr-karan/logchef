package loki

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// The sidebar preload reads at most preloadLabelLimit stream labels with
// preloadConcurrency requests in flight (RFC 0011 "Field discovery").
const (
	preloadLabelLimit  = 30
	preloadConcurrency = 4
)

// GetFieldValues counts the values of one field over the logs of the request's
// query with one instant query at the range end. It works for stream labels,
// structured metadata and parsed fields alike. The range is capped at the
// source's sidebar lookback.
func (p *Provider) GetFieldValues(ctx context.Context, source *models.Source, req datasource.FieldValuesRequest) (*datasource.FieldValuesResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	field := req.FieldName
	if !labelNamePattern.MatchString(field) || field == "_time" || field == "_msg" {
		return nil, &datasource.ValidationError{Field: "field", Message: fmt.Sprintf("Loki has no values for the field %q", field)}
	}
	if req.Limit <= 0 {
		return nil, &datasource.ValidationError{Field: "limit", Message: "the field values limit must be positive"}
	}
	start, end, err := sidebarWindow(conn, req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	labels, err := p.streamLabels(ctx, conn, conn.Selector, start, end)
	if err != nil {
		return nil, err
	}
	logQuery, err := p.filterQuery(ctx, source, req.Language, req.QueryText, labels)
	if err != nil {
		return nil, err
	}

	// The label filter drops entries without the field. The newline ends a
	// trailing # comment of a native query.
	expr := fmt.Sprintf("topk(%d, sum by (%s) (count_over_time(%s\n| %s!=\"\"\n[%dms])))",
		req.Limit, field, logQuery, field, end.Sub(start).Milliseconds())
	samples, err := p.instantVector(ctx, conn, expr, end)
	if err != nil {
		return nil, err
	}
	values := make([]datasource.FieldValueInfo, 0, len(samples))
	for _, sample := range samples {
		count, err := strconv.ParseFloat(sampleValue(sample.Value[1]), 64)
		if err != nil {
			return nil, &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Message: "Loki sent a field value count that could not be read", Err: err}
		}
		values = append(values, datasource.FieldValueInfo{Value: sample.Metric[field], Count: int64(count)})
	}
	slices.SortFunc(values, func(a, b datasource.FieldValueInfo) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Value, b.Value))
	})

	isLabel := slices.Contains(labels, field)
	fieldType := req.FieldType
	if isLabel {
		fieldType = labelColumnType
	}
	return &datasource.FieldValuesResult{
		FieldName:        field,
		FieldType:        fieldType,
		IsLowCardinality: isLabel,
		Values:           values,
		TotalDistinct:    int64(len(values)),
	}, nil
}

// GetAllFieldValues serves the sidebar preload. It reads the values of stream
// labels only, from the index, under the stream selector of the request's
// query. Loki's label values carry no counts, so Count stays zero.
func (p *Provider) GetAllFieldValues(ctx context.Context, source *models.Source, req datasource.AllFieldValuesRequest) (datasource.AllFieldValuesResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	if req.Limit <= 0 {
		return nil, &datasource.ValidationError{Field: "limit", Message: "the field values limit must be positive"}
	}
	start, end, err := sidebarWindow(conn, req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	selector, err := p.hoistedSelector(ctx, source, conn, req.Language, req.QueryText, start, end)
	if err != nil {
		return nil, err
	}
	labels, err := p.streamLabels(ctx, conn, selector, start, end)
	if err != nil {
		return nil, err
	}
	labels = labels[:min(len(labels), preloadLabelLimit)]

	results := make([]*datasource.FieldValuesResult, len(labels))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(preloadConcurrency)
	for i, label := range labels {
		group.Go(func() error {
			values, err := p.labelValues(groupCtx, conn, label, selector, start, end)
			if err != nil {
				return err
			}
			kept := make([]datasource.FieldValueInfo, 0, min(len(values), req.Limit))
			for _, value := range values[:min(len(values), req.Limit)] {
				kept = append(kept, datasource.FieldValueInfo{Value: value})
			}
			results[i] = &datasource.FieldValuesResult{
				FieldName:        label,
				FieldType:        labelColumnType,
				IsLowCardinality: true,
				Values:           kept,
				TotalDistinct:    int64(len(values)),
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	all := make(datasource.AllFieldValuesResult, len(results))
	for _, result := range results {
		all[result.FieldName] = result
	}
	return all, nil
}

// sidebarWindow caps [start, end] at the source's sidebar lookback, keeping
// the end.
func sidebarWindow(conn models.LokiConnectionInfo, start, end time.Time) (windowStart, windowEnd time.Time, err error) {
	if !end.After(start) {
		return time.Time{}, time.Time{}, &datasource.ValidationError{Field: "start_time", Message: "the start time must be before the end time"}
	}
	lookback := time.Duration(conn.EffectiveLimits().SidebarLookbackSeconds) * time.Second
	// LogQL ranges have millisecond precision.
	span := max(min(end.Sub(start), lookback).Truncate(time.Millisecond), time.Millisecond)
	start = end.Add(-span)
	return start.UTC(), end.UTC(), nil
}

// filterQuery returns the LogQL log query whose entries a field's values
// count. An empty query is the source's selector with its parser.
func (p *Provider) filterQuery(ctx context.Context, source *models.Source, language models.QueryLanguage, queryText string, labels []string) (string, error) {
	switch models.NormalizeQueryLanguage(language) {
	case models.QueryLanguageLogQL, "":
		if query := strings.TrimSpace(queryText); query != "" {
			return query, nil
		}
	case models.QueryLanguageLogchefQL:
	default:
		return "", &datasource.ValidationError{Field: "query_language", Message: fmt.Sprintf("Loki sources do not support the query language %q", language)}
	}
	return p.compileWithLabels(ctx, source, queryText, labels)
}

// compileWithLabels compiles LogchefQL with the given stream labels, so a
// condition on one is part of the stream selector.
func (p *Provider) compileWithLabels(ctx context.Context, source *models.Source, queryText string, labels []string) (string, error) {
	withLabels := *source
	withLabels.Columns = make([]models.ColumnInfo, len(labels))
	for i, label := range labels {
		withLabels.Columns[i] = models.ColumnInfo{Name: label, Type: labelColumnType}
	}
	compiled, err := p.CompileLogchefQL(ctx, &withLabels, datasource.LogchefQLCompileRequest{Query: queryText})
	if err != nil {
		return "", err
	}
	return compiled.Query, nil
}

// hoistedSelector returns the stream selector that starts the request's query.
// A native query that does not start with one, such as a metric query, falls
// back to the source's selector, because the preload is an index read that
// cannot apply a pipeline.
func (p *Provider) hoistedSelector(ctx context.Context, source *models.Source, conn models.LokiConnectionInfo, language models.QueryLanguage, queryText string, start, end time.Time) (string, error) {
	if strings.TrimSpace(queryText) == "" {
		return conn.Selector, nil
	}
	query := queryText
	switch models.NormalizeQueryLanguage(language) {
	case models.QueryLanguageLogQL, "":
	case models.QueryLanguageLogchefQL:
		labels, err := p.streamLabels(ctx, conn, conn.Selector, start, end)
		if err != nil {
			return "", err
		}
		if query, err = p.compileWithLabels(ctx, source, queryText, labels); err != nil {
			return "", err
		}
	default:
		return "", &datasource.ValidationError{Field: "query_language", Message: fmt.Sprintf("Loki sources do not support the query language %q", language)}
	}
	if selector := leadingSelector(query); selector != "" {
		return selector, nil
	}
	return conn.Selector, nil
}

// leadingSelector returns the stream selector that starts a LogQL log query,
// or "" when the query does not start with one.
func leadingSelector(query string) string {
	q := strings.TrimSpace(query)
	if !strings.HasPrefix(q, "{") {
		return ""
	}
	for i := 1; i < len(q); i++ {
		switch q[i] {
		case '"':
			for i++; i < len(q) && q[i] != '"'; i++ {
				if q[i] == '\\' {
					i++
				}
			}
		case '`':
			end := strings.IndexByte(q[i+1:], '`')
			if end < 0 {
				return ""
			}
			i += end + 1
		case '}':
			return q[:i+1]
		}
	}
	return ""
}

// streamLabels lists the stream label names of the streams that selector
// matches in [start, end], sorted.
func (p *Provider) streamLabels(ctx context.Context, conn models.LokiConnectionInfo, selector string, start, end time.Time) ([]string, error) {
	params := lokiParams{"query": {selector}, "start": {nanos(start)}, "end": {nanos(end)}}
	body, err := p.get(ctx, conn, "/loki/api/v1/labels", params, metadataMaxBytes)
	if err != nil {
		return nil, err
	}
	var labels labelsResponse
	if err := decodeBody(body, &labels); err != nil {
		return nil, err
	}
	slices.Sort(labels.Data)
	return labels.Data, nil
}

func (p *Provider) labelValues(ctx context.Context, conn models.LokiConnectionInfo, label, selector string, start, end time.Time) ([]string, error) {
	// The name becomes part of the request path.
	if !labelNamePattern.MatchString(label) {
		return nil, &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Message: fmt.Sprintf("Loki listed an invalid label name %q", label)}
	}
	params := lokiParams{"query": {selector}, "start": {nanos(start)}, "end": {nanos(end)}}
	body, err := p.get(ctx, conn, "/loki/api/v1/label/"+label+"/values", params, metadataMaxBytes)
	if err != nil {
		return nil, err
	}
	var values labelsResponse
	if err := decodeBody(body, &values); err != nil {
		return nil, err
	}
	slices.Sort(values.Data)
	return values.Data, nil
}

type vectorSample struct {
	Metric map[string]string  `json:"metric"`
	Value  [2]json.RawMessage `json:"value"`
}

// instantVector runs a metric query at one time. A LogQL parse error means the
// filter is not a log query, the only kind count_over_time takes.
func (p *Provider) instantVector(ctx context.Context, conn models.LokiConnectionInfo, expr string, at time.Time) ([]vectorSample, error) {
	params := lokiParams{"query": {expr}, "time": {nanos(at)}}
	body, err := p.get(ctx, conn, "/loki/api/v1/query", params, metadataMaxBytes)
	if validation, ok := errors.AsType[*datasource.ValidationError](err); ok && strings.HasPrefix(validation.Message, "parse error") {
		return nil, &datasource.ValidationError{Field: "query", Message: "Field values need a LogQL log query: " + validation.Message, Err: validation}
	}
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			ResultType string         `json:"resultType"`
			Result     []vectorSample `json:"result"`
		} `json:"data"`
	}
	if err := decodeBody(body, &resp); err != nil {
		return nil, err
	}
	if resp.Data.ResultType != "vector" {
		return nil, &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Message: fmt.Sprintf("Loki returned a %q result where a \"vector\" was expected", resp.Data.ResultType)}
	}
	return resp.Data.Result, nil
}
