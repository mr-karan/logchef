// Package cache implements the per-dashboard TTL result cache. It is a
// hand-rolled, dependency-light byte-bounded LRU with lazy + periodic TTL
// expiry, mirroring the style of internal/server/ratelimit.go (one mutex, plain
// stdlib containers). Stored values are the already-encoded JSON response bytes
// so hits replay verbatim without re-marshaling, and byte accounting is exact.
package cache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"io"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/mr-karan/logchef/internal/metrics"
)

// Status is the cache outcome for a single request, surfaced to the client via
// the X-Logchef-Cache response header.
type Status string

const (
	// StatusHit means the response was served from a live cache entry.
	StatusHit Status = "HIT"
	// StatusMiss means this caller executed the query and populated the cache.
	StatusMiss Status = "MISS"
	// StatusCoalesced means this caller shared an in-flight fill started by
	// another caller (stampede protection collapsed the duplicate query).
	StatusCoalesced Status = "COALESCED"
	// StatusBypass means the result was not cached (too large, or caching off).
	StatusBypass Status = "BYPASS"
)

// keyVersion is bumped when the canonical cache-key encoding changes so old
// entries can never collide with new ones.
const keyVersion = 1

// sweepInterval is the cadence of the background expiry sweep. TTL is primarily
// enforced lazily on Get; the sweep just reclaims memory from entries that are
// never read again.
const sweepInterval = time.Minute

// Config controls the dashboard result cache. It mirrors the
// [dashboard_cache] server config.
type Config struct {
	Enabled       bool
	DefaultTTL    time.Duration
	MaxTTL        time.Duration
	MaxBytes      int64
	MaxEntryBytes int
	MaxEntries    int
}

type entry struct {
	key       [32]byte
	data      []byte
	size      int
	storedAt  time.Time
	expiresAt time.Time
	lruElem   *list.Element
}

// Cache is a byte-bounded LRU + TTL cache of encoded JSON response bytes.
type Cache struct {
	cfg Config
	sf  singleflight.Group

	mu       sync.Mutex
	entries  map[[32]byte]*entry
	lru      *list.List // front = most recently used; Value is *entry
	curBytes int64

	stop     chan struct{}
	stopOnce sync.Once
}

// New builds a Cache and, when enabled, starts its background expiry sweep.
func New(cfg Config) *Cache {
	c := &Cache{
		cfg:     cfg,
		entries: make(map[[32]byte]*entry),
		lru:     list.New(),
		stop:    make(chan struct{}),
	}
	if cfg.Enabled {
		go c.sweepLoop()
	}
	return c
}

// Enabled reports whether the cache is active.
func (c *Cache) Enabled() bool { return c.cfg.Enabled }

// Close stops the background sweep. Safe to call multiple times.
func (c *Cache) Close() {
	c.stopOnce.Do(func() { close(c.stop) })
}

func (c *Cache) sweepLoop() {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.sweep()
		}
	}
}

func (c *Cache) sweep() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		if now.After(e.expiresAt) {
			c.removeLocked(e)
		}
	}
	c.updateGaugesLocked()
}

// Get returns the cached bytes and their age, applying lazy TTL expiry. ok is
// false on miss or expiry.
func (c *Cache) Get(key [32]byte) (data []byte, age time.Duration, ok bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.entries[key]
	if !found {
		return nil, 0, false
	}
	if now.After(e.expiresAt) {
		c.removeLocked(e)
		c.updateGaugesLocked()
		return nil, 0, false
	}
	c.lru.MoveToFront(e.lruElem)
	return e.data, now.Sub(e.storedAt), true
}

// set stores data under key with the given TTL, then evicts down to the byte
// and entry budgets (byte budget primary, entry count secondary).
func (c *Cache) set(key [32]byte, data []byte, ttl time.Duration) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[key]; ok {
		c.curBytes -= int64(e.size)
		e.data = data
		e.size = len(data)
		e.storedAt = now
		e.expiresAt = now.Add(ttl)
		c.curBytes += int64(e.size)
		c.lru.MoveToFront(e.lruElem)
	} else {
		e := &entry{key: key, data: data, size: len(data), storedAt: now, expiresAt: now.Add(ttl)}
		e.lruElem = c.lru.PushFront(e)
		c.entries[key] = e
		c.curBytes += int64(e.size)
	}
	c.evictLocked()
	c.updateGaugesLocked()
}

func (c *Cache) evictLocked() {
	for (c.cfg.MaxBytes > 0 && c.curBytes > c.cfg.MaxBytes) ||
		(c.cfg.MaxEntries > 0 && len(c.entries) > c.cfg.MaxEntries) {
		back := c.lru.Back()
		if back == nil {
			break
		}
		c.removeLocked(back.Value.(*entry))
		metrics.RecordDashboardCacheEviction()
	}
}

