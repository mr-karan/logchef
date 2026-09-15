package datasource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

func tsRow(ts time.Time, msg string) map[string]any {
	return map[string]any{"timestamp": ts, "_msg": msg}
}

func TestTailDedupProcess(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	dedup := newTailDedup()

	// First poll: two rows, both fresh.
	fresh, newest := dedup.process([]map[string]any{
		tsRow(base, "a"),
		tsRow(base.Add(time.Second), "b"),
	}, "timestamp")
	if len(fresh) != 2 {
		t.Fatalf("first poll: expected 2 fresh rows, got %d", len(fresh))
	}
	if !newest.Equal(base.Add(time.Second)) {
		t.Fatalf("first poll: expected newest %v, got %v", base.Add(time.Second), newest)
	}

	// Second poll re-fetches the boundary row "b" (inclusive >= cursor) plus a
	// new row "c" sharing the same boundary second. "b" is deduped; "c" is fresh.
	fresh, newest = dedup.process([]map[string]any{
		tsRow(base.Add(time.Second), "b"),
		tsRow(base.Add(time.Second), "c"),
	}, "timestamp")
	if len(fresh) != 1 {
		t.Fatalf("second poll: expected 1 fresh row, got %d (%v)", len(fresh), fresh)
	}
	if fresh[0]["_msg"] != "c" {
		t.Fatalf("second poll: expected fresh row 'c', got %v", fresh[0]["_msg"])
	}
	if !newest.Equal(base.Add(time.Second)) {
		t.Fatalf("second poll: expected newest %v, got %v", base.Add(time.Second), newest)
	}
}

func TestTailDedupEvictBefore(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	dedup := newTailDedup()
	dedup.process([]map[string]any{
		tsRow(base, "a"),
		tsRow(base.Add(2*time.Second), "b"),
	}, "timestamp")

	// Advance cursor past "a"; only the boundary row "b" should be retained.
	dedup.evictBefore(base.Add(2 * time.Second))
	if len(dedup.seen) != 1 {
		t.Fatalf("expected 1 retained key after eviction, got %d", len(dedup.seen))
	}

	// Re-feeding "a" now surfaces it as fresh (its dedup key was evicted), while
	// "b" is still deduped.
	fresh, _ := dedup.process([]map[string]any{
		tsRow(base, "a"),
		tsRow(base.Add(2*time.Second), "b"),
	}, "timestamp")
	if len(fresh) != 1 || fresh[0]["_msg"] != "a" {
		t.Fatalf("expected only 'a' fresh after eviction, got %v", fresh)
	}
}

func TestTailDedupKeyDistinguishesValues(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	// Same fields regardless of map iteration order collide.
	k1 := tailDedupKey(map[string]any{"a": "1", "b": "2"}, ts)
	k2 := tailDedupKey(map[string]any{"b": "2", "a": "1"}, ts)
	if k1 != k2 {
		t.Fatalf("expected identical rows to share a key, got %q vs %q", k1, k2)
	}
	// A differing value yields a different key.
	k3 := tailDedupKey(map[string]any{"a": "1", "b": "3"}, ts)
	if k1 == k3 {
		t.Fatalf("expected differing values to yield different keys")
	}
	// A differing timestamp yields a different key.
	k4 := tailDedupKey(map[string]any{"a": "1", "b": "2"}, ts.Add(time.Nanosecond))
	if k1 == k4 {
		t.Fatalf("expected differing timestamps to yield different keys")
	}
}

func TestBuildTailPollSQL(t *testing.T) {
	t.Parallel()

	cursor := time.Date(2026, 7, 7, 10, 30, 0, 0, time.UTC)
	upperBound := cursor.Add(time.Minute)

	withFilter := buildTailPollSQL("default.http", "timestamp", "status = 500", cursor, upperBound)
	if !strings.Contains(withFilter, "SELECT * FROM default.http WHERE `timestamp` >= toDateTime64('2026-07-07 10:30:00', 9, 'UTC')") {
		t.Fatalf("unexpected SQL: %s", withFilter)
	}
	if !strings.Contains(withFilter, "AND `timestamp` <= toDateTime64('2026-07-07 10:31:00', 9, 'UTC')") {
		t.Fatalf("expected frozen upper bound in SQL: %s", withFilter)
	}
	if !strings.Contains(withFilter, "AND (status = 500)") {
		t.Fatalf("expected filter clause in SQL: %s", withFilter)
	}
	if !strings.HasSuffix(withFilter, "ORDER BY `timestamp` ASC, cityHash64(toJSONString(tuple(*))) ASC, toJSONString(tuple(*)) ASC") {
		t.Fatalf("expected ascending sort in SQL: %s", withFilter)
	}

	noFilter := buildTailPollSQL("default.http", "timestamp", "", cursor, upperBound)
	if strings.Contains(noFilter, "AND (") {
		t.Fatalf("expected no filter clause when filter is empty: %s", noFilter)
	}
}

