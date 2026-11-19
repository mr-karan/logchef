package reports

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestBuildCSVMatchesTheResult(t *testing.T) {
	at := time.Date(2026, 11, 15, 9, 0, 0, 123456789, time.UTC)
	res := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "ts", Type: "DateTime64(9)"}, {Name: "msg", Type: "String"}, {Name: "n", Type: "UInt64"}, {Name: "tags", Type: "Map(String, String)"}},
		Logs: []map[string]any{
			{"ts": at, "msg": `a "quoted", line` + "\nwith a break", "n": uint64(18446744073709551615), "tags": map[string]any{"k": "v"}},
			{"ts": nil, "msg": "", "n": 7, "tags": nil},
		},
	}
	file, err := BuildCSV(res, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(bytes.NewReader(file.Data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ts", "msg", "n", "tags"},
		{"2026-11-15T09:00:00.123456789Z", "a \"quoted\", line\nwith a break", "18446744073709551615", `{"k":"v"}`},
		{"", "", "7", ""},
	}
	if !slices.EqualFunc(records, want, slices.Equal) {
		t.Errorf("records = %q\nwant      %q", records, want)
	}
	if file.Rows != 2 || file.TotalRows != 2 || file.Cut() {
		t.Errorf("file = %d of %d rows, cut %v", file.Rows, file.TotalRows, file.Cut())
	}
}

func TestBuildCSVCutsOnAWholeRow(t *testing.T) {
	res := &models.QueryResult{Columns: []models.ColumnInfo{{Name: "id"}, {Name: "msg"}}}
	for i := range 2000 {
		res.Logs = append(res.Logs, map[string]any{"id": i, "msg": fmt.Sprintf("message number %d, quoted", i)})
	}
	const maxBytes = 1000
	file, err := BuildCSV(res, maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Data) > maxBytes || !bytes.HasSuffix(file.Data, []byte("\n")) {
		t.Fatalf("file is %d bytes, ends %q; want at most %d ending on a row", len(file.Data), file.Data[len(file.Data)-5:], maxBytes)
	}
	records, err := csv.NewReader(bytes.NewReader(file.Data)).ReadAll()
	if err != nil {
		t.Fatalf("cut file does not parse: %v", err)
	}
	if file.Rows != len(records)-1 || !file.Cut() || file.TotalRows != 2000 {
		t.Fatalf("file = %d rows (%d records), total %d", file.Rows, len(records), file.TotalRows)
	}
	last := records[len(records)-1]
	if want := fmt.Sprintf("message number %d, quoted", file.Rows-1); last[1] != want {
		t.Errorf("last row = %q, want %q", last, want)
	}
	if note := CSVNote(file); !strings.Contains(note, fmt.Sprintf("first %d of 2000 rows", file.Rows)) {
		t.Errorf("note = %q", note)
	}
}

func TestCSVName(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 15, 20, 0, 0, 0, time.UTC)
	if got := CSVName("Daily 5xx/errors: résumé", at, kolkata); got != "Daily-5xx-errors--résumé-2026-11-16.csv" {
		t.Errorf("CSVName = %q", got)
	}
}

func TestBuildCSVLeavesOutAHeaderPastTheCap(t *testing.T) {
	res := &models.QueryResult{Columns: []models.ColumnInfo{{Name: strings.Repeat("c", 200)}}}
	file, err := BuildCSV(res, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Data) > 100 || !file.HeaderTooLarge {
		t.Fatalf("file = %d bytes, header too large %v; want no file within a 100-byte cap", len(file.Data), file.HeaderTooLarge)
	}
	if note := CSVNote(file); !strings.Contains(note, "left out") {
		t.Errorf("note = %q", note)
	}
}

// TestCSVNameKeepsHeaderLinesShort uses the longest name a report allows,
// in characters that RFC 2231 encodes at 9 bytes each.
func TestCSVNameKeepsHeaderLinesShort(t *testing.T) {
	name := CSVName(strings.Repeat("界", 200), time.Now(), time.UTC)
	raw, err := BuildMessage(Message{
		From: mail.Address{Address: "logchef@example.com"}, To: mail.Address{Address: "user@example.com"},
		Subject: "x", Date: time.Now(), Text: "t", HTML: "h",
		Attachments: []Attachment{{Name: name, ContentType: "text/csv", Data: []byte("a\n")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line of %d bytes, over the SMTP limit: %.60q", len(line), line)
		}
	}
}
