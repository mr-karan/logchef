package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/valyala/fasthttp"

	"github.com/mr-karan/logchef/internal/config"
)

// Exercise Fiber's raw HTTP parser, including the request line. Handler-only
// tests cannot reproduce the 431 returned before route matching.
func TestExplorerRequestHeaders(t *testing.T) {
	db := newServerTestDB(t)
	cfg := &config.Config{Auth: config.AuthConfig{APITokenSecret: authzTestTokenSecret}}
	s := New(ServerOptions{
		Config: cfg,
		SQLite: db,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		FS: http.FS(fstest.MapFS{
			"index.html": {Data: []byte(`<!doctype html><html><head><base href="/" /></head><body>Explorer</body></html>`)},
		}),
	})
	t.Cleanup(func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	for _, tc := range []struct {
		name, path, referer, cookie string
		want                        int
	}{
		{name: "ordinary explorer", path: "/logs/explore?team=1&source=4", want: http.StatusOK},
		{name: "long SQL link", path: "/logs/explore?team=1&source=4&mode=native&sql=" + url.QueryEscape("SELECT '"+strings.Repeat("x", 8192)+"'"), want: http.StatusOK},
		{name: "encoded Unicode link", path: "/logs/explore?team=1&source=4&q=" + url.QueryEscape(strings.Repeat("ошибка", 500)), want: http.StatusOK},
		{name: "long referer on API request", path: "/api/v1/health", referer: "https://example.test/logs/explore?q=" + strings.Repeat("x", 16384), want: http.StatusOK},
		{name: "normal proxy and cookie overhead", path: "/logs/explore?q=" + strings.Repeat("x", 24576), referer: "https://example.test/logs/explore?q=" + strings.Repeat("x", 24576), cookie: "synthetic=" + strings.Repeat("x", 4096), want: http.StatusOK},
		{name: "authentication remains required", path: "/api/v1/me?q=" + strings.Repeat("x", 8192), want: http.StatusUnauthorized},
		{name: "oversized request line remains bounded", path: "/logs/explore?q=" + strings.Repeat("x", 131072), want: http.StatusRequestHeaderFieldsTooLarge},
		{name: "oversized header remains bounded", path: "/api/v1/health", referer: "https://example.test/" + strings.Repeat("x", 131072), want: http.StatusRequestHeaderFieldsTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, http.NoBody)
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if tc.cookie != "" {
				req.Header.Set("Cookie", tc.cookie)
			}
			resp, err := s.app.Test(req)
			if tc.want == http.StatusRequestHeaderFieldsTooLarge {
				if _, ok := errors.AsType[*fasthttp.ErrSmallBuffer](err); !ok {
					t.Fatalf("expected bounded parser rejection, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("HTTP %d, want %d", resp.StatusCode, tc.want)
			}
			if tc.want == http.StatusOK && strings.HasPrefix(tc.path, "/logs/") {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(body), "Explorer") {
					t.Fatal("long Explorer link did not return the UI")
				}
			}
		})
	}
}
