package server

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Server) startQueryHistoryWorker() {
	s.startBackgroundTask(func() {
		for {
			select {
			case <-s.stop:
				s.drainQueryHistory()
				return
			default:
			}
			select {
			case <-s.stop:
				s.drainQueryHistory()
				return
			case rec := <-s.queryRecords:
				s.writeQueryRecord(context.WithoutCancel(s.baseCtx), rec)
			}
		}
	})
}

// drainQueryHistory stores the records of queries that finish during
// shutdown. It accepts records until every query run has finished,
// then empties the queue. A record past the shutdown deadline is dropped.
func (s *Server) drainQueryHistory() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.historyDrainCtx), 4*time.Second)
	defer cancel()
	if deadline, ok := s.historyDrainCtx.Deadline(); ok {
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithDeadline(ctx, deadline.Add(-100*time.Millisecond))
		defer deadlineCancel()
	}
	store := func(rec queryRecord) {
		if ctx.Err() != nil {
			s.dropQueryRecord(rec)
		} else {
			s.writeQueryRecord(ctx, rec)
		}
	}
	for waiting := true; waiting; {
		select {
		case rec := <-s.queryRecords:
			store(rec)
		case <-s.queryRunsDone:
			waiting = false
		case <-ctx.Done():
			waiting = false
		}
	}
	s.backgroundMu.Lock()
	s.queryRecordsClosed = true
	s.backgroundMu.Unlock()
	for {
		select {
		case rec := <-s.queryRecords:
			store(rec)
		default:
			return
		}
	}
}

func (s *Server) logBackgroundDrops() {
	if dropped := s.queryRecordsDropped.Load(); dropped > 0 {
		s.log.Warn("query records dropped: query history rows and query.run audit events", "dropped", dropped)
	}
	if dropped := s.tokenDropped.Load(); dropped > 0 {
		s.log.Warn("token last-used updates dropped", "dropped", dropped)
	}
}

// writeQueryRecord stores a finished query run: its query.run event, its
// history row, and its daily stats and shape, or its failure.
func (s *Server) writeQueryRecord(parent context.Context, rec queryRecord) {
	if rec.event != nil {
		s.audit.Record(parent, *rec.event)
	}
	if rec.history == nil && rec.usage == nil && rec.failure == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	now := time.Now()
	day := now.UTC().Format(time.DateOnly)
	// Demo logins are shared, so only aggregate counters are retained.
	if entry := rec.history; entry != nil && (s.config == nil || !s.config.Demo.ReadOnly) {
		if err := s.store.RecordQueryHistory(ctx, entry); err != nil {
			s.queryRecordsDropped.Add(1)
			s.log.Warn("failed to record query history", "error", err, "user_id", entry.UserID, "source_id", entry.SourceID)
		}
	}
	if u := rec.usage; u != nil {
		if err := s.store.IncrementQueryStats(ctx, models.QueryStatsIncrement{
			BucketDate: day, UserID: u.run.UserID, TeamID: u.run.TeamID, SourceID: u.run.SourceID, Language: u.run.QueryLanguage,
			DurationMs: u.run.DurationMs, RowsRead: u.rowsRead, BytesRead: u.bytesRead, RowsReturned: u.run.RowCount,
		}); err != nil {
			s.log.Warn("failed to increment query stats", "error", err, "user_id", u.run.UserID, "source_id", u.run.SourceID)
		}
		// A shape holds no user data, so it is recorded in demo mode too.
		s.recordQueryShape(parent, now, u)
	}
	if f := rec.failure; f != nil {
		inc := *f
		inc.BucketDate = day
		if err := s.store.IncrementQueryFailureStats(ctx, inc); err != nil {
			s.log.Warn("failed to increment query failure stats", "error", err, "user_id", f.UserID, "source_id", f.SourceID)
		}
	}
}
