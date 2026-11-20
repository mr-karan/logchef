package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

type windowedQueryRequest struct {
	models.APIQueryRequest
	QueryLanguage models.QueryLanguage `json:"query_language"`
	Cursor        string               `json:"cursor,omitempty"`
	Count         bool                 `json:"count,omitempty"`
	Step          string               `json:"step,omitempty"`
	WindowIndex   *int                 `json:"window_index,omitempty"`
	SkipWindow    *int                 `json:"skip_window,omitempty"`
}

const windowedQueryFailure = "Windowed query failed"

// errWindowFailed ends the query.run event of a windowed search in which a
// window failed.
var errWindowFailed = &datasource.UpstreamError{Message: "a window of the search failed"}

func (s *Server) handleStreamFields(c fiber.Ctx) error {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(c.Context(), core.SchemaTimeout)
	defer cancel()
	fields, err := s.datasources.StreamFields(ctx, src.SourceID())
	if err != nil {
		return s.sendQueryError(c, src.SourceID(), err, "Could not discover stream fields", "Stream field discovery")
	}
	return SendSuccess(c, fiber.StatusOK, fields)
}

func (s *Server) handleWindowedQuery(c fiber.Ctx) (err error) {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	var req windowedQueryRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	user := currentUser(c)
	if user == nil {
		return sendMissingUser(c)
	}
	// A count request draws the histogram of a search: like /logs/histogram,
	// it returns no rows, so it records no query run.
	var run *queryRun
	if !req.Count {
		run = s.newQueryRun(c, src, user, queryRunWindowed, req.QueryText, req.QueryLanguage, req.Limit)
		// The first page writes the history row; later pages continue it.
		run.history = req.Cursor == "" && req.WindowIndex == nil && req.SkipWindow == nil
		defer func() { s.endQueryRun(c, run, err, recover()) }()
	}
	source, err := core.LoadSource(c.Context(), s.datasources, src.SourceID())
	if err != nil {
		return s.sendQueryError(c, src.SourceID(), err, windowedQueryFailure, "Windowed search")
	}
	if run != nil {
		run.source = source
	}
	if !source.IsVictoriaLogs() {
		return SendErrorWithType(c, fiber.StatusUnprocessableEntity, "Windowed search requires a VictoriaLogs source", models.ValidationErrorType)
	}
	var params datasource.QueryRequest
	if req.QueryLanguage == models.QueryLanguageLogchefQL {
		prepared, err := core.PrepareLogchefQLQuery(c.Context(), s.datasources, s.config.Query, src, core.LogchefQLQueryRequest{
			Query: req.QueryText, StartTime: req.StartTime, EndTime: req.EndTime, Timezone: req.Timezone, Limit: req.Limit, QueryTimeout: req.QueryTimeout, Variables: req.Variables,
		})
		if err != nil {
			return s.sendQueryError(c, src.SourceID(), err, windowedQueryFailure, "Windowed search")
		}
		params = prepared.Params
	} else {
		if req.QueryLanguage != models.QueryLanguageLogsQL {
			return SendErrorWithType(c, fiber.StatusBadRequest, "query_language must be logchefql or logsql", models.ValidationErrorType)
		}
		params, err = core.PrepareSourceSQLQuery(c.Context(), s.datasources, s.config.Query, src, req.APIQueryRequest)
		if err != nil {
			return s.sendQueryError(c, src.SourceID(), err, windowedQueryFailure, "Windowed search")
		}
	}
	if run != nil {
		run.from, run.to = params.StartTime, params.EndTime
	}
	plan, err := s.datasources.PrepareWindowedQuery(c.Context(), src.SourceID(), datasource.WindowedRequest{
		QueryRequest: params, Cursor: req.Cursor, Count: req.Count, Step: req.Step, WindowIndex: req.WindowIndex, SkipWindow: req.SkipWindow, StreamFilters: req.ExtraStreamFilters,
	})
	if errors.Is(err, datasource.ErrOperationNotSupported) {
		return SendErrorWithType(c, fiber.StatusUnprocessableEntity, "Windowing is disabled or this query contains pipes. Run it through the standard query endpoint.", models.ValidationErrorType)
	}
	if err != nil {
		return s.sendQueryError(c, src.SourceID(), err, windowedQueryFailure, "Windowed search")
	}
	class := QueryClassPreview
	if req.Count {
		class = QueryClassHistogram
	}
	ctx, cancel := context.WithCancel(c.Context())
	queryID, err := queryTracker.StartQuery(class, user.ID, src.SourceID(), src.TeamID(), params.RawQuery, cancel, s.config.Query.MaxConcurrentPerUser, s.config.Query.MaxConcurrentGlobal)
	if err != nil {
		cancel()
		return SendErrorWithType(c, fiber.StatusTooManyRequests, err.Error(), models.ValidationErrorType)
	}
	if run != nil {
		run.handedOff = true
	}
	return s.streamWindowedResponse(ctx, c, src.SourceID(), plan, cancel, queryID, run)
}

