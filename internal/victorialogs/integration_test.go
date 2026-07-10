package victorialogs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// integrationBaseURL returns the VictoriaLogs URL to test against, or ""/skip
// when the env var is unset. Keeping this env-gated means `go test ./...` stays
// hermetic: without a live instance the whole suite is skipped.
func integrationBaseURL(t *testing.T) string {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("LOGCHEF_TEST_VICTORIALOGS_URL"))
	if baseURL == "" {
		t.Skip("LOGCHEF_TEST_VICTORIALOGS_URL is not set; skipping VictoriaLogs integration tests")
	}
	return baseURL
}

// fixtureRow is one log record we ingest. It is also the source of truth for
// every assertion (expected level counts, known field values, row count).
type fixtureRow struct {
	msg        string
	level      string
	service    string
	durationMs int
	offset     time.Duration // relative to the fixture window base
}

func fixtureRows() []fixtureRow {
	return []fixtureRow{
		{msg: "login failed", level: "error", service: "api", durationMs: 120, offset: 0},
		{msg: "db timeout", level: "error", service: "api", durationMs: 340, offset: 15 * time.Second},
		{msg: "request ok", level: "info", service: "web", durationMs: 12, offset: 30 * time.Second},
		{msg: "cache hit", level: "info", service: "web", durationMs: 5, offset: 45 * time.Second},
		{msg: "disk almost full", level: "warn", service: "api", durationMs: 0, offset: 60 * time.Second},
	}
}

// newTestRunID returns a random hex tag so fixtures ingested by this run never
// collide with other data already present in the shared local instance. Every
// query in the suite is scoped to this tag via the source's immutable scope.
func newTestRunID(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate test run id: %v", err)
	}
	return "logchef_it_" + hex.EncodeToString(buf)
}

// ingestFixtures pushes the fixture rows via the jsonline insert API, tagging
// each with the run id and _time so they land in a known window.
func ingestFixtures(t *testing.T, baseURL, runID string, base time.Time, rows []fixtureRow) {
	t.Helper()

	var body bytes.Buffer
	encoder := json.NewEncoder(&body) // Encode writes a trailing newline: exactly the stream+json framing.
	for _, row := range rows {
		record := map[string]any{
			"_time":       base.Add(row.offset).Format(time.RFC3339),
			"_msg":        row.msg,
			"level":       row.level,
			"service":     row.service,
			"duration_ms": row.durationMs,
			"test_run":    runID,
		}
		if err := encoder.Encode(record); err != nil {
			t.Fatalf("encode fixture: %v", err)
		}
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/insert/jsonline?_time_field=_time&_msg_field=_msg"
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		t.Fatalf("create ingest request: %v", err)
	}
	req.Header.Set("Content-Type", "application/stream+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingest fixtures: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("ingest fixtures returned status %d", resp.StatusCode)
	}
}

