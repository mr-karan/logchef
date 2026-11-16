package reports

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

// DefaultDashboardRange is the range of a report on a dashboard that sets
// none.
const DefaultDashboardRange = "24h"

// defaultTableLimit is the row limit of a result query that sets none, as
// the dashboard view runs it.
const defaultTableLimit = 1000

// ErrNoSuchTab means the report's tab_id names no tab of the dashboard.
var ErrNoSuchTab = errors.New("the dashboard has no such tab")

// SelectDashboardPanels returns the panels a report sends: the tab's, or
// every tab's in tab order when tabID is "".
func SelectDashboardPanels(view *models.DashboardView, tabID string) ([]models.DashboardViewPanel, error) {
	var out []models.DashboardViewPanel
	for _, tab := range view.Tabs {
		if tabID == "" || tab.ID == tabID {
			out = append(out, tab.Panels...)
		}
	}
	if tabID != "" && !slices.ContainsFunc(view.Tabs, func(t models.DashboardViewTab) bool { return t.ID == tabID }) {
		return nil, fmt.Errorf("%w: %q", ErrNoSuchTab, tabID)
	}
	return out, nil
}

// Renders reports whether this release renders the panel's visualization.
// Charts, toplists and pies are unsupported until RFC 0008 unit R7, and a
// LogQL panel until Loki sources declare their capabilities.
func Renders(p *models.DashboardViewPanel) bool {
	if p.Query.Language == models.QueryLanguageLogQL {
		return false
	}
	switch p.Viz.Type {
	case models.DashboardVizStat:
		return true
	case models.DashboardVizTable:
		return p.Query.Mode == models.DashboardQueryModeResult
	default:
		return false
	}
}

// FixedWindow reports a ClickHouse SQL panel that takes no time macro: its
// SQL sets the window, and the email says so, as the dashboard chip does.
func FixedWindow(q *models.DashboardViewQuery) bool {
	return q.Language == models.QueryLanguageClickHouseSQL && !template.UsesTimeRange(q.Text)
}

// PanelWindow is the window a panel queries: its own range when it sets
// one, at the report's anchor, else the report window. A calendar range
// (today, yesterday, today-<n>d) is already anchored to days, so it resolves
// at the run's time with either anchor.
func PanelWindow(q *models.DashboardViewQuery, report Window, dayStart bool, loc *time.Location, at time.Time) (Window, error) {
	if q.Range == "" {
		return report, nil
	}
	rolling := rangeDuration.MatchString(q.Range)
	return ResolveWindow(q.Range, dayStart && rolling, loc, at)
}

// intervalWindows are the histogram windows in seconds, shortest first.
var intervalWindows = []struct {
	window  string
	seconds int64
}{
	{"1s", 1}, {"5s", 5}, {"10s", 10}, {"15s", 15}, {"30s", 30}, {"1m", 60}, {"5m", 300},
	{"10m", 600}, {"15m", 900}, {"30m", 1800}, {"1h", 3600}, {"2h", 7200}, {"3h", 10800},
	{"6h", 21600}, {"12h", 43200}, {"24h", 86400},
}

const (
	// autoTargetBuckets is the bucket count the auto interval aims for.
	autoTargetBuckets = 100
	// maxIntervalBuckets is the most buckets a fixed interval may give.
	maxIntervalBuckets = 1000
)

// ResolveInterval is the window a panel queries with, as the dashboard view
// resolves it (resolveInterval in frontend/src/utils/dashboardRuntime.ts):
// auto picks the smallest window that gives at most about 100 buckets, and a
// fixed window that gives more than 1,000 buckets grows until it fits.
func ResolveInterval(interval string, w Window) string {
	span := w.End.Sub(w.Start).Seconds()
	if interval == "" || interval == models.DashboardQueryIntervalAuto {
		for _, iw := range intervalWindows {
			if float64(iw.seconds) >= span/autoTargetBuckets {
				return iw.window
			}
		}
		return intervalWindows[len(intervalWindows)-1].window
	}
	chosen := slices.IndexFunc(intervalWindows, func(iw struct {
		window  string
		seconds int64
	}) bool {
		return iw.window == interval
	})
	for _, iw := range intervalWindows[max(chosen, 0):] {
		if span/float64(iw.seconds) <= maxIntervalBuckets {
			return iw.window
		}
	}
	return intervalWindows[len(intervalWindows)-1].window
}

