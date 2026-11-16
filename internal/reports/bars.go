package reports

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// This file ports buildPanelChart and buildPanelList of
// frontend/src/utils/panelCharts.ts to the bar table email shows for a
// chart, a toplist and a pie: one row per x value or label with the
// formatted value and a bar whose width is the share of the largest value.

const (
	// MaxBars bounds the bars of a count chart, because 24 bars fit an email.
	MaxBars = 24
	// maxSeries bounds the blocks of a chart; the rest print as a count.
	maxSeries          = 5
	maxPieSlices       = 50
	defaultToplistRows = 20
)

var bigHundred = big.NewInt(100)

// Bars is a chart, toplist or pie panel as a bar table.
type Bars struct {
	// Blocks holds one block per series. No block means no data.
	Blocks []BarBlock
	// MoreSeries counts the series left out past the first five.
	MoreSeries int
	// Percent shows each row's share of the total, for a pie.
	Percent bool
	// Notes qualify the data, as the dashboard's panel notices do.
	Notes []string
	// Rows and Truncation describe the result the bars came from, so a
	// cut result prints its label.
	Rows       int
	Truncation models.Truncation
}

// BarBlock is one series. Title is "" when the panel has one series.
type BarBlock struct {
	Title string
	Rows  []BarRow
}

// BarRow is one bar. Width is the share of the largest value on the
// block's scale, 0 to 100.
type BarRow struct {
	Label   string
	Value   string
	Percent string
	Width   int
}

// CountSeries names the series of one count bucket: a group value, the
// NULL group, or the remainder that the histogram folds past its top
// series.
type CountSeries struct {
	Value string
	Null  bool
	Other bool
}

func (s CountSeries) label() string {
	switch {
	case s.Other:
		return "Other"
	case s.Null:
		return "(null)"
	case s.Value == "":
		return "(empty)"
	}
	return s.Value
}

// SeriesBucket is one bucket of a count panel and one series.
type SeriesBucket struct {
	At     time.Time
	Count  int64
	Series CountSeries
}

// barGrid is the bucket grid of a count chart: bucketGrid without the
// bucket that starts at or after the window end, which holds no row of
// the window.
func barGrid(w Window, step time.Duration, loc *time.Location, fixedOffset bool) ([]int64, error) {
	grid, err := bucketGrid(w, step, loc, fixedOffset)
	if err != nil {
		return nil, err
	}
	end := w.End.UnixMilli()
	for len(grid) > 1 && grid[len(grid)-1] >= end {
		grid = grid[:len(grid)-1]
	}
	return grid, nil
}

// BarWindow is the histogram window of a count chart: the smallest
// window, no shorter than the panel's own interval, that gives at most
// MaxBars buckets over w. A range above 24 days has none and takes the
// largest window; CountChartBars then adds adjacent buckets together.
func BarWindow(interval string, w Window, loc *time.Location, fixedOffset bool) string {
	from := 0
	if interval != "" && interval != models.DashboardQueryIntervalAuto {
		resolved := ResolveInterval(interval, w)
		from = slices.IndexFunc(intervalWindows, func(iw struct {
			window  string
			seconds int64
		}) bool {
			return iw.window == resolved
		})
	}
	for _, iw := range intervalWindows[from:] {
		grid, err := barGrid(w, time.Duration(iw.seconds)*time.Second, loc, fixedOffset)
		if err == nil && len(grid) <= MaxBars {
			return iw.window
		}
	}
	return intervalWindows[len(intervalWindows)-1].window
}

