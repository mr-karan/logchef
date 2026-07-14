package datasource

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// tailBatchLimit caps rows fetched per poll. A poll returning a full batch
	// signals more rows are waiting past this poll's LIMIT; TailLogs re-polls
	// immediately (up to tailMaxConsecutiveDrains times) instead of waiting for
	// the next tick, so a busy boundary drains rather than silently losing the
	// remainder. The row-rate ceiling upstream (server-side) is what actually
	// drops excess rows for a sustained firehose.
	tailBatchLimit = 1000
	// tailPollTimeoutSeconds bounds each poll query tightly — a tail must never
	// block on a slow poll.
	tailPollTimeoutSeconds = 10
	// tailMaxConsecutiveFailures ends the stream after this many back-to-back
	// poll errors; transient errors below the threshold are retried next tick.
	tailMaxConsecutiveFailures = 5
	// tailMaxConsecutiveDrains caps back-to-back immediate re-polls triggered by
	// full batches. Without a cap, a source ingesting faster than tailBatchLimit
	// per poll would keep this goroutine draining forever and never reach the
	// select that checks ctx.Done(), so cancellation (client disconnect, TTL,
	// admission eviction) would never be observed.
	tailMaxConsecutiveDrains = 5
	// defaultTailPollInterval is the fallback cadence when the request carries none.
	defaultTailPollInterval = 2 * time.Second
	// defaultTailLookbackMargin is the fallback re-scan window when the request
	// carries none (e.g. a caller that doesn't populate it, such as a test).
	defaultTailLookbackMargin = 5 * time.Second
)

