package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// queryStreamConfig describes the shape of the streamed preview response so the
// two preview endpoints stay byte-compatible with their previous buffered
// responses. logsKey is the JSON key the log rows array is written under
// ("data" for /logs/query, "logs" for /logchefql/query). The generated* fields
// are emitted only for the LogchefQL endpoint (includeGenerated).
type queryStreamConfig struct {
	failure           string
	operation         string
	logsKey           string
	includeGenerated  bool
	generatedSQL      string
	generatedQuery    string
	generatedLanguage models.QueryLanguage
	conditions        []logchefql.FilterCondition
	fieldsUsed        []string
}

// queryStreamWriter incrementally writes a success envelope
//
//	{"status":"success","data":{ "columns":[...], "<logsKey>":[ <row>, ... ],
//	 "stats":{...}, "query_id":"...", "warnings":[...] , (generated_* ) }}
//
// so the response body is produced row-by-row without ever holding the full
// result set in memory. Key order within the object is irrelevant to the
// frontend (it reads by key), so stats/query_id/warnings are emitted last,
// after the streamed rows, where they become known.
//
// It implements datasource.StreamWriter (SetWarnings/Begin/WriteRow/Finish) and
// adds WriteError for graceful mid-stream failure handling.
type queryStreamWriter struct {
	out     *bufio.Writer
	cfg     queryStreamConfig
	queryID string

	warnings []models.QueryWarning
	begun    bool
	firstRow bool
	rows     int
	closed   bool
}

const queryStreamFlushEvery = 100

func newQueryStreamWriter(out *bufio.Writer, cfg queryStreamConfig, queryID string) *queryStreamWriter {
	return &queryStreamWriter{out: out, cfg: cfg, queryID: queryID, firstRow: true}
}

// SetWarnings records the build-time warnings (LIMIT_APPLIED / LIMIT_CAPPED) so
// they can be emitted in the response tail. Called before Begin.
func (w *queryStreamWriter) SetWarnings(warnings []models.QueryWarning) {
	w.warnings = warnings
}

// Begin writes the envelope prefix and the (now known) column metadata, then
// opens the log rows array. Called once, before any WriteRow, only after the
// query executed successfully — so a failure before this point leaves the body
// empty and WriteError can emit a clean error envelope with a proper status.
func (w *queryStreamWriter) Begin(columns []models.ColumnInfo) error {
	if columns == nil {
		columns = []models.ColumnInfo{}
	}
	colBytes, err := json.Marshal(columns)
	if err != nil {
		return err
	}
	if _, err := w.out.WriteString(`{"status":"success","data":{"columns":`); err != nil {
		return err
	}
	if _, err := w.out.Write(colBytes); err != nil {
		return err
	}
	if _, err := w.out.WriteString(`,"` + w.cfg.logsKey + `":[`); err != nil {
		return err
	}
	w.begun = true
	return w.out.Flush()
}

func (w *queryStreamWriter) WriteRow(row map[string]any) error {
	rowBytes, err := json.Marshal(row)
	if err != nil {
		return err
	}
	if !w.firstRow {
		if err := w.out.WriteByte(','); err != nil {
			return err
		}
	}
	if _, err := w.out.Write(rowBytes); err != nil {
		return err
	}
	w.firstRow = false
	w.rows++
	if w.rows%queryStreamFlushEvery == 0 {
		return w.out.Flush()
	}
	return nil
}

// Finish closes the log rows array and writes the response tail (stats,
// query_id, warnings, and the generated_* fields for LogchefQL), then closes
// the envelope and flushes.
func (w *queryStreamWriter) Finish(stats models.QueryStats) error {
	return w.closeEnvelope(stats, nil)
}

// WriteError terminates the response after a streaming failure. If nothing has
// been written yet (the query failed before returning any columns — the common
// case for bad SQL / timeouts), it emits a standard error envelope with a
// proper error status, matching the non-streamed error contract the frontend
// already understands. If rows were already streamed, it closes the open
// envelope validly and attaches an "error" field so the body stays parseable.
func (w *queryStreamWriter) WriteError(streamErr error) error {
	if w.closed {
		return nil
	}
	if !w.begun {
		w.closed = true
		response := classifyQueryError(streamErr, w.cfg.failure, w.cfg.operation)
		payload := map[string]any{
			"status":     "error",
			"message":    response.message,
			"error_type": string(response.errorType),
		}
		enc, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := w.out.Write(enc); err != nil {
			return err
		}
		return w.out.Flush()
	}
	return w.closeEnvelope(models.QueryStats{}, streamErr)
}

