package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
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
		app.Get("/go", func(c *fiber.Ctx) error {
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
		tc := tc
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
