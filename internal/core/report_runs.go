package core

// Running a report (RFC 0008, "Run flow"): the manual run, its execution as
// the owner, its delivery and its final update, and the preview.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// maxReportQueryRows bounds the rows one panel query reads.
	maxReportQueryRows = 10_000
	// maxReportResultBytes bounds one panel result, below
	// query.max_response_bytes when that is smaller.
	maxReportResultBytes = 4 << 20
	// reportFailurePause is how many failed runs in a row pause a report.
	reportFailurePause = 3
)

// ReportRunRecipients selects who a manual run sends to: the caller, or
// every recipient of the report, which is its owner until recipient rows
// exist.
type ReportRunRecipients string

const (
	ReportRunToMe  ReportRunRecipients = "me"
	ReportRunToAll ReportRunRecipients = "all"
)

// ReportRunner runs reports as their owners.
type ReportRunner struct {
	DB          store.Store
	DS          *datasource.Service
	Query       config.QueryConfig
	Mailer      reports.Mailer
	Audit       *audit.Recorder
	Log         *slog.Logger
	UIURL       string
	QueryBudget time.Duration
	// Slots bounds the runs of this replica that execute at once; a run
	// waits for a slot inside its own deadline. Nil does not bound them.
	Slots chan struct{}
	// Sleep waits between delivery attempts; nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error
}

// StartManualRun takes the manual-run cooldown and claims a run of a report
// the caller may edit. The caller then executes the run in the background.
func (rr *ReportRunner) StartManualRun(ctx context.Context, p access.Principal, id models.ReportID) (*models.ReportRun, error) {
	if _, err := GetReportForEditor(ctx, rr.DB, p, id); err != nil {
		return nil, err
	}
	taken, err := rr.DB.TakeReportCooldown(ctx, id, models.ReportCooldownManualRun)
	if err != nil {
		return nil, fmt.Errorf("taking the manual run cooldown of report %d: %w", id, err)
	}
	if !taken {
		return nil, ErrReportCooldown
	}
	run, err := rr.DB.CreateReportRun(ctx, models.ReportRunClaim{
		ReportID:      id,
		Trigger:       models.ReportTriggerManual,
		ScheduledFor:  time.Now().UTC(),
		ActorID:       &p.User.ID,
		HandlerBudget: reports.HandlerBudget(rr.QueryBudget),
	})
	if err != nil {
		return nil, fmt.Errorf("claiming a run of report %d: %w", id, err)
	}
	return run, nil
}

// Execute runs a claimed run and writes its result. ctx ends at the run's
// handler deadline; the final update runs on a context that deadline does
// not cancel.
func (rr *ReportRunner) Execute(ctx context.Context, run *models.ReportRun, to ReportRunRecipients) {
	var (
		r   *models.Report
		res models.ReportRunResult
	)
	release, err := rr.acquire(ctx)
	if err == nil {
		defer release()
		r, err = rr.DB.GetReport(ctx, run.ReportID)
	}
	if err == nil {
		res, err = rr.execute(ctx, run, r, to)
	}
	if err != nil {
		rr.Log.Error("report run failed", "report_id", run.ReportID, "run_id", run.ID, "error", err)
		res = models.ReportRunResult{Status: models.ReportRunFailed, Reason: models.ReportReasonAllPanelsFailed}
		if ctx.Err() != nil {
			res.Reason = models.ReportReasonTimeout
		}
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reports.FinishTimeout)
	defer cancel()
	failures, err := rr.DB.FinishReportRun(finishCtx, run.ID, res)
	if err != nil {
		rr.Log.Error("report run result not stored", "report_id", run.ReportID, "run_id", run.ID, "error", err)
		return
	}
	if r == nil {
		return
	}
	switch {
	case res.Reason == models.ReportReasonOwnerInactive && r.PauseReason != models.ReportPauseOwnerInactive:
		rr.pause(finishCtx, run, r, models.ReportPauseOwnerInactive)
	case res.Status == models.ReportRunFailed && failures >= reportFailurePause && r.State == models.ReportStateActive:
		rr.pause(finishCtx, run, r, models.ReportPauseFailures)
		rr.noticeOwnerOfPause(finishCtx, r, res.Reason)
	}
}

