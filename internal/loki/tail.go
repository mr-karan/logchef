package loki

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// tailPageLimit is the entries asked for in one tail request.
	tailPageLimit = 1000
	// tailMaxPagesPerTick bounds the requests of one tick, so a burst cannot
	// hold the tailer in a loop past cancellation and newly arriving entries.
	tailMaxPagesPerTick = 10
	// tailPollTimeout bounds one request. A tail must never block on a slow poll.
	tailPollTimeout = 10 * time.Second
	// tailMaxConsecutiveFailures, defaultTailPollInterval and
	// defaultTailLookbackMargin are the ClickHouse tailer's values.
	tailMaxConsecutiveFailures = 5
	defaultTailPollInterval    = 2 * time.Second
	defaultTailLookbackMargin  = 5 * time.Second
)

// TailLogs follows a LogQL log query by polling query_range forward
// (RFC 0011 "Live tail"). Loki's /tail WebSocket is not used: it rejects a
// multi-tenant org ID, is capped at ten per tenant, and proxies often do not
// upgrade the connection. Each poll rescans a margin behind the cursor, so an
// entry that arrives late is still read, and a set of emitted keys drops the
// overlap.
func (p *Provider) TailLogs(ctx context.Context, source *models.Source, req datasource.TailRequest, emit datasource.TailEmitter) error {
	conn, err := p.connectionForSource(source)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return &datasource.ValidationError{Field: "query", Message: "Enter a LogQL query, such as " + conn.Selector}
	}
	interval := req.PollInterval
	if interval <= 0 {
		interval = defaultTailPollInterval
	}
	margin := req.LookbackMargin
	if margin <= 0 {
		margin = defaultTailLookbackMargin
	}

	t := newTailer(time.Now().UnixNano(), margin, conn, emit, p.log.With("source_id", source.ID))
	t.fetch = p.tailFetch(conn, query)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failures := 0
	for {
		err := t.tick(ctx, time.Now().UnixNano())
		switch {
		case err == nil:
			failures = 0
		case ctx.Err() != nil:
			return ctx.Err()
		case isFinalTailError(err):
			return err
		default:
			failures++
			p.log.Warn("tail poll failed", "source_id", source.ID, "consecutive_failures", failures, "error", err)
			if failures >= tailMaxConsecutiveFailures {
				return fmt.Errorf("tail poll failed %d times consecutively: %w", failures, err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// tailFetch reads one forward page of query in [start, end).
func (p *Provider) tailFetch(conn models.LokiConnectionInfo, query string) func(ctx context.Context, start, end int64, limit int) (*rangePage, error) {
	return func(ctx context.Context, start, end int64, limit int) (*rangePage, error) {
		ctx, cancel := context.WithTimeout(ctx, tailPollTimeout)
		defer cancel()
		from := time.Unix(0, start)
		step, _ := queryStep(from, time.Unix(0, end), 0)
		return p.fetchPage(ctx, conn, rangeRequest{query: query, start: from, step: step, forward: true}, end, limit, defaultMaxResponseBytes)
	}
}

// isFinalTailError reports an error that a later poll cannot fix: the
// receiver stopped, or Loki rejected the query.
func isFinalTailError(err error) bool {
	var emitErr *tailEmitError
	var validationErr *datasource.ValidationError
	return errors.As(err, &emitErr) || errors.As(err, &validationErr)
}

// tailEmitError marks an error returned by the emitter, which ends the tail
// at once instead of counting as a failed poll.
type tailEmitError struct{ err error }

func (e *tailEmitError) Error() string { return e.err.Error() }
func (e *tailEmitError) Unwrap() error { return e.err }

// tailKey identifies an entry by its timestamp and a hash of its label
// string and line, so the emitted set does not hold whole lines.
type tailKey struct {
	ts  int64
	sum [16]byte
}

func keyOf(e logEntry) tailKey {
	h := fnv.New128a()
	_, _ = h.Write([]byte(e.labelKey))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(e.line))
	k := tailKey{ts: e.ts}
	h.Sum(k.sum[:0])
	return k
}

// tailer holds the poll state between ticks. Times are Unix nanoseconds.
// A request reads [scanFrom, upper). cursor is the newest timestamp emitted,
// and emitted holds the keys of the entries in [windowStart(), cursor].
type tailer struct {
	fetch     func(ctx context.Context, start, end int64, limit int) (*rangePage, error)
	emit      datasource.TailEmitter
	log       *slog.Logger
	pageLimit int
	margin    int64
	// sessionStart is the earliest timestamp read, so a tail never replays
	// history from before it began.
	sessionStart int64
	cursor       int64
	scanFrom     int64
	upper        int64
	// draining is set while a full page leaves entries before upper unread.
	draining bool
	emitted  map[tailKey]struct{}
}

func newTailer(start int64, margin time.Duration, conn models.LokiConnectionInfo, emit datasource.TailEmitter, log *slog.Logger) *tailer {
	return &tailer{
		emit:         emit,
		log:          log,
		pageLimit:    min(tailPageLimit, conn.EffectiveLimits().MaxEntriesPerQuery),
		margin:       int64(margin),
		sessionStart: start,
		cursor:       start,
		emitted:      map[tailKey]struct{}{},
	}
}

// windowStart is where a normal poll starts: the margin behind the cursor,
// but not before the session.
func (t *tailer) windowStart() int64 {
	return max(t.sessionStart, t.cursor-t.margin)
}

// tick reads pages until a short page drains the window or the page bound is
// reached. A normal poll reads [cursor - margin, now). A full page starts a
// drain that keeps upper and moves scanFrom to the newest timestamp on the
// page, so the next page overlaps it only at that timestamp. A drain cut by
// the page bound continues on the next tick.
func (t *tailer) tick(ctx context.Context, now int64) error {
	if !t.draining {
		t.upper = now
		t.scanFrom = t.windowStart()
	}
	for range tailMaxPagesPerTick {
		if t.scanFrom >= t.upper {
			t.draining = false
			return nil
		}
		page, err := t.fetch(ctx, t.scanFrom, t.upper, t.pageLimit)
		if errors.Is(err, errBodyTooLarge) {
			return &datasource.ValidationError{Field: "query", Message: "The tailed lines exceed the size limit. Narrow the query"}
		}
		if err != nil {
			return err
		}
		if page.resultType != "streams" {
			return &datasource.ValidationError{Field: "query", Message: "Live tail needs a LogQL log query"}
		}
		entries, err := flattenStreams(page.streams)
		if err != nil {
			return err
		}
		slices.Reverse(entries)
		if err := t.emitFresh(entries); err != nil {
			return err
		}
		if len(entries) < t.pageLimit {
			t.draining = false
			return nil
		}

		t.draining = true
		oldest, newest := entries[0].ts, entries[len(entries)-1].ts
		if oldest == newest {
			// A forward page starts at its oldest entry, so the whole page is
			// one nanosecond and scanFrom cannot reach past it any other way.
			t.log.Warn("tail: more entries share one timestamp than a page holds; skipping the rest of that timestamp",
				"timestamp", newest, "page_limit", t.pageLimit)
			t.scanFrom = newest + 1
		} else {
			t.scanFrom = newest
		}
	}
	return nil
}

// emitFresh emits the entries, oldest first, whose keys were not emitted
// before, advances the cursor and forgets keys behind the next window.
func (t *tailer) emitFresh(entries []logEntry) error {
	var fresh []map[string]any
	for _, entry := range entries {
		key := keyOf(entry)
		if _, ok := t.emitted[key]; ok {
			continue
		}
		t.emitted[key] = struct{}{}
		t.cursor = max(t.cursor, entry.ts)
		fresh = append(fresh, entry.row())
	}
	floor := t.windowStart()
	for key := range t.emitted {
		if key.ts < floor {
			delete(t.emitted, key)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	if err := t.emit(fresh); err != nil {
		return &tailEmitError{err: err}
	}
	return nil
}
