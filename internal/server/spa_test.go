package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
)

func newSPATestServer(t *testing.T, frontendURL string) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: logger,
		Config: config.SQLiteConfig{Path: sqlitetest.Path(t)},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ui := fstest.MapFS{
		"index.html":    {Data: []byte(`<html><head>` + baseHrefTag + `<script src="./assets/app.js"></script></head><body>` + strings.Repeat("<p>long enough to compress</p>", 20) + `</body></html>`)},
		"logo.svg":      {Data: []byte("<svg/>" + strings.Repeat("<!-- long enough to compress -->", 20))},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	s := New(ServerOptions{
		Config: &config.Config{Server: config.ServerConfig{FrontendURL: frontendURL}},
		SQLite: store,
		FS:     http.FS(ui),
		Logger: logger,
	})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return s
}

func getSPA(t *testing.T, s *Server, path string) (status int, body string) {
	t.Helper()
	resp, err := s.app.Test(httptest.NewRequest(http.MethodGet, path, http.NoBody))
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: reading body: %v", path, err)
	}
	return resp.StatusCode, string(raw)
}

func TestSPAServesIndexWithConfiguredBaseHref(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		frontendURL string
		wantBase    string
	}{
		{"", `<base href="/" />`},
		{"https://logs.example.com", `<base href="/" />`},
		{"https://example.com/logchef", `<base href="/logchef/" />`},
		{"https://example.com/tools/logchef/", `<base href="/tools/logchef/" />`},
	} {
		s := newSPATestServer(t, tc.frontendURL)
		for _, path := range []string{"/", "/index.html", "/logs/explore", "/logs/saved/42?team=1"} {
			status, body := getSPA(t, s, path)
			if status != http.StatusOK {
				t.Errorf("frontend_url=%q GET %s: status %d, want 200", tc.frontendURL, path, status)
			}
			if !strings.Contains(body, tc.wantBase) {
				t.Errorf("frontend_url=%q GET %s: body %q lacks %s", tc.frontendURL, path, body, tc.wantBase)
			}
		}
	}
}

// Routes and files registered before the SPA fallback must not be answered
// with index.html.
func TestSPAFallbackDoesNotShadowOtherRoutes(t *testing.T) {
	t.Parallel()
	s := newSPATestServer(t, "https://example.com/logchef")

	for _, tc := range []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/logo.svg", http.StatusOK, "<svg/>"},
		{"/assets/app.js", http.StatusOK, "console.log(1)"},
		{"/assets/does-not-exist.js", http.StatusNotFound, ""},
		{"/api/v1/does-not-exist", http.StatusNotFound, `"status":"error"`},
		{"/metrics", http.StatusOK, "go_goroutines"},
	} {
		status, body := getSPA(t, s, tc.path)
		if status != tc.wantStatus || !strings.Contains(body, tc.wantBody) || strings.Contains(body, "<base href") {
			t.Errorf("GET %s: status %d body %q, want %d containing %q and no index.html", tc.path, status, body, tc.wantStatus, tc.wantBody)
		}
	}
}

// Hashed assets never change, so browsers keep them without asking again.
// Everything else revalidates, so the next load after a deploy gets the new
// index.html and through it the new asset names.
func TestSPACacheHeaders(t *testing.T) {
	t.Parallel()
	s := newSPATestServer(t, "")

	for _, tc := range []struct {
		path             string
		wantCacheControl string
		wantRevalidation bool
	}{
		{"/assets/app.js", "public, max-age=31536000, immutable", false},
		{"/", "no-cache", true},
		{"/index.html", "no-cache", true},
		{"/logs/explore?team=1", "no-cache", true},
		{"/logo.svg", "no-cache", true},
	} {
		for _, encoding := range []string{"", "gzip"} {
			status, header := requestSPA(t, s, tc.path, map[string]string{"Accept-Encoding": encoding})
			if status != http.StatusOK {
				t.Fatalf("GET %s (%q): status %d, want 200", tc.path, encoding, status)
			}
			if got := header.Get("Cache-Control"); got != tc.wantCacheControl {
				t.Errorf("GET %s (%q): Cache-Control %q, want %q", tc.path, encoding, got, tc.wantCacheControl)
			}
			if got := header.Get("Last-Modified"); got != "" {
				t.Errorf("GET %s (%q): Last-Modified %q, want none", tc.path, encoding, got)
			}
			if !tc.wantRevalidation {
				continue
			}
			etag := header.Get("ETag")
			if etag == "" {
				t.Errorf("GET %s (%q): no ETag to revalidate with", tc.path, encoding)
				continue
			}
			again, _ := requestSPA(t, s, tc.path, map[string]string{"Accept-Encoding": encoding, "If-None-Match": etag})
			if again != http.StatusNotModified {
				t.Errorf("GET %s (%q) with If-None-Match %s: status %d, want 304", tc.path, encoding, etag, again)
			}
		}
	}
}

// During a rolling deploy a chunk can be missing on one replica and present
// on another, so a CDN must not keep the 404.
func TestSPAMissingAssetIsNotCached(t *testing.T) {
	t.Parallel()
	s := newSPATestServer(t, "")

	status, header := requestSPA(t, s, "/assets/does-not-exist.js", nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET missing asset: status %d, want 404", status)
	}
	if got := header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("GET missing asset: Cache-Control %q, want %q", got, "no-store")
	}
}

func requestSPA(t *testing.T, s *Server, path string, headers map[string]string) (int, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	for name, value := range headers {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
	resp, err := s.app.Test(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header
}
