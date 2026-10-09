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

// windowCursorVersion is part of the plan fingerprint, so cursors from an
// incompatible pagination scheme do not match.
const windowCursorVersion = 2

type windowCursor struct {
	Fingerprint string `json:"fingerprint"`
	Index       int    `json:"index"`
	TimeNanos   int64  `json:"time_ns,omitempty"`
	Skip        int    `json:"skip,omitempty"`
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
		Version  int             `json:"version"`
		SourceID models.SourceID `json:"source_id"`
		Revision time.Time       `json:"revision"`
		Query    string          `json:"query"`
		Start    time.Time       `json:"start"`
		End      time.Time       `json:"end"`
		Width    int             `json:"width"`
		Filters  []string        `json:"filters"`
	}{windowCursorVersion, source.ID, source.UpdatedAt, req.RawQuery, *req.StartTime, *req.EndTime, settings.MaxWindowSeconds, req.StreamFilters})
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
		if err := decodeWindowCursor(plan, req.Cursor); err != nil {
			return err
		}
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

func decodeWindowCursor(plan *datasource.WindowedPlan, encoded string) error {
	if len(encoded) > 2048 {
		return fmt.Errorf("invalid search cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("invalid search cursor")
	}
	var cursor windowCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Fingerprint != plan.Fingerprint || cursor.Index < 0 || cursor.Index >= len(plan.Windows) || cursor.Skip < 0 || cursor.Skip > math.MaxInt-plan.Request.Limit-1 {
		return fmt.Errorf("search cursor does not match this query or source")
	}
	plan.Index = cursor.Index
	if cursor.Skip > 0 {
		window := plan.Windows[cursor.Index]
		position := datasource.WindowPosition{Time: time.Unix(0, cursor.TimeNanos).UTC(), Skip: cursor.Skip}
		if position.Time.Before(window.Start) || !position.Time.Before(window.End) {
			return fmt.Errorf("search cursor does not match this query or source")
		}
		plan.Position = position
	}
	return nil
}

func searchCursor(plan *datasource.WindowedPlan, index int, position datasource.WindowPosition) string {
	if index >= len(plan.Windows) {
		return ""
	}
	data := fmt.Sprintf(`{"fingerprint":%q,"index":%d}`, plan.Fingerprint, index)
	if position.Skip > 0 {
		data = fmt.Sprintf(`{"fingerprint":%q,"index":%d,"time_ns":%d,"skip":%d}`, plan.Fingerprint, index, position.Time.UnixNano(), position.Skip)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(data))
}

type windowResult struct {
	index int
	rows  []map[string]any
	// positions[i] is the window position after rows[:i+1].
	positions []datasource.WindowPosition
	buckets   []datasource.HistogramBucket
	stats     models.QueryStats
	err       error
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
	if err := emit(datasource.WindowedEvent{Type: datasource.WindowPlan, Windows: plan.Windows, Cursor: searchCursor(plan, plan.Index, plan.Position)}); err != nil {
		return err
	}
	index := plan.Index
	if plan.Request.SkipWindow != nil {
		window := plan.Windows[index]
		index++
		if err := emit(datasource.WindowedEvent{Type: datasource.WindowState, Window: &window, Status: datasource.WindowSkipped, Cursor: searchCursor(plan, index, datasource.WindowPosition{})}); err != nil {
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
	startPosition := func(i int) datasource.WindowPosition {
		if i == plan.Index && plan.Request.SkipWindow == nil {
			return plan.Position
		}
		return datasource.WindowPosition{}
	}
	launch := func(i int) error {
		window := plan.Windows[i]
		position := startPosition(i)
		if err := emit(datasource.WindowedEvent{Type: datasource.WindowState, Window: &window, Status: datasource.WindowRunning, Cursor: searchCursor(plan, i, position)}); err != nil {
			return err
		}
		inFlight++
		workers.Go(func() {
			result := windowResult{index: i}
			if err := limiter.acquire(ctx); err != nil {
				result.err = err
			} else {
				result = p.querySearchWindow(ctx, conn, source.ID, plan.Request, window, position)
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
			position := startPosition(index)
			cursor := searchCursor(plan, index, position)
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
			exhausted := len(result.rows) <= n && !result.stats.Truncated
			if exhausted {
				cursor = searchCursor(plan, index+1, datasource.WindowPosition{})
			} else if n > 0 {
				cursor = searchCursor(plan, index, result.positions[n-1])
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

func (p *Provider) querySearchWindow(ctx context.Context, conn models.VictoriaLogsConnectionInfo, sourceID models.SourceID, req datasource.WindowedRequest, window datasource.SearchWindow, position datasource.WindowPosition) windowResult {
	result := windowResult{index: window.Index}
	if req.QueryTimeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*req.QueryTimeout)*time.Second)
		defer cancel()
	}
	if req.Count {
		// Align buckets to the selected timezone, as the standard hits histogram does.
		bucket := "_time:" + req.Step
		if offset := formatTimezoneOffset(req.Timezone, req.StartTime, req.EndTime); offset != "" {
			bucket += " offset " + offset
		}
		query := req.RawQuery + " | stats by (" + bucket + ") count() as log_count"
		var rows []map[string]any
		rows, result.stats, result.err = p.queryWindowRows(ctx, conn, req, query, window.Start, window.End)
		if result.err == nil && result.stats.Truncated {
			result.err = fmt.Errorf("count response exceeds the response byte limit")
		}
		if result.err == nil {
			result.buckets, result.err = decodeCountRows(rows)
		}
		return result
	}
	// Sorting packed rows orders timestamp ties exactly, but packs every row
	// in the range. Find the oldest timestamp this page can reach with a
	// time-only sort first, so only that short range is packed and sorted.
	// VictoriaLogs optimizes the time-only sort to read the newest logs.
	start, end := window.Start, window.End
	newer := end
	if position.Skip > 0 {
		newer = position.Time
		end = position.Time.Add(time.Nanosecond)
	}
	oldest, err := p.querySearchBoundary(ctx, conn, req, start, newer)
	if err != nil {
		result.err = err
		return result
	}
	if !oldest.IsZero() {
		start = oldest
	}
	// The complete row breaks timestamp ties, and decoding it restores even
	// a pre-existing _logchef_row field.
	query := req.RawQuery + fmt.Sprintf(" | pack_json as _logchef_row | sort by (_time desc, _logchef_row) offset %d limit %d | fields _logchef_row", position.Skip, req.Limit+1)
	rows, stats, err := p.queryWindowRows(ctx, conn, req, query, start, end)
	// The narrowed range holds every row at or after its start, so its rows
	// are a correct prefix of the window. A correct boundary leaves at least
	// Limit+1 rows in that range, so a short page that was not cut by the
	// byte limit means the boundary was too new and cannot prove that the
	// window is exhausted. VictoriaLogs v1.52.0 can return such a boundary
	// from its last-N optimization. Read the page again from the window start.
	if err == nil && start.After(window.Start) && !stats.Truncated && len(rows) <= req.Limit {
		p.log.Warn("search boundary did not fill the page, reading the full window", "source_id", sourceID, "window", window.Index, "window_start", window.Start, "window_end", window.End)
		rows, stats, err = p.queryWindowRows(ctx, conn, req, query, window.Start, end)
	}
	result.stats = stats
	if err != nil {
		result.err = err
		return result
	}
	result.rows, result.positions, result.err = decodeSearchRows(rows, position)
	return result
}

// queryWindowRows runs one window query over [start, end) and reads at most
// the response byte limit.
func (p *Provider) queryWindowRows(ctx context.Context, conn models.VictoriaLogsConnectionInfo, req datasource.WindowedRequest, query string, start, end time.Time) ([]map[string]any, models.QueryStats, error) {
	resp, err := p.doFormRequest(ctx, conn, "/select/logsql/query", windowForm(conn, req, query, start, end))
	if err != nil {
		return nil, models.QueryStats{}, err
	}
	defer resp.Body.Close()
	rows, _, bytesReturned, truncated, err := readQueryRows(resp.Body, req.MaxResponseBytes, req.Limit+1)
	stats := statsFromHeaders(resp, len(rows))
	stats.BytesReturned = bytesReturned
	stats.Truncated = truncated != ""
	stats.TruncatedReason = truncated
	return rows, stats, err
}

func decodeCountRows(rows []map[string]any) ([]datasource.HistogramBucket, error) {
	buckets := make([]datasource.HistogramBucket, 0, len(rows))
	for _, row := range rows {
		bucket, err := rowTime(row)
		if err != nil {
			return nil, fmt.Errorf("invalid count bucket: %w", err)
		}
		count, err := strconv.Atoi(fmt.Sprint(row["log_count"]))
		if err != nil || count < 0 {
			return nil, fmt.Errorf("invalid window count")
		}
		buckets = append(buckets, datasource.HistogramBucket{Bucket: bucket, LogCount: count})
	}
	return buckets, nil
}

// decodeSearchRows unpacks rows sorted by (_time desc, packed row) and returns
// the window position after each row.
func decodeSearchRows(rows []map[string]any, position datasource.WindowPosition) ([]map[string]any, []datasource.WindowPosition, error) {
	originals := make([]map[string]any, 0, len(rows))
	positions := make([]datasource.WindowPosition, 0, len(rows))
	for _, row := range rows {
		packed, ok := row["_logchef_row"].(string)
		if !ok {
			return nil, nil, fmt.Errorf("window result has no packed row")
		}
		var original map[string]any
		if err := json.Unmarshal([]byte(packed), &original); err != nil {
			return nil, nil, err
		}
		timestamp, err := rowTime(original)
		if err != nil {
			return nil, nil, err
		}
		if timestamp.Equal(position.Time) {
			position.Skip++
		} else {
			position = datasource.WindowPosition{Time: timestamp, Skip: 1}
		}
		originals = append(originals, original)
		positions = append(positions, position)
	}
	return originals, positions, nil
}

// querySearchBoundary returns the timestamp of the (limit+1)-th newest row in
// [start, end), or the zero time when the range has fewer rows. The query
// matches the "sort by (_time desc) offset N limit M" form that VictoriaLogs
// optimizes for the last N results.
func (p *Provider) querySearchBoundary(ctx context.Context, conn models.VictoriaLogsConnectionInfo, req datasource.WindowedRequest, start, end time.Time) (time.Time, error) {
	query := req.RawQuery + fmt.Sprintf(" | sort by (_time desc) offset %d limit 1 | fields _time", req.Limit)
	resp, err := p.doFormRequest(ctx, conn, "/select/logsql/query", windowForm(conn, req, query, start, end))
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	rows, _, _, truncated, err := readQueryRows(resp.Body, 4096, 1)
	if err != nil {
		return time.Time{}, err
	}
	if truncated != "" {
		return time.Time{}, fmt.Errorf("invalid search boundary response")
	}
	if len(rows) == 0 {
		return time.Time{}, nil
	}
	return rowTime(rows[0])
}

func windowForm(conn models.VictoriaLogsConnectionInfo, req datasource.WindowedRequest, query string, start, end time.Time) url.Values {
	form := url.Values{"query": {query}, "start": {formatAPITime(start)}, "end": {formatAPITime(end)}}
	if timeout := formatTimeout(req.QueryTimeout); timeout != "" {
		form.Set("timeout", timeout)
	}
	applyScopeFilters(form, conn)
	for _, filter := range req.StreamFilters {
		form.Add("extra_stream_filters", filter)
	}
	return form
}

func rowTime(row map[string]any) (time.Time, error) {
	timestamp, ok := row["_time"].(string)
	if !ok {
		return time.Time{}, fmt.Errorf("window row has no timestamp")
	}
	return time.Parse(time.RFC3339Nano, timestamp)
}
