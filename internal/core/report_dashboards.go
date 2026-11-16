package core

// Dashboard reports (RFC 0008 units R6 and R7): load the dashboard, select
// the tab, authorize each panel for the owner and run it. A stat becomes a
// tile, a table a table, and a chart, toplist or pie a bar table.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/pkg/models"
)

// errNoDashboardPanels means the selected tab, or the whole dashboard,
// holds no panel to send.
var errNoDashboardPanels = errors.New("the dashboard has no panels to send")

// reportDashboardPanels reads the panels a report on d sends. A blob that
// no longer validates, or a tab that is gone, is an error that names the
// fault.
func reportDashboardPanels(d *models.Dashboard, tabID string) (*models.DashboardView, []models.DashboardViewPanel, error) {
	view, err := models.ReadDashboardView(d.PanelsJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("dashboard %d: %w", d.ID, err)
	}
	panels, err := reports.SelectDashboardPanels(view, tabID)
	if err != nil {
		return nil, nil, err
	}
	if len(panels) == 0 {
		return nil, nil, errNoDashboardPanels
	}
	return view, panels, nil
}

// dashboardReportRange is the report's range, else the dashboard's
// default, else 24h.
func dashboardReportRange(timeRange string, view *models.DashboardView) string {
	return cmp.Or(timeRange, view.DefaultTimeRange, reports.DefaultDashboardRange)
}

func (rr *ReportRunner) planDashboard(ctx context.Context, r *models.Report, owner *models.User, loc *time.Location, at time.Time) (*reportPlan, *runStop, error) {
	d, err := rr.DB.GetDashboard(ctx, *r.DashboardID)
	if models.IsNotFound(err) {
		return nil, stop(models.ReportRunFailed, models.ReportReasonSourceInvalid), nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("loading dashboard %d: %w", *r.DashboardID, err)
	}
	view, panels, err := reportDashboardPanels(d, r.TabID)
	if err != nil {
		return nil, sourceInvalid(err), nil
	}
	canView, err := UserCanViewDashboard(ctx, rr.DB, owner, d)
	if err != nil {
		return nil, nil, fmt.Errorf("checking the owner's view of dashboard %d: %w", d.ID, err)
	}
	if !canView {
		return nil, stop(models.ReportRunFailed, models.ReportReasonNoAccess), nil
	}
	dayStart := r.RangeAnchor == models.ReportAnchorDayStart
	window, err := reports.ResolveWindow(dashboardReportRange(r.TimeRange, view), dayStart, loc, at)
	if err != nil {
		return nil, sourceInvalid(err), nil
	}

	plan := &reportPlan{report: r, owner: owner, loc: loc, window: window}
	if rr.UIURL != "" {
		plan.sourceURL = fmt.Sprintf("%s/dashboards/%d", rr.UIURL, d.ID)
		if r.TabID != "" {
			plan.sourceURL += "?tab=" + url.QueryEscape(r.TabID)
		}
	}
	named := map[models.SourceID]bool{}
	locked := 0
	for i := range panels {
		p := &panels[i]
		if !named[p.SourceID] {
			named[p.SourceID] = true
			source, err := rr.DB.GetSource(ctx, p.SourceID)
			if err != nil && !models.IsNotFound(err) {
				return nil, nil, fmt.Errorf("loading source %d: %w", p.SourceID, err)
			}
			if err == nil {
				plan.sources = append(plan.sources, source.Name)
			}
		}
		src, ok, err := authorizeReportSource(ctx, rr.DB, owner, p.TeamID, p.SourceID)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			plan.panels = append(plan.panels, lockedPanel(p.ID, p.Title, p.TeamID, p.SourceID))
			locked++
			continue
		}
		planned := plannedPanel{teamID: p.TeamID, sourceID: p.SourceID}
		if reports.Renders(p) {
			planned.job = rr.dashboardPanelJob(src, *p, window, dayStart, loc, at, r.MaxRows)
		} else {
			planned.fixed = reports.Panel{ID: p.ID, Title: p.Title, Unsupported: true}
			planned.status = models.ReportPanelUnsupported
		}
		plan.panels = append(plan.panels, planned)
	}
	if locked == len(panels) {
		return nil, stop(models.ReportRunFailed, models.ReportReasonNoAccess), nil
	}
	return plan, nil, nil
}

