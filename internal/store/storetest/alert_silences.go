package storetest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testAlertSilences covers the storage of silences (RFC 0006 unit S1): both
// schedules round-trip, an update keeps source and creator, and a silence
// goes with its rule.
func testAlertSilences(t *testing.T, ctx context.Context, s store.Store) {
	src := mkSource(t, ctx, s, "alert-silences")
	alert := &models.Alert{
		SourceID: src.ID, Name: "silenced", Query: "SELECT count() FROM logs",
		QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
		LookbackSeconds: 300, FrequencySeconds: 60, IsActive: true,
		ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
		Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
	}
	if err := s.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	once := &models.AlertSilence{
		SourceID: src.ID, AlertID: &alert.ID, Matchers: map[string]string{"service": "checkout"},
		Comment: "deploy", ScheduleKind: models.SilenceOnce, StartsAt: &start, EndsAt: &end,
	}
	if err := s.CreateAlertSilence(ctx, once); err != nil || once.ID == 0 || once.CreatedAt.IsZero() {
		t.Fatalf("create once = %+v, %v", once, err)
	}
	window := &models.SilenceWeeklyWindow{Weekdays: []int{0, 6}, Start: "02:00", DurationMinutes: 120, Timezone: "Asia/Kolkata"}
	weekly := &models.AlertSilence{SourceID: src.ID, Comment: "batch", ScheduleKind: models.SilenceWeekly, Weekly: window}
	if err := s.CreateAlertSilence(ctx, weekly); err != nil {
		t.Fatal(err)
	}

	testSilenceRoundTrip(t, ctx, s, once, weekly)
	testSilenceUpdate(t, ctx, s, weekly.ID)

	list, err := s.ListAlertSilences(ctx, src.ID)
	if err != nil || len(list) != 2 || list[0].ID != once.ID || list[1].ID != weekly.ID {
		t.Fatalf("list = %+v, %v; want both by id", list, err)
	}
	if err := s.DeleteAlert(ctx, alert.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAlertSilence(ctx, once.ID); !models.IsNotFound(err) {
		t.Fatalf("silence of a deleted rule err = %v, want not found", err)
	}
	if err := s.DeleteAlertSilence(ctx, weekly.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAlertSilence(ctx, weekly.ID); !models.IsNotFound(err) {
		t.Fatalf("second delete err = %v, want not found", err)
	}
}

func testSilenceRoundTrip(t *testing.T, ctx context.Context, s store.Store, once, weekly *models.AlertSilence) {
	t.Helper()
	start, end, window, alertID, src := *once.StartsAt, *once.EndsAt, weekly.Weekly, *once.AlertID, once.SourceID
	got, err := s.GetAlertSilence(ctx, once.ID)
	if err != nil || got.AlertID == nil || *got.AlertID != alertID || !got.StartsAt.Equal(start) || !got.EndsAt.Equal(end) ||
		!reflect.DeepEqual(got.Matchers, once.Matchers) || got.Weekly != nil {
		t.Fatalf("once round trip = %+v, %v", got, err)
	}
	got, err = s.GetAlertSilence(ctx, weekly.ID)
	if err != nil || got.AlertID != nil || got.StartsAt != nil || got.EndsAt != nil || !reflect.DeepEqual(got.Weekly, window) || len(got.Matchers) != 0 {
		t.Fatalf("weekly round trip = %+v, %v", got, err)
	}
	if n, err := s.CountAlertSilences(ctx, src); err != nil || n != 2 {
		t.Fatalf("count = %d, %v; want 2", n, err)
	}
}

func testSilenceUpdate(t *testing.T, ctx context.Context, s store.Store, id models.AlertSilenceID) {
	t.Helper()
	got, err := s.GetAlertSilence(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	src, window := got.SourceID, got.Weekly
	got.Comment = "batch, moved"
	got.Weekly = &models.SilenceWeeklyWindow{Weekdays: []int{1}, Start: "03:00", DurationMinutes: 60, Timezone: "UTC"}
	got.SourceID = 0
	if err := s.UpdateAlertSilence(ctx, got); err != nil || got.SourceID != src || got.Comment != "batch, moved" || got.Weekly.Start != "03:00" {
		t.Fatalf("update = %+v, %v; want the new window on the same source", got, err)
	}
	if err := s.UpdateAlertSilence(ctx, &models.AlertSilence{ID: 1 << 40, Comment: "x", ScheduleKind: models.SilenceWeekly, Weekly: window}); !models.IsNotFound(err) {
		t.Fatalf("update of a missing silence err = %v, want not found", err)
	}
}
