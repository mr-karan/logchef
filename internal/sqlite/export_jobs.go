package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

const createExportJobSQL = `
INSERT INTO export_jobs (
    id,
    team_id,
    source_id,
    created_by,
    status,
    format,
    request_json,
    expires_at,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

const getExportJobSQL = `
SELECT
    id,
    team_id,
    source_id,
    created_by,
    status,
    format,
    request_json,
    file_name,
    file_path,
    error_message,
    rows_exported,
    bytes_written,
    expires_at,
    completed_at,
    created_at,
    updated_at
FROM export_jobs
WHERE id = ?
`

const updateExportJobRunningSQL = `
UPDATE export_jobs
SET
    status = ?,
    error_message = NULL,
    updated_at = ?
WHERE id = ?
`

const completeExportJobSQL = `
UPDATE export_jobs
SET
    status = ?,
    file_name = ?,
    file_path = ?,
    error_message = NULL,
    rows_exported = ?,
    bytes_written = ?,
    completed_at = ?,
    updated_at = ?
WHERE id = ?
`

const failExportJobSQL = `
UPDATE export_jobs
SET
    status = ?,
    file_name = NULL,
    file_path = NULL,
    error_message = ?,
    completed_at = NULL,
    updated_at = ?
WHERE id = ?
`

const selectExpiredExportJobPathsSQL = `
SELECT file_path
FROM export_jobs
WHERE expires_at < ?
  AND file_path IS NOT NULL
`

const pruneExpiredExportJobsSQL = `
DELETE FROM export_jobs
WHERE expires_at < ?
`

func (db *DB) CreateExportJob(ctx context.Context, job *models.ExportJob) error {
	_, err := db.writeDB.ExecContext(ctx,
		createExportJobSQL,
		job.ID,
		int64(job.TeamID),
		int64(job.SourceID),
		int64(job.CreatedBy),
		string(job.Status),
		job.Format,
		string(job.RequestPayload),
		job.ExpiresAt,
		job.CreatedAt,
		job.UpdatedAt,
	)
	if err != nil {
		db.log.Error("failed to create export job", "error", err, "job_id", job.ID, "team_id", job.TeamID, "source_id", job.SourceID)
		return fmt.Errorf("error creating export job: %w", err)
	}
	return nil
}

func (db *DB) GetExportJob(ctx context.Context, id string) (*models.ExportJob, error) {
	var (
		job          models.ExportJob
		status       string
		requestJSON  string
		fileName     sql.NullString
		filePath     sql.NullString
		errorMessage sql.NullString
		completedAt  sql.NullTime
	)

	err := db.readDB.QueryRowContext(ctx, getExportJobSQL, id).Scan(
		&job.ID,
		&job.TeamID,
		&job.SourceID,
		&job.CreatedBy,
		&status,
		&job.Format,
		&requestJSON,
		&fileName,
		&filePath,
		&errorMessage,
		&job.RowsExported,
		&job.BytesWritten,
		&job.ExpiresAt,
		&completedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		db.log.Error("failed to get export job", "error", err, "job_id", id)
		return nil, fmt.Errorf("error getting export job: %w", err)
	}

	job.Status = models.ExportJobStatus(status)
	job.RequestPayload = []byte(requestJSON)
	if fileName.Valid {
		job.FileName = fileName.String
	}
	if filePath.Valid {
		job.FilePath = filePath.String
	}
	if errorMessage.Valid {
		job.ErrorMessage = errorMessage.String
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	return &job, nil
}

func (db *DB) UpdateExportJobRunning(ctx context.Context, id string, updatedAt time.Time) error {
	res, err := db.writeDB.ExecContext(ctx, updateExportJobRunningSQL, string(models.ExportJobStatusRunning), updatedAt, id)
	if err != nil {
		db.log.Error("failed to mark export job running", "error", err, "job_id", id)
		return fmt.Errorf("error updating export job status: %w", err)
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) CompleteExportJob(ctx context.Context, id, fileName, filePath string, rowsExported int, bytesWritten int64, completedAt time.Time) error {
	res, err := db.writeDB.ExecContext(ctx,
		completeExportJobSQL,
		string(models.ExportJobStatusComplete),
		fileName,
		filePath,
		rowsExported,
		bytesWritten,
		completedAt,
		completedAt,
		id,
	)
	if err != nil {
		db.log.Error("failed to complete export job", "error", err, "job_id", id)
		return fmt.Errorf("error completing export job: %w", err)
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) FailExportJob(ctx context.Context, id, errorMessage string, updatedAt time.Time) error {
	res, err := db.writeDB.ExecContext(ctx, failExportJobSQL, string(models.ExportJobStatusFailed), errorMessage, updatedAt, id)
	if err != nil {
		db.log.Error("failed to mark export job failed", "error", err, "job_id", id)
		return fmt.Errorf("error failing export job: %w", err)
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) PruneExpiredExportJobs(ctx context.Context, before time.Time) ([]string, error) {
	tx, err := db.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error starting export job prune transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, selectExpiredExportJobPathsSQL, before)
	if err != nil {
		db.log.Error("failed to list expired export job paths", "error", err)
		return nil, fmt.Errorf("error listing expired export job paths: %w", err)
	}
	defer rows.Close()

	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("error scanning expired export job path: %w", err)
		}
		if path != "" {
			paths = append(paths, path)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating expired export job paths: %w", err)
	}

	if _, err := tx.ExecContext(ctx, pruneExpiredExportJobsSQL, before); err != nil {
		db.log.Error("failed to prune expired export jobs", "error", err)
		return nil, fmt.Errorf("error pruning expired export jobs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("error committing export job prune transaction: %w", err)
	}

	return paths, nil
}
