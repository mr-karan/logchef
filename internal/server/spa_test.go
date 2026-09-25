package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
)

func newSPATestServer(t *testing.T, frontendURL string) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: logger,
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "spa.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ui := fstest.MapFS{
		"index.html":    {Data: []byte(`<html><head>` + baseHrefTag + `<script src="./assets/app.js"></script></head></html>`)},
		"logo.svg":      {Data: []byte("<svg/>")},
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

func TestSPAServesStaticFilesAndKeepsAPINotFound(t *testing.T) {
	t.Parallel()
	s := newSPATestServer(t, "https://example.com/logchef")

	for _, tc := range []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/logo.svg", http.StatusOK, "<svg/>"},
		{"/assets/app.js", http.StatusOK, "console.log(1)"},
		{"/api/v1/does-not-exist", http.StatusNotFound, `"status":"error"`},
	} {
		status, body := getSPA(t, s, tc.path)
		if status != tc.wantStatus || !strings.Contains(body, tc.wantBody) {
			t.Errorf("GET %s: status %d body %q, want %d containing %q", tc.path, status, body, tc.wantStatus, tc.wantBody)
		}
	}
}
