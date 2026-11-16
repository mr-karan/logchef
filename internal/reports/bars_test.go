package reports

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func barLines(b *Bars) []string {
	var lines []string
	for _, block := range b.Blocks {
		if block.Title != "" {
			lines = append(lines, "# "+block.Title)
		}
		for _, r := range block.Rows {
			line := fmt.Sprintf("%s | %s | %d", r.Label, r.Value, r.Width)
			if b.Percent {
				line += " | " + r.Percent
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func wantLines(t *testing.T, what string, got *Bars, want ...string) {
	t.Helper()
	if g := strings.Join(barLines(got), "\n"); g != strings.Join(want, "\n") {
		t.Errorf("%s bars:\n%s\nwant:\n%s", what, g, strings.Join(want, "\n"))
	}
}

func TestBarWindow(t *testing.T) {
	end := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	days := func(n int) Window { return Window{Start: end.AddDate(0, 0, -n), End: end} }
	for _, tc := range []struct {
		name     string
		interval string
		w        Window
		want     string
	}{
		{"one day in hours", "auto", days(1), "1h"},
		{"7 days, the 12-hour window", "auto", days(7), "12h"},
		{"30 days has no window under 24 buckets", "", days(30), "24h"},
		{"a coarser panel interval stays", "24h", days(7), "24h"},
		{"a finer panel interval grows", "1h", days(7), "12h"},
		// 09:30 to 09:30 starts and ends inside an hour: 25 hourly buckets.
		{"an unaligned day takes 2h", "auto", Window{Start: end.Add(-14*time.Hour - 30*time.Minute), End: end.Add(9*time.Hour + 30*time.Minute)}, "2h"},
	} {
		if got := BarWindow(tc.interval, tc.w, time.UTC, false); got != tc.want {
			t.Errorf("%s: BarWindow(%q) = %q, want %q", tc.name, tc.interval, got, tc.want)
		}
	}
}

// TestCountChartMergesThirtyDays is the R7 check: 30 daily buckets become
// 15 bars of two days whose values add up to the 30 buckets.
func TestCountChartMergesThirtyDays(t *testing.T) {
	loc := kolkata(t)
	start := time.Date(2026, 10, 17, 0, 0, 0, 0, loc)
	w := Window{Start: start, End: start.AddDate(0, 0, 30)}
	var buckets []SeriesBucket
	var total int64
	for day := range 30 {
		buckets = append(buckets, SeriesBucket{At: start.AddDate(0, 0, day), Count: int64(day + 1)})
		total += int64(day + 1)
	}
	if got := BarWindow("auto", w, loc, false); got != "24h" {
		t.Fatalf("BarWindow = %q, want 24h", got)
	}
	bars, err := CountChartBars(buckets, false, "24h", w, loc, false, models.DashboardNumberFormat{})
	if err != nil {
		t.Fatal(err)
	}
	if len(bars.Blocks) != 1 || len(bars.Blocks[0].Rows) != 15 {
		t.Fatalf("bars = %+v, want one block of 15 bars", bars)
	}
	var sum int64
	for i, r := range bars.Blocks[0].Rows {
		want := int64(4*i + 3) // days 2i and 2i+1 hold 2i+1 and 2i+2 logs
		if r.Value != fmt.Sprint(want) {
			t.Errorf("bar %d = %s, want %d", i, r.Value, want)
		}
		if wantLabel := start.AddDate(0, 0, 2*i).Format("2006-01-02 15:04 MST"); r.Label != wantLabel {
			t.Errorf("bar %d label = %q, want %q", i, r.Label, wantLabel)
		}
		sum += want
	}
	if sum != total || total != 465 {
		t.Errorf("bars add up to %d, buckets to %d", sum, total)
	}
	if len(bars.Notes) != 1 || bars.Notes[0] != "Each bar covers 2 days" {
		t.Errorf("notes = %q", bars.Notes)
	}
	if w := bars.Blocks[0].Rows[14].Width; w != 100 {
		t.Errorf("the largest bar is %d%% wide, want 100%%", w)
	}
}

func TestCountChartSeries(t *testing.T) {
	at := time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)
	w := Window{Start: at, End: at.Add(3 * time.Hour)}
	series := func(v string) CountSeries { return CountSeries{Value: v} }
	buckets := []SeriesBucket{
		{At: at, Count: 50, Series: CountSeries{Other: true}},
		{At: at, Count: 10, Series: series("GET")},
		{At: at.Add(time.Hour), Count: 30, Series: series("POST")},
		{At: at.Add(2 * time.Hour), Count: 20, Series: series("GET")},
		{At: at, Count: 1, Series: CountSeries{Null: true}},
		{At: at, Count: 2, Series: series("")},
		{At: at, Count: 3, Series: series("PUT")},
		{At: at, Count: 4, Series: series("PATCH")},
	}
	bars, err := CountChartBars(buckets, true, "1h", w, time.UTC, false, models.DashboardNumberFormat{})
	if err != nil {
		t.Fatal(err)
	}
	// GET and POST tie at 30 and keep the order they arrived in; Other
	// goes last whatever its size, and the five shown leave two out.
	if bars.MoreSeries != 2 {
		t.Errorf("MoreSeries = %d, want 2", bars.MoreSeries)
	}
	wantLines(t, "count series", bars,
		"# GET", "2026-11-16 06:00 UTC | 10 | 33", "2026-11-16 07:00 UTC | 0 | 0", "2026-11-16 08:00 UTC | 20 | 67",
		"# POST", "2026-11-16 06:00 UTC | 0 | 0", "2026-11-16 07:00 UTC | 30 | 100", "2026-11-16 08:00 UTC | 0 | 0",
		"# PATCH", "2026-11-16 06:00 UTC | 4 | 13", "2026-11-16 07:00 UTC | 0 | 0", "2026-11-16 08:00 UTC | 0 | 0",
		"# PUT", "2026-11-16 06:00 UTC | 3 | 10", "2026-11-16 07:00 UTC | 0 | 0", "2026-11-16 08:00 UTC | 0 | 0",
		"# (empty)", "2026-11-16 06:00 UTC | 2 | 7", "2026-11-16 07:00 UTC | 0 | 0", "2026-11-16 08:00 UTC | 0 | 0",
	)
}

func TestResultChartTwoYColumns(t *testing.T) {
	res := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "minute", Type: "DateTime"}, {Name: "requests", Type: "UInt64"}, {Name: "p99", Type: "Float64"}},
		Logs: []map[string]any{
			{"minute": "2026-11-16 03:31:00", "requests": "123456789012345678901234567890", "p99": 0.0042},
			{"minute": "2026-11-16 03:30:00", "requests": "61728394506172839450617283945", "p99": 1250.5},
			{"minute": nil, "requests": "1", "p99": 1.0},
		},
		Stats: models.QueryStats{Truncation: models.TruncationRows},
	}
	bars, err := ResultChartBars(res, ChartOptions{X: "minute", Y: []string{"requests", "p99"}}, kolkata(t))
	if err != nil {
		t.Fatal(err)
	}
	// Time order; each y column has its own scale; a value below one keeps
	// its first digits.
	wantLines(t, "result chart", bars,
		"# requests", "2026-11-16 09:00:00 IST | 61,728,394,506,172,839,450,617,283,945 | 50", "2026-11-16 09:01:00 IST | 123,456,789,012,345,678,901,234,567,890 | 100",
		"# p99", "2026-11-16 09:00:00 IST | 1,250.5 | 100", "2026-11-16 09:01:00 IST | 0.0042 | 1",
	)
	if len(bars.Notes) != 1 || bars.Notes[0] != "1 row(s) with NULL x values omitted from the time axis." {
		t.Errorf("notes = %q", bars.Notes)
	}
	if bars.Rows != 3 || bars.Truncation != models.TruncationRows {
		t.Errorf("rows %d truncation %q, want the result's", bars.Rows, bars.Truncation)
	}

	res.Logs = append(res.Logs, map[string]any{"minute": "2026-11-16 03:30:00", "requests": "2", "p99": 2.0})
	if _, err := ResultChartBars(res, ChartOptions{X: "minute", Y: []string{"requests"}}, time.UTC); err == nil || !strings.Contains(err.Error(), "1 duplicate key(s)") {
		t.Errorf("a repeated x value: err = %v, want a duplicate key error", err)
	}
	if _, err := ResultChartBars(res, ChartOptions{X: "minute", Y: []string{"missing"}}, time.UTC); err == nil {
		t.Error("a missing y column is accepted")
	}
}

