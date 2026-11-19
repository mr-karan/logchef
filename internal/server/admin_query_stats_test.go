package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestAdminQueryStatsSummary(t *testing.T) {
	db := newServerTestDB(t)
	s := &Server{store: db, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	app := fiber.New()
	app.Get("/api/v1/admin/query-stats", s.handleAdminQueryStats)

	read := func(want models.QueryStatsSummary, wantRanked int) {
		t.Helper()
		res, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/admin/query-stats?days=7", http.NoBody))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		var body struct {
			Data struct {
				Summary    *models.QueryStatsSummary `json:"summary"`
				TopUsers   []models.UserQueryStat    `json:"top_users"`
				TopSources []models.SourceQueryStat  `json:"top_sources"`
			} `json:"data"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Summary == nil || *body.Data.Summary != want {
			t.Fatalf("summary = %+v, want %+v", body.Data.Summary, want)
		}
		if len(body.Data.TopUsers) != wantRanked || len(body.Data.TopSources) != wantRanked {
			t.Fatalf("ranking sizes = %d users, %d sources, want %d each", len(body.Data.TopUsers), len(body.Data.TopSources), wantRanked)
		}
	}
	read(models.QueryStatsSummary{}, 0)

	today := time.Now().UTC()
	for i := range 12 {
		user := &models.User{Email: fmt.Sprintf("summary-%d@test.dev", i), Role: models.UserRoleMember, Status: models.UserStatusActive}
		if err := db.CreateUser(t.Context(), user); err != nil {
			t.Fatal(err)
		}
		count, duration := 2, int64(100)
		if i >= 10 {
			count, duration = 1, 2300
		}
		for range count {
			if err := db.IncrementQueryStats(t.Context(), today.Format(time.DateOnly), user.ID, 1, models.SourceID(i+1), models.QueryLanguageLogchefQL, duration); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.IncrementQueryStats(t.Context(), today.AddDate(0, 0, -7).Format(time.DateOnly), 999999, 1, 999999, models.QueryLanguageLogchefQL, 10000); err != nil {
		t.Fatal(err)
	}
	read(models.QueryStatsSummary{QueryCount: 22, ActiveUsers: 12, ActiveSources: 12, AvgDurationMs: 300}, 10)
}
