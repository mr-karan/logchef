package server

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// recordQueryHistory persists one executed-query record best-effort and
// non-blocking: it fires a goroutine with its own short-lived context so a slow
// or failing write never delays or fails the user's query. Errors are logged
// and swallowed. Called only after a query executed successfully on the preview
// paths.
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
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.sqlite.RecordQueryHistory(ctx, entry); err != nil {
			s.log.Warn("failed to record query history", "error", err, "user_id", entry.UserID, "source_id", sourceID)
		}
		// Also increment the non-pruned daily rollup so all-time usage analytics
		// stay correct even after query_history is pruned per user. Best-effort:
		// log and swallow so recording never blocks or fails the query path.
		bucketDate := time.Now().UTC().Format("2006-01-02")
		if err := s.sqlite.IncrementQueryStats(ctx, bucketDate, entry.UserID, teamID, sourceID, entry.QueryLanguage, durationMs); err != nil {
			s.log.Warn("failed to increment query stats", "error", err, "user_id", entry.UserID, "source_id", sourceID)
		}
	}()
}
