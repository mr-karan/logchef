package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Store) CreateReport(ctx context.Context, r *models.Report) error {
	p, err := reportColumns(r)
	if err != nil {
		return err
	}
	id, err := s.q.CreateReport(ctx, sqlc.CreateReportParams{
		Name:          p.Name,
		OwnerID:       userIDParam(r.OwnerID),
		SourceKind:    p.SourceKind,
		DashboardID:   p.DashboardID,
		TabID:         p.TabID,
		SavedQueryID:  p.SavedQueryID,
		ScheduleJson:  p.ScheduleJSON,
		Timezone:      p.Timezone,
		TimeRange:     p.TimeRange,
		RangeAnchor:   p.RangeAnchor,
		VariablesJson: p.VariablesJSON,
		MaxRows:       p.MaxRows,
		SkipIfEmpty:   p.SkipIfEmpty,
		AttachCsv:     p.AttachCsv,
	})
	if err != nil {
		return fmt.Errorf("error creating report: %w", err)
	}
	return s.reloadReport(ctx, models.ReportID(id), r)
}

func (s *Store) GetReport(ctx context.Context, id models.ReportID) (*models.Report, error) {
	row, err := s.q.GetReport(ctx, int64(id))
	if err != nil {
		if notFound(err) {
			return nil, fmt.Errorf("error getting report %d: %w", id, models.ErrNotFound)
		}
		return nil, fmt.Errorf("error getting report %d: %w", id, err)
	}
	return reportFromSQLC(&row)
}

func (s *Store) ListReports(ctx context.Context) ([]*models.Report, error) {
	rows, err := s.q.ListReports(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing reports: %w", err)
	}
	return reportsFromSQLC(rows)
}

func (s *Store) ListReportsByOwner(ctx context.Context, owner models.UserID) ([]*models.Report, error) {
	rows, err := s.q.ListReportsByOwner(ctx, int8Val(int64(owner)))
	if err != nil {
		return nil, fmt.Errorf("error listing reports of owner %d: %w", owner, err)
	}
	return reportsFromSQLC(rows)
}

func (s *Store) ListReportsForRecipient(ctx context.Context, userID models.UserID) ([]*models.Report, error) {
	rows, err := s.q.ListReportsForRecipient(ctx, int8Val(int64(userID)))
	if err != nil {
		return nil, fmt.Errorf("error listing reports received by user %d: %w", userID, err)
	}
	return reportsFromSQLC(rows)
}

func (s *Store) UpdateReport(ctx context.Context, r *models.Report) error {
	p, err := reportColumns(r)
	if err != nil {
		return err
	}
	n, err := s.q.UpdateReport(ctx, sqlc.UpdateReportParams{
		Name:          p.Name,
		SourceKind:    p.SourceKind,
		DashboardID:   p.DashboardID,
		TabID:         p.TabID,
		SavedQueryID:  p.SavedQueryID,
		ScheduleJson:  p.ScheduleJSON,
		Timezone:      p.Timezone,
		TimeRange:     p.TimeRange,
		RangeAnchor:   p.RangeAnchor,
		VariablesJson: p.VariablesJSON,
		MaxRows:       p.MaxRows,
		SkipIfEmpty:   p.SkipIfEmpty,
		AttachCsv:     p.AttachCsv,
		ID:            int64(r.ID),
	})
	if err := rowsChanged(n, err, "updating report", r.ID); err != nil {
		return err
	}
	return s.reloadReport(ctx, r.ID, r)
}

func (s *Store) DeleteReport(ctx context.Context, id models.ReportID) error {
	n, err := s.q.DeleteReport(ctx, int64(id))
	return rowsChanged(n, err, "deleting report", id)
}

func (s *Store) PauseReport(ctx context.Context, id models.ReportID, reason models.ReportPauseReason) error {
	if reason == models.ReportPauseNone {
		return fmt.Errorf("pausing report %d: a pause needs a reason", id)
	}
	n, err := s.q.PauseReport(ctx, sqlc.PauseReportParams{PauseReason: string(reason), ID: int64(id)})
	return rowsChanged(n, err, "pausing report", id)
}

func (s *Store) ResumeReport(ctx context.Context, id models.ReportID) error {
	n, err := s.q.ResumeReport(ctx, int64(id))
	return rowsChanged(n, err, "resuming report", id)
}

func (s *Store) ClaimReport(ctx context.Context, id models.ReportID, owner models.UserID) error {
	n, err := s.q.ClaimReport(ctx, sqlc.ClaimReportParams{OwnerID: int8Val(int64(owner)), ID: int64(id)})
	return rowsChanged(n, err, "claiming report", id)
}

