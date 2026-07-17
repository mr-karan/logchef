package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

// dashboardCacheScope is the only Scope value that enables result caching.
const dashboardCacheScope = "dashboard"

// errCacheBudgetExceeded aborts a buffered ClickHouse fill once the encoded
// response would exceed max_entry_bytes, so the handler falls back to the
// unbuffered streaming path (the OOM guardrail). It is only ever produced for
// the dashboard-directive path; the explorer streaming path never buffers.
var errCacheBudgetExceeded = errors.New("dashboard cache: response exceeds max_entry_bytes")

// dashboardCacheParams resolves the effective TTL for a request's cache
// directive and reports whether the request is eligible for the dashboard
// result cache. Eligible iff the cache is enabled, the directive opts into the
// "dashboard" scope, and the clamped TTL (min of requested TTL and max_ttl) is
// positive.
func (s *Server) dashboardCacheParams(cd *models.CacheDirective) (time.Duration, bool) {
	if s.dashCache == nil || !s.dashCache.Enabled() {
		return 0, false
	}
	if cd == nil || cd.Scope != dashboardCacheScope {
		return 0, false
	}
	eff := time.Duration(cd.TTLSeconds) * time.Second
	if maxTTL := s.config.DashboardCache.MaxTTL; maxTTL > 0 && eff > maxTTL {
		eff = maxTTL
	}
	if eff <= 0 {
		return 0, false
	}
	return eff, true
}

// canonCacheTime renders a time pointer as its canonical UTC RFC3339Nano form
// for the cache key ("" when absent), so equal instants always hash equally.
func canonCacheTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// writeCachedBytes writes an already-encoded JSON response body with the cache
// status header, adding Age on a HIT. The body is byte-identical to what the
// uncached path for the same backend would have produced.
func writeCachedBytes(c *fiber.Ctx, data []byte, status cache.Status, age time.Duration) error {
	c.Set("X-Logchef-Cache", string(status))
	if status == cache.StatusHit {
		c.Set("Age", strconv.Itoa(int(age.Seconds())))
	}
	c.Set(fiber.HeaderContentType, "application/json")
	return c.Status(fiber.StatusOK).Send(data)
}

// tryServeDashboardCache serves key from the dashboard cache, running fill under
// singleflight on a miss. It returns handled=true when it has written a
// response (HIT/MISS/COALESCED, or a served-but-uncached BYPASS), and
// handled=false when the caller must fall back to its normal execution path
// (the fill errored or exceeded the buffered entry budget); in that case the
// BYPASS header is already set.
func (s *Server) tryServeDashboardCache(
	c *fiber.Ctx,
	key [32]byte,
	effTTL, fillTimeout time.Duration,
	fill func(ctx context.Context) ([]byte, error),
) (handled bool, err error) {
	data, status, age, ferr := s.dashCache.GetOrFill(c.Context(), key, effTTL, fillTimeout, fill)
	if ferr != nil {
		metrics.RecordDashboardCacheRequest("bypass")
		c.Set("X-Logchef-Cache", string(cache.StatusBypass))
		return false, nil
	}
	return true, writeCachedBytes(c, data, status, age)
}

// cappedBuffer is an io.Writer that accumulates bytes up to limit, returning
// errCacheBudgetExceeded once a write would overflow the budget. It bounds the
// memory used while buffering a ClickHouse result for caching.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.limit {
		return 0, errCacheBudgetExceeded
	}
	return b.buf.Write(p)
}

// fillClickHouseStream returns a cache fill that buffers a ClickHouse query
// result into the exact streamed JSON envelope (via queryStreamWriter, so cached
// bytes are byte-identical to the streaming response), bounded by
// max_entry_bytes. On overflow it returns errCacheBudgetExceeded and the caller
// falls back to the unbuffered streaming path.
func (s *Server) fillClickHouseStream(sourceID models.SourceID, params datasource.QueryRequest, cfg queryStreamConfig) func(ctx context.Context) ([]byte, error) {
	return func(ctx context.Context) ([]byte, error) {
		cb := &cappedBuffer{limit: s.config.DashboardCache.MaxEntryBytes}
		bw := bufio.NewWriter(cb)
		writer := newQueryStreamWriter(bw, cfg, uuid.New().String())
		if _, err := s.datasources.QueryLogsStream(ctx, sourceID, params, writer); err != nil {
			return nil, err
		}
		if err := bw.Flush(); err != nil {
			return nil, err
		}
		return cb.buf.Bytes(), nil
	}
}
