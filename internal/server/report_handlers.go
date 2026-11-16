package server

import (
	"context"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/pkg/models"
)

const defaultReportRunsLimit = 20

// newReportRunner wires the report runner to the server's store,
// datasources and the instance SMTP settings.
func (s *Server) newReportRunner(opts ServerOptions) *core.ReportRunner {
	return &core.ReportRunner{
		DB:          s.store,
		DS:          opts.Datasources,
		Query:       s.config.Query,
		Mailer:      reports.NewSMTPMailer(s.store),
		Audit:       s.audit,
		Log:         s.log.With("component", "reports"),
		UIURL:       uiURL(&s.config.Server),
		QueryBudget: s.config.Reports.QueryBudget,
		Slots:       make(chan struct{}, s.config.Reports.MaxConcurrentRuns),
	}
}

func (s *Server) reportDeps() core.ReportDeps {
	return core.ReportDeps{DB: s.store, UIURL: s.reports.UIURL}
}

// requireReportsEnabled answers 503 when reports.enabled is false.
func (s *Server) requireReportsEnabled(c fiber.Ctx) error {
	if !s.config.Reports.Enabled {
		return SendErrorWithType(c, fiber.StatusServiceUnavailable, "Scheduled reports are disabled on this server", models.GeneralErrorType)
	}
	return c.Next()
}

func parseReportID(c fiber.Ctx) (models.ReportID, error) {
	id, err := parsePositiveIntParam(c, "reportID")
	return models.ReportID(id), err
}

// sendReportError maps a core report error to its status, as the error
// table of RFC 0008 lists them.
func (s *Server) sendReportError(c fiber.Ctx, err error, action string) error {
	switch {
	case errors.Is(err, core.ErrInvalidReport):
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	case errors.Is(err, core.ErrReportForbidden):
		return SendErrorWithType(c, fiber.StatusForbidden, err.Error(), models.AuthorizationErrorType)
	case errors.Is(err, core.ErrReportNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, "Report not found", models.NotFoundErrorType)
	case errors.Is(err, models.ErrReportRunInFlight):
		return SendErrorWithType(c, fiber.StatusConflict, "A run of this report is in flight", models.ConflictErrorType)
	case errors.Is(err, core.ErrReportOwnerUnavailable):
		return SendErrorWithType(c, fiber.StatusConflict, "The report has no active owner. Claim it first.", models.ConflictErrorType)
	case errors.Is(err, core.ErrReportCooldown):
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(int(models.ReportCooldown.Seconds())))
		return SendErrorWithType(c, fiber.StatusTooManyRequests, "A report allows one manual run and one preview per minute", models.ValidationErrorType)
	}
	s.log.Error("report request failed", "action", action, "error", err)
	return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to "+action, models.GeneralErrorType)
}

func (s *Server) sendReport(c fiber.Ctx, status int, r *models.Report) error {
	resp, err := core.BuildReportResponse(c.Context(), s.reportDeps(), principalFromLocals(c), r)
	if err != nil {
		return s.sendReportError(c, err, "load the report")
	}
	return SendSuccess(c, status, resp)
}

// handleListReports lists the caller's reports, or every report for a
// global admin with scope=all.
func (s *Server) handleListReports(c fiber.Ctx) error {
	scope := core.ReportScope(c.Query("scope", string(core.ReportScopeMine)))
	list, err := core.ListReports(c.Context(), s.reportDeps(), principalFromLocals(c), scope)
	if err != nil {
		return s.sendReportError(c, err, "list reports")
	}
	return SendSuccess(c, fiber.StatusOK, list)
}

func (s *Server) handleCreateReport(c fiber.Ctx) error {
	var in core.ReportInput
	if err := c.Bind().Body(&in); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	r, err := core.CreateReport(c.Context(), s.store, principalFromLocals(c), &in)
	if err != nil {
		return s.sendReportError(c, err, "create the report")
	}
	auditSet(c, audit.ReportTarget(r), nil, audit.ReportFields(r))
	return s.sendReport(c, fiber.StatusCreated, r)
}

func (s *Server) handleGetReport(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	r, err := core.GetReportForEditor(c.Context(), s.store, principalFromLocals(c), id)
	if err != nil {
		return s.sendReportError(c, err, "load the report")
	}
	return s.sendReport(c, fiber.StatusOK, r)
}