func TestResultChartSeriesInLegendOrder(t *testing.T) {
	res := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "host", Type: "String"}, {Name: "level", Type: "String"}, {Name: "n", Type: "UInt64"}},
		Logs: []map[string]any{
			{"host": "b", "level": "warn", "n": 4}, {"host": "a", "level": "error", "n": 2}, {"host": "a", "level": "warn", "n": 1},
		},
	}
	bars, err := ResultChartBars(res, ChartOptions{X: "host", Y: []string{"n"}, Series: "level"}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	// A category axis keeps result order, and a host with no error row
	// has no bar in the error block.
	wantLines(t, "series chart", bars, "# warn", "b | 4 | 100", "a | 1 | 25", "# error", "a | 2 | 50")
}

func TestListBars(t *testing.T) {
	res := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "service", Type: "Nullable(String)"}, {Name: "errors", Type: "Nullable(Int64)"}},
		Logs: []map[string]any{
			{"service": "api", "errors": 600}, {"service": "web", "errors": 300}, {"service": nil, "errors": 98},
			{"service": "<script>", "errors": 1}, {"service": "", "errors": 1}, {"service": "batch", "errors": nil},
		},
	}
	toplist, err := ResultListBars(res, ListOptions{Label: "service", Value: "errors", MaxRows: 5}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, "toplist", toplist, "api | 600 | 100", "web | 300 | 50", "(null) | 98 | 16", "<script> | 1 | 1", "(empty) | 1 | 1")
	if len(toplist.Notes) != 1 || toplist.Notes[0] != "5 of 6 rows shown." {
		t.Errorf("toplist notes = %q", toplist.Notes)
	}

	pie, err := ResultListBars(res, ListOptions{Pie: true, Label: "service", Value: "errors", MinSlicePercent: 1}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	// The NULL value is left out; the two slices under 1% fold into Other;
	// the shares add up to 100.0%.
	wantLines(t, "pie", pie, "api | 600 | 100 | 60.0%", "web | 300 | 50 | 30.0%", "(null) | 98 | 16 | 9.8%", "Other | 2 | 1 | 0.2%")
	if len(pie.Notes) != 1 || pie.Notes[0] != "1 NULL value(s) omitted." {
		t.Errorf("pie notes = %q", pie.Notes)
	}

	res.Logs = []map[string]any{{"service": "a", "errors": -1}}
	if _, err := ResultListBars(res, ListOptions{Pie: true, Label: "service", Value: "errors"}, time.UTC); err == nil {
		t.Error("a negative pie value is accepted")
	}
}

