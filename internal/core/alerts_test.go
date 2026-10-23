package core

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestUserCanEditAlert(t *testing.T) {
	t.Parallel()

	creator := models.UserID(42)
	other := models.UserID(99)

	cases := []struct {
		name  string
		alert *models.Alert
		user  *models.User
		want  bool
	}{
		{
			name:  "nil alert",
			alert: nil,
			user:  &models.User{ID: creator},
			want:  false,
		},
		{
			name:  "nil user",
			alert: &models.Alert{CreatedBy: &creator},
			user:  nil,
			want:  false,
		},
		{
			name:  "global admin always allowed",
			alert: &models.Alert{CreatedBy: &other},
			user:  &models.User{ID: creator, Role: models.UserRoleAdmin},
			want:  true,
		},
		{
			name:  "global admin allowed on legacy NULL-creator alert",
			alert: &models.Alert{CreatedBy: nil},
			user:  &models.User{ID: creator, Role: models.UserRoleAdmin},
			want:  true,
		},
		{
			name:  "creator allowed",
			alert: &models.Alert{CreatedBy: &creator},
			user:  &models.User{ID: creator, Role: models.UserRoleMember},
			want:  true,
		},
		{
			name:  "non-creator member denied",
			alert: &models.Alert{CreatedBy: &creator},
			user:  &models.User{ID: other, Role: models.UserRoleMember},
			want:  false,
		},
		{
			name:  "legacy NULL-creator alert denied for non-admin",
			alert: &models.Alert{CreatedBy: nil},
			user:  &models.User{ID: creator, Role: models.UserRoleMember},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := UserCanEditAlert(tc.alert, tc.user); got != tc.want {
				t.Errorf("UserCanEditAlert(%+v, %+v) = %v, want %v", tc.alert, tc.user, got, tc.want)
			}
		})
	}
}

func newTestCreateAlertRequest() *models.CreateAlertRequest {
	return &models.CreateAlertRequest{
		Name:              "5xx spike",
		QueryLanguage:     models.QueryLanguageClickHouseSQL,
		EditorMode:        models.AlertEditorModeNative,
		Query:             "SELECT count() FROM logs",
		LookbackSeconds:   300,
		ThresholdOperator: models.AlertThresholdGreaterThan,
		ThresholdValue:    10,
		FrequencySeconds:  60,
		Severity:          models.AlertSeverityWarning,
		IsActive:          true,
	}
}

