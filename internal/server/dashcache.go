package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

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
	// Clamp the requested seconds BEFORE converting to a Duration: a
	// maliciously large JSON integer times time.Second (1e9) would overflow
	// int64 and wrap. Clamping the plain second count first keeps it bounded.
	secs := cd.TTLSeconds
	if maxTTL := s.config.DashboardCache.MaxTTL; maxTTL > 0 {
		if maxSecs := int(maxTTL / time.Second); secs > maxSecs {
			secs = maxSecs
		}
	}
	if secs <= 0 {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
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
// handled=false, err=nil only when caching is unavailable or the fill exceeded
// the buffered entry budget. The caller may then use its normal execution path.
// Other fill errors are returned unchanged for the caller's endpoint-specific
// error response. Callers must check err before deciding to fall back.
func (s *Server) tryServeDashboardCache(
	c *fiber.Ctx,
	key [32]byte,
	effTTL, fillTimeout time.Duration,
	fill func(ctx context.Context) ([]byte, error),
) (handled bool, err error) {
	if s.dashCache == nil || !s.dashCache.Enabled() {
		metrics.RecordDashboardCacheRequest("bypass")
		c.Set("X-Logchef-Cache", string(cache.StatusBypass))
		return false, nil
	}
	data, status, age, ferr := s.dashCache.GetOrFill(c.Context(), key, effTTL, fillTimeout, fill)
	if ferr != nil {
		metrics.RecordDashboardCacheRequest("bypass")
		c.Set("X-Logchef-Cache", string(cache.StatusBypass))
		if errors.Is(ferr, errCacheBudgetExceeded) {
			return false, nil
		}
		return false, ferr
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

type dashboardStreamError struct {
	err     error
	body    []byte
	queryID string
}

func (e *dashboardStreamError) Error() string { return e.err.Error() }
func (e *dashboardStreamError) Unwrap() error { return e.err }

// writeDashboardStreamError preserves the streaming response, including any
// partial rows, without executing the failed query again.
func writeDashboardStreamError(c *fiber.Ctx, err error) error {
	var admissionErr *QueryAdmissionError
	if errors.As(err, &admissionErr) {
		return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
	}
	var streamErr *dashboardStreamError
	if errors.As(err, &streamErr) {
		c.Set("X-LogChef-Query-ID", streamErr.queryID)
		c.Set(fiber.HeaderContentType, "application/json; charset=utf-8")
		return c.Status(fiber.StatusOK).Send(streamErr.body)
	}
	// No complete envelope is available when the cache times out before the
	// fill starts or encoding the execution error exceeds the buffer budget.
	// Nothing has been committed to the response here, so a real status is
	// still possible: status-driven consumers must not read a failure as
	// success.
	if errors.Is(err, context.Canceled) {
		return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
	}
	return SendErrorWithType(c, fiber.StatusInternalServerError, err.Error(), models.DatabaseErrorType)
}

// fillClickHouseStream returns a cache fill that buffers a ClickHouse query
// result into the exact streamed JSON envelope (via queryStreamWriter, so cached
// bytes are byte-identical to the streaming response), bounded by
// max_entry_bytes. On overflow it returns errCacheBudgetExceeded and the caller
// falls back to the unbuffered streaming path.
func (s *Server) fillClickHouseStream(userID models.UserID, teamID models.TeamID, sourceID models.SourceID, params datasource.QueryRequest, cfg queryStreamConfig) func(ctx context.Context) ([]byte, error) {
	return s.dashboardQueryFill(userID, teamID, sourceID, params.RawQuery, func(ctx context.Context, queryID string) ([]byte, error) {
		cb := &cappedBuffer{limit: s.config.DashboardCache.MaxEntryBytes}
		bw := bufio.NewWriter(cb)
		writer := newQueryStreamWriter(bw, cfg, queryID)
		if _, err := s.datasources.QueryLogsStream(ctx, sourceID, params, writer); err != nil {
			if errors.Is(err, errCacheBudgetExceeded) {
				return nil, err
			}
			if writeErr := writer.WriteError(err); writeErr != nil {
				// An execution failure must not become a budget fallback merely
				// because its error envelope also exceeds the buffer limit.
				return nil, err
			}
			return nil, &dashboardStreamError{err: err, body: cb.buf.Bytes(), queryID: queryID}
		}
		if err := bw.Flush(); err != nil {
			return nil, err
		}
		return cb.buf.Bytes(), nil
	})
}

// dashboardQueryFill admits only actual singleflight work, not hits or waiters,
// under the dashboard class so a panel refresh cannot starve the interactive
// preview budget.
func (s *Server) dashboardQueryFill(userID models.UserID, teamID models.TeamID, sourceID models.SourceID, query string, fill func(context.Context, string) ([]byte, error)) func(context.Context) ([]byte, error) {
	return func(ctx context.Context) ([]byte, error) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		maxPerUser, maxGlobal := s.admissionLimits(QueryClassDashboard)
		queryID, err := queryTracker.StartQuery(QueryClassDashboard, userID, sourceID, teamID, query, cancel,
			maxPerUser, maxGlobal)
		if err != nil {
			return nil, err
		}
		defer queryTracker.RemoveQuery(queryID)
		return fill(ctx, queryID)
	}
}
