package auth

import (
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

func TestWindowLimiterIsolatesDistinctBuckets(t *testing.T) {
	l := NewWindowLimiter(time.Minute, 1)
	const first = "first"
	var second string
	for i := range limiterBucketCount * 64 {
		key := strconv.Itoa(i)
		if l.bucket(key) != l.bucket(first) {
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
	l := NewGlobalWindowLimiter(time.Minute, 5)
	for range 5 {
		if !l.allowAt("victim", 0) {
			t.Fatal("victim rejected early")
		}
	}
	for _, elapsed := range []time.Duration{0, time.Minute} {
		if l.allowAt("collision", elapsed) {
			t.Fatalf("collision reopened exhausted estimate at %s", elapsed)
		}
	}
	for range 3 {
		if !l.allowAt("collision", 90*time.Second) {
			t.Fatal("weighted previous count did not release shared capacity")
		}
	}
	if l.allowAt("victim", 90*time.Second) {
		t.Fatal("victim admitted with a full combined estimate")
	}
	if !l.allowAt("victim", 2*time.Minute) {
		t.Fatal("expired previous counter did not release capacity")
	}
}

func TestWindowLimiterLoneKeyResetsWithinBound(t *testing.T) {
	for _, start := range []time.Duration{0, time.Nanosecond, time.Minute - time.Nanosecond, time.Minute + time.Nanosecond} {
		l := NewWindowLimiter(time.Minute, 1)
		if !l.allowAt("key", start) || l.allowAt("key", start) {
			t.Fatalf("key did not exhaust its budget at %s", start)
		}
		if !l.allowAt("key", start+time.Minute+time.Nanosecond) {
			t.Fatalf("lone key did not recover within one window at %s", start)
		}
	}
}

func TestWindowLimiterCollisionsPreserveAdmissionBounds(t *testing.T) {
	l := NewGlobalWindowLimiter(time.Minute, 5)
	type counts struct {
		epoch             int64
		current, previous int
	}
	histories := map[string]*counts{"first": {}, "second": {}}
	random := rand.New(rand.NewPCG(17, 31))
	var elapsed time.Duration
	for range 10_000 {
		elapsed += time.Duration(random.IntN(5000)) * time.Millisecond
		key := []string{"first", "second"}[random.IntN(2)]
		h := histories[key]
		epoch := int64(elapsed / time.Minute)
		if epoch != h.epoch {
			h.previous = 0
			if epoch-h.epoch == 1 {
				h.previous = h.current
			}
			h.current = 0
			h.epoch = epoch
		}
		ownEstimate := float64(h.current) + float64(h.previous)*float64(time.Minute-elapsed%time.Minute)/float64(time.Minute)
		if l.allowAt(key, elapsed) {
			if ownEstimate >= 5 {
				t.Fatalf("collision admitted %q with lone-key estimate %f at %s", key, ownEstimate, elapsed)
			}
			h.current++
		}
	}
}

func TestWindowLimiterExpiresFloodedBuckets(t *testing.T) {
	l := NewWindowLimiter(time.Second, 5)
	l.allowAt("stale", 0)
	for i := range 10_000 {
		l.allowAt(strconv.Itoa(i), 0)
	}
	for _, key := range []string{"fresh", "stale"} {
		if !l.allowAt(key, 2*time.Second) {
			t.Fatalf("expired bucket rejected %q", key)
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

func TestWindowLimiterCollidingAdmittedRate(t *testing.T) {
	for _, limit := range []int{120, 300} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			l := NewGlobalWindowLimiter(time.Minute, limit)
			total := 100 * limit
			admitted := 0
			for i := range total {
				key := []string{"first", "second"}[i%2]
				if l.allowAt(key, time.Duration(i)*time.Minute/time.Duration(limit)) {
					admitted++
				}
			}
			if admitted*100 < total*99 {
				t.Fatalf("colliding keys admitted %d/%d requests, want at least 99%% at the shared limit", admitted, total)
			}
		})
	}
}

func TestGlobalWindowLimiterSharesBudget(t *testing.T) {
	l := NewGlobalWindowLimiter(time.Minute, 1)
	if l.buckets != nil {
		t.Fatal("global limiter allocated keyed buckets")
	}
	if !l.allowAt("first", 0) {
		t.Fatal("first global request rejected")
	}
	for i := range 10_000 {
		if l.allowAt(strconv.Itoa(i), 0) {
			t.Fatal("new key bypassed the global budget")
		}
	}
	if !l.allowAt("fresh", time.Minute+time.Nanosecond) {
		t.Fatal("global budget did not recover after one window")
	}
}
