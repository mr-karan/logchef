package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestHistogramTimeRangeValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		req     models.APIHistogramRequest
		invalid bool
	}{
		{name: "SQL embedded range", req: models.APIHistogramRequest{}},
		{name: "equal endpoints", req: models.APIHistogramRequest{StartTime: "2026-09-01T00:00:00Z", EndTime: "2026-09-01T00:00:00Z"}},
		{name: "reversed RFC3339", req: models.APIHistogramRequest{StartTime: "2026-09-02T00:00:00Z", EndTime: "2026-09-01T00:00:00Z"}, invalid: true},
		{name: "reversed milliseconds", req: models.APIHistogramRequest{StartTimestamp: 2000, EndTimestamp: 1000}, invalid: true},
		{name: "timezone boundary", req: models.APIHistogramRequest{StartTime: "2026-09-02T00:00:00+05:30", EndTime: "2026-09-01T19:00:00Z"}},
		{name: "missing endpoint", req: models.APIHistogramRequest{StartTime: "2026-09-02T00:00:00Z"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, message := buildHistogramParams(tc.req, "SELECT * FROM logs")
			if (message != "") != tc.invalid {
				t.Fatalf("validation message = %q, invalid = %v", message, tc.invalid)
			}
		})
	}
}

func TestHistogramAdmissionLifecycle(t *testing.T) {
	s := newDashboardTestServer(t)
	s.config = &config.Config{}
	s.datasources = datasource.NewService(s.sqlite, s.log)
	s.config.Query.MaxConcurrentPerUser = 1
	s.config.DashboardCache.MaxConcurrentFills = 1
	const userID models.UserID = 91001
	const sourceID models.SourceID = 91001
	const teamID models.TeamID = 91001
	params := core.HistogramParams{Query: "SELECT * FROM logs", Window: "1m"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.executeHistogram(ctx, QueryClassHistogram, userID, teamID, sourceID, params); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled execution: %v", err)
	}

	// An explorer view issues a log query and a histogram together, so a busy
	// preview slot must not reject the histogram that accompanies it.
	previewID, err := queryTracker.StartQuery(QueryClassPreview, userID, sourceID, teamID, params.Query, cancel, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.executeHistogram(context.Background(), QueryClassHistogram, userID, teamID, sourceID, params); !errors.Is(err, core.ErrSourceNotFound) {
		t.Fatalf("preview query blocked the histogram beside it: %v", err)
	}
	queryTracker.RemoveQuery(previewID)

	for _, tc := range []struct {
		name  string
		class QueryClass
	}{
		{"histogram", QueryClassHistogram},
		{"dashboard", QueryClassDashboard},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queryID, err := queryTracker.StartQuery(tc.class, userID, sourceID, teamID, params.Query, cancel, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { queryTracker.RemoveQuery(queryID) })
			_, err = s.executeHistogram(context.Background(), tc.class, userID, teamID, sourceID, params)
			if _, ok := errors.AsType[*QueryAdmissionError](err); !ok {
				t.Fatalf("%s class did not bound itself: %v", tc.class, err)
			}
		})
	}

	// A failed datasource lookup must release admission for the next attempt.
	for range 2 {
		_, err := s.executeHistogram(context.Background(), QueryClassHistogram, userID, teamID, sourceID, params)
		if !errors.Is(err, core.ErrSourceNotFound) {
			t.Fatalf("datasource failure did not release admission: %v", err)
		}
	}
}

func TestHistogramErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"bucket budget", fmt.Errorf("provider: %w", models.ErrHistogramBudgetExceeded), fiber.StatusBadRequest},
		{"admission", &QueryAdmissionError{Message: "Too many active preview queries"}, fiber.StatusTooManyRequests},
		{"canceled fill", fmt.Errorf("provider: %w", context.Canceled), fiber.StatusRequestTimeout},
		{"fill deadline", context.DeadlineExceeded, fiber.StatusRequestTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			s := &Server{}
			app.Get("/", func(c *fiber.Ctx) error { return s.handleHistogramError(c, 1, tc.err) })
			response, err := app.Test(httptest.NewRequest(http.MethodGet, "/", http.NoBody))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status=%d, want %d", response.StatusCode, tc.status)
			}
		})
	}
}
