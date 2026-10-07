package server

// Histogram data handler and its request-parsing helpers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	dashcache "github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

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
		return sendQueryRequestError(c, err)
	}

	// Dashboard panel requests may opt into the per-dashboard result cache.
	// Histogram results always buffer (no streaming path), so the whole response
	// is a cache candidate. A source lookup hiccup just falls through to the
	// uncached path below. Explorer requests carry no directive.
	if effTTL, ok := s.dashboardCacheParams(req.Cache); ok && len(req.ExtraStreamFilters) == 0 {
		if source, serr := core.GetSource(c.Context(), s.datasources, sourceID); serr == nil {
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
				return s.handleHistogramError(c, sourceID, err)
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
		return s.handleHistogramError(c, sourceID, err)
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

// handleHistogramError maps a core.GetHistogramData error to the appropriate
// HTTP error response.
func (s *Server) handleHistogramError(c fiber.Ctx, sourceID models.SourceID, err error) error {
	if errors.Is(err, context.Canceled) {
		return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
	}
	if errors.Is(err, models.ErrHistogramBudgetExceeded) {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	if admissionErr, ok := errors.AsType[*QueryAdmissionError](err); ok {
		return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
	}
	if errors.Is(err, core.ErrSourceNotFound) {
		return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
	}
	if errors.Is(err, datasource.ErrOperationNotSupported) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Histogram is not supported for this source type yet", models.ValidationErrorType)
	}

	// Check for specific error types
	switch {
	case strings.Contains(err.Error(), "query parameter is required"):
		return SendErrorWithType(c, fiber.StatusBadRequest, "Query parameter is required for histogram data", models.ValidationErrorType)
	case strings.Contains(err.Error(), "invalid histogram window"):
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	case strings.Contains(err.Error(), "invalid"):
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	default:
		// Handle other errors
		s.log.Error("failed to get histogram data", "error", err, "source_id", sourceID)
		// Pass the actual error message to the client for better debugging
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Failed to generate histogram data: %v", err), models.DatabaseErrorType)
	}
}
