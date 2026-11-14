package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestSendSourceActivityErrorTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   models.ErrorType
	}{
		{"source deleted", fmt.Errorf("inspect: %w", core.ErrSourceNotFound), http.StatusNotFound, models.NotFoundErrorType},
		{"activity unavailable", datasource.ErrSourceActivityUnavailable, http.StatusServiceUnavailable, models.ActivityUnsupportedErrorType},
		{"operation not supported", fmt.Errorf("inspect: %w", datasource.ErrOperationNotSupported), http.StatusServiceUnavailable, models.ActivityUnsupportedErrorType},
		{"scan limit exceeded", fmt.Errorf("inspect: %w", datasource.ErrSourceActivityLimitExceeded), http.StatusUnprocessableEntity, models.ActivityLimitExceededErrorType},
		{"view timeout", fmt.Errorf("%w: %w", datasource.ErrSourceActivityLimitExceeded, context.DeadlineExceeded), http.StatusUnprocessableEntity, models.ActivityLimitExceededErrorType},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, models.GeneralErrorType},
		{"backend failure", errors.New("connection refused"), http.StatusInternalServerError, models.GeneralErrorType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Server{log: slog.New(slog.DiscardHandler)}
			app := fiber.New()
			app.Get("/activity", func(c fiber.Ctx) error {
				return s.sendSourceActivity(c, 1, nil, tt.err)
			})

			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/activity", http.NoBody))
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var got Response
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("decode %s: %v", body, err)
			}
			if tt.name == "view timeout" && got.Message != "Recent activity query exceeded its time limit" {
				t.Fatalf("timeout message = %q", got.Message)
			}
			if resp.StatusCode != tt.wantStatus || got.ErrorType != string(tt.wantType) {
				t.Fatalf("got %d %q, want %d %q", resp.StatusCode, got.ErrorType, tt.wantStatus, tt.wantType)
			}
		})
	}
}

func TestSendSourceInspectionErrorTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   models.ErrorType
	}{
		{"source deleted", fmt.Errorf("inspect: %w", core.ErrSourceNotFound), http.StatusNotFound, models.NotFoundErrorType},
		{"not supported", fmt.Errorf("inspect: %w", datasource.ErrOperationNotSupported), http.StatusBadRequest, models.ValidationErrorType},
		{"backend failure", errors.New("connection refused"), http.StatusInternalServerError, models.ExternalServiceErrorType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Server{log: slog.New(slog.DiscardHandler)}
			app := fiber.New()
			app.Get("/stats", func(c fiber.Ctx) error {
				return s.sendSourceInspectionError(c, 1, tt.err)
			})

			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/stats", http.NoBody))
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var got Response
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("decode %s: %v", body, err)
			}
			if resp.StatusCode != tt.wantStatus || got.ErrorType != string(tt.wantType) {
				t.Fatalf("got %d %q, want %d %q", resp.StatusCode, got.ErrorType, tt.wantStatus, tt.wantType)
			}
		})
	}
}

func TestViewActivityCapResponseClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin, err := ch.Open(&ch.Options{Addr: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	view := fmt.Sprintf("logchef_activity_cap_%d", time.Now().UnixNano())
	if err := admin.Exec(ctx, "CREATE VIEW default."+view+" AS SELECT now64(3) - INTERVAL 1 MINUTE AS timestamp FROM numbers(100000)"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP VIEW default."+view)
	log := slog.New(slog.DiscardHandler)
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	rowsCap := int64(2)
	source := &models.Source{ID: 1, SourceType: models.SourceTypeClickHouse, MetaTSField: "timestamp", Connection: models.ConnectionInfo{
		Host: addr, Database: "default", TableName: view,
		Activity: &models.ClickHouseActivityConfig{AllowView: true, MaxRowsToRead: &rowsCap},
	}}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := datasource.NewClickHouseProvider(manager, log)
	s := &Server{log: log}
	app := fiber.New()
	app.Get("/activity", func(c fiber.Ctx) error {
		activity, err := provider.InspectSourceActivity(ctx, source)
		return s.sendSourceActivity(c, source.ID, activity, err)
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/activity", http.NoBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got Response
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnprocessableEntity || got.ErrorType != string(models.ActivityLimitExceededErrorType) {
		t.Fatalf("response = %d %+v, want 422 ActivityLimitExceeded", resp.StatusCode, got)
	}
}