func TestPieSharesAddUpToAHundred(t *testing.T) {
	buckets := []SeriesBucket{{Count: 1, Series: CountSeries{Value: "a"}}, {Count: 1, Series: CountSeries{Value: "b"}}, {Count: 1, Series: CountSeries{Value: "c"}}}
	pie, err := CountListBars(buckets, ListOptions{Pie: true})
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, "thirds", pie, "a | 1 | 100 | 33.4%", "b | 1 | 100 | 33.3%", "c | 1 | 100 | 33.3%")
}

func TestPieSharesOfHugeFloats(t *testing.T) {
	res := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "k", Type: "String"}, {Name: "v", Type: "Float64"}},
		Logs:    []map[string]any{{"k": "a", "v": 1e308}, {"k": "b", "v": 1e308}},
	}
	pie, err := ResultListBars(res, ListOptions{Pie: true, Label: "k", Value: "v"}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range pie.Blocks[0].Rows {
		if r.Percent != "50.0%" {
			t.Errorf("%s share = %s, want 50.0%%", r.Label, r.Percent)
		}
	}
}

func TestCountListSortsLargestFirst(t *testing.T) {
	at := time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)
	buckets := []SeriesBucket{
		{At: at, Count: 5, Series: CountSeries{Value: "info"}},
		{At: at, Count: 7, Series: CountSeries{Value: "error"}},
		{At: at.Add(time.Hour), Count: 6, Series: CountSeries{Value: "info"}},
		{At: at.Add(time.Hour), Count: 2, Series: CountSeries{Other: true}},
	}
	bars, err := CountListBars(buckets, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantLines(t, "count toplist", bars, "info | 11 | 100", "error | 7 | 64", "Other | 2 | 18")
}

