package loki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	defaultHistogramWindow = "1m"
	// histogramGroupLimit matches the other providers' top-10 group cap.
	histogramGroupLimit = 10
	// histogramOtherGroup is the group value of the remainder, as on ClickHouse.
	histogramOtherGroup = "Other"
)

var labelNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// histogramQuery is one Loki request of a histogram plan. A range query
// returns a point at each multiple of the window from start to end, and the
// point at t counts (t - window, t]. An instant query counts (at - width, at].
type histogramQuery struct {
	instant bool
	// at is the evaluation time of an instant query, in Unix milliseconds.
	at int64
	// start and end bound a range query, in Unix milliseconds.
	start, end int64
	// width is the range of count_over_time, in milliseconds.
	width int64
	// bucket labels the result of an instant query.
	bucket int64
}

// planHistogram picks the requests for the buckets of [start, end), all in Unix
// milliseconds. Bucket boundaries are multiples of window since the epoch, and
// every time sent to Loki is already one, or is the request's own start or
// end, so Loki's splitter does not move it (RFC 0011 "Histogram").
func planHistogram(start, end, window int64) []histogramQuery {
	if end <= start {
		return nil
	}
	floor := func(t int64) int64 { return t - mod(t, window) }
	if floor(start) == floor(end-1) {
		return []histogramQuery{{instant: true, at: end, width: end - start, bucket: floor(start)}}
	}
	first := floor(start)
	if first < start {
		first += window
	}
	last := floor(end)

	var plan []histogramQuery
	if first > start {
		plan = append(plan, histogramQuery{instant: true, at: first, width: first - start, bucket: first - window})
	}
	if first < last {
		plan = append(plan, histogramQuery{start: first + window, end: last, width: window})
	}
	if last < end {
		plan = append(plan, histogramQuery{instant: true, at: end, width: end - last, bucket: last})
	}
	return plan
}

// mod is the non-negative remainder, so times before the epoch floor down.
func mod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// Histogram counts the entries of a LogQL log query per bucket with
// count_over_time. Every window is open at its start and closed at its end, so
// an entry exactly on a boundary falls in the bucket before it.
func (p *Provider) Histogram(ctx context.Context, source *models.Source, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return nil, err
	}
	spec, err := readHistogramRequest(req)
	if err != nil {
		return nil, err
	}
	start, end := req.StartTime.UnixMilli(), req.EndTime.UnixMilli()
	if buckets := (end-start)/spec.widthMs + 2; buckets > models.MaxHistogramBuckets {
		return nil, models.ErrHistogramBudgetExceeded
	}
	if req.QueryTimeout != nil && *req.QueryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*req.QueryTimeout)*time.Second)
		defer cancel()
	}
	h := histogramRun{p: p, conn: conn, plan: planHistogram(start, end, spec.widthMs)}

	totals, err := h.count(ctx, spec.query, "")
	if err != nil {
		return nil, err
	}
	result := &datasource.HistogramResult{Granularity: spec.window, Data: []datasource.HistogramBucket{}}
	if spec.groupBy == "" {
		for _, bucket := range sortedBuckets(totals) {
			result.Data = append(result.Data, datasource.HistogramBucket{Bucket: time.UnixMilli(bucket).UTC(), LogCount: totals[bucket][""]})
		}
		return result, nil
	}

	groups, err := h.topGroups(ctx, spec.query, spec.groupBy, start, end)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return result, nil
	}
	counts, err := h.count(ctx, spec.query+"\n| "+spec.groupBy+`=~`+strconv.Quote(groupRegex(groups)), spec.groupBy)
	if err != nil {
		return nil, err
	}
	data, other := groupedBuckets(totals, counts, groups)
	result.Data = data
	if other {
		result.Notice = fmt.Sprintf("Showing top %d series by %q; the rest are aggregated into an \"Other\" bucket.", histogramGroupLimit, spec.groupBy)
	}
	return result, nil
}

type histogramSpec struct {
	window  string
	widthMs int64
	groupBy string
	query   string
}