// dashboardPanelJob runs one panel as the dashboard view would, at the
// report's window, and renders it.
func (rr *ReportRunner) dashboardPanelJob(src access.AuthorizedSource, p models.DashboardViewPanel, report reports.Window, dayStart bool, loc *time.Location, at time.Time, maxRows int) *reports.PanelJob {
	return &reports.PanelJob{ID: p.ID, Title: p.Title, Run: func(ctx context.Context) (reports.PanelOutput, error) {
		w, err := reports.PanelWindow(&p.Query, report, dayStart, loc, at)
		if err != nil {
			return reports.PanelOutput{}, err
		}
		var out reports.PanelOutput
		switch {
		case p.Query.Mode == models.DashboardQueryModeCount && p.Viz.Type == models.DashboardVizStat:
			out, err = rr.runCountStat(ctx, src, &p, w, loc)
		case p.Query.Mode == models.DashboardQueryModeCount:
			out, err = rr.runCountBars(ctx, src, &p, w, loc)
		default:
			out, err = rr.runResultPanel(ctx, src, &p, w, loc, maxRows)
		}
		if err != nil {
			return reports.PanelOutput{}, err
		}
		out.Panel.ID, out.Panel.Title = p.ID, p.Title
		out.Panel.Note = reports.PanelNote(&p.Query, w, loc)
		return out, nil
	}}
}

// runResultPanel runs a result query and renders it as a stat tile or a
// table.
func (rr *ReportRunner) runResultPanel(ctx context.Context, src access.AuthorizedSource, p *models.DashboardViewPanel, w reports.Window, loc *time.Location, maxRows int) (reports.PanelOutput, error) {
	cfg, timeout := rr.reportQueryConfig()
	q := &p.Query
	start, end := w.Start.UTC().Format(time.RFC3339), w.End.UTC().Format(time.RFC3339)
	interval := reports.ResolveInterval(q.Interval, w)
	limit := reports.ResultLimit(q.Limit, maxReportQueryRows)
	var (
		res     *models.QueryResult
		untyped bool
		err     error
	)
	if q.Language == models.QueryLanguageLogchefQL {
		var prepared *PreparedLogchefQL
		prepared, res, err = RunLogchefQL(ctx, rr.DS, cfg, src, LogchefQLQueryRequest{
			Query: q.Text, StartTime: start, EndTime: end, Timezone: loc.String(), Limit: limit,
			QueryTimeout: &timeout, Interval: interval, AllowAggregation: true,
		})
		// LogsQL reports no column types, except for an aggregation, whose
		// columns the compiler declares.
		untyped = err == nil && prepared.Compiled.Language == models.QueryLanguageLogsQL && prepared.Compiled.Aggregation == nil
	} else {
		res, err = RunQuery(ctx, rr.DS, cfg, src, models.APIQueryRequest{
			QueryText: q.Text, StartTime: start, EndTime: end, Timezone: loc.String(), Limit: limit,
			QueryTimeout: &timeout, Interval: interval,
		})
		untyped = q.Language == models.QueryLanguageLogsQL
	}
	if err != nil {
		return reports.PanelOutput{}, err
	}
	v := &p.Viz
	switch v.Type {
	case models.DashboardVizTable:
		return tableOutput(p.ID, p.Title, reports.VisibleColumns(res, v.VisibleColumns), loc, maxRows), nil
	case models.DashboardVizChart:
		bars, err := reports.ResultChartBars(res, reports.ChartOptions{
			X: v.X, Y: v.Y, Series: v.Series, XScale: v.XScale, Format: v.Format, Columns: v.Columns, Untyped: untyped,
		}, loc)
		return barsOutput(bars, len(res.Logs), res.Stats.Truncation), err
	case models.DashboardVizToplist, models.DashboardVizPie:
		bars, err := reports.ResultListBars(res, reports.ListOptions{
			Pie: v.Type == models.DashboardVizPie, Label: v.Label, Value: v.Value, MaxRows: v.MaxRows,
			MinSlicePercent: v.MinSlicePercent, Format: v.Format, Columns: v.Columns, Untyped: untyped,
		}, loc)
		return barsOutput(bars, len(res.Logs), res.Stats.Truncation), err
	case models.DashboardVizStat:
	}
	n, label, err := reports.ReduceStat(res, reports.StatOptions{
		Value: v.Value, Reduce: cmp.Or(v.Reduce, models.DashboardStatReduceLast), OrderBy: v.OrderBy,
		Columns: v.Columns, Untyped: untyped,
	}, loc)
	if err != nil {
		return reports.PanelOutput{}, err
	}
	tile := reports.FormatTile(n, label, v.Format)
	tile.Rows, tile.Truncation = len(res.Logs), res.Stats.Truncation
	return reports.PanelOutput{
		Panel: reports.Panel{Stat: &tile},
		Rows:  len(res.Logs), Truncation: res.Stats.Truncation, Empty: len(res.Logs) == 0,
	}, nil
}

// countHistogram is a count panel's histogram: its buckets, the window
// they use and whether they keep the window start's zone offset, as
// VictoriaLogs buckets do. A LogchefQL panel compiles to the source's
// native query first, as the dashboard view does. window picks the
// histogram window once the query language is known.
type countHistogram struct {
	data        []datasource.HistogramBucket
	granularity string
	fixedOffset bool
}

