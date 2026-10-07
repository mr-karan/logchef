package victorialogs

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const maxSearchWindows = 4096

type windowCursor struct {
	Fingerprint string `json:"fingerprint"`
	Index       int    `json:"index"`
	Offset      int    `json:"offset"`
}

type windowLimiter struct {
	mu      sync.Mutex
	active  int
	limit   int
	changed chan struct{}
}

func (p *Provider) windowLimiter(sourceID models.SourceID, concurrency int) *windowLimiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.windowLimits == nil {
		p.windowLimits = make(map[models.SourceID]*windowLimiter)
	}
	limiter := p.windowLimits[sourceID]
	if limiter == nil {
		limiter = &windowLimiter{limit: concurrency, changed: make(chan struct{})}
		p.windowLimits[sourceID] = limiter
	}
	limiter.mu.Lock()
	limiter.limit = concurrency
	close(limiter.changed)
	limiter.changed = make(chan struct{})
	limiter.mu.Unlock()
	return limiter
}

func (l *windowLimiter) acquire(ctx context.Context) error {
	for {
		l.mu.Lock()
		if err := ctx.Err(); err != nil {
			l.mu.Unlock()
			return err
		}
		if l.active < l.limit {
			l.active++
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (l *windowLimiter) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active--
	close(l.changed)
	l.changed = make(chan struct{})
}

func (p *Provider) PrepareWindowedQuery(source *models.Source, req datasource.WindowedRequest) (*datasource.WindowedPlan, error) {
	conn, err := source.VictoriaLogsConnection()
	if err != nil {
		return nil, err
	}
	if conn.Optimizer == nil || !conn.Optimizer.Enabled {
		return nil, datasource.ErrOperationNotSupported
	}
	if err := conn.Optimizer.Validate(); err != nil {
		return nil, err
	}
	if err := validateWindowedRequest(&req); err != nil {
		return nil, err
	}
	settings := conn.Optimizer.WithDefaults()
	width := time.Duration(settings.MaxWindowSeconds) * time.Second
	windows := make([]datasource.SearchWindow, 0)
	for end := *req.EndTime; end.After(*req.StartTime); {
		if len(windows) == maxSearchWindows {
			return nil, fmt.Errorf("time range exceeds %d search windows", maxSearchWindows)
		}
		start := end.Add(-width)
		if start.Before(*req.StartTime) {
			start = *req.StartTime
		}
		windows = append(windows, datasource.SearchWindow{Index: len(windows), Start: start, End: end})
		end = start
	}
	limit, _, _ := resolveQueryLimit(req.Limit, req.DefaultLimit, req.MaxLimit)
	req.Limit = limit
	fingerprintData, err := json.Marshal(struct {
		SourceID models.SourceID `json:"source_id"`
		Revision time.Time       `json:"revision"`
		Query    string          `json:"query"`
		Start    time.Time       `json:"start"`
		End      time.Time       `json:"end"`
		Width    int             `json:"width"`
		Filters  []string        `json:"filters"`
	}{source.ID, source.UpdatedAt, req.RawQuery, *req.StartTime, *req.EndTime, settings.MaxWindowSeconds, req.StreamFilters})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(fingerprintData)
	plan := &datasource.WindowedPlan{Request: req, Windows: windows, Fingerprint: hex.EncodeToString(hash[:]), Concurrency: settings.Concurrency}
	if err := restoreWindowCursor(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func validateWindowedRequest(req *datasource.WindowedRequest) error {
	query := strings.TrimSpace(req.RawQuery)
	if query == "" {
		query = "*"
	}
	req.RawQuery = query
	if len(splitTopLevelPipes(query)) != 1 {
		return datasource.ErrOperationNotSupported
	}
	if req.StartTime == nil || req.EndTime == nil || !req.StartTime.Before(*req.EndTime) {
		return fmt.Errorf("windowed search requires start_time before end_time")
	}
	if req.Count {
		step, err := time.ParseDuration(req.Step)
		if err != nil || step < time.Second {
			return fmt.Errorf("count step must be at least 1s")
		}
		if req.EndTime.Sub(*req.StartTime)/step > 1000 {
			return fmt.Errorf("count step produces more than 1000 buckets")
		}
		req.Step = step.String()
	}
	if len(req.StreamFilters) > 16 {
		return fmt.Errorf("too many stream filters")
	}
	for _, filter := range req.StreamFilters {
		if len(filter) > 65536 || !strings.HasPrefix(filter, "{") || !strings.HasSuffix(filter, "}") {
			return fmt.Errorf("stream filters must be stream selectors")
		}
	}
	return nil
}

func restoreWindowCursor(plan *datasource.WindowedPlan) error {
	req := plan.Request
	if req.Cursor != "" {
		if len(req.Cursor) > 2048 {
			return fmt.Errorf("invalid search cursor")
		}
		data, err := base64.RawURLEncoding.DecodeString(req.Cursor)
		if err != nil {
			return fmt.Errorf("invalid search cursor")
		}
		var cursor windowCursor
		if err := json.Unmarshal(data, &cursor); err != nil || cursor.Fingerprint != plan.Fingerprint || cursor.Index < 0 || cursor.Index >= len(plan.Windows) || cursor.Offset < 0 || cursor.Offset > math.MaxInt-plan.Request.Limit-1 {
			return fmt.Errorf("search cursor does not match this query or source")
		}
		plan.Index, plan.Offset = cursor.Index, cursor.Offset
	}
	if req.WindowIndex != nil {
		if *req.WindowIndex < 0 || *req.WindowIndex >= len(plan.Windows) {
			return fmt.Errorf("window_index is outside the selected range")
		}
		if !req.Count && (req.Cursor == "" || *req.WindowIndex != plan.Index) {
			return fmt.Errorf("search retries require a cursor for the selected window")
		}
		plan.Index = *req.WindowIndex
	}
	if req.SkipWindow != nil && (req.Count || *req.SkipWindow != plan.Index) {
		return fmt.Errorf("only the current search window can be skipped")
	}
	return nil
}

func searchCursor(plan *datasource.WindowedPlan, index, offset int) string {
	if index >= len(plan.Windows) {
		return ""
	}
	data := fmt.Sprintf(`{"fingerprint":%q,"index":%d,"offset":%d}`, plan.Fingerprint, index, offset)
	return base64.RawURLEncoding.EncodeToString([]byte(data))
}

type windowResult struct {
	index   int
	rows    []map[string]any
	buckets []datasource.HistogramBucket
	stats   models.QueryStats
	err     error
}

// StreamWindowedQuery owns its workers. At most Concurrency windows are in
// flight or buffered, and the source limiter is shared by all search/count streams.
func (p *Provider) StreamWindowedQuery(ctx context.Context, source *models.Source, plan *datasource.WindowedPlan, emit func(datasource.WindowedEvent) error) error { //nolint:gocyclo // ordered streaming coordinator
	conn, err := p.connectionForSource(source)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	limiter := p.windowLimiter(source.ID, plan.Concurrency)
	if err := emit(datasource.WindowedEvent{Type: datasource.WindowPlan, Windows: plan.Windows, Cursor: searchCursor(plan, plan.Index, plan.Offset)}); err != nil {
		return err
	}
	index := plan.Index
	if plan.Request.SkipWindow != nil {
		window := plan.Windows[index]
		index++
		if err := emit(datasource.WindowedEvent{Type: datasource.WindowState, Window: &window, Status: datasource.WindowSkipped, Cursor: searchCursor(plan, index, 0)}); err != nil {
			return err
		}
	}
	last := len(plan.Windows)
	if plan.Request.WindowIndex != nil {
		last = index + 1
	}
	results := make(chan windowResult, plan.Concurrency)
	next := index
	inFlight := 0
	remaining := plan.Request.Limit
	bytesRemaining := plan.Request.MaxResponseBytes
	pending := make(map[int]windowResult)
	launch := func(i int) error {
		window := plan.Windows[i]
		offset := 0
		if i == plan.Index && plan.Request.SkipWindow == nil {
			offset = plan.Offset
		}
		if err := emit(datasource.WindowedEvent{Type: datasource.WindowState, Window: &window, Status: datasource.WindowRunning, Cursor: searchCursor(plan, i, offset)}); err != nil {
			return err
		}
		inFlight++
		workers.Go(func() {
			result := windowResult{index: i}
			if err := limiter.acquire(ctx); err != nil {
				result.err = err
			} else {
				result = p.querySearchWindow(ctx, conn, plan.Request, window, offset)
				limiter.release()
			}
			select {
			case results <- result:
			case <-ctx.Done():
			}
		})
		return nil
	}
	for index < last {
		for next < last && inFlight+len(pending) < plan.Concurrency {
			if err := launch(next); err != nil {
				return err
			}
			next++
		}
		if _, ready := pending[index]; !ready {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case result := <-results:
				inFlight--
				pending[result.index] = result
			}
		}
		for {
			result, ok := pending[index]
			if !ok {
				break
			}
			delete(pending, index)
			window := plan.Windows[index]
			cursor := searchCursor(plan, index, 0)
			if index == plan.Index {
				cursor = searchCursor(plan, index, plan.Offset)
			}
			if result.err != nil {
				p.log.Warn("window query failed", "source_id", source.ID, "window", index, "error", result.err)
				if err := emit(datasource.WindowedEvent{Type: datasource.WindowState, Window: &window, Status: datasource.WindowFailed, Cursor: cursor, Message: "Window query failed. Retry this time range."}); err != nil {
					return err
				}
				if !plan.Request.Count {
					return emit(datasource.WindowedEvent{Type: datasource.WindowEnd, Cursor: cursor})
				}
				index++
				continue
			}
			if plan.Request.Count {
				if err := emit(datasource.WindowedEvent{Type: datasource.WindowCount, Window: &window, Status: datasource.WindowComplete, Buckets: result.buckets}); err != nil {
					return err
				}
				index++
				continue
			}
			n := min(len(result.rows), remaining)
			if plan.Request.MaxResponseBytes > 0 {
				for j, row := range result.rows[:n] {
					data, err := json.Marshal(row)
					if err != nil {
						return err
					}
					if len(data)+1 > bytesRemaining {
						n = j
						break
					}
					bytesRemaining -= len(data) + 1
				}
			}
			offset := 0
			if index == plan.Index && plan.Request.SkipWindow == nil {
				offset = plan.Offset
			}
			exhausted := len(result.rows) <= n && !result.stats.Truncated
			if exhausted {
				cursor = searchCursor(plan, index+1, 0)
			} else {
				cursor = searchCursor(plan, index, offset+n)
			}
			result.stats.RowsReturned = n
			result.stats.LimitApplied = plan.Request.Limit
			status := datasource.WindowComplete
			if !exhausted {
				status = datasource.WindowPartial
			}
			if n == 0 && !exhausted {
				if remaining == plan.Request.Limit {
					if err := emit(datasource.WindowedEvent{Type: datasource.WindowState, Window: &window, Status: datasource.WindowFailed, Cursor: cursor, Message: "A log in this time range exceeds the response size limit. Skip this window or narrow the query."}); err != nil {
						return err
					}
				}
				return emit(datasource.WindowedEvent{Type: datasource.WindowEnd, Cursor: cursor})
			}
			if err := emit(datasource.WindowedEvent{Type: datasource.WindowRows, Window: &window, Status: status, Logs: result.rows[:n], Stats: &result.stats, Cursor: cursor}); err != nil {
				return err
			}
			remaining -= n
			if remaining == 0 || !exhausted || (plan.Request.MaxResponseBytes > 0 && bytesRemaining == 0) {
				return emit(datasource.WindowedEvent{Type: datasource.WindowEnd, Cursor: cursor, Complete: cursor == ""})
			}
			index++
		}
	}
	return emit(datasource.WindowedEvent{Type: datasource.WindowEnd, Complete: true})
}

func (p *Provider) querySearchWindow(ctx context.Context, conn models.VictoriaLogsConnectionInfo, req datasource.WindowedRequest, window datasource.SearchWindow, offset int) windowResult {
	result := windowResult{index: window.Index}
	if req.QueryTimeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*req.QueryTimeout)*time.Second)
		defer cancel()
	}
	query := req.RawQuery
	if req.Count {
		// Align buckets to the selected timezone, as the standard hits histogram does.
		bucket := "_time:" + req.Step
		if offset := formatTimezoneOffset(req.Timezone, req.StartTime, req.EndTime); offset != "" {
			bucket += " offset " + offset
		}
		query += " | stats by (" + bucket + ") count() as log_count"
	} else {
		// Pack the original row before sorting. The complete row breaks timestamp
		// ties, and decoding it restores even a pre-existing _logchef_row field.
		query += fmt.Sprintf(" | pack_json as _logchef_row | sort by (_time desc, _logchef_row) offset %d limit %d | fields _logchef_row", offset, req.Limit+1)
	}
	form := url.Values{"query": {query}, "start": {formatAPITime(window.Start)}, "end": {formatAPITime(window.End)}}
	if timeout := formatTimeout(req.QueryTimeout); timeout != "" {
		form.Set("timeout", timeout)
	}
	applyScopeFilters(form, conn)
	for _, filter := range req.StreamFilters {
		form.Add("extra_stream_filters", filter)
	}
	resp, err := p.doFormRequest(ctx, conn, "/select/logsql/query", form)
	if err != nil {
		result.err = err
		return result
	}
	defer resp.Body.Close()
	rows, _, bytesReturned, truncated, err := readQueryRows(resp.Body, req.MaxResponseBytes, req.Limit+1)
	result.stats = statsFromHeaders(resp, len(rows))
	result.stats.BytesReturned = bytesReturned
	result.stats.Truncated = truncated != ""
	result.stats.TruncatedReason = truncated
	if err != nil {
		result.err = err
		return result
	}
	if req.Count && truncated != "" {
		result.err = fmt.Errorf("count response exceeds the response byte limit")
		return result
	}
	for _, row := range rows {
		if req.Count {
			timestamp, ok := row["_time"].(string)
			if !ok {
				result.err = fmt.Errorf("count bucket has no timestamp")
				return result
			}
			bucket, err := time.Parse(time.RFC3339Nano, timestamp)
			if err != nil {
				result.err = err
				return result
			}
			count, err := strconv.Atoi(fmt.Sprint(row["log_count"]))
			if err != nil || count < 0 {
				result.err = fmt.Errorf("invalid window count")
				return result
			}
			result.buckets = append(result.buckets, datasource.HistogramBucket{Bucket: bucket, LogCount: count})
		} else {
			packed, ok := row["_logchef_row"].(string)
			if !ok {
				result.err = fmt.Errorf("window result has no packed row")
				return result
			}
			var original map[string]any
			if err := json.Unmarshal([]byte(packed), &original); err != nil {
				result.err = err
				return result
			}
			result.rows = append(result.rows, original)
		}
	}
	return result
}
