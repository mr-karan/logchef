package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{
		Enabled:       true,
		DefaultTTL:    time.Minute,
		MaxTTL:        time.Hour,
		MaxBytes:      1 << 20,
		MaxEntryBytes: 1 << 20,
		MaxEntries:    1024,
	}
}

func keyN(n byte) [32]byte {
	var k [32]byte
	k[0] = n
	return k
}

func TestCacheGetSetHitAndAge(t *testing.T) {
	c := New(testConfig())
	defer c.Close()

	k := keyN(1)
	if _, _, ok := c.Get(k); ok {
		t.Fatal("expected miss on empty cache")
	}
	c.set(k, []byte("hello"), time.Minute)

	data, age, ok := c.Get(k)
	if !ok {
		t.Fatal("expected hit after set")
	}
	if string(data) != "hello" {
		t.Errorf("data = %q, want hello", data)
	}
	if age < 0 || age > time.Second {
		t.Errorf("age = %s, want ~0", age)
	}
}

func TestCacheTTLExpiryLazy(t *testing.T) {
	c := New(testConfig())
	defer c.Close()

	k := keyN(2)
	c.set(k, []byte("v"), 20*time.Millisecond)
	if _, _, ok := c.Get(k); !ok {
		t.Fatal("expected hit before expiry")
	}
	time.Sleep(40 * time.Millisecond)
	if _, _, ok := c.Get(k); ok {
		t.Fatal("expected miss after TTL expiry (lazy)")
	}
	// Expired entry must be reclaimed, not just hidden.
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("entries = %d, want 0 after lazy expiry", n)
	}
}

func TestCacheEntryCountEviction(t *testing.T) {
	cfg := testConfig()
	cfg.MaxEntries = 2
	c := New(cfg)
	defer c.Close()

	c.set(keyN(1), []byte("a"), time.Minute)
	c.set(keyN(2), []byte("b"), time.Minute)
	// Touch key 1 so it becomes most-recently-used; key 2 is now the LRU victim.
	if _, _, ok := c.Get(keyN(1)); !ok {
		t.Fatal("expected key1 hit")
	}
	c.set(keyN(3), []byte("c"), time.Minute)

	if _, _, ok := c.Get(keyN(2)); ok {
		t.Error("expected key2 evicted (LRU)")
	}
	if _, _, ok := c.Get(keyN(1)); !ok {
		t.Error("expected key1 retained (recently used)")
	}
	if _, _, ok := c.Get(keyN(3)); !ok {
		t.Error("expected key3 retained (newest)")
	}
}

func TestCacheByteBudgetEviction(t *testing.T) {
	cfg := testConfig()
	cfg.MaxBytes = 10
	cfg.MaxEntries = 1000
	c := New(cfg)
	defer c.Close()

	c.set(keyN(1), make([]byte, 6), time.Minute)
	c.set(keyN(2), make([]byte, 6), time.Minute) // 12 > 10 => evict key1

	if _, _, ok := c.Get(keyN(1)); ok {
		t.Error("expected key1 evicted by byte budget")
	}
	if _, _, ok := c.Get(keyN(2)); !ok {
		t.Error("expected key2 retained")
	}
	c.mu.Lock()
	cur := c.curBytes
	c.mu.Unlock()
	if cur > cfg.MaxBytes {
		t.Errorf("curBytes = %d, exceeds budget %d", cur, cfg.MaxBytes)
	}
}

func TestCacheBypassOversizedEntry(t *testing.T) {
	cfg := testConfig()
	cfg.MaxEntryBytes = 4
	c := New(cfg)
	defer c.Close()

	data, status, _, err := c.GetOrFill(context.Background(), keyN(9), time.Minute, time.Second,
		func(context.Context) ([]byte, error) { return []byte("way too big"), nil })
	if err != nil {
		t.Fatalf("GetOrFill: %v", err)
	}
	if status != StatusBypass {
		t.Errorf("status = %s, want BYPASS", status)
	}
	if string(data) != "way too big" {
		t.Errorf("data = %q, want the fill result served through", data)
	}
	if _, _, ok := c.Get(keyN(9)); ok {
		t.Error("oversized entry must not be cached")
	}
}

