package server

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

func newWindowLimiter(window time.Duration, limit int) *auth.WindowLimiter {
	return auth.NewWindowLimiter(window, limit)
}

// tooManyRequests writes the shared 429 response used by both limiter
// middlewares after recording the rejection metric for scope.
func tooManyRequests(c fiber.Ctx, scope string) error {
	metrics.RecordRateLimitRejection(scope)
	return SendErrorWithType(c, fiber.StatusTooManyRequests, "Too many requests, please slow down", models.ValidationErrorType)
}

// authRateLimitMiddleware limits the unauthenticated auth/token endpoints per
// client IP within a one-minute window, and optionally enforces a global cap
// across all clients when globalPerMinute > 0.
func authRateLimitMiddleware(perIPPerMinute, globalPerMinute int) fiber.Handler {
	perIP := newWindowLimiter(time.Minute, perIPPerMinute)

	var global *auth.WindowLimiter
	if globalPerMinute > 0 {
		global = newWindowLimiter(time.Minute, globalPerMinute)
	}

	return func(c fiber.Ctx) error {
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

	return func(c fiber.Ctx) error {
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