func TestClickHouseAlertQueryValidation(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	owner := newTestUser(t, db, "sql-owner@example.com", "Owner")
	source := newTestSource(t, db, "sql-alerts")
	ds := datasource.NewService(db, log)
	ds.Register(datasource.NewClickHouseProvider(nil, log))

	req := newTestCreateAlertRequest()
	alert, err := CreateAlert(ctx, db, ds, log, source.ID, owner.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"DROP TABLE x",
		"SELECT 1; DROP TABLE x",
		// Valid ClickHouse that Explore's raw SQL path also rejects.
		"SELECT count() FROM logs # comment",
		"SELECT /* a /* b */ c */ count() FROM logs",
		"WITH RECURSIVE t AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM t WHERE n < 3) SELECT count() FROM t",
		"SELECT count() FILTER (WHERE status >= 500) FROM logs",
	} {
		t.Run(query, func(t *testing.T) {
			badRequest := newTestCreateAlertRequest()
			badRequest.Query = query
			if _, err := CreateAlert(ctx, db, ds, log, source.ID, owner.ID, badRequest); !isInvalidAlertQuery(err) {
				t.Errorf("CreateAlert() error = %v, want invalid alert query", err)
			}
			if _, err := UpdateAlert(ctx, db, ds, log, alert.ID, &models.UpdateAlertRequest{Query: &query}); !isInvalidAlertQuery(err) {
				t.Errorf("UpdateAlert() error = %v, want invalid alert query", err)
			}
			stored, err := db.GetAlert(ctx, alert.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Query != req.Query {
				t.Errorf("stored query = %q, want %q", stored.Query, req.Query)
			}
		})
	}

	blank := newTestCreateAlertRequest()
	blank.Query = "   "
	if _, err := CreateAlert(ctx, db, ds, log, source.ID, owner.ID, blank); err == nil || !strings.Contains(err.Error(), "query is required for native alerts") {
		t.Errorf("CreateAlert(blank query) error = %v, want query is required", err)
	}

	for _, aggregate := range []string{"count(*)", "sum(`duration`)", "avg(`duration`)", "min(`duration`)", "max(`duration`)"} {
		t.Run(aggregate, func(t *testing.T) {
			conditionRequest := newTestCreateAlertRequest()
			conditionRequest.EditorMode = models.AlertEditorModeCondition
			conditionRequest.ConditionJSON = `severity = "ERROR"`
			conditionRequest.Query = "SELECT " + aggregate + " as value\nFROM default.logs\nWHERE (severity = 'ERROR') AND `timestamp` >= now() - toIntervalSecond(300)"
			created, err := CreateAlert(ctx, db, ds, log, source.ID, owner.ID, conditionRequest)
			if err != nil {
				t.Fatal(err)
			}
			if created.Query != conditionRequest.Query {
				t.Errorf("query changed to %q, want %q", created.Query, conditionRequest.Query)
			}
		})
	}
}

func TestUpdateAlertWithUnparseableStoredQuery(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	owner := newTestUser(t, db, "legacy-owner@example.com", "Owner")
	source := newTestSource(t, db, "legacy-alerts")
	ds := datasource.NewService(db, log)
	ds.Register(datasource.NewClickHouseProvider(nil, log))

	legacyQuery := "SELECT count() FROM logs # saved before validation"
	alert := &models.Alert{
		SourceID:          source.ID,
		Name:              "legacy",
		QueryLanguage:     models.QueryLanguageClickHouseSQL,
		EditorMode:        models.AlertEditorModeNative,
		Query:             legacyQuery,
		LookbackSeconds:   300,
		ThresholdOperator: models.AlertThresholdGreaterThan,
		ThresholdValue:    10,
		FrequencySeconds:  60,
		Severity:          models.AlertSeverityWarning,
		IsActive:          true,
		CreatedBy:         &owner.ID,
	}
	if err := db.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}

	inactive := false
	renamed := "legacy (paused)"
	updated, err := UpdateAlert(ctx, db, ds, log, alert.ID, &models.UpdateAlertRequest{Name: &renamed, IsActive: &inactive, Query: &legacyQuery})
	if err != nil {
		t.Fatalf("UpdateAlert(pause, unchanged query) error = %v", err)
	}
	if updated.IsActive || updated.Name != renamed || updated.Query != legacyQuery {
		t.Errorf("UpdateAlert() = active %v name %q query %q", updated.IsActive, updated.Name, updated.Query)
	}

	edited := "SELECT count() FROM logs # edited"
	if _, err := UpdateAlert(ctx, db, ds, log, alert.ID, &models.UpdateAlertRequest{Query: &edited}); !isInvalidAlertQuery(err) {
		t.Errorf("UpdateAlert(changed query) error = %v, want invalid alert query", err)
	}
}

