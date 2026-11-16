package core

// Scheduled reports (RFC 0008): save validation, who may see and change a
// report, and the response shape. report_runs.go runs them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrReportNotFound means the report does not exist or the caller may
	// not see it.
	ErrReportNotFound = errors.New("report not found")
	// ErrInvalidReport wraps a field that fails validation. The message
	// names the field.
	ErrInvalidReport = errors.New("invalid report")
	// ErrReportForbidden means the caller may not change the report, or the
	// owner cannot view its source.
	ErrReportForbidden = errors.New("not allowed")
	// ErrReportCooldown means a manual run or a preview of the report
	// started less than models.ReportCooldown ago.
	ErrReportCooldown = errors.New("a report allows one manual run and one preview per minute")
	// ErrReportOwnerUnavailable means the report has no active owner to run
	// as.
	ErrReportOwnerUnavailable = errors.New("the report has no active owner")
)

func invalidReport(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidReport, fmt.Sprintf(format, args...))
}

const (
	defaultReportMaxRows = 25
	minReportMaxRows     = 5
	maxReportMaxRows     = 100
	maxReportNameRunes   = 200
	// reportPanelID names the one panel of a saved-query report in the run
	// history and in warnings.
	reportPanelID = "query"
)

// ReportSourceRef names what a report sends.
type ReportSourceRef struct {
	Kind         models.ReportSourceKind `json:"kind"`
	DashboardID  *int                    `json:"dashboard_id,omitempty"`
	TabID        string                  `json:"tab_id,omitempty"`
	SavedQueryID *int                    `json:"saved_query_id,omitempty"`
}

// ReportInput is the body of POST and PUT /reports. PUT replaces every
// field.
type ReportInput struct {
	Name        string                   `json:"name"`
	Source      ReportSourceRef          `json:"source"`
	Schedule    json.RawMessage          `json:"schedule"`
	Timezone    string                   `json:"timezone"`
	TimeRange   *string                  `json:"time_range"`
	RangeAnchor models.ReportRangeAnchor `json:"range_anchor"`
	Variables   map[string]any           `json:"variables"`
	MaxRows     *int                     `json:"max_rows"`
	SkipIfEmpty bool                     `json:"skip_if_empty"`
	AttachCSV   bool                     `json:"attach_csv"`
	Recipients  []json.RawMessage        `json:"recipients"`
}

// ReportDeps is what the report functions read and call.
type ReportDeps struct {
	DB store.Store
	// UIURL is the base URL of the web UI, empty when none is configured.
	UIURL string
}

// canEditReport: the owner or a global admin. An OAuth or assistant
// principal is never a global admin.
func canEditReport(p access.Principal, r *models.Report) bool {
	if p.User == nil {
		return false
	}
	if p.RequireGlobalAdmin() == nil {
		return true
	}
	return r.OwnerID != nil && *r.OwnerID == p.User.ID
}

// GetReportForEditor returns the report when the caller is its owner or a
// global admin, and ErrReportNotFound otherwise, so a caller does not learn
// that a report exists.
func GetReportForEditor(ctx context.Context, db store.StoreOps, p access.Principal, id models.ReportID) (*models.Report, error) {
	r, err := db.GetReport(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrReportNotFound
		}
		return nil, fmt.Errorf("loading report %d: %w", id, err)
	}
	if !canEditReport(p, r) {
		return nil, ErrReportNotFound
	}
	return r, nil
}

