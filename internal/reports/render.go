package reports

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"slices"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// MaxBodyBytes bounds the HTML body. Mail clients such as Gmail cut a larger
// one.
const MaxBodyBytes = 100 * 1024

// minRowsShown is the floor of the first size step: tables stop halving at
// this many rows.
const minRowsShown = 5

// ErrBodyTooLarge means the body does not fit MaxBodyBytes even with every
// panel cut to its title.
var ErrBodyTooLarge = errors.New("report body does not fit the size limit")

// Document is everything one email shows.
type Document struct {
	ReportName string
	Location   *time.Location
	Window     Window
	Panels     []Panel
	// Owner and Sources name who the report runs as and what it reads, for
	// the footer.
	Owner   string
	Sources []string
	// SourceURL opens the dashboard or saved query at the window, and
	// ReportURL opens the report. Both are empty when no frontend URL is
	// configured, and the email then has no links.
	SourceURL string
	ReportURL string
}

// Panel is one rendered panel. A failed panel has no Table; an empty one
// has a Table with no rows.
type Panel struct {
	ID     string
	Title  string
	Table  *Table
	Failed bool
}

// Rendered is a body that fits MaxBodyBytes.
type Rendered struct {
	HTML string
	Text string
	// CutPanels lists the panels replaced by their title, in order.
	CutPanels []string
}

// Render writes the HTML and text bodies. To keep the HTML under
// MaxBodyBytes it halves the rows per table until the body fits or tables
// show minRowsShown rows, then replaces panels from the end with their
// title and a link. maxRows is the report's rows per table.
func Render(doc Document, maxRows int) (Rendered, error) {
	cut := make([]bool, len(doc.Panels))
	rows := maxRows
	for {
		html, err := renderHTML(doc, rows, cut)
		if err != nil {
			return Rendered{}, err
		}
		if len(html) <= MaxBodyBytes {
			return Rendered{HTML: html, Text: renderText(doc, rows, cut), CutPanels: cutIDs(doc.Panels, cut)}, nil
		}
		if rows > minRowsShown {
			rows = max(rows/2, minRowsShown)
			continue
		}
		next := lastUncut(cut)
		if next < 0 {
			return Rendered{}, fmt.Errorf("%w: %d bytes with every panel cut", ErrBodyTooLarge, len(html))
		}
		cut[next] = true
	}
}

func lastUncut(cut []bool) int {
	for i, c := range slices.Backward(cut) {
		if !c {
			return i
		}
	}
	return -1
}