// acquire waits for a run slot until ctx ends.
func (rr *ReportRunner) acquire(ctx context.Context) (release func(), err error) {
	if rr.Slots == nil {
		return func() {}, nil
	}
	select {
	case rr.Slots <- struct{}{}:
		return func() { <-rr.Slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for a report run slot: %w", ctx.Err())
	}
}

// pause is a pause by the system, outside HTTP, so it records its audit
// event itself.
func (rr *ReportRunner) pause(ctx context.Context, run *models.ReportRun, r *models.Report, reason models.ReportPauseReason) {
	if err := rr.DB.PauseReport(ctx, r.ID, reason); err != nil {
		rr.Log.Error("report not paused", "report_id", r.ID, "reason", reason, "error", err)
		return
	}
	rr.Audit.Record(ctx, audit.Event{
		Action:  audit.ReportPause,
		Outcome: models.AuditOutcomeSuccess,
		Actor: audit.Actor{
			Type:         models.AuditActorSystem,
			AuthMethod:   models.AuditAuthScheduler,
			CredentialID: "report_run:" + strconv.FormatInt(int64(run.ID), 10),
		},
		Target: audit.ReportTarget(r),
		Detail: audit.NewFields().Set("reason", reason),
	})
}

// noticeOwnerOfPause sends the owner one plain email that names the report
// and the last reason. It is best effort: the pause stands either way.
func (rr *ReportRunner) noticeOwnerOfPause(ctx context.Context, r *models.Report, reason models.ReportRunReason) {
	if r.OwnerID == nil {
		return
	}
	owner, err := rr.DB.GetUser(ctx, *r.OwnerID)
	if err != nil || owner.Status != models.UserStatusActive || owner.Email == "" {
		return
	}
	text := fmt.Sprintf("The report %q failed %d times in a row and is paused. The last run failed with %s.\nResume it in Logchef once the cause is fixed.\n", r.Name, reportFailurePause, reason)
	msg := reports.MailMessage{
		Subject: "[Logchef] Report paused: " + r.Name,
		Date:    time.Now(),
		Text:    text,
		HTML:    "<p>" + html.EscapeString(text) + "</p>",
	}
	if err := rr.Mailer.Send(ctx, mail.Address{Name: owner.FullName, Address: owner.Email}, msg); err != nil {
		rr.Log.Warn("report pause notice not sent", "report_id", r.ID, "error", err)
	}
}

// reportPlan is a report ready to run: its owner, its window, and its
// panels, each authorized for the owner at this run.
type reportPlan struct {
	report  *models.Report
	owner   *models.User
	loc     *time.Location
	window  reports.Window
	sources []string
	// sourceURL opens the dashboard or saved query, "" without a UI URL.
	sourceURL string
	panels    []plannedPanel
}

// plannedPanel is a panel that runs (job set), or one that the owner can
// no longer reach or that email does not show (fixed set).
type plannedPanel struct {
	job    *reports.PanelJob
	fixed  reports.Panel
	status models.ReportPanelStatus
	// teamID is the panel's team on a dashboard and 0 for a saved query,
	// which any team with sourceID reaches.
	teamID   models.TeamID
	sourceID models.SourceID
}

func lockedPanel(id, title string, teamID models.TeamID, sourceID models.SourceID) plannedPanel {
	return plannedPanel{
		fixed:  reports.Panel{ID: id, Title: title, Locked: true},
		status: models.ReportPanelLocked, teamID: teamID, sourceID: sourceID,
	}
}

// runStop ends a run before any query, by an owner rule or a source fault.
type runStop struct {
	status models.ReportRunStatus
	reason models.ReportRunReason
	// cause is the fault behind a source_invalid stop, for the log.
	cause error
}

func stop(status models.ReportRunStatus, reason models.ReportRunReason) *runStop {
	return &runStop{status: status, reason: reason}
}

// sourceInvalid stops a run whose stored source no longer runs: the saved
// query's content, the timezone, the range or a variable.
func sourceInvalid(cause error) *runStop {
	return &runStop{status: models.ReportRunFailed, reason: models.ReportReasonSourceInvalid, cause: cause}
}

// plan applies the owner rules of "Run identity" and authorizes the owner
// for the source at this run, with nothing cached.
func (rr *ReportRunner) plan(ctx context.Context, r *models.Report, at time.Time) (*reportPlan, *runStop, error) {
	if r.OwnerID == nil {
		return nil, stop(models.ReportRunSkipped, models.ReportReasonNoOwner), nil
	}
	owner, err := rr.DB.GetUser(ctx, *r.OwnerID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, stop(models.ReportRunSkipped, models.ReportReasonNoOwner), nil
		}
		return nil, nil, fmt.Errorf("loading report owner: %w", err)
	}
	if owner.Status != models.UserStatusActive {
		return nil, stop(models.ReportRunSkipped, models.ReportReasonOwnerInactive), nil
	}
	loc, err := time.LoadLocation(r.Timezone)
	if err != nil {
		return nil, sourceInvalid(err), nil
	}
	switch {
	case r.SourceKind == models.ReportSourceSavedQuery && r.SavedQueryID != nil:
		return rr.planSavedQuery(ctx, r, owner, loc, at)
	case r.SourceKind == models.ReportSourceDashboard && r.DashboardID != nil:
		return rr.planDashboard(ctx, r, owner, loc, at)
	}
	return nil, stop(models.ReportRunFailed, models.ReportReasonSourceInvalid), nil
}

