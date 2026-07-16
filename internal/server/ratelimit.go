package server

import (
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

// windowLimiter is a dependency-free, single-key fixed-window rate limiter. It
// tracks a request count per key within the current window and lazily prunes
// keys whose window has expired so the map can't grow unbounded. It mirrors the
// pattern used by auth.LoginRateLimiter but limits on a single key at a time,
// which keeps the Fiber middleware wrappers below trivial.
type windowLimiter struct {
	mu     sync.Mutex
	window time.Duration
	limit  int
	keys   map[string]*limiterWindow
}

type limiterWindow struct {
	start time.Time
	count int
}

// newWindowLimiter creates a limiter allowing up to limit requests per key
// within each window.
func newWindowLimiter(window time.Duration, limit int) *windowLimiter {
	return &windowLimiter{
		window: window,
		limit:  limit,
		keys:   make(map[string]*limiterWindow),
	}
}

// Allow records a request for key and reports whether it is within the limit.
// An empty key is always allowed (nothing to bucket on). Stale windows are
// pruned lazily on every call.
func (l *windowLimiter) Allow(key string) bool {
	if key == "" {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	w, ok := l.keys[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.keys[key] = &limiterWindow{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= l.limit
}

// pruneLocked drops expired windows. Caller must hold l.mu.
func (l *windowLimiter) pruneLocked(now time.Time) {
	for k, w := range l.keys {
		if now.Sub(w.start) >= l.window {
			delete(l.keys, k)
		}
	}
}

// tooManyRequests writes the shared 429 response used by both limiter
// middlewares after recording the rejection metric for scope.
func tooManyRequests(c *fiber.Ctx, scope string) error {
	metrics.RecordRateLimitRejection(scope)
	return SendErrorWithType(c, fiber.StatusTooManyRequests, "Too many requests, please slow down", models.ValidationErrorType)
}

// authRateLimitMiddleware limits the unauthenticated auth/token endpoints per
// client IP within a one-minute window, and optionally enforces a global cap
// across all clients when globalPerMinute > 0.
func authRateLimitMiddleware(perIPPerMinute, globalPerMinute int) fiber.Handler {
	perIP := newWindowLimiter(time.Minute, perIPPerMinute)

	var global *windowLimiter
	if globalPerMinute > 0 {
		global = newWindowLimiter(time.Minute, globalPerMinute)
	}

	return func(c *fiber.Ctx) error {
		if global != nil && !global.Allow("global") {
			return tooManyRequests(c, "auth")
		}
		if !perIP.Allow(c.IP()) {
			return tooManyRequests(c, "auth")
		}
		return c.Next()
	}
}

// queryRateLimitMiddleware limits the authenticated query endpoints per user
// within a one-minute window. It keys on the authenticated user id (falling
// back to the client IP if the user context is somehow absent) and must run
// after requireAuth so the user is populated.
func queryRateLimitMiddleware(perUserPerMinute int) fiber.Handler {
	perUser := newWindowLimiter(time.Minute, perUserPerMinute)

	return func(c *fiber.Ctx) error {
		key := c.IP()
		if user, ok := c.Locals("user").(*models.User); ok && user != nil {
			key = "user:" + strconv.Itoa(int(user.ID))
		}
		if !perUser.Allow(key) {
			return tooManyRequests(c, "query")
		}
		return c.Next()
	}
}
