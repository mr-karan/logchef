package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestTailRateLimiterAdmit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	limiter := newTailRateLimiter(100)
	limiter.now = func() time.Time { return now }

	// First batch within the ceiling: all admitted.
	if allowed, dropped := limiter.admit(60); allowed != 60 || dropped != 0 {
		t.Fatalf("batch 1: allowed=%d dropped=%d, want 60/0", allowed, dropped)
	}
	// Second batch pushes past the ceiling: only the remaining 40 admitted.
	if allowed, dropped := limiter.admit(70); allowed != 40 || dropped != 30 {
		t.Fatalf("batch 2: allowed=%d dropped=%d, want 40/30", allowed, dropped)
	}
	// Still in the same window: everything dropped.
	if allowed, dropped := limiter.admit(10); allowed != 0 || dropped != 10 {
		t.Fatalf("batch 3: allowed=%d dropped=%d, want 0/10", allowed, dropped)
	}

	// Notice fires at most once per window.
	if !limiter.shouldNotify() {
		t.Fatalf("expected first shouldNotify to be true")
	}
	if limiter.shouldNotify() {
		t.Fatalf("expected second shouldNotify in same window to be false")
	}

	// Advance past the one-second window: the ceiling and notice reset.
	now = now.Add(time.Second)
	if allowed, dropped := limiter.admit(50); allowed != 50 || dropped != 0 {
		t.Fatalf("new window: allowed=%d dropped=%d, want 50/0", allowed, dropped)
	}
	if !limiter.shouldNotify() {
		t.Fatalf("expected shouldNotify to reset in the new window")
	}
}

func TestTailRateLimiterUnlimited(t *testing.T) {
	t.Parallel()

	limiter := newTailRateLimiter(0) // 0 or negative means unlimited.
	if allowed, dropped := limiter.admit(100000); allowed != 100000 || dropped != 0 {
		t.Fatalf("unlimited: allowed=%d dropped=%d, want 100000/0", allowed, dropped)
	}
}

func TestHandleTailLogsInvalidParams(t *testing.T) {
	t.Parallel()

	s := &Server{}
	app := fiber.New()
	// No middleware/user context — exercises the pre-auth param validation only.
	app.Get("/teams/:teamID/sources/:sourceID/logs/tail", s.handleTailLogs)

	t.Run("invalid source ID returns 400", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/teams/1/sources/notanumber/logs/tail", http.NoBody)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("invalid team ID returns 400", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/teams/notanumber/sources/1/logs/tail", http.NoBody)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})
}

// Regression: resolveTailQuery's failure paths must both write the error
// response AND report ok=false — the Send* helpers return nil, and treating
// that as an error sentinel once let a rejected request continue into a 200
// SSE stream.
func TestResolveTailQueryRejectsRawSQL(t *testing.T) {
	t.Parallel()

	s := &Server{}
	app := fiber.New()
	app.Get("/probe", func(c *fiber.Ctx) error {
		source := &models.Source{
			SourceType:     models.SourceTypeClickHouse,
			QueryLanguages: []models.QueryLanguage{models.QueryLanguageLogchefQL, models.QueryLanguageClickHouseSQL},
		}
		query, lang, ok := s.resolveTailQuery(c, source, 1)
		if ok {
			t.Errorf("resolveTailQuery ok=true for clickhouse-sql, want false (query=%q lang=%q)", query, lang)
		}
		return nil
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe?query=SELECT%201&query_language=clickhouse-sql", http.NoBody))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 written by resolveTailQuery", resp.StatusCode)
	}
}