func TestTestAlertQueryReportsTruncation(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := newTestDB(t)
	log := discardLogger()
	source := &models.Source{Name: "truncation", Connection: models.ConnectionInfo{Host: addr, Database: "system", TableName: "numbers"}}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	ds := datasource.NewService(db, log)
	ds.Register(datasource.NewClickHouseProvider(manager, log))
	request := func(query string) *models.TestAlertQueryRequest {
		return &models.TestAlertQueryRequest{
			QueryLanguage:     models.QueryLanguageClickHouseSQL,
			EditorMode:        models.AlertEditorModeNative,
			Query:             query,
			ThresholdOperator: models.AlertThresholdGreaterThan,
			ThresholdValue:    10,
		}
	}

	resp, err := testAlertQuery(ctx, db, ds, source.ID, request("SELECT number AS value FROM numbers(1000) WHERE number >= 0"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(resp.Warnings, "Results were capped at 100 buffered rows. Alert queries should typically return a single numeric value (e.g., count, sum, avg).") {
		t.Errorf("testAlertQuery() warnings = %q, want the 100-row cap reported", resp.Warnings)
	}

	if resp, err := testAlertQuery(ctx, db, ds, source.ID, request("SELECT count() AS value, repeat('xx', 600000) AS detail FROM numbers(1501)")); err == nil {
		t.Errorf("testAlertQuery(first row over byte limit) = %+v, want error", resp)
	}
}

func isInvalidAlertQuery(err error) bool {
	return errors.Is(err, ErrInvalidAlertConfiguration) && strings.Contains(err.Error(), "invalid alert query")
}

// TestCreateAlertFullLifecycle exercises CreateAlert -> GetAlert -> UpdateAlert
// -> ListAlertsBySource/ForUser -> DeleteAlert -> GetAlert(not found) through a
// real sqlite store with a fakeProvider standing in for ClickHouse/VictoriaLogs.
// This is the "how a due alert's query/condition gets dispatched" seam: the
// wiring from core through datasource.Service to the registered provider.
func TestCreateAlertFullLifecycle(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "alert-owner@example.com", "Owner")
	src := newTestSource(t, db, "alert-src")
	ds := newFakeDatasourceService(db, log, nil)

	req := newTestCreateAlertRequest()
	alert, err := CreateAlert(ctx, db, ds, log, src.ID, owner.ID, req)
	if err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}
	if alert.ID == 0 {
		t.Fatalf("CreateAlert did not populate ID")
	}
	if alert.CreatedBy == nil || *alert.CreatedBy != owner.ID {
		t.Errorf("CreatedBy = %+v, want %d", alert.CreatedBy, owner.ID)
	}

	got, err := GetAlert(ctx, db, log, alert.ID)
	if err != nil || got.Name != "5xx spike" {
		t.Fatalf("GetAlert: %v / %+v", err, got)
	}

	newName := "5xx spike (updated)"
	newThreshold := 20.0
	updated, err := UpdateAlert(ctx, db, ds, log, alert.ID, &models.UpdateAlertRequest{
		Name:           &newName,
		ThresholdValue: &newThreshold,
	})
	if err != nil {
		t.Fatalf("UpdateAlert: %v", err)
	}
	if updated.Name != newName || updated.ThresholdValue != newThreshold {
		t.Errorf("UpdateAlert result = %+v, want name=%q threshold=%v", updated, newName, newThreshold)
	}
	// Fields not touched by the update must survive untouched.
	if updated.ThresholdOperator != models.AlertThresholdGreaterThan {
		t.Errorf("untouched ThresholdOperator changed: %v", updated.ThresholdOperator)
	}

	bySrc, err := ListAlertsBySource(ctx, db, src.ID)
	if err != nil || len(bySrc) != 1 {
		t.Fatalf("ListAlertsBySource: %v / %d", err, len(bySrc))
	}

	if err := DeleteAlert(ctx, db, log, alert.ID); err != nil {
		t.Fatalf("DeleteAlert: %v", err)
	}
	if _, err := GetAlert(ctx, db, log, alert.ID); !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("GetAlert(deleted) err = %v, want ErrAlertNotFound", err)
	}
}

