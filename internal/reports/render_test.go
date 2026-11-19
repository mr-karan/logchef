package reports

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/reports -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file; rerun with -update and review the diff\n--- got ---\n%s", name, got)
	}
}

func kolkata(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// savedQueryResult holds the values the R2 check names: a time column, a
// NULL, a 30-digit integer and a cell that holds <script>, in the shapes the
// ClickHouse reader returns them.
func savedQueryResult() *models.QueryResult {
	ts := time.Date(2026, 11, 16, 3, 25, 7, 0, time.UTC)
	return &models.QueryResult{
		Columns: []models.ColumnInfo{
			{Name: "timestamp", Type: "DateTime64(3, 'UTC')"},
			{Name: "day", Type: "Date"},
			{Name: "service", Type: "LowCardinality(String)"},
			{Name: "trace_id", Type: "Nullable(String)"},
			{Name: "bytes", Type: "UInt256"},
			{Name: "count", Type: "UInt64"},
			{Name: "message", Type: "String"},
		},
		Logs: []map[string]any{
			{"timestamp": ts, "day": time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), "service": "api", "trace_id": nil, "bytes": "123456789012345678901234567890", "count": uint64(42), "message": `<script>alert("x")</script>`},
			{"timestamp": ts.Add(-time.Minute), "day": time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), "service": "web", "trace_id": "abc", "bytes": json.Number("7"), "count": uint64(1), "message": "line one\nline two"},
			{"timestamp": ts.Add(-2 * time.Minute), "day": time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), "service": "web", "trace_id": "def", "bytes": json.Number("8"), "count": uint64(3), "message": "ok"},
		},
		Stats: models.QueryStats{Truncation: models.TruncationRows},
	}
}

func goldenDocument(t *testing.T, table Table) Document {
	loc := kolkata(t)
	return Document{
		ReportName: "Daily 5xx & <errors>",
		Location:   loc,
		Window:     Window{Start: time.Date(2026, 11, 15, 9, 0, 0, 0, loc), End: time.Date(2026, 11, 16, 9, 0, 0, 0, loc)},
		Panels:     []Panel{{ID: "query", Title: "5xx by service", Table: &table}},
		Owner:      "Owner A",
		Sources:    []string{"logs.http"},
		SourceURL:  "https://logchef.example.com/logs/saved/12?start=1&end=2",
		ReportURL:  "https://logchef.example.com/reports/3",
	}
}

func TestRenderTableGolden(t *testing.T) {
	table := RenderTable(savedQueryResult(), kolkata(t), 2)
	r, err := Render(goldenDocument(t, table), 25)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "saved_query.html", r.HTML)
	golden(t, "saved_query.txt", r.Text)

	for _, want := range []string{
		"2026-11-16 08:55:07 IST", // an instant in the report's zone
		"<td style=\"padding:4px 6px;border-bottom:1px solid #f4f4f5;vertical-align:top;\">2026-11-16</td>", // a Date as the date
		"123456789012345678901234567890",                  // every digit of a wide integer
		"color:#a1a1aa;\">NULL</td>",                      // a NULL, marked
		"&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;", // escaped
		"First 3 rows",
		"1 more rows",
	} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("html lacks %q", want)
		}
	}
	if strings.Contains(r.HTML, "<script>") {
		t.Error("html holds an unescaped <script>")
	}
	if strings.Contains(r.HTML, "<img") || strings.Contains(r.HTML, "<link") || strings.Contains(r.HTML, "src=") {
		t.Error("html loads a remote resource")
	}
}

func TestRenderWithoutLinks(t *testing.T) {
	doc := goldenDocument(t, RenderTable(savedQueryResult(), kolkata(t), 1))
	doc.SourceURL, doc.ReportURL = "", ""
	r, err := Render(doc, 25)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.HTML, "<a ") || strings.Contains(r.Text, "://") {
		t.Error("an email with no frontend URL has a link")
	}
	if !strings.Contains(r.HTML, "2 more rows") {
		t.Error("the more-rows line is missing without a link")
	}
}

