package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

// queryRunMode is how the rows of a query reached the client.
type queryRunMode string

const (
	queryRunBuffered queryRunMode = "buffered"
	queryRunStream   queryRunMode = "stream"
	queryRunWindowed queryRunMode = "windowed"
	queryRunMCP      queryRunMode = "mcp"
	queryRunAskChef  queryRunMode = "ask_chef"
	queryRunTail     queryRunMode = "tail"
	queryRunContext  queryRunMode = "context"
)

// queryRun is one data query, from the handler or tool that receives it to
// the background writer that stores its query.run event and its history row.
// It holds copies of the request values, because Fiber reuses the request
// context after the handler returns.
//
// Exactly one owner finishes a run: the handler, the stream goroutine it
// handed the run to, or the MCP tool call that started it.
type queryRun struct {
	actor   *audit.Actor // nil when no authenticated actor made the request
	request audit.Request
	// user owns the history row. It is nil for a run that writes none.
	user     *models.User
	teamID   models.TeamID
	sourceID models.SourceID
	// source names the target and fills the history row. It is nil when the
	// query failed before its source loaded.
	source   *models.Source
	query    string
	language models.QueryLanguage
	mode     queryRunMode
	from, to *time.Time
	limit    int
	// tool is the MCP tool of an mcp or ask_chef run, and fields are the
	// fields whose values it read.
	tool   string
	fields []string
	// anchor is the timestamp that a log context read is around.
	anchor  *time.Time
	started time.Time
	// history says that the run writes a query history row when it ran:
	// false for a cached dashboard panel and a windowed page after the first.
	history bool
	// handedOff and finished are read and written by the owner only: once
	// handedOff is set, the handler touches the run no more.
	handedOff, finished bool
}

// queryOutcome is how a query run ended.
type queryOutcome struct {
	// err is nil when the query returned its rows.
	err     error
	status  int
	queryID string
	rows    int64
	// stats is nil when the query returned none: a failure, a windowed
	// search, or a cached response that this request's fill did not run.
	stats *models.QueryStats
	// cache is the dashboard cache status of a cached response.
	cache string
}

// queryRecord is a finished run as the background writer stores it. Each
// part is nil when the run writes none.
type queryRecord struct {
	event   *audit.Event
	history *models.QueryHistory
	usage   *queryUsage
	failure *models.QueryFailureIncrement
}

// queryUsage is a successful run as the daily rollups count it: run as a
// history row holds it, and what it scanned.
type queryUsage struct {
	run                 *models.QueryHistory
	rowsRead, bytesRead int64
}

// newQueryRun starts the run of a handler's query. A nil user writes no
// history row.
func (s *Server) newQueryRun(c fiber.Ctx, src access.AuthorizedSource, user *models.User, mode queryRunMode, query string, language models.QueryLanguage, limit int) *queryRun {
	run := &queryRun{
		request:  auditRequest(c),
		user:     user,
		teamID:   src.TeamID(),
		sourceID: src.SourceID(),
		query:    query,
		language: language,
		mode:     mode,
		limit:    limit,
		history:  user != nil,
	}
	if actor, ok := requestActor(c); ok {
		run.actor = &actor
	}
	return s.startQueryRun(run)
}

// startQueryRun starts run, which its caller filled, and counts it until it
// finishes.
func (s *Server) startQueryRun(run *queryRun) *queryRun {
	run.started = time.Now()
	s.backgroundMu.Lock()
	s.activeQueryRuns++
	s.backgroundMu.Unlock()
	return run
}

// endQueryRun finishes a run that its handler neither finished nor handed
// off: a request that failed before the query ran or while a buffered query
// ran. The response status gives the outcome, and the error that
// sendQueryError kept gives the reason. panicked is what the handler's
// deferred recover returned: the run then fails with status 500, the status
// that the recover middleware answers once endQueryRun panics again.
func (s *Server) endQueryRun(c fiber.Ctx, run *queryRun, chainErr error, panicked any) {
	if panicked != nil {
		if run != nil && !run.handedOff {
			s.finishQueryRun(run, queryOutcome{status: fiber.StatusInternalServerError})
		}
		panic(panicked)
	}
	if run == nil || run.handedOff || run.finished {
		return
	}
	queryErr, _ := c.Locals(queryErrorKey{}).(error)
	s.finishQueryRun(run, queryOutcome{err: queryErr, status: responseStatus(c, chainErr)})
}

// finishCachedQueryRun records a dashboard panel that the cache answered.
// filled is the stats of the query this request's fill ran, nil when the
// response came from an entry or another request's fill. A fill counts like
// any query; a response that ran nothing has the cache status and no row
// count. Neither writes query history.
func (s *Server) finishCachedQueryRun(c fiber.Ctx, run *queryRun, filled *models.QueryStats) {
	run.history = false
	out := queryOutcome{
		status: c.Response().StatusCode(),
		cache:  strings.ToLower(c.GetRespHeader("X-Logchef-Cache")),
		stats:  filled,
	}
	if filled != nil {
		out.rows = int64(filled.RowsReturned)
	}
	s.finishQueryRun(run, out)
}