// validateReport checks in against the RFC's save rules and returns the
// report to store, with the owner and the state left to the caller. owner
// is nil for a report without one: the source check then waits for a claim.
func validateReport(ctx context.Context, db store.StoreOps, owner *models.User, in *ReportInput, now time.Time) (*models.Report, error) {
	if strings.TrimSpace(in.Name) == "" || utf8.RuneCountInString(in.Name) > maxReportNameRunes {
		return nil, invalidReport("name must be 1 to %d characters", maxReportNameRunes)
	}
	if strings.ContainsAny(in.Name, "\r\n") {
		return nil, invalidReport("name must not contain a line break")
	}
	if len(in.Recipients) > 0 {
		return nil, invalidReport("recipients: a report is sent to its owner for now; user, team and email recipients are not supported yet")
	}
	maxRows := defaultReportMaxRows
	if in.MaxRows != nil {
		maxRows = *in.MaxRows
	}
	if maxRows < minReportMaxRows || maxRows > maxReportMaxRows {
		return nil, invalidReport("max_rows must be %d to %d", minReportMaxRows, maxReportMaxRows)
	}
	var (
		r   *models.Report
		err error
	)
	switch in.Source.Kind {
	case models.ReportSourceSavedQuery:
		r, err = validateSavedQueryReport(ctx, db, owner, in, now)
	case models.ReportSourceDashboard:
		r, err = validateDashboardReport(ctx, db, owner, in, now)
	default:
		return nil, invalidReport("source.kind must be dashboard or saved_query")
	}
	if err != nil {
		return nil, err
	}
	r.Name, r.MaxRows, r.SkipIfEmpty = in.Name, maxRows, in.SkipIfEmpty
	return r, nil
}

func validateSavedQueryReport(ctx context.Context, db store.StoreOps, owner *models.User, in *ReportInput, now time.Time) (*models.Report, error) {
	sq, err := validateReportSource(ctx, db, owner, in.Source)
	if err != nil {
		return nil, err
	}
	if in.TimeRange == nil || *in.TimeRange == "" {
		return nil, invalidReport("time_range is required for a saved query")
	}
	timing, err := validateReportTiming(in, *in.TimeRange, now)
	if err != nil {
		return nil, err
	}
	content, err := savedQueryContent(sq)
	if err != nil {
		return nil, invalidReport("source: %v", err)
	}
	variables := in.Variables
	if variables == nil {
		variables = map[string]any{}
	}
	if _, err := resolveReportVariables(content.Variables, variables, timing.loc, now); err != nil {
		return nil, invalidReport("%v", err)
	}
	return &models.Report{
		SourceKind:   models.ReportSourceSavedQuery,
		SavedQueryID: &sq.ID,
		Schedule:     timing.schedule,
		Timezone:     in.Timezone,
		TimeRange:    *in.TimeRange,
		RangeAnchor:  timing.anchor,
		Variables:    variables,
		AttachCSV:    in.AttachCSV,
	}, nil
}

// validateDashboardReport checks a dashboard source: the dashboard runs,
// the tab exists and the owner can view the dashboard. RFC 0008 bounds a
// report at 30 query panels; a dashboard holds at most
// models.MaxDashboardPanels, fewer, so no selection reaches the bound. A dashboard has no
// variables, and its range defaults to the dashboard's.
func validateDashboardReport(ctx context.Context, db store.StoreOps, owner *models.User, in *ReportInput, now time.Time) (*models.Report, error) {
	src := in.Source
	if in.AttachCSV {
		return nil, invalidReport("attach_csv needs a saved_query source")
	}
	if len(in.Variables) > 0 {
		return nil, invalidReport("variables: a dashboard report takes no variables")
	}
	if src.SavedQueryID != nil {
		return nil, invalidReport("source: a dashboard source takes no saved_query_id")
	}
	if src.DashboardID == nil || *src.DashboardID <= 0 {
		return nil, invalidReport("source.dashboard_id is required")
	}
	d, err := db.GetDashboard(ctx, *src.DashboardID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, invalidReport("source.dashboard_id: dashboard %d does not exist", *src.DashboardID)
		}
		return nil, fmt.Errorf("loading dashboard %d: %w", *src.DashboardID, err)
	}
	view, _, err := reportDashboardPanels(d, src.TabID)
	if err != nil {
		return nil, invalidReport("source: %v", err)
	}
	var timeRange string
	if in.TimeRange != nil {
		timeRange = *in.TimeRange
	}
	timing, err := validateReportTiming(in, dashboardReportRange(timeRange, view), now)
	if err != nil {
		return nil, err
	}
	if owner != nil {
		canView, err := UserCanViewDashboard(ctx, db, owner, d)
		if err != nil {
			return nil, fmt.Errorf("checking the owner's view of dashboard %d: %w", d.ID, err)
		}
		if !canView {
			return nil, fmt.Errorf("%w: the report owner cannot view this dashboard", ErrReportForbidden)
		}
	}
	return &models.Report{
		SourceKind:  models.ReportSourceDashboard,
		DashboardID: &d.ID,
		TabID:       src.TabID,
		Schedule:    timing.schedule,
		Timezone:    in.Timezone,
		TimeRange:   timeRange,
		RangeAnchor: timing.anchor,
		Variables:   map[string]any{},
	}, nil
}