// TestCreateAlertValidation pins the validation seam callers rely on: bad
// alert configurations are rejected with ErrInvalidAlertConfiguration before
// ever reaching the store.
func TestCreateAlertValidation(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "validation-owner@example.com", "Owner")
	src := newTestSource(t, db, "validation-src")
	ds := newFakeDatasourceService(db, log, nil)

	cases := []struct {
		name    string
		mutate  func(*models.CreateAlertRequest)
		wantErr bool
	}{
		{name: "valid baseline", mutate: func(*models.CreateAlertRequest) {}, wantErr: false},
		{name: "missing name", mutate: func(r *models.CreateAlertRequest) { r.Name = "  " }, wantErr: true},
		{name: "invalid threshold operator", mutate: func(r *models.CreateAlertRequest) {
			r.ThresholdOperator = "not-an-operator"
		}, wantErr: true},
		{name: "invalid severity", mutate: func(r *models.CreateAlertRequest) { r.Severity = "not-a-severity" }, wantErr: true},
		{name: "zero frequency", mutate: func(r *models.CreateAlertRequest) { r.FrequencySeconds = 0 }, wantErr: true},
		{name: "negative lookback", mutate: func(r *models.CreateAlertRequest) { r.LookbackSeconds = -1 }, wantErr: true},
		{name: "native mode missing query", mutate: func(r *models.CreateAlertRequest) {
			r.EditorMode = models.AlertEditorModeNative
			r.Query = ""
		}, wantErr: true},
		{name: "condition mode missing condition_json", mutate: func(r *models.CreateAlertRequest) {
			r.EditorMode = models.AlertEditorModeCondition
			r.ConditionJSON = ""
			r.Query = "SELECT 1"
		}, wantErr: true},
		{name: "unknown recipient user", mutate: func(r *models.CreateAlertRequest) {
			r.RecipientUserIDs = []models.UserID{999999}
		}, wantErr: true},
		{name: "invalid webhook URL", mutate: func(r *models.CreateAlertRequest) {
			r.WebhookURLs = []string{"not-a-url"}
		}, wantErr: true},
		{name: "webhook URL with unsupported scheme", mutate: func(r *models.CreateAlertRequest) {
			r.WebhookURLs = []string{"ftp://example.com/hook"}
		}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newTestCreateAlertRequest()
			tc.mutate(req)
			_, err := CreateAlert(ctx, db, ds, log, src.ID, owner.ID, req)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidAlertConfiguration) {
					t.Errorf("CreateAlert(%s) err = %v, want ErrInvalidAlertConfiguration", tc.name, err)
				}
			} else if err != nil {
				t.Errorf("CreateAlert(%s) unexpected err: %v", tc.name, err)
			}
		})
	}
}

// TestCreateAlertUnsupportedByProvider guards the provider-capability gate:
// a query language/mode the registered provider doesn't support must be
// rejected even though the request is otherwise well-formed.
func TestCreateAlertUnsupportedByProvider(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "provider-owner@example.com", "Owner")
	src := newTestSource(t, db, "provider-src")
	// A provider that only speaks LogsQL (like VictoriaLogs) should reject a
	// ClickHouse-SQL alert request.
	ds := newFakeDatasourceService(db, log, &fakeProvider{
		queryLanguages: []models.QueryLanguage{models.QueryLanguageLogsQL},
		alertModes:     []models.AlertEditorMode{models.AlertEditorModeNative},
	})

	req := newTestCreateAlertRequest()
	if _, err := CreateAlert(ctx, db, ds, log, src.ID, owner.ID, req); err == nil {
		t.Error("expected CreateAlert to fail for a query language the provider does not support")
	}
}

func TestUpdateAlertNotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ds := newFakeDatasourceService(db, log, nil)

	name := "new name"
	_, err := UpdateAlert(context.Background(), db, ds, log, models.AlertID(999999), &models.UpdateAlertRequest{Name: &name})
	if !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("UpdateAlert(missing) err = %v, want ErrAlertNotFound", err)
	}
}