// finishQueryRun queues the run's record for the background writer. A query
// never waits for the audit insert.
func (s *Server) finishQueryRun(run *queryRun, out queryOutcome) {
	if run.finished {
		return
	}
	run.finished = true
	defer s.queryRunFinished()
	durationMs := time.Since(run.started).Milliseconds()
	if out.stats != nil && out.stats.ExecutionTimeMs > 0 {
		durationMs = int64(out.stats.ExecutionTimeMs)
	}
	reason := ""
	if out.err != nil || out.status >= fiber.StatusBadRequest {
		reason = queryFailureReason(out.err, out.status)
	}
	var rec queryRecord
	if run.actor != nil {
		ev := run.event(out, durationMs, reason)
		rec.event = &ev
	}
	// A cancelled query stays in the history: the user ran it.
	if run.history && (reason == "" || errors.Is(out.err, context.Canceled)) {
		rec.history = run.historyEntry(durationMs, out.rows)
	}
	switch {
	// A run with no user, such as a tail, log context or MCP read, counts in
	// no rollup, so its failure does not either: the failure rate divides by
	// the counted runs.
	case run.user == nil:
	case reason != "":
		rec.failure = &models.QueryFailureIncrement{
			UserID: run.user.ID, TeamID: run.teamID, SourceID: run.sourceID, Language: run.language, Reason: reason,
		}
	// A windowed page after the first and a cached response that ran nothing
	// count no run.
	case run.history || out.cache != "" && out.stats != nil:
		rec.usage = &queryUsage{run: run.historyEntry(durationMs, out.rows)}
		if out.stats != nil {
			rec.usage.rowsRead, rec.usage.bytesRead = int64(out.stats.RowsRead), int64(out.stats.BytesRead)
		}
	}
	if rec.event == nil && rec.history == nil && rec.usage == nil && rec.failure == nil {
		return
	}
	s.enqueueQueryRecord(rec)
}

// enqueueQueryRecord queues rec for the writer, which keeps accepting
// records during shutdown until every query run has finished.
func (s *Server) enqueueQueryRecord(rec queryRecord) {
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	if s.queryRecordsClosed {
		s.dropQueryRecord(rec)
		return
	}
	select {
	case s.queryRecords <- rec:
	default:
		s.dropQueryRecord(rec)
	}
}

func (s *Server) queryRunFinished() {
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	s.activeQueryRuns--
	s.closeIfQueryRunsDone()
}

// closeIfQueryRunsDone tells the writer that no more records are coming:
// the HTTP server has stopped and every run has finished. The caller holds
// backgroundMu.
func (s *Server) closeIfQueryRunsDone() {
	if !s.httpStopped || s.activeQueryRuns > 0 {
		return
	}
	select {
	case <-s.queryRunsDone:
	default:
		close(s.queryRunsDone)
	}
}

func (s *Server) dropQueryRecord(rec queryRecord) {
	s.queryRecordsDropped.Add(1)
	if rec.event != nil {
		metrics.RecordAuditEvent("dropped")
	}
}

// event is the run's query.run event. reason is "" for a success.
func (r *queryRun) event(out queryOutcome, durationMs int64, reason string) audit.Event {
	outcome := models.AuditOutcomeSuccess
	if reason != "" {
		outcome = models.AuditOutcomeFailure
	}
	label := ""
	if r.source != nil {
		label = r.source.Name
	}
	detail := audit.NewFields().
		Query("query", r.query).
		Set("language", r.language).
		Set("mode", r.mode).
		Set("time_from", r.from).
		Set("time_to", r.to).
		Set("limit_requested", r.limit).
		Set("duration_ms", durationMs)
	if out.cache != "" {
		detail.Set("cache", out.cache)
	}
	// A cached response that ran nothing returned no rows of its own.
	if out.cache == "" || out.stats != nil {
		detail.Set("rows_returned", out.rows)
	}
	if st := out.stats; st != nil {
		detail.Set("limit_applied", st.LimitApplied).
			Set("rows_read", st.RowsRead).
			Set("bytes_read", st.BytesRead).
			// "truncated" marks an event whose payload the recorder cut.
			Set("result_truncated", st.Truncated)
	}
	if out.queryID != "" {
		detail.Set("query_id", out.queryID)
	}
	if r.tool != "" {
		detail.Set("tool", r.tool)
	}
	if len(r.fields) > 0 {
		detail.Set("fields", r.fields)
	}
	if r.anchor != nil {
		detail.Set("anchor", r.anchor)
	}
	request := r.request
	request.Status = out.status
	return audit.Event{
		Action:  audit.QueryRun,
		Outcome: outcome,
		Reason:  reason,
		Actor:   *r.actor,
		Target:  audit.QueryRunTarget(r.teamID, r.sourceID, label),
		Request: request,
		Detail:  detail,
	}
}

func (r *queryRun) historyEntry(durationMs, rows int64) *models.QueryHistory {
	entry := &models.QueryHistory{
		UserID:        r.user.ID,
		TeamID:        r.teamID,
		SourceID:      r.sourceID,
		QueryText:     r.query,
		QueryLanguage: models.NormalizeQueryLanguage(r.language),
		DurationMs:    durationMs,
		RowCount:      rows,
	}
	if r.from != nil {
		lookback := max(int64(time.Since(*r.from)/time.Second), 0)
		entry.LookbackSeconds = &lookback
	}
	if r.source != nil && r.source.IsClickHouse() {
		entry.SourceDatabase, entry.SourceTable = r.source.Connection.Database, r.source.Connection.TableName
	}
	return entry
}

// queryFailureReason is the reason code of a failed query or export. err is
// nil when the handler answered without one, and status then decides.
func queryFailureReason(err error, status int) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded), err != nil && clickhouse.IsQueryTimeoutError(err):
		return "timeout"
	case err != nil:
		status = classifyQueryError(err, "", "").status
	}
	switch {
	case status == fiber.StatusTooManyRequests:
		return "rejected"
	case status == fiber.StatusRequestTimeout:
		return "timeout"
	case status == fiber.StatusBadGateway:
		return "upstream_error"
	case status >= fiber.StatusInternalServerError, status < fiber.StatusBadRequest:
		return "internal_error"
	default:
		return "invalid_query"
	}
}