// reportTiming is the validated schedule, timezone and range anchor.
type reportTiming struct {
	schedule models.ReportSchedule
	loc      *time.Location
	anchor   models.ReportRangeAnchor
}

// validateReportTiming checks the schedule, the timezone and the anchor,
// and that timeRange, the range a run resolves, resolves with them.
func validateReportTiming(in *ReportInput, timeRange string, now time.Time) (reportTiming, error) {
	if len(bytes.TrimSpace(in.Schedule)) == 0 {
		return reportTiming{}, invalidReport("schedule is required")
	}
	schedule, err := models.ParseReportSchedule(in.Schedule)
	if err != nil {
		return reportTiming{}, invalidReport("%v", err)
	}
	if in.Timezone == "" {
		return reportTiming{}, invalidReport("timezone is required")
	}
	loc, err := ValidateTimezone(in.Timezone)
	if err != nil {
		return reportTiming{}, invalidReport("timezone: %v", err)
	}
	anchor := in.RangeAnchor
	if anchor == "" {
		anchor = models.ReportAnchorSendTime
	}
	if anchor != models.ReportAnchorSendTime && anchor != models.ReportAnchorDayStart {
		return reportTiming{}, invalidReport("range_anchor must be send_time or day_start")
	}
	if _, err := reports.ResolveWindow(timeRange, anchor == models.ReportAnchorDayStart, loc, now); err != nil {
		return reportTiming{}, invalidReport("time_range: %v", err)
	}
	return reportTiming{schedule: schedule, loc: loc, anchor: anchor}, nil
}

func validateReportSource(ctx context.Context, db store.StoreOps, owner *models.User, src ReportSourceRef) (*models.SavedQuery, error) {
	if src.DashboardID != nil || src.TabID != "" {
		return nil, invalidReport("source: a saved_query source takes no dashboard_id or tab_id")
	}
	if src.SavedQueryID == nil || *src.SavedQueryID <= 0 {
		return nil, invalidReport("source.saved_query_id is required")
	}
	sq, err := db.GetSavedQuery(ctx, *src.SavedQueryID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, invalidReport("source.saved_query_id: saved query %d does not exist", *src.SavedQueryID)
		}
		return nil, fmt.Errorf("loading saved query %d: %w", *src.SavedQueryID, err)
	}
	if owner == nil {
		return sq, nil
	}
	reach, err := db.UserHasSourceAccess(ctx, owner.ID, sq.SourceID)
	if err != nil {
		return nil, fmt.Errorf("checking source access: %w", err)
	}
	if !reach {
		return nil, fmt.Errorf("%w: the report owner cannot view this saved query, because none of the owner's teams has its source", ErrReportForbidden)
	}
	return sq, nil
}

func savedQueryContent(sq *models.SavedQuery) (*models.SavedQueryContent, error) {
	var content models.SavedQueryContent
	if err := json.Unmarshal([]byte(sq.QueryContent), &content); err != nil {
		return nil, fmt.Errorf("saved query %d has unreadable content: %w", sq.ID, err)
	}
	if strings.TrimSpace(content.Content) == "" {
		return nil, fmt.Errorf("saved query %d has no query", sq.ID)
	}
	return &content, nil
}

