package server

import (
	"bytes"
	"encoding/json"
	"errors"
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
		{"IdP access denied", errAccessDenied, "access_denied"},
		{"invalid state", errInvalidState, "session_expired"},
		{"unauthorized", auth.ErrUnauthorizedUser, "unauthorized"},
		{"inactive", auth.ErrUserInactive, "user_inactive"},
		{"invalid token", auth.ErrOIDCInvalidToken, "invalid_token"},
		{"unverified email", auth.ErrOIDCEmailNotVerified, "email_not_verified"},
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
				var logs bytes.Buffer
				s := &Server{
					log:    slog.New(slog.NewTextHandler(&logs, nil)),
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
				level := "WARN"
				if tc.code == "auth_error" {
					level = "ERROR"
				}
				if !strings.Contains(logs.String(), "level="+level) {
					t.Errorf("authentication failure log = %s, want level %s", logs.String(), level)
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

func TestHandleCallbackErrors(t *testing.T) {
	t.Parallel()
	state := fmt.Sprintf("state.nonce.%d", time.Now().Unix())
	expiredState := fmt.Sprintf("state.nonce.%d", time.Now().Add(-10*time.Minute).Unix())
	cases := []struct {
		name   string
		query  string
		cookie string
		code   string
	}{
		{"IdP access denied", "error=access_denied&error_description=provider-detail&state=" + state, state, "access_denied"},
		{"IdP server error", "error=server_error&error_description=provider-detail&state=" + state, state, "auth_error"},
		{"missing state cookie", "code=code&state=" + state, "", "session_expired"},
		{"mismatched state cookie", "code=code&state=other", state, "session_expired"},
		{"missing state parameter", "code=code", state, "session_expired"},
		{"expired state cookie", "code=code&state=" + expiredState, expiredState, "session_expired"},
		{"malformed state cookie", "code=code&state=malformed", "malformed", "session_expired"},
		{"invalid state timestamp", "code=code&state=state.nonce.invalid", "state.nonce.invalid", "session_expired"},
		{"missing code", "state=" + state, state, "auth_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			s := &Server{
				log:          slog.New(slog.NewTextHandler(&logs, nil)),
				config:       &config.Config{Server: config.ServerConfig{FrontendURL: "https://logchef.example/"}},
				oidcProvider: &auth.OIDCProvider{},
			}
			app := fiber.New()
			app.Get("/api/v1/auth/callback", s.handleCallback)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/callback?"+tc.query, http.NoBody)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: stateCookieName, Value: tc.cookie})
			}
			resp, err := app.Test(req)
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
			if got := strings.Count(strings.TrimSpace(logs.String()), "\n") + 1; got != 1 {
				t.Errorf("callback logged %d lines, want one: %s", got, logs.String())
			}
			if !strings.Contains(logs.String(), "error_code="+tc.code) {
				t.Errorf("missing stable error code: %s", logs.String())
			}
			if strings.Contains(tc.query, "error_description=") && !strings.Contains(logs.String(), "error_description=provider-detail") {
				t.Error("IdP error description missing from server log")
			}
		})
	}
}

func TestCallbackTokenFailureLogsOnce(t *testing.T) {
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": idp.URL, "jwks_uri": idp.URL + "/jwks"})
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"token","token_type":"Bearer"}`)
	}))
	defer idp.Close()
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	provider, err := auth.NewOIDCProvider(t.Context(), &config.OIDCConfig{ProviderURL: idp.URL, TokenURL: idp.URL + "/token"}, log)
	if err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	s := &Server{log: log, config: &config.Config{}, oidcProvider: provider}
	app := fiber.New()
	app.Get("/callback", s.handleCallback)
	state := fmt.Sprintf("state.nonce.%d", time.Now().Unix())
	req := httptest.NewRequest(http.MethodGet, "/callback?code=code&state="+state, http.NoBody)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Location"); got != "/auth/login?error=invalid_token" {
		t.Fatalf("redirect = %q", got)
	}
	if got := strings.Count(strings.TrimSpace(logs.String()), "\n") + 1; got != 1 {
		t.Fatalf("callback logged %d lines, want one: %s", got, logs.String())
	}
	if !strings.Contains(logs.String(), "error_code=invalid_token") {
		t.Fatalf("missing stable error code: %s", logs.String())
	}
}
