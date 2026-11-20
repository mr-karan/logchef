package core

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// QueryActivityAccount selects whose queries the Query Activity page counts.
type QueryActivityAccount string

const (
	QueryActivityHumans   QueryActivityAccount = "human"
	QueryActivityServices QueryActivityAccount = "service"
	QueryActivityAll      QueryActivityAccount = "all"
)

// Bounds of the Query Activity page.
const (
	MinQueryActivityDays     = 1
	MaxQueryActivityDays     = 365
	queryActivityTopUsers    = 10
	queryActivityTopSources  = 20
	queryActivityTopShapes   = 10
	queryActivityTopServices = 3
)

// QueryActivityRequest is a validated Query Activity request. Days is within
// MinQueryActivityDays..MaxQueryActivityDays; a zero SourceID or UserID
// selects every source or user.
type QueryActivityRequest struct {
	Days     int
	Account  QueryActivityAccount
	SourceID models.SourceID
	UserID   models.UserID
}

// DateWindow is a run of whole UTC days. Since and Until are 'YYYY-MM-DD'
// and inclusive.
type DateWindow struct {
	Since string `json:"since"`
	Until string `json:"until"`
	Days  int    `json:"days"`
}

// QueryActivity is the GET /api/v1/admin/query-stats payload. Every count
// covers Window and honors Filters, and Prior fields cover PriorWindow,
// except where a field says otherwise.
type QueryActivity struct {
	Window      DateWindow           `json:"window"`
	PriorWindow DateWindow           `json:"prior_window"`
	Filters     QueryActivityFilters `json:"filters"`
	// AccountTotals ignores Filters.Account, so the account switch can show
	// each choice's query count.
	AccountTotals AccountQueryTotals `json:"account_totals"`
	// TotalUsers counts the active users of the selected account type.
	TotalUsers int64                `json:"total_users"`
	Summary    QueryActivitySummary `json:"summary"`
	// VolumeByDay holds every day of Window, ascending, zero when idle.
	VolumeByDay []models.DailyQueryStat `json:"volume_by_day"`
	TopUsers    []models.UserQueryStat  `json:"top_users"`
	// ServiceSummary is nil in the service view and when no service account
	// queried.
	ServiceSummary *ServiceAccountSummary      `json:"service_summary"`
	Sources        []SourceActivity            `json:"sources"`
	Shapes         ShapeActivity               `json:"shapes"`
	LanguageMix    []models.LanguageQueryCount `json:"language_mix"`
}

// QueryActivityFilters echoes the request. A name is "" when its user or
// source row is gone.
type QueryActivityFilters struct {
	Account    QueryActivityAccount `json:"account"`
	SourceID   *models.SourceID     `json:"source_id"`
	SourceName string               `json:"source_name"`
	UserID     *models.UserID       `json:"user_id"`
	UserName   string               `json:"user_name"`
}

// AccountQueryTotals splits the window's queries by account type. All also
// counts the queries of deleted users, whose type is unknown.
type AccountQueryTotals struct {
	Human   int64 `json:"human"`
	Service int64 `json:"service"`
	All     int64 `json:"all"`
}

// QueryActivitySummary is the headline strip.
type QueryActivitySummary struct {
	Current ActivityPeriod `json:"current"`
	Prior   ActivityPeriod `json:"prior"`
}

// ActivityPeriod totals one window. AvgDailyActiveUsers is the mean over
// every day of the window of that day's distinct users.
type ActivityPeriod struct {
	models.QueryStatsPeriod
	AvgDailyActiveUsers float64 `json:"avg_daily_active_users"`
}

// ServiceAccountSummary sums the service accounts outside the service view.
type ServiceAccountSummary struct {
	AccountCount int64                  `json:"account_count"`
	QueryCount   int64                  `json:"query_count"`
	Top          []models.UserQueryStat `json:"top"`
}

// SourceActivity is one row of the sources table. Advisor is nil for a
// source the schema advisor does not analyze.
type SourceActivity struct {
	models.SourceQueryStat
	Advisor *AdvisorFindingCounts `json:"advisor"`
}

