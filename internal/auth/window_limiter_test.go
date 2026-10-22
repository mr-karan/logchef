package auth

import (
	"hash/maphash"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"
)

func TestWindowLimiterFloodCannotResetBlockedKey(t *testing.T) {
	l := NewWindowLimiter(time.Hour, 1)
	if !l.Allow("victim") || l.Allow("victim") {
		t.Fatal("victim did not exhaust its budget")
	}
	for i := range 20_000 {
		l.Allow(strconv.Itoa(i))
	}
	if l.Allow("victim") {
		t.Fatal("flood restored a blocked key's budget before its window expired")
	}
}

func collidingLimiterKey(t *testing.T, l *WindowLimiter, key string) string {
	t.Helper()
	bucket := maphash.String(l.seed, key) % limiterBucketCount
	for i := range limiterBucketCount * 64 {
		candidate := strconv.Itoa(i)
		if candidate != key && maphash.String(l.seed, candidate)%limiterBucketCount == bucket {
			return candidate
		}
	}
	t.Fatal("could not find a colliding key")
	return ""
}

func TestWindowLimiterIsolatesDistinctBuckets(t *testing.T) {
	l := NewWindowLimiter(time.Minute, 1)
	const first = "first"
	firstBucket := maphash.String(l.seed, first) % limiterBucketCount
	var second string
	for i := range limiterBucketCount * 64 {
		key := strconv.Itoa(i)
		if maphash.String(l.seed, key)%limiterBucketCount != firstBucket {
			second = key
			break
		}
	}
	if second == "" {
		t.Fatal("could not find an independent bucket")
	}
	if !l.Allow(first) || l.Allow(first) {
		t.Fatal("first key did not exhaust its limit")
	}
	if !l.Allow(second) || l.Allow(second) {
		t.Fatal("distinct bucket did not keep an independent limit")
	}
}

func TestWindowLimiterCollisionCannotReopenWindow(t *testing.T) {
	l := NewWindowLimiter(time.Minute, 5)
	const first = "first"
	second := collidingLimiterKey(t, l, first)
	for range 2 {
		if !l.allowAt(first, 0) {
			t.Fatal("first key rejected before its limit")
		}
	}
	for range 3 {
		if !l.allowAt(second, 50*time.Second) {
			t.Fatal("colliding key rejected before the shared limit")
		}
	}
	for _, elapsed := range []time.Duration{50 * time.Second, time.Minute, 110 * time.Second} {
		if l.allowAt(second, elapsed) {
			t.Fatalf("collision reopened the bucket at %s", elapsed)
		}
	}
	if !l.allowAt(second, 2*time.Minute) {
		t.Fatal("expired counters did not release the bucket")
	}
}

func TestWindowLimiterLoneKeyResetsWithinBound(t *testing.T) {
	for _, start := range []time.Duration{0, time.Nanosecond, time.Minute - time.Nanosecond, time.Minute + time.Nanosecond} {
		l := NewWindowLimiter(time.Minute, 1)
		if !l.allowAt("key", start) || l.allowAt("key", start+time.Minute-time.Nanosecond) {
			t.Fatalf("key did not keep its configured window at offset %s", start)
		}
		if !l.allowAt("key", start+2*time.Minute) {
			t.Fatalf("lone key did not reset within two windows at offset %s", start)
		}
	}
}

func TestWindowLimiterCollisionsOnlyRestrictAdmissions(t *testing.T) {
	l := NewWindowLimiter(time.Minute, 5)
	keys := []string{"first", collidingLimiterKey(t, l, "first")}
	starts := make(map[string]time.Duration)
	counts := make(map[string]int)
	random := rand.New(rand.NewPCG(17, 31))
	var elapsed time.Duration
	for range 10_000 {
		elapsed += time.Duration(random.IntN(5000)) * time.Millisecond
		key := keys[random.IntN(len(keys))]
		if !l.allowAt(key, elapsed) {
			continue
		}
		if counts[key] == 0 || elapsed-starts[key] >= time.Minute {
			starts[key], counts[key] = elapsed, 0
		}
		counts[key]++
		if counts[key] > 5 {
			t.Fatalf("key %q exceeded its independent window at %s", key, elapsed)
		}
	}
}

func TestWindowLimiterSteadyCallerKeepsProgressing(t *testing.T) {
	l := NewWindowLimiter(time.Minute, 5)
	for i := range 100 {
		if !l.allowAt("key", time.Duration(i)*54*time.Second) {
			t.Fatalf("steady caller stopped progressing after request %d", i+1)
		}
	}
}
