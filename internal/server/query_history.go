package server

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// recordQueryHistory queues a best-effort write without blocking the query.
// start is the query's start time, nil when the request had none.
func (s *Server) recordQueryHistory(user *models.User, teamID models.TeamID, source *models.Source, start *time.Time, queryText string, language models.QueryLanguage, durationMs, rowCount int64) {
	if user == nil {
		return
	}
	entry := &models.QueryHistory{
		UserID:        user.ID,
		TeamID:        teamID,
		SourceID:      source.ID,
		QueryText:     queryText,
		QueryLanguage: models.NormalizeQueryLanguage(language),
		DurationMs:    durationMs,
		RowCount:      rowCount,
	}
	if start != nil {
		lookback := max(int64(time.Since(*start)/time.Second), 0)
		entry.LookbackSeconds = &lookback
	}
	if source.IsClickHouse() {
		entry.SourceDatabase, entry.SourceTable = source.Connection.Database, source.Connection.TableName
	}
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	select {
	case <-s.stop:
		s.historyDropped.Add(1)
		return
	default:
	}
	select {
	case s.historyQueue <- entry:
	default:
		s.historyDropped.Add(1)
	}
}

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
			case entry := <-s.historyQueue:
				s.writeQueryHistory(context.WithoutCancel(s.baseCtx), entry)
			}
		}
	})
}

func (s *Server) drainQueryHistory() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.historyDrainCtx), 4*time.Second)
	defer cancel()
	if deadline, ok := s.historyDrainCtx.Deadline(); ok {
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithDeadline(ctx, deadline.Add(-100*time.Millisecond))
		defer deadlineCancel()
	}
	for {
		select {
		case entry := <-s.historyQueue:
			if ctx.Err() != nil {
				s.historyDropped.Add(1)
			} else {
				s.writeQueryHistory(ctx, entry)
			}
		default:
			return
		}
	}
}

func (s *Server) logBackgroundDrops() {
	if dropped := s.historyDropped.Load(); dropped > 0 {
		s.log.Warn("query history entries dropped", "dropped", dropped)
	}
	if dropped := s.tokenDropped.Load(); dropped > 0 {
		s.log.Warn("token last-used updates dropped", "dropped", dropped)
	}
}

func (s *Server) writeQueryHistory(parent context.Context, entry *models.QueryHistory) {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	// Demo logins are shared, so only aggregate counters are retained.
	if s.config == nil || !s.config.Demo.ReadOnly {
		if err := s.store.RecordQueryHistory(ctx, entry); err != nil {
			s.historyDropped.Add(1)
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
