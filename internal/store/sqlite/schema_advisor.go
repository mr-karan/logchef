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
