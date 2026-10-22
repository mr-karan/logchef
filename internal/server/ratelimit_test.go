package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
)

func TestWindowLimiterAllowsUpToLimit(t *testing.T) {
	l := auth.NewWindowLimiter(time.Minute, 3)
	for i := 1; i <= 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("request %d rejected, want allowed", i)
		}
	}
	if l.Allow("k") {
		t.Fatal("request 4 allowed, want rejected")
	}
	if l.Allow("k") {
		t.Fatal("request 5 allowed, want rejected")
	}
}

func TestWindowLimiterEmptyKeyAlwaysAllowed(t *testing.T) {
	l := auth.NewWindowLimiter(time.Minute, 1)
	for i := range 5 {
		if !l.Allow("") {
			t.Fatalf("empty key rejected on request %d", i)
		}
	}
}

func TestAuthRateLimitTrustsOnlyConfiguredProxy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		proxies []string
		want    []int
	}{
		{"untrusted peer", nil, []int{http.StatusInternalServerError, http.StatusTooManyRequests}},
		// app.Test uses 0.0.0.0 as its synthetic TCP peer.
		{"trusted peer", []string{"0.0.0.0/32"}, []int{http.StatusInternalServerError, http.StatusInternalServerError}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			db, err := sqlite.New(t.Context(), sqlite.Options{
				Logger: logger,
				Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "proxy.db")},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			s := New(ServerOptions{
				Config: &config.Config{
					Server: config.ServerConfig{TrustedProxies: tc.proxies, ProxyHeader: "X-Forwarded-For"},
					RateLimit: config.RateLimitConfig{
						Enabled: true, AuthPerIPPerMinute: 1,
					},
				},
				SQLite: db,
				FS: http.FS(fstest.MapFS{
					"index.html": {Data: []byte(`<base href="/" />`)},
				}),
				Logger: logger,
			})
			t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

			for i, forwarded := range []string{"198.51.100.10", "203.0.113.20"} {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", http.NoBody)
				req.Header.Set("X-Forwarded-For", forwarded)
				resp, err := s.app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != tc.want[i] {
					t.Fatalf("request %d: status %d, want %d", i+1, resp.StatusCode, tc.want[i])
				}
			}
		})
	}
}