func (c *Cache) removeLocked(e *entry) {
	c.lru.Remove(e.lruElem)
	delete(c.entries, e.key)
	c.curBytes -= int64(e.size)
}

func (c *Cache) updateGaugesLocked() {
	metrics.SetDashboardCacheBytes(c.curBytes)
	metrics.SetDashboardCacheEntries(len(c.entries))
}

// GetOrFill returns a cached response or runs fill exactly once across all
// concurrent callers sharing key (stampede protection via singleflight). The
// fill runs under its OWN bounded context (fillTimeout) so a single caller
// disconnecting cannot cancel everyone's shared query; an individual waiter may
// still bail when its own reqCtx is cancelled. Successful results are cached
// only when they fit within MaxEntryBytes.
func (c *Cache) GetOrFill(
	reqCtx context.Context,
	key [32]byte,
	ttl, fillTimeout time.Duration,
	fill func(ctx context.Context) ([]byte, error),
) (data []byte, status Status, age time.Duration, err error) {
	if d, a, ok := c.Get(key); ok {
		metrics.RecordDashboardCacheRequest("hit")
		return d, StatusHit, a, nil
	}

	filled := false
	//nolint:contextcheck // the shared fill runs under its OWN bounded context by
	// design, so one caller disconnecting cannot cancel everyone's query.
	fn := func() (interface{}, error) {
		fillCtx, cancel := context.WithTimeout(context.Background(), fillTimeout)
		defer cancel()
		b, e := fill(fillCtx)
		if e != nil {
			return nil, e
		}
		filled = true
		if len(b) <= c.cfg.MaxEntryBytes {
			c.set(key, b, ttl)
		}
		return b, nil
	}

	ch := c.sf.DoChan(string(key[:]), fn)
	select {
	case <-reqCtx.Done():
		return nil, StatusBypass, 0, reqCtx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, StatusBypass, 0, res.Err
		}
		data = res.Val.([]byte)
		if !filled {
			metrics.RecordDashboardCacheRequest("coalesced")
			return data, StatusCoalesced, 0, nil
		}
		if len(data) > c.cfg.MaxEntryBytes {
			metrics.RecordDashboardCacheRequest("bypass")
			return data, StatusBypass, 0, nil
		}
		metrics.RecordDashboardCacheRequest("miss")
		return data, StatusMiss, 0, nil
	}
}

// KeyInput is the deterministic set of fields that identify a cacheable result.
// Fields are hashed in declaration order; see ComputeKey.
type KeyInput struct {
	EndpointKind     string // "logs" | "histogram" | "logchefql-logs"
	TeamID           int64
	SourceID         int64
	SourceRevision   int64 // source.UpdatedAt unix-nanos; invalidates on source change
	EffTTLSeconds    int64
	Language         string
	FinalizedQuery   string // exact executable query, AFTER substitution + compilation
	CanonicalStart   string
	CanonicalEnd     string
	Timezone         string
	EffectiveLimit   int64
	HistogramWindow  string
	HistogramGroupBy string
	QueryTimeoutSecs int64
}

// ComputeKey returns the SHA-256 of the canonical, length-prefixed encoding of
// in. Length-prefixing every field makes the encoding unambiguous even when a
// query string contains the field separator bytes. The finalized query is
// hashed verbatim (no whitespace/case normalization) per the frozen contract.
func ComputeKey(in KeyInput) [32]byte {
	h := sha256.New()
	writeInt(h, keyVersion)
	writeStr(h, in.EndpointKind)
	writeInt(h, in.TeamID)
	writeInt(h, in.SourceID)
	writeInt(h, in.SourceRevision)
	writeInt(h, in.EffTTLSeconds)
	writeStr(h, in.Language)
	writeStr(h, in.FinalizedQuery)
	writeStr(h, in.CanonicalStart)
	writeStr(h, in.CanonicalEnd)
	writeStr(h, in.Timezone)
	writeInt(h, in.EffectiveLimit)
	writeStr(h, in.HistogramWindow)
	writeStr(h, in.HistogramGroupBy)
	writeInt(h, in.QueryTimeoutSecs)

	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func writeStr(h hash.Hash, s string) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(len(s)))
	_, _ = h.Write(b[:])
	_, _ = io.WriteString(h, s)
}

func writeInt(h hash.Hash, n int64) {
	var b [8]byte
	//nolint:gosec // deterministic hash encoding; two's-complement wraparound is intentional.
	binary.LittleEndian.PutUint64(b[:], uint64(n))
	_, _ = h.Write(b[:])
}