func (rr *ReportRunner) planSavedQuery(ctx context.Context, r *models.Report, owner *models.User, loc *time.Location, at time.Time) (*reportPlan, *runStop, error) {
	sq, err := rr.DB.GetSavedQuery(ctx, *r.SavedQueryID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, stop(models.ReportRunFailed, models.ReportReasonSourceInvalid), nil
		}
		return nil, nil, fmt.Errorf("loading saved query %d: %w", *r.SavedQueryID, err)
	}
	content, err := savedQueryContent(sq)
	if err != nil {
		return nil, sourceInvalid(err), nil
	}
	window, err := reports.ResolveWindow(r.TimeRange, r.RangeAnchor == models.ReportAnchorDayStart, loc, at)
	if err != nil {
		return nil, sourceInvalid(err), nil
	}
	variables, err := resolveReportVariables(content.Variables, r.Variables, loc, at)
	if err != nil {
		return nil, sourceInvalid(err), nil
	}

	src, ok, err := rr.authorizeOwner(ctx, owner, sq.SourceID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, stop(models.ReportRunFailed, models.ReportReasonNoAccess), nil
	}
	source, err := rr.DB.GetSource(ctx, sq.SourceID)
	if err != nil {
		return nil, nil, fmt.Errorf("loading source %d: %w", sq.SourceID, err)
	}
	run := rr.savedQueryRun(src, sq, content, r.Timezone, window, variables)
	maxRows := r.MaxRows
	job := &reports.PanelJob{ID: reportPanelID, Title: sq.Name, Run: func(ctx context.Context) (reports.PanelOutput, error) {
		res, err := run(ctx)
		if err != nil {
			return reports.PanelOutput{}, err
		}
		return tableOutput(reportPanelID, sq.Name, res, loc, maxRows), nil
	}}
	plan := &reportPlan{
		report: r, owner: owner, loc: loc, window: window, sources: []string{source.Name},
		panels: []plannedPanel{{job: job, sourceID: sq.SourceID}},
	}
	if rr.UIURL != "" {
		plan.sourceURL = fmt.Sprintf("%s/logs/saved/%d", rr.UIURL, sq.ID)
	}
	return plan, nil, nil
}

// tableOutput renders a result as an email table.
func tableOutput(id, title string, res *models.QueryResult, loc *time.Location, maxRows int) reports.PanelOutput {
	table := reports.RenderTable(res, loc, maxRows)
	return reports.PanelOutput{
		Panel: reports.Panel{ID: id, Title: title, Table: &table},
		Rows:  len(res.Logs), Truncation: res.Stats.Truncation, Empty: len(res.Logs) == 0,
	}
}

// authorizeOwner passes the owner's report principal through
// AuthorizeTeamSource for the first team of the owner that has the source.
// ok is false when no team of the owner has it.
func (rr *ReportRunner) authorizeOwner(ctx context.Context, owner *models.User, sourceID models.SourceID) (access.AuthorizedSource, bool, error) {
	teams, err := ListTeamsWithAccessToSource(ctx, rr.DB, rr.Log, sourceID, owner.ID)
	if err != nil {
		return access.AuthorizedSource{}, false, fmt.Errorf("listing the owner's teams for source %d: %w", sourceID, err)
	}
	if len(teams) == 0 {
		return access.AuthorizedSource{}, false, nil
	}
	team := slices.MinFunc(teams, func(a, b *models.Team) int { return cmp.Compare(a.ID, b.ID) })
	return authorizeReportSource(ctx, rr.DB, owner, team.ID, sourceID)
}

