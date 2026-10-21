package server

// Histogram data handler and its request-parsing helpers.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"

	dashcache "github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

const histogramFailure = "Failed to generate histogram data"

// handleGetHistogram generates histogram data (log counts over time intervals) for a specific source.
// Access is controlled by the requireTeamHasSource middleware.
func (s *Server) handleGetHistogram(c fiber.Ctx) error {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	sourceID := src.SourceID()

	// Parse request body containing time range, window, groupBy and optional filter query
	var req models.APIHistogramRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	params, err := core.PrepareHistogram(req)
	if err != nil {
		return s.sendQueryError(c, sourceID, err, histogramFailure, "Histogram generation")
	}

	// Dashboard panel requests may opt into the per-dashboard result cache.
	// Histogram results always buffer (no streaming path), so the whole response
	// is a cache candidate. A source lookup hiccup just falls through to the
	// uncached path below. Explorer requests carry no directive.
	if effTTL, ok := s.dashboardCacheParams(req.Cache); ok && len(req.ExtraStreamFilters) == 0 {
		if source, serr := core.LoadSource(c.Context(), s.datasources, sourceID); serr == nil {
			key := dashcache.ComputeKey(dashcache.KeyInput{
				EndpointKind:     "histogram",
				TeamID:           int64(src.TeamID()),
				SourceID:         int64(sourceID),
				SourceRevision:   source.UpdatedAt.UnixNano(),
				EffTTLSeconds:    int64(effTTL / time.Second),
				FinalizedQuery:   params.Query,
				CanonicalStart:   canonCacheTime(params.StartTime),
				CanonicalEnd:     canonCacheTime(params.EndTime),
				Timezone:         params.Timezone,
				EffectiveLimit:   0, // histogram ignores limit; keep it out of the key
				HistogramWindow:  params.Window,
				HistogramGroupBy: params.GroupBy,
				// Key on the effective per-request execution timeout (what
				// actually governs the query), not the fixed outer wrapper —
				// otherwise requests with different timeouts collide. Limit
				// is omitted: histogram execution ignores it.
				QueryTimeoutSecs: int64(*params.QueryTimeout),
			})
			fill := func(ctx context.Context) ([]byte, error) {
				result, err := s.executeHistogram(ctx, QueryClassDashboard, src, params)
				if err != nil {
					return nil, err
				}
				return json.Marshal(NewSuccessResponse(result))
			}
			if handled, err := s.tryServeDashboardCache(c, key, effTTL, core.HistogramTimeout, fill); handled {
				return err
			} else if err != nil {
				return s.sendQueryError(c, sourceID, err, histogramFailure, "Histogram generation")
			}
		}
	}

	// Execute histogram query via core function, bounded by HistogramTimeout so
	// a slow/misbehaving datasource can't hang the request indefinitely.
	ctx, cancel := context.WithTimeout(c.Context(), core.HistogramTimeout)
	defer cancel()

	result, err := s.executeHistogram(ctx, QueryClassHistogram, src, params)
	if err != nil {
		if ctx.Err() == context.Canceled || errors.Is(err, context.Canceled) {
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
		}
		if ctx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded) {
			s.log.Warn("histogram request timed out", "source_id", sourceID, "timeout", core.HistogramTimeout)
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
		}
		return s.sendQueryError(c, sourceID, err, histogramFailure, "Histogram generation")
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// executeHistogram owns admission for actual work, including shared cache
// fills. class selects the budget: a dashboard panel fill is bounded by the
// cache fill budget, an explorer histogram by its own interactive budget.
func (s *Server) executeHistogram(ctx context.Context, class QueryClass, src access.AuthorizedSource, params core.HistogramParams) (*core.HistogramResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	maxPerUser, maxGlobal := s.admissionLimits(class)
	queryID, err := queryTracker.StartQuery(class, src.UserID(), src.SourceID(), src.TeamID(), params.Query, cancel,
		maxPerUser, maxGlobal)
	if err != nil {
		return nil, err
	}
	defer queryTracker.RemoveQuery(queryID)
	return core.GetHistogramData(ctx, s.datasources, src, params)
}
