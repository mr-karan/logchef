package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// GetSchemaAdvisorReport returns the last schema advisor report of a source.
func (db *DB) GetSchemaAdvisorReport(ctx context.Context, sourceID models.SourceID) (*models.StoredSchemaAdvisorReport, error) {
	row, err := db.readQueries.GetSchemaAdvisorReport(ctx, int64(sourceID))
	if err != nil {
		return nil, handleNotFoundError(err, fmt.Sprintf("error getting schema advisor report of source %d", sourceID))
	}
	stored := &models.StoredSchemaAdvisorReport{
		SourceID:    models.SourceID(row.SourceID),
		GeneratedAt: row.GeneratedAt,
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
func (db *DB) UpsertSchemaAdvisorReport(ctx context.Context, report *models.StoredSchemaAdvisorReport) error {
	encoded, err := json.Marshal(report.Report)
	if err != nil {
		return fmt.Errorf("error encoding schema advisor report: %w", err)
	}
	var by sql.NullInt64
	if report.GeneratedBy != nil {
		by = sql.NullInt64{Int64: int64(*report.GeneratedBy), Valid: true}
	}
	if err := db.writeQueries.UpsertSchemaAdvisorReport(ctx, sqlc.UpsertSchemaAdvisorReportParams{
		SourceID:    int64(report.SourceID),
		GeneratedAt: report.GeneratedAt.UTC(),
		GeneratedBy: by,
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
		DismissedAt: row.DismissedAt.UTC(),
	}
	if row.DismissedBy.Valid {
		by := models.UserID(row.DismissedBy.Int64)
		d.DismissedBy = &by
	}
	return d
}

// ListSchemaAdvisorDismissals returns the dismissed findings of a source.
func (db *DB) ListSchemaAdvisorDismissals(ctx context.Context, sourceID models.SourceID) ([]models.SchemaAdvisorDismissal, error) {
	rows, err := db.readQueries.ListSchemaAdvisorDismissals(ctx, int64(sourceID))
	if err != nil {
		return nil, fmt.Errorf("error listing schema advisor dismissals of source %d: %w", sourceID, err)
	}
	out := make([]models.SchemaAdvisorDismissal, len(rows))
	for i, row := range rows {
		out[i] = dismissalFromRow(row)
	}
	return out, nil
}

// GetSchemaAdvisorDismissal returns the dismissal of one finding.
func (db *DB) GetSchemaAdvisorDismissal(ctx context.Context, sourceID models.SourceID, findingID string) (*models.SchemaAdvisorDismissal, error) {
	row, err := db.readQueries.GetSchemaAdvisorDismissal(ctx, sqlc.GetSchemaAdvisorDismissalParams{SourceID: int64(sourceID), FindingID: findingID})
	if err != nil {
		return nil, handleNotFoundError(err, fmt.Sprintf("error getting dismissal of finding %s of source %d", findingID, sourceID))
	}
	d := dismissalFromRow(row)
	return &d, nil
}

// UpsertSchemaAdvisorDismissal dismisses a finding.
func (db *DB) UpsertSchemaAdvisorDismissal(ctx context.Context, d *models.SchemaAdvisorDismissal) error {
	var by sql.NullInt64
	if d.DismissedBy != nil {
		by = sql.NullInt64{Int64: int64(*d.DismissedBy), Valid: true}
	}
	if err := db.writeQueries.UpsertSchemaAdvisorDismissal(ctx, sqlc.UpsertSchemaAdvisorDismissalParams{
		SourceID:    int64(d.SourceID),
		FindingID:   d.FindingID,
		Reason:      d.Reason,
		DismissedBy: by,
		DismissedAt: d.DismissedAt.UTC(),
	}); err != nil {
		return fmt.Errorf("error saving dismissal of finding %s of source %d: %w", d.FindingID, d.SourceID, err)
	}
	return nil
}

// DeleteSchemaAdvisorDismissal restores a dismissed finding.
func (db *DB) DeleteSchemaAdvisorDismissal(ctx context.Context, sourceID models.SourceID, findingID string) error {
	n, err := db.writeQueries.DeleteSchemaAdvisorDismissal(ctx, sqlc.DeleteSchemaAdvisorDismissalParams{SourceID: int64(sourceID), FindingID: findingID})
	if err != nil {
		return fmt.Errorf("error deleting dismissal of finding %s of source %d: %w", findingID, sourceID, err)
	}
	if n == 0 {
		return models.ErrNotFound
	}
	return nil
}