// authorizeReportSource authorizes the owner's report principal for one
// team and source. ok is false when the owner cannot reach it.
func authorizeReportSource(ctx context.Context, db store.StoreOps, owner *models.User, teamID models.TeamID, sourceID models.SourceID) (access.AuthorizedSource, bool, error) {
	src, err := access.AuthorizeTeamSource(ctx, db, access.ReportPrincipal(owner), teamID, sourceID, models.TokenScopeLogsRead)
	switch {
	case errors.Is(err, access.ErrNotTeamMember), errors.Is(err, access.ErrSourceNotInTeam), errors.Is(err, access.ErrInsufficientScope):
		return access.AuthorizedSource{}, false, nil
	case err != nil:
		return access.AuthorizedSource{}, false, err
	}
	return src, true, nil
}

// reportQueryConfig bounds a report's panel queries: the result size and
// the query timeout.
func (rr *ReportRunner) reportQueryConfig() (cfg config.QueryConfig, timeoutSeconds int) {
	cfg = rr.Query
	cfg.MaxResponseBytes = min(cfg.MaxResponseBytes, maxReportResultBytes)
	return cfg, min(models.DefaultQueryTimeoutSeconds, cfg.MaxTimeoutSeconds)
}

// savedQueryRun runs the saved query through the paths HTTP and MCP share.
func (rr *ReportRunner) savedQueryRun(src access.AuthorizedSource, sq *models.SavedQuery, content *models.SavedQueryContent, tz string, w reports.Window, variables []models.TemplateVariable) func(context.Context) (*models.QueryResult, error) {
	cfg, timeout := rr.reportQueryConfig()
	limit := min(content.Limit, maxReportQueryRows)
	start, end := w.Start.UTC().Format(time.RFC3339), w.End.UTC().Format(time.RFC3339)
	return func(ctx context.Context) (*models.QueryResult, error) {
		if sq.QueryLanguage == models.QueryLanguageLogchefQL {
			_, res, err := RunLogchefQL(ctx, rr.DS, cfg, src, LogchefQLQueryRequest{
				Query: content.Content, StartTime: start, EndTime: end, Timezone: tz, Limit: limit,
				QueryTimeout: &timeout, Variables: variables, AllowAggregation: true,
			})
			return res, err
		}
		return RunQuery(ctx, rr.DS, cfg, src, models.APIQueryRequest{
			QueryText: content.Content, StartTime: start, EndTime: end, Timezone: tz, Limit: limit,
			QueryTimeout: &timeout, Variables: variables,
		})
	}
}

// renderedRun is a run whose panels ran and whose email is built.
type renderedRun struct {
	panels   []models.ReportPanelOutcome
	rendered reports.Rendered
	stop     *runStop
}