func cutIDs(panels []Panel, cut []bool) []string {
	var ids []string
	for i, p := range panels {
		if cut[i] {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// truncationLabel is the RFC 0003 label of a cut result. A sum over a cut
// result is wrong without it.
func truncationLabel(t *Table) string {
	switch t.Truncation {
	case models.TruncationRows:
		return fmt.Sprintf("First %d rows", t.TotalRows)
	case models.TruncationBytes:
		return "Cut by size"
	case models.TruncationUnknown:
		return "May be incomplete"
	case models.TruncationNone:
	}
	return ""
}

// panelView is a panel as the templates print it.
type panelView struct {
	Title      string
	Cut        bool
	Failed     bool
	Empty      bool
	Columns    []string
	Rows       [][]Cell
	MoreRows   int
	Truncation string
}

func panelViews(doc Document, rows int, cut []bool) []panelView {
	views := make([]panelView, len(doc.Panels))
	for i, p := range doc.Panels {
		v := panelView{Title: p.Title, Cut: cut[i], Failed: p.Failed}
		if p.Table != nil && !cut[i] {
			shown := p.Table.Rows[:min(rows, len(p.Table.Rows))]
			v.Columns, v.Rows = p.Table.Columns, shown
			v.Empty = p.Table.TotalRows == 0
			v.MoreRows = p.Table.TotalRows - len(shown)
			v.Truncation = truncationLabel(p.Table)
		}
		views[i] = v
	}
	return views
}

func formatWindowEdge(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02 15:04 MST")
}

var htmlBody = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>{{.Name}}</title></head>
<body style="margin:0;padding:0;background:#f4f4f5;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f4f5;"><tr><td align="center" style="padding:24px 12px;">
<table role="presentation" width="640" cellpadding="0" cellspacing="0" style="max-width:640px;width:100%;background:#ffffff;border:1px solid #e4e4e7;font-family:Arial,Helvetica,sans-serif;color:#18181b;">
<tr><td style="padding:20px 24px;border-bottom:1px solid #e4e4e7;">
<div style="font-size:18px;font-weight:bold;">{{.Name}}</div>
<div style="font-size:13px;color:#52525b;margin-top:4px;">{{.From}} to {{.To}} ({{.Zone}})</div>
</td></tr>
{{range .Panels}}<tr><td style="padding:16px 24px;border-bottom:1px solid #e4e4e7;">
<div style="font-size:15px;font-weight:bold;margin-bottom:8px;">{{.Title}}</div>
{{if .Cut}}<div style="font-size:13px;color:#52525b;">This panel was left out to keep the email small.{{if $.SourceURL}} <a href="{{$.SourceURL}}" style="color:#2563eb;">Open it in Logchef</a>.{{end}}</div>
{{else if .Failed}}<div style="font-size:13px;color:#b91c1c;">This panel failed to load.</div>
{{else if .Empty}}<div style="font-size:13px;color:#52525b;">No rows</div>
{{else}}{{if .Truncation}}<div style="font-size:12px;color:#a16207;margin-bottom:6px;">{{.Truncation}}</div>
{{end}}<table cellpadding="0" cellspacing="0" style="border-collapse:collapse;width:100%;font-size:12px;">
<tr>{{range .Columns}}<th align="left" style="padding:4px 6px;border-bottom:1px solid #d4d4d8;background:#fafafa;">{{.}}</th>{{end}}</tr>
{{range .Rows}}<tr>{{range .}}<td style="padding:4px 6px;border-bottom:1px solid #f4f4f5;vertical-align:top;{{if .Null}}color:#a1a1aa;{{end}}">{{.Text}}</td>{{end}}</tr>
{{end}}</table>
{{if .MoreRows}}<div style="font-size:12px;color:#52525b;margin-top:6px;">{{if $.SourceURL}}<a href="{{$.SourceURL}}" style="color:#2563eb;">{{.MoreRows}} more rows</a>{{else}}{{.MoreRows}} more rows{{end}}</div>
{{end}}{{end}}</td></tr>
{{end}}<tr><td style="padding:16px 24px;font-size:12px;color:#71717a;">
{{if .Owner}}<div>Runs as {{.Owner}}.</div>{{end}}
{{if .Sources}}<div>Sources: {{range $i, $s := .Sources}}{{if $i}}, {{end}}{{$s}}{{end}}.</div>{{end}}
<div>Window: {{.From}} to {{.To}}, timezone {{.Zone}}.</div>
{{if .SourceURL}}<div><a href="{{.SourceURL}}" style="color:#2563eb;">Open in Logchef</a>{{if .ReportURL}} · <a href="{{.ReportURL}}" style="color:#2563eb;">Report settings</a>{{end}}</div>{{else if .ReportURL}}<div><a href="{{.ReportURL}}" style="color:#2563eb;">Report settings</a></div>{{end}}
</td></tr>
</table>
</td></tr></table>
</body></html>
`))

func renderHTML(doc Document, rows int, cut []bool) (string, error) {
	var b bytes.Buffer
	err := htmlBody.Execute(&b, map[string]any{
		"Name":      doc.ReportName,
		"From":      formatWindowEdge(doc.Window.Start, doc.Location),
		"To":        formatWindowEdge(doc.Window.End, doc.Location),
		"Zone":      doc.Location.String(),
		"Panels":    panelViews(doc, rows, cut),
		"Owner":     doc.Owner,
		"Sources":   doc.Sources,
		"SourceURL": doc.SourceURL,
		"ReportURL": doc.ReportURL,
	})
	if err != nil {
		return "", fmt.Errorf("rendering report html: %w", err)
	}
	return b.String(), nil
}

func renderText(doc Document, rows int, cut []bool) string {
	var b strings.Builder
	from, to := formatWindowEdge(doc.Window.Start, doc.Location), formatWindowEdge(doc.Window.End, doc.Location)
	fmt.Fprintf(&b, "%s\n%s to %s (%s)\n", doc.ReportName, from, to, doc.Location)
	for _, p := range panelViews(doc, rows, cut) {
		fmt.Fprintf(&b, "\n== %s ==\n", p.Title)
		switch {
		case p.Cut:
			b.WriteString("This panel was left out to keep the email small.\n")
		case p.Failed:
			b.WriteString("This panel failed to load.\n")
		case p.Empty:
			b.WriteString("No rows\n")
		default:
			if p.Truncation != "" {
				b.WriteString(p.Truncation + "\n")
			}
			b.WriteString(strings.Join(p.Columns, " | ") + "\n")
			for _, row := range p.Rows {
				texts := make([]string, len(row))
				for i, c := range row {
					texts[i] = strings.Join(strings.Fields(c.Text), " ")
				}
				b.WriteString(strings.Join(texts, " | ") + "\n")
			}
			if p.MoreRows > 0 {
				fmt.Fprintf(&b, "%d more rows\n", p.MoreRows)
			}
		}
	}
	b.WriteString("\n--\n")
	if doc.Owner != "" {
		fmt.Fprintf(&b, "Runs as %s.\n", doc.Owner)
	}
	if len(doc.Sources) > 0 {
		fmt.Fprintf(&b, "Sources: %s.\n", strings.Join(doc.Sources, ", "))
	}
	fmt.Fprintf(&b, "Window: %s to %s, timezone %s.\n", from, to, doc.Location)
	if doc.SourceURL != "" {
		fmt.Fprintf(&b, "Open in Logchef: %s\n", doc.SourceURL)
	}
	if doc.ReportURL != "" {
		fmt.Fprintf(&b, "Report settings: %s\n", doc.ReportURL)
	}
	return b.String()
}