// TestUpdateAlertRejectsInvalidPartialFields ensures partial updates run
// through the same validation as create — a lone bad field fails the whole
// update rather than silently persisting an inconsistent alert.
func TestUpdateAlertRejectsInvalidPartialFields(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "update-owner@example.com", "Owner")
	src := newTestSource(t, db, "update-src")
	ds := newFakeDatasourceService(db, log, nil)

	alert, err := CreateAlert(ctx, db, ds, log, src.ID, owner.ID, newTestCreateAlertRequest())
	if err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}

	zero := 0
	if _, err := UpdateAlert(ctx, db, ds, log, alert.ID, &models.UpdateAlertRequest{FrequencySeconds: &zero}); !errors.Is(err, ErrInvalidAlertConfiguration) {
		t.Errorf("UpdateAlert(zero frequency) err = %v, want ErrInvalidAlertConfiguration", err)
	}

	badOp := models.AlertThresholdOperator("nonsense")
	if _, err := UpdateAlert(ctx, db, ds, log, alert.ID, &models.UpdateAlertRequest{ThresholdOperator: &badOp}); !errors.Is(err, ErrInvalidAlertConfiguration) {
		t.Errorf("UpdateAlert(bad operator) err = %v, want ErrInvalidAlertConfiguration", err)
	}

	// The alert itself must be unchanged after the rejected updates.
	reloaded, err := GetAlert(ctx, db, log, alert.ID)
	if err != nil {
		t.Fatalf("GetAlert: %v", err)
	}
	if reloaded.FrequencySeconds != 60 || reloaded.ThresholdOperator != models.AlertThresholdGreaterThan {
		t.Errorf("alert mutated despite rejected update: %+v", reloaded)
	}
}

func TestDeleteAlertNotFound(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	if err := DeleteAlert(context.Background(), db, log, models.AlertID(999999)); !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("DeleteAlert(missing) err = %v, want ErrAlertNotFound", err)
	}
}

func TestResolveAlertNoUnresolvedHistory(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "resolve-owner@example.com", "Owner")
	src := newTestSource(t, db, "resolve-src")
	ds := newFakeDatasourceService(db, log, nil)
	alert, err := CreateAlert(ctx, db, ds, log, src.ID, owner.ID, newTestCreateAlertRequest())
	if err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}

	// No alert_history row exists yet, so there is nothing to resolve.
	if err := ResolveAlert(ctx, db, log, alert.ID, "manual resolve"); !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("ResolveAlert(no history) err = %v, want ErrAlertNotFound", err)
	}
}

// TestAlertQueryEvalWiring pins the seam between TestAlertQuery and the
// registered provider's EvaluateAlert: the numeric value the provider returns
// flows through to the threshold comparison and the response payload.
func TestAlertQueryEvalWiring(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	src := newTestSource(t, db, "eval-src")

	ds := newFakeDatasourceService(db, discardLogger(), &fakeProvider{
		evaluateAlertFn: func(ctx context.Context, source *models.Source, req datasource.AlertQueryRequest) (*models.QueryResult, error) {
			return &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "count", Type: "UInt64"}},
				Logs:    []map[string]any{{"count": int64(42)}},
			}, nil
		},
	})

	req := &models.TestAlertQueryRequest{
		QueryLanguage:     models.QueryLanguageClickHouseSQL,
		EditorMode:        models.AlertEditorModeNative,
		Query:             "SELECT count() AS count FROM logs WHERE timestamp >= now() - INTERVAL 5 MINUTE",
		LookbackSeconds:   300,
		ThresholdOperator: models.AlertThresholdGreaterThan,
		ThresholdValue:    10,
	}
	resp, err := testAlertQuery(ctx, db, ds, src.ID, req)
	if err != nil {
		t.Fatalf("TestAlertQuery: %v", err)
	}
	if resp.Value != 42 {
		t.Errorf("Value = %v, want 42 (provider's row must flow through)", resp.Value)
	}
	if !resp.ThresholdMet {
		t.Errorf("ThresholdMet = false, want true (42 > 10)")
	}
	if resp.RowsReturned != 1 {
		t.Errorf("RowsReturned = %d, want 1", resp.RowsReturned)
	}
}

