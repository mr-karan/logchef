package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func exportJobToModel(r sqlc.ExportJob) *models.ExportJob {
	return &models.ExportJob{
		ID:             r.ID,
		SourceID:       models.SourceID(r.SourceID),
		CreatedBy:      models.UserID(r.CreatedBy),
		Status:         models.ExportJobStatus(r.Status),
		Format:         r.Format,
		RequestPayload: []byte(r.RequestJson),
		FileName:       textStr(r.FileName),
		FilePath:       textStr(r.FilePath),
		ErrorMessage:   textStr(r.ErrorMessage),
		RowsExported:   int(r.RowsExported),
		BytesWritten:   r.BytesWritten,
		ExpiresAt:      r.ExpiresAt.Time,
		CompletedAt:    tsPtr(r.CompletedAt),
		CreatedAt:      r.CreatedAt.Time,
		UpdatedAt:      r.UpdatedAt.Time,
	}
}

// CreateExportJob inserts a new export job.
func (s *Store) CreateExportJob(ctx context.Context, job *models.ExportJob) error {
	err := s.q.CreateExportJob(ctx, sqlc.CreateExportJobParams{
		ID:          job.ID,
		SourceID:    int64(job.SourceID),
		CreatedBy:   int64(job.CreatedBy),
		Status:      string(job.Status),
		Format:      job.Format,
		RequestJson: string(job.RequestPayload),
		ExpiresAt:   ts(job.ExpiresAt),
		CreatedAt:   ts(job.CreatedAt),
		UpdatedAt:   ts(job.UpdatedAt),
	})
	if err != nil {
		s.log.Error("failed to create export job", "error", err, "job_id", job.ID, "source_id", job.SourceID)
		return fmt.Errorf("error creating export job: %w", err)
	}
	return nil
}

// GetExportJob retrieves an export job by ID. Returns models.ErrNotFound if absent.
func (s *Store) GetExportJob(ctx context.Context, id string) (*models.ExportJob, error) {
	row, err := s.q.GetExportJob(ctx, id)
	if err != nil {
		if notFound(err) {
			return nil, models.ErrNotFound
		}
		s.log.Error("failed to get export job", "error", err, "job_id", id)
		return nil, fmt.Errorf("error getting export job: %w", err)
	}
	return exportJobToModel(row), nil
}

// UpdateExportJobRunning marks an export job running.
func (s *Store) UpdateExportJobRunning(ctx context.Context, id string, updatedAt time.Time) error {
	_, err := s.q.UpdateExportJobRunning(ctx, sqlc.UpdateExportJobRunningParams{
		Status:    string(models.ExportJobStatusRunning),
		UpdatedAt: ts(updatedAt),
		ID:        id,
	})
	if err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		s.log.Error("failed to mark export job running", "error", err, "job_id", id)
		return fmt.Errorf("error updating export job status: %w", err)
	}
	return nil
}

// CompleteExportJob marks an export job complete with its artifact details.
func (s *Store) CompleteExportJob(ctx context.Context, id, fileName, filePath string, rowsExported int, bytesWritten int64, completedAt time.Time) error {
	_, err := s.q.CompleteExportJob(ctx, sqlc.CompleteExportJobParams{
		Status:       string(models.ExportJobStatusComplete),
		FileName:     text(fileName),
		FilePath:     text(filePath),
		RowsExported: int64(rowsExported),
		BytesWritten: bytesWritten,
		CompletedAt:  ts(completedAt),
		UpdatedAt:    ts(completedAt),
		ID:           id,
	})
	if err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		s.log.Error("failed to complete export job", "error", err, "job_id", id)
		return fmt.Errorf("error completing export job: %w", err)
	}
	return nil
}

// FailExportJob marks an export job failed with an error message.
func (s *Store) FailExportJob(ctx context.Context, id, errorMessage string, updatedAt time.Time) error {
	_, err := s.q.FailExportJob(ctx, sqlc.FailExportJobParams{
		Status:       string(models.ExportJobStatusFailed),
		ErrorMessage: text(errorMessage),
		UpdatedAt:    ts(updatedAt),
		ID:           id,
	})
	if err != nil {
		if notFound(err) {
			return models.ErrNotFound
		}
		s.log.Error("failed to mark export job failed", "error", err, "job_id", id)
		return fmt.Errorf("error failing export job: %w", err)
	}
	return nil
}

// ListExpiredExportJobPaths returns artifact paths for jobs expiring before t.
func (s *Store) ListExpiredExportJobPaths(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := s.q.ListExpiredExportJobPaths(ctx, ts(before))
	if err != nil {
		s.log.Error("failed to list expired export job paths", "error", err)
		return nil, fmt.Errorf("error listing expired export job paths: %w", err)
	}
	paths := make([]string, 0, len(rows))
	for _, p := range rows {
		if p.Valid && p.String != "" {
			paths = append(paths, p.String)
		}
	}
	return paths, nil
}

// DeleteExpiredExportJobs removes rows expiring before t.
func (s *Store) DeleteExpiredExportJobs(ctx context.Context, before time.Time) error {
	if err := s.q.DeleteExpiredExportJobs(ctx, ts(before)); err != nil {
		s.log.Error("failed to prune expired export jobs", "error", err)
		return fmt.Errorf("error pruning expired export jobs: %w", err)
	}
	return nil
}
