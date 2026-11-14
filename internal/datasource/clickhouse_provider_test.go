package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestEvaluateAlertRejectsNonSelectQueries(t *testing.T) {
	provider := &ClickHouseProvider{manager: &clickhouse.Manager{}}
	result, err := provider.EvaluateAlert(context.Background(), &models.Source{ID: 1}, AlertQueryRequest{Query: "DROP TABLE x"})
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "query" {
		t.Fatalf("EvaluateAlert() = %v, %v; want query validation error before connection lookup", result, err)
	}
}

func TestEvaluateAlertClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	source := &models.Source{ID: 1, Connection: models.ConnectionInfo{Host: addr, Database: "system", TableName: "numbers"}}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := NewClickHouseProvider(manager, log)
	for _, tc := range []struct {
		query string
		want  any
	}{
		{query: "SELECT count() AS value FROM numbers(1501)", want: uint64(1501)},
		{query: "SELECT count() AS value FROM (SELECT number FROM numbers(1501) LIMIT 1201)", want: uint64(1201)},
		// ClickHouse nests block comments and the parser does not, so the raw
		// text would run the hidden SELECT 42 instead of the validated literal.
		{query: "/* /* */ SELECT '*/ SELECT 42 AS value -- '", want: "*/ SELECT 42 AS value -- "},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: tc.query})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Logs) != 1 || len(result.Logs[0]) != 1 {
				t.Fatalf("EvaluateAlert() rows = %v, want one row with one column", result.Logs)
			}
			for _, got := range result.Logs[0] {
				if got != tc.want {
					t.Fatalf("EvaluateAlert() value = %#v, want %#v", got, tc.want)
				}
			}
		})
	}

	result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: "SELECT number AS value FROM numbers(1000000)"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != alertMaxRows || !result.Stats.Truncated {
		t.Fatalf("EvaluateAlert() buffered %d rows (truncated=%v), want %d truncated", len(result.Logs), result.Stats.Truncated, alertMaxRows)
	}

	result, err = provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: "SELECT number AS value FROM numbers(10) WHERE number > 100"})
	if err != nil || len(result.Logs) != 0 || result.Stats.Truncated {
		t.Fatalf("EvaluateAlert(empty) = %v, %v; want zero rows without error", result, err)
	}

	result, err = provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: "SELECT count() AS value, repeat('xx', 600000) AS detail FROM numbers(1501)"})
	if err == nil {
		t.Fatalf("EvaluateAlert(first row over byte limit) = %v, want error instead of an empty result", result.Logs)
	}

	// The parser keeps these as identifiers while ClickHouse reads an escaped
	// backtick or a heredoc string, which would run the hidden 42 AS value.
	for _, query := range []string{
		"SELECT 'n' AS `\\`, '`, 42 AS value -- '",
		`SELECT 'n' AS "\", '", 42 AS value -- '`,
		"SELECT $$ AS a, '$$, 42 AS value -- '",
	} {
		t.Run(query, func(t *testing.T) {
			result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: query})
			if _, ok := errors.AsType[*ValidationError](err); !ok {
				t.Fatalf("EvaluateAlert() = %v, %v; want query validation error", result, err)
			}
		})
	}
}

