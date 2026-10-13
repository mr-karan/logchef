package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestParseLogchefQLTimeRange(t *testing.T) {
	t.Run("accepts legacy picker format", func(t *testing.T) {
		start, end, err := parseLogchefQLTimeRange("2026-04-08 00:00:00", "2026-04-08 01:00:00", "UTC")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got := start.Format(time.RFC3339); got != "2026-04-08T00:00:00Z" {
			t.Fatalf("unexpected start time %q", got)
		}
		if got := end.Format(time.RFC3339); got != "2026-04-08T01:00:00Z" {
			t.Fatalf("unexpected end time %q", got)
		}
	})

	t.Run("accepts iso8601 timestamps", func(t *testing.T) {
		start, end, err := parseLogchefQLTimeRange("2026-04-08T00:00:00Z", "2026-04-08T01:00:00Z", "UTC")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got := start.Format(time.RFC3339); got != "2026-04-08T00:00:00Z" {
			t.Fatalf("unexpected start time %q", got)
		}
		if got := end.Format(time.RFC3339); got != "2026-04-08T01:00:00Z" {
			t.Fatalf("unexpected end time %q", got)
		}
	})
}

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
			tc.req.QueryText = "SELECT * FROM logs"
			_, err := PrepareHistogram(tc.req)
			if (err != nil) != tc.invalid {
				t.Fatalf("validation error = %v, invalid = %v", err, tc.invalid)
			}
		})
	}
}

func TestPrepareSQLQuery(t *testing.T) {
	cfg := config.QueryConfig{
		DefaultPreviewLimit:   100,
		MaxPreviewLimit:       1000,
		MaxResponseBytes:      4096,
		DefaultTimeoutSeconds: 30,
		MaxTimeoutSeconds:     60,
	}
	tooLong := 61

	params, err := PrepareSQLQuery(cfg, models.APIQueryRequest{
		QueryText: "SELECT * FROM logs WHERE level = {{level}}",
		Variables: []models.TemplateVariable{{Name: "level", Type: "string", Value: "error"}},
		StartTime: "2026-04-08T00:00:00Z",
		EndTime:   "2026-04-08T01:00:00Z",
	})
	if err != nil {
		t.Fatalf("PrepareSQLQuery: %v", err)
	}
	if params.RawQuery != "SELECT * FROM logs WHERE level = 'error'" {
		t.Fatalf("RawQuery = %q", params.RawQuery)
	}
	if *params.QueryTimeout != 30 || params.DefaultLimit != 100 || params.MaxLimit != 1000 || params.MaxResponseBytes != 4096 {
		t.Fatalf("limits not applied: timeout %d, %+v", *params.QueryTimeout, params)
	}
	if params.StartTime == nil || params.EndTime == nil {
		t.Fatal("time range not parsed")
	}

	for _, tc := range []struct {
		name string
		req  models.APIQueryRequest
		want string
	}{
		{"timeout over max", models.APIQueryRequest{QueryText: "SELECT 1", QueryTimeout: &tooLong}, "Query timeout cannot exceed 60 seconds for Run"},
		{"variables missing", models.APIQueryRequest{QueryText: "SELECT {{a}}"}, "Query contains template variables (a) but no variables were provided. Please define variable values before executing."},
		{"half a time range", models.APIQueryRequest{QueryText: "SELECT 1", StartTime: "2026-04-08T00:00:00Z"}, "start_time and end_time must both be provided"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PrepareSQLQuery(cfg, tc.req)
			reqErr, ok := errors.AsType[*QueryRequestError](err)
			if !ok || reqErr.Message != tc.want {
				t.Fatalf("error = %v, want QueryRequestError %q", err, tc.want)
			}
		})
	}
}

