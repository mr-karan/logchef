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

func (s *Server) handleStreamFields(c fiber.Ctx) error {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(c.Context(), core.SchemaTimeout)
	defer cancel()
	fields, err := s.datasources.StreamFields(ctx, src.SourceID())
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Could not discover stream fields", models.ExternalServiceErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fields)
}

func (s *Server) handleWindowedQuery(c fiber.Ctx) error {
	src, ok := s.authorizedSource(c)
	if !ok {
		return nil
	}
	var req windowedQueryRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	source, err := core.GetSource(c.Context(), s.datasources, src.SourceID())
	if err != nil {
		return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
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
			return s.handleLogchefQLQueryError(c, src.SourceID(), err)
		}
		params = prepared.Params
	} else {
		if req.QueryLanguage != models.QueryLanguageLogsQL {
			return SendErrorWithType(c, fiber.StatusBadRequest, "query_language must be logchefql or logsql", models.ValidationErrorType)
		}
		params, err = core.PrepareSQLQuery(s.config.Query, req.APIQueryRequest)
		if err != nil {
			return sendQueryRequestError(c, err)
		}
	}
	plan, err := s.datasources.PrepareWindowedQuery(c.Context(), src.SourceID(), datasource.WindowedRequest{
		QueryRequest: params, Cursor: req.Cursor, Count: req.Count, Step: req.Step, WindowIndex: req.WindowIndex, SkipWindow: req.SkipWindow, StreamFilters: req.ExtraStreamFilters,
	})
	if errors.Is(err, datasource.ErrOperationNotSupported) {
		return SendErrorWithType(c, fiber.StatusUnprocessableEntity, "Windowing is disabled or this query contains pipes. Run it through the standard query endpoint.", models.ValidationErrorType)
	}
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
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
	var record func(int64)
	if !req.Count && req.Cursor == "" && req.WindowIndex == nil && req.SkipWindow == nil {
		started := time.Now()
		record = func(rows int64) {
			duration := time.Since(started).Milliseconds()
			s.recordQueryHistory(user, src.TeamID(), src.SourceID(), req.QueryText, req.QueryLanguage, duration, rows)
			s.log.Info("query.execute", "user", user.Email, "team_id", src.TeamID(), "source_id", src.SourceID(), "mode", "windowed", "query_id", queryID, "rows", rows, "duration_ms", duration)
		}
	}
	return s.streamWindowedResponse(ctx, c, src.SourceID(), plan, cancel, queryID, record)
}

func (s *Server) streamWindowedResponse(ctx context.Context, c fiber.Ctx, sourceID models.SourceID, plan *datasource.WindowedPlan, cancel context.CancelFunc, queryID string, record func(int64)) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	c.Set("X-LogChef-Query-ID", queryID)
	c.RequestCtx().Response.ImmediateHeaderFlush = true
	c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer) {
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
			return
		}
		if err := w.Flush(); err != nil {
			return
		}
		var rows int64
		failed := false
		heartbeat := time.NewTicker(tailHeartbeatInterval)
		defer heartbeat.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat.C:
				if _, err := w.WriteString(": hb\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case event, open := <-events:
				if !open {
					if err := <-done; err != nil {
						s.log.Warn("windowed query ended", "source_id", sourceID, "error", err)
						_ = writeEvent(datasource.WindowedEvent{Type: datasource.WindowEnd, Message: "Windowed query stopped before completion. Retry the pending window."})
					} else if !failed && record != nil {
						record(rows)
					}
					return
				}
				rows += int64(len(event.Logs))
				failed = failed || event.Status == "failed"
				if err := writeEvent(event); err != nil {
					return
				}
			}
		}
	})
	return nil
}
