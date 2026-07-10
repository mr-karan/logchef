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
	// signals the stream is falling behind; the row-rate ceiling upstream then
	// drops the excess rather than letting it back up.
	tailBatchLimit = 1000
	// tailPollTimeoutSeconds bounds each poll query tightly — a tail must never
	// block on a slow poll.
	tailPollTimeoutSeconds = 10
	// tailMaxConsecutiveFailures ends the stream after this many back-to-back
	// poll errors; transient errors below the threshold are retried next tick.
	tailMaxConsecutiveFailures = 5
	// defaultTailPollInterval is the fallback cadence when the request carries none.
	defaultTailPollInterval = 2 * time.Second
)

// TailLogs polls the source table on a ticker, emitting rows newer than a cursor
// that starts at time.Now() and advances to the newest timestamp seen. The poll
// window is inclusive (>= cursor) so rows sharing the boundary timestamp are not
// missed at second granularity; boundary rows already emitted are removed by the
// dedup set (ported from the Rust CLI's tail command). req.Query is a ClickHouse
// SQL WHERE-fragment (conditions only), which is composed into the poll query.
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

	tsField := source.MetaTSField
	filter := strings.TrimSpace(req.Query)
	dedup := newTailDedup()
	cursor := time.Now()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0
	for {
		rows, err := p.pollTail(ctx, client, source, tsField, filter, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			consecutiveFailures++
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
			dedup.evictBefore(cursor)
			if len(fresh) > 0 {
				if err := emit(fresh); err != nil {
					return err
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
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