// TestRunQueryBoundsAndAuthorizedSource checks that RunQuery runs against the
// authorized source with the configured bounds and a context deadline.
func TestRunQueryBoundsAndAuthorizedSource(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	admin := &models.User{Email: "run-admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	source := newTestSource(t, db, "run_query_src")
	team, err := CreateTeam(ctx, db, log, "run-query", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	src, err := access.AuthorizeTeamSource(ctx, db, access.SessionPrincipal(admin), team.ID, source.ID, models.TokenScopeLogsRead)
	if err != nil {
		t.Fatalf("AuthorizeTeamSource: %v", err)
	}

	var (
		gotSource   models.SourceID
		gotParams   datasource.QueryRequest
		hasDeadline bool
	)
	ds := newFakeDatasourceService(db, log, &fakeProvider{
		queryLogsFn: func(ctx context.Context, s *models.Source, req datasource.QueryRequest) (*models.QueryResult, error) {
			gotSource, gotParams = s.ID, req
			_, hasDeadline = ctx.Deadline()
			return &models.QueryResult{}, nil
		},
	})
	cfg := config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 1000, DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 60}

	if _, err := RunQuery(ctx, ds, cfg, src, models.APIQueryRequest{QueryText: "SELECT 1"}); err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if gotSource != source.ID || gotParams.MaxLimit != 1000 || *gotParams.QueryTimeout != 30 || !hasDeadline {
		t.Fatalf("source %d params %+v deadline %v", gotSource, gotParams, hasDeadline)
	}

	tooLong := 61
	_, err = RunQuery(ctx, ds, cfg, src, models.APIQueryRequest{QueryText: "SELECT 1", QueryTimeout: &tooLong})
	if _, ok := errors.AsType[*QueryRequestError](err); !ok {
		t.Fatalf("RunQuery over max timeout: %v, want QueryRequestError", err)
	}
}

// TestStreamFilterValidation checks that every standard query path rejects
// malformed stream filters, and that a non-VictoriaLogs source rejects any
// stream filter instead of ignoring it.
func TestStreamFilterValidation(t *testing.T) {
	t.Parallel()
	cfg := config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 1000, DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 60}
	malformed := []string{`[{"app":"api"}]`}

	if _, err := PrepareSQLQuery(cfg, models.APIQueryRequest{QueryText: "*", ExtraStreamFilters: malformed}); !isQueryRequestError(err) {
		t.Fatalf("PrepareSQLQuery malformed filter: %v, want QueryRequestError", err)
	}
	if _, err := PrepareHistogram(models.APIHistogramRequest{QueryText: "*", ExtraStreamFilters: malformed}); !isQueryRequestError(err) {
		t.Fatalf("PrepareHistogram malformed filter: %v, want QueryRequestError", err)
	}
	picker := []string{`{"app":["api"]}`, `{env="prod"}`}
	params, err := PrepareSQLQuery(cfg, models.APIQueryRequest{QueryText: "*", ExtraStreamFilters: picker})
	if err != nil || len(params.ExtraStreamFilters) != 2 {
		t.Fatalf("PrepareSQLQuery picker filters: %+v, %v", params.ExtraStreamFilters, err)
	}
	histogram, err := PrepareHistogram(models.APIHistogramRequest{QueryText: "*", ExtraStreamFilters: picker})
	if err != nil || len(histogram.ExtraStreamFilters) != 2 {
		t.Fatalf("PrepareHistogram picker filters: %+v, %v", histogram.ExtraStreamFilters, err)
	}

	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	admin := &models.User{Email: "filters-admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	source := newTestSource(t, db, "stream_filter_src")
	team, err := CreateTeam(ctx, db, log, "stream-filters", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	src, err := access.AuthorizeTeamSource(ctx, db, access.SessionPrincipal(admin), team.ID, source.ID, models.TokenScopeLogsRead)
	if err != nil {
		t.Fatalf("AuthorizeTeamSource: %v", err)
	}
	queried := false
	ds := newFakeDatasourceService(db, log, &fakeProvider{
		queryLanguages: []models.QueryLanguage{models.QueryLanguageClickHouseSQL, models.QueryLanguageLogchefQL},
		queryLogsFn: func(context.Context, *models.Source, datasource.QueryRequest) (*models.QueryResult, error) {
			queried = true
			return &models.QueryResult{}, nil
		},
	})

	if _, err := RunQuery(ctx, ds, cfg, src, models.APIQueryRequest{QueryText: "SELECT 1"}); err != nil || !queried {
		t.Fatalf("RunQuery without filters: queried %v, %v", queried, err)
	}
	queried = false
	_, err = RunQuery(ctx, ds, cfg, src, models.APIQueryRequest{QueryText: "SELECT 1", ExtraStreamFilters: picker})
	if !datasource.IsValidationError(err) || queried {
		t.Fatalf("RunQuery on ClickHouse with filters: queried %v, %v, want ValidationError", queried, err)
	}
	_, err = RunHistogram(ctx, ds, src, models.APIHistogramRequest{QueryText: "*", ExtraStreamFilters: picker})
	if !datasource.IsValidationError(err) || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("RunHistogram on ClickHouse with filters: %v, want ValidationError containing \"invalid\"", err)
	}
	_, err = PrepareLogchefQLQuery(ctx, ds, cfg, src, LogchefQLQueryRequest{
		Query: `level="error"`, StartTime: "2026-04-08T00:00:00Z", EndTime: "2026-04-08T01:00:00Z", ExtraStreamFilters: picker,
	})
	if !isQueryRequestError(err) || !strings.Contains(err.Error(), "only VictoriaLogs") {
		t.Fatalf("PrepareLogchefQLQuery on ClickHouse with filters: %v, want QueryRequestError", err)
	}
}

func isQueryRequestError(err error) bool {
	_, ok := errors.AsType[*QueryRequestError](err)
	return ok
}