func readHistogramRequest(req datasource.HistogramRequest) (histogramSpec, error) {
	if len(req.ExtraStreamFilters) > 0 {
		return histogramSpec{}, &datasource.ValidationError{Field: "extra_stream_filters", Message: "invalid for this source: only VictoriaLogs sources support stream filters"}
	}
	if req.StartTime == nil || req.EndTime == nil {
		return histogramSpec{}, &datasource.ValidationError{Field: "start_time", Message: "the histogram of a Loki source needs a start and an end time"}
	}
	spec := histogramSpec{window: strings.TrimSpace(req.Window), groupBy: strings.TrimSpace(req.GroupBy), query: strings.TrimSpace(req.Query)}
	if spec.window == "" {
		spec.window = defaultHistogramWindow
	}
	width, err := parseHistogramWindow(spec.window)
	if err != nil {
		return histogramSpec{}, &datasource.ValidationError{Field: "window", Message: "invalid histogram window: use a whole number of milliseconds or more, such as 30s, 5m, 1h or 1d"}
	}
	spec.widthMs = width.Milliseconds()
	if spec.groupBy != "" && !labelNamePattern.MatchString(spec.groupBy) {
		return histogramSpec{}, &datasource.ValidationError{Field: "group_by", Message: fmt.Sprintf("%q is not a valid Loki label name", spec.groupBy)}
	}
	if spec.query == "" {
		return histogramSpec{}, &datasource.ValidationError{Field: "query", Message: "The histogram needs a LogQL log query"}
	}
	return spec, nil
}

// groupedBuckets lists each bucket's top groups, then Other: the bucket's
// total minus its top groups. other reports whether any Other count exists.
func groupedBuckets(totals, counts bucketCounts, groups []string) (data []datasource.HistogramBucket, other bool) {
	data = []datasource.HistogramBucket{}
	for _, bucket := range sortedBuckets(totals) {
		at := time.UnixMilli(bucket).UTC()
		grouped := 0
		for _, group := range groups {
			if n := counts[bucket][group]; n > 0 {
				grouped += n
				data = append(data, datasource.HistogramBucket{Bucket: at, LogCount: n, GroupValue: group})
			}
		}
		if rest := totals[bucket][""] - grouped; rest > 0 {
			other = true
			data = append(data, datasource.HistogramBucket{Bucket: at, LogCount: rest, GroupValue: histogramOtherGroup, IsOther: true})
		}
	}
	return data, other
}

// parseHistogramWindow reads a Go duration, or a whole number of days or
// weeks such as 1d. LogQL ranges have millisecond precision.
func parseHistogramWindow(window string) (time.Duration, error) {
	var d time.Duration
	if unit := window[len(window)-1]; unit == 'd' || unit == 'w' {
		n, err := strconv.Atoi(window[:len(window)-1])
		if err != nil || n <= 0 || n > 3660 {
			return 0, fmt.Errorf("invalid window %q", window)
		}
		d = time.Duration(n) * 24 * time.Hour
		if unit == 'w' {
			d *= 7
		}
	} else {
		parsed, err := time.ParseDuration(window)
		if err != nil {
			return 0, err
		}
		d = parsed
	}
	if d < time.Millisecond || d%time.Millisecond != 0 {
		return 0, fmt.Errorf("invalid window %q", window)
	}
	return d, nil
}

// groupRegex matches exactly the given label values. Loki anchors label
// filter regexes, and an empty value matches lines without the label.
func groupRegex(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = regexp.QuoteMeta(v)
	}
	return strings.Join(quoted, "|")
}

// bucketCounts maps a bucket's start, in Unix milliseconds, to the count of
// each group value. An ungrouped count is under "".
type bucketCounts map[int64]map[string]int

func (c bucketCounts) add(bucket int64, group string, n int) {
	if n == 0 {
		return
	}
	if c[bucket] == nil {
		c[bucket] = map[string]int{}
	}
	c[bucket][group] += n
}

func sortedBuckets(c bucketCounts) []int64 {
	buckets := make([]int64, 0, len(c))
	for bucket := range c {
		buckets = append(buckets, bucket)
	}
	slices.Sort(buckets)
	return buckets
}

type histogramRun struct {
	p    *Provider
	conn models.LokiConnectionInfo
	plan []histogramQuery
}

// count runs the plan for one log query, grouped by the label groupBy when
// it is set.
func (h histogramRun) count(ctx context.Context, logQuery, groupBy string) (bucketCounts, error) {
	counts := bucketCounts{}
	for _, q := range h.plan {
		expr := countExpr(logQuery, groupBy, q.width)
		var series []histogramSeries
		var err error
		if q.instant {
			series, err = h.instant(ctx, expr, q.at)
		} else {
			series, err = h.rangeQuery(ctx, expr, q.start, q.end, q.width)
		}
		if err != nil {
			return nil, err
		}
		for _, s := range series {
			group := ""
			if groupBy != "" {
				group = s.Metric[groupBy]
			}
			for _, point := range s.points {
				bucket := q.bucket
				if !q.instant {
					bucket = point.at - q.width
				}
				counts.add(bucket, group, point.count)
			}
		}
	}
	return counts, nil
}

