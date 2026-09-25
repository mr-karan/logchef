package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"

	"github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
)

func newDashboardCacheTestServer(t *testing.T, enabled bool) *Server {
	t.Helper()
	c := cache.New(cache.Config{
		Enabled: enabled, MaxBytes: 1024, MaxEntryBytes: 128, MaxEntries: 8,
	})
	t.Cleanup(c.Close)
	return &Server{dashCache: c}
}

func newDashboardCacheTestContext() (c fiber.Ctx, release func()) {
	app := fiber.New()
	var req fasthttp.Request
	var ctx fasthttp.RequestCtx
	ctx.Init(&req, nil, nil)
	c = app.AcquireCtx(&ctx)
	return c, func() { app.ReleaseCtx(c) }
}

func TestDashboardCacheFillErrors(t *testing.T) {
	queryErr := errors.New("database query failed")
	for _, want := range []error{
		queryErr,
		fmt.Errorf("query: %w", datasource.ErrOperationNotSupported),
		fmt.Errorf("query: %w", context.Canceled),
		fmt.Errorf("query: %w", context.DeadlineExceeded),
	} {
		t.Run(want.Error(), func(t *testing.T) {
			s := newDashboardCacheTestServer(t, true)
			c, release := newDashboardCacheTestContext()
			defer release()
			key := [32]byte{1}
			handled, err := s.tryServeDashboardCache(c, key, time.Minute, time.Second,
				func(context.Context) ([]byte, error) { return nil, want })
			if handled || !errors.Is(err, want) {
				t.Fatalf("handled=%v, err=%v; want false and original error %v", handled, err, want)
			}
			if len(c.Response().Body()) != 0 {
				t.Fatalf("error wrote a success body: %s", c.Response().Body())
			}
			if _, _, ok := s.dashCache.Get(key); ok {
				t.Fatal("failed query was cached")
			}
		})
	}
}

func TestDashboardCacheBudgetFallback(t *testing.T) {
	s := newDashboardCacheTestServer(t, true)
	c, release := newDashboardCacheTestContext()
	defer release()
	handled, err := s.tryServeDashboardCache(c, [32]byte{}, time.Minute, time.Second,
		func(context.Context) ([]byte, error) {
			buffer := cappedBuffer{limit: 2}
			_, err := buffer.Write([]byte("large response"))
			return nil, fmt.Errorf("buffering response: %w", err)
		})
	if handled || err != nil {
		t.Fatalf("handled=%v, err=%v; want fallback", handled, err)
	}
	if got := string(c.Response().Header.Peek("X-Logchef-Cache")); got != "BYPASS" {
		t.Fatalf("cache header=%q, want BYPASS", got)
	}
}

func TestDashboardCacheUnavailableFallback(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprintf("absent=%v", absent), func(t *testing.T) {
			s := newDashboardCacheTestServer(t, false)
			if absent {
				s.dashCache = nil
			}
			c, release := newDashboardCacheTestContext()
			defer release()
			handled, err := s.tryServeDashboardCache(c, [32]byte{}, time.Minute, time.Second,
				func(context.Context) ([]byte, error) {
					t.Error("unavailable cache executed fill")
					return nil, nil
				})
			if handled || err != nil {
				t.Fatalf("handled=%v, err=%v; want fallback", handled, err)
			}
		})
	}
}

func TestDashboardCacheConcurrentFailureExecutesOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newDashboardCacheTestServer(t, true)
		defer s.dashCache.Close()
		s.config = &config.Config{}
		s.config.DashboardCache.MaxConcurrentFills = 1
		queryErr := errors.New("database query failed")
		var executions atomic.Int32
		release := make(chan struct{})
		fill := s.dashboardQueryFill(93001, 1, 1, "query", func(context.Context, string) ([]byte, error) {
			executions.Add(1)
			<-release
			return []byte("incomplete query result"), queryErr
		})
		var callers sync.WaitGroup
		for range 20 {
			callers.Go(func() {
				_, _, _, err := s.dashCache.GetOrFill(t.Context(), [32]byte{1}, time.Minute, time.Second, fill)
				if !errors.Is(err, queryErr) {
					t.Errorf("error=%v, want original query error", err)
				}
			})
		}
		// Every caller has joined the real cache's in-flight query.
		synctest.Wait()
		close(release)
		callers.Wait()
		if got := executions.Load(); got != 1 {
			t.Fatalf("query executed %d times, want 1", got)
		}
	})
}