// TailLogs polls the source table on a ticker, emitting rows newer than a
// cursor seeded from ClickHouse's own clock (see now64) rather than the app
// server's, so app/ClickHouse clock skew cannot create a gap or a backlog at
// session start. Each poll re-scans a trailing lookbackMargin window behind
// the cursor — not just the cursor itself — so rows that finish ingesting
// slightly behind the cursor (ingestion lag, batched inserts arriving after
// their own timestamp was already polled past) are still picked up; the
// dedup set (ported from the Rust CLI's tail command) absorbs the resulting
// overlap so nothing already emitted surfaces twice. The margin window never
// reaches before the session's start time, so it cannot re-scan history from
// before the tail began. A poll returning a full batch triggers an immediate
// re-poll (capped) instead of waiting for the next tick, draining a busy
// boundary rather than silently dropping the remainder. req.Query is a
// ClickHouse SQL WHERE-fragment (conditions only), which is composed into the
// poll query.
func (p *ClickHouseProvider) TailLogs(ctx context.Context, source *models.Source, req TailRequest, emit TailEmitter) error {
	if source == nil {
		return fmt.Errorf("source is required")
	}
	if source.MetaTSField == "" {
		return fmt.Errorf("source %d does not have a timestamp field configured", source.ID)
	}

	client, err := p.manager.GetConnection(source.ID)
	if err != nil {
		return fmt.Errorf("error getting database connection for source %d: %w", source.ID, err)
	}

	interval := req.PollInterval
	if interval <= 0 {
		interval = defaultTailPollInterval
	}
	margin := req.LookbackMargin
	if margin <= 0 {
		margin = defaultTailLookbackMargin
	}

	tsField := source.MetaTSField
	filter := strings.TrimSpace(req.Query)
	dedup := newTailDedup()

	sessionStart, err := p.now64(ctx, client)
	if err != nil {
		p.log.Warn("tail: SELECT now64() failed, falling back to app clock for initial cursor",
			"source_id", source.ID, "error", err)
		sessionStart = time.Now().UTC()
	}
	cursor := sessionStart

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0
	consecutiveDrains := 0
	for {
		windowStart := tailPollWindowStart(sessionStart, cursor, margin)
		rows, err := p.pollTail(ctx, client, source, tsField, filter, windowStart)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			consecutiveFailures++
			consecutiveDrains = 0
			p.log.Warn("tail poll failed",
				"source_id", source.ID,
				"consecutive_failures", consecutiveFailures,
				"error", err)
			if consecutiveFailures >= tailMaxConsecutiveFailures {
				return fmt.Errorf("tail poll failed %d times consecutively: %w", consecutiveFailures, err)
			}
		} else {
			consecutiveFailures = 0
			fresh, newest := dedup.process(rows, tsField)
			if !newest.IsZero() && newest.After(cursor) {
				cursor = newest
			}
			dedup.evictBefore(tailPollWindowStart(sessionStart, cursor, margin))
			if len(fresh) > 0 {
				if err := emit(fresh); err != nil {
					return err
				}
			}

			if len(rows) >= tailBatchLimit && consecutiveDrains < tailMaxConsecutiveDrains {
				consecutiveDrains++
				// Still observe cancellation promptly during a drain burst rather
				// than looping straight back into pollTail.
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					continue
				}
			}
			consecutiveDrains = 0
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// tailPollWindowStart computes the inclusive lower bound of a poll: margin
// behind the cursor, but never before the session started (so a tail never
// re-scans history from before it began).
func tailPollWindowStart(sessionStart, cursor time.Time, margin time.Duration) time.Time {
	windowStart := cursor.Add(-margin)
	if windowStart.Before(sessionStart) {
		return sessionStart
	}
	return windowStart
}

// now64 asks ClickHouse for its own current wall-clock time, used to seed the
// tail cursor. Seeding from the server's clock (instead of the app server's
// time.Now()) avoids a gap or backlog at session start when the two clocks
// disagree, and gives subsecond precision that a bare time.Now() truncated
// against a DateTime (not DateTime64) column would otherwise lack.
func (p *ClickHouseProvider) now64(ctx context.Context, client *clickhouse.Client) (time.Time, error) {
	timeout := tailPollTimeoutSeconds
	result, err := client.QueryWithOptions(ctx, "SELECT now64(9, 'UTC') AS ts", clickhouse.QueryOptions{
		TimeoutSeconds: &timeout,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("query now64: %w", err)
	}
	if len(result.Logs) == 0 {
		return time.Time{}, fmt.Errorf("now64 query returned no rows")
	}
	ts := extractRowTime(result.Logs[0]["ts"])
	if ts.IsZero() {
		return time.Time{}, fmt.Errorf("now64 query returned an unparseable timestamp: %v", result.Logs[0]["ts"])
	}
	return ts, nil
}

// pollTail builds and runs a single poll query for rows at/after the cursor.
func (p *ClickHouseProvider) pollTail(ctx context.Context, client *clickhouse.Client, source *models.Source, tsField, filter string, cursor time.Time) ([]map[string]any, error) {
	sql := buildTailPollSQL(source.GetFullTableName(), tsField, filter, cursor)

	qb := clickhouse.NewExtendedQueryBuilder(source.GetFullTableName(), tailBatchLimit)
	buildResult, err := qb.BuildRawQueryWithLimitPolicy(sql, tailBatchLimit, tailBatchLimit, tailBatchLimit)
	if err != nil {
		return nil, fmt.Errorf("build tail query: %w", err)
	}

	timeout := tailPollTimeoutSeconds
	result, err := client.QueryWithOptions(ctx, buildResult.SQL, clickhouse.QueryOptions{
		TimeoutSeconds: &timeout,
		Settings: map[string]any{
			"max_execution_time":   timeout,
			"max_result_rows":      buildResult.AppliedLimit,
			"result_overflow_mode": "break",
		},
		LimitApplied: buildResult.AppliedLimit,
		MaxRows:      buildResult.AppliedLimit,
	})
	if err != nil {
		return nil, err
	}
	return result.Logs, nil
}

// buildTailPollSQL composes the poll SELECT. The cursor is emitted as a
// nanosecond DateTime64 literal so it composes with either DateTime or
// DateTime64 timestamp columns via implicit conversion.
func buildTailPollSQL(fullTableName, tsField, filter string, cursor time.Time) string {
	cursorLiteral := fmt.Sprintf("toDateTime64('%s', 9, 'UTC')", cursor.UTC().Format("2006-01-02 15:04:05.999999999"))

	var sb strings.Builder
	sb.WriteString("SELECT * FROM ")
	sb.WriteString(fullTableName)
	sb.WriteString(" WHERE `")
	sb.WriteString(tsField)
	sb.WriteString("` >= ")
	sb.WriteString(cursorLiteral)
	if filter != "" {
		sb.WriteString(" AND (")
		sb.WriteString(filter)
		sb.WriteString(")")
	}
	sb.WriteString(" ORDER BY `")
	sb.WriteString(tsField)
	sb.WriteString("` ASC")
	return sb.String()
}

// tailDedup tracks the rows already emitted so a re-fetched boundary timestamp
// (from the inclusive >= cursor window) does not surface a row twice.
type tailDedup struct {
	seen map[string]time.Time
}

func newTailDedup() *tailDedup {
	return &tailDedup{seen: make(map[string]time.Time)}
}

// process returns the rows not previously emitted and the newest timestamp
// observed across the whole batch (seen or not), which drives the cursor.
func (d *tailDedup) process(rows []map[string]any, tsField string) (fresh []map[string]any, newest time.Time) {
	for _, row := range rows {
		ts := extractRowTime(row[tsField])
		if ts.After(newest) {
			newest = ts
		}
		key := tailDedupKey(row, ts)
		if _, ok := d.seen[key]; ok {
			continue
		}
		d.seen[key] = ts
		fresh = append(fresh, row)
	}
	return fresh, newest
}

// evictBefore drops dedup keys older than the cursor. Anything older than the
// inclusive poll window cannot reappear, so it is safe to forget.
func (d *tailDedup) evictBefore(cursor time.Time) {
	for key, ts := range d.seen {
		if ts.Before(cursor) {
			delete(d.seen, key)
		}
	}
}

// tailDedupKey fingerprints a row: the timestamp plus a hash over its sorted
// field/value pairs, so identical rows collide regardless of map ordering.
//
// Inherent limitation: ClickHouse rows have no guaranteed unique identifier
// (no auto-increment, no row UUID). Two genuinely distinct log events that are
// byte-identical across every selected column, including the timestamp down
// to whatever precision the column stores, are indistinguishable from a
// re-fetch of the same row and will only be emitted once. This is expected to
// be rare (it requires a full-row collision, not just a shared timestamp) and
// is documented as a known limitation rather than solved, since fixing it
// would require either a schema change (a mandatory unique row ID column) or
// tracking result-set position, neither of which this stream boundary API
// exposes.
func tailDedupKey(row map[string]any, ts time.Time) string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := fnv.New64a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = fmt.Fprint(h, row[k])
		_, _ = h.Write([]byte{0})
	}
	return strconv.FormatInt(ts.UnixNano(), 10) + ":" + strconv.FormatUint(h.Sum64(), 16)
}

// extractRowTime coerces a timestamp column value to time.Time, handling the
// driver's native time.Time as well as the string forms ClickHouse may return.
func extractRowTime(value any) time.Time {
	switch v := value.(type) {
	case time.Time:
		return v
	case *time.Time:
		if v != nil {
			return *v
		}
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, v); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}
