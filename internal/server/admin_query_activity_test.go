package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
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

func TestDemoQueryActivityDoesNotExposeDetailedHistory(t *testing.T) {
	t.Parallel()

	s := &Server{config: &config.Config{Demo: config.DemoConfig{ReadOnly: true}}}
	app := fiber.New()
	app.Get("/api/v1/admin/query-activity", s.handleAdminQueryActivity)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/admin/query-activity?limit=100&q=incident&source_id=1&user_id=1&language=logql", http.NoBody))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var envelope struct {
		Data queryActivityResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Total != 0 || envelope.Data.WindowSize != 0 || envelope.Data.WindowLimit != 2000 || len(envelope.Data.Recent) != 0 || len(envelope.Data.Slowest) != 0 || len(envelope.Data.BySource) != 0 || len(envelope.Data.ByLanguage) != 0 {
		t.Fatalf("demo activity exposed detailed history: %+v", envelope.Data)
	}
}

func readActivityHTTP(t *testing.T, s *Server, cookie *http.Cookie, params string) queryActivityResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/query-activity"+params, http.NoBody)
	req.AddCookie(cookie)
	resp, err := s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var envelope struct {
		Data queryActivityResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func TestAdminQueryActivityFiltersHTTP(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.store, "investigator@example.test", models.UserRoleAdmin)
	other := mkTestUser(t, s.store, "other@example.test", models.UserRoleMember)
	team, source := mkTestTeam(t, s.store, "Incident Archive")
	_, otherSource := mkTestTeam(t, s.store, "Other")
	record := func(user models.UserID, src models.SourceID, text string, lang models.QueryLanguage, duration int64) int64 {
		t.Helper()
		row := &models.QueryHistory{UserID: user, TeamID: team.ID, SourceID: src, QueryText: text, QueryLanguage: lang, DurationMs: duration}
		if err := s.store.RecordQueryHistory(t.Context(), row); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	first := record(admin.ID, source.ID, "Incident-247 oldest", models.QueryLanguageLogsQL, 900)
	second := record(admin.ID, source.ID, "incident-247 newer", models.QueryLanguageLogQL, 700)
	record(other.ID, source.ID, "incident-247 other user", models.QueryLanguageLogQL, 5000)
	record(admin.ID, otherSource.ID, "incident-247 other source", models.QueryLanguageLogQL, 6000)
	for range 110 {
		record(other.ID, otherSource.ID, "ordinary", models.QueryLanguageClickHouseSQL, 10000)
	}
	cookie := sessionCookie(t, s.store, admin)
	scope := fmt.Sprintf("&source_id=%d&user_id=%d", source.ID, admin.ID)
	got := readActivityHTTP(t, s, cookie, "?limit=1&q="+url.QueryEscape("  INCIDENT-247  ")+scope)
	if got.Total != 2 || len(got.Recent) != 1 || got.Recent[0].ID != second || got.WindowSize != 114 || got.WindowLimit != 2000 {
		t.Fatalf("scoped search = %+v, window=%d/%d", got, got.WindowSize, got.WindowLimit)
	}
	if len(got.Slowest) != 2 || got.Slowest[0].ID != first || got.Slowest[1].ID != second {
		t.Fatalf("slowest must cover all matches: %+v", got.Slowest)
	}
	if len(got.ByLanguage) != 2 || got.ByLanguage[0].Language != models.QueryLanguageLogQL || got.ByLanguage[0].Count != 1 || got.ByLanguage[1].Language != models.QueryLanguageLogsQL || got.ByLanguage[1].Count != 1 {
		t.Fatalf("language breakdown = %+v", got.ByLanguage)
	}
	if len(got.BySource) != 1 || got.BySource[0].SourceID != source.ID || got.BySource[0].Count != 2 {
		t.Fatalf("source breakdown = %+v", got.BySource)
	}
	for _, tc := range []struct {
		params string
		total  int
		newest int64
	}{
		{"?q=INVESTIGATOR%40EXAMPLE.TEST", 3, second + 2},
		{"?q=ARCHIVE", 3, second + 1},
		{"?q=LOGSQL", 1, first},
		{"?language=logsql" + scope, 1, first},
		{"?language=logql" + scope, 1, second},
		{"?language=logchefql", 0, 0},
		{"?language=clickhouse-sql&limit=1", 110, second + 112},
		{"?source_id=99999", 0, 0},
		{"?user_id=99999", 0, 0},
		{"?q=no-match", 0, 0},
		{"?q=" + strings.Repeat("x", 256), 0, 0},
		{"?q=%20%20&limit=9999", 114, second + 112},
	} {
		t.Run(tc.params, func(t *testing.T) {
			got := readActivityHTTP(t, s, cookie, tc.params)
			if got.Total != tc.total || got.WindowSize != 114 || got.WindowLimit != 2000 {
				t.Fatalf("response = %+v, window=%d/%d", got, got.WindowSize, got.WindowLimit)
			}
			if tc.total > 0 && (len(got.Recent) == 0 || got.Recent[0].ID != tc.newest) {
				t.Fatalf("recent = %+v", got.Recent)
			}
			if tc.total == 0 && (got.Recent == nil || len(got.Recent) != 0 || got.Slowest == nil || len(got.Slowest) != 0 || got.BySource == nil || len(got.BySource) != 0 || got.ByLanguage == nil || len(got.ByLanguage) != 0) {
				t.Fatalf("empty response = %+v", got)
			}
		})
	}
	got = readActivityHTTP(t, s, cookie, "")
	if got.Total != 114 || len(got.Recent) != 100 || got.WindowSize != 114 || got.WindowLimit != 2000 {
		t.Fatalf("unfiltered response = %+v, window=%d/%d", got, got.WindowSize, got.WindowLimit)
	}
}

func TestAdminQueryActivityInvalidParamsHTTP(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.store, "admin@example.test", models.UserRoleAdmin)
	cookie := sessionCookie(t, s.store, admin)
	for _, params := range []string{
		"source_id=0", "source_id=-1", "source_id=abc", "source_id=1.5", "source_id=999999999999999999999999",
		"user_id=0", "user_id=-1", "user_id=abc", "user_id=1.5", "user_id=999999999999999999999999",
		"language=sql", "language=LOGQL", "language=unknown", "language=%20logql%20",
		"q=" + strings.Repeat("x", 257), "limit=0", "limit=-1", "limit=abc",
	} {
		t.Run(params, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/query-activity?"+params, http.NoBody)
			req.AddCookie(cookie)
			resp, err := s.app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body Response
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusBadRequest || body.ErrorType != string(models.ValidationErrorType) {
				t.Fatalf("status=%d body=%+v", resp.StatusCode, body)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		auth   func(*http.Request)
		status int
	}{
		{"anonymous", func(*http.Request) {}, http.StatusUnauthorized},
		{"member", withSession(sessionCookie(t, s.store, mkTestUser(t, s.store, "member@example.test", models.UserRoleMember))), http.StatusForbidden},
		{"wrong scope", withBearer(apiToken(t, s, admin, models.TokenScopeSourcesRead)), http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := doAuthzRequest(t, s, http.MethodGet, "/api/v1/admin/query-activity?q=incident", "", tc.auth)
			if got.status != tc.status {
				t.Fatalf("status=%d, want %d", got.status, tc.status)
			}
		})
	}
}

func TestAdminQueryActivityWindowBoundHTTP(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.store, "admin@example.test", models.UserRoleAdmin)
	record := func(user models.UserID, source models.SourceID, query string) {
		t.Helper()
		if err := s.store.RecordQueryHistory(t.Context(), &models.QueryHistory{
			UserID: user, TeamID: 1, SourceID: source, QueryText: query, QueryLanguage: models.QueryLanguageLogchefQL,
		}); err != nil {
			t.Fatal(err)
		}
	}
	record(admin.ID, 999, "outside-window")
	for i := range 10 {
		user := mkTestUser(t, s.store, fmt.Sprintf("user-%d@example.test", i), models.UserRoleMember)
		for range 200 {
			record(user.ID, 1, "ordinary")
		}
	}
	cookie := sessionCookie(t, s.store, admin)
	for _, params := range []string{"?q=outside-window", "?source_id=999", fmt.Sprintf("?user_id=%d", admin.ID)} {
		got := readActivityHTTP(t, s, cookie, params)
		if got.Total != 0 || len(got.Recent) != 0 || len(got.Slowest) != 0 || len(got.BySource) != 0 || len(got.ByLanguage) != 0 || got.WindowSize != 2000 || got.WindowLimit != 2000 {
			t.Fatalf("%s widened window: %+v, window=%d/%d", params, got, got.WindowSize, got.WindowLimit)
		}
	}
	got := readActivityHTTP(t, s, cookie, "?limit=9999")
	if got.Total != 2000 || len(got.Recent) != 500 || got.Recent[0].ID != 2001 || got.Recent[499].ID != 1502 || got.WindowSize != 2000 || got.WindowLimit != 2000 {
		t.Fatalf("bounded unfiltered response: total=%d recent=%d window=%d/%d", got.Total, len(got.Recent), got.WindowSize, got.WindowLimit)
	}
}