func (s *Store) TakeReportCooldown(ctx context.Context, id models.ReportID, kind models.ReportCooldownKind) (bool, error) {
	window := models.ReportCooldown.Seconds()
	var n int64
	var err error
	switch kind {
	case models.ReportCooldownManualRun:
		n, err = s.q.TakeReportManualRunCooldown(ctx, sqlc.TakeReportManualRunCooldownParams{ID: int64(id), WindowSeconds: window})
	case models.ReportCooldownPreview:
		n, err = s.q.TakeReportPreviewCooldown(ctx, sqlc.TakeReportPreviewCooldownParams{ID: int64(id), WindowSeconds: window})
	default:
		return false, fmt.Errorf("unknown report cooldown %q", kind)
	}
	if err != nil {
		return false, fmt.Errorf("error taking %s cooldown of report %d: %w", kind, id, err)
	}
	return n == 1, nil
}

func (s *Store) ListReportRecipients(ctx context.Context, id models.ReportID) ([]models.ReportRecipient, error) {
	rows, err := s.q.ListReportRecipients(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("error listing recipients of report %d: %w", id, err)
	}
	out := make([]models.ReportRecipient, len(rows))
	for i := range rows {
		row := &rows[i]
		out[i] = models.ReportRecipient{
			ID:        models.ReportRecipientID(row.ID),
			ReportID:  models.ReportID(row.ReportID),
			Kind:      models.ReportRecipientKind(row.Kind),
			UserID:    userIDPtr(row.UserID),
			TeamID:    teamIDPtr(row.TeamID),
			Email:     row.Email,
			AddedBy:   userIDPtr(row.AddedBy),
			CreatedAt: row.CreatedAt.Time,
		}
	}
	return out, nil
}

func (s *Store) ReplaceReportRecipients(ctx context.Context, id models.ReportID, recipients []models.ReportRecipient) error {
	return s.inTx(ctx, func(q sqlc.Querier) error {
		if _, err := q.LockReport(ctx, int64(id)); notFound(err) {
			return fmt.Errorf("error replacing recipients of report %d: %w", id, models.ErrNotFound)
		} else if err != nil {
			return fmt.Errorf("error locking report %d: %w", id, err)
		}
		if err := q.DeleteReportRecipients(ctx, int64(id)); err != nil {
			return fmt.Errorf("error clearing recipients of report %d: %w", id, err)
		}
		for _, rec := range recipients {
			var team pgtype.Int8
			if rec.TeamID != nil {
				team = int8Val(int64(*rec.TeamID))
			}
			err := q.InsertReportRecipient(ctx, sqlc.InsertReportRecipientParams{
				ReportID: int64(id),
				Kind:     string(rec.Kind),
				UserID:   userIDParam(rec.UserID),
				TeamID:   team,
				Email:    rec.Email,
				AddedBy:  userIDParam(rec.AddedBy),
			})
			if isUniqueViolation(err) {
				return fmt.Errorf("report %d lists recipient %s twice: %w", id, rec.Kind, models.ErrConflict)
			}
			if err != nil {
				return fmt.Errorf("error adding a recipient to report %d: %w", id, err)
			}
		}
		return nil
	})
}

func (s *Store) RemoveReportUserRecipient(ctx context.Context, id models.ReportID, userID models.UserID) error {
	n, err := s.q.DeleteReportUserRecipient(ctx, sqlc.DeleteReportUserRecipientParams{ReportID: int64(id), UserID: int8Val(int64(userID))})
	return rowsChanged(n, err, "removing a recipient of report", id)
}

func (s *Store) CreateReportRun(ctx context.Context, claim models.ReportRunClaim) (*models.ReportRun, error) {
	scheduledFor := ts(claim.ScheduledFor.UTC().Truncate(time.Millisecond))
	row, err := s.q.InsertRunningReportRun(ctx, sqlc.InsertRunningReportRunParams{
		ReportID:        int64(claim.ReportID),
		Trigger:         string(claim.Trigger),
		ScheduledFor:    scheduledFor,
		ActorID:         userIDParam(claim.ActorID),
		DeadlineSeconds: claim.HandlerBudget.Seconds(),
	})
	if notFound(err) {
		return nil, s.runConflict(ctx, claim.ReportID, claim.Trigger, scheduledFor)
	}
	if err != nil {
		return nil, fmt.Errorf("error claiming a run of report %d: %w", claim.ReportID, err)
	}
	return reportRunFromSQLC(&row)
}

