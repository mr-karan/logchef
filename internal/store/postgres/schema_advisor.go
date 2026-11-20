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
	return reportFromRow(row)
}

// ListSchemaAdvisorReports returns the reports of those of the sources that
// have one.
func (s *Store) ListSchemaAdvisorReports(ctx context.Context, sourceIDs []models.SourceID) ([]*models.StoredSchemaAdvisorReport, error) {
	rows, err := s.q.ListSchemaAdvisorReportsBySources(ctx, sourceIDParams(sourceIDs))
	if err != nil {
		return nil, fmt.Errorf("error listing schema advisor reports: %w", err)
	}
	out := make([]*models.StoredSchemaAdvisorReport, 0, len(rows))
	for _, row := range rows {
		stored, err := reportFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

func reportFromRow(row sqlc.SchemaAdvisorReport) (*models.StoredSchemaAdvisorReport, error) {
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
		return nil, fmt.Errorf("error decoding schema advisor report of source %d: %w", row.SourceID, err)
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

func dismissalFromRow(row sqlc.SchemaAdvisorDismissal) models.SchemaAdvisorDismissal {
	d := models.SchemaAdvisorDismissal{
		SourceID:    models.SourceID(row.SourceID),
		FindingID:   row.FindingID,
		Reason:      row.Reason,
		DismissedAt: row.DismissedAt.Time.UTC(),
	}
	if row.DismissedBy.Valid {
		by := models.UserID(row.DismissedBy.Int64)
		d.DismissedBy = &by
	}
	return d
}

// ListSchemaAdvisorDismissals returns the dismissed findings of a source.
func (s *Store) ListSchemaAdvisorDismissals(ctx context.Context, sourceID models.SourceID) ([]models.SchemaAdvisorDismissal, error) {
	rows, err := s.q.ListSchemaAdvisorDismissals(ctx, int64(sourceID))
	if err != nil {
		return nil, fmt.Errorf("error listing schema advisor dismissals of source %d: %w", sourceID, err)
	}
	out := make([]models.SchemaAdvisorDismissal, len(rows))
	for i, row := range rows {
		out[i] = dismissalFromRow(row)
	}
	return out, nil
}

// ListSchemaAdvisorDismissalsBySources returns the dismissed findings of the
// sources.
func (s *Store) ListSchemaAdvisorDismissalsBySources(ctx context.Context, sourceIDs []models.SourceID) ([]models.SchemaAdvisorDismissal, error) {
	rows, err := s.q.ListSchemaAdvisorDismissalsBySources(ctx, sourceIDParams(sourceIDs))
	if err != nil {
		return nil, fmt.Errorf("error listing schema advisor dismissals: %w", err)
	}
	out := make([]models.SchemaAdvisorDismissal, len(rows))
	for i, row := range rows {
		out[i] = dismissalFromRow(row)
	}
	return out, nil
}

func sourceIDParams(ids []models.SourceID) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}

// GetSchemaAdvisorDismissal returns the dismissal of one finding.
func (s *Store) GetSchemaAdvisorDismissal(ctx context.Context, sourceID models.SourceID, findingID string) (*models.SchemaAdvisorDismissal, error) {
	row, err := s.q.GetSchemaAdvisorDismissal(ctx, sqlc.GetSchemaAdvisorDismissalParams{SourceID: int64(sourceID), FindingID: findingID})
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		return nil, fmt.Errorf("error getting dismissal of finding %s of source %d: %w", findingID, sourceID, err)
	}
	d := dismissalFromRow(row)
	return &d, nil
}

// UpsertSchemaAdvisorDismissal dismisses a finding.
func (s *Store) UpsertSchemaAdvisorDismissal(ctx context.Context, d *models.SchemaAdvisorDismissal) error {
	var by *int64
	if d.DismissedBy != nil {
		id := int64(*d.DismissedBy)
		by = &id
	}
	if err := s.q.UpsertSchemaAdvisorDismissal(ctx, sqlc.UpsertSchemaAdvisorDismissalParams{
		SourceID:    int64(d.SourceID),
		FindingID:   d.FindingID,
		Reason:      d.Reason,
		DismissedBy: int8FromPtr(by),
		DismissedAt: ts(d.DismissedAt),
	}); err != nil {
		return fmt.Errorf("error saving dismissal of finding %s of source %d: %w", d.FindingID, d.SourceID, err)
	}
	return nil
}

// DeleteSchemaAdvisorDismissal restores a dismissed finding.
func (s *Store) DeleteSchemaAdvisorDismissal(ctx context.Context, sourceID models.SourceID, findingID string) error {
	n, err := s.q.DeleteSchemaAdvisorDismissal(ctx, sqlc.DeleteSchemaAdvisorDismissalParams{SourceID: int64(sourceID), FindingID: findingID})
	if err != nil {
		return fmt.Errorf("error deleting dismissal of finding %s of source %d: %w", findingID, sourceID, err)
	}
	if n == 0 {
		return models.ErrNotFound
	}
	return nil
}
