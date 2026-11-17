// Package providertest is the conformance suite that every log provider
// passes against a live backend (RFC 0011, "Provider conformance tests").
// A provider's integration test builds a Harness and calls Run.
package providertest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

// Row is one fixture entry. The harness stores Level as a field named
// "level" that LogchefQL and field values can read, and Message as the log
// line (_msg).
type Row struct {
	Time    time.Time
	Level   string
	Message string
}

// Queries are the provider's native text for what the suite checks. Each one
// sees only the rows this run ingested.
type Queries struct {
	// All matches every ingested row.
	All string
	// Count is an alert query whose single result row holds the number of
	// rows in the last hour in its first column.
	Count string
	// LevelError is the native equivalent of the LogchefQL filter
	// level="error".
	LevelError string
}

// Harness is what a provider gives the suite.
type Harness struct {
	Provider datasource.Provider
	// Source points at the live backend and is scoped to this run's rows.
	Source *models.Source
	// Language is the provider's native query language.
	Language models.QueryLanguage
	// Ingest writes rows to the backend. Rows may become visible later.
	Ingest  func(t *testing.T, rows []Row)
	Queries Queries
}

const visibilityTimeout = 30 * time.Second

// suite is one run's fixture and the expectations read from it.
type suite struct {
	h          Harness
	rows       []Row
	start, end time.Time
	// newestFirst and errorsNewestFirst are fixture messages in the order a
	// search returns them.
	newestFirst, errorsNewestFirst []string
}

// Run ingests a fixture through the harness and asserts what every provider
// must do.
func Run(t *testing.T, h Harness) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	s := &suite{
		h: h,
		// The window keeps every row away from its edges, where providers
		// differ on open and closed bounds.
		start: now.Add(-3 * time.Minute),
		end:   now.Add(time.Minute),
	}
	levels := []string{"error", "info", "error", "warn", "error", "info"}
	for i, level := range levels {
		s.rows = append(s.rows, Row{
			Time:    now.Add(time.Duration(i-len(levels)) * 20 * time.Second),
			Level:   level,
			Message: fmt.Sprintf("conformance row %d", i),
		})
	}
	for _, row := range slices.Backward(s.rows) {
		s.newestFirst = append(s.newestFirst, row.Message)
		if row.Level == "error" {
			s.errorsNewestFirst = append(s.errorsNewestFirst, row.Message)
		}
	}

	h.Ingest(t, s.rows)
	s.waitForRows(t)

	t.Run("QueryLogs returns rows newest first", s.newestFirstOrder)
	t.Run("QueryLogs at a limit equal to the rows is not truncated", func(t *testing.T) {
		s.limit(t, len(s.rows), models.TruncationNone)
	})
	t.Run("QueryLogs below the row count is a row truncation", func(t *testing.T) {
		s.limit(t, len(s.rows)-2, models.TruncationRows)
	})
	t.Run("histogram buckets sum to the row count", s.histogramSum)
	t.Run("field values have the seeded counts", s.fieldValues)
	t.Run("an alert returns one row with the count first", s.alertCount)
	t.Run("a LogchefQL equality filter returns the native rows", s.logchefQLEquality)
	// Last, because it ingests a row the other checks do not expect.
	t.Run("tail emits a row ingested after it starts once", s.tailOnce)
}

func (s *suite) query(t *testing.T, text string, limit int) *models.QueryResult {
	t.Helper()
	result, err := s.h.Provider.QueryLogs(t.Context(), s.h.Source, datasource.QueryRequest{
		RawQuery: text, StartTime: &s.start, EndTime: &s.end, Limit: limit, MaxLimit: 1000,
	})
	if err != nil {
		t.Fatalf("QueryLogs %q: %v", text, err)
	}
	return result
}

