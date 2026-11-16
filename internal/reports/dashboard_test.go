package reports

import (
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestFixedWindow(t *testing.T) {
	for _, tc := range []struct {
		language models.QueryLanguage
		text     string
		fixed    bool
	}{
		{models.QueryLanguageClickHouseSQL, "SELECT count() FROM logs", true},
		{models.QueryLanguageClickHouseSQL, "SELECT count() FROM logs WHERE {{__time_filter}}", false},
		{models.QueryLanguageClickHouseSQL, "SELECT 1 WHERE ts BETWEEN {{ __from }} AND {{__to}}", false},
		{models.QueryLanguageClickHouseSQL, "SELECT 1 WHERE ts > {{__from}}", true},
		{models.QueryLanguageClickHouseSQL, "-- {{__time_filter}}\nSELECT 1", true},
		{models.QueryLanguageClickHouseSQL, "SELECT '{{__time_filter}}'", true},
		{models.QueryLanguageLogchefQL, "status = 500", false},
		{models.QueryLanguageLogsQL, "* | stats count()", false},
	} {
		if got := FixedWindow(&models.DashboardViewQuery{Language: tc.language, Text: tc.text}); got != tc.fixed {
			t.Errorf("FixedWindow(%s %q) = %v, want %v", tc.language, tc.text, got, tc.fixed)
		}
	}
}

func TestResolveInterval(t *testing.T) {
	at := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	span := func(d time.Duration) Window { return Window{Start: at.Add(-d), End: at} }
	for _, tc := range []struct {
		interval string
		w        Window
		want     string
	}{
		{"auto", span(24 * time.Hour), "15m"},
		{"", span(7 * 24 * time.Hour), "2h"},
		{"auto", span(15 * time.Minute), "10s"},
		{"auto", span(400 * 24 * time.Hour), "24h"},
		{"1h", span(24 * time.Hour), "1h"},
		{"1m", span(7 * 24 * time.Hour), "15m"},
	} {
		if got := ResolveInterval(tc.interval, tc.w); got != tc.want {
			t.Errorf("ResolveInterval(%q, %s) = %q, want %q", tc.interval, tc.w.End.Sub(tc.w.Start), got, tc.want)
		}
	}
}

func TestCountRowsFillsTheWindow(t *testing.T) {
	utc := func(h, m int) time.Time { return time.Date(2026, 11, 15, h, m, 0, 0, time.UTC) }
	w := Window{Start: utc(8, 30), End: utc(11, 30)}
	buckets := []CountRow{{At: utc(9, 0), Count: 2}, {At: utc(9, 0), Count: 3}, {At: utc(11, 0), Count: 1}}
	rows, err := CountRows(buckets, "1h", w, time.UTC, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []CountRow{{utc(8, 0), 0}, {utc(9, 0), 5}, {utc(10, 0), 0}, {utc(11, 0), 1}}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
	for i := range want {
		if !rows[i].At.Equal(want[i].At) || rows[i].Count != want[i].Count {
			t.Errorf("row %d = %v, want %v", i, rows[i], want[i])
		}
	}

	// An hour bucket in India starts on the local hour, half past in UTC.
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	rows, err = CountRows([]CountRow{{At: utc(9, 30), Count: 1}}, "1h", Window{Start: utc(8, 45), End: utc(10, 0)}, kolkata, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[0].At.Equal(utc(8, 30)) || !rows[1].At.Equal(utc(9, 30)) || rows[1].Count != 1 {
		t.Errorf("Kolkata rows = %v, want 08:30Z (0) and 09:30Z (1)", rows)
	}

	if rows, err := CountRows(nil, "1h", w, time.UTC, false); err != nil || rows != nil {
		t.Errorf("no buckets = %v, %v, want no rows", rows, err)
	}
	if _, err := CountRows(buckets, "1s", Window{Start: utc(0, 0), End: utc(23, 0)}, time.UTC, false); err == nil || !strings.Contains(err.Error(), "too many count buckets") {
		t.Errorf("1s buckets over a day: err = %v, want too many count buckets", err)
	}
}

func TestPanelWindow(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 15, 9, 0, 0, 0, kolkata)
	report := Window{Start: at.Add(-time.Hour), End: at}
	midnight := time.Date(2026, 11, 15, 0, 0, 0, 0, kolkata)
	for _, tc := range []struct {
		name     string
		rangeStr string
		dayStart bool
		want     Window
	}{
		{"no range takes the report window", "", true, report},
		{"a rolling range keeps the anchor", "7d", true, Window{Start: midnight.AddDate(0, 0, -7), End: midnight}},
		{"a rolling range at send time", "6h", false, Window{Start: at.Add(-6 * time.Hour), End: at}},
		{"a calendar range resolves at the run time", "today", true, Window{Start: midnight, End: at}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PanelWindow(&models.DashboardViewQuery{Range: tc.rangeStr}, report, tc.dayStart, kolkata, at)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Start.Equal(tc.want.Start) || !got.End.Equal(tc.want.End) {
				t.Errorf("window = %v to %v, want %v to %v", got.Start, got.End, tc.want.Start, tc.want.End)
			}
		})
	}
}

// TestCountRowsAcrossASkippedHour covers 2h buckets over the spring change
// in New York, where 02:00 does not exist: the buckets still advance, and
// the one after 00:00 starts at 03:00 EDT, as the dashboard view places it.
func TestCountRowsAcrossASkippedHour(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	local := func(d, h int) time.Time { return time.Date(2026, 3, d, h, 0, 0, 0, ny) }
	w := Window{Start: local(8, 0), End: local(8, 6)}
	done := make(chan struct{})
	var rows []CountRow
	go func() {
		defer close(done)
		rows, err = CountRows([]CountRow{{At: local(8, 4), Count: 3}}, "2h", w, ny, false)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CountRows did not return across the skipped hour")
	}
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.At.In(ny).Format("15:04 MST"))
	}
	if strings.Join(got, " ") != "00:00 EST 03:00 EDT 04:00 EDT 06:00 EDT" {
		t.Errorf("buckets = %v", got)
	}
}