// resolveReportVariables gives each declared variable its value: the
// report's, else the stored default, else the stored value. A day value of
// today or yesterday resolves at at in loc. A variable that is not optional
// must end with a value.
func resolveReportVariables(declared []models.SavedQueryVariable, values map[string]any, loc *time.Location, at time.Time) ([]models.TemplateVariable, error) {
	for name := range values {
		if !slices.ContainsFunc(declared, func(v models.SavedQueryVariable) bool { return v.Name == name }) {
			return nil, fmt.Errorf("variables: the saved query declares no variable %q", name)
		}
	}
	var out []models.TemplateVariable
	for i := range declared {
		v := &declared[i]
		value := values[v.Name]
		if !variableValueSet(value) {
			value = v.DefaultValue
		}
		if !variableValueSet(value) {
			value = v.Value
		}
		if !variableValueSet(value) {
			if !v.IsOptional {
				return nil, fmt.Errorf("variables: %q needs a value", v.Name)
			}
			continue
		}
		if v.Type == "day" {
			value = resolveDayValue(value, loc, at)
		}
		out = append(out, models.TemplateVariable{Name: v.Name, Type: v.Type, Value: value})
	}
	return out, nil
}

func variableValueSet(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	}
	return true
}

func resolveDayValue(v any, loc *time.Location, at time.Time) any {
	switch v {
	case "today":
		return at.In(loc).Format(time.DateOnly)
	case "yesterday":
		y, m, d := at.In(loc).Date()
		return time.Date(y, m, d-1, 12, 0, 0, 0, loc).Format(time.DateOnly)
	}
	return v
}

// CreateReport validates in and stores an active report owned by the
// caller.
func CreateReport(ctx context.Context, db store.StoreOps, p access.Principal, in *ReportInput) (*models.Report, error) {
	r, err := validateReport(ctx, db, p.User, in, time.Now())
	if err != nil {
		return nil, err
	}
	r.OwnerID = &p.User.ID
	if err := db.CreateReport(ctx, r); err != nil {
		return nil, fmt.Errorf("creating report: %w", err)
	}
	return r, nil
}

// UpdateReport replaces the edited fields of a report the caller may edit.
// The source check uses the owner, also when a global admin edits. It
// returns the report before and after the change.
func UpdateReport(ctx context.Context, db store.StoreOps, p access.Principal, id models.ReportID, in *ReportInput) (before, after *models.Report, err error) {
	existing, err := GetReportForEditor(ctx, db, p, id)
	if err != nil {
		return nil, nil, err
	}
	var owner *models.User
	if existing.OwnerID != nil {
		if owner, err = db.GetUser(ctx, *existing.OwnerID); err != nil {
			return nil, nil, fmt.Errorf("loading report owner: %w", err)
		}
	}
	r, err := validateReport(ctx, db, owner, in, time.Now())
	if err != nil {
		return nil, nil, err
	}
	r.ID = existing.ID
	if err := db.UpdateReport(ctx, r); err != nil {
		if models.IsNotFound(err) {
			return nil, nil, ErrReportNotFound
		}
		return nil, nil, fmt.Errorf("updating report %d: %w", id, err)
	}
	return existing, r, nil
}

// DeleteReport deletes a report the caller may edit and returns it.
func DeleteReport(ctx context.Context, db store.StoreOps, p access.Principal, id models.ReportID) (*models.Report, error) {
	r, err := GetReportForEditor(ctx, db, p, id)
	if err != nil {
		return nil, err
	}
	if err := db.DeleteReport(ctx, id); err != nil {
		if models.IsNotFound(err) {
			return nil, ErrReportNotFound
		}
		return nil, fmt.Errorf("deleting report %d: %w", id, err)
	}
	return r, nil
}

// PauseReport pauses a report the caller may edit, with reason user.
func PauseReport(ctx context.Context, db store.StoreOps, p access.Principal, id models.ReportID) (*models.Report, error) {
	if _, err := GetReportForEditor(ctx, db, p, id); err != nil {
		return nil, err
	}
	if err := db.PauseReport(ctx, id, models.ReportPauseUser); err != nil {
		return nil, fmt.Errorf("pausing report %d: %w", id, err)
	}
	return reloadReport(ctx, db, id)
}

// ResumeReport makes a report the caller may edit active and clears its
// failure streak.
func ResumeReport(ctx context.Context, db store.StoreOps, p access.Principal, id models.ReportID) (*models.Report, error) {
	if _, err := GetReportForEditor(ctx, db, p, id); err != nil {
		return nil, err
	}
	if err := db.ResumeReport(ctx, id); err != nil {
		return nil, fmt.Errorf("resuming report %d: %w", id, err)
	}
	return reloadReport(ctx, db, id)
}