func TestHasLeadingTimestampSortKey(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		field string
		want  bool
	}{
		{name: "leading field matches", keys: []string{"event_time", "service"}, field: "event_time", want: true},
		{name: "quoted leading field matches", keys: []string{"`event_time`"}, field: "event_time", want: true},
		{name: "non-leading field is rejected", keys: []string{"service", "event_time"}, field: "event_time", want: false},
		{name: "missing metadata is rejected", field: "event_time", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLeadingTimestampSortKey(&clickhouse.TableInfo{SortKeys: tt.keys}, tt.field); got != tt.want {
				t.Fatalf("hasLeadingTimestampSortKey() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCanInspectSourceActivity(t *testing.T) {
	t.Parallel()

	optedIn := &models.ClickHouseActivityConfig{AllowView: true}
	tests := []struct {
		name      string
		engine    string
		sortKeys  []string
		activity  *models.ClickHouseActivityConfig
		wantAllow bool
		wantCaps  bool
	}{
		{
			name:      "MergeTree with leading timestamp sort key uses table path",
			engine:    "MergeTree",
			sortKeys:  []string{"timestamp", "service"},
			wantAllow: true,
			wantCaps:  false,
		},
		{
			name:      "MergeTree without leading timestamp sort key stays blocked even with allow_view",
			engine:    "MergeTree",
			sortKeys:  []string{"service", "timestamp"},
			activity:  optedIn,
			wantAllow: false,
			wantCaps:  false,
		},
		{
			name:      "MaterializedView without leading timestamp sort key stays blocked",
			engine:    "MaterializedView",
			activity:  optedIn,
			wantAllow: false,
			wantCaps:  false,
		},
		{
			name:      "View is blocked by default",
			engine:    "View",
			wantAllow: false,
			wantCaps:  false,
		},
		{
			name:      "View with allow_view=false is blocked",
			engine:    "View",
			activity:  &models.ClickHouseActivityConfig{AllowView: false},
			wantAllow: false,
			wantCaps:  false,
		},
		{
			name:      "View with allow_view=true is allowed with enforced scan limits",
			engine:    "View",
			activity:  optedIn,
			wantAllow: true,
			wantCaps:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := &models.Source{
				MetaTSField: "timestamp",
				Connection:  models.ConnectionInfo{Activity: tt.activity},
			}
			info := &clickhouse.TableInfo{Engine: tt.engine, SortKeys: tt.sortKeys}
			gotAllow, gotCaps := canInspectSourceActivity(src, info)
			if gotAllow != tt.wantAllow || gotCaps != tt.wantCaps {
				t.Fatalf("canInspectSourceActivity() = (%t, %t), want (%t, %t)", gotAllow, gotCaps, tt.wantAllow, tt.wantCaps)
			}
		})
	}
}

func TestConnectionFromConfigActivityValidationAndRedaction(t *testing.T) {
	t.Parallel()

	provider := &ClickHouseProvider{}

	conn, err := provider.connectionFromConfig(json.RawMessage(`{"host":"localhost:9000","database":"default","table_name":"logs_view","activity":{}}`))
	if err != nil {
		t.Fatalf("empty activity block should normalize cleanly: %v", err)
	}
	if conn.Activity != nil {
		t.Fatalf("empty activity block = %#v, want nil", conn.Activity)
	}

	for _, invalid := range []string{
		`{"host":"localhost:9000","database":"default","table_name":"logs_view","activity":{"allow_view":true,"max_rows_to_read":0}}`,
		`{"host":"localhost:9000","database":"default","table_name":"logs_view","activity":{"allow_view":true,"max_bytes_to_read":-1}}`,
	} {
		_, err := provider.connectionFromConfig(json.RawMessage(invalid))
		var valErr *ValidationError
		if !errors.As(err, &valErr) || valErr.Field != "connection.activity" {
			t.Fatalf("connectionFromConfig(%s) err = %v, want connection.activity validation error", invalid, err)
		}
	}

	maxRows := int64(5_000_000)
	src := &models.Source{
		SourceType: models.SourceTypeClickHouse,
		Connection: models.ConnectionInfo{
			Host:      "localhost:9000",
			Username:  "logchef",
			Password:  "top-secret",
			Database:  "default",
			TableName: "logs_view",
			Activity: &models.ClickHouseActivityConfig{
				AllowView:     true,
				MaxRowsToRead: &maxRows,
			},
		},
	}
	var redacted models.ConnectionInfoResponse
	if err := json.Unmarshal(src.RedactedConnectionConfig(), &redacted); err != nil {
		t.Fatalf("unmarshal redacted connection: %v", err)
	}
	if !redacted.HasPassword {
		t.Fatal("expected HasPassword=true on redacted connection")
	}
	if redacted.Activity == nil || !redacted.Activity.AllowView || redacted.Activity.MaxRowsToRead == nil || *redacted.Activity.MaxRowsToRead != maxRows {
		t.Fatalf("redacted activity = %#v, want allow_view=true and max_rows_to_read=%d", redacted.Activity, maxRows)
	}
}

func TestInspectSourceViewActivityClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rawConn, err := ch.Open(&ch.Options{
		Addr: []string{addr},
		Auth: ch.Auth{Database: "default"},
	})
	if err != nil {
		t.Fatalf("open ClickHouse admin connection: %v", err)
	}
	t.Cleanup(func() { _ = rawConn.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	baseTable := "test_logs_base_" + suffix
	viewName := "test_logs_view_" + suffix
	tightViewName := "test_logs_tight_view_" + suffix
	slowViewName := "test_logs_slow_view_" + suffix

	if err := rawConn.Exec(ctx, fmt.Sprintf(`CREATE TABLE default.%s (timestamp DateTime64(3), service LowCardinality(String), body String) ENGINE = MergeTree ORDER BY (timestamp, service)`, baseTable)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = rawConn.Exec(cleanupCtx, "DROP VIEW IF EXISTS default."+slowViewName)
		_ = rawConn.Exec(cleanupCtx, "DROP VIEW IF EXISTS default."+tightViewName)
		_ = rawConn.Exec(cleanupCtx, "DROP VIEW IF EXISTS default."+viewName)
		_ = rawConn.Exec(cleanupCtx, "DROP TABLE IF EXISTS default."+baseTable)
	})
	if err := rawConn.Exec(ctx, fmt.Sprintf(`CREATE VIEW default.%s AS SELECT timestamp, service, body FROM default.%s WHERE service = 'api'`, viewName, baseTable)); err != nil {
		t.Fatal(err)
	}
	if err := rawConn.Exec(ctx, fmt.Sprintf(`CREATE VIEW default.%s AS SELECT timestamp, service, body FROM default.%s WHERE service = 'api'`, tightViewName, baseTable)); err != nil {
		t.Fatal(err)
	}
	if err := rawConn.Exec(ctx, fmt.Sprintf(`CREATE VIEW default.%s AS SELECT toDateTime64(now64(3) - toIntervalMinute(1), 3) AS timestamp FROM numbers(3000000) WHERE sleepEachRow(0.000002) = 0`, slowViewName)); err != nil {
		t.Fatal(err)
	}
	// Insert 10 'api' rows and 5 'worker' rows within the last hour so we can verify
	// that activity queries run through the view (counting 10, not 15).
	if err := rawConn.Exec(ctx, fmt.Sprintf(`INSERT INTO default.%s SELECT now64(3) - toIntervalMinute(number + 1), if(number < 10, 'api', 'worker'), toString(number) FROM numbers(15)`, baseTable)); err != nil {
		t.Fatal(err)
	}

	readActivityQueryLog := func(targetView, logType string) (map[string]string, int32) {
		t.Helper()
		if err := rawConn.Exec(ctx, "SYSTEM FLUSH LOGS"); err != nil {
			t.Fatalf("SYSTEM FLUSH LOGS: %v", err)
		}
		var (
			settings      map[string]string
			exceptionCode int32
		)
		err := rawConn.QueryRow(
			ctx,
			`SELECT Settings, exception_code
			 FROM system.query_log
			 WHERE type = ?
			   AND query LIKE ?
			   AND query LIKE '%toStartOfHour%'
			   AND query NOT LIKE '%system.query_log%'
			 ORDER BY event_time_microseconds DESC
			 LIMIT 1`,
			logType,
			"%"+targetView+"%",
		).Scan(&settings, &exceptionCode)
		if err != nil {
			t.Fatalf("query system.query_log (%s, %s): %v", targetView, logType, err)
		}
		return settings, exceptionCode
	}

	provider := NewClickHouseProvider(manager, log)

	// 1. Default view source (no opt-in): activity is unavailable and storage is nil.
	defaultViewSource := &models.Source{
		ID:          101,
		SourceType:  models.SourceTypeClickHouse,
		MetaTSField: "timestamp",
		Connection:  models.ConnectionInfo{Host: addr, Database: "default", TableName: viewName},
	}
	if err := manager.AddSource(ctx, defaultViewSource); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.InspectSourceActivity(ctx, defaultViewSource); !errors.Is(err, ErrSourceActivityUnavailable) {
		t.Fatalf("default view activity err = %v, want ErrSourceActivityUnavailable", err)
	}
	inspection, err := provider.InspectSource(ctx, defaultViewSource)
	if err != nil {
		t.Fatalf("InspectSource(view): %v", err)
	}
	if inspection.Storage != nil {
		t.Fatalf("InspectSource(view) Storage = %#v, want nil", inspection.Storage)
	}

	// 2. Opted-in view source with permissive source settings: activity queries through the view,
	// counts only 'api' rows, and ClickHouse's own system.query_log observes max_execution_time=3
	// (not 120 and not clickhouse-go's deadline+5=7 overwrite) plus throw overflow modes.
	permissiveTimeout := 120
	breakMode := "break"
	optedInViewSource := &models.Source{
		ID:          102,
		SourceType:  models.SourceTypeClickHouse,
		MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{
			Host:      addr,
			Database:  "default",
			TableName: viewName,
			Settings: &models.ClickHouseQuerySettings{
				MaxExecutionTime:   &permissiveTimeout,
				ResultOverflowMode: &breakMode,
			},
			Activity: &models.ClickHouseActivityConfig{AllowView: true},
		},
	}
	if err := manager.AddSource(ctx, optedInViewSource); err != nil {
		t.Fatal(err)
	}
	fillCtx, fillCancel := context.WithTimeout(ctx, 4*time.Second)
	defer fillCancel()
	activity, err := provider.InspectSourceActivity(fillCtx, optedInViewSource)
	if err != nil {
		t.Fatalf("opted-in view activity failed: %v", err)
	}
	if activity.Rows24h != 10 || activity.Rows1h != 10 {
		t.Fatalf("opted-in view activity = %+v, want 10 rows (through view filter)", activity)
	}
	observedSettings, _ := readActivityQueryLog(viewName, "QueryFinish")
	wantSettings := map[string]string{
		"max_execution_time": "3",
		"max_rows_to_read":   "50000000",
		"max_bytes_to_read":  "1073741824",
		"max_threads":        "2",
	}
	for k, want := range wantSettings {
		if got := observedSettings[k]; got != want {
			t.Fatalf("system.query_log Settings[%q] = %q, want %q (all settings: %v)", k, got, want, observedSettings)
		}
	}
	// system.query_log.Settings only records settings that differ from the server
	// default ("throw"), so overriding source "break" back to "throw" leaves them
	// either omitted ("") or "throw", never "break".
	for _, modeKey := range []string{"read_overflow_mode", "timeout_overflow_mode", "result_overflow_mode"} {
		if got := observedSettings[modeKey]; got != "" && got != "throw" {
			t.Fatalf("system.query_log Settings[%q] = %q, want default/throw (all settings: %v)", modeKey, got, observedSettings)
		}
	}

	// 3. Opted-in view source with tighter max_execution_time=1 preserves 1s server-side
	// and aborts a slow view with server TIMEOUT_EXCEEDED (159) rather than waiting 3s/7s.
	tightTimeout := 1
	tightViewSource := &models.Source{
		ID:          103,
		SourceType:  models.SourceTypeClickHouse,
		MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{
			Host:      addr,
			Database:  "default",
			TableName: tightViewName,
			Settings:  &models.ClickHouseQuerySettings{MaxExecutionTime: &tightTimeout},
			Activity:  &models.ClickHouseActivityConfig{AllowView: true},
		},
	}
	if err := manager.AddSource(ctx, tightViewSource); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.InspectSourceActivity(fillCtx, tightViewSource); err != nil {
		t.Fatalf("tight-timeout view activity failed: %v", err)
	}
	tightObserved, _ := readActivityQueryLog(tightViewName, "QueryFinish")
	if got := tightObserved["max_execution_time"]; got != "1" {
		t.Fatalf("tight-timeout system.query_log Settings['max_execution_time'] = %q, want \"1\"", got)
	}

	slowViewSource := &models.Source{
		ID:          104,
		SourceType:  models.SourceTypeClickHouse,
		MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{
			Host:      addr,
			Database:  "default",
			TableName: slowViewName,
			Settings:  &models.ClickHouseQuerySettings{MaxExecutionTime: &tightTimeout},
			Activity:  &models.ClickHouseActivityConfig{AllowView: true},
		},
	}
	if err := manager.AddSource(ctx, slowViewSource); err != nil {
		t.Fatal(err)
	}
	slowStart := time.Now()
	slowCallCtx, slowCallCancel := context.WithTimeout(ctx, 4*time.Second)
	defer slowCallCancel()
	_, err = provider.InspectSourceActivity(slowCallCtx, slowViewSource)
	slowElapsed := time.Since(slowStart)
	if !errors.Is(err, ErrSourceActivityLimitExceeded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow view activity err = %v, want activity limit and deadline errors", err)
	}
	if slowElapsed > 2500*time.Millisecond {
		t.Fatalf("slow view activity took %v with max_execution_time=1, want <2.5s", slowElapsed)
	}
	slowObserved, slowCode := readActivityQueryLog(slowViewName, "ExceptionWhileProcessing")
	if got := slowObserved["max_execution_time"]; got != "1" {
		t.Fatalf("slow view system.query_log Settings['max_execution_time'] = %q, want \"1\"", got)
	}
	if slowCode != 159 {
		t.Fatalf("slow view system.query_log exception_code = %d, want 159 (TIMEOUT_EXCEEDED)", slowCode)
	}

	// 4. Opted-in view exceeding max_rows_to_read fails explicitly instead of returning partial counts,
	// even if connection.settings configures result_overflow_mode="break".
	tinyCap := int64(2)
	cappedViewSource := &models.Source{
		ID:          105,
		SourceType:  models.SourceTypeClickHouse,
		MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{
			Host:      addr,
			Database:  "default",
			TableName: viewName,
			Settings:  &models.ClickHouseQuerySettings{ResultOverflowMode: &breakMode},
			Activity:  &models.ClickHouseActivityConfig{AllowView: true, MaxRowsToRead: &tinyCap},
		},
	}
	if err := manager.AddSource(ctx, cappedViewSource); err != nil {
		t.Fatal(err)
	}
	cappedActivity, err := provider.InspectSourceActivity(ctx, cappedViewSource)
	if !errors.Is(err, ErrSourceActivityLimitExceeded) {
		t.Fatalf("capped view activity = %+v, %v; want ErrSourceActivityLimitExceeded", cappedActivity, err)
	}
}

func TestCompileLogchefQLWithoutColumnsDoesNotMutateSource(t *testing.T) {
	provider := &ClickHouseProvider{}
	source := &models.Source{}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			result, err := provider.CompileLogchefQL(context.Background(), source, LogchefQLCompileRequest{Query: `message = "hello"`})
			if err != nil {
				t.Error(err)
				return
			}
			if !result.Valid {
				t.Error("expected valid best-effort translation")
			}
		})
	}
	wg.Wait()
	if source.Columns != nil {
		t.Fatal("compilation mutated source columns")
	}
}