// AdvisorFindingCounts counts a source's open schema advisor findings by
// impact. Analyzed is false before the source's first report.
type AdvisorFindingCounts struct {
	Analyzed bool `json:"analyzed"`
	High     int  `json:"high"`
	Medium   int  `json:"medium"`
	Low      int  `json:"low"`
}

// ShapeActivity ranks the repeated filter shapes. The shape rollup carries
// no user, so only the source filter applies, and it keeps 90 days: Since
// is where the counted rows start, later than Window.Since for a longer
// window, and a row's Prior is nil when PriorWindow is not fully kept.
type ShapeActivity struct {
	Since string             `json:"since"`
	Rows  []ShapeActivityRow `json:"rows"`
	// UnanalyzedQueryCount counts the queries no single shape stands for.
	UnanalyzedQueryCount int64 `json:"unanalyzed_query_count"`
}

// ShapeActivityRow is one shape and the open finding of its source that
// filters on one of its columns, the most impactful first.
type ShapeActivityRow struct {
	models.QueryShapeStat
	Finding *ShapeFinding `json:"finding"`
}

// ShapeFinding names a schema advisor finding.
type ShapeFinding struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Impact string `json:"impact"`
}

// QueryActivityWindows returns the window of the last days UTC days, today
// included, and the same-length window just before it.
func QueryActivityWindows(now time.Time, days int) (current, prior DateWindow) {
	today := now.UTC()
	since := today.AddDate(0, 0, -(days - 1))
	current = DateWindow{Since: since.Format(time.DateOnly), Until: today.Format(time.DateOnly), Days: days}
	prior = DateWindow{
		Since: since.AddDate(0, 0, -days).Format(time.DateOnly),
		Until: since.AddDate(0, 0, -1).Format(time.DateOnly),
		Days:  days,
	}
	return current, prior
}

// GetQueryActivity reads the Query Activity page from the daily rollups.
func GetQueryActivity(ctx context.Context, db store.Store, now time.Time, req QueryActivityRequest) (*QueryActivity, error) {
	window, prior := QueryActivityWindows(now, req.Days)
	filter := models.QueryStatsFilter{
		PriorSince:  prior.Since,
		Since:       window.Since,
		Until:       window.Until,
		AccountType: accountTypeOf(req.Account),
		SourceID:    req.SourceID,
		UserID:      req.UserID,
	}
	out := &QueryActivity{Window: window, PriorWindow: prior}

	var err error
	if out.Filters, err = activityFilters(ctx, db, req); err != nil {
		return nil, err
	}
	if out.AccountTotals, err = accountTotals(ctx, db, filter); err != nil {
		return nil, err
	}
	if out.TotalUsers, err = totalUsers(ctx, db, filter.AccountType); err != nil {
		return nil, err
	}
	summary, err := db.QueryStatsSummary(ctx, filter)
	if err != nil {
		return nil, err
	}
	days, err := db.QueryStatsByDay(ctx, filter)
	if err != nil {
		return nil, err
	}
	out.VolumeByDay = dailySeries(now, window, days)
	out.Summary = QueryActivitySummary{
		Current: ActivityPeriod{QueryStatsPeriod: summary.Current, AvgDailyActiveUsers: avgDailyActive(window, days)},
		Prior:   ActivityPeriod{QueryStatsPeriod: summary.Prior, AvgDailyActiveUsers: avgDailyActive(prior, days)},
	}
	if out.TopUsers, err = db.QueryStatsByUser(ctx, filter, queryActivityTopUsers); err != nil {
		return nil, err
	}
	if req.Account != QueryActivityServices {
		if out.ServiceSummary, err = serviceSummary(ctx, db, filter); err != nil {
			return nil, err
		}
	}
	if out.LanguageMix, err = db.QueryStatsByLanguage(ctx, filter); err != nil {
		return nil, err
	}
	sources, err := db.QueryStatsBySource(ctx, filter, queryActivityTopSources)
	if err != nil {
		return nil, err
	}
	shapeFilter, priorKept := shapeWindow(now, window, prior, req.SourceID)
	shapes, err := db.TopQueryShapes(ctx, shapeFilter, queryActivityTopShapes)
	if err != nil {
		return nil, err
	}
	unanalyzed, err := db.CountUnanalyzedQueryShapes(ctx, shapeFilter)
	if err != nil {
		return nil, err
	}
	findings, err := openFindings(ctx, db, sources, shapes)
	if err != nil {
		return nil, err
	}
	out.Sources = sourceActivity(sources, findings)
	out.Shapes = ShapeActivity{Since: shapeFilter.Since, Rows: shapeRows(shapes, findings, priorKept), UnanalyzedQueryCount: unanalyzed}
	return out, nil
}

