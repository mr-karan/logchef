package server

import (
	"strconv"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// Query-stats window bounds and the top-N cap for the authoritative all-time
// usage endpoint. Unlike the recent-activity view, these aggregates come from
// the non-pruned query_stats_daily rollup, so they are correct all-time.
const (
	queryStatsDefaultDays = 30
	queryStatsMinDays     = 1
	queryStatsMaxDays     = 365
	queryStatsTopN        = 10
)

// queryStatsResponse is the admin all-time usage payload, sourced from the
// non-pruned daily rollup. since is 'YYYY-MM-DD' (UTC), the inclusive start of
// the window; top_sources/top_users are count-desc (capped at 10); volume_by_day
// is ascending by date.
type queryStatsResponse struct {
	Since       string                    `json:"since"`
	Days        int                       `json:"days"`
	TopSources  []models.SourceQueryStat  `json:"top_sources"`
	TopUsers    []models.UserQueryStat    `json:"top_users"`
	VolumeByDay []models.DailyQueryVolume `json:"volume_by_day"`
}

// handleAdminQueryStats returns authoritative all-time usage analytics over the
// non-pruned query_stats_daily rollup: top sources, top users, and volume by
// day for the last `days` days (default 30, clamped 1..365). Unlike
// /admin/query-activity (a recent window over the capped query_history), these
// aggregates are correct all-time because the rollup is never pruned.
// URL: GET /api/v1/admin/query-stats?days=<N>
// Requires: admin (requireAuth + requireAdmin) and logs:read token scope.
func (s *Server) handleAdminQueryStats(c *fiber.Ctx) error {
	days := queryStatsDefaultDays
	if raw := c.Query("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid days", models.ValidationErrorType)
		}
		days = parsed
	}
	if days < queryStatsMinDays {
		days = queryStatsMinDays
	}
	if days > queryStatsMaxDays {
		days = queryStatsMaxDays
	}

	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	topSources, err := s.sqlite.TopSourcesByQueries(c.Context(), since, queryStatsTopN)
	if err != nil {
		s.log.Error("failed to list top sources by queries", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error listing query stats")
	}
	topUsers, err := s.sqlite.TopUsersByQueries(c.Context(), since, queryStatsTopN)
	if err != nil {
		s.log.Error("failed to list top users by queries", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error listing query stats")
	}
	volumeByDay, err := s.sqlite.QueryVolumeByDay(c.Context(), since)
	if err != nil {
		s.log.Error("failed to list query volume by day", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error listing query stats")
	}

	return SendSuccess(c, fiber.StatusOK, queryStatsResponse{
		Since:       since,
		Days:        days,
		TopSources:  topSources,
		TopUsers:    topUsers,
		VolumeByDay: volumeByDay,
	})
}