// ClaimReport makes the caller the owner of a report that has no owner, or
// whose owner is inactive. A global admin may claim any such report; anyone
// else must reach its source, because the claim lends the caller's access.
// It returns the report before the claim and after it.
func ClaimReport(ctx context.Context, db store.StoreOps, p access.Principal, id models.ReportID) (before, after *models.Report, err error) {
	r, err := db.GetReport(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, nil, ErrReportNotFound
		}
		return nil, nil, fmt.Errorf("loading report %d: %w", id, err)
	}
	if p.RequireGlobalAdmin() != nil {
		reach, err := userCanViewReportSource(ctx, db, p.User, r)
		if err != nil {
			return nil, nil, err
		}
		if !reach {
			return nil, nil, ErrReportNotFound
		}
	}
	claimable := r.OwnerID == nil || r.PauseReason == models.ReportPauseOwnerInactive
	if !claimable {
		return nil, nil, fmt.Errorf("%w: the report has an active owner", ErrReportForbidden)
	}
	if err := db.ClaimReport(ctx, id, p.User.ID); err != nil {
		if errors.Is(err, models.ErrReportOwned) {
			return nil, nil, fmt.Errorf("%w: the report has an active owner", ErrReportForbidden)
		}
		return nil, nil, fmt.Errorf("claiming report %d: %w", id, err)
	}
	after, err = reloadReport(ctx, db, id)
	return r, after, err
}

// userCanViewReportSource reports whether u can view the report's source:
// a team of u's own has the saved query's source, or u can view the
// dashboard.
func userCanViewReportSource(ctx context.Context, db store.StoreOps, u *models.User, r *models.Report) (bool, error) {
	switch {
	case r.SavedQueryID != nil:
		sq, err := db.GetSavedQuery(ctx, *r.SavedQueryID)
		if err != nil {
			return false, fmt.Errorf("loading saved query of report %d: %w", r.ID, err)
		}
		reach, err := db.UserHasSourceAccess(ctx, u.ID, sq.SourceID)
		if err != nil {
			return false, fmt.Errorf("checking source access: %w", err)
		}
		return reach, nil
	case r.DashboardID != nil:
		d, err := db.GetDashboard(ctx, *r.DashboardID)
		if err != nil {
			return false, fmt.Errorf("loading dashboard of report %d: %w", r.ID, err)
		}
		return UserCanViewDashboard(ctx, db, u, d)
	}
	return false, nil
}

func reloadReport(ctx context.Context, db store.StoreOps, id models.ReportID) (*models.Report, error) {
	r, err := db.GetReport(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrReportNotFound
		}
		return nil, fmt.Errorf("loading report %d: %w", id, err)
	}
	return r, nil
}

// ReportScope selects the reports of a list.
type ReportScope string

const (
	ReportScopeMine ReportScope = "mine"
	ReportScopeAll  ReportScope = "all"
)

// ListReports lists the caller's reports, or every report for a global
// admin with scope all.
func ListReports(ctx context.Context, d ReportDeps, p access.Principal, scope ReportScope) ([]*ReportResponse, error) {
	var (
		list []*models.Report
		err  error
	)
	switch scope {
	case ReportScopeMine:
		list, err = d.DB.ListReportsByOwner(ctx, p.User.ID)
	case ReportScopeAll:
		if p.RequireGlobalAdmin() != nil {
			return nil, fmt.Errorf("%w: scope all needs a global admin", ErrReportForbidden)
		}
		list, err = d.DB.ListReports(ctx)
	default:
		return nil, invalidReport("scope must be mine or all")
	}
	if err != nil {
		return nil, fmt.Errorf("listing reports: %w", err)
	}
	return buildReportResponses(ctx, d, p, list)
}

// ReportOwner identifies the owner in a response.
type ReportOwner struct {
	ID    models.UserID `json:"id"`
	Name  string        `json:"name"`
	Email string        `json:"email"`
}

// ReportSourceResponse is the source with its name.
type ReportSourceResponse struct {
	ReportSourceRef
	Name string `json:"name"`
}