func TestClickHouseProviderSourceMetadata(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(nil, log)
	provider := NewClickHouseProvider(nil, log)
	service.Register(provider)
	source := &models.Source{SourceType: models.SourceTypeClickHouse}
	if err := service.ApplySourceMetadata(source); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"query languages", source.QueryLanguages, []models.QueryLanguage{models.QueryLanguageLogchefQL, models.QueryLanguageClickHouseSQL}},
		{"saved query editor modes", source.SavedQueryEditorModes, []models.SavedQueryEditorMode{models.SavedQueryEditorModeBuilder, models.SavedQueryEditorModeNative}},
		{"alert editor modes", source.AlertEditorModes, []models.AlertEditorMode{models.AlertEditorModeCondition, models.AlertEditorModeNative}},
		{"capabilities", source.Capabilities, []string{"schema_inspection", "histogram", "field_values", "source_inspection", "ai_sql_generation", "log_context", "exports", "live_tail"}},
		{"logchefql needs columns", provider.Describe().LogchefQLNeedsColumns, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

// TestClassifyActivityError keeps a timeout on an ordinary source retryable:
// only an opted-in View, which runs under a fixed time limit, turns a timeout
// into ErrSourceActivityLimitExceeded.
func TestClassifyActivityError(t *testing.T) {
	timeout := fmt.Errorf("query: %w", context.DeadlineExceeded)
	readCap := &ch.Exception{Code: 158, Message: "limit for rows to read exceeded"}
	tests := []struct {
		name    string
		err     error
		enforce bool
		limit   bool
	}{
		{"timeout on a table source stays a timeout", timeout, false, false},
		{"timeout on an opted-in View is a limit", timeout, true, true},
		{"read cap on a table source is a limit", readCap, false, true},
		{"read cap on an opted-in View is a limit", readCap, true, true},
		{"other errors pass through", errors.New("boom"), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyActivityError(tt.err, tt.enforce)
			if errors.Is(got, ErrSourceActivityLimitExceeded) != tt.limit {
				t.Fatalf("limit = %v, want %v (%v)", !tt.limit, tt.limit, got)
			}
			if !errors.Is(got, tt.err) {
				t.Fatalf("lost the cause: %v", got)
			}
		})
	}
}

// TestUpdateSourceNeverSendsStoredPasswordToNewHost refuses to reuse the
// stored password when the host, username or TLS setting changes, before any
// connection.
func TestUpdateSourceNeverSendsStoredPasswordToNewHost(t *testing.T) {
	provider := NewClickHouseProvider(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for name, conn := range map[string]string{
		"new host":       `{"host":"other.example:9000","username":"reader","password":"","database":"logs","table_name":"app","tls_enable":true}`,
		"new username":   `{"host":"ch.example:9000","username":"admin","password":"","database":"logs","table_name":"app","tls_enable":true}`,
		"TLS turned off": `{"host":"ch.example:9000","username":"reader","password":"","database":"logs","table_name":"app","tls_enable":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := &models.Source{
				SourceType: models.SourceTypeClickHouse,
				Connection: models.ConnectionInfo{Host: "ch.example:9000", Username: "reader", Password: "stored-secret", Database: "logs", TableName: "app", TLSEnable: true},
			}
			_, err := provider.UpdateSource(context.Background(), source, &models.UpdateSourceRequest{Connection: json.RawMessage(conn)})
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Field != "connection.password" {
				t.Fatalf("err = %v, want a connection.password validation error", err)
			}
		})
	}
}