func accountTypeOf(account QueryActivityAccount) models.UserAccountType {
	switch account {
	case QueryActivityHumans:
		return models.UserAccountTypeHuman
	case QueryActivityServices:
		return models.UserAccountTypeService
	default:
		return ""
	}
}

func activityFilters(ctx context.Context, db store.Store, req QueryActivityRequest) (QueryActivityFilters, error) {
	f := QueryActivityFilters{Account: req.Account}
	if req.SourceID != 0 {
		id := req.SourceID
		f.SourceID = &id
		source, err := db.GetSource(ctx, id)
		switch {
		case err == nil:
			f.SourceName = source.Name
		case !errors.Is(err, models.ErrNotFound):
			return f, err
		}
	}
	if req.UserID != 0 {
		id := req.UserID
		f.UserID = &id
		user, err := db.GetUser(ctx, id)
		switch {
		case err == nil:
			f.UserName = user.FullName
			if f.UserName == "" {
				f.UserName = user.Email
			}
		case !errors.Is(err, models.ErrNotFound):
			return f, err
		}
	}
	return f, nil
}

func accountTotals(ctx context.Context, db store.Store, filter models.QueryStatsFilter) (AccountQueryTotals, error) {
	counts, err := db.QueryCountsByAccountType(ctx, filter)
	if err != nil {
		return AccountQueryTotals{}, err
	}
	var totals AccountQueryTotals
	for _, c := range counts {
		totals.All += c.QueryCount
		switch c.AccountType {
		case models.UserAccountTypeHuman:
			totals.Human = c.QueryCount
		case models.UserAccountTypeService:
			totals.Service = c.QueryCount
		}
	}
	return totals, nil
}

func totalUsers(ctx context.Context, db store.Store, accountType models.UserAccountType) (int64, error) {
	counts, err := db.CountActiveUsersByAccountType(ctx)
	if err != nil {
		return 0, err
	}
	if accountType != "" {
		return counts[accountType], nil
	}
	var total int64
	for _, n := range counts {
		total += n
	}
	return total, nil
}

// dailySeries returns every day of the window that ends today, filling idle
// days with zeros.
func dailySeries(now time.Time, w DateWindow, days []models.DailyQueryStat) []models.DailyQueryStat {
	byDate := make(map[string]models.DailyQueryStat, len(days))
	for _, d := range days {
		byDate[d.Date] = d
	}
	since := now.UTC().AddDate(0, 0, -(w.Days - 1))
	out := make([]models.DailyQueryStat, w.Days)
	for i := range out {
		date := since.AddDate(0, 0, i).Format(time.DateOnly)
		out[i] = models.DailyQueryStat{Date: date}
		if d, ok := byDate[date]; ok {
			out[i] = d
		}
	}
	return out
}

func avgDailyActive(w DateWindow, days []models.DailyQueryStat) float64 {
	var sum int64
	for _, d := range days {
		if d.Date >= w.Since && d.Date <= w.Until {
			sum += d.ActiveUsers
		}
	}
	return float64(sum) / float64(w.Days)
}

func serviceSummary(ctx context.Context, db store.Store, filter models.QueryStatsFilter) (*ServiceAccountSummary, error) {
	filter.AccountType = models.UserAccountTypeService
	summary, err := db.QueryStatsSummary(ctx, filter)
	if err != nil {
		return nil, err
	}
	if summary.Current.QueryCount == 0 {
		return nil, nil
	}
	top, err := db.QueryStatsByUser(ctx, filter, queryActivityTopServices)
	if err != nil {
		return nil, err
	}
	return &ServiceAccountSummary{AccountCount: summary.Current.ActiveUsers, QueryCount: summary.Current.QueryCount, Top: top}, nil
}

