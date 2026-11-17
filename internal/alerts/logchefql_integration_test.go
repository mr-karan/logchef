package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/loki"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

// conditionRow is one log line, written ago before the evaluation instant.
type conditionRow struct {
	ago     time.Duration
	level   string
	service string
	dur     float64
}

// The rule's lookback is 300 seconds. The window that ends at the instant
// holds three errors: checkout 10 and 20, search 30. The window before it
// holds one error, checkout 7. The two edge rows sit on the bounds of the
// window, where a backend's own rule decides whether they count.
var conditionRows = []conditionRow{
	{0, "edge", "checkout", 1},
	{10 * time.Second, "error", "checkout", 10},
	{20 * time.Second, "error", "checkout", 20},
	{30 * time.Second, "error", "search", 30},
	{40 * time.Second, "info", "search", 1000},
	{conditionLookback * time.Second, "edge", "checkout", 1},
	{450 * time.Second, "error", "checkout", 7},
}

const conditionLookback = 300

// conditionEnv is one real backend that holds conditionRows.
type conditionEnv struct {
	db       *sqlite.DB
	service  *datasource.Service
	manager  *Manager
	source   *models.Source
	template *models.Alert
	at       time.Time
	// grouped reports that the backend compiles a LogchefQL aggregation with
	// by keys.
	grouped bool
}

func newConditionEnv(t *testing.T, provider datasource.Provider, source *models.Source, at time.Time, grouped bool) *conditionEnv {
	t.Helper()
	db, template, logger := newManagerTestAlert(t)
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	service := datasource.NewService(db, logger)
	service.Register(provider)
	if err := service.InitializeSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Options{DB: db, Logger: logger, Datasources: service, Config: config.AlertsConfig{HistoryLimit: 100}})
	return &conditionEnv{db: db, service: service, manager: manager, source: source, template: template, at: at, grouped: grouped}
}

// evaluationInstant is a whole second far enough in the past that every
// backend has made the rows searchable by the time the test reads them.
func evaluationInstant() time.Time {
	return time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
}

// legacyConditionQuery is the native query that the alert form generated
// for a condition before the server compiled conditions
// (frontend/src/composables/useAlertForm.ts before RFC 0006 unit Q1). It
// builds on the form's translation of the filter.
func (e *conditionEnv) legacyConditionQuery(t *testing.T, condition, aggregate string) (language models.QueryLanguage, query string) {
	t.Helper()
	filter, err := e.service.CompileLogchefQL(t.Context(), e.source.ID, datasource.LogchefQLCompileRequest{Query: condition})
	if err != nil {
		t.Fatal(err)
	}
	switch filter.Language {
	case models.QueryLanguageLogsQL:
		stats := "count()"
		if aggregate != "count" {
			stats = aggregate + "(dur)"
		}
		return filter.Language, fmt.Sprintf("%s | stats %s as value", filter.Query, stats)
	case models.QueryLanguageLogQL:
		window := fmt.Sprintf("[%ds]", conditionLookback)
		unwrapped := fmt.Sprintf(`%s | unwrap dur | __error__="" %s`, filter.Query, window)
		switch aggregate {
		case "count":
			return filter.Language, fmt.Sprintf("sum(count_over_time(%s %s))", filter.Query, window)
		case "avg":
			return filter.Language, fmt.Sprintf("avg_over_time(%s) by ()", unwrapped)
		default:
			return filter.Language, fmt.Sprintf("%s(%s_over_time(%s))", aggregate, aggregate, unwrapped)
		}
	default:
		value := "count(*)"
		if aggregate != "count" {
			value = aggregate + "(`dur`)"
		}
		return filter.Language, fmt.Sprintf("SELECT %s as value\nFROM %s\nWHERE (%s) AND {{__time_filter}}", value, e.source.GetFullTableName(), filter.FilterOnly)
	}
}

// samples evaluates an ungrouped query at instant and returns its samples.
func (e *conditionEnv) samples(t *testing.T, language models.QueryLanguage, query string, instant time.Time) []Sample {
	t.Helper()
	result, err := e.service.EvaluateAlert(t.Context(), e.source.ID, datasource.AlertQueryRequest{
		At: instant, Language: language, Query: query, LookbackSeconds: conditionLookback,
	})
	if err != nil {
		t.Fatalf("evaluate %s %q: %v", language, query, err)
	}
	samples, err := SamplesFromResult(result, nil, language)
	if err != nil {
		t.Fatalf("samples of %s %q: %v", language, query, err)
	}
	return samples
}

