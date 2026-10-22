package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
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
			s := &Server{localLoginLimiter: auth.NewLoginRateLimiter(time.Minute, 1, 1), store: newServerTestDB(t), config: &config.Config{Auth: config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}}}}
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
				resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
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

func TestLocalLoginLimiterServerScope(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			cfg := &config.Config{Auth: config.AuthConfig{Local: config.LocalAuthConfig{Enabled: enabled}}}
			first := newServerForTest(t, cfg, newServerTestDB(t), nil)
			if (first.localLoginLimiter != nil) != enabled {
				t.Fatal("local limiter allocation does not match local auth setting")
			}
			if !enabled {
				return
			}
			for range 5 {
				if !first.localLoginLimiter.Allow("ip", "user@example.com") {
					t.Fatal("first server budget exhausted early")
				}
			}
			if first.localLoginLimiter.Allow("ip", "user@example.com") {
				t.Fatal("first server did not exhaust email budget")
			}
			if err := first.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			second := newServerForTest(t, cfg, newServerTestDB(t), nil)
			if !second.localLoginLimiter.Allow("ip", "user@example.com") {
				t.Fatal("new server inherited a previous server's login budget")
			}
		})
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	for _, secure := range []bool{true, false} {
		t.Run(fmt.Sprintf("secure=%v", secure), func(t *testing.T) {
			db := newServerTestDB(t)
			s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil)), store: db, config: &config.Config{Auth: config.AuthConfig{
				Local: config.LocalAuthConfig{Enabled: true}, SessionDuration: time.Hour, MaxConcurrentSessions: 5,
			}}}
			s.config.Server.FrontendURL = "https://logs.example.com/logchef/"
			s.config.Server.SecureCookie = &secure
			s.localLoginLimiter = auth.NewLoginRateLimiter(time.Minute, 10, 5)
			secret := rand.Text()
			hash, err := auth.HashLocalPassword(secret)
			if err != nil {
				t.Fatal(err)
			}
			user := &models.User{Email: "local@example.com", FullName: "Local", Role: models.UserRoleMember, Status: models.UserStatusActive,
				AccountType: models.UserAccountTypeHuman}
			if err := db.CreateUser(t.Context(), user); err != nil {
				t.Fatal(err)
			}
			if err := db.SetUserPasswordHash(t.Context(), user.ID, hash); err != nil {
				t.Fatal(err)
			}
			app := fiber.New()
			app.Post("/login", s.handleLocalLogin)
			app.Post("/logout", s.handleLogout)
			sessionCookieFrom := func(resp *http.Response) *http.Cookie {
				t.Helper()
				for _, cookie := range resp.Cookies() {
					if cookie.Name == sessionCookieName {
						if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure != secure || cookie.Path != "/logchef" {
							t.Fatalf("session cookie protections = %+v", cookie)
						}
						return cookie
					}
				}
				t.Fatal("response set no session cookie")
				return nil
			}

			body, err := json.Marshal(map[string]string{"email": user.Email, "password": secret})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("login status = %d, want 200", resp.StatusCode)
			}
			set := sessionCookieFrom(resp)
			session, err := db.GetSession(t.Context(), models.SessionID(set.Value))
			if err != nil {
				t.Fatalf("login session was not persisted: %v", err)
			}
			if !set.Expires.Equal(session.ExpiresAt.Truncate(time.Second)) {
				t.Fatalf("cookie expires %v, session expires %v", set.Expires, session.ExpiresAt)
			}

			req = httptest.NewRequest(http.MethodPost, "/logout", http.NoBody)
			req.AddCookie(set)
			resp, err = app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			cleared := sessionCookieFrom(resp)
			if cleared.Value != "" || !cleared.Expires.Before(time.Now()) {
				t.Fatalf("logout did not clear the session cookie: %+v", cleared)
			}
		})
	}
}