// runAndRender runs the panels until queryDeadline, decides the outcomes
// that end a run before delivery, and renders the email.
func (rr *ReportRunner) runAndRender(ctx context.Context, plan *reportPlan, queryDeadline time.Time) (*renderedRun, error) {
	var jobs []reports.PanelJob
	for _, p := range plan.panels {
		if p.job != nil {
			jobs = append(jobs, *p.job)
		}
	}
	results := reports.RunPanels(ctx, queryDeadline, jobs)
	out := &renderedRun{panels: make([]models.ReportPanelOutcome, len(plan.panels))}
	doc := reports.Document{
		ReportName: plan.report.Name,
		Location:   plan.loc,
		Window:     plan.window,
		Owner:      cmp.Or(plan.owner.FullName, plan.owner.Email),
		Sources:    plan.sources,
		SourceURL:  plan.sourceURL,
	}
	if rr.UIURL != "" {
		doc.ReportURL = fmt.Sprintf("%s/reports/%d", rr.UIURL, plan.report.ID)
	}
	ran, failed, timedOut, empty := 0, 0, 0, 0
	for i, p := range plan.panels {
		if p.job == nil {
			out.panels[i] = models.ReportPanelOutcome{PanelID: p.fixed.ID, Title: p.fixed.Title, Status: p.status}
			doc.Panels = append(doc.Panels, p.fixed)
			continue
		}
		res := results[ran]
		ran++
		o := models.ReportPanelOutcome{PanelID: res.Job.ID, Title: res.Job.Title}
		panel := res.Output.Panel
		switch {
		case res.Err != nil:
			o.Status, o.Error = models.ReportPanelFailed, res.Err.Error()
			panel = reports.Panel{ID: res.Job.ID, Title: res.Job.Title, Failed: true}
			failed++
			if errors.Is(res.Err, reports.ErrQueryBudget) {
				timedOut++
			}
		default:
			o.Rows, o.Truncation, o.Status = res.Output.Rows, res.Output.Truncation, models.ReportPanelOK
			if res.Output.Empty {
				o.Status = models.ReportPanelEmpty
				empty++
			}
		}
		out.panels[i] = o
		doc.Panels = append(doc.Panels, panel)
	}
	switch {
	case ran > 0 && failed == ran && timedOut == failed:
		out.stop = stop(models.ReportRunFailed, models.ReportReasonTimeout)
		return out, nil
	case ran > 0 && failed == ran:
		out.stop = stop(models.ReportRunFailed, models.ReportReasonAllPanelsFailed)
		return out, nil
	case ran > 0 && empty == ran && plan.report.SkipIfEmpty:
		out.stop = stop(models.ReportRunSkipped, models.ReportReasonEmpty)
		return out, nil
	}
	rendered, err := reports.Render(doc, plan.report.MaxRows)
	if errors.Is(err, reports.ErrBodyTooLarge) {
		out.stop = stop(models.ReportRunFailed, models.ReportReasonAllPanelsFailed)
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.rendered = rendered
	return out, nil
}

func (rr *ReportRunner) execute(ctx context.Context, run *models.ReportRun, r *models.Report, to ReportRunRecipients) (models.ReportRunResult, error) {
	if run.DeadlineAt == nil {
		return models.ReportRunResult{}, fmt.Errorf("run %d has no deadline", run.ID)
	}
	deadlines := reports.DeadlinesFrom(*run.DeadlineAt)
	plan, halt, err := rr.plan(ctx, r, run.ScheduledFor)
	if err != nil {
		return models.ReportRunResult{}, err
	}
	if halt != nil {
		if halt.cause != nil {
			rr.Log.Warn("report source no longer runs", "report_id", r.ID, "run_id", run.ID, "error", halt.cause)
		}
		return models.ReportRunResult{Status: halt.status, Reason: halt.reason}, nil
	}
	res := models.ReportRunResult{WindowStart: &plan.window.Start, WindowEnd: &plan.window.End}
	rendered, err := rr.runAndRender(ctx, plan, deadlines.Query)
	if err != nil {
		return models.ReportRunResult{}, err
	}
	res.Panels = rendered.panels
	if rendered.stop != nil {
		res.Status, res.Reason = rendered.stop.status, rendered.stop.reason
		return res, nil
	}

	users, err := rr.runRecipients(ctx, run, plan, to)
	if err != nil {
		return models.ReportRunResult{}, err
	}
	msg := reports.MailMessage{
		Subject: reports.Subject(r.Name, run.ScheduledFor, plan.loc),
		Date:    time.Now(),
		Text:    rendered.rendered.Text,
		HTML:    rendered.rendered.HTML,
	}
	for _, u := range users {
		recipient := reports.Recipient{
			Address: mail.Address{Name: u.FullName, Address: u.Email},
			Class:   models.ReportDeliveryUser,
			Target:  strconv.FormatInt(int64(u.ID), 10),
			Via:     "direct",
		}
		if reason, err := rr.userCannotReceive(ctx, u, plan); err != nil {
			return models.ReportRunResult{}, err
		} else if reason != "" {
			res.Deliveries = append(res.Deliveries, models.ReportDelivery{
				Class: recipient.Class, Target: recipient.Target, Via: recipient.Via,
				Status: models.ReportDeliverySkipped, Reason: reason,
			})
			continue
		}
		res.Deliveries = append(res.Deliveries, reports.Delivery{Mailer: rr.Mailer, Sleep: rr.Sleep}.Deliver(ctx, recipient, msg, deadlines.Delivery))
	}
	res.Status, res.Reason = reports.DeliveryOutcome(res.Panels, res.Deliveries)
	return res, nil
}

// runRecipients is the caller for a test sent to them, and the owner for a
// run sent to every recipient.
func (rr *ReportRunner) runRecipients(ctx context.Context, run *models.ReportRun, plan *reportPlan, to ReportRunRecipients) ([]*models.User, error) {
	if to == ReportRunToAll {
		return []*models.User{plan.owner}, nil
	}
	if run.ActorID == nil {
		return nil, fmt.Errorf("manual run %d has no actor to send to", run.ID)
	}
	u, err := rr.DB.GetUser(ctx, *run.ActorID)
	if err != nil {
		return nil, fmt.Errorf("loading the caller of run %d: %w", run.ID, err)
	}
	return []*models.User{u}, nil
}

// userCannotReceive applies the per-user test of RFC 0008: an active human
// account with an address that reaches every panel the email shows data
// of. A saved query is reached through any team of the user's own that has
// its source; a dashboard panel through the panel's team. It returns the
// skip reason, or "".
//
// Until per-recipient locking (RFC 0008 unit R8), a user who cannot reach
// one of the panels gets no email, rather than one with that panel locked.
func (rr *ReportRunner) userCannotReceive(ctx context.Context, u *models.User, plan *reportPlan) (string, error) {
	if u.Status != models.UserStatusActive || u.AccountType == models.UserAccountTypeService || u.Email == "" {
		return "inactive", nil
	}
	for _, p := range plan.panels {
		if p.status == models.ReportPanelLocked || p.status == models.ReportPanelUnsupported {
			continue
		}
		var (
			reach bool
			err   error
		)
		if p.teamID == 0 {
			reach, err = rr.DB.UserHasSourceAccess(ctx, u.ID, p.sourceID)
		} else {
			reach, err = UserHasAccessToTeamSource(ctx, rr.DB, rr.Log, u.ID, p.teamID, p.sourceID)
		}
		if err != nil {
			return "", fmt.Errorf("checking source access of user %d: %w", u.ID, err)
		}
		if !reach {
			return string(models.ReportReasonNoAccess), nil
		}
	}
	return "", nil
}

// ReportPreview is the email the caller would receive.
type ReportPreview struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

// Preview runs a report the caller may edit as its owner and renders the
// email the caller would receive. It takes the preview cooldown and stores
// no run.
func (rr *ReportRunner) Preview(ctx context.Context, p access.Principal, id models.ReportID) (*ReportPreview, error) {
	r, err := GetReportForEditor(ctx, rr.DB, p, id)
	if err != nil {
		return nil, err
	}
	taken, err := rr.DB.TakeReportCooldown(ctx, id, models.ReportCooldownPreview)
	if err != nil {
		return nil, fmt.Errorf("taking the preview cooldown of report %d: %w", id, err)
	}
	if !taken {
		return nil, ErrReportCooldown
	}
	at := time.Now()
	plan, halt, err := rr.plan(ctx, r, at)
	if err != nil {
		return nil, err
	}
	if halt != nil {
		switch halt.reason {
		case models.ReportReasonNoOwner, models.ReportReasonOwnerInactive:
			return nil, ErrReportOwnerUnavailable
		case models.ReportReasonNoAccess:
			return nil, fmt.Errorf("%w: the report owner cannot view its source", ErrReportForbidden)
		default:
		}
		return nil, invalidReport("the report's source can no longer run: %s", halt.reason)
	}
	reason, err := rr.userCannotReceive(ctx, p.User, plan)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return nil, fmt.Errorf("%w: a run would send you nothing (%s)", ErrReportForbidden, reason)
	}
	rendered, err := rr.runAndRender(ctx, plan, at.Add(rr.QueryBudget))
	if err != nil {
		return nil, err
	}
	if rendered.stop != nil {
		var errs []string
		for _, panel := range rendered.panels {
			if panel.Error != "" {
				errs = append(errs, panel.Error)
			}
		}
		return nil, invalidReport("a run would send nothing (%s) %s", rendered.stop.reason, strings.Join(errs, "; "))
	}
	return &ReportPreview{Subject: reports.Subject(r.Name, at, plan.loc), HTML: rendered.rendered.HTML}, nil
}