func TestTailPollWindowStart(t *testing.T) {
	t.Parallel()

	sessionStart := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	margin := 5 * time.Second

	// At session start the cursor equals sessionStart: cursor-margin is before
	// sessionStart, so the window clamps to sessionStart rather than reaching
	// into history from before the tail began.
	if got := tailPollWindowStart(sessionStart, sessionStart, margin); !got.Equal(sessionStart) {
		t.Fatalf("at session start: got %v, want %v (clamped)", got, sessionStart)
	}

	// Once the cursor has advanced well past sessionStart+margin, the window is
	// simply cursor-margin.
	cursor := sessionStart.Add(30 * time.Second)
	want := cursor.Add(-margin)
	if got := tailPollWindowStart(sessionStart, cursor, margin); !got.Equal(want) {
		t.Fatalf("advanced cursor: got %v, want %v", got, want)
	}

	// Just past the clamp boundary: cursor-margin lands exactly on sessionStart.
	cursor = sessionStart.Add(margin)
	if got := tailPollWindowStart(sessionStart, cursor, margin); !got.Equal(sessionStart) {
		t.Fatalf("boundary cursor: got %v, want %v", got, sessionStart)
	}

	// Zero margin: window is exactly the cursor (no re-scan), matching the old
	// cursor-only behavior.
	cursor = sessionStart.Add(time.Minute)
	if got := tailPollWindowStart(sessionStart, cursor, 0); !got.Equal(cursor) {
		t.Fatalf("zero margin: got %v, want %v", got, cursor)
	}
}

// TestTailBoundaryOverflowRecoveredViaMarginAndDedup covers a full page at a
// shared timestamp. The forward drain uses a fixed lower bound and OFFSET,
// while the dedup set filters overlap rows when the next scan cycle begins.
// This simulates a poll LIMIT of 2 cutting a 3-row tied timestamp short, then
// recovering the missed row on the next page.
func TestTailBoundaryOverflowRecoveredViaMarginAndDedup(t *testing.T) {
	t.Parallel()

	const simulatedBatchLimit = 2
	tsField := "timestamp"
	boundary := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	sessionStart := boundary.Add(-time.Minute)
	margin := 5 * time.Second

	all := []map[string]any{
		tsRow(boundary, "a"),
		tsRow(boundary, "b"),
		tsRow(boundary, "c"),
	}

	dedup := newTailDedup()

	// First poll: LIMIT cuts the tie short at 2 of the 3 tied rows (as
	// ORDER BY timestamp ASC LIMIT simulatedBatchLimit would in ClickHouse).
	firstBatch := all[:simulatedBatchLimit]
	fresh1, newest1 := dedup.process(firstBatch, tsField)
	if len(fresh1) != 2 {
		t.Fatalf("first poll: expected 2 fresh rows, got %d", len(fresh1))
	}
	if !newest1.Equal(boundary) {
		t.Fatalf("first poll: expected cursor to advance to %v, got %v", boundary, newest1)
	}
	cursor := newest1
	dedup.evictBefore(tailPollWindowStart(sessionStart, cursor, margin))

	// The batch hit the limit, so TailLogs keeps the same lower bound and advances
	// the page offset instead of waiting for the next tick. Because all three
	// rows share the exact boundary timestamp, the next page includes the missed
	// row without relying on timestamp precision as a tie-break.
	windowStart := tailPollWindowStart(sessionStart, cursor, margin)
	if windowStart.After(boundary) {
		t.Fatalf("re-poll window %v must not be after the boundary %v", windowStart, boundary)
	}
	fresh2, newest2 := dedup.process(all, tsField)
	if !newest2.Equal(boundary) {
		t.Fatalf("second poll: expected cursor to stay at %v, got %v", boundary, newest2)
	}
	if len(fresh2) != 1 || fresh2[0]["_msg"] != "c" {
		t.Fatalf("second poll: expected only the missed row 'c' to surface fresh, got %v", fresh2)
	}

	// Across both polls, every row was emitted exactly once.
	seenMsgs := map[string]bool{}
	for _, row := range append(fresh1, fresh2...) {
		msg := row["_msg"].(string)
		if seenMsgs[msg] {
			t.Fatalf("row %q emitted more than once", msg)
		}
		seenMsgs[msg] = true
	}
	if len(seenMsgs) != 3 {
		t.Fatalf("expected all 3 rows emitted across both polls, got %d: %v", len(seenMsgs), seenMsgs)
	}
}