// waitForFixtures polls until all fixture rows are queryable. VictoriaLogs
// indexes newly ingested data with a small delay, so we retry (bounded) instead
// of sleeping blindly.
func waitForFixtures(t *testing.T, provider *Provider, source *models.Source, window queryWindow, expected int) {
	t.Helper()

	const maxAttempts = 40
	for attempt := 0; attempt < maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		result, err := provider.QueryLogs(ctx, source, datasource.QueryRequest{
			RawQuery:  "*",
			StartTime: &window.start,
			EndTime:   &window.end,
			Limit:     100,
			MaxLimit:  1000,
		})
		cancel()
		if err == nil && len(result.Logs) >= expected {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("fixtures not visible after %d attempts", maxAttempts)
}

type queryWindow struct {
	start time.Time
	end   time.Time
}

// sortedMessages extracts and sorts the _msg values from a result set so two
// result sets can be compared regardless of row order.
func sortedMessages(logs []map[string]any) []string {
	msgs := make([]string, 0, len(logs))
	for _, row := range logs {
		if msg, ok := row["_msg"].(string); ok {
			msgs = append(msgs, msg)
		}
	}
	sort.Strings(msgs)
	return msgs
}

func TestIntegrationVictoriaLogs(t *testing.T) {
	baseURL := integrationBaseURL(t)

	runID := newTestRunID(t)
	rows := fixtureRows()

	// Fixtures live in a narrow window near "now" so lookback-based endpoints
	// (alerts, activity) capture them.
	now := time.Now().UTC().Truncate(time.Second)
	base := now.Add(-2 * time.Minute)
	window := queryWindow{start: base.Add(-time.Minute), end: now.Add(time.Minute)}

	// scopeFilter is the immutable per-run isolation applied to EVERY provider
	// call (native LogsQL). It is stored on the source's connection scope, which
	// is exactly how production pins a source to a tenant/namespace slice.
	scopeFilter := fmt.Sprintf(`test_run:=%q`, runID)

	conn := models.VictoriaLogsConnectionInfo{
		BaseURL: baseURL,
		Scope:   models.VictoriaLogsScope{Query: scopeFilter},
	}
	connectionJSON := mustJSON(t, conn)

	// Build the source exactly as production does: SourceType victorialogs,
	// _time as the meta timestamp field, connection_config JSON pointing at the
	// live instance.
	source := mustSource(t, conn)
	provider := newTestProvider(nil) // nil server => real http.Client hitting the env URL.

	// Mirror the production source lifecycle (health check + connection cache).
	initCtx, cancelInit := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelInit()
	if err := provider.InitializeSource(initCtx, source); err != nil {
		t.Fatalf("InitializeSource: %v", err)
	}

	ingestFixtures(t, baseURL, runID, base, rows)
	waitForFixtures(t, provider, source, window, len(rows))

	// ---- Subtest 1: ValidateConnection against the live instance. ----
	t.Run("ValidateConnection", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := provider.ValidateConnection(ctx, &models.ValidateConnectionRequest{
			SourceType: models.SourceTypeVictoriaLogs,
			Connection: connectionJSON,
		})
		if err != nil {
			t.Fatalf("ValidateConnection: %v", err)
		}
		if result == nil || strings.TrimSpace(result.Message) == "" {
			t.Fatalf("expected a validation message, got %#v", result)
		}
	})

	// ---- Subtest 2: native LogsQL returns exactly the fixture rows. ----
	t.Run("QueryLogsNativeLogsQL", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := provider.QueryLogs(ctx, source, datasource.QueryRequest{
			RawQuery:  scopeFilter, // native LogsQL, scoped to this run.
			StartTime: &window.start,
			EndTime:   &window.end,
			Limit:     100,
			MaxLimit:  1000,
		})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if len(result.Logs) != len(rows) {
			t.Fatalf("expected %d rows, got %d", len(rows), len(result.Logs))
		}
		// Stats populated: RowsRead reflects the returned rows.
		if result.Stats.RowsRead != len(rows) {
			t.Fatalf("unexpected stats.rows_read: %d", result.Stats.RowsRead)
		}
		// Columns populated with our fixture fields.
		columnNames := make(map[string]struct{}, len(result.Columns))
		for _, column := range result.Columns {
			columnNames[column.Name] = struct{}{}
		}
		for _, want := range []string{"_time", "_msg", "level", "service", "test_run", "duration_ms"} {
			if _, ok := columnNames[want]; !ok {
				t.Fatalf("expected column %q in %v", want, columnNames)
			}
		}
		// Known field value: the "login failed" row is a level=error api log.
		var found bool
		for _, row := range result.Logs {
			if row["_msg"] == "login failed" {
				found = true
				if row["level"] != "error" {
					t.Fatalf("expected level=error for 'login failed', got %v", row["level"])
				}
				if row["service"] != "api" {
					t.Fatalf("expected service=api for 'login failed', got %v", row["service"])
				}
			}
		}
		if !found {
			t.Fatalf("did not find the 'login failed' fixture row")
		}
	})

	// ---- Subtest 3: LogchefQL compiles to LogsQL and yields the same rows. ----
	t.Run("LogchefQLMatchesNative", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		compiled, err := provider.CompileLogchefQL(ctx, source, datasource.LogchefQLCompileRequest{
			Query: fmt.Sprintf("test_run = %q", runID),
		})
		if err != nil {
			t.Fatalf("CompileLogchefQL: %v", err)
		}
		if !compiled.Valid || strings.TrimSpace(compiled.Query) == "" {
			t.Fatalf("unexpected compile result: %#v", compiled)
		}

		compiledResult, err := provider.QueryLogs(ctx, source, datasource.QueryRequest{
			RawQuery:  compiled.Query,
			StartTime: &window.start,
			EndTime:   &window.end,
			Limit:     100,
			MaxLimit:  1000,
		})
		if err != nil {
			t.Fatalf("QueryLogs (compiled): %v", err)
		}

		nativeResult, err := provider.QueryLogs(ctx, source, datasource.QueryRequest{
			RawQuery:  scopeFilter,
			StartTime: &window.start,
			EndTime:   &window.end,
			Limit:     100,
			MaxLimit:  1000,
		})
		if err != nil {
			t.Fatalf("QueryLogs (native): %v", err)
		}

		gotMsgs := sortedMessages(compiledResult.Logs)
		wantMsgs := sortedMessages(nativeResult.Logs)
		if len(gotMsgs) != len(rows) {
			t.Fatalf("expected %d compiled rows, got %d", len(rows), len(gotMsgs))
		}
		if strings.Join(gotMsgs, "|") != strings.Join(wantMsgs, "|") {
			t.Fatalf("compiled rows %v != native rows %v", gotMsgs, wantMsgs)
		}
	})

	// ---- Subtest 4: histogram bucket counts sum to the fixture row count. ----
	t.Run("Histogram", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		histogram, err := provider.Histogram(ctx, source, datasource.HistogramRequest{
			StartTime: &window.start,
			EndTime:   &window.end,
			Window:    "1m",
			Query:     scopeFilter,
		})
		if err != nil {
			t.Fatalf("Histogram: %v", err)
		}
		total := 0
		for _, bucket := range histogram.Data {
			total += bucket.LogCount
		}
		if total != len(rows) {
			t.Fatalf("expected histogram counts to sum to %d, got %d", len(rows), total)
		}
	})

	// ---- Subtest 5: schema discovery includes the fixture field names. ----
	t.Run("GetSourceSchema", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		columns, err := provider.GetSourceSchema(ctx, source)
		if err != nil {
			t.Fatalf("GetSourceSchema: %v", err)
		}
		columnNames := make(map[string]struct{}, len(columns))
		for _, column := range columns {
			columnNames[column.Name] = struct{}{}
		}
		for _, want := range []string{"test_run", "level", "service", "duration_ms", "_msg", "_time"} {
			if _, ok := columnNames[want]; !ok {
				t.Fatalf("expected field %q in schema %v", want, columnNames)
			}
		}
	})

	// ---- Subtest 6: field values for level, scoped, with correct counts. ----
	t.Run("GetFieldValues", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := provider.GetFieldValues(ctx, source, datasource.FieldValuesRequest{
			FieldName: "level",
			Language:  models.QueryLanguageLogsQL,
			StartTime: window.start,
			EndTime:   window.end,
			Limit:     100,
		})
		if err != nil {
			t.Fatalf("GetFieldValues: %v", err)
		}

		// Expected distinct level counts derived from the fixtures.
		wantCounts := map[string]int64{}
		for _, row := range rows {
			wantCounts[row.level]++
		}
		gotCounts := map[string]int64{}
		for _, value := range result.Values {
			gotCounts[value.Value] = value.Count
		}
		if len(gotCounts) != len(wantCounts) {
			t.Fatalf("expected %d distinct levels, got %d (%v)", len(wantCounts), len(gotCounts), gotCounts)
		}
		for level, want := range wantCounts {
			if gotCounts[level] != want {
				t.Fatalf("level %q: expected count %d, got %d", level, want, gotCounts[level])
			}
		}
	})

	// ---- Subtest 7: EvaluateAlert stats count() equals the fixture count. ----
	t.Run("EvaluateAlert", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := provider.EvaluateAlert(ctx, source, datasource.AlertQueryRequest{
			Language:        models.QueryLanguageLogsQL,
			Query:           fmt.Sprintf(`%s | stats count() as value`, scopeFilter),
			LookbackSeconds: 3600,
		})
		if err != nil {
			t.Fatalf("EvaluateAlert: %v", err)
		}
		if len(result.Logs) != 1 {
			t.Fatalf("expected 1 alert row, got %d (%#v)", len(result.Logs), result.Logs)
		}
		if got := fmt.Sprint(result.Logs[0]["value"]); got != fmt.Sprint(len(rows)) {
			t.Fatalf("expected alert value %d, got %q", len(rows), got)
		}
	})

	// ---- Subtest 8: live tail streams a newly-ingested row and honors cancel. ----
	t.Run("TailLogsStreamsNewRows", func(t *testing.T) {
		tailCtx, cancelTail := context.WithCancel(context.Background())
		defer cancelTail()

		rowCh := make(chan map[string]any, 64)
		tailErr := make(chan error, 1)
		go func() {
			emit := func(batch []map[string]any) error {
				for _, row := range batch {
					select {
					case rowCh <- row:
					default:
					}
				}
				return nil
			}
			tailErr <- provider.TailLogs(tailCtx, source, datasource.TailRequest{
				Query:    scopeFilter, // native LogsQL, scoped to this run.
				Language: models.QueryLanguageLogsQL,
			}, emit)
		}()

		// Let the upstream tail stream establish before ingesting, so the probe
		// row lands after the tail's live cutoff.
		time.Sleep(time.Second)

		marker := "tail probe " + runID
		ingestFixtures(t, baseURL, runID, time.Now().UTC(), []fixtureRow{
			{msg: marker, level: "info", service: "tail", durationMs: 1, offset: 0},
		})

		deadline := time.After(20 * time.Second)
		found := false
		for !found {
			select {
			case row := <-rowCh:
				if row["_msg"] == marker {
					found = true
				}
			case <-deadline:
				t.Fatalf("tail did not deliver the probe row within the window")
			}
		}

		// Cancelling the context must terminate the provider tail promptly.
		cancelTail()
		select {
		case <-tailErr:
		case <-time.After(10 * time.Second):
			t.Fatalf("tail did not terminate after context cancel")
		}
	})
}
