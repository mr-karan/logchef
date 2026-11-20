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

// writeQueryRecord stores a finished query run: its query.run event and,
// when it ran, its history row, daily stats and shape.
func (s *Server) writeQueryRecord(parent context.Context, rec queryRecord) {
	if rec.event != nil {
		s.audit.Record(parent, *rec.event)
	}
	if rec.history != nil {
		s.writeQueryHistory(parent, rec.history)
	}
}

func (s *Server) writeQueryHistory(parent context.Context, entry *models.QueryHistory) {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	// Demo logins are shared, so only aggregate counters are retained.
	if s.config == nil || !s.config.Demo.ReadOnly {
		if err := s.store.RecordQueryHistory(ctx, entry); err != nil {
			s.queryRecordsDropped.Add(1)
			s.log.Warn("failed to record query history", "error", err, "user_id", entry.UserID, "source_id", entry.SourceID)
		}
	}
	now := time.Now()
	bucketDate := now.UTC().Format("2006-01-02")
	if err := s.store.IncrementQueryStats(ctx, bucketDate, entry.UserID, entry.TeamID, entry.SourceID, entry.QueryLanguage, entry.DurationMs); err != nil {
		s.log.Warn("failed to increment query stats", "error", err, "user_id", entry.UserID, "source_id", entry.SourceID)
	}
	// A shape holds no user data, so it is recorded in demo mode too.
	s.recordQueryShape(parent, now, entry)
}