func TestExtractRowTime(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	if got := extractRowTime(want); !got.Equal(want) {
		t.Fatalf("time.Time passthrough: got %v", got)
	}
	if got := extractRowTime("2026-07-07T10:00:00Z"); !got.Equal(want) {
		t.Fatalf("RFC3339 string: got %v", got)
	}
	if got := extractRowTime("2026-07-07 10:00:00"); !got.Equal(want) {
		t.Fatalf("space-separated string: got %v", got)
	}
	if got := extractRowTime(nil); !got.IsZero() {
		t.Fatalf("nil: expected zero time, got %v", got)
	}
}

func TestTailDedupIsBounded(t *testing.T) {
	t.Parallel()

	dedup := newTailDedup()
	base := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	rows := make([]map[string]any, tailMaxDedupEntries+1)
	for i := range rows {
		rows[i] = tsRow(base, fmt.Sprintf("row-%d", i))
	}
	dedup.process(rows, "timestamp")
	if len(dedup.seen) != tailMaxDedupEntries || len(dedup.order) != tailMaxDedupEntries {
		t.Fatalf("dedup size = map:%d order:%d, want %d", len(dedup.seen), len(dedup.order), tailMaxDedupEntries)
	}
}

func TestTailLogsDrainsEqualTimestampRowsClickHouse(t *testing.T) {
	runTailProgressClickHouse(t, false)
}

func TestTailLogsDrainsNativeJSONRowsClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	setupConn, err := ch.Open(&ch.Options{Addr: []string{addr}, Protocol: ch.Native})
	if err != nil {
		t.Fatal(err)
	}
	defer setupConn.Close()
	var version string
	if err := setupConn.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(version, ".")
	major, minor := 0, 0
	if len(parts) >= 2 {
		major, _ = strconv.Atoi(parts[0])
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < 25 || major == 25 && minor < 3 {
		t.Skipf("native JSON requires ClickHouse >= 25.3, found %s", version)
	}
	runTailProgressWithConnection(t, addr, ctx, cancel, setupConn, log, true)
}

func runTailProgressClickHouse(t *testing.T, includeJSON bool) {
	t.Helper()
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	setupConn, err := ch.Open(&ch.Options{Addr: []string{addr}, Protocol: ch.Native})
	if err != nil {
		t.Fatal(err)
	}
	defer setupConn.Close()
	runTailProgressWithConnection(t, addr, ctx, cancel, setupConn, log, includeJSON)
}