func TestDashboardCacheFillDeadline(t *testing.T) {
	t.Run("bounded fill", func(t *testing.T) {
		s := newDashboardCacheTestServer(t, true)
		c, release := newDashboardCacheTestContext()
		defer release()
		handled, err := s.tryServeDashboardCache(c, [32]byte{}, time.Minute, time.Millisecond,
			func(ctx context.Context) ([]byte, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
		if handled || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("handled=%v, err=%v; want deadline error", handled, err)
		}
	})
}

func TestDashboardCacheAdmissionSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newDashboardCacheTestServer(t, true)
		defer s.dashCache.Close()
		s.config = &config.Config{}
		s.config.DashboardCache.MaxConcurrentFills = 1
		release := make(chan struct{})
		releaseFill := sync.OnceFunc(func() { close(release) })
		defer releaseFill()
		var executions atomic.Int32
		fill := s.dashboardQueryFill(93002, 1, 1, "query", func(context.Context, string) ([]byte, error) {
			executions.Add(1)
			<-release
			return []byte("result"), nil
		})
		var callers sync.WaitGroup
		for range 20 {
			callers.Go(func() {
				data, _, _, err := s.dashCache.GetOrFill(t.Context(), [32]byte{1}, time.Minute, time.Second, fill)
				if err != nil || string(data) != "result" {
					t.Errorf("coalesced response=%q, err=%v", data, err)
				}
			})
		}
		synctest.Wait()
		_, _, _, err := s.dashCache.GetOrFill(t.Context(), [32]byte{2}, time.Minute, time.Second, fill)
		if _, ok := errors.AsType[*QueryAdmissionError](err); !ok {
			t.Fatalf("distinct fill bypassed admission: %v", err)
		}
		releaseFill()
		callers.Wait()
		// Completion must release admission. An occupied slot must not block a hit.
		queryID, err := queryTracker.StartQuery(QueryClassDashboard, 93002, 1, 1, "occupied", func() {}, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer queryTracker.RemoveQuery(queryID)
		_, status, _, err := s.dashCache.GetOrFill(t.Context(), [32]byte{1}, time.Minute, time.Second, fill)
		if err != nil || status != cache.StatusHit || executions.Load() != 1 {
			t.Fatalf("hit status=%s, err=%v, executions=%d", status, err, executions.Load())
		}
	})
}

func TestDashboardCacheAdmissionCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline=%v", deadline), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newDashboardCacheTestServer(t, true)
				defer s.dashCache.Close()
				s.config = &config.Config{}
				s.config.DashboardCache.MaxConcurrentFills = 1
				var queryID string
				fill := s.dashboardQueryFill(93003, 1, 1, "query", func(ctx context.Context, id string) ([]byte, error) {
					queryID = id
					<-ctx.Done()
					return nil, ctx.Err()
				})
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if _, err := fill(ctx); !errors.Is(err, context.Canceled) || queryID != "" {
					t.Fatalf("already-canceled fill executed: id=%q, err=%v", queryID, err)
				}
				var fillErr error
				go func() {
					_, _, _, fillErr = s.dashCache.GetOrFill(t.Context(), [32]byte{1}, time.Minute, time.Second, fill)
				}()
				synctest.Wait()
				wantErr := context.Canceled
				if deadline {
					wantErr = context.DeadlineExceeded
					time.Sleep(time.Second)
				} else if !queryTracker.CancelQuery(queryID, 93003) {
					t.Fatal("shared fill was not registered for cancellation")
				}
				synctest.Wait()
				if !errors.Is(fillErr, wantErr) {
					t.Fatalf("fill error=%v, want %v", fillErr, wantErr)
				}
				id, err := queryTracker.StartQuery(QueryClassDashboard, 93003, 1, 1, "next", func() {}, 1, 0)
				if err != nil {
					t.Fatalf("canceled fill leaked admission: %v", err)
				}
				queryTracker.RemoveQuery(id)
			})
		})
	}
}

func TestDashboardCacheSuccessfulResponses(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		t.Run(fmt.Sprintf("oversized=%v", oversized), func(t *testing.T) {
			s := newDashboardCacheTestServer(t, true)
			body := `{"status":"success","data":"ok"}`
			if oversized {
				body = `{"status":"success","data":"` + strings.Repeat("x", 256) + `"}`
			}
			var executions atomic.Int32
			fill := func(context.Context) ([]byte, error) {
				executions.Add(1)
				return []byte(body), nil
			}
			for i := range 2 {
				c, release := newDashboardCacheTestContext()
				t.Cleanup(release)
				handled, err := s.tryServeDashboardCache(c, [32]byte{}, time.Minute, time.Second, fill)
				if !handled || err != nil {
					t.Fatalf("handled=%v, err=%v; want served response", handled, err)
				}
				if c.Response().StatusCode() != fiber.StatusOK || string(c.Response().Body()) != body {
					t.Fatalf("response=%s", c.Response())
				}
				wantStatus := cache.StatusMiss
				if oversized {
					wantStatus = cache.StatusBypass
				} else if i > 0 {
					wantStatus = cache.StatusHit
				}
				if got := string(c.Response().Header.Peek("X-Logchef-Cache")); got != string(wantStatus) {
					t.Fatalf("cache header=%q, want %s", got, wantStatus)
				}
			}
			wantExecutions := int32(1)
			if oversized {
				wantExecutions = 2
			}
			if got := executions.Load(); got != wantExecutions {
				t.Fatalf("executions=%d, want %d", got, wantExecutions)
			}
		})
	}
}
