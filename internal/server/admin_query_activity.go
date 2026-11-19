package server

import (
	"sort"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v3"
)

// Query-activity feed bounds. The recent feed length is caller-controlled and
// clamped; the fetched window is a hard cap so aggregates stay honest and cheap
// (query_history is already capped per user, so this is recent activity, not
// all-time analytics).
const (
	queryActivityDefaultLimit    = 100
	queryActivityMinLimit        = 1
	queryActivityMaxLimit        = 500
	queryActivityWindow          = 2000
	queryActivitySlowestCount    = 10
	queryActivityMaxSearchLength = 256
)

// queryActivityLanguageCount is one entry in the by_language breakdown.
type queryActivityLanguageCount struct {
	Language models.QueryLanguage `json:"language"`
	Count    int                  `json:"count"`
}

// queryActivitySourceCount is one entry in the by_source breakdown.
type queryActivitySourceCount struct {
	SourceID   models.SourceID `json:"source_id"`
	SourceName string          `json:"source_name"`
	Count      int             `json:"count"`
}

// queryActivityResponse is the admin recent-activity payload. total is the size
// of the matching window (not an all-time count); by_language/by_source are
// sorted by count desc; slowest is the top matching rows by duration.
type queryActivityResponse struct {
	WindowSize  int                          `json:"window_size"`
	WindowLimit int                          `json:"window_limit"`
	Total       int                          `json:"total"`
	Recent      []models.QueryActivityRecord `json:"recent"`
	ByLanguage  []queryActivityLanguageCount `json:"by_language"`
	BySource    []queryActivitySourceCount   `json:"by_source"`
	Slowest     []models.QueryActivityRecord `json:"slowest"`
}

// handleAdminQueryActivity returns a recent-activity snapshot over the shared
// query_history table: a newest-first feed plus by-language/by-source
// breakdowns and the slowest queries, all computed over a bounded recent
// window. Because query_history is capped per user, this is deliberately RECENT
// activity, not authoritative all-time analytics.
// URL: GET /api/v1/admin/query-activity?limit=<N>&q=<text>&source_id=<id>&user_id=<id>&language=<dialect>
// Requires: admin (requireAuth + requireAdmin) and logs:read token scope.
func (s *Server) handleAdminQueryActivity(c fiber.Ctx) error {
	limit := queryActivityDefaultLimit
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid limit", models.ValidationErrorType)
		}
		limit = parsed
	}
	if limit < queryActivityMinLimit {
		limit = queryActivityMinLimit
	}
	if limit > queryActivityMaxLimit {
		limit = queryActivityMaxLimit
	}

	filter := queryActivityFilter{
		query:    strings.TrimSpace(c.Query("q")),
		language: models.QueryLanguage(c.Query("language")),
	}
	if len(filter.query) > queryActivityMaxSearchLength {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Search query exceeds 256 bytes", models.ValidationErrorType)
	}
	filter.query = strings.ToLower(filter.query)
	if raw := c.Query("source_id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id", models.ValidationErrorType)
		}
		filter.sourceID = models.SourceID(id)
	}
	if raw := c.Query("user_id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid user_id", models.ValidationErrorType)
		}
		filter.userID = models.UserID(id)
	}
	switch filter.language {
	case "", models.QueryLanguageLogchefQL, models.QueryLanguageClickHouseSQL, models.QueryLanguageLogsQL, models.QueryLanguageLogQL:
	default:
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid language", models.ValidationErrorType)
	}

	// The public demo uses a shared identity. Detailed history would disclose
	// one visitor's query text to another, so only the separate aggregate stats
	// endpoint is populated in demo mode.
	if s.config != nil && s.config.Demo.ReadOnly {
		return SendSuccess(c, fiber.StatusOK, queryActivityResponse{
			WindowLimit: queryActivityWindow,
			Recent:      []models.QueryActivityRecord{},
			ByLanguage:  []queryActivityLanguageCount{},
			BySource:    []queryActivitySourceCount{},
			Slowest:     []models.QueryActivityRecord{},
		})
	}

	window, err := s.store.ListQueryActivity(c.Context(), queryActivityWindow)
	if err != nil {
		s.log.Error("failed to list query activity", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error listing query activity")
	}

	matches := window[:0]
	for i := range window {
		if filter.matches(&window[i]) {
			matches = append(matches, window[i])
		}
	}
	resp := queryActivityResponse{
		WindowSize:  len(window),
		WindowLimit: queryActivityWindow,
		Total:       len(matches),
		Recent:      recentActivity(matches, limit),
		ByLanguage:  languageBreakdown(matches),
		BySource:    sourceBreakdown(matches),
		Slowest:     slowestActivity(matches, queryActivitySlowestCount),
	}

	return SendSuccess(c, fiber.StatusOK, resp)
}

type queryActivityFilter struct {
	query    string
	sourceID models.SourceID
	userID   models.UserID
	language models.QueryLanguage
}

func (f queryActivityFilter) matches(row *models.QueryActivityRecord) bool {
	if f.sourceID != 0 && row.SourceID != f.sourceID {
		return false
	}
	if f.userID != 0 && row.UserID != f.userID {
		return false
	}
	if f.language != "" && row.QueryLanguage != f.language {
		return false
	}
	return f.query == "" || strings.Contains(strings.ToLower(row.QueryText), f.query) ||
		strings.Contains(strings.ToLower(row.UserEmail), f.query) ||
		strings.Contains(strings.ToLower(row.SourceName), f.query) ||
		strings.Contains(strings.ToLower(string(row.QueryLanguage)), f.query)
}

// recentActivity returns the first `limit` rows of the already newest-first
// window as a fresh slice.
func recentActivity(window []models.QueryActivityRecord, limit int) []models.QueryActivityRecord {
	n := min(limit, len(window))
	recent := make([]models.QueryActivityRecord, n)
	copy(recent, window[:n])
	return recent
}

// languageBreakdown counts rows per query language, sorted by count desc
// (language asc for stable ties).
func languageBreakdown(window []models.QueryActivityRecord) []queryActivityLanguageCount {
	counts := make(map[models.QueryLanguage]int)
	for i := range window {
		counts[window[i].QueryLanguage]++
	}
	out := make([]queryActivityLanguageCount, 0, len(counts))
	for lang, count := range counts {
		out = append(out, queryActivityLanguageCount{Language: lang, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Language < out[j].Language
	})
	return out
}

// sourceBreakdown counts rows per source (keyed by source_id), sorted by count
// desc (source_id asc for stable ties).
func sourceBreakdown(window []models.QueryActivityRecord) []queryActivitySourceCount {
	type agg struct {
		name  string
		count int
	}
	counts := make(map[models.SourceID]*agg)
	for i := range window {
		r := window[i]
		if a, ok := counts[r.SourceID]; ok {
			a.count++
		} else {
			counts[r.SourceID] = &agg{name: r.SourceName, count: 1}
		}
	}
	out := make([]queryActivitySourceCount, 0, len(counts))
	for id, a := range counts {
		out = append(out, queryActivitySourceCount{SourceID: id, SourceName: a.name, Count: a.count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].SourceID < out[j].SourceID
	})
	return out
}

// slowestActivity returns up to `n` rows with the highest duration_ms, desc
// (id desc for stable ties).
func slowestActivity(window []models.QueryActivityRecord, n int) []models.QueryActivityRecord {
	sorted := make([]models.QueryActivityRecord, len(window))
	copy(sorted, window)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].DurationMs != sorted[j].DurationMs {
			return sorted[i].DurationMs > sorted[j].DurationMs
		}
		return sorted[i].ID > sorted[j].ID
	})
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}
