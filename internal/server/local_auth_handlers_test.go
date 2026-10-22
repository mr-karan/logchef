package server

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
)

func TestHandleLocalLoginGating(t *testing.T) {
	t.Parallel()

	newApp := func(enabled bool) *fiber.App {
		s := &Server{config: &config.Config{Auth: config.AuthConfig{Local: config.LocalAuthConfig{Enabled: enabled}}}}
		app := fiber.New()
		app.Post("/login", s.handleLocalLogin)
		return app
	}

	t.Run("disabled returns 404", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"a@example.com","password":"x-y-z-12345"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := newApp(false).Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing fields returns 400", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"a@example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := newApp(true).Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})
}

func TestHandleLocalLoginEmailLength(t *testing.T) {
	for _, email := range []string{
		strings.Repeat("a", 243) + "@example.com",
		strings.Repeat("é", 122) + "@example.com",
	} {
		t.Run(email[:8], func(t *testing.T) {
			previous := localLoginLimiter
			localLoginLimiter = auth.NewLoginRateLimiter(time.Minute, 1, 1)
			t.Cleanup(func() { localLoginLimiter = previous })
			s := &Server{sqlite: newServerTestDB(t), config: &config.Config{Auth: config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}}}}
			app := fiber.New()
			app.Post("/login", s.handleLocalLogin)
			request := func(email string, wantStatus int) []byte {
				t.Helper()
				body, err := json.Marshal(map[string]string{"email": email, "password": rand.Text()})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != wantStatus {
					t.Errorf("email length %d: status = %d, want %d", len(email), resp.StatusCode, wantStatus)
				}
				data, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			first := request(email, http.StatusUnauthorized)
			second := request(email, http.StatusUnauthorized)
			boundaryEmail := strings.Repeat("a", 242) + "@example.com"
			atLimit := request(boundaryEmail, http.StatusUnauthorized)
			if !bytes.Equal(first, second) || !bytes.Equal(first, atLimit) {
				t.Fatal("overlong emails must return the generic failure without consuming login budgets")
			}
			request(boundaryEmail, http.StatusTooManyRequests)
		})
	}
}
