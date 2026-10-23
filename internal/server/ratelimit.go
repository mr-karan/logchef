package server

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

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
	perIP := auth.NewWindowLimiter(time.Minute, perIPPerMinute)

	var global *auth.WindowLimiter
	if globalPerMinute > 0 {
		global = auth.NewGlobalWindowLimiter(time.Minute, globalPerMinute)
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
	perUser := auth.NewWindowLimiter(time.Minute, perUserPerMinute)

	return func(c fiber.Ctx) error {
		key := c.IP()
		if user := currentUser(c); user != nil {
			key = "user:" + strconv.Itoa(int(user.ID))
		}
		if !perUser.Allow(key) {
			return tooManyRequests(c, "query")
		}
		return c.Next()
	}
}