func (s *Store) SkipReportTick(ctx context.Context, id models.ReportID, scheduledFor time.Time, reason models.ReportRunReason) (*models.ReportRun, error) {
	row, err := s.q.InsertSkippedReportTick(ctx, sqlc.InsertSkippedReportTickParams{
		ReportID:     int64(id),
		ScheduledFor: ts(scheduledFor.UTC().Truncate(time.Millisecond)),
		Reason:       string(reason),
	})
	if notFound(err) {
		return nil, fmt.Errorf("report %d tick %s: %w", id, scheduledFor.UTC().Format(time.RFC3339), models.ErrReportTickTaken)
	}
	if err != nil {
		return nil, fmt.Errorf("error skipping a tick of report %d: %w", id, err)
	}
	return reportRunFromSQLC(&row)
}

// runConflict tells which partial unique index refused a run: the tick
// index when a scheduled row for the tick exists, otherwise the running one.
func (s *Store) runConflict(ctx context.Context, id models.ReportID, trigger models.ReportRunTrigger, scheduledFor pgtype.Timestamptz) error {
	if trigger == models.ReportTriggerSchedule {
		taken, err := s.q.ReportTickExists(ctx, sqlc.ReportTickExistsParams{ReportID: int64(id), ScheduledFor: scheduledFor})
		if err != nil {
			return fmt.Errorf("error reading a tick of report %d: %w", id, err)
		}
		if taken {
			return fmt.Errorf("report %d tick %s: %w", id, scheduledFor.Time.Format(time.RFC3339), models.ErrReportTickTaken)
		}
	}
	return fmt.Errorf("report %d: %w", id, models.ErrReportRunInFlight)
}