// streamWindowedResponse streams the windows of a search as server-sent
// events and finishes run, which is nil for a count request.
func (s *Server) streamWindowedResponse(ctx context.Context, c fiber.Ctx, sourceID models.SourceID, plan *datasource.WindowedPlan, cancel context.CancelFunc, queryID string, run *queryRun) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	c.Set("X-LogChef-Query-ID", queryID)
	c.RequestCtx().Response.ImmediateHeaderFlush = true
	c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer) {
		var rows int64
		// runErr is why the search stopped, nil when every window completed.
		// A failed write means the client went away.
		var runErr error
		defer func() {
			if run == nil {
				return
			}
			if runErr == nil {
				s.log.Info("query.execute", "user", run.user.Email, "team_id", run.teamID, "source_id", sourceID, "mode", "windowed", "query_id", queryID, "rows", rows, "duration_ms", time.Since(run.started).Milliseconds())
			}
			s.finishQueryRun(run, queryOutcome{err: runErr, status: fiber.StatusOK, queryID: queryID, rows: rows})
		}()
		defer cancel()
		defer queryTracker.RemoveQuery(queryID)
		events := make(chan datasource.WindowedEvent, 1)
		done := make(chan error, 1)
		go func() {
			done <- s.datasources.StreamWindowedQuery(ctx, sourceID, plan, func(event datasource.WindowedEvent) error {
				select {
				case events <- event:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			close(events)
		}()
		writeEvent := func(event datasource.WindowedEvent) error {
			data, err := json.Marshal(event)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
				return err
			}
			return w.Flush()
		}
		if _, err := w.WriteString(": ok\n\n"); err != nil {
			runErr = context.Canceled
			return
		}
		if err := w.Flush(); err != nil {
			runErr = context.Canceled
			return
		}
		heartbeat := time.NewTicker(tailHeartbeatInterval)
		defer heartbeat.Stop()
		for {
			select {
			case <-ctx.Done():
				runErr = ctx.Err()
				return
			case <-heartbeat.C:
				if _, err := w.WriteString(": hb\n\n"); err != nil {
					runErr = context.Canceled
					return
				}
				if err := w.Flush(); err != nil {
					runErr = context.Canceled
					return
				}
			case event, open := <-events:
				if !open {
					if err := <-done; err != nil {
						runErr = err
						s.log.Warn("windowed query ended", "source_id", sourceID, "error", err)
						_ = writeEvent(datasource.WindowedEvent{Type: datasource.WindowEnd, Message: "Windowed query stopped before completion. Retry the pending window."})
					}
					return
				}
				rows += int64(len(event.Logs))
				if event.Status == datasource.WindowFailed && runErr == nil {
					runErr = errWindowFailed
				}
				if err := writeEvent(event); err != nil {
					runErr = context.Canceled
					return
				}
			}
		}
	})
	return nil
}