// CountChartBars draws a count chart: one block per series, the empty
// buckets filled with zero, at most MaxBars bars. Past MaxBars buckets,
// adjacent buckets add up into bars of equal size. Series rank by their
// total, the histogram's remainder last, and the first five are shown.
func CountChartBars(buckets []SeriesBucket, grouped bool, granularity string, w Window, loc *time.Location, fixedOffset bool, f models.DashboardNumberFormat) (*Bars, error) {
	if len(buckets) == 0 {
		return &Bars{}, nil
	}
	step, err := windowStep(granularity)
	if err != nil {
		return nil, err
	}
	grid, err := barGrid(w, step, loc, fixedOffset)
	if err != nil {
		return nil, err
	}
	type series struct {
		key    CountSeries
		total  int64
		counts map[int64]int64
	}
	byKey := map[CountSeries]*series{}
	var order []*series
	onGrid := make(map[int64]bool, len(grid))
	for _, ts := range grid {
		onGrid[ts] = true
	}
	times := slices.Clone(grid)
	for _, b := range buckets {
		key := b.Series
		if !grouped {
			key = CountSeries{}
		}
		s := byKey[key]
		if s == nil {
			s = &series{key: key, counts: map[int64]int64{}}
			byKey[key] = s
			order = append(order, s)
		}
		ts := b.At.UnixMilli()
		s.counts[ts] += b.Count
		s.total += b.Count
		// A bucket off the grid is kept, never dropped.
		if !onGrid[ts] {
			onGrid[ts] = true
			times = append(times, ts)
		}
	}
	slices.Sort(times)
	ranked := slices.Clone(order)
	slices.SortStableFunc(ranked, func(a, b *series) int {
		if a.key.Other != b.key.Other {
			if a.key.Other {
				return 1
			}
			return -1
		}
		return cmp.Compare(b.total, a.total)
	})
	per := (len(times) + MaxBars - 1) / MaxBars
	out := &Bars{MoreSeries: max(len(ranked)-maxSeries, 0)}
	out.Notes = []string{"Each bar covers " + spanText(step*time.Duration(per))}
	var largest float64
	values := make([][]int64, 0, maxSeries)
	for _, s := range ranked[:min(len(ranked), maxSeries)] {
		block := BarBlock{}
		if grouped {
			block.Title = s.key.label()
		}
		var sums []int64
		for i := 0; i < len(times); i += per {
			var sum int64
			for _, ts := range times[i:min(i+per, len(times))] {
				sum += s.counts[ts]
			}
			sums = append(sums, sum)
			largest = max(largest, math.Abs(float64(sum)))
			block.Rows = append(block.Rows, BarRow{Label: formatWindowEdge(time.UnixMilli(times[i]), loc), Value: FormatNumber(countNumber(sum), f)})
		}
		values = append(values, sums)
		out.Blocks = append(out.Blocks, block)
	}
	for b, sums := range values {
		for i, sum := range sums {
			out.Blocks[b].Rows[i].Width = barWidth(float64(sum), largest)
		}
	}
	return out, nil
}

