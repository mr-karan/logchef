package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

const tailHeartbeatInterval = 15 * time.Second

// handleTailLogs streams live logs over Server-Sent Events. It mirrors the
// /logs/query middleware chain (auth, team member, team-has-source,
// logs:read). The wire contract:
//
//	: hb                     (comment, heartbeat every 15s)
//	event: rows
//	data: [ {row}, ... ]
//	event: notice
//	data: {"code":"rate_limited","message":"..."}
//	event: end
//	data: {"reason":"ttl_expired"}
//
// GET /api/v1/teams/:teamID/sources/:sourceID/logs/tail?query=&query_language=
func (s *Server) handleTailLogs(c *fiber.Ctx) error { //nolint:gocyclo // request handler, inherently branchy
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}
	teamID, err := core.ParseTeamID(c.Params("teamID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	// Gate on the source capability before any streaming setup so non-supporting
	// sources get a clean 400.
	source, err := core.GetSource(c.Context(), s.datasources, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source for tail", "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}
	if !source.HasCapability(string(datasource.CapabilityLiveTail)) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Live tail is not supported for this source type yet", models.ValidationErrorType)
	}

	// Resolve the native tail query. LogchefQL is compiled via the unified
	// CompileLogchefQL path (no time window — tail follows from now()); native
	// languages pass through, gated by the source.
	nativeQuery, nativeLang, ok := s.resolveTailQuery(c, source, sourceID)
	if !ok {
		// resolveTailQuery already wrote the error response.
		return nil
	}

	tailReq := datasource.TailRequest{
		Query:        nativeQuery,
		Language:     nativeLang,
		PollInterval: s.config.Tail.PollInterval,
	}

	// Admission control: class tail, per-user and global caps → 429.
	streamCtx, cancel := context.WithCancel(c.Context())
	queryID, err := queryTracker.StartQuery(
		QueryClassTail,
		user.ID,
		sourceID,
		teamID,
		nativeQuery,
		cancel,
		s.config.Tail.MaxPerUser,
		s.config.Tail.MaxGlobal,
	)
	if err != nil {
		cancel()
		var admissionErr *QueryAdmissionError
		if errors.As(err, &admissionErr) {
			return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to track tail query", models.GeneralErrorType)
	}

	c.Status(fiber.StatusOK)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no") // disable proxy buffering so frames flush promptly
	c.Set("X-LogChef-Query-ID", queryID)

	// Stream the response body incrementally: send headers immediately and flush
	// each frame to the socket rather than buffering the whole (never-ending) body.
	c.Context().Response.ImmediateHeaderFlush = true

	// Detach everything the stream writer needs before returning — the fiber ctx
	// is not valid inside SetBodyStreamWriter.
	sessionTTL := s.config.Tail.SessionTTL
	maxRowsPerSec := s.config.Tail.MaxRowsPerSec
	log := s.log
	email := user.Email

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		defer queryTracker.RemoveQuery(queryID)

		writeFrame := func(event string, data []byte) bool {
			if event != "" {
				if _, err := w.WriteString("event: " + event + "\n"); err != nil {
					return false
				}
			}
			if _, err := w.WriteString("data: "); err != nil {
				return false
			}
			if _, err := w.Write(data); err != nil {
				return false
			}
			if _, err := w.WriteString("\n\n"); err != nil {
				return false
			}
			return w.Flush() == nil
		}
		writeComment := func(comment string) bool {
			if _, err := w.WriteString(comment + "\n\n"); err != nil {
				return false
			}
			return w.Flush() == nil
		}

		// Flush an initial comment so response headers reach the client and the
		// SSE connection is confirmed open before the first row/heartbeat.
		if !writeComment(": ok") {
			return
		}

		// Run the provider tail in a goroutine; it feeds batches back over a
		// channel. emit blocks until the writer drains or the stream is cancelled.
		batchCh := make(chan []map[string]any, 16)
		tailDone := make(chan error, 1)
		go func() {
			emit := func(rows []map[string]any) error {
				select {
				case batchCh <- rows:
					return nil
				case <-streamCtx.Done():
					return streamCtx.Err()
				}
			}
			tailDone <- s.datasources.TailLogs(streamCtx, sourceID, tailReq, emit)
		}()

		heartbeat := time.NewTicker(tailHeartbeatInterval)
		defer heartbeat.Stop()

		var ttlCh <-chan time.Time
		if sessionTTL > 0 {
			ttlTimer := time.NewTimer(sessionTTL)
			defer ttlTimer.Stop()
			ttlCh = ttlTimer.C
		}

		limiter := newTailRateLimiter(maxRowsPerSec)

		for {
			select {
			case <-streamCtx.Done():
				return
			case <-ttlCh:
				writeFrame("end", []byte(`{"reason":"ttl_expired"}`))
				log.Info("query.tail.end", "reason", "ttl_expired", "user", email, "source_id", sourceID, "query_id", queryID)
				return
			case <-heartbeat.C:
				if !writeComment(": hb") {
					return
				}
			case err := <-tailDone:
				if err != nil && streamCtx.Err() == nil {
					log.Warn("tail stream ended with error", "source_id", sourceID, "query_id", queryID, "error", err)
					payload, _ := json.Marshal(map[string]string{"reason": "error", "message": err.Error()})
					writeFrame("end", payload)
				} else {
					writeFrame("end", []byte(`{"reason":"completed"}`))
				}
				return
			case batch := <-batchCh:
				allowed, dropped := limiter.admit(len(batch))
				if allowed > 0 {
					payload, marshalErr := json.Marshal(batch[:allowed])
					if marshalErr == nil {
						if !writeFrame("rows", payload) {
							return
						}
					}
				}
				if dropped > 0 && limiter.shouldNotify() {
					notice, _ := json.Marshal(map[string]string{
						"code":    "rate_limited",
						"message": fmt.Sprintf("dropped %d rows; narrow your filter", dropped),
					})
					if !writeFrame("notice", notice) {
						return
					}
				}
			}
		}
	})

	return nil
}

