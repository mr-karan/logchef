package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (db *DB) CreateExportJob(ctx context.Context, job *models.ExportJob) error {
	err := db.writeQueries.CreateExportJob(ctx, sqlc.CreateExportJobParams{
		ID:          job.ID,
		TeamID:      int64(job.TeamID),
		SourceID:    int64(job.SourceID),
		CreatedBy:   int64(job.CreatedBy),
		Status:      string(job.Status),
		Format:      job.Format,
		RequestJson: string(job.RequestPayload),
		ExpiresAt:   job.ExpiresAt,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
	})
	if err != nil {
		db.log.Error("failed to create export job", "error", err, "job_id", job.ID, "team_id", job.TeamID, "source_id", job.SourceID)
		return fmt.Errorf("error creating export job: %w", err)
	}
	return nil
}

func (db *DB) GetExportJob(ctx context.Context, id string) (*models.ExportJob, error) {
	row, err := db.readQueries.GetExportJob(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		db.log.Error("failed to get export job", "error", err, "job_id", id)
		return nil, fmt.Errorf("error getting export job: %w", err)
	}
	return exportJobFromSQLC(row), nil
}

func (db *DB) UpdateExportJobRunning(ctx context.Context, id string, updatedAt time.Time) error {
	_, err := db.writeQueries.UpdateExportJobRunning(ctx, sqlc.UpdateExportJobRunningParams{
		Status:    string(models.ExportJobStatusRunning),
		UpdatedAt: updatedAt,
		ID:        id,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return err
		}
		db.log.Error("failed to mark export job running", "error", err, "job_id", id)
		return fmt.Errorf("error updating export job status: %w", err)
	}
	return nil
}

func (db *DB) CompleteExportJob(ctx context.Context, id, fileName, filePath string, rowsExported int, bytesWritten int64, completedAt time.Time) error {
	_, err := db.writeQueries.CompleteExportJob(ctx, sqlc.CompleteExportJobParams{
		Status:       string(models.ExportJobStatusComplete),
		FileName:     nullString(fileName),
		FilePath:     nullString(filePath),
		RowsExported: int64(rowsExported),
		BytesWritten: bytesWritten,
		CompletedAt:  sql.NullTime{Time: completedAt, Valid: true},
		UpdatedAt:    completedAt,
		ID:           id,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return err
		}
		db.log.Error("failed to complete export job", "error", err, "job_id", id)
		return fmt.Errorf("error completing export job: %w", err)
	}
	return nil
}

func (db *DB) FailExportJob(ctx context.Context, id, errorMessage string, updatedAt time.Time) error {
	_, err := db.writeQueries.FailExportJob(ctx, sqlc.FailExportJobParams{
		Status:       string(models.ExportJobStatusFailed),
		ErrorMessage: nullString(errorMessage),
		UpdatedAt:    updatedAt,
		ID:           id,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return err
		}
		db.log.Error("failed to mark export job failed", "error", err, "job_id", id)
		return fmt.Errorf("error failing export job: %w", err)
	}
	return nil
}

// ListExpiredExportJobPaths returns artifact paths for jobs whose
// expires_at is before the given time, without deleting anything.
// Callers should unlink the files first, then call DeleteExpiredExportJobs.
func (db *DB) ListExpiredExportJobPaths(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := db.readQueries.ListExpiredExportJobPaths(ctx, before)
	if err != nil {
		db.log.Error("failed to list expired export job paths", "error", err)
		return nil, fmt.Errorf("error listing expired export job paths: %w", err)
	}

	paths := make([]string, 0, len(rows))
	for _, path := range rows {
		if path.Valid && path.String != "" {
			paths = append(paths, path.String)
		}
	}
	return paths, nil
}

// DeleteExpiredExportJobs removes rows whose expires_at is before the given time.
func (db *DB) DeleteExpiredExportJobs(ctx context.Context, before time.Time) error {
	if err := db.writeQueries.DeleteExpiredExportJobs(ctx, before); err != nil {
		db.log.Error("failed to prune expired export jobs", "error", err)
		return fmt.Errorf("error pruning expired export jobs: %w", err)
	}
	return nil
}

func exportJobFromSQLC(row sqlc.ExportJob) *models.ExportJob {
	job := &models.ExportJob{
		ID:             row.ID,
		TeamID:         models.TeamID(row.TeamID),
		SourceID:       models.SourceID(row.SourceID),
		CreatedBy:      models.UserID(row.CreatedBy),
		Status:         models.ExportJobStatus(row.Status),
		Format:         row.Format,
		RequestPayload: []byte(row.RequestJson),
		RowsExported:   int(row.RowsExported),
		BytesWritten:   row.BytesWritten,
		ExpiresAt:      row.ExpiresAt,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
	if row.FileName.Valid {
		job.FileName = row.FileName.String
	}
	if row.FilePath.Valid {
		job.FilePath = row.FilePath.String
	}
	if row.ErrorMessage.Valid {
		job.ErrorMessage = row.ErrorMessage.String
	}
	if row.CompletedAt.Valid {
		job.CompletedAt = &row.CompletedAt.Time
	}
	return job
}
