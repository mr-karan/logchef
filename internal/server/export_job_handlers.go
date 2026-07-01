package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Server) handleCreateExportJob(c *fiber.Ctx) error {
	teamID, err := core.ParseTeamID(c.Params("teamID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	var req models.CreateExportJobRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if strings.TrimSpace(req.RawSQL) == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "raw_sql is required", models.ValidationErrorType)
	}

	formatInput := strings.TrimSpace(req.Format)
	format := formatInput
	if format == "" {
		format = "csv"
	} else {
		normalized, ok := normalizeExplicitExportFormat(format)
		if !ok {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Unsupported export format. Use csv or ndjson.", models.ValidationErrorType)
		}
		format = normalized
	}
	if !isExportFormatAllowed(format, s.config.Export.Formats) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Unsupported export format. Use csv or ndjson.", models.ValidationErrorType)
	}

	if req.QueryTimeout == nil {
		defaultTimeout := s.config.Export.DefaultTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}
	if err := models.ValidateQueryTimeout(req.QueryTimeout); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	if s.config.Export.MaxTimeoutSeconds > 0 && *req.QueryTimeout > s.config.Export.MaxTimeoutSeconds {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Query timeout cannot exceed %d seconds for Download", s.config.Export.MaxTimeoutSeconds),
			models.ValidationErrorType)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		s.log.Error("failed to marshal export job request", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create export job", models.GeneralErrorType)
	}

	now := time.Now().UTC()
	job := &models.ExportJob{
		ID:             uuid.New().String(),
		SourceID:       sourceID,
		CreatedBy:      user.ID,
		Status:         models.ExportJobStatusPending,
		Format:         format,
		RequestPayload: payload,
		ExpiresAt:      now.Add(s.config.Export.ArtifactTTL),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Admit synchronously so a saturated cap returns 429 to the client
	// instead of accepting the job and async-failing it.
	queryCtx, cancel := context.WithCancel(context.Background())
	if err := queryTracker.StartQueryWithID(
		job.ID,
		QueryClassExport,
		user.ID,
		sourceID,
		teamID,
		req.RawSQL,
		cancel,
		s.config.Export.MaxConcurrentPerUser,
		s.config.Export.MaxConcurrentGlobal,
	); err != nil {
		cancel()
		var admissionErr *QueryAdmissionError
		if errors.As(err, &admissionErr) {
			return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to track export query", models.GeneralErrorType)
	}

	if err := s.sqlite.CreateExportJob(c.Context(), job); err != nil {
		queryTracker.RemoveQuery(job.ID)
		cancel()
		s.log.Error("failed to persist export job", "error", err, "job_id", job.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create export job", models.GeneralErrorType)
	}

	runReq := exportLogsRequest{
		RawSQL:       req.RawSQL,
		Format:       format,
		Limit:        req.Limit,
		QueryTimeout: req.QueryTimeout,
		Variables:    req.Variables,
	}
	go s.runExportJob(job.ID, queryCtx, cancel, teamID, sourceID, user.Email, runReq)

	return SendSuccess(c, fiber.StatusAccepted, exportJobResponse(teamID, job))
}

func (s *Server) handleGetExportJob(c *fiber.Ctx) error {
	job, err := s.authorizeExportJob(c)
	if err != nil {
		return err
	}
	if time.Now().UTC().After(job.ExpiresAt) {
		return SendErrorWithType(c, fiber.StatusGone, "Export has expired", models.NotFoundErrorType)
	}
	teamID, _ := core.ParseTeamID(c.Params("teamID"))
	return SendSuccess(c, fiber.StatusOK, exportJobResponse(teamID, job))
}

func (s *Server) handleDownloadExportJob(c *fiber.Ctx) error {
	job, err := s.authorizeExportJob(c)
	if err != nil {
		return err
	}
	if time.Now().UTC().After(job.ExpiresAt) {
		return SendErrorWithType(c, fiber.StatusGone, "Export has expired", models.NotFoundErrorType)
	}

	switch job.Status {
	case models.ExportJobStatusPending, models.ExportJobStatusRunning:
		return SendErrorWithType(c, fiber.StatusConflict, "Export is still being prepared", models.ValidationErrorType)
	case models.ExportJobStatusFailed:
		message := job.ErrorMessage
		if message == "" {
			message = "Export failed"
		}
		return SendErrorWithType(c, fiber.StatusConflict, message, models.GeneralErrorType)
	case models.ExportJobStatusComplete:
		// handled below
	default:
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Export job is in an unknown state", models.GeneralErrorType)
	}

	if strings.TrimSpace(job.FilePath) == "" {
		_ = s.sqlite.FailExportJob(c.Context(), job.ID, "export artifact is unavailable", time.Now().UTC())
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Export artifact is unavailable", models.GeneralErrorType)
	}
	if _, err := os.Stat(job.FilePath); err != nil {
		s.log.Error("failed to stat export artifact", "error", err, "job_id", job.ID, "path", job.FilePath)
		if errors.Is(err, os.ErrNotExist) {
			_ = s.sqlite.FailExportJob(c.Context(), job.ID, "export artifact is unavailable", time.Now().UTC())
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Export artifact is unavailable", models.GeneralErrorType)
	}

	return c.Download(job.FilePath, job.FileName)
}

func (s *Server) authorizeExportJob(c *fiber.Ctx) (*models.ExportJob, error) {
	teamID, err := core.ParseTeamID(c.Params("teamID"))
	if err != nil {
		return nil, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return nil, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}
	exportID := strings.TrimSpace(c.Params("exportID"))
	if exportID == "" {
		return nil, SendErrorWithType(c, fiber.StatusBadRequest, "Export ID is required", models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	if user == nil {
		return nil, SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	job, err := s.sqlite.GetExportJob(c.Context(), exportID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, SendErrorWithType(c, fiber.StatusNotFound, "Export job not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get export job", "error", err, "job_id", exportID)
		return nil, SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get export job", models.GeneralErrorType)
	}
	if job.SourceID != sourceID {
		return nil, SendErrorWithType(c, fiber.StatusNotFound, "Export job not found", models.NotFoundErrorType)
	}
	_ = teamID // teamID in the URL is an auth gate via middleware; no need to compare on the row
	if user.Role != models.UserRoleAdmin && job.CreatedBy != user.ID {
		return nil, SendErrorWithType(c, fiber.StatusForbidden, "You do not have access to this export", models.AuthorizationErrorType)
	}
	return job, nil
}

// runExportJob runs the export pipeline. The caller must have already
// reserved an admission slot via queryTracker.StartQueryWithID — this
// function takes ownership and releases it on exit.
func (s *Server) runExportJob(jobID string, queryCtx context.Context, cancel context.CancelFunc, teamID models.TeamID, sourceID models.SourceID, userEmail string, req exportLogsRequest) {
	defer cancel()
	defer queryTracker.RemoveQuery(jobID)

	// Job-state writes must record the outcome even if the query context is
	// canceled, so derive a detached context that still carries request values.
	bgCtx := context.WithoutCancel(queryCtx)

	now := time.Now().UTC()
	if err := s.sqlite.UpdateExportJobRunning(bgCtx, jobID, now); err != nil {
		s.log.Error("failed to mark export job running", "error", err, "job_id", jobID)
		return
	}

	processedSQL := req.RawSQL
	if len(req.Variables) > 0 {
		vars := make([]template.Variable, len(req.Variables))
		for i, v := range req.Variables {
			vars[i] = template.Variable{
				Name:  v.Name,
				Type:  template.VariableType(v.Type),
				Value: v.Value,
			}
		}
		substituted, err := template.SubstituteVariables(req.RawSQL, vars)
		if err != nil {
			s.failExportJob(bgCtx, jobID, "", fmt.Sprintf("Variable substitution failed: %v", err))
			return
		}
		processedSQL = substituted
	}

	source, err := s.sqlite.GetSource(bgCtx, sourceID)
	if err != nil {
		s.failExportJob(bgCtx, jobID, "", "Source not found")
		return
	}
	client, err := s.clickhouse.GetConnection(sourceID)
	if err != nil {
		s.log.Error("failed to get clickhouse client for export job", "source_id", sourceID, "error", err, "job_id", jobID)
		s.failExportJob(bgCtx, jobID, "", "Failed to get source connection")
		return
	}

	exportLimit := req.Limit
	if exportLimit <= 0 {
		exportLimit = s.config.Export.MaxRows
	}
	if exportLimit > s.config.Export.MaxRows {
		exportLimit = s.config.Export.MaxRows
	}

	qb := clickhouse.NewExtendedQueryBuilder(source.GetFullTableName(), s.config.Export.MaxRows)
	buildResult, err := qb.BuildRawQueryWithLimitPolicy(processedSQL, req.Limit, exportLimit, s.config.Export.MaxRows)
	if err != nil {
		s.failExportJob(bgCtx, jobID, "", fmt.Sprintf("Invalid request: %v", err))
		return
	}

	opts := clickhouse.QueryOptions{
		TimeoutSeconds: req.QueryTimeout,
		Settings: map[string]interface{}{
			"max_execution_time":   *req.QueryTimeout,
			"max_result_rows":      buildResult.AppliedLimit,
			"result_overflow_mode": "break",
		},
		LimitApplied: buildResult.AppliedLimit,
		MaxRows:      buildResult.AppliedLimit,
	}

	_, extension := exportContentType(req.Format)
	fileName := fmt.Sprintf("logchef-%s.%s", time.Now().UTC().Format("20060102-150405"), extension)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("logchef-export-%s-*."+extension, jobID))
	if err != nil {
		s.log.Error("failed to create export artifact", "error", err, "job_id", jobID)
		s.failExportJob(bgCtx, jobID, "", "Failed to create export artifact")
		return
	}
	filePath := tmpFile.Name()
	writer := newExportRowWriter(req.Format, bufio.NewWriter(tmpFile), jobID, buildResult.AppliedLimit)

	stats, err := client.QueryStream(queryCtx, buildResult.SQL, opts, writer)
	if err != nil {
		s.log.Error("failed to execute export job", "error", err, "source_id", sourceID, "job_id", jobID)
		_ = tmpFile.Close()
		s.failExportJob(bgCtx, jobID, filePath, exportFailureMessage(err))
		return
	}
	if err := tmpFile.Close(); err != nil {
		s.log.Error("failed to close export artifact", "error", err, "job_id", jobID)
		s.failExportJob(bgCtx, jobID, filePath, "Failed to finalize export artifact")
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		s.log.Error("failed to stat export artifact", "error", err, "job_id", jobID, "path", filePath)
		s.failExportJob(bgCtx, jobID, filePath, "Failed to finalize export artifact")
		return
	}

	completedAt := time.Now().UTC()
	if err := s.sqlite.CompleteExportJob(bgCtx, jobID, fileName, filePath, stats.RowsReturned, info.Size(), completedAt); err != nil {
		s.log.Error("failed to complete export job", "error", err, "job_id", jobID)
		s.failExportJob(bgCtx, jobID, filePath, "Failed to persist export metadata")
		return
	}

	s.log.Info("query.export.job.complete",
		"user", userEmail,
		"team_id", teamID,
		"source_id", sourceID,
		"job_id", jobID,
		"format", req.Format,
		"rows", stats.RowsReturned,
		"duration_ms", stats.ExecutionTimeMs,
		"limit_applied", stats.LimitApplied,
		"bytes_written", info.Size(),
	)
}

func (s *Server) failExportJob(ctx context.Context, jobID, filePath, message string) {
	if filePath != "" {
		if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("failed to remove partial export artifact", "error", err, "job_id", jobID, "path", filePath)
		}
	}
	if err := s.sqlite.FailExportJob(ctx, jobID, message, time.Now().UTC()); err != nil {
		s.log.Error("failed to mark export job failed", "error", err, "job_id", jobID)
	}
}

func exportFailureMessage(err error) string {
	var admissionErr *QueryAdmissionError
	if errors.As(err, &admissionErr) {
		return admissionErr.Message
	}
	return err.Error()
}

func exportJobResponse(teamID models.TeamID, job *models.ExportJob) models.ExportJobResponse {
	return models.ExportJobResponse{
		ID:           job.ID,
		Status:       job.Status,
		Format:       job.Format,
		FileName:     job.FileName,
		ErrorMessage: job.ErrorMessage,
		RowsExported: job.RowsExported,
		BytesWritten: job.BytesWritten,
		ExpiresAt:    job.ExpiresAt,
		CompletedAt:  job.CompletedAt,
		CreatedAt:    job.CreatedAt,
		UpdatedAt:    job.UpdatedAt,
		StatusURL:    buildExportJobStatusURL(teamID, job),
		DownloadURL:  buildExportJobDownloadURL(teamID, job),
	}
}

func buildExportJobStatusURL(teamID models.TeamID, job *models.ExportJob) string {
	return fmt.Sprintf("/api/v1/teams/%d/sources/%d/exports/%s", teamID, job.SourceID, job.ID)
}

func buildExportJobDownloadURL(teamID models.TeamID, job *models.ExportJob) string {
	return fmt.Sprintf("/api/v1/teams/%d/sources/%d/exports/%s/download", teamID, job.SourceID, job.ID)
}

func (s *Server) startBackgroundCleanup() {
	go func() {
		s.cleanupExpiredBackgroundState()

		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			s.cleanupExpiredBackgroundState()
		}
	}()
}

func (s *Server) cleanupExpiredBackgroundState() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := time.Now().UTC()
	if err := s.sqlite.PruneExpiredQueryShares(ctx, now); err != nil {
		s.log.Warn("failed to prune expired query shares", "error", err)
	}

	// Unlink files first, then delete rows. If the process dies between
	// the two steps, the next cycle re-lists the same rows and ignores
	// already-removed files (ErrNotExist), so no artifact is orphaned.
	paths, err := s.sqlite.ListExpiredExportJobPaths(ctx, now)
	if err != nil {
		s.log.Warn("failed to list expired export jobs", "error", err)
		return
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("failed to remove expired export artifact", "error", err, "path", path)
		}
	}
	if err := s.sqlite.DeleteExpiredExportJobs(ctx, now); err != nil {
		s.log.Warn("failed to delete expired export job rows", "error", err)
	}
}
