package datasource

import (
	"strings"
	"testing"
	"time"
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

	withFilter := buildTailPollSQL("default.http", "timestamp", "status = 500", cursor)
	if !strings.Contains(withFilter, "SELECT * FROM default.http WHERE `timestamp` >= toDateTime64('2026-07-07 10:30:00', 9, 'UTC')") {
		t.Fatalf("unexpected SQL: %s", withFilter)
	}
	if !strings.Contains(withFilter, "AND (status = 500)") {
		t.Fatalf("expected filter clause in SQL: %s", withFilter)
	}
	if !strings.HasSuffix(withFilter, "ORDER BY `timestamp` ASC") {
		t.Fatalf("expected ascending sort in SQL: %s", withFilter)
	}

	noFilter := buildTailPollSQL("default.http", "timestamp", "", cursor)
	if strings.Contains(noFilter, "AND (") {
		t.Fatalf("expected no filter clause when filter is empty: %s", noFilter)
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