// topGroups returns the values of groupBy with the most entries in
// (start, end], at most histogramGroupLimit of them.
func (h histogramRun) topGroups(ctx context.Context, logQuery, groupBy string, start, end int64) ([]string, error) {
	expr := fmt.Sprintf("topk(%d, %s)", histogramGroupLimit, countExpr(logQuery, groupBy, end-start))
	series, err := h.instant(ctx, expr, end)
	if err != nil {
		return nil, err
	}
	groups := make([]string, 0, len(series))
	for _, s := range series {
		groups = append(groups, s.Metric[groupBy])
	}
	slices.Sort(groups)
	return slices.Compact(groups), nil
}

// countExpr wraps a log query in count_over_time. The newline ends a trailing
// # comment of the query, which would otherwise hide the range.
func countExpr(logQuery, groupBy string, widthMs int64) string {
	by := ""
	if groupBy != "" {
		by = " by (" + groupBy + ")"
	}
	return fmt.Sprintf("sum%s (count_over_time(%s\n[%dms]))", by, logQuery, widthMs)
}

type histogramPoint struct {
	at    int64
	count int
}

type histogramSeries struct {
	Metric map[string]string
	points []histogramPoint
}

type histogramResponse struct {
	Data struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
}

func (h histogramRun) instant(ctx context.Context, expr string, at int64) ([]histogramSeries, error) {
	params := lokiParams{"query": {expr}, "time": {nanos(time.UnixMilli(at))}}
	data, err := h.fetch(ctx, "/loki/api/v1/query", params, "vector")
	if err != nil {
		return nil, err
	}
	var vector []struct {
		Metric map[string]string  `json:"metric"`
		Value  [2]json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &vector); err != nil {
		return nil, histogramDecodeError(err)
	}
	series := make([]histogramSeries, len(vector))
	for i, v := range vector {
		point, err := histogramSample(v.Value)
		if err != nil {
			return nil, err
		}
		series[i] = histogramSeries{Metric: v.Metric, points: []histogramPoint{point}}
	}
	return series, nil
}

func (h histogramRun) rangeQuery(ctx context.Context, expr string, start, end, step int64) ([]histogramSeries, error) {
	params := lokiParams{
		"query": {expr},
		"start": {nanos(time.UnixMilli(start))},
		"end":   {nanos(time.UnixMilli(end))},
		"step":  {strconv.FormatInt(step, 10) + "ms"},
	}
	data, err := h.fetch(ctx, "/loki/api/v1/query_range", params, "matrix")
	if err != nil {
		return nil, err
	}
	var matrix []lokiSeries
	if err := json.Unmarshal(data, &matrix); err != nil {
		return nil, histogramDecodeError(err)
	}
	series := make([]histogramSeries, len(matrix))
	for i, m := range matrix {
		series[i].Metric = m.Metric
		for _, value := range m.Values {
			point, err := histogramSample(value)
			if err != nil {
				return nil, err
			}
			series[i].points = append(series[i].points, point)
		}
	}
	return series, nil
}

// fetch runs one metric query and returns its result. A LogQL parse error
// means the query is not a log query, the only kind count_over_time takes.
func (h histogramRun) fetch(ctx context.Context, path string, params lokiParams, resultType string) (json.RawMessage, error) {
	body, err := h.p.get(ctx, h.conn, path, params, models.MaxHistogramResponseBytes)
	if errors.Is(err, errBodyTooLarge) {
		return nil, models.ErrHistogramBudgetExceeded
	}
	if validation, ok := errors.AsType[*datasource.ValidationError](err); ok && strings.HasPrefix(validation.Message, "parse error") {
		return nil, &datasource.ValidationError{Field: "query", Message: "The histogram needs a LogQL log query: " + validation.Message, Err: validation}
	}
	if err != nil {
		return nil, err
	}
	var resp histogramResponse
	if err := decodeBody(body, &resp); err != nil {
		return nil, err
	}
	if resp.Data.ResultType != resultType {
		return nil, &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Message: fmt.Sprintf("Loki returned a %q result where a %q was expected", resp.Data.ResultType, resultType)}
	}
	return resp.Data.Result, nil
}

// histogramSample reads one [unix seconds, "count"] pair. The time is a
// multiple of a millisecond, so it is read as text, not through a float.
func histogramSample(value [2]json.RawMessage) (histogramPoint, error) {
	at, err := time.Parse(time.RFC3339Nano, sampleTime(value[0]))
	if err != nil {
		return histogramPoint{}, histogramDecodeError(err)
	}
	count, err := strconv.ParseFloat(sampleValue(value[1]), 64)
	if err != nil {
		return histogramPoint{}, histogramDecodeError(err)
	}
	return histogramPoint{at: at.UnixMilli(), count: int(count)}, nil
}

func histogramDecodeError(err error) error {
	return &datasource.UpstreamError{Kind: datasource.UpstreamUnavailable, Message: "Loki sent a histogram result that could not be read", Err: err}
}