func (w *queryStreamWriter) closeEnvelope(stats models.QueryStats, streamErr error) error {
	if w.closed {
		return nil
	}
	w.closed = true

	if _, err := w.out.WriteString(`]`); err != nil { // close the log rows array
		return err
	}

	statsBytes, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	if _, err := w.out.WriteString(`,"stats":`); err != nil {
		return err
	}
	if _, err := w.out.Write(statsBytes); err != nil {
		return err
	}

	if err := w.writeStringField("query_id", w.queryID); err != nil {
		return err
	}

	warnings := w.warnings
	if warnings == nil {
		warnings = []models.QueryWarning{}
	}
	if err := w.writeJSONField("warnings", warnings); err != nil {
		return err
	}

	if w.cfg.includeGenerated {
		if err := w.writeJSONField("conditions", w.cfg.conditions); err != nil {
			return err
		}
		if err := w.writeJSONField("fields_used", w.cfg.fieldsUsed); err != nil {
			return err
		}
		if err := w.writeStringField("generated_sql", w.cfg.generatedSQL); err != nil {
			return err
		}
		if err := w.writeStringField("generated_query", w.cfg.generatedQuery); err != nil {
			return err
		}
		if err := w.writeJSONField("generated_query_language", w.cfg.generatedLanguage); err != nil {
			return err
		}
	}

	if streamErr != nil {
		if err := w.writeStringField("error", classifyQueryError(streamErr, w.cfg.failure, w.cfg.operation).message); err != nil {
			return err
		}
	}

	if _, err := w.out.WriteString(`}}`); err != nil { // close data + envelope
		return err
	}
	return w.out.Flush()
}

func (w *queryStreamWriter) writeStringField(key, value string) error {
	enc, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := w.out.WriteString(`,"` + key + `":`); err != nil {
		return err
	}
	_, err = w.out.Write(enc)
	return err
}

func (w *queryStreamWriter) writeJSONField(key string, value any) error {
	enc, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := w.out.WriteString(`,"` + key + `":`); err != nil {
		return err
	}
	_, err = w.out.Write(enc)
	return err
}

// streamPreviewQuery admits the query, then streams the ClickHouse result to the
// client as a JSON body via SetBodyStreamWriter. Admission failures return a
// proper status code before any body is committed; failures during execution
// are handled by the writer (see WriteError). The caller must have already
// resolved the source as ClickHouse-backed.
func (s *Server) streamPreviewQuery(c fiber.Ctx, user *models.User, q previewQuery) error {
	sourceID, teamID, cfg := q.src.SourceID(), q.src.TeamID(), q.cfg
	queryID := uuid.New().String()
	streamCtx, cancel := context.WithCancel(c.Context())
	if err := queryTracker.StartQueryWithID(
		queryID,
		QueryClassPreview,
		user.ID,
		sourceID,
		teamID,
		q.trackedQuery,
		cancel,
		s.config.Query.MaxConcurrentPerUser,
		s.config.Query.MaxConcurrentGlobal,
	); err != nil {
		cancel()
		if admissionErr, ok := errors.AsType[*QueryAdmissionError](err); ok {
			return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to track query", models.GeneralErrorType)
	}

	reader, producer := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		defer queryTracker.RemoveQuery(queryID)
		defer producer.Close()
		output := bufio.NewWriter(producer)
		writer := newQueryStreamWriter(output, cfg, queryID)
		start := time.Now()
		stats, err := s.datasources.QueryLogsStream(streamCtx, sourceID, q.params, writer)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				s.recordQueryHistory(user, teamID, sourceID, q.historyQuery, q.historyLanguage,
					time.Since(start).Milliseconds(), int64(writer.rows))
			} else if writer.begun {
				s.log.Error("failed to stream query", "error", err, "source_id", sourceID, "query_id", queryID, "mode", q.mode)
			}
			if !writer.begun {
				_ = producer.CloseWithError(err)
				return
			}
			_ = writer.WriteError(err)
			return
		}

		s.log.Info("query.execute",
			"user", user.Email,
			"team_id", teamID,
			"source_id", sourceID,
			"mode", q.mode,
			"query_id", queryID,
			"rows", stats.RowsReturned,
			"duration_ms", stats.ExecutionTimeMs,
			"limit_requested", q.requestedLimit,
			"limit_applied", stats.LimitApplied,
			"truncated", stats.Truncated,
		)
		s.recordQueryHistory(user, teamID, sourceID, q.historyQuery, q.historyLanguage,
			int64(stats.ExecutionTimeMs), int64(stats.RowsReturned))
	}()

	// The first byte proves execution reached Begin before HTTP headers are committed.
	var first [1]byte
	if _, err := io.ReadFull(reader, first[:]); err != nil {
		_ = reader.Close()
		cancel()
		<-done
		return s.sendQueryError(c, sourceID, err, cfg.failure, cfg.operation)
	}
	c.Status(fiber.StatusOK)
	c.Set("Content-Type", "application/json; charset=utf-8")
	c.Set("X-LogChef-Query-ID", queryID)
	c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer func() {
			cancel()
			_ = reader.CloseWithError(context.Canceled)
			<-done
		}()
		if err := w.WriteByte(first[0]); err != nil {
			return
		}
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := reader.Read(buffer)
			if n > 0 {
				if _, err := w.Write(buffer[:n]); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	})
	return nil
}