func (s *Server) handleUpdateReport(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var in core.ReportInput
	if err := c.Bind().Body(&in); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	before, after, err := core.UpdateReport(c.Context(), s.store, principalFromLocals(c), id, &in)
	if err != nil {
		return s.sendReportError(c, err, "update the report")
	}
	auditSet(c, audit.ReportTarget(after), audit.ReportFields(before), audit.ReportFields(after))
	return s.sendReport(c, fiber.StatusOK, after)
}

func (s *Server) handleDeleteReport(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	r, err := core.DeleteReport(c.Context(), s.store, principalFromLocals(c), id)
	if err != nil {
		return s.sendReportError(c, err, "delete the report")
	}
	auditTarget(c, audit.ReportTarget(r))
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handlePauseReport(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	r, err := core.PauseReport(c.Context(), s.store, principalFromLocals(c), id)
	if err != nil {
		return s.sendReportError(c, err, "pause the report")
	}
	auditTarget(c, audit.ReportTarget(r))
	auditDetail(c, "reason", models.ReportPauseUser)
	return s.sendReport(c, fiber.StatusOK, r)
}

func (s *Server) handleResumeReport(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	r, err := core.ResumeReport(c.Context(), s.store, principalFromLocals(c), id)
	if err != nil {
		return s.sendReportError(c, err, "resume the report")
	}
	auditTarget(c, audit.ReportTarget(r))
	return s.sendReport(c, fiber.StatusOK, r)
}

func (s *Server) handleClaimReport(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	before, after, err := core.ClaimReport(c.Context(), s.store, principalFromLocals(c), id)
	if err != nil {
		return s.sendReportError(c, err, "claim the report")
	}
	auditTarget(c, audit.ReportTarget(after))
	auditDetail(c, "previous_owner_id", before.OwnerID)
	return s.sendReport(c, fiber.StatusOK, after)
}

type reportRunRequest struct {
	Recipients core.ReportRunRecipients `json:"recipients"`
}

// handleRunReport claims a manual run and executes it in the background
// until the run's handler deadline. It answers 202 with the claimed run.
func (s *Server) handleRunReport(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	req := reportRunRequest{Recipients: core.ReportRunToMe}
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
		}
	}
	switch req.Recipients {
	case "":
		req.Recipients = core.ReportRunToMe
	case core.ReportRunToMe, core.ReportRunToAll:
	default:
		return SendErrorWithType(c, fiber.StatusBadRequest, "recipients must be me or all", models.ValidationErrorType)
	}
	run, err := s.reports.StartManualRun(c.Context(), principalFromLocals(c), id)
	if err != nil {
		return s.sendReportError(c, err, "start the run")
	}
	ctx, cancel := context.WithDeadline(s.baseCtx, *run.DeadlineAt)
	if !s.startBackgroundTask(func() {
		defer cancel()
		s.reports.Execute(ctx, run, req.Recipients)
	}) {
		cancel()
		return SendErrorWithType(c, fiber.StatusServiceUnavailable, "The server is shutting down", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusAccepted, run)
}

// handlePreviewReport returns the email the caller would receive, without
// sending it.
func (s *Server) handlePreviewReport(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	preview, err := s.reports.Preview(c.Context(), principalFromLocals(c), id)
	if err != nil {
		return s.sendReportError(c, err, "preview the report")
	}
	return SendSuccess(c, fiber.StatusOK, preview)
}

// handleListReportRuns returns the run history, newest first, paged with
// before (a run id) and limit.
func (s *Server) handleListReportRuns(c fiber.Ctx) error {
	id, err := parseReportID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	limit, before := defaultReportRunsLimit, 0
	for name, dst := range map[string]*int{"limit": &limit, "before": &before} {
		if raw := c.Query(name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				return SendErrorWithType(c, fiber.StatusBadRequest, name+" must be a non-negative integer", models.ValidationErrorType)
			}
			*dst = n
		}
	}
	runs, err := core.ListReportRuns(c.Context(), s.store, principalFromLocals(c), id, models.ReportRunID(before), limit)
	if err != nil {
		return s.sendReportError(c, err, "list the runs")
	}
	return SendSuccess(c, fiber.StatusOK, runs)
}

// interruptStaleReportRuns marks runs whose handler deadline passed more
// than reports.InterruptGrace ago on the database clock. A live run ends
// its final update at most reports.FinishTimeout after its deadline, so
// this never reaps a live run.
func (s *Server) interruptStaleReportRuns(ctx context.Context) {
	n, err := s.store.InterruptStaleReportRuns(ctx, reports.InterruptGrace)
	if err != nil {
		s.log.Warn("failed to interrupt stale report runs", "error", err)
		return
	}
	if n > 0 {
		s.log.Info("interrupted stale report runs", "count", n)
	}
}
