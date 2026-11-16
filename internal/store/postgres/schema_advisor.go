package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// GetSchemaAdvisorReport returns the last schema advisor report of a source.
func (s *Store) GetSchemaAdvisorReport(ctx context.Context, sourceID models.SourceID) (*models.StoredSchemaAdvisorReport, error) {
	row, err := s.q.GetSchemaAdvisorReport(ctx, int64(sourceID))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("error getting schema advisor report of source %d: %w", sourceID, err)
	}
	stored := &models.StoredSchemaAdvisorReport{
		SourceID:    models.SourceID(row.SourceID),
		GeneratedAt: row.GeneratedAt.Time.UTC(),
		WindowDays:  int(row.WindowDays),
	}
	if row.GeneratedBy.Valid {
		by := models.UserID(row.GeneratedBy.Int64)
		stored.GeneratedBy = &by
	}
	if err := json.Unmarshal([]byte(row.Report), &stored.Report); err != nil {
		return nil, fmt.Errorf("error decoding schema advisor report of source %d: %w", sourceID, err)
	}
	return stored, nil
}

// UpsertSchemaAdvisorReport replaces the schema advisor report of a source.
func (s *Store) UpsertSchemaAdvisorReport(ctx context.Context, report *models.StoredSchemaAdvisorReport) error {
	encoded, err := json.Marshal(report.Report)
	if err != nil {
		return fmt.Errorf("error encoding schema advisor report: %w", err)
	}
	var by *int64
	if report.GeneratedBy != nil {
		id := int64(*report.GeneratedBy)
		by = &id
	}
	if err := s.q.UpsertSchemaAdvisorReport(ctx, sqlc.UpsertSchemaAdvisorReportParams{
		SourceID:    int64(report.SourceID),
		GeneratedAt: ts(report.GeneratedAt),
		GeneratedBy: int8FromPtr(by),
		WindowDays:  int64(report.WindowDays),
		Report:      string(encoded),
	}); err != nil {
		return fmt.Errorf("error saving schema advisor report of source %d: %w", report.SourceID, err)
	}
	return nil
}