// ReportLastRun is the newest run of a report.
type ReportLastRun struct {
	ID         models.ReportRunID     `json:"id"`
	Status     models.ReportRunStatus `json:"status"`
	Reason     models.ReportRunReason `json:"reason"`
	StartedAt  time.Time              `json:"started_at"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
}

// ReportWarning is a fixed code, with the panel it concerns.
type ReportWarning struct {
	Code    string `json:"code"`
	PanelID string `json:"panel_id,omitempty"`
}

// ReportResponse is the one shape of a report in every response.
type ReportResponse struct {
	ID          models.ReportID          `json:"id"`
	Name        string                   `json:"name"`
	Source      ReportSourceResponse     `json:"source"`
	Schedule    models.ReportSchedule    `json:"schedule"`
	Timezone    string                   `json:"timezone"`
	TimeRange   string                   `json:"time_range"`
	RangeAnchor models.ReportRangeAnchor `json:"range_anchor"`
	Variables   map[string]any           `json:"variables"`
	MaxRows     int                      `json:"max_rows"`
	SkipIfEmpty bool                     `json:"skip_if_empty"`
	AttachCSV   bool                     `json:"attach_csv"`
	Recipients  []models.ReportRecipient `json:"recipients"`
	Owner       *ReportOwner             `json:"owner"`
	State       models.ReportState       `json:"state"`
	PauseReason models.ReportPauseReason `json:"pause_reason"`
	LastRun     *ReportLastRun           `json:"last_run"`
	CanEdit     bool                     `json:"can_edit"`
	Warnings    []ReportWarning          `json:"warnings"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

// buildReportResponses reads the owners, the saved queries and the newest
// runs once for the whole list.
func buildReportResponses(ctx context.Context, d ReportDeps, p access.Principal, list []*models.Report) ([]*ReportResponse, error) {
	out := make([]*ReportResponse, 0, len(list))
	if len(list) == 0 {
		return out, nil
	}
	users, err := d.DB.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	owners := make(map[models.UserID]*models.User, len(users))
	for _, u := range users {
		owners[u.ID] = u
	}
	queries, err := d.DB.ListAllSavedQueries(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing saved queries: %w", err)
	}
	queryNames := make(map[int]string, len(queries))
	for _, q := range queries {
		queryNames[q.ID] = q.Name
	}
	dashboards, err := d.DB.ListDashboardSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing dashboards: %w", err)
	}
	dashboardNames := make(map[int]string, len(dashboards))
	for _, dash := range dashboards {
		dashboardNames[dash.ID] = dash.Name
	}
	runs, err := d.DB.ListLatestReportRuns(ctx)
	if err != nil {
		return nil, err
	}
	lastRuns := make(map[models.ReportID]*models.ReportRun, len(runs))
	for _, run := range runs {
		lastRuns[run.ReportID] = run
	}
	for _, r := range list {
		resp := reportResponse(p, r, lastRuns[r.ID])
		switch {
		case r.SavedQueryID != nil:
			resp.Source.Name = queryNames[*r.SavedQueryID]
		case r.DashboardID != nil:
			resp.Source.Name = dashboardNames[*r.DashboardID]
		}
		if r.OwnerID != nil {
			if u := owners[*r.OwnerID]; u != nil {
				resp.Owner = &ReportOwner{ID: u.ID, Name: u.FullName, Email: u.Email}
			}
		}
		out = append(out, resp)
	}
	return out, nil
}