// ResultLimit is the row limit of a result query: the panel's, else the
// dashboard's default, at most maxRows.
func ResultLimit(limit, maxRows int) int {
	if limit <= 0 {
		limit = defaultTableLimit
	}
	return min(limit, maxRows)
}

func windowStep(window string) (time.Duration, error) {
	for _, iw := range intervalWindows {
		if iw.window == window {
			return time.Duration(iw.seconds) * time.Second, nil
		}
	}
	return 0, fmt.Errorf("histogram granularity %q is not a known window", window)
}

// maxCountBuckets bounds the buckets a count frame fills.
const maxCountBuckets = 5000

// CountRows is a histogram, one row per bucket and series, as the stat of
// a count panel reads it: one row per bucket with the count of every series
// added up, the empty buckets of
// the window filled with zero, in time order (countFrame and fillBucketGaps
// in the frontend). VictoriaLogs buckets keep the offset of the window start
// across a daylight-saving change, so fixedOffset aligns them that way.
func CountRows(buckets []CountRow, granularity string, w Window, loc *time.Location, fixedOffset bool) ([]CountRow, error) {
	if len(buckets) == 0 {
		return nil, nil
	}
	step, err := windowStep(granularity)
	if err != nil {
		return nil, err
	}
	totals := map[int64]int64{}
	for _, b := range buckets {
		totals[b.At.UnixMilli()] += b.Count
	}
	bounds := bucketBounds(w, step, loc, fixedOffset)
	if (bounds.last-bounds.first)/step.Milliseconds() > maxCountBuckets {
		return nil, errors.New("too many count buckets; choose a shorter range")
	}
	var rows []CountRow
	for ts := bounds.first; ts <= bounds.last; ts = bounds.next(ts) {
		if len(rows) > maxCountBuckets {
			return nil, errors.New("too many count buckets; choose a shorter range")
		}
		rows = append(rows, CountRow{At: time.UnixMilli(ts), Count: totals[ts]})
		delete(totals, ts)
	}
	// A bucket off the grid is kept, never dropped.
	for ts, n := range totals {
		rows = append(rows, CountRow{At: time.UnixMilli(ts), Count: n})
	}
	slices.SortFunc(rows, func(a, b CountRow) int { return a.At.Compare(b.At) })
	return rows, nil
}

// CountRow is one bucket of a count panel.
type CountRow struct {
	At    time.Time
	Count int64
}

type bounds struct {
	first, last int64
	next        func(int64) int64
}

// bucketBounds ports histogramBucketBounds: ClickHouse buckets start on the
// local wall clock, except 10m, 15m and 30m, which start on the epoch.
func bucketBounds(w Window, step time.Duration, loc *time.Location, fixedOffset bool) bounds {
	stepMs := step.Milliseconds()
	plus := func(ts int64) int64 { return ts + stepMs }
	if fixedOffset {
		_, offset := w.Start.In(loc).Zone()
		off := int64(offset) * 1000
		align := func(ts int64) int64 { return floorDiv(ts+off, stepMs)*stepMs - off }
		return bounds{first: align(w.Start.UnixMilli()), last: align(w.End.UnixMilli()), next: plus}
	}
	hours := int(step / time.Hour)
	align := func(ts int64) int64 {
		if step == 10*time.Minute || step == 15*time.Minute || step == 30*time.Minute {
			return floorDiv(ts, stepMs) * stepMs
		}
		t := time.UnixMilli(ts).In(loc)
		if step > time.Hour {
			y, m, d := t.Date()
			return wallHour(y, m, d, t.Hour()/hours*hours, loc).UnixMilli()
		}
		within := int64(t.Minute())*60_000 + int64(t.Second())*1000 + int64(t.Nanosecond()/1e6)
		return ts - within%stepMs
	}
	next := plus
	if step > time.Hour {
		next = func(ts int64) int64 {
			t := time.UnixMilli(ts).In(loc)
			y, m, d := t.Date()
			// A repeated hour can map the next wall time onto ts itself;
			// the grid always moves forward.
			return max(wallHour(y, m, d, t.Hour()+hours-t.Hour()%hours, loc).UnixMilli(), ts+time.Hour.Milliseconds())
		}
	}
	return bounds{first: align(w.Start.UnixMilli()), last: align(w.End.UnixMilli()), next: next}
}

