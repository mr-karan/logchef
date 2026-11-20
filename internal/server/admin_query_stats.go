package server

import (
	"strconv"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v3"
)

const queryStatsDefaultDays = 30

// handleAdminQueryStats returns the Query Activity page: usage over the
// query_stats_daily and query_shape_stats_daily rollups for the last `days`
// UTC days (default 30, clamped 1..365) beside the same-length window before.
// URL: GET /api/v1/admin/query-stats?days=<N>&account=human|service|all&source_id=<id>&user_id=<id>
// Requires: admin (requireAuth + requireAdmin) and logs:read token scope.
func (s *Server) handleAdminQueryStats(c fiber.Ctx) error {
	req := core.QueryActivityRequest{Days: queryStatsDefaultDays, Account: core.QueryActivityHumans}
	if raw := c.Query("days"); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid days", models.ValidationErrorType)
		}
		req.Days = min(max(days, core.MinQueryActivityDays), core.MaxQueryActivityDays)
	}
	if raw := c.Query("account"); raw != "" {
		switch account := core.QueryActivityAccount(raw); account {
		case core.QueryActivityHumans, core.QueryActivityServices, core.QueryActivityAll:
			req.Account = account
		default:
			return SendErrorWithType(c, fiber.StatusBadRequest, "account must be human, service or all", models.ValidationErrorType)
		}
	}
	if raw := c.Query("source_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id", models.ValidationErrorType)
		}
		req.SourceID = models.SourceID(id)
	}
	if raw := c.Query("user_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid user_id", models.ValidationErrorType)
		}
		req.UserID = models.UserID(id)
	}

	activity, err := core.GetQueryActivity(c.Context(), s.store, time.Now(), req)
	if err != nil {
		s.log.Error("failed to read query activity", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error listing query stats")
	}
	return SendSuccess(c, fiber.StatusOK, activity)
}