func runTailProgressWithConnection(t *testing.T, addr string, ctx context.Context, cancel context.CancelFunc, setupConn ch.Conn, log *slog.Logger, includeJSON bool) {
	t.Helper()

	table := fmt.Sprintf("tail_progress_%d", time.Now().UnixNano())
	payloadColumn := ""
	payloadValue := ""
	if includeJSON {
		payloadColumn = ", payload JSON"
		payloadValue = ", CAST(concat('{\"number\":', toString(number), '}'), 'JSON')"
	}
	if err := setupConn.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (ts DateTime64(9, 'UTC'), msg String, attrs Map(String, String)%s) ENGINE=MergeTree ORDER BY (ts, msg)", table, payloadColumn)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cleanupCancel()
		_ = setupConn.Exec(cleanupCtx, "DROP TABLE "+table)
	}()
	if err := setupConn.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT now64(9, 'UTC') + INTERVAL 1 SECOND, toString(number), map('n', toString(number))%s FROM numbers(8000)", table, payloadValue)); err != nil {
		t.Fatal(err)
	}

	manager := clickhouse.NewManager(log)
	defer manager.Close()
	source := &models.Source{
		ID:          1,
		MetaTSField: "ts",
		Connection:  models.ConnectionInfo{Host: addr, Database: "default", TableName: table},
	}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := NewClickHouseProvider(manager, log)
	var emitted []map[string]any
	lateInserted := false
	err := provider.TailLogs(ctx, source, TailRequest{PollInterval: 10 * time.Millisecond, LookbackMargin: 5 * time.Second}, func(rows []map[string]any) error {
		emitted = append(emitted, rows...)
		if len(emitted) >= tailBatchLimit && !lateInserted {
			lateInserted = true
			latePayload := ""
			if includeJSON {
				latePayload = ", CAST('{\"number\":8000}', 'JSON')"
			}
			if err := setupConn.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT min(ts), 'late', map('n', 'late')%s FROM %s", table, latePayload, table)); err != nil {
				return err
			}
		}
		if len(emitted) >= 8001 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("TailLogs error = %v, want context.Canceled", err)
	}
	if len(emitted) != 8001 {
		t.Fatalf("emitted %d rows, want 8001 including late insert", len(emitted))
	}
	seen := make(map[string]struct{}, len(emitted))
	for _, row := range emitted {
		msg, ok := row["msg"].(string)
		if !ok {
			t.Fatalf("row msg has type %T", row["msg"])
		}
		seen[msg] = struct{}{}
	}
	if len(seen) != 8001 {
		t.Fatalf("emitted %d distinct rows, want 8001", len(seen))
	}
}

// BenchmarkTailDrainOffsetClickHouse records actual ClickHouse query costs at
// representative drain offsets. It is evidence for evaluating OFFSET's cost,
// not a claim that OFFSET is faster than another pagination strategy.
func BenchmarkTailDrainOffsetClickHouse(b *testing.B) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		b.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	setupConn, err := ch.Open(&ch.Options{Addr: []string{addr}, Protocol: ch.Native})
	if err != nil {
		b.Fatal(err)
	}
	defer setupConn.Close()
	table := fmt.Sprintf("tail_benchmark_%d", time.Now().UnixNano())
	if err := setupConn.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (ts DateTime64(9, 'UTC'), msg String, attrs Map(String, String)) ENGINE=MergeTree ORDER BY (ts, msg)", table)); err != nil {
		b.Fatal(err)
	}
	defer setupConn.Exec(context.Background(), "DROP TABLE "+table)
	if err := setupConn.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT now64(9, 'UTC') + INTERVAL 1 SECOND, toString(number), map('n', toString(number)) FROM numbers(8000)", table)); err != nil {
		b.Fatal(err)
	}
	client, err := clickhouse.NewClient(clickhouse.ClientOptions{Host: addr}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()
	source := &models.Source{Connection: models.ConnectionInfo{Database: "default", TableName: table}}
	cursor := time.Now().UTC().Add(-time.Minute)
	for _, offset := range []int{0, 4000, 7000} {
		b.Run(fmt.Sprintf("offset_%d", offset), func(b *testing.B) {
			timeout := tailPollTimeoutSeconds
			query := buildTailPollSQL(source.GetFullTableName(), "ts", "", cursor, time.Now().UTC().Add(time.Minute))
			qb := clickhouse.NewExtendedQueryBuilder(source.GetFullTableName(), tailBatchLimit)
			built, err := qb.BuildRawQueryWithLimitPolicy(query, tailBatchLimit, tailBatchLimit, tailBatchLimit)
			if err != nil {
				b.Fatal(err)
			}
			if offset > 0 {
				built.SQL = fmt.Sprintf("%s OFFSET %d", built.SQL, offset)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := client.QueryWithOptions(ctx, built.SQL, clickhouse.QueryOptions{
					TimeoutSeconds: &timeout,
					LimitApplied:   built.AppliedLimit,
					MaxRows:        built.AppliedLimit,
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
