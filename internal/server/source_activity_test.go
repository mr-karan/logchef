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
	"testing"

	"github.com/gofiber/fiber/v3"

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