// waitForRows waits until the backend counts the three errors of the window.
func (e *conditionEnv) waitForRows(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		got := e.samples(t, models.QueryLanguageLogchefQL, `level="error" | count()`, e.at)
		if len(got) == 1 && got[0].Value == 3 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the backend counts %+v errors, want 3", got)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func runLogchefQLConditionTests(t *testing.T, env *conditionEnv) {
	env.waitForRows(t)

	t.Run("a condition saved before the change evaluates to the same value", func(t *testing.T) {
		for _, tc := range []struct {
			aggregate string
			logchefql string
			want      float64
		}{
			{"count", `level="error" | count() as value`, 3},
			{"sum", `level="error" | sum(dur) as value`, 60},
			{"avg", `level="error" | avg(dur) as value`, 20},
			{"min", `level="error" | min(dur) as value`, 10},
			{"max", `level="error" | max(dur) as value`, 30},
		} {
			language, legacy := env.legacyConditionQuery(t, `level="error"`, tc.aggregate)
			before := env.samples(t, language, legacy, env.at)
			after := env.samples(t, models.QueryLanguageLogchefQL, tc.logchefql, env.at)
			if len(before) != 1 || len(after) != 1 || before[0].Value != tc.want || after[0].Value != tc.want {
				t.Errorf("%s: saved condition %q reads %+v, compiled %q reads %+v; want %v from both", tc.aggregate, legacy, before, tc.logchefql, after, tc.want)
			}
		}
		// The window's bounds count the same rows before and after.
		language, legacy := env.legacyConditionQuery(t, `level="edge"`, "count")
		before := env.samples(t, language, legacy, env.at)
		after := env.samples(t, models.QueryLanguageLogchefQL, `level="edge" | count()`, env.at)
		if !reflect.DeepEqual(before, after) {
			t.Errorf("rows on the bounds: saved condition reads %+v, compiled reads %+v", before, after)
		} else {
			t.Logf("rows on the bounds of the window counted: %+v", after)
		}
	})

	t.Run("an aggregate without numeric input is no sample", func(t *testing.T) {
		if got := env.samples(t, models.QueryLanguageLogchefQL, `level="nomatch" | sum(dur)`, env.at); len(got) != 0 {
			t.Fatalf("samples = %+v, want none", got)
		}
	})

	t.Run("a grouped condition fires per group", func(t *testing.T) {
		query := `level="error" | count() by service as svc`
		condition, err := env.service.CompileAlertCondition(t.Context(), env.source.ID, query, conditionLookback)
		if !env.grouped {
			if err == nil || !strings.Contains(err.Error(), "Write a native LogQL query to group") {
				t.Fatalf("compile %q: %v, want the Loki grouping refusal", query, err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(condition.GroupBy, []string{"svc"}) {
			t.Fatalf("group_by = %q, want svc", condition.GroupBy)
		}
		alert := *env.template
		alert.SourceID = env.source.ID
		alert.QueryLanguage, alert.EditorMode = models.QueryLanguageLogchefQL, models.AlertEditorModeCondition
		alert.ConditionJSON, alert.Query, alert.GroupBy = `level="error"`, query, condition.GroupBy
		alert.ThresholdOperator, alert.ThresholdValue = models.AlertThresholdGreaterThanOrEqual, 1
		if err := env.db.CreateAlert(t.Context(), &alert); err != nil {
			t.Fatal(err)
		}
		if err := env.manager.evaluateAlert(t.Context(), &alert, env.at); err != nil {
			t.Fatal(err)
		}
		instances, err := env.db.ListAlertInstances(t.Context(), alert.ID)
		if err != nil {
			t.Fatal(err)
		}
		firing := map[string]float64{}
		for _, instance := range instances {
			if instance.State == models.AlertInstanceStateFiring && instance.Value != nil {
				firing[instance.Group["svc"]] = *instance.Value
			}
		}
		if len(firing) != 2 || firing["checkout"] != 2 || firing["search"] != 1 {
			t.Fatalf("firing groups = %v, want checkout 2 and search 1", firing)
		}
	})

	t.Run("a backtest replays the compiled condition", func(t *testing.T) {
		rule := BacktestRule{
			Rule:     Rule{Operator: models.AlertThresholdGreaterThanOrEqual, Threshold: 3, NoData: models.AlertNoDataZero},
			Language: models.QueryLanguageLogchefQL, FrequencySeconds: conditionLookback, LookbackSeconds: conditionLookback, NotifyOnResolve: true,
		}
		query := func(ctx context.Context, at time.Time) (*models.QueryResult, error) {
			return env.service.EvaluateAlert(ctx, env.source.ID, datasource.AlertQueryRequest{
				At: at, Language: models.QueryLanguageLogchefQL, Query: `level="error" | count()`, LookbackSeconds: conditionLookback,
			})
		}
		result, err := Backtest(t.Context(), rule, env.at, 3600, query)
		if err != nil {
			t.Fatal(err)
		}
		series := result.Series
		if result.Evaluations != 12 || len(series) != 12 || result.Summary.Errors != 0 {
			t.Fatalf("evaluations=%d series=%d errors=%d first error %q", result.Evaluations, len(series), result.Summary.Errors, result.FirstError)
		}
		last, previous := series[len(series)-1], series[len(series)-2]
		if !last.T.Equal(env.at) || last.Value == nil || *last.Value != 3 || last.Status != models.BacktestStatusFiring {
			t.Errorf("evaluation at the instant = %+v, want 3 and firing", last)
		}
		if previous.Value == nil || *previous.Value != 1 || previous.Status != models.BacktestStatusOK {
			t.Errorf("evaluation one step earlier = %+v, want 1 and ok", previous)
		}
	})
}

// TestLogchefQLConditionClickHouse compiles conditions on a real ClickHouse
// table (RFC 0006 unit Q1).
func TestLogchefQLConditionClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	at := evaluationInstant()
	conn, err := ch.Open(&ch.Options{Addr: []string{addr}, Protocol: ch.Native})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	table := fmt.Sprintf("alert_conditions_%d", time.Now().UnixNano())
	if err := conn.Exec(t.Context(), fmt.Sprintf("CREATE TABLE default.%s (timestamp DateTime64(3, 'UTC'), level String, service String, dur Float64) ENGINE = MergeTree ORDER BY timestamp", table)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DROP TABLE default."+table) })
	batch, err := conn.PrepareBatch(t.Context(), "INSERT INTO default."+table)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range conditionRows {
		if err := batch.Append(at.Add(-row.ago), row.level, row.service, row.dur); err != nil {
			t.Fatal(err)
		}
	}
	if err := batch.Send(); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.DiscardHandler)
	connections := clickhouse.NewManager(logger)
	t.Cleanup(func() { _ = connections.Close() })
	source := &models.Source{Name: "ch conditions", MetaTSField: "timestamp", Connection: models.ConnectionInfo{Host: addr, Database: "default", TableName: table}}
	runLogchefQLConditionTests(t, newConditionEnv(t, datasource.NewClickHouseProvider(connections, logger), source, at, true))
}

// TestLogchefQLConditionVictoriaLogs compiles conditions on a real
// VictoriaLogs (RFC 0006 unit Q1).
func TestLogchefQLConditionVictoriaLogs(t *testing.T) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("LOGCHEF_TEST_VICTORIALOGS_URL")), "/")
	if baseURL == "" {
		t.Skip("set LOGCHEF_TEST_VICTORIALOGS_URL to run against VictoriaLogs")
	}
	at := evaluationInstant()
	runID := fmt.Sprintf("alert_conditions_%d", time.Now().UnixNano())
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, row := range conditionRows {
		record := map[string]any{
			"_time": at.Add(-row.ago).Format(time.RFC3339Nano), "_msg": "request", "test_run": runID,
			"level": row.level, "service": row.service, "dur": fmt.Sprint(row.dur),
		}
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := http.Post(baseURL+"/insert/jsonline?_time_field=_time&_msg_field=_msg", "application/stream+json", &body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("ingest status %d", resp.StatusCode)
	}

	connection, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: fmt.Sprintf("test_run:=%q", runID)}})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "vl conditions", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: connection}
	runLogchefQLConditionTests(t, newConditionEnv(t, victorialogs.NewProvider(slog.New(slog.DiscardHandler)), source, at, true))
}