func (rr *ReportRunner) runCountHistogram(ctx context.Context, src access.AuthorizedSource, q *models.DashboardViewQuery, w reports.Window, loc *time.Location, window func(fixedOffset bool) string) (countHistogram, error) {
	_, timeout := rr.reportQueryConfig()
	start, end := w.Start.UTC().Format(time.RFC3339), w.End.UTC().Format(time.RFC3339)
	text, language := q.Text, q.Language
	if language == models.QueryLanguageLogchefQL {
		compiled, err := CompileLogchefQL(ctx, rr.DS, src, datasource.LogchefQLCompileRequest{
			Query: q.Text, StartTime: start, EndTime: end, Timezone: loc.String(), Limit: q.TranslateLimit,
		})
		if err != nil {
			return countHistogram{}, err
		}
		text, language = compiled.Query, compiled.Language
	}
	fixedOffset := language == models.QueryLanguageLogsQL
	interval := window(fixedOffset)
	hist, err := RunHistogram(ctx, rr.DS, src, models.APIHistogramRequest{
		QueryText: text, Window: interval, GroupBy: q.GroupBy,
		StartTime: start, EndTime: end, Timezone: loc.String(), QueryTimeout: &timeout,
	})
	if err != nil {
		return countHistogram{}, err
	}
	return countHistogram{data: hist.Data, granularity: cmp.Or(hist.Granularity, interval), fixedOffset: fixedOffset}, nil
}

// runCountStat runs a count panel through the histogram path and reduces
// its buckets.
func (rr *ReportRunner) runCountStat(ctx context.Context, src access.AuthorizedSource, p *models.DashboardViewPanel, w reports.Window, loc *time.Location) (reports.PanelOutput, error) {
	hist, err := rr.runCountHistogram(ctx, src, &p.Query, w, loc, func(bool) string { return reports.ResolveInterval(p.Query.Interval, w) })
	if err != nil {
		return reports.PanelOutput{}, err
	}
	buckets := make([]reports.CountRow, len(hist.data))
	for i, b := range hist.data {
		buckets[i] = reports.CountRow{At: b.Bucket, Count: int64(b.LogCount)}
	}
	rows, err := reports.CountRows(buckets, hist.granularity, w, loc, hist.fixedOffset)
	if err != nil {
		return reports.PanelOutput{}, err
	}
	n, label := reports.ReduceCount(rows, cmp.Or(p.Viz.Reduce, models.DashboardStatReduceSum), loc)
	tile := reports.FormatTile(&n, label, p.Viz.Format)
	return reports.PanelOutput{Panel: reports.Panel{Stat: &tile}, Rows: len(hist.data), Empty: len(hist.data) == 0}, nil
}

// runCountBars runs a count chart, toplist or pie. A chart asks for the
// window that gives at most reports.MaxBars bars; a toplist or a pie adds
// up every bucket, so it keeps the panel's interval.
func (rr *ReportRunner) runCountBars(ctx context.Context, src access.AuthorizedSource, p *models.DashboardViewPanel, w reports.Window, loc *time.Location) (reports.PanelOutput, error) {
	chart := p.Viz.Type == models.DashboardVizChart
	hist, err := rr.runCountHistogram(ctx, src, &p.Query, w, loc, func(fixedOffset bool) string {
		if chart {
			return reports.BarWindow(p.Query.Interval, w, loc, fixedOffset)
		}
		return reports.ResolveInterval(p.Query.Interval, w)
	})
	if err != nil {
		return reports.PanelOutput{}, err
	}
	buckets := make([]reports.SeriesBucket, len(hist.data))
	for i, b := range hist.data {
		buckets[i] = reports.SeriesBucket{At: b.Bucket, Count: int64(b.LogCount), Series: reports.CountSeries{Value: b.GroupValue, Null: b.IsNull, Other: b.IsOther}}
	}
	var bars *reports.Bars
	if chart {
		bars, err = reports.CountChartBars(buckets, p.Query.GroupBy != "", hist.granularity, w, loc, hist.fixedOffset, p.Viz.Format)
	} else {
		bars, err = reports.CountListBars(buckets, reports.ListOptions{Pie: p.Viz.Type == models.DashboardVizPie, MaxRows: p.Viz.MaxRows, MinSlicePercent: p.Viz.MinSlicePercent, Format: p.Viz.Format})
	}
	return barsOutput(bars, len(hist.data), models.TruncationNone), err
}

// barsOutput is a bar panel's output. An error leaves it unused.
func barsOutput(bars *reports.Bars, rows int, truncation models.Truncation) reports.PanelOutput {
	if bars == nil {
		return reports.PanelOutput{}
	}
	return reports.PanelOutput{
		Panel: reports.Panel{Bars: bars},
		Rows:  rows, Truncation: truncation, Empty: len(bars.Blocks) == 0,
	}
}