// shapeWindow clamps the shape reads to the days the shape rollup keeps,
// the same days a schema analysis may read. priorKept is false when the
// prior window starts before them.
func shapeWindow(now time.Time, window, prior DateWindow, sourceID models.SourceID) (f models.QueryShapeFilter, priorKept bool) {
	kept := now.UTC().AddDate(0, 0, -(maxSchemaAdviceWindowDays - 1)).Format(time.DateOnly)
	f = models.QueryShapeFilter{Since: max(window.Since, kept), Until: window.Until, SourceID: sourceID}
	priorKept = prior.Since >= kept
	f.PriorSince = f.Since
	if priorKept {
		f.PriorSince = prior.Since
	}
	return f, priorKept
}

// openFindings returns the undismissed findings of the ClickHouse sources
// among the rows, by source.
func openFindings(ctx context.Context, db store.Store, sources []models.SourceQueryStat, shapes []models.QueryShapeStat) (map[models.SourceID][]models.SchemaFinding, error) {
	var ids []models.SourceID
	add := func(id models.SourceID, sourceType string) {
		if sourceType == string(models.SourceTypeClickHouse) && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, s := range sources {
		add(s.SourceID, s.SourceType)
	}
	for i := range shapes {
		add(shapes[i].SourceID, shapes[i].SourceType)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	reports, err := db.ListSchemaAdvisorReports(ctx, ids)
	if err != nil {
		return nil, err
	}
	dismissals, err := db.ListSchemaAdvisorDismissalsBySources(ctx, ids)
	if err != nil {
		return nil, err
	}
	dismissed := make(map[models.SourceID]map[string]bool)
	for _, d := range dismissals {
		if dismissed[d.SourceID] == nil {
			dismissed[d.SourceID] = make(map[string]bool)
		}
		dismissed[d.SourceID][d.FindingID] = true
	}
	out := make(map[models.SourceID][]models.SchemaFinding, len(reports))
	for _, r := range reports {
		open := []models.SchemaFinding{}
		for i := range r.Report.Findings {
			if !dismissed[r.SourceID][r.Report.Findings[i].ID] {
				open = append(open, r.Report.Findings[i])
			}
		}
		out[r.SourceID] = open
	}
	return out, nil
}

func sourceActivity(sources []models.SourceQueryStat, findings map[models.SourceID][]models.SchemaFinding) []SourceActivity {
	out := make([]SourceActivity, len(sources))
	for i, s := range sources {
		out[i] = SourceActivity{SourceQueryStat: s}
		if s.SourceType != string(models.SourceTypeClickHouse) {
			continue
		}
		open, analyzed := findings[s.SourceID]
		counts := &AdvisorFindingCounts{Analyzed: analyzed}
		for i := range open {
			switch open[i].Impact {
			case models.ImpactHigh:
				counts.High++
			case models.ImpactMedium:
				counts.Medium++
			case models.ImpactLow:
				counts.Low++
			}
		}
		out[i].Advisor = counts
	}
	return out
}

var impactRank = map[string]int{models.ImpactHigh: 3, models.ImpactMedium: 2, models.ImpactLow: 1}

func shapeRows(shapes []models.QueryShapeStat, findings map[models.SourceID][]models.SchemaFinding, priorKept bool) []ShapeActivityRow {
	out := make([]ShapeActivityRow, len(shapes))
	for i := range shapes {
		s := shapes[i]
		if !priorKept {
			s.Prior = nil
		}
		out[i] = ShapeActivityRow{QueryShapeStat: s, Finding: shapeFinding(s.Shape, findings[s.SourceID])}
	}
	return out
}

// shapeFinding returns the most impactful finding on a column or path the
// shape filters on, or nil.
func shapeFinding(shape models.FilterShape, findings []models.SchemaFinding) *ShapeFinding {
	var best *models.SchemaFinding
	for i := range findings {
		f := &findings[i]
		if f.Column == "" || !slices.ContainsFunc(shape, func(item models.FilterItem) bool {
			return item.Column == f.Column && slices.Equal(item.Path, f.Path)
		}) {
			continue
		}
		if best == nil || impactRank[f.Impact] > impactRank[best.Impact] {
			best = f
		}
	}
	if best == nil {
		return nil
	}
	return &ShapeFinding{ID: best.ID, Title: best.Title, Impact: best.Impact}
}
