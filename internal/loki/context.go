package loki

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// contextWindow is how far before and after the target row context reads.
const contextWindow = time.Hour

const notOneStream = "This row does not identify one stream"

// GetLogContext returns the lines around a row in the row's own stream
// (RFC 0011 "Log context"). The request's stream labels must be the complete
// label set of one stream, which /series proves, because a native pipeline
// or a stale schema can leave a row with too few or rewritten labels. Before
// logs end at the target and include it unless ExcludeBoundary is set; after
// logs start just past it, as on ClickHouse.
func (p *Provider) GetLogContext(ctx context.Context, source *models.Source, req datasource.LogContextRequest) (*models.LogContextResponse, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	if err := checkContextRequest(conn, req); err != nil {
		return nil, err
	}
	if req.QueryTimeout != nil && *req.QueryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*req.QueryTimeout)*time.Second)
		defer cancel()
	}
	target := time.UnixMilli(req.TargetTimestamp).UTC()
	if req.TargetTime != nil {
		target = req.TargetTime.UTC()
	}
	from, to := target.Add(-contextWindow), target.Add(contextWindow)

	selector, err := p.streamSelector(ctx, conn, req.StreamLabels, from, to)
	if err != nil {
		return nil, err
	}
	query := selector
	if conn.Parser != "" {
		query += " | " + conn.Parser + " | drop __error__, __error_details__"
	}
	step, _ := queryStep(from, to, 0)

	beforeEnd := target.UnixNano() + 1
	if req.ExcludeBoundary {
		beforeEnd = target.UnixNano()
	}
	var stats models.QueryStats
	before, err := p.contextEntries(ctx, conn, rangeRequest{query: query, start: from, step: step}, beforeEnd, req.BeforeOffset, req.BeforeLimit, &stats)
	if err != nil {
		return nil, err
	}
	slices.Reverse(before)
	after, err := p.contextEntries(ctx, conn, rangeRequest{query: query, start: target.Add(time.Nanosecond), step: step, forward: true}, to.UnixNano()+1, req.AfterOffset, req.AfterLimit, &stats)
	if err != nil {
		return nil, err
	}

	resp := &models.LogContextResponse{
		TargetTimestamp: target.UnixMilli(),
		BeforeLogs:      rows(before),
		TargetLogs:      []map[string]any{},
		AfterLogs:       rows(after),
	}
	stats.RowsReturned = len(resp.BeforeLogs) + len(resp.AfterLogs)
	resp.Stats = stats
	return resp, nil
}

func checkContextRequest(conn models.LokiConnectionInfo, req datasource.LogContextRequest) error {
	if len(req.StreamLabels) == 0 {
		return &datasource.ValidationError{Field: "stream_labels", Message: "Context on Loki needs the row's stream labels"}
	}
	for name := range req.StreamLabels {
		if !labelNamePattern.MatchString(name) {
			return &datasource.ValidationError{Field: "stream_labels", Message: fmt.Sprintf("%q is not a valid Loki label name", name)}
		}
	}
	if req.BeforeLimit <= 0 || req.AfterLimit <= 0 || req.BeforeOffset < 0 || req.AfterOffset < 0 {
		return &datasource.ValidationError{Field: "before_limit", Message: "context limits must be positive and offsets must not be negative"}
	}
	// Loki has no offset, so a read asks for offset + limit entries.
	maxEntries := conn.EffectiveLimits().MaxEntriesPerQuery
	if req.BeforeOffset+req.BeforeLimit > maxEntries || req.AfterOffset+req.AfterLimit > maxEntries {
		return &datasource.ValidationError{Field: "before_offset", Message: fmt.Sprintf("Context on this source reads at most %d lines in each direction", maxEntries)}
	}
	return nil
}

// streamSelector proves that labels is the complete label set of one stream
// in [from, to], and returns a selector for exactly that stream: every label
// as an exact matcher, and name="" for every other label that a stream with
// these labels carries, so a stream with one more label does not match.
func (p *Provider) streamSelector(ctx context.Context, conn models.LokiConnectionInfo, labels map[string]string, from, to time.Time) (string, error) {
	params := lokiParams{
		"match[]": {exactSelector(labels, nil)},
		"start":   {nanos(from)},
		"end":     {nanos(to)},
	}
	body, err := p.get(ctx, conn, "/loki/api/v1/series", params, metadataMaxBytes)
	if err != nil {
		return "", err
	}
	var resp struct {
		Data []map[string]string `json:"data"`
	}
	if err := decodeBody(body, &resp); err != nil {
		return "", err
	}
	var verified map[string]string
	others := map[string]struct{}{}
	for _, series := range resp.Data {
		if maps.Equal(series, labels) {
			verified = series
		}
		for name := range series {
			if _, ok := labels[name]; !ok {
				others[name] = struct{}{}
			}
		}
	}
	if verified == nil {
		return "", &datasource.ValidationError{Field: "stream_labels", Message: notOneStream}
	}
	return exactSelector(verified, others), nil
}

// exactSelector matches every label exactly and every name in absent as
// unset.
func exactSelector(labels map[string]string, absent map[string]struct{}) string {
	matchers := make([]string, 0, len(labels)+len(absent))
	for _, name := range slices.Sorted(maps.Keys(labels)) {
		matchers = append(matchers, name+"="+strconv.Quote(labels[name]))
	}
	for _, name := range slices.Sorted(maps.Keys(absent)) {
		matchers = append(matchers, name+`=""`)
	}
	return "{" + strings.Join(matchers, ", ") + "}"
}

// contextEntries reads offset + limit entries from the start of r in its
// direction, up to end, and drops the first offset. The result is ordered in
// the read's direction.
func (p *Provider) contextEntries(ctx context.Context, conn models.LokiConnectionInfo, r rangeRequest, end int64, offset, limit int, stats *models.QueryStats) ([]logEntry, error) {
	page, err := p.fetchPage(ctx, conn, r, end, offset+limit, defaultMaxResponseBytes)
	if errors.Is(err, errBodyTooLarge) {
		return nil, &datasource.ValidationError{Field: "before_limit", Message: "The context lines exceed the size limit. Load fewer lines"}
	}
	if err != nil {
		return nil, err
	}
	stats.ExecutionTimeMs += page.summary.ExecTime * 1000
	stats.RowsRead += int(page.summary.TotalLinesProcessed)
	stats.BytesRead += int(page.summary.TotalBytesProcessed)
	entries, err := flattenStreams(page.streams)
	if err != nil {
		return nil, err
	}
	if r.forward {
		slices.Reverse(entries)
	}
	if offset >= len(entries) {
		return nil, nil
	}
	return entries[offset:min(len(entries), offset+limit)], nil
}

func rows(entries []logEntry) []map[string]any {
	out := make([]map[string]any, len(entries))
	for i, entry := range entries {
		out[i] = entry.row()
	}
	return out
}