func reportResponse(p access.Principal, r *models.Report, last *models.ReportRun) *ReportResponse {
	resp := &ReportResponse{
		ID:   r.ID,
		Name: r.Name,
		Source: ReportSourceResponse{ReportSourceRef: ReportSourceRef{
			Kind: r.SourceKind, DashboardID: r.DashboardID, TabID: r.TabID, SavedQueryID: r.SavedQueryID,
		}},
		Schedule:    r.Schedule,
		Timezone:    r.Timezone,
		TimeRange:   r.TimeRange,
		RangeAnchor: r.RangeAnchor,
		Variables:   r.Variables,
		MaxRows:     r.MaxRows,
		SkipIfEmpty: r.SkipIfEmpty,
		AttachCSV:   r.AttachCSV,
		Recipients:  []models.ReportRecipient{},
		State:       r.State,
		PauseReason: r.PauseReason,
		CanEdit:     canEditReport(p, r),
		Warnings:    []ReportWarning{},
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if last != nil {
		resp.LastRun = &ReportLastRun{ID: last.ID, Status: last.Status, Reason: last.Reason, StartedAt: last.StartedAt, FinishedAt: last.FinishedAt}
	}
	return resp
}

// BuildReportResponse builds the response of one report, with the warnings
// that need reads of their own: whether the owner still reaches the source,
// whether SMTP is configured and whether emails can carry links.
func BuildReportResponse(ctx context.Context, d ReportDeps, p access.Principal, r *models.Report) (*ReportResponse, error) {
	list, err := buildReportResponses(ctx, d, p, []*models.Report{r})
	if err != nil {
		return nil, err
	}
	resp := list[0]
	if d.UIURL == "" {
		resp.Warnings = append(resp.Warnings, ReportWarning{Code: "no_frontend_url"})
	}
	if store.GetSettingWithDefault(ctx, d.DB, "alerts.smtp_host", "") == "" || store.GetSettingWithDefault(ctx, d.DB, "alerts.smtp_from", "") == "" {
		resp.Warnings = append(resp.Warnings, ReportWarning{Code: "smtp_not_configured"})
	}
	if r.OwnerID != nil {
		locked, err := panelsLockedForOwner(ctx, d.DB, *r.OwnerID, r)
		if err != nil {
			return nil, err
		}
		for _, id := range locked {
			resp.Warnings = append(resp.Warnings, ReportWarning{Code: "panel_locked_for_owner", PanelID: id})
		}
	}
	return resp, nil
}

// panelsLockedForOwner lists the panels of the report's source that the
// owner no longer reaches. A dashboard that no longer runs locks nothing:
// its run fails with source_invalid instead.
func panelsLockedForOwner(ctx context.Context, db store.StoreOps, ownerID models.UserID, r *models.Report) ([]string, error) {
	switch {
	case r.SavedQueryID != nil:
		sq, err := db.GetSavedQuery(ctx, *r.SavedQueryID)
		if err != nil {
			return nil, fmt.Errorf("loading saved query %d: %w", *r.SavedQueryID, err)
		}
		reach, err := db.UserHasSourceAccess(ctx, ownerID, sq.SourceID)
		if err != nil {
			return nil, fmt.Errorf("checking source access: %w", err)
		}
		if !reach {
			return []string{reportPanelID}, nil
		}
	case r.DashboardID != nil:
		dash, err := db.GetDashboard(ctx, *r.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("loading dashboard %d: %w", *r.DashboardID, err)
		}
		_, panels, err := reportDashboardPanels(dash, r.TabID)
		if err != nil {
			return nil, nil //nolint:nilerr // a dashboard that no longer runs fails its run, it locks no panel
		}
		var locked []string
		for i := range panels {
			member, err := IsTeamMember(ctx, db, panels[i].TeamID, ownerID)
			if err != nil {
				return nil, fmt.Errorf("checking team membership: %w", err)
			}
			linked := false
			if member {
				if linked, err = db.TeamHasSource(ctx, panels[i].TeamID, panels[i].SourceID); err != nil {
					return nil, fmt.Errorf("checking team source link: %w", err)
				}
			}
			if !linked {
				locked = append(locked, panels[i].ID)
			}
		}
		return locked, nil
	}
	return nil, nil
}

// ListReportRuns returns runs of a report the caller may edit, newest
// first. Runs hold query error text, so only the owner and global admins
// read them.
func ListReportRuns(ctx context.Context, db store.StoreOps, p access.Principal, id models.ReportID, before models.ReportRunID, limit int) ([]*models.ReportRun, error) {
	if _, err := GetReportForEditor(ctx, db, p, id); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > models.ReportRunHistoryLimit {
		return nil, invalidReport("limit must be 1 to %d", models.ReportRunHistoryLimit)
	}
	return db.ListReportRuns(ctx, id, before, limit)
}