// barsDocument holds the four R7 golden panels: a count chart with a
// series, a result chart with two y columns, a toplist and a pie.
func barsDocument(t *testing.T) Document {
	t.Helper()
	loc := kolkata(t)
	at := time.Date(2026, 11, 16, 0, 0, 0, 0, loc)
	w := Window{Start: at.Add(-6 * time.Hour), End: at}
	var buckets []SeriesBucket
	for h := range 6 {
		ts := w.Start.Add(time.Duration(h) * time.Hour)
		buckets = append(buckets, SeriesBucket{At: ts, Count: int64(100 * (h + 1)), Series: CountSeries{Value: "2xx"}}, SeriesBucket{At: ts, Count: int64(h), Series: CountSeries{Value: "5xx"}})
	}
	countChart, err := CountChartBars(buckets, true, "1h", w, loc, false, models.DashboardNumberFormat{})
	if err != nil {
		t.Fatal(err)
	}
	resultChart, err := ResultChartBars(&models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "day", Type: "Date"}, {Name: "bytes", Type: "UInt64"}, {Name: "hits", Type: "UInt64"}},
		Logs: []map[string]any{
			{"day": "2026-11-14", "bytes": 1048576, "hits": 12}, {"day": "2026-11-15", "bytes": 3145728, "hits": 30},
		},
	}, ChartOptions{X: "day", Y: []string{"bytes", "hits"}, Format: models.DashboardNumberFormat{Notation: "compact"}}, loc)
	if err != nil {
		t.Fatal(err)
	}
	list := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "path", Type: "String"}, {Name: "n", Type: "UInt64"}},
		Logs:    []map[string]any{{"path": "/api/<script>", "n": 70}, {"path": "/login", "n": 20}, {"path": "/health", "n": 10}},
	}
	toplist, err := ResultListBars(list, ListOptions{Label: "path", Value: "n"}, loc)
	if err != nil {
		t.Fatal(err)
	}
	pie, err := ResultListBars(list, ListOptions{Pie: true, Label: "path", Value: "n"}, loc)
	if err != nil {
		t.Fatal(err)
	}
	return Document{
		ReportName: "Edge traffic",
		Location:   loc,
		Window:     w,
		Panels: []Panel{
			{ID: "status", Title: "Requests by status", Bars: countChart},
			{ID: "daily", Title: "Daily traffic", Bars: resultChart},
			{ID: "paths", Title: "Top paths", Bars: toplist},
			{ID: "share", Title: "Path share", Bars: pie},
			{ID: "none", Title: "Nothing yet", Bars: &Bars{}},
		},
		Owner:     "Owner One",
		Sources:   []string{"edge_logs"},
		SourceURL: "https://logchef.example.com/dashboards/7",
	}
}

func TestRenderBarsGolden(t *testing.T) {
	out, err := Render(barsDocument(t), 25)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "bars.html", out.HTML)
	golden(t, "bars.txt", out.Text)
	if strings.Contains(out.HTML, "<script>") || !strings.Contains(out.HTML, "/api/&lt;script&gt;") {
		t.Error("a label is not escaped")
	}
	for _, want := range []string{`width="100%"`, "3.15M", "70.0%", "No data", "Each bar covers 1h"} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("html lacks %q", want)
		}
	}
}

func TestRenderCutsBarRowsAndSeries(t *testing.T) {
	block := BarBlock{Title: "s"}
	for i := range 12 {
		block.Rows = append(block.Rows, BarRow{Label: fmt.Sprint(i), Value: fmt.Sprint(i), Width: 50})
	}
	doc := Document{ReportName: "r", Location: time.UTC, SourceURL: "https://x.example.com/d", Panels: []Panel{
		{ID: "c", Title: "c", Bars: &Bars{Blocks: []BarBlock{block}, MoreSeries: 3}},
	}}
	out, err := Render(doc, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 more rows", "3 more series"} {
		if !strings.Contains(out.HTML, want) || !strings.Contains(out.Text, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(out.Text, "| 10\n") {
		t.Error("an 11th bar is shown past the row cap")
	}
}
