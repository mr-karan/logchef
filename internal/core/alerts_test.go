package core

import (
	"context"
	"errors"
	"testing"

	"github.com/mr-karan/logchef/internal/datasource"
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
	resp, err := TestAlertQuery(ctx, db, ds, src.ID, req)
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
	resp, err := TestAlertQuery(ctx, db, ds, src.ID, req)
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
			if got := compareAlertThreshold(tc.value, tc.threshold, tc.operator); got != tc.want {
				t.Errorf("compareAlertThreshold(%v, %v, %q) = %v, want %v", tc.value, tc.threshold, tc.operator, got, tc.want)
			}
		})
	}
}
