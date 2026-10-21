package server

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// recordQueryHistory queues a best-effort write without blocking the query.
func (s *Server) recordQueryHistory(user *models.User, teamID models.TeamID, sourceID models.SourceID, queryText string, language models.QueryLanguage, durationMs, rowCount int64) {
	if user == nil {
		return
	}
	entry := &models.QueryHistory{
		UserID:        user.ID,
		TeamID:        teamID,
		SourceID:      sourceID,
		QueryText:     queryText,
		QueryLanguage: models.NormalizeQueryLanguage(language),
		DurationMs:    durationMs,
		RowCount:      rowCount,
	}
	select {
	case <-s.stop:
		return
	default:
	}
	select {
	case s.historyQueue <- entry:
	default:
		s.log.Warn("query history queue full", "dropped", s.historyDropped.Add(1))
	}
}

func (s *Server) startQueryHistoryWorker() {
	s.startBackgroundTask(func() {
		for {
			select {
			case <-s.baseCtx.Done():
				return
			case entry := <-s.historyQueue:
				s.writeQueryHistory(entry)
			}
		}
	})
}

func (s *Server) writeQueryHistory(entry *models.QueryHistory) {
	ctx, cancel := context.WithTimeout(s.baseCtx, time.Second)
	defer cancel()
	// Demo logins are shared, so only aggregate counters are retained.
	if s.config == nil || !s.config.Demo.ReadOnly {
		if err := s.sqlite.RecordQueryHistory(ctx, entry); err != nil {
			s.log.Warn("failed to record query history", "error", err, "user_id", entry.UserID, "source_id", entry.SourceID)
		}
	}
	bucketDate := time.Now().UTC().Format("2006-01-02")
	if err := s.sqlite.IncrementQueryStats(ctx, bucketDate, entry.UserID, entry.TeamID, entry.SourceID, entry.QueryLanguage, entry.DurationMs); err != nil {
		s.log.Warn("failed to increment query stats", "error", err, "user_id", entry.UserID, "source_id", entry.SourceID)
	}
}
