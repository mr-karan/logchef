package loki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	defaultQueryLimit = 1000
	// maxPointsPerSeries is the number of steps Loki's request parser
	// accepts in one range, for log queries too.
	maxPointsPerSeries = 11000
	// timeLayout writes every _time with nanoseconds, so rows sort as text.
	timeLayout = "2006-01-02T15:04:05.000000000Z07:00"
)

type queryRangeResponse struct {
	Data struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
		Stats      struct {
			Summary lokiSummary `json:"summary"`
		} `json:"stats"`
	} `json:"data"`
}

type lokiSummary struct {
	ExecTime            float64 `json:"execTime"`
	TotalLinesProcessed int64   `json:"totalLinesProcessed"`
	TotalBytesProcessed int64   `json:"totalBytesProcessed"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	// Values are [nanosecond timestamp, line] pairs.
	Values [][2]string `json:"values"`
}

type lokiSeries struct {
	Metric map[string]string `json:"metric"`
	// Values are [unix seconds as a JSON number, sample as a string] pairs.
	Values [][2]json.RawMessage `json:"values"`
}

// rangePage is one decoded query_range response.
type rangePage struct {
	resultType string
	streams    []lokiStream
	matrix     []lokiSeries
	summary    lokiSummary
	size       int
}

// logEntry is one flattened log line. labelKey is the stream's sorted label
// string, which orders ties and identifies the entry with ts and line.
type logEntry struct {
	ts       int64
	labels   map[string]string
	labelKey string
	line     string
}

func (e logEntry) key() string {
	return strconv.FormatInt(e.ts, 10) + "\x00" + e.labelKey + "\x00" + e.line
}

// newestFirst orders entries by timestamp, newest first, then by label
// string and line.
func newestFirst(a, b logEntry) int {
	switch {
	case a.ts != b.ts:
		if a.ts > b.ts {
			return -1
		}
		return 1
	case a.labelKey != b.labelKey:
		return strings.Compare(a.labelKey, b.labelKey)
	default:
		return strings.Compare(a.line, b.line)
	}
}

// rangeRequest is everything a query_range page needs except its end and limit.
type rangeRequest struct {
	query string
	start time.Time
	step  time.Duration
	// forward reads the oldest entries first instead of the newest.
	forward bool
}

func (p *Provider) fetchPage(ctx context.Context, conn models.LokiConnectionInfo, r rangeRequest, end int64, limit int, maxBytes int64) (*rangePage, error) {
	direction := "backward"
	if r.forward {
		direction = "forward"
	}
	params := lokiParams{
		"query":     {r.query},
		"start":     {nanos(r.start)},
		"end":       {strconv.FormatInt(end, 10)},
		"limit":     {strconv.Itoa(limit)},
		"direction": {direction},
		"step":      {strconv.FormatInt(int64(r.step/time.Second), 10)},
	}
	body, err := p.get(ctx, conn, "/loki/api/v1/query_range", params, maxBytes)
	if err != nil {
		return nil, err
	}
	var resp queryRangeResponse
	if err := decodeBody(body, &resp); err != nil {
		return nil, err
	}
	page := &rangePage{resultType: resp.Data.ResultType, summary: resp.Data.Stats.Summary, size: len(body)}
	switch page.resultType {
	case "streams":
		err = decodeBody(resp.Data.Result, &page.streams)
	case "matrix":
		err = decodeBody(resp.Data.Result, &page.matrix)
	default:
		return nil, &datasource.ValidationError{Field: "query", Message: fmt.Sprintf("Loki returned a %q result, which Logchef cannot show. Use a log query or a range metric query", page.resultType)}
	}
	if err != nil {
		return nil, err
	}
	return page, nil
}

// queryStep is the step sent with every range query. Loki rejects a range of
// more than 11,000 steps before it knows the query kind, so the step is at
// least range/11,000, rounded up to a whole second and never below one
// second. raised reports that a requested step was increased.
func queryStep(start, end time.Time, requested time.Duration) (step time.Duration, raised bool) {
	span := end.Sub(start)
	step = max(time.Second, requested, (span+maxPointsPerSeries-1)/maxPointsPerSeries)
	if rest := step % time.Second; rest != 0 {
		step += time.Second - rest
	}
	return step, requested > 0 && step > requested
}

// resolveQueryLimit applies the limit rule of the other providers: no limit
// means the default, and any limit is capped at the maximum.
func resolveQueryLimit(limit, defaultLimit, maxLimit int) (int, models.LimitSource) {
	if defaultLimit <= 0 {
		defaultLimit = defaultQueryLimit
	}
	if maxLimit <= 0 {
		maxLimit = defaultLimit
	}
	applied, source := limit, models.LimitSourceRequest
	if limit <= 0 {
		applied, source = defaultLimit, models.LimitSourceDefault
	}
	if applied > maxLimit {
		applied, source = maxLimit, models.LimitSourceMax
	}
	return applied, source
}

// QueryLogs runs native LogQL as written. Loki's result type decides the
// shape: a log result is paged by time, a metric result is flattened.
func (p *Provider) QueryLogs(ctx context.Context, source *models.Source, req datasource.QueryRequest) (*models.QueryResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.RawQuery)
	if query == "" {
		return nil, &datasource.ValidationError{Field: "query", Message: "Enter a LogQL query, such as " + conn.Selector}
	}
	if req.StartTime == nil || req.EndTime == nil {
		return nil, &datasource.ValidationError{Field: "start_time", Message: "A Loki query needs start_time and end_time"}
	}
	if !req.EndTime.After(*req.StartTime) {
		return nil, &datasource.ValidationError{Field: "end_time", Message: "end_time must be after start_time"}
	}
	// Loki has no per-request timeout, so the query timeout is the deadline.
	if req.QueryTimeout != nil && *req.QueryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*req.QueryTimeout)*time.Second)
		defer cancel()
	}

	limit, limitSource := resolveQueryLimit(req.Limit, req.DefaultLimit, req.MaxLimit)
	step, stepRaised := queryStep(*req.StartTime, *req.EndTime, req.Step)
	budget := int64(req.MaxResponseBytes)
	if budget <= 0 {
		budget = defaultMaxResponseBytes
	}
	pager := &logPager{
		fetch: func(ctx context.Context, end int64, pageLimit int, maxBytes int64) (*rangePage, error) {
			return p.fetchPage(ctx, conn, rangeRequest{query: query, start: *req.StartTime, step: step}, end, pageLimit, maxBytes)
		},
		end:      req.EndTime.UnixNano(),
		target:   limit + 1,
		pageSize: conn.EffectiveLimits().MaxEntriesPerQuery,
		budget:   budget,
	}
	if err := pager.run(ctx); err != nil {
		return nil, err
	}

	var result *models.QueryResult
	if pager.metric != nil {
		result = metricResult(pager.metric, limit)
		if stepRaised {
			result.Warnings = append(result.Warnings, models.QueryWarning{
				Code:    "STEP_RAISED",
				Message: fmt.Sprintf("Step raised to %d seconds to stay within Loki's limit of 11,000 points", int64(step/time.Second)),
			})
		}
	} else {
		result = logResult(pager, limit)
	}
	finishStats(result, pager, limit, limitSource, req.Limit)
	return result, nil
}

// logPager reads a log result page by page, newest first. Each page ends one
// nanosecond after the oldest entry of the page before it, so entries at that
// timestamp come back and are dropped by key.
type logPager struct {
	fetch    func(ctx context.Context, end int64, limit int, maxBytes int64) (*rangePage, error)
	end      int64
	target   int
	pageSize int
	budget   int64

	entries   []logEntry
	metric    *rangePage
	summary   lokiSummary
	bytesRead int64
	// truncated is "row_limit" or "byte_limit" when the pager stopped with
	// entries left unread.
	truncated string
	// tieWarning is set when more entries than a page share one timestamp.
	tieWarning bool
}

func (lp *logPager) run(ctx context.Context) error {
	b := pageBoundary{ts: -1, keys: map[string]struct{}{}}
	retried := false
	first := true
	for {
		if b.count >= lp.pageSize {
			lp.truncated, lp.tieWarning = "row_limit", true
			return nil
		}
		want := min(lp.target-len(lp.entries)+b.count, lp.pageSize)
		page, err := lp.fetch(ctx, lp.end, want, lp.budget-lp.bytesRead)
		if errors.Is(err, errBodyTooLarge) {
			if !first {
				lp.truncated = "byte_limit"
				return nil
			}
			if retried {
				return &datasource.ValidationError{Field: "query", Message: "The response exceeds the size limit. Narrow the query or the time range"}
			}
			retried = true
			lp.pageSize = max(1, want/10)
			continue
		}
		if err != nil {
			return err
		}
		lp.bytesRead += int64(page.size)
		lp.summary.ExecTime += page.summary.ExecTime
		lp.summary.TotalLinesProcessed += page.summary.TotalLinesProcessed
		lp.summary.TotalBytesProcessed += page.summary.TotalBytesProcessed
		if page.resultType == "matrix" {
			if !first {
				return &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Message: "Loki changed the result type between pages"}
			}
			lp.metric = page
			return nil
		}
		first = false

		entries, err := flattenStreams(page.streams)
		if err != nil {
			return err
		}
		added := 0
		for _, entry := range entries {
			if !b.seen(entry) {
				lp.entries = append(lp.entries, entry)
				added++
			}
		}
		if len(entries) < want || len(lp.entries) >= lp.target {
			return nil
		}
		// A full page of entries already seen cannot move the end back.
		if added == 0 {
			lp.truncated, lp.tieWarning = "row_limit", true
			return nil
		}

		b.advance(entries, lp.entries)
		lp.end = b.ts + 1
	}
}

// pageBoundary is the oldest timestamp read so far. The next page ends one
// nanosecond after it, so its entries come back and are dropped by key.
type pageBoundary struct {
	ts   int64
	keys map[string]struct{}
	// count is the number of collected entries at ts, repeats of one key
	// included: the next page returns every one of them again.
	count int
}

func (b *pageBoundary) seen(entry logEntry) bool {
	if entry.ts != b.ts {
		return false
	}
	_, ok := b.keys[entry.key()]
	return ok
}

// advance moves the boundary to the oldest entry of a full page. collected
// goes back in time, so its entries at the boundary end the slice.
func (b *pageBoundary) advance(page, collected []logEntry) {
	oldest := page[len(page)-1].ts
	if oldest != b.ts {
		b.ts = oldest
		clear(b.keys)
	}
	for _, entry := range page {
		if entry.ts == oldest {
			b.keys[entry.key()] = struct{}{}
		}
	}
	b.count = 0
	for i := len(collected) - 1; i >= 0 && collected[i].ts == oldest; i-- {
		b.count++
	}
}

// flattenStreams merges the streams of a page into entries, newest first.
func flattenStreams(streams []lokiStream) ([]logEntry, error) {
	var entries []logEntry
	for _, stream := range streams {
		labelKey := labelString(stream.Stream)
		for _, value := range stream.Values {
			ts, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				return nil, &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Message: "Loki sent a response that could not be read", Err: fmt.Errorf("entry timestamp %q: %w", value[0], err)}
			}
			entries = append(entries, logEntry{ts: ts, labels: stream.Stream, labelKey: labelKey, line: value[1]})
		}
	}
	slices.SortFunc(entries, newestFirst)
	return entries, nil
}

// labelString writes a label set as Loki does, with sorted names.
func labelString(labels map[string]string) string {
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	slices.Sort(names)
	var b strings.Builder
	b.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(name + "=" + strconv.Quote(labels[name]))
	}
	b.WriteByte('}')
	return b.String()
}

// logResult writes each entry as a row of _time, _msg and its labels.
func logResult(lp *logPager, limit int) *models.QueryResult {
	entries := lp.entries
	slices.SortFunc(entries, newestFirst)
	if len(entries) > limit {
		entries = entries[:limit]
		lp.truncated = "row_limit"
	}
	names := map[string]struct{}{}
	rows := make([]map[string]any, len(entries))
	for i, entry := range entries {
		for name := range entry.labels {
			names[name] = struct{}{}
		}
		rows[i] = entry.row()
	}
	columns := []models.ColumnInfo{{Name: "_time", Type: timeColumnType}, {Name: "_msg", Type: stringColumnType}}
	columns = append(columns, labelColumns(names, "_time", "_msg")...)
	result := &models.QueryResult{Logs: rows, Columns: columns}
	if lp.tieWarning {
		result.Warnings = append(result.Warnings, models.QueryWarning{
			Code:    "TIMESTAMP_TIE",
			Message: fmt.Sprintf("More than %d entries share one timestamp. Narrow the query.", lp.pageSize),
		})
	}
	return result
}

// row writes the entry as _time, _msg and its labels. A label named _time
// or _msg loses to the column.
func (e logEntry) row() map[string]any {
	row := make(map[string]any, len(e.labels)+2)
	for name, value := range e.labels {
		row[name] = value
	}
	row["_time"] = time.Unix(0, e.ts).UTC().Format(timeLayout)
	row["_msg"] = e.line
	return row
}

// metricResult writes each sample as a row of value, time and the series
// labels. value comes first, because alert evaluation reads the first column.
func metricResult(page *rangePage, limit int) *models.QueryResult {
	series := slices.Clone(page.matrix)
	slices.SortFunc(series, func(a, b lokiSeries) int { return strings.Compare(labelString(a.Metric), labelString(b.Metric)) })
	names := map[string]struct{}{}
	var rows []map[string]any
	truncated := false
	for _, s := range series {
		for _, sample := range s.Values {
			if len(rows) == limit {
				truncated = true
				break
			}
			row := make(map[string]any, len(s.Metric)+2)
			for name, value := range s.Metric {
				row[name] = value
				names[name] = struct{}{}
			}
			row["time"] = sampleTime(sample[0])
			row["value"] = sampleValue(sample[1])
			rows = append(rows, row)
		}
	}
	columns := []models.ColumnInfo{{Name: "value", Type: stringColumnType}, {Name: "time", Type: timeColumnType}}
	columns = append(columns, labelColumns(names, "value", "time")...)
	result := &models.QueryResult{Logs: rows, Columns: columns}
	if rows == nil {
		result.Logs = []map[string]any{}
	}
	if truncated {
		result.Stats.TruncatedReason = "row_limit"
	}
	return result
}

// sampleTime converts Loki's unix seconds, such as 1794811158.985, to RFC
// 3339 in UTC without going through a float.
func sampleTime(raw json.RawMessage) string {
	text := string(raw)
	secText, fracText, _ := strings.Cut(text, ".")
	sec, err := strconv.ParseInt(secText, 10, 64)
	if err != nil {
		return text
	}
	fracText = (fracText + "000000000")[:9]
	nsec, err := strconv.ParseInt(fracText, 10, 64)
	if err != nil {
		return text
	}
	return time.Unix(sec, nsec).UTC().Format(time.RFC3339Nano)
}

func sampleValue(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}

func labelColumns(names map[string]struct{}, reserved ...string) []models.ColumnInfo {
	sorted := make([]string, 0, len(names))
	for name := range names {
		if !slices.Contains(reserved, name) {
			sorted = append(sorted, name)
		}
	}
	slices.Sort(sorted)
	columns := make([]models.ColumnInfo, len(sorted))
	for i, name := range sorted {
		columns[i] = models.ColumnInfo{Name: name, Type: stringColumnType}
	}
	return columns
}

func finishStats(result *models.QueryResult, lp *logPager, limit int, limitSource models.LimitSource, requested int) {
	reason := lp.truncated
	if result.Stats.TruncatedReason != "" {
		reason = result.Stats.TruncatedReason
	}
	result.Stats = models.QueryStats{
		ExecutionTimeMs: lp.summary.ExecTime * 1000,
		RowsRead:        int(lp.summary.TotalLinesProcessed),
		BytesRead:       int(lp.summary.TotalBytesProcessed),
		RowsReturned:    len(result.Logs),
		BytesReturned:   int(lp.bytesRead),
		LimitApplied:    limit,
		LimitSource:     limitSource,
		Truncation:      models.TruncationNone,
	}
	switch reason {
	case "row_limit":
		result.Stats.Truncation = models.TruncationRows
	case "byte_limit":
		result.Stats.Truncation = models.TruncationBytes
	}
	if reason != "" {
		result.Stats.Truncated = true
		result.Stats.TruncatedReason = reason
	}
	if requested <= 0 {
		result.Warnings = append(result.Warnings, models.QueryWarning{
			Code:    "LIMIT_APPLIED",
			Message: fmt.Sprintf("Showing first %d rows. Use download for larger results.", limit),
		})
	}
	if limitSource == models.LimitSourceMax {
		result.Warnings = append(result.Warnings, models.QueryWarning{
			Code:    "LIMIT_CAPPED",
			Message: fmt.Sprintf("Result limit capped at %d rows.", limit),
		})
	}
}