func (s *Store) FinishReportRun(ctx context.Context, runID models.ReportRunID, res models.ReportRunResult) (int, error) {
	if err := res.Validate(); err != nil {
		return 0, err
	}
	panels, deliveries, err := res.ColumnsJSON()
	if err != nil {
		return 0, err
	}
	var failures int64
	err = s.inTx(ctx, func(q sqlc.Querier) error {
		reportID, err := q.FinishReportRun(ctx, sqlc.FinishReportRunParams{
			Status:         string(res.Status),
			Reason:         string(res.Reason),
			WindowStart:    tsFromPtr(res.WindowStart),
			WindowEnd:      tsFromPtr(res.WindowEnd),
			PanelsJson:     panels,
			DeliveriesJson: deliveries,
			ID:             int64(runID),
		})
		if notFound(err) {
			return fmt.Errorf("error finishing report run %d: %w", runID, models.ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("error finishing report run %d: %w", runID, err)
		}
		if failures, err = q.CountReportFailure(ctx, sqlc.CountReportFailureParams{Status: string(res.Status), ID: reportID}); err != nil {
			return fmt.Errorf("error counting failures of report %d: %w", reportID, err)
		}
		if err := q.PruneReportRuns(ctx, sqlc.PruneReportRunsParams{ReportID: reportID, Keep: models.ReportRunHistoryLimit}); err != nil {
			return fmt.Errorf("error pruning runs of report %d: %w", reportID, err)
		}
		return nil
	})
	return int(failures), err
}

func (s *Store) ListReportRuns(ctx context.Context, id models.ReportID, before models.ReportRunID, limit int) ([]*models.ReportRun, error) {
	rows, err := s.q.ListReportRuns(ctx, sqlc.ListReportRunsParams{ReportID: int64(id), Before: int64(before), MaxRuns: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("error listing runs of report %d: %w", id, err)
	}
	out := make([]*models.ReportRun, len(rows))
	for i := range rows {
		if out[i], err = reportRunFromSQLC(&rows[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) InterruptStaleReportRuns(ctx context.Context, grace time.Duration) (int64, error) {
	n, err := s.q.InterruptStaleReportRuns(ctx, grace.Seconds())
	if err != nil {
		return 0, fmt.Errorf("error interrupting stale report runs: %w", err)
	}
	return n, nil
}

func (s *Store) reloadReport(ctx context.Context, id models.ReportID, r *models.Report) error {
	stored, err := s.GetReport(ctx, id)
	if err != nil {
		return err
	}
	*r = *stored
	return nil
}

func rowsChanged(n int64, err error, action string, id models.ReportID) error {
	if err != nil {
		return fmt.Errorf("error %s %d: %w", action, id, err)
	}
	if n == 0 {
		return fmt.Errorf("error %s %d: %w", action, id, models.ErrNotFound)
	}
	return nil
}

// reportParams holds the editable columns in their Postgres form.
type reportParams struct {
	Name          string
	SourceKind    string
	DashboardID   pgtype.Int8
	TabID         pgtype.Text
	SavedQueryID  pgtype.Int8
	ScheduleJSON  string
	Timezone      string
	TimeRange     pgtype.Text
	RangeAnchor   string
	VariablesJSON string
	MaxRows       int64
	SkipIfEmpty   bool
	AttachCsv     bool
}

func reportColumns(r *models.Report) (reportParams, error) {
	schedule, err := json.Marshal(r.Schedule)
	if err != nil {
		return reportParams{}, fmt.Errorf("encoding report schedule: %w", err)
	}
	variables := []byte("{}")
	if len(r.Variables) > 0 {
		if variables, err = json.Marshal(r.Variables); err != nil {
			return reportParams{}, fmt.Errorf("encoding report variables: %w", err)
		}
	}
	return reportParams{
		Name:          r.Name,
		SourceKind:    string(r.SourceKind),
		DashboardID:   intParam(r.DashboardID),
		TabID:         text(r.TabID),
		SavedQueryID:  intParam(r.SavedQueryID),
		ScheduleJSON:  string(schedule),
		Timezone:      r.Timezone,
		TimeRange:     text(r.TimeRange),
		RangeAnchor:   string(r.RangeAnchor),
		VariablesJSON: string(variables),
		MaxRows:       int64(r.MaxRows),
		SkipIfEmpty:   r.SkipIfEmpty,
		AttachCsv:     r.AttachCSV,
	}, nil
}

func reportsFromSQLC(rows []sqlc.Report) ([]*models.Report, error) {
	out := make([]*models.Report, len(rows))
	for i := range rows {
		r, err := reportFromSQLC(&rows[i])
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

func reportFromSQLC(row *sqlc.Report) (*models.Report, error) {
	r := &models.Report{
		ID:                  models.ReportID(row.ID),
		Name:                row.Name,
		OwnerID:             userIDPtr(row.OwnerID),
		SourceKind:          models.ReportSourceKind(row.SourceKind),
		DashboardID:         intPtr(row.DashboardID),
		TabID:               textStr(row.TabID),
		SavedQueryID:        intPtr(row.SavedQueryID),
		Timezone:            row.Timezone,
		TimeRange:           textStr(row.TimeRange),
		RangeAnchor:         models.ReportRangeAnchor(row.RangeAnchor),
		MaxRows:             int(row.MaxRows),
		SkipIfEmpty:         row.SkipIfEmpty,
		AttachCSV:           row.AttachCsv,
		State:               models.ReportState(row.State),
		PauseReason:         models.ReportPauseReason(row.PauseReason),
		ConsecutiveFailures: int(row.ConsecutiveFailures),
		LastManualRunAt:     tsPtr(row.LastManualRunAt),
		LastPreviewAt:       tsPtr(row.LastPreviewAt),
		CreatedAt:           row.CreatedAt.Time,
		UpdatedAt:           row.UpdatedAt.Time,
	}
	if err := json.Unmarshal([]byte(row.ScheduleJson), &r.Schedule); err != nil {
		return nil, fmt.Errorf("report %d has an unreadable schedule: %w", row.ID, err)
	}
	if err := json.Unmarshal([]byte(row.VariablesJson), &r.Variables); err != nil {
		return nil, fmt.Errorf("report %d has unreadable variables: %w", row.ID, err)
	}
	if r.Variables == nil {
		r.Variables = map[string]any{}
	}
	return r, nil
}

func reportRunFromSQLC(row *sqlc.ReportRun) (*models.ReportRun, error) {
	run := &models.ReportRun{
		ID:           models.ReportRunID(row.ID),
		ReportID:     models.ReportID(row.ReportID),
		Trigger:      models.ReportRunTrigger(row.Trigger),
		ScheduledFor: row.ScheduledFor.Time,
		ActorID:      userIDPtr(row.ActorID),
		Status:       models.ReportRunStatus(row.Status),
		Reason:       models.ReportRunReason(row.Reason),
		WindowStart:  tsPtr(row.WindowStart),
		WindowEnd:    tsPtr(row.WindowEnd),
		StartedAt:    row.StartedAt.Time,
		DeadlineAt:   tsPtr(row.DeadlineAt),
		FinishedAt:   tsPtr(row.FinishedAt),
	}
	if err := json.Unmarshal([]byte(row.PanelsJson), &run.Panels); err != nil {
		return nil, fmt.Errorf("report run %d has unreadable panels: %w", row.ID, err)
	}
	if err := json.Unmarshal([]byte(row.DeliveriesJson), &run.Deliveries); err != nil {
		return nil, fmt.Errorf("report run %d has unreadable deliveries: %w", row.ID, err)
	}
	return run, nil
}

func userIDParam(id *models.UserID) pgtype.Int8 {
	if id == nil {
		return pgtype.Int8{}
	}
	return int8Val(int64(*id))
}

func intParam(v *int) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return int8Val(int64(*v))
}

func intPtr(v pgtype.Int8) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}