// TestAlertQueryNoRowsIsGraceful pins the no-rows behavior: a query that
// matches nothing evaluates to value=0 rather than erroring out (otherwise a
// healthy but quiet alert would fail every test-query call) AND surfaces the
// "no matching data yet" warning so the user can tell it apart from a real 0.
// (#85: the warning used to be dead code — it lived under `if err != nil`, but
// ExtractFirstNumeric returns (0, nil) on an empty result, so it never fired.
// Now the row count is checked independently.)
func TestAlertQueryNoRowsIsGraceful(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	src := newTestSource(t, db, "no-rows-src")

	ds := newFakeDatasourceService(db, discardLogger(), &fakeProvider{
		evaluateAlertFn: func(ctx context.Context, source *models.Source, req datasource.AlertQueryRequest) (*models.QueryResult, error) {
			return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "count"}}, Logs: nil}, nil
		},
	})

	req := &models.TestAlertQueryRequest{
		QueryLanguage:     models.QueryLanguageClickHouseSQL,
		EditorMode:        models.AlertEditorModeNative,
		Query:             "SELECT count() AS count FROM logs WHERE timestamp >= now() - INTERVAL 5 MINUTE",
		LookbackSeconds:   300,
		ThresholdOperator: models.AlertThresholdGreaterThan,
		ThresholdValue:    10,
	}
	resp, err := testAlertQuery(ctx, db, ds, src.ID, req)
	if err != nil {
		t.Fatalf("TestAlertQuery: %v", err)
	}
	if resp.Value != 0 {
		t.Errorf("Value = %v, want 0 for no rows", resp.Value)
	}
	if resp.ThresholdMet {
		t.Errorf("ThresholdMet = true, want false (0 > 10 is false)")
	}
	// A query that matched nothing must surface the "no rows" warning so the
	// user knows the alert is waiting for data rather than firing on a real 0
	// (the dead-code bug this test originally pinned is fixed — #85).
	if len(resp.Warnings) == 0 {
		t.Errorf("got no warnings, want the no-rows warning for an empty result")
	}
}

// TestCompareAlertThreshold pins the threshold-comparison seam directly:
// every supported operator, including the epsilon-based equal/not-equal.
func TestCompareAlertThreshold(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		value     float64
		threshold float64
		operator  models.AlertThresholdOperator
		want      bool
	}{
		{"gt true", 11, 10, models.AlertThresholdGreaterThan, true},
		{"gt false", 10, 10, models.AlertThresholdGreaterThan, false},
		{"gte true at boundary", 10, 10, models.AlertThresholdGreaterThanOrEqual, true},
		{"lt true", 9, 10, models.AlertThresholdLessThan, true},
		{"lte true at boundary", 10, 10, models.AlertThresholdLessThanOrEqual, true},
		{"eq true", 10, 10, models.AlertThresholdEqual, true},
		{"eq false", 10.5, 10, models.AlertThresholdEqual, false},
		{"neq true", 11, 10, models.AlertThresholdNotEqual, true},
		{"neq false", 10, 10, models.AlertThresholdNotEqual, false},
		{"unknown operator defaults false", 100, 10, models.AlertThresholdOperator("bogus"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := util.CompareAlertThreshold(tc.value, tc.threshold, tc.operator); got != tc.want {
				t.Errorf("CompareAlertThreshold(%v, %v, %q) = %v, want %v", tc.value, tc.threshold, tc.operator, got, tc.want)
			}
		})
	}
}

func TestGenerateQueryWarningsNamesTruncationCause(t *testing.T) {
	const query = "SELECT count() AS value FROM logs WHERE ts > now() - INTERVAL 5 MINUTE"
	rows := []map[string]any{{"value": 1}, {"value": 2}, {"value": 3}}

	tests := []struct {
		name   string
		stats  models.QueryStats
		prefix string
	}{
		{"byte limit", models.QueryStats{Truncated: true, TruncatedReason: "byte_limit"}, "Results were cut off by the response size limit after 3 rows."},
		{"row limit", models.QueryStats{Truncated: true, TruncatedReason: "row_limit"}, "Results were capped at 3 buffered rows."},
		{"complete", models.QueryStats{}, "Query returned 3 rows."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			warnings := generateQueryWarnings(query, time.Millisecond, &models.QueryResult{Logs: rows, Stats: tc.stats})
			if len(warnings) != 1 || !strings.HasPrefix(warnings[0], tc.prefix) {
				t.Fatalf("warnings = %q, want one starting with %q", warnings, tc.prefix)
			}
		})
	}
}
