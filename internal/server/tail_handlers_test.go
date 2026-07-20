package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestTailRateLimiterDroppedTotalAccumulates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	limiter := newTailRateLimiter(10)
	limiter.now = func() time.Time { return now }

	// Window 1: 15 rows in, 10 admitted, 5 dropped.
	if allowed, dropped := limiter.admit(15); allowed != 10 || dropped != 5 {
		t.Fatalf("window 1: allowed=%d dropped=%d, want 10/5", allowed, dropped)
	}
	if limiter.droppedTotal != 5 {
		t.Fatalf("window 1: droppedTotal=%d, want 5", limiter.droppedTotal)
	}

	// Window 2 (next second): another 8 dropped. droppedTotal must be the sum
	// across both windows (13), not just the current window's count (8) — a
	// client sampling one notice per second must never undercount the session
	// total.
	now = now.Add(time.Second)
	if allowed, dropped := limiter.admit(18); allowed != 10 || dropped != 8 {
		t.Fatalf("window 2: allowed=%d dropped=%d, want 10/8", allowed, dropped)
	}
	if limiter.droppedTotal != 13 {
		t.Fatalf("window 2: droppedTotal=%d, want 13 (cumulative)", limiter.droppedTotal)
	}

	// A window with nothing dropped must not perturb the running total.
	now = now.Add(time.Second)
	if allowed, dropped := limiter.admit(3); allowed != 3 || dropped != 0 {
		t.Fatalf("window 3: allowed=%d dropped=%d, want 3/0", allowed, dropped)
	}
	if limiter.droppedTotal != 13 {
		t.Fatalf("window 3: droppedTotal=%d, want unchanged 13", limiter.droppedTotal)
	}
}

func TestTailRateLimiterUnlimited(t *testing.T) {
	t.Parallel()

	limiter := newTailRateLimiter(0) // 0 or negative means unlimited.
	if allowed, dropped := limiter.admit(100000); allowed != 100000 || dropped != 0 {
		t.Fatalf("unlimited: allowed=%d dropped=%d, want 100000/0", allowed, dropped)
	}
}

func TestSanitizeTailErrorMessage(t *testing.T) {
	t.Parallel()

	t.Run("collapses embedded newlines and whitespace", func(t *testing.T) {
		t.Parallel()
		err := errors.New("victorialogs request failed with status 500: <html>\n\t<body>internal error\n\ttraceback line 1\n\ttraceback line 2</body>\n</html>")
		got := sanitizeTailErrorMessage(err)
		if strings.ContainsAny(got, "\n\t") {
			t.Fatalf("expected no raw newlines/tabs in sanitized message, got %q", got)
		}
	})

	t.Run("caps length", func(t *testing.T) {
		t.Parallel()
		err := errors.New(strings.Repeat("x", 4096)) // simulates a 4KiB upstream body
		got := sanitizeTailErrorMessage(err)
		if len(got) > tailSanitizedErrorMaxLen+len("...") {
			t.Fatalf("sanitized message too long: %d chars", len(got))
		}
		if !strings.HasSuffix(got, "...") {
			t.Fatalf("expected truncation suffix, got %q", got)
		}
	})

	t.Run("empty error message gets a fallback", func(t *testing.T) {
		t.Parallel()
		got := sanitizeTailErrorMessage(errors.New(""))
		if got == "" {
			t.Fatalf("expected a non-empty fallback message")
		}
	})
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