// waitForRows polls until the backend returns every fixture row, because
// ingested rows become visible asynchronously.
func (s *suite) waitForRows(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(visibilityTimeout)
	for {
		got := len(s.query(t, s.h.Queries.All, 100).Logs)
		if got >= len(s.rows) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d rows visible after %s", got, len(s.rows), visibilityTimeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *suite) newestFirstOrder(t *testing.T) {
	result := s.query(t, s.h.Queries.All, 100)
	if got := messages(result.Logs); !slices.Equal(got, s.newestFirst) {
		t.Fatalf("messages\n got %v\nwant %v", got, s.newestFirst)
	}
	var previous time.Time
	for i, row := range result.Logs {
		ts, err := time.Parse(time.RFC3339Nano, fmt.Sprint(row["_time"]))
		if err != nil {
			t.Fatalf("row %d _time: %v", i, err)
		}
		if i > 0 && ts.After(previous) {
			t.Fatalf("row %d (%s) is newer than row %d (%s)", i, ts, i-1, previous)
		}
		previous = ts
	}
}

func (s *suite) limit(t *testing.T, limit int, want models.Truncation) {
	result := s.query(t, s.h.Queries.All, limit)
	if got := messages(result.Logs); !slices.Equal(got, s.newestFirst[:limit]) {
		t.Fatalf("messages\n got %v\nwant %v", got, s.newestFirst[:limit])
	}
	if result.Stats.Truncation != want {
		t.Fatalf("truncation %q, want %q", result.Stats.Truncation, want)
	}
}

func (s *suite) histogramSum(t *testing.T) {
	histogram, err := s.h.Provider.Histogram(t.Context(), s.h.Source, datasource.HistogramRequest{
		Query: s.h.Queries.All, StartTime: &s.start, EndTime: &s.end, Window: "1m",
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, bucket := range histogram.Data {
		sum += bucket.LogCount
	}
	if searched := len(s.query(t, s.h.Queries.All, 100).Logs); sum != len(s.rows) || searched != sum {
		t.Fatalf("histogram sum %d, search rows %d, ingested %d", sum, searched, len(s.rows))
	}
}

func (s *suite) fieldValues(t *testing.T) {
	result, err := s.h.Provider.GetFieldValues(t.Context(), s.h.Source, datasource.FieldValuesRequest{
		FieldName: "level", FieldType: "string", Language: s.h.Language, StartTime: s.start, EndTime: s.end, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, value := range result.Values {
		got[value.Value] = value.Count
	}
	want := map[string]int64{"error": 3, "info": 2, "warn": 1}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("level values %v, want %v", got, want)
	}
}

func (s *suite) alertCount(t *testing.T) {
	result, err := s.h.Provider.EvaluateAlert(t.Context(), s.h.Source, datasource.AlertQueryRequest{
		Language: s.h.Language, Query: s.h.Queries.Count, At: time.Now(), LookbackSeconds: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != 1 {
		t.Fatalf("rows %v", result.Logs)
	}
	value, err := util.ExtractFirstNumeric(result)
	if err != nil || value != float64(len(s.rows)) {
		t.Fatalf("first column %v (%v), want %d; row %v columns %v", value, err, len(s.rows), result.Logs[0], result.Columns)
	}
}

func (s *suite) logchefQLEquality(t *testing.T) {
	compiler, ok := s.h.Provider.(datasource.LogchefQLCompiler)
	if !ok {
		t.Fatalf("%s provider does not implement LogchefQLCompiler", s.h.Provider.Type())
	}
	source := s.h.Source
	// The service resolves columns the same way before it compiles.
	if s.h.Provider.Describe().LogchefQLNeedsColumns && len(source.Columns) == 0 {
		columns, err := s.h.Provider.GetSourceSchema(t.Context(), source)
		if err != nil {
			t.Fatal(err)
		}
		withColumns := *source
		withColumns.Columns = columns
		source = &withColumns
	}
	compiled, err := compiler.CompileLogchefQL(t.Context(), source, datasource.LogchefQLCompileRequest{Query: `level="error"`})
	if err != nil {
		t.Fatal(err)
	}
	got := messages(s.query(t, compiled.Query, 100).Logs)
	native := messages(s.query(t, s.h.Queries.LevelError, 100).Logs)
	if !slices.Equal(native, s.errorsNewestFirst) || !slices.Equal(got, native) {
		t.Fatalf("LogchefQL (%s) %v, native %v, want %v", compiled.Query, got, native, s.errorsNewestFirst)
	}
}

func (s *suite) tailOnce(t *testing.T) {
	tailer, ok := s.h.Provider.(datasource.LogTailer)
	if !ok {
		t.Fatalf("%s provider does not implement LogTailer", s.h.Provider.Type())
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	marker := "conformance tail " + s.start.Format(time.RFC3339Nano)
	var mu sync.Mutex
	seen := 0
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
	done := make(chan error, 1)
	go func() {
		done <- tailer.TailLogs(ctx, s.h.Source, datasource.TailRequest{
			Query: s.h.Queries.All, Language: s.h.Language, PollInterval: 250 * time.Millisecond,
		}, func(batch []map[string]any) error {
			mu.Lock()
			defer mu.Unlock()
			for _, row := range batch {
				if row["_msg"] == marker {
					seen++
				}
			}
			return nil
		})
	}()

	// The tail starts at its first poll, so the row is written after it.
	time.Sleep(time.Second)
	s.h.Ingest(t, []Row{{Time: time.Now().UTC(), Level: "info", Message: marker}})
	deadline := time.Now().Add(visibilityTimeout)
	for count() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the tail did not emit the row")
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Later polls read the row again; none may emit it.
	time.Sleep(2 * time.Second)
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("tail ended with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the tail did not stop after cancel")
	}
	if n := count(); n != 1 {
		t.Fatalf("the row was emitted %d times", n)
	}
}

func messages(rows []map[string]any) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = fmt.Sprint(row["_msg"])
	}
	return out
}