func spanText(d time.Duration) string {
	switch {
	case d == 24*time.Hour:
		return "1 day"
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%d days", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return fmt.Sprintf("%ds", d/time.Second)
}

// barWidth is a value's share of the largest value, in percent. A value
// that is not zero keeps a visible bar.
func barWidth(v, largest float64) int {
	if largest == 0 || v == 0 {
		return 0
	}
	return max(int(math.Round(math.Abs(v)/largest*100)), 1)
}

// coordinate is a number as the chart draws it. A value past the float
// range has no coordinate, as in the frontend's chartValue.
func coordinate(n Number) (float64, error) {
	if n.isFloat {
		return n.float, nil
	}
	f, err := strconv.ParseFloat(n.exact.text(-1), 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, errors.New("a value is outside the chart coordinate range; use a table to see its exact digits")
	}
	return f, nil
}

// barValue formats a chart value: a value below one in magnitude keeps
// enough decimals to show its first digits, as chartValue does.
func barValue(n Number, f models.DashboardNumberFormat) (text string, coord float64, err error) {
	c, err := coordinate(n)
	if err != nil {
		return "", 0, err
	}
	if f.Decimals == nil {
		scale := 1.0
		if f.Scale != nil {
			scale = *f.Scale
		}
		if m := math.Abs(c * scale); m > 0 && m < 1 {
			decimals := max(2, 1-int(math.Floor(math.Log10(m))))
			f.Decimals = &decimals
		}
	}
	return FormatNumber(n, f), c, nil
}

// identity keys a read cell, as identity() in panelCharts.ts.
func identity(v any) string {
	if n, ok := v.(Number); ok {
		if n.isFloat {
			return "number:" + strconv.FormatFloat(n.float, 'g', -1, 64)
		}
		return "number:" + n.exact.text(-1)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

// categoryLabel prints an x value or a list label.
func categoryLabel(v any, c frameColumn, loc *time.Location) string {
	if v == nil {
		return "(null)"
	}
	return cmp.Or(cellLabel(v, c, loc), "(empty)")
}

func requireColumn(cols []frameColumn, name string, numeric bool) (*frameColumn, error) {
	if name == "" {
		return nil, errors.New("select the visualization's column roles")
	}
	c := findColumn(cols, name)
	if c == nil {
		return nil, fmt.Errorf("column %q is missing from the result", name)
	}
	if numeric && c.kind != "number" {
		return nil, fmt.Errorf("column %q must be a number (backend type %s)", name, cmp.Or(c.backendType, "untyped"))
	}
	return c, nil
}

func duplicateKeys(keys map[string]int, first string, roles []string) error {
	duplicates := 0
	for _, n := range keys {
		if n > 1 {
			duplicates++
		}
	}
	if duplicates == 0 {
		return nil
	}
	quoted := make([]string, len(roles))
	for i, r := range roles {
		quoted[i] = strconv.Quote(r)
	}
	return fmt.Errorf("%d duplicate key(s). First key: %s. Group the query by %s", duplicates, first, strings.Join(quoted, " and "))
}

// ChartOptions is a result-mode chart's viz.
type ChartOptions struct {
	X      string
	Y      []string
	Series string
	// XScale is "time", "category" or "" for the column's kind.
	XScale string
	Format models.DashboardNumberFormat
	// Columns holds confirmed column kinds by name.
	Columns map[string]string
	// Untyped ignores the backend types: native LogsQL reports none.
	Untyped bool
}

// ResultChartBars draws a result-mode chart: one block per y column and
// series value, in the dashboard legend's order, rows in result order or
// in time order on a time axis. A row without a value for a series is
// left out of that series' block, where the chart leaves a gap.
func ResultChartBars(res *models.QueryResult, opts ChartOptions, loc *time.Location) (*Bars, error) {
	cols := frameColumns(res, opts.Columns, opts.Untyped)
	if len(res.Logs) == 0 && len(cols) == 0 {
		return &Bars{}, nil
	}
	roles, err := chartRolesOf(cols, opts)
	if err != nil {
		return nil, err
	}
	frame, err := readChart(res, roles, loc)
	if err != nil {
		return nil, err
	}
	out := &Bars{MoreSeries: max(len(frame.series)-maxSeries, 0), Rows: len(res.Logs), Truncation: res.Stats.Truncation}
	if frame.nullX > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d row(s) with NULL x values omitted from the time axis.", frame.nullX))
	}
	shown := frame.series[:min(len(frame.series), maxSeries)]
	largest := make([]float64, len(roles.ys))
	coords := make([][]float64, len(shown))
	for b, s := range shown {
		block := BarBlock{}
		if len(frame.series) > 1 {
			block.Title = s.label
		}
		for _, row := range frame.rows {
			n := row.values[s.key]
			if n == nil {
				continue
			}
			text, c, err := barValue(*n, opts.Format)
			if err != nil {
				return nil, err
			}
			coords[b] = append(coords[b], c)
			largest[s.y] = max(largest[s.y], math.Abs(c))
			block.Rows = append(block.Rows, BarRow{Label: row.label, Value: text})
		}
		out.Blocks = append(out.Blocks, block)
	}
	// Series of one y column share a scale; each y column has its own.
	for b, s := range shown {
		for i, c := range coords[b] {
			out.Blocks[b].Rows[i].Width = barWidth(c, largest[s.y])
		}
	}
	return out, nil
}

type chartRoles struct {
	x, group *frameColumn
	ys       []*frameColumn
	timeAxis bool
}

func chartRolesOf(cols []frameColumn, opts ChartOptions) (chartRoles, error) {
	var (
		r   chartRoles
		err error
	)
	if r.x, err = requireColumn(cols, opts.X, false); err != nil {
		return r, err
	}
	if len(opts.Y) == 0 {
		return r, errors.New("select at least one y column")
	}
	r.ys = make([]*frameColumn, len(opts.Y))
	for i, name := range opts.Y {
		if r.ys[i], err = requireColumn(cols, name, true); err != nil {
			return r, err
		}
	}
	if opts.Series != "" {
		if r.group, err = requireColumn(cols, opts.Series, false); err != nil {
			return r, err
		}
	}
	xTime := r.x.kind == "time" || r.x.kind == "date"
	r.timeAxis = opts.XScale == "time" || opts.XScale != "category" && xTime
	if r.timeAxis && !xTime {
		return r, fmt.Errorf("column %q must be a time or date for a time axis", r.x.name)
	}
	return r, nil
}

type chartRow struct {
	at     int64
	label  string
	values map[string]*Number
}

type chartSeries struct {
	key, label string
	y          int
}

type chartFrame struct {
	rows   []*chartRow
	series []chartSeries
	nullX  int
}

// readChart reads the result into x rows and series in the order they
// first appear, as buildPanelChart does, and refuses a repeated x and
// series pair.
func readChart(res *models.QueryResult, r chartRoles, loc *time.Location) (chartFrame, error) {
	var (
		f          chartFrame
		firstDup   string
		byRow      = map[string]*chartRow{}
		seenSeries = map[string]bool{}
		keys       = map[string]int{}
	)
	for _, raw := range res.Logs {
		xv := readCell(raw[r.x.name], *r.x)
		if r.timeAxis && xv == nil {
			f.nullX++
			continue
		}
		var gv any
		groupKey := ""
		if r.group != nil {
			gv = readCell(raw[r.group.name], *r.group)
			groupKey = identity(gv)
		}
		rowKey := identity(xv)
		key := rowKey + "\x00" + groupKey
		keys[key]++
		if keys[key] == 2 && firstDup == "" {
			firstDup = categoryLabel(xv, *r.x, loc)
			if r.group != nil {
				firstDup += ", " + categoryLabel(gv, *r.group, loc)
			}
		}
		row := byRow[rowKey]
		if row == nil {
			row = &chartRow{label: categoryLabel(xv, *r.x, loc), values: map[string]*Number{}}
			if r.timeAxis {
				row.at = cellMillis(xv)
			}
			byRow[rowKey] = row
			f.rows = append(f.rows, row)
		}
		for i, y := range r.ys {
			sk := y.name + "\x00" + groupKey
			if !seenSeries[sk] {
				seenSeries[sk] = true
				f.series = append(f.series, chartSeries{key: sk, label: seriesLabel(r, gv, y.name, loc), y: i})
			}
			row.values[sk] = nil
			if n, ok := readCell(raw[y.name], *y).(Number); ok {
				row.values[sk] = &n
			}
		}
	}
	roles := []string{r.x.name}
	if r.group != nil {
		roles = append(roles, r.group.name)
	}
	if err := duplicateKeys(keys, firstDup, roles); err != nil {
		return f, err
	}
	if r.timeAxis {
		slices.SortStableFunc(f.rows, func(a, b *chartRow) int { return cmp.Compare(a.at, b.at) })
	}
	return f, nil
}

func seriesLabel(r chartRoles, gv any, y string, loc *time.Location) string {
	if r.group == nil {
		return y
	}
	label := categoryLabel(gv, *r.group, loc)
	if len(r.ys) > 1 {
		label += " · " + y
	}
	return label
}

// cellMillis is the instant of a read time or date cell, in milliseconds.
func cellMillis(v any) int64 {
	s, _ := v.(string)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UnixMilli()
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.UnixMilli()
	}
	return 0
}

// ListOptions is a toplist's or a pie's viz.
type ListOptions struct {
	Pie   bool
	Label string
	Value string
	// MaxRows is a toplist's viz.max_rows; 0 means the default of 20.
	MaxRows int
	// MinSlicePercent folds a pie's smaller slices into Other; 0 folds none.
	MinSlicePercent float64
	Format          models.DashboardNumberFormat
	Columns         map[string]string
	Untyped         bool
}

type listEntry struct {
	label string
	value *Number
}

// ResultListBars draws a result-mode toplist or pie, in result order.
func ResultListBars(res *models.QueryResult, opts ListOptions, loc *time.Location) (*Bars, error) {
	cols := frameColumns(res, opts.Columns, opts.Untyped)
	if len(res.Logs) == 0 && len(cols) == 0 {
		return &Bars{Percent: opts.Pie}, nil
	}
	dimension, err := requireColumn(cols, opts.Label, false)
	if err != nil {
		return nil, err
	}
	metric, err := requireColumn(cols, opts.Value, true)
	if err != nil {
		return nil, err
	}
	var (
		entries  []listEntry
		notes    []string
		nulls    int
		firstDup string
		keys     = map[string]int{}
	)
	for _, raw := range res.Logs {
		dv := readCell(raw[dimension.name], *dimension)
		key, label := identity(dv), categoryLabel(dv, *dimension, loc)
		keys[key]++
		if keys[key] == 2 && firstDup == "" {
			firstDup = label
		}
		var value *Number
		if n, ok := readCell(raw[metric.name], *metric).(Number); ok {
			value = &n
		}
		if value == nil && opts.Pie {
			nulls++
			continue
		}
		if opts.Pie && value != nil && value.sign() < 0 {
			return nil, errors.New("pie values must be nonnegative")
		}
		entries = append(entries, listEntry{label: label, value: value})
	}
	if err := duplicateKeys(keys, firstDup, []string{dimension.name}); err != nil {
		return nil, err
	}
	if nulls > 0 {
		notes = append(notes, fmt.Sprintf("%d NULL value(s) omitted.", nulls))
	}
	out, err := listBars(entries, opts, notes)
	if err != nil {
		return nil, err
	}
	out.Rows, out.Truncation = len(res.Logs), res.Stats.Truncation
	return out, nil
}

// CountListBars draws a count-mode toplist or pie: the count of each
// series over the window, largest first.
func CountListBars(buckets []SeriesBucket, opts ListOptions) (*Bars, error) {
	totals := map[CountSeries]int64{}
	var order []CountSeries
	for _, b := range buckets {
		if _, ok := totals[b.Series]; !ok {
			order = append(order, b.Series)
		}
		totals[b.Series] += b.Count
	}
	slices.SortStableFunc(order, func(a, b CountSeries) int { return cmp.Compare(totals[b], totals[a]) })
	entries := make([]listEntry, len(order))
	for i, s := range order {
		n := countNumber(totals[s])
		entries[i] = listEntry{label: s.label(), value: &n}
	}
	return listBars(entries, opts, nil)
}

// listBars folds small and overflow pie slices into Other, keeps the rows
// the panel shows and scales the bars, as buildPanelList does.
func listBars(entries []listEntry, opts ListOptions, notes []string) (*Bars, error) {
	if opts.Pie {
		entries, notes = foldPie(entries, opts.MinSlicePercent, notes)
	}
	maxRows, noun := cmp.Or(opts.MaxRows, defaultToplistRows), "rows"
	if opts.Pie {
		maxRows, noun = maxPieSlices, "slices"
	}
	if len(entries) > maxRows {
		notes = append(notes, fmt.Sprintf("%d of %d %s shown.", maxRows, len(entries), noun))
		entries = entries[:maxRows]
	}
	if len(entries) == 0 {
		return &Bars{Percent: opts.Pie, Notes: notes}, nil
	}
	block := BarBlock{Rows: make([]BarRow, len(entries))}
	coords := make([]float64, len(entries))
	var largest float64
	for i, e := range entries {
		block.Rows[i] = BarRow{Label: e.label, Value: "NULL"}
		if e.value == nil {
			continue
		}
		text, c, err := barValue(*e.value, opts.Format)
		if err != nil {
			return nil, err
		}
		block.Rows[i].Value, coords[i] = text, c
		largest = max(largest, math.Abs(c))
	}
	for i, c := range coords {
		block.Rows[i].Width = barWidth(c, largest)
	}
	if opts.Pie {
		for i, tenths := range percentTenths(coords, largest) {
			block.Rows[i].Percent = fmt.Sprintf("%d.%d%%", tenths/10, tenths%10)
		}
	}
	return &Bars{Blocks: []BarBlock{block}, Percent: opts.Pie, Notes: notes}, nil
}

// foldPie folds the slices under minPercent of the total, and the slices
// past maxPieSlices, into one Other slice at the end.
func foldPie(entries []listEntry, minPercent float64, notes []string) (kept []listEntry, withNotes []string) {
	distinct := len(entries)
	var total, remainder *Number
	for _, e := range entries {
		if e.value != nil {
			total = addNumber(total, *e.value)
		}
	}
	fold := func(e listEntry) {
		if e.value != nil {
			remainder = addNumber(remainder, *e.value)
		}
	}
	if minPercent > 0 && total != nil {
		threshold, okThreshold := floatExact(minPercent)
		totalExact, okTotal := asExact(*total)
		if okThreshold && okTotal {
			bound := threshold.mul(totalExact)
			entries = slices.DeleteFunc(entries, func(e listEntry) bool {
				v, ok := asExact(*e.value)
				if ok && v.mul(exact{coef: bigHundred}).cmp(bound) < 0 {
					fold(e)
					return true
				}
				return false
			})
		}
	}
	extra := 0
	if remainder != nil {
		extra = 1
	}
	if len(entries)+extra > maxPieSlices {
		notes = append(notes, fmt.Sprintf("%d of %d slices shown. Overflow folded into Other.", maxPieSlices, distinct))
		for _, e := range entries[maxPieSlices-1:] {
			fold(e)
		}
		entries = entries[:maxPieSlices-1]
	}
	if remainder != nil {
		entries = append(entries, listEntry{label: "Other", value: remainder})
	}
	return entries, notes
}

// percentTenths is each value's share of sum in tenths of a percent,
// rounded down, with the tenths left over given to the largest
// remainders so the shares add up to 100.0, as the pie legend does.
func percentTenths(coords []float64, largest float64) []int {
	tenths := make([]int, len(coords))
	if largest == 0 {
		return tenths
	}
	// Each value is a share of the largest before it is added up, so values
	// near the float limit do not add up to infinity.
	var sum float64
	for _, c := range coords {
		sum += math.Abs(c) / largest
	}
	exactShare := make([]float64, len(coords))
	left := 1000
	for i, c := range coords {
		exactShare[i] = math.Abs(c) / largest / sum * 1000
		tenths[i] = int(math.Floor(exactShare[i]))
		left -= tenths[i]
	}
	order := make([]int, len(coords))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(exactShare[b]-float64(tenths[b]), exactShare[a]-float64(tenths[a]))
	})
	for _, i := range order[:min(max(left, 0), len(order))] {
		tenths[i]++
	}
	return tenths
}

// addNumber adds b to a, nil a being zero. Two exact numbers add exactly;
// a float on either side gives a float, as sum() in panelCharts.ts.
func addNumber(a *Number, b Number) *Number {
	if a == nil {
		return &b
	}
	if !a.isFloat && !b.isFloat {
		n := exactNumber(a.exact.add(b.exact))
		return &n
	}
	af, _ := coordinate(*a)
	bf, _ := coordinate(b)
	n := floatNumber(af + bf)
	return &n
}

func asExact(n Number) (exact, bool) {
	if n.isFloat {
		return floatExact(n.float)
	}
	return n.exact, true
}

func (n Number) sign() int {
	if n.isFloat {
		return cmp.Compare(n.float, 0)
	}
	return n.exact.coef.Sign()
}
