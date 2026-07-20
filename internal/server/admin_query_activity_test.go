package server

import (
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func activityWindow() []models.QueryActivityRecord {
	base := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	return []models.QueryActivityRecord{
		{ID: 5, SourceID: 2, SourceName: "App Logs", QueryLanguage: "logchefql", DurationMs: 42, CreatedAt: base.Add(4 * time.Minute)},
		{ID: 4, SourceID: 2, SourceName: "App Logs", QueryLanguage: "clickhouse-sql", DurationMs: 5000, CreatedAt: base.Add(3 * time.Minute)},
		{ID: 3, SourceID: 3, SourceName: "Web Logs", QueryLanguage: "logchefql", DurationMs: 100, CreatedAt: base.Add(2 * time.Minute)},
		{ID: 2, SourceID: 2, SourceName: "App Logs", QueryLanguage: "logchefql", DurationMs: 7, CreatedAt: base.Add(1 * time.Minute)},
		{ID: 1, SourceID: 3, SourceName: "Web Logs", QueryLanguage: "clickhouse-sql", DurationMs: 3000, CreatedAt: base},
	}
}

func TestRecentActivity(t *testing.T) {
	window := activityWindow()

	got := recentActivity(window, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got[0].ID != 5 || got[1].ID != 4 {
		t.Fatalf("expected newest-first ids [5 4], got [%d %d]", got[0].ID, got[1].ID)
	}

	// Limit larger than the window returns the whole window, no panic.
	if all := recentActivity(window, 100); len(all) != len(window) {
		t.Fatalf("expected %d rows, got %d", len(window), len(all))
	}

	// Mutating the result must not touch the source window.
	got[0].SourceName = "mutated"
	if window[0].SourceName == "mutated" {
		t.Fatal("recentActivity returned a slice aliasing the input window")
	}
}

func TestLanguageBreakdown(t *testing.T) {
	got := languageBreakdown(activityWindow())
	if len(got) != 2 {
		t.Fatalf("expected 2 languages, got %d", len(got))
	}
	if got[0].Language != "logchefql" || got[0].Count != 3 {
		t.Fatalf("expected logchefql=3 first, got %s=%d", got[0].Language, got[0].Count)
	}
	if got[1].Language != "clickhouse-sql" || got[1].Count != 2 {
		t.Fatalf("expected clickhouse-sql=2 second, got %s=%d", got[1].Language, got[1].Count)
	}
}

func TestSourceBreakdown(t *testing.T) {
	got := sourceBreakdown(activityWindow())
	if len(got) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(got))
	}
	if got[0].SourceID != 2 || got[0].Count != 3 || got[0].SourceName != "App Logs" {
		t.Fatalf("expected source 2 (App Logs)=3 first, got %d (%s)=%d", got[0].SourceID, got[0].SourceName, got[0].Count)
	}
	if got[1].SourceID != 3 || got[1].Count != 2 {
		t.Fatalf("expected source 3=2 second, got %d=%d", got[1].SourceID, got[1].Count)
	}
}

func TestSlowestActivity(t *testing.T) {
	got := slowestActivity(activityWindow(), 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(got))
	}
	wantDurations := []int64{5000, 3000, 100}
	for i, d := range wantDurations {
		if got[i].DurationMs != d {
			t.Fatalf("slowest[%d]: expected %d, got %d", i, d, got[i].DurationMs)
		}
	}

	// n larger than the window returns the whole window sorted, no panic.
	if all := slowestActivity(activityWindow(), 100); len(all) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(all))
	}
}