func TestRenderPanelStates(t *testing.T) {
	empty := RenderTable(&models.QueryResult{Columns: []models.ColumnInfo{{Name: "n", Type: "UInt64"}}}, time.UTC, 25)
	doc := Document{
		ReportName: "States",
		Location:   time.UTC,
		Window:     Window{Start: time.Unix(0, 0), End: time.Unix(3600, 0)},
		Panels: []Panel{
			{ID: "a", Title: "Empty", Table: &empty},
			{ID: "b", Title: "Broken", Failed: true},
		},
	}
	r, err := Render(doc, 25)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No rows", "This panel failed to load."} {
		if !strings.Contains(r.HTML, want) || !strings.Contains(r.Text, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

// wideResult is 2,000 rows of 12 columns with 300 characters each, about
// 7 MiB of cell text.
func wideResult() *models.QueryResult {
	res := &models.QueryResult{}
	for c := range 12 {
		res.Columns = append(res.Columns, models.ColumnInfo{Name: "col_" + string(rune('a'+c)), Type: "String"})
	}
	cell := strings.Repeat("x", 300)
	for range 2000 {
		row := map[string]any{}
		for _, c := range res.Columns {
			row[c.Name] = cell
		}
		res.Logs = append(res.Logs, row)
	}
	return res
}

func TestRenderBodySize(t *testing.T) {
	table := RenderTable(wideResult(), time.UTC, 100)
	doc := Document{ReportName: "Wide", Location: time.UTC, Window: Window{Start: time.Unix(0, 0), End: time.Unix(60, 0)}, Panels: []Panel{{ID: "query", Title: "Wide", Table: &table}}, SourceURL: "https://logchef.example.com/q"}
	r, err := Render(doc, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.HTML) >= MaxBodyBytes {
		t.Errorf("html is %d bytes, want under %d", len(r.HTML), MaxBodyBytes)
	}
	if len(r.CutPanels) != 0 {
		t.Errorf("cut panels %v, want the rows halved first", r.CutPanels)
	}
	// 100 rows halve to 50, 25, 12 and stop at the floor of 5 or the first
	// count that fits; each row is about 4 KiB, so 12 rows fit.
	if !strings.Contains(r.HTML, ">1988 more rows</a>") {
		t.Errorf("html lacks the more-rows line for 12 shown rows")
	}
}

func TestRenderCutsPanelsFromTheEnd(t *testing.T) {
	huge := strings.Repeat("y", 30*1024)
	res := &models.QueryResult{Columns: []models.ColumnInfo{{Name: "body", Type: "String"}}}
	for range 5 {
		res.Logs = append(res.Logs, map[string]any{"body": huge})
	}
	table := RenderTable(res, time.UTC, 25)
	small := RenderTable(&models.QueryResult{Columns: []models.ColumnInfo{{Name: "n", Type: "UInt64"}}, Logs: []map[string]any{{"n": 1}}}, time.UTC, 25)
	doc := Document{ReportName: "Cut", Location: time.UTC, Window: Window{Start: time.Unix(0, 0), End: time.Unix(60, 0)}, Panels: []Panel{
		{ID: "first", Title: "Small", Table: &small},
		{ID: "second", Title: "Huge", Table: &table},
		{ID: "third", Title: "Also huge", Table: &table},
	}}
	r, err := Render(doc, 25)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.CutPanels, ",") != "second,third" {
		t.Errorf("cut panels = %v, want the two huge panels, last first", r.CutPanels)
	}
	if len(r.HTML) > MaxBodyBytes || !strings.Contains(r.HTML, "This panel was left out to keep the email small.") {
		t.Errorf("html is %d bytes or lacks the cut note", len(r.HTML))
	}

	doc.ReportName = strings.Repeat("n", MaxBodyBytes)
	if _, err := Render(doc, 25); !errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("a body that cannot fit = %v, want ErrBodyTooLarge", err)
	}
}

func TestRenderLabelsATruncatedStat(t *testing.T) {
	doc := Document{
		ReportName: "Stats", Location: time.UTC, Window: Window{Start: time.Unix(0, 0), End: time.Unix(3600, 0)},
		Panels: []Panel{{ID: "s", Title: "Total", Stat: &Tile{Value: "1,000", Rows: 1000, Truncation: models.TruncationRows}}},
	}
	r, err := Render(doc, 25)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"html": r.HTML, "text": r.Text} {
		if !strings.Contains(body, "First 1000 rows") {
			t.Errorf("%s body lacks the truncation label", name)
		}
	}
}

// TestRenderLockedPanelsShowNoData gives every panel kind data and a lock,
// for the owner or for the recipient, and asserts that both bodies show
// each title with its locked line and none of the data.
func TestRenderLockedPanelsShowNoData(t *testing.T) {
	bars := func(value string, percent bool) *Bars {
		row := BarRow{Label: "secret-label", Value: value, Width: 100}
		if percent {
			row.Percent = "100.0%"
		}
		return &Bars{Blocks: []BarBlock{{Title: "secret-series", Rows: []BarRow{row}}}, Percent: percent, Notes: []string{"secret-note"}, Truncation: models.TruncationRows, Rows: 1}
	}
	panels := []Panel{
		{ID: "stat", Title: "Stat panel", Stat: &Tile{Value: "4,242", RowLabel: "secret-row", Truncation: models.TruncationRows, Rows: 1}},
		{ID: "table", Title: "Table panel", Table: &Table{Columns: []string{"secret-column"}, Rows: [][]Cell{{{Text: "secret-cell"}}}, TotalRows: 9, Truncation: models.TruncationRows}},
		{ID: "chart", Title: "Count chart panel", Bars: bars("6,161", false)},
		{ID: "toplist", Title: "Toplist panel", Bars: bars("5,151", false)},
		{ID: "pie", Title: "Pie panel", Bars: bars("3,131", true)},
	}
	for _, lock := range []struct {
		name string
		set  func(*Panel)
		line string
	}{
		{"recipient", func(p *Panel) { p.Restricted = true }, "You do not have access to this panel."},
		{"owner", func(p *Panel) { p.Locked = true }, "The report owner no longer has access to this panel."},
	} {
		doc := Document{ReportName: "r", Location: time.UTC, Panels: make([]Panel, len(panels))}
		for i := range panels {
			doc.Panels[i] = panels[i]
			lock.set(&doc.Panels[i])
		}
		out, err := Render(doc, 25)
		if err != nil {
			t.Fatal(err)
		}
		for body, text := range map[string]string{"html": out.HTML, "text": out.Text} {
			for _, leak := range []string{"4,242", "6,161", "5,151", "3,131", "secret", "100.0%", "First ", "more rows"} {
				if strings.Contains(text, leak) {
					t.Errorf("%s lock, %s body shows %q", lock.name, body, leak)
				}
			}
			if n := strings.Count(text, lock.line); n != len(panels) {
				t.Errorf("%s lock, %s body has %d locked lines, want %d", lock.name, body, n, len(panels))
			}
			for _, p := range panels {
				if !strings.Contains(text, p.Title) {
					t.Errorf("%s lock, %s body lacks the title %q", lock.name, body, p.Title)
				}
			}
		}
	}
}