// TestLogchefQLConditionLoki compiles conditions on a real Loki (RFC 0006
// unit Q1). Loki compiles one ungrouped aggregate.
func TestLogchefQLConditionLoki(t *testing.T) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("LOGCHEF_TEST_LOKI_URL")), "/")
	if baseURL == "" {
		t.Skip("set LOGCHEF_TEST_LOKI_URL to run against Loki")
	}
	at := evaluationInstant()
	runID := fmt.Sprintf("conditions%d", time.Now().UnixNano())
	tenant := "logchef-" + runID
	values := make([][2]string, 0, len(conditionRows))
	for _, row := range slices.Backward(conditionRows) {
		line := fmt.Sprintf("level=%s service=%s dur=%v", row.level, row.service, row.dur)
		values = append(values, [2]string{fmt.Sprint(at.Add(-row.ago).UnixNano()), line})
	}
	body, err := json.Marshal(map[string]any{"streams": []map[string]any{{"stream": map[string]string{"test_run": runID}, "values": values}}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL+"/loki/api/v1/push", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scope-OrgID", tenant)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("push status %d", resp.StatusCode)
	}

	connection, err := json.Marshal(models.LokiConnectionInfo{BaseURL: baseURL, TenantID: tenant, Selector: fmt.Sprintf(`{test_run=%q}`, runID), Parser: "logfmt"})
	if err != nil {
		t.Fatal(err)
	}
	provider := loki.NewProvider(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = provider.Close() })
	source := &models.Source{Name: "loki conditions", SourceType: models.SourceTypeLoki, MetaTSField: "_time", ConnectionConfig: connection}
	runLogchefQLConditionTests(t, newConditionEnv(t, provider, source, at, false))
}
