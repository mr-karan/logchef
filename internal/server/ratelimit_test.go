package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
)

func TestWindowLimiterAllowsUpToLimit(t *testing.T) {
	l := newWindowLimiter(time.Minute, 3)
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

func TestWindowLimiterIsolatesKeys(t *testing.T) {
	l := newWindowLimiter(time.Minute, 1)
	if !l.Allow("a") {
		t.Fatal("first request for a rejected")
	}
	if l.Allow("a") {
		t.Fatal("second request for a allowed, want rejected")
	}
	// A different key has its own independent window.
	if !l.Allow("b") {
		t.Fatal("first request for b rejected; keys are not isolated")
	}
}

func TestWindowLimiterEmptyKeyAlwaysAllowed(t *testing.T) {
	l := newWindowLimiter(time.Minute, 1)
	for i := range 5 {
		if !l.Allow("") {
			t.Fatalf("empty key rejected on request %d", i)
		}
	}
}

func TestWindowLimiterResetsAfterWindow(t *testing.T) {
	l := newWindowLimiter(20*time.Millisecond, 1)
	if !l.Allow("k") {
		t.Fatal("first request rejected")
	}
	if l.Allow("k") {
		t.Fatal("second request within window allowed, want rejected")
	}
	time.Sleep(30 * time.Millisecond)
	if !l.Allow("k") {
		t.Fatal("request after window elapsed rejected, want allowed")
	}
}

func TestWindowLimiterPrunesStaleKeys(t *testing.T) {
	l := newWindowLimiter(time.Second, 5)
	l.Allow("stale")
	for i := 1; i < 10_000; i++ {
		l.Allow(strconv.Itoa(i))
	}
	if l.Allow("overflow") {
		t.Fatal("new key admitted at capacity")
	}
	time.Sleep(1100 * time.Millisecond)
	if !l.Allow("fresh") {
		t.Fatal("expired keys did not release capacity")
	}
	if !l.Allow("stale") {
		t.Fatal("expired key did not get a new window")
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