// resolveTailQuery turns the request's query/query_language into the provider's
// native tail input. On any gating or compile failure it writes the error
// response itself and returns ok=false — the Send* helpers return the nil
// result of writing the response, so their return value must NOT be used as an
// error sentinel.
func (s *Server) resolveTailQuery(c *fiber.Ctx, source *models.Source, sourceID models.SourceID) (string, models.QueryLanguage, bool) {
	rawQuery := c.Query("query")
	language := models.NormalizeQueryLanguage(models.QueryLanguage(c.Query("query_language")))

	if language == "" || language == models.QueryLanguageLogchefQL {
		if !source.SupportsQueryLanguage(models.QueryLanguageLogchefQL) {
			_ = SendErrorWithType(c, fiber.StatusBadRequest, "LogchefQL is not supported for this source", models.ValidationErrorType)
			return "", "", false
		}
		compiled, compileErr := s.datasources.CompileLogchefQL(c.Context(), sourceID, datasource.LogchefQLCompileRequest{
			Query: rawQuery,
		})
		if compiled == nil {
			if errors.Is(compileErr, datasource.ErrOperationNotSupported) {
				_ = SendErrorWithType(c, fiber.StatusBadRequest, "LogchefQL is not supported for this source", models.ValidationErrorType)
				return "", "", false
			}
			s.log.Error("failed to compile logchefql tail query", "error", compileErr, "source_id", sourceID)
			_ = SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to compile query", models.GeneralErrorType)
			return "", "", false
		}
		if compileErr != nil || !compiled.Valid {
			message := "invalid LogchefQL query"
			if compiled.Error != nil {
				message = compiled.Error.Error()
			} else if compileErr != nil {
				message = compileErr.Error()
			}
			_ = SendErrorWithType(c, fiber.StatusBadRequest, message, models.ValidationErrorType)
			return "", "", false
		}
		// ClickHouse tails compose a WHERE-fragment (conditions only); VictoriaLogs
		// takes the full LogsQL query.
		if compiled.Language == models.QueryLanguageClickHouseSQL {
			return compiled.FilterOnly, compiled.Language, true
		}
		return compiled.Query, compiled.Language, true
	}

	if !source.SupportsQueryLanguage(language) {
		_ = SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Query language %q is not supported for this source", language), models.ValidationErrorType)
		return "", "", false
	}
	// ClickHouse tails need a WHERE-fragment, not a full SELECT — raw SQL has
	// no meaningful follow semantics. LogchefQL is the tail language there;
	// VictoriaLogs accepts native LogsQL filters.
	if language == models.QueryLanguageClickHouseSQL {
		_ = SendErrorWithType(c, fiber.StatusBadRequest,
			"Live tail supports LogchefQL queries for ClickHouse sources", models.ValidationErrorType)
		return "", "", false
	}
	return strings.TrimSpace(rawQuery), language, true
}

// tailRateLimiter enforces a per-stream row-rate ceiling. It never buffers:
// rows beyond the ceiling in the current one-second window are dropped, and at
// most one notice is surfaced per window.
type tailRateLimiter struct {
	maxPerSec   int
	windowStart time.Time
	emitted     int
	noticeSent  bool
	now         func() time.Time
}

func newTailRateLimiter(maxPerSec int) *tailRateLimiter {
	return &tailRateLimiter{maxPerSec: maxPerSec, now: time.Now}
}

// admit reports how many of n rows may be emitted now and how many are dropped.
func (l *tailRateLimiter) admit(n int) (allowed, dropped int) {
	if l.maxPerSec <= 0 {
		return n, 0 // unlimited
	}
	now := l.now()
	if l.windowStart.IsZero() || now.Sub(l.windowStart) >= time.Second {
		l.windowStart = now
		l.emitted = 0
		l.noticeSent = false
	}
	remaining := l.maxPerSec - l.emitted
	if remaining < 0 {
		remaining = 0
	}
	if n <= remaining {
		l.emitted += n
		return n, 0
	}
	l.emitted += remaining
	return remaining, n - remaining
}

// shouldNotify reports whether a rate-limit notice should be sent for the
// current window, ensuring at most one notice per second.
func (l *tailRateLimiter) shouldNotify() bool {
	if l.noticeSent {
		return false
	}
	l.noticeSent = true
	return true
}