// wallHour is the instant of a local wall-clock hour. An hour that a
// daylight-saving change skips moves forward by the skipped length, as the
// frontend's toZoned does; Go's time.Date would move it back.
func wallHour(y int, m time.Month, d, hour int, loc *time.Location) time.Time {
	t := time.Date(y, m, d, hour, 0, 0, 0, loc)
	want := time.Date(y, m, d, hour, 0, 0, 0, time.UTC)
	if wall := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC); wall.Before(want) {
		return t.Add(want.Sub(wall))
	}
	return t
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// ReduceCount reduces the rows of a count panel. Without rows the count is
// zero for every reducer; first and last name the bucket they select.
func ReduceCount(rows []CountRow, reduce models.DashboardStatReduce, loc *time.Location) (value Number, rowLabel string) {
	if len(rows) == 0 {
		return exactNumber(exact{coef: new(big.Int)}), ""
	}
	switch reduce {
	case models.DashboardStatReduceFirst:
		return countNumber(rows[0].Count), formatInstant(rows[0].At, loc)
	case models.DashboardStatReduceLast:
		last := rows[len(rows)-1]
		return countNumber(last.Count), formatInstant(last.At, loc)
	default:
	}
	values := make([]Number, len(rows))
	for i, r := range rows {
		values[i] = countNumber(r.Count)
	}
	n, _ := reduceNumbers(values, reduce)
	return *n, ""
}

func countNumber(n int64) Number {
	e, _ := parseExact(strconv.FormatInt(n, 10))
	return exactNumber(e)
}

// FormatTile prints a reduced stat. A nil value prints "No data".
func FormatTile(n *Number, rowLabel string, f models.DashboardNumberFormat) Tile {
	if n == nil {
		return Tile{RowLabel: rowLabel}
	}
	return Tile{Value: FormatNumber(*n, f), RowLabel: rowLabel}
}

// VisibleColumns limits a table to the columns a table panel lists, in that
// order. A listed column the result lacks prints as NULL, as on the
// dashboard.
func VisibleColumns(res *models.QueryResult, visible []string) *models.QueryResult {
	if len(visible) == 0 {
		return res
	}
	types := make(map[string]string, len(res.Columns))
	for _, c := range res.Columns {
		types[c.Name] = c.Type
	}
	out := *res
	out.Columns = make([]models.ColumnInfo, len(visible))
	for i, name := range visible {
		out.Columns[i] = models.ColumnInfo{Name: name, Type: types[name]}
	}
	return &out
}

// PanelNote qualifies a panel's data in its header: a fixed window, or the
// panel's own range.
func PanelNote(q *models.DashboardViewQuery, w Window, loc *time.Location) string {
	switch {
	case FixedWindow(q):
		return "Fixed window: the query sets its own time range"
	case q.Range != "":
		return fmt.Sprintf("Range %s: %s to %s", q.Range, formatWindowEdge(w.Start, loc), formatWindowEdge(w.End, loc))
	}
	return ""
}
