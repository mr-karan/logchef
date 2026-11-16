package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (db *DB) CreateReport(ctx context.Context, r *models.Report) error {
	p, err := reportColumns(r)
	if err != nil {
		return err
	}
	id, err := db.writeQueries.CreateReport(ctx, sqlc.CreateReportParams{
		Name:          p.Name,
		OwnerID:       nullUserID(r.OwnerID),
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
		return fmt.Errorf("error creating report: %w", translateBusy(err))
	}
	return db.reloadReport(ctx, models.ReportID(id), r)
}

func (db *DB) GetReport(ctx context.Context, id models.ReportID) (*models.Report, error) {
	row, err := db.readQueries.GetReport(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("error getting report %d: %w", id, translateNotFound(err))
	}
	return reportFromSQLC(&row)
}

func (db *DB) ListReports(ctx context.Context) ([]*models.Report, error) {
	rows, err := db.readQueries.ListReports(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing reports: %w", err)
	}
	return reportsFromSQLC(rows)
}

func (db *DB) ListReportsByOwner(ctx context.Context, owner models.UserID) ([]*models.Report, error) {
	rows, err := db.readQueries.ListReportsByOwner(ctx, sql.NullInt64{Int64: int64(owner), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("error listing reports of owner %d: %w", owner, err)
	}
	return reportsFromSQLC(rows)
}

func (db *DB) ListReportsForRecipient(ctx context.Context, userID models.UserID) ([]*models.Report, error) {
	rows, err := db.readQueries.ListReportsForRecipient(ctx, sql.NullInt64{Int64: int64(userID), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("error listing reports received by user %d: %w", userID, err)
	}
	return reportsFromSQLC(rows)
}

func (db *DB) UpdateReport(ctx context.Context, r *models.Report) error {
	p, err := reportColumns(r)
	if err != nil {
		return err
	}
	n, err := db.writeQueries.UpdateReport(ctx, sqlc.UpdateReportParams{
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
	return db.reloadReport(ctx, r.ID, r)
}

func (db *DB) DeleteReport(ctx context.Context, id models.ReportID) error {
	n, err := db.writeQueries.DeleteReport(ctx, int64(id))
	return rowsChanged(n, err, "deleting report", id)
}

func (db *DB) PauseReport(ctx context.Context, id models.ReportID, reason models.ReportPauseReason) error {
	if reason == models.ReportPauseNone {
		return fmt.Errorf("pausing report %d: a pause needs a reason", id)
	}
	n, err := db.writeQueries.PauseReport(ctx, sqlc.PauseReportParams{PauseReason: string(reason), ID: int64(id)})
	return rowsChanged(n, err, "pausing report", id)
}

func (db *DB) ResumeReport(ctx context.Context, id models.ReportID) error {
	n, err := db.writeQueries.ResumeReport(ctx, int64(id))
	return rowsChanged(n, err, "resuming report", id)
}

func (db *DB) ClaimReport(ctx context.Context, id models.ReportID, owner models.UserID) error {
	n, err := db.writeQueries.ClaimReport(ctx, sqlc.ClaimReportParams{
		OwnerID: sql.NullInt64{Int64: int64(owner), Valid: true},
		ID:      int64(id),
	})
	return rowsChanged(n, err, "claiming report", id)
}

func (db *DB) TakeReportCooldown(ctx context.Context, id models.ReportID, kind models.ReportCooldownKind) (bool, error) {
	window := secondsModifier(-models.ReportCooldown)
	var n int64
	var err error
	switch kind {
	case models.ReportCooldownManualRun:
		n, err = db.writeQueries.TakeReportManualRunCooldown(ctx, sqlc.TakeReportManualRunCooldownParams{ID: int64(id), WindowModifier: window})
	case models.ReportCooldownPreview:
		n, err = db.writeQueries.TakeReportPreviewCooldown(ctx, sqlc.TakeReportPreviewCooldownParams{ID: int64(id), WindowModifier: window})
	default:
		return false, fmt.Errorf("unknown report cooldown %q", kind)
	}
	if err != nil {
		return false, fmt.Errorf("error taking %s cooldown of report %d: %w", kind, id, translateBusy(err))
	}
	return n == 1, nil
}

func (db *DB) ListReportRecipients(ctx context.Context, id models.ReportID) ([]models.ReportRecipient, error) {
	rows, err := db.readQueries.ListReportRecipients(ctx, int64(id))
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
			Email:     row.Email,
			AddedBy:   userIDPtr(row.AddedBy),
			CreatedAt: row.CreatedAt,
		}
		if row.TeamID.Valid {
			team := models.TeamID(row.TeamID.Int64)
			out[i].TeamID = &team
		}
	}
	return out, nil
}

func (db *DB) ReplaceReportRecipients(ctx context.Context, id models.ReportID, recipients []models.ReportRecipient) error {
	return db.inWriteTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.LockReport(ctx, int64(id)); err != nil {
			return fmt.Errorf("error replacing recipients of report %d: %w", id, translateBusy(translateNotFound(err)))
		}
		if err := q.DeleteReportRecipients(ctx, int64(id)); err != nil {
			return fmt.Errorf("error clearing recipients of report %d: %w", id, translateBusy(err))
		}
		for _, rec := range recipients {
			err := q.InsertReportRecipient(ctx, sqlc.InsertReportRecipientParams{
				ReportID: int64(id),
				Kind:     string(rec.Kind),
				UserID:   nullUserID(rec.UserID),
				TeamID:   nullTeamID(rec.TeamID),
				Email:    rec.Email,
				AddedBy:  nullUserID(rec.AddedBy),
			})
			if isUniqueConstraintSQLiteError(err, "", "") {
				return fmt.Errorf("report %d lists recipient %s twice: %w", id, rec.Kind, models.ErrConflict)
			}
			if err != nil {
				return fmt.Errorf("error adding a recipient to report %d: %w", id, translateBusy(err))
			}
		}
		return nil
	})
}

func (db *DB) RemoveReportUserRecipient(ctx context.Context, id models.ReportID, userID models.UserID) error {
	n, err := db.writeQueries.DeleteReportUserRecipient(ctx, sqlc.DeleteReportUserRecipientParams{
		ReportID: int64(id),
		UserID:   sql.NullInt64{Int64: int64(userID), Valid: true},
	})
	return rowsChanged(n, err, "removing a recipient of report", id)
}

func (db *DB) CreateReportRun(ctx context.Context, claim models.ReportRunClaim) (*models.ReportRun, error) {
	scheduledFor := claim.ScheduledFor.UTC().Format(jobTimeLayout)
	row, err := db.writeQueries.InsertRunningReportRun(ctx, sqlc.InsertRunningReportRunParams{
		ReportID:         int64(claim.ReportID),
		Trigger:          string(claim.Trigger),
		ScheduledFor:     scheduledFor,
		ActorID:          nullUserID(claim.ActorID),
		DeadlineModifier: secondsModifier(claim.HandlerBudget),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, db.runConflict(ctx, claim.ReportID, claim.Trigger, scheduledFor)
	}
	if err != nil {
		return nil, fmt.Errorf("error claiming a run of report %d: %w", claim.ReportID, translateBusy(err))
	}
	return reportRunFromSQLC(&row)
}

func (db *DB) SkipReportTick(ctx context.Context, id models.ReportID, scheduledFor time.Time, reason models.ReportRunReason) (*models.ReportRun, error) {
	row, err := db.writeQueries.InsertSkippedReportTick(ctx, sqlc.InsertSkippedReportTickParams{
		ReportID:     int64(id),
		ScheduledFor: scheduledFor.UTC().Format(jobTimeLayout),
		Reason:       string(reason),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("report %d tick %s: %w", id, scheduledFor.UTC().Format(time.RFC3339), models.ErrReportTickTaken)
	}
	if err != nil {
		return nil, fmt.Errorf("error skipping a tick of report %d: %w", id, translateBusy(err))
	}
	return reportRunFromSQLC(&row)
}

// runConflict tells which partial unique index refused a run: the tick
// index when a scheduled row for the tick exists, otherwise the running one.
func (db *DB) runConflict(ctx context.Context, id models.ReportID, trigger models.ReportRunTrigger, scheduledFor string) error {
	if trigger == models.ReportTriggerSchedule {
		taken, err := db.writeQueries.ReportTickExists(ctx, sqlc.ReportTickExistsParams{ReportID: int64(id), ScheduledFor: scheduledFor})
		if err != nil {
			return fmt.Errorf("error reading a tick of report %d: %w", id, translateBusy(err))
		}
		if taken {
			return fmt.Errorf("report %d tick %s: %w", id, scheduledFor, models.ErrReportTickTaken)
		}
	}
	return fmt.Errorf("report %d: %w", id, models.ErrReportRunInFlight)
}

func (db *DB) FinishReportRun(ctx context.Context, runID models.ReportRunID, res models.ReportRunResult) (int, error) {
	if err := res.Validate(); err != nil {
		return 0, err
	}
	panels, deliveries, err := res.ColumnsJSON()
	if err != nil {
		return 0, err
	}
	var failures int64
	err = db.inWriteTx(ctx, func(q *sqlc.Queries) error {
		reportID, err := q.FinishReportRun(ctx, sqlc.FinishReportRunParams{
			Status:         string(res.Status),
			Reason:         string(res.Reason),
			WindowStart:    nullTimeText(res.WindowStart),
			WindowEnd:      nullTimeText(res.WindowEnd),
			PanelsJson:     panels,
			DeliveriesJson: deliveries,
			ID:             int64(runID),
		})
		if err != nil {
			return fmt.Errorf("error finishing report run %d: %w", runID, translateBusy(translateNotFound(err)))
		}
		failures, err = q.CountReportFailure(ctx, sqlc.CountReportFailureParams{Status: string(res.Status), ID: reportID})
		if err != nil {
			return fmt.Errorf("error counting failures of report %d: %w", reportID, translateBusy(err))
		}
		if err := q.PruneReportRuns(ctx, sqlc.PruneReportRunsParams{ReportID: reportID, Keep: models.ReportRunHistoryLimit}); err != nil {
			return fmt.Errorf("error pruning runs of report %d: %w", reportID, translateBusy(err))
		}
		return nil
	})
	return int(failures), err
}

func (db *DB) ListReportRuns(ctx context.Context, id models.ReportID, before models.ReportRunID, limit int) ([]*models.ReportRun, error) {
	rows, err := db.readQueries.ListReportRuns(ctx, sqlc.ListReportRunsParams{ReportID: int64(id), Before: int64(before), MaxRuns: int64(limit)})
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

func (db *DB) InterruptStaleReportRuns(ctx context.Context, grace time.Duration) (int64, error) {
	n, err := db.writeQueries.InterruptStaleReportRuns(ctx, secondsModifier(-grace))
	if err != nil {
		return 0, fmt.Errorf("error interrupting stale report runs: %w", translateBusy(err))
	}
	return n, nil
}

func (db *DB) reloadReport(ctx context.Context, id models.ReportID, r *models.Report) error {
	row, err := db.writeQueries.GetReport(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("error reading report %d: %w", id, translateNotFound(err))
	}
	stored, err := reportFromSQLC(&row)
	if err != nil {
		return err
	}
	*r = *stored
	return nil
}

func rowsChanged(n int64, err error, action string, id models.ReportID) error {
	if err != nil {
		return fmt.Errorf("error %s %d: %w", action, id, translateBusy(err))
	}
	if n == 0 {
		return fmt.Errorf("error %s %d: %w", action, id, models.ErrNotFound)
	}
	return nil
}

// reportParams holds the editable columns in their SQLite form.
type reportParams struct {
	Name          string
	SourceKind    string
	DashboardID   sql.NullInt64
	TabID         sql.NullString
	SavedQueryID  sql.NullInt64
	ScheduleJSON  string
	Timezone      string
	TimeRange     sql.NullString
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
		DashboardID:   nullIntPtr(r.DashboardID),
		TabID:         nullString(r.TabID),
		SavedQueryID:  nullIntPtr(r.SavedQueryID),
		ScheduleJSON:  string(schedule),
		Timezone:      r.Timezone,
		TimeRange:     nullString(r.TimeRange),
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
		TabID:               row.TabID.String,
		Timezone:            row.Timezone,
		TimeRange:           row.TimeRange.String,
		RangeAnchor:         models.ReportRangeAnchor(row.RangeAnchor),
		MaxRows:             int(row.MaxRows),
		SkipIfEmpty:         row.SkipIfEmpty,
		AttachCSV:           row.AttachCsv,
		State:               models.ReportState(row.State),
		PauseReason:         models.ReportPauseReason(row.PauseReason),
		ConsecutiveFailures: int(row.ConsecutiveFailures),
		LastManualRunAt:     jobTimePtr(row.LastManualRunAt),
		LastPreviewAt:       jobTimePtr(row.LastPreviewAt),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
	if row.DashboardID.Valid {
		id := int(row.DashboardID.Int64)
		r.DashboardID = &id
	}
	if row.SavedQueryID.Valid {
		id := int(row.SavedQueryID.Int64)
		r.SavedQueryID = &id
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
		ScheduledFor: row.ScheduledFor,
		ActorID:      userIDPtr(row.ActorID),
		Status:       models.ReportRunStatus(row.Status),
		Reason:       models.ReportRunReason(row.Reason),
		WindowStart:  jobTimePtr(row.WindowStart),
		WindowEnd:    jobTimePtr(row.WindowEnd),
		StartedAt:    row.StartedAt,
		DeadlineAt:   jobTimePtr(row.DeadlineAt),
		FinishedAt:   jobTimePtr(row.FinishedAt),
	}
	if err := json.Unmarshal([]byte(row.PanelsJson), &run.Panels); err != nil {
		return nil, fmt.Errorf("report run %d has unreadable panels: %w", row.ID, err)
	}
	if err := json.Unmarshal([]byte(row.DeliveriesJson), &run.Deliveries); err != nil {
		return nil, fmt.Errorf("report run %d has unreadable deliveries: %w", row.ID, err)
	}
	return run, nil
}

// secondsModifier is a strftime modifier that adds d, with an explicit sign
// so a negative d stays a valid modifier.
func secondsModifier(d time.Duration) string {
	return fmt.Sprintf("%+.3f seconds", d.Seconds())
}

func nullTimeText(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(jobTimeLayout), Valid: true}
}

func nullIntPtr(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func nullUserID(id *models.UserID) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*id), Valid: true}
}

func nullTeamID(id *models.TeamID) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*id), Valid: true}
}

func userIDPtr(v sql.NullInt64) *models.UserID {
	if !v.Valid {
		return nil
	}
	id := models.UserID(v.Int64)
	return &id
}
