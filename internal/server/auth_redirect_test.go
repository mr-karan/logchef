package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
)

// TestIsSafeLocalPath covers the open-redirect guard (#89): only local absolute
// paths are accepted; protocol-relative, scheme-bearing, and relative paths are
// rejected.
func TestIsSafeLocalPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		{"/logs/explore", true},
		{"/", true},
		{"/a/b?c=d#e", true},
		{"", false},
		{"//evil.example/phish", false},
		{`/\evil.example/phish`, false},
		{"https://evil.example", false},
		{"http://evil.example", false},
		{"javascript://evil", false},
		{"logs/explore", false}, // relative, no leading slash
		{" /logs", false},       // leading space, not a '/'
	}
	for _, tc := range cases {
		if got := isSafeLocalPath(tc.path); got != tc.want {
			t.Errorf("isSafeLocalPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestRedirectToFrontendAuthErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"unauthorized", auth.ErrUnauthorizedUser, "unauthorized"},
		{"inactive", auth.ErrUserInactive, "user_inactive"},
		{"invalid token", auth.ErrOIDCInvalidToken, "invalid_token"},
		{"unverified email", auth.ErrOIDCEmailNotVerified, "email_not_verified"},
		{"missing session", core.ErrSessionNotFound, "session_not_found"},
		{"expired session", core.ErrSessionExpired, "session_expired"},
		{"missing user", core.ErrUserNotFound, "user_not_found"},
		{"unknown error", errors.New("unexpected failure"), "auth_error"},
	}
	for _, tc := range cases {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/wrapped=%t", tc.name, wrapped), func(t *testing.T) {
				t.Parallel()
				loginErr := tc.err
				if wrapped {
					loginErr = fmt.Errorf("callback failed: %w", loginErr)
				}
				s := &Server{
					log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
					config: &config.Config{Server: config.ServerConfig{FrontendURL: "https://logchef.example/"}},
				}
				app := fiber.New()
				app.Get("/callback", func(c fiber.Ctx) error {
					return s.redirectToFrontend(c, "/logs/explore", loginErr)
				})
				resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/callback", http.NoBody))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusTemporaryRedirect {
					t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTemporaryRedirect)
				}
				want := "https://logchef.example/auth/login?error=" + tc.code
				if got := resp.Header.Get("Location"); got != want {
					t.Errorf("Location = %q, want %q", got, want)
				}
			})
		}
	}
}

// TestRedirectToFrontendRejectsOpenRedirect ensures that with an empty
// frontend_url (the documented default), an attacker-supplied protocol-relative
// path cannot become the Location header — it falls back to root instead.
func TestRedirectToFrontendRejectsOpenRedirect(t *testing.T) {
	t.Parallel()

	newApp := func(path string) *fiber.App {
		s := &Server{
			log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
			config: &config.Config{Server: config.ServerConfig{FrontendURL: ""}},
		}
		app := fiber.New()
		app.Get("/go", func(c fiber.Ctx) error {
			return s.redirectToFrontend(c, path, nil)
		})
		return app
	}

	cases := []struct {
		name         string
		path         string
		wantLocation string
	}{
		{"protocol relative", "//evil.example/phish", "/"},
		{"backslash variant", `/\evil.example/phish`, "/"},
		{"absolute url", "https://evil.example", "/"},
		{"valid local path", "/logs/explore", "/logs/explore"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := newApp(tc.path).Test(httptest.NewRequest(http.MethodGet, "/go", http.NoBody))
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			if got := resp.Header.Get("Location"); got != tc.wantLocation {
				t.Errorf("Location = %q, want %q", got, tc.wantLocation)
			}
		})
	}
}