func TestCacheSingleflightCollapse(t *testing.T) {
	c := New(testConfig())
	defer c.Close()

	const n = 50
	var fillCount int32
	release := make(chan struct{})
	start := make(chan struct{})

	var mu sync.Mutex
	statuses := make(map[Status]int)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			data, status, _, err := c.GetOrFill(context.Background(), keyN(7), time.Minute, 5*time.Second,
				func(context.Context) ([]byte, error) {
					atomic.AddInt32(&fillCount, 1)
					<-release // hold the fill open until all callers have coalesced
					return []byte("filled"), nil
				})
			if err != nil {
				t.Errorf("GetOrFill: %v", err)
				return
			}
			if string(data) != "filled" {
				t.Errorf("data = %q, want filled", data)
			}
			mu.Lock()
			statuses[status]++
			mu.Unlock()
		}()
	}

	close(start)
	time.Sleep(75 * time.Millisecond) // let all callers coalesce onto the one leader
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&fillCount); got != 1 {
		t.Fatalf("fill executed %d times, want exactly 1 (singleflight collapse)", got)
	}
	if statuses[StatusMiss] != 1 {
		t.Errorf("MISS count = %d, want 1", statuses[StatusMiss])
	}
	if statuses[StatusCoalesced] != n-1 {
		t.Errorf("COALESCED count = %d, want %d", statuses[StatusCoalesced], n-1)
	}
}

func TestComputeKeyDeterministicAndDistinct(t *testing.T) {
	base := KeyInput{
		EndpointKind:   "logs",
		TeamID:         1,
		SourceID:       2,
		SourceRevision: 100,
		EffTTLSeconds:  600,
		Language:       "clickhouse-sql",
		FinalizedQuery: "SELECT 1",
		CanonicalStart: "2026-01-01T00:00:00Z",
		CanonicalEnd:   "2026-01-02T00:00:00Z",
		Timezone:       "UTC",
		EffectiveLimit: 1000,
	}
	first := ComputeKey(base)
	second := ComputeKey(base)
	if first != second {
		t.Fatal("ComputeKey is not deterministic")
	}

	// Each field must change the key.
	mutators := []func(*KeyInput){
		func(k *KeyInput) { k.EndpointKind = "histogram" },
		func(k *KeyInput) { k.TeamID = 99 },
		func(k *KeyInput) { k.SourceID = 99 },
		func(k *KeyInput) { k.SourceRevision = 999 },
		func(k *KeyInput) { k.EffTTLSeconds = 60 },
		func(k *KeyInput) { k.Language = "logsql" },
		func(k *KeyInput) { k.FinalizedQuery = "SELECT 2" },
		func(k *KeyInput) { k.CanonicalStart = "2026-02-01T00:00:00Z" },
		func(k *KeyInput) { k.CanonicalEnd = "2026-02-02T00:00:00Z" },
		func(k *KeyInput) { k.Timezone = "Asia/Kolkata" },
		func(k *KeyInput) { k.EffectiveLimit = 500 },
		func(k *KeyInput) { k.HistogramWindow = "5m" },
		func(k *KeyInput) { k.HistogramGroupBy = "level" },
		func(k *KeyInput) { k.QueryTimeoutSecs = 30 },
	}
	baseKey := ComputeKey(base)
	for i, m := range mutators {
		in := base
		m(&in)
		if ComputeKey(in) == baseKey {
			t.Errorf("mutator %d did not change the cache key", i)
		}
	}

	// Length-prefixing must prevent field-boundary collisions.
	a := base
	a.FinalizedQuery = "ab"
	a.CanonicalStart = "c"
	b := base
	b.FinalizedQuery = "a"
	b.CanonicalStart = "bc"
	if ComputeKey(a) == ComputeKey(b) {
		t.Error("adjacent fields collided; length-prefixing is broken")
	}
}
