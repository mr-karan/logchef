package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestStartupReapsInterruptedExports(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var db store.Store
			if backend == "sqlite" {
				db = newServerTestDB(t)
			} else {
				dsn := os.Getenv("LOGCHEF_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("LOGCHEF_TEST_POSTGRES_DSN not set")
				}
				var err error
				db, err = postgres.New(context.Background(), postgres.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: config.PostgresConfig{DSN: dsn}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
			}
			ctx := context.Background()
			user := mkTestUser(t, db, uuid.NewString()+"@example.com", models.UserRoleMember)
			_, source := mkTestTeam(t, db, uuid.NewString(), user)
			var jobs []*models.ExportJob
			var paths []string
			for _, status := range []models.ExportJobStatus{models.ExportJobStatusPending, models.ExportJobStatusRunning, models.ExportJobStatusComplete} {
				now := time.Now().UTC().Add(-2 * time.Hour)
				job := &models.ExportJob{ID: uuid.NewString(), SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusPending, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: time.Now().Add(time.Hour), CreatedAt: now, UpdatedAt: now}
				if status == models.ExportJobStatusRunning {
					job.Status = status
				}
				if err := db.CreateExportJob(ctx, job); err != nil {
					t.Fatal(err)
				}
				f, err := os.CreateTemp("", "logchef-export-"+job.ID+"-*.csv")
				if err != nil {
					t.Fatal(err)
				}
				_ = f.Close()
				t.Cleanup(func() { _ = os.Remove(f.Name()) })
				if status == models.ExportJobStatusComplete {
					if err := db.CompleteExportJob(ctx, job.ID, "export.csv", f.Name(), 1, 0, now); err != nil {
						t.Fatal(err)
					}
				}
				job.Status = status
				jobs = append(jobs, job)
				paths = append(paths, f.Name())
			}
			replicaDB := db
			if backend == "postgres" {
				var err error
				replicaDB, err = postgres.New(ctx, postgres.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: config.PostgresConfig{DSN: os.Getenv("LOGCHEF_TEST_POSTGRES_DSN")}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = replicaDB.Close() })
			}
			replicaB := New(ServerOptions{Config: testOAuthConfig(), SQLite: replicaDB, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), FS: http.FS(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})})
			t.Cleanup(func() { _ = replicaB.Shutdown(ctx) })
			fresh := &models.ExportJob{ID: uuid.NewString(), SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusRunning, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if err := replicaDB.CreateExportJob(ctx, fresh); err != nil {
				t.Fatal(err)
			}
			partial, err := os.CreateTemp("", "logchef-export-"+fresh.ID+"-*.csv")
			if err != nil {
				t.Fatal(err)
			}
			_ = partial.Close()
			t.Cleanup(func() { _ = os.Remove(partial.Name()) })
			for range 2 {
				srv := New(ServerOptions{Config: testOAuthConfig(), SQLite: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), FS: http.FS(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})})
				stale := *fresh
				stale.ID = uuid.NewString()
				stale.UpdatedAt = time.Now().UTC().Add(-2 * time.Hour)
				if err := db.CreateExportJob(ctx, &stale); err != nil {
					t.Fatal(err)
				}
				srv.cleanupExpiredBackgroundState()
				reaped, err := db.GetExportJob(ctx, stale.ID)
				if err != nil {
					t.Fatal(err)
				}
				if reaped.Status != models.ExportJobStatusFailed {
					t.Errorf("periodic cleanup left stale job %s", reaped.Status)
				}
				if err := srv.Shutdown(ctx); err != nil {
					t.Fatal(err)
				}
				gotFresh, err := db.GetExportJob(ctx, fresh.ID)
				if err != nil {
					t.Fatal(err)
				}
				if gotFresh.Status != models.ExportJobStatusRunning {
					t.Errorf("other replica's fresh job = %s, want running", gotFresh.Status)
				}
				if _, err := os.Stat(partial.Name()); err != nil {
					t.Errorf("other replica's partial file removed: %v", err)
				}
				for i, job := range jobs {
					got, err := db.GetExportJob(ctx, job.ID)
					if err != nil {
						t.Fatal(err)
					}
					if job.Status == models.ExportJobStatusComplete {
						if got.Status != job.Status {
							t.Fatalf("complete job changed to %s", got.Status)
						}
						if _, err := os.Stat(paths[i]); err != nil {
							t.Fatal(err)
						}
					} else {
						if got.Status != models.ExportJobStatusFailed || got.ErrorMessage != "interrupted by restart" {
							t.Errorf("stale %s job = %s, %q", job.Status, got.Status, got.ErrorMessage)
						}
						if _, err := os.Stat(paths[i]); !os.IsNotExist(err) {
							t.Errorf("partial artifact still exists: %s (%v)", filepath.Base(paths[i]), err)
						}
						if err := db.CompleteExportJob(ctx, job.ID, "export.csv", paths[i], 1, 0, time.Now()); err == nil {
							t.Error("reaped job can be completed")
						}
						if err := db.UpdateExportJobRunning(ctx, job.ID); err == nil {
							t.Error("reaped job can be restarted")
						}
					}
				}
			}
		})
	}
}

func TestServerShutdownBoundsBackgroundWait(t *testing.T) {
	srv := newServerForTest(t, testOAuthConfig(), newServerTestDB(t), nil)
	done := make(chan struct{})
	srv.wg.Go(func() {
		defer close(done)
		time.Sleep(300 * time.Millisecond)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := srv.Shutdown(ctx)
	elapsed := time.Since(start)
	<-done
	if err == nil || elapsed > 200*time.Millisecond {
		t.Fatalf("Shutdown = %v after %s, want bounded timeout", err, elapsed)
	}
}

func TestServerShutdownLeavesNoMaintenanceGoroutines(t *testing.T) {
	db := newServerTestDB(t)
	before := runtime.NumGoroutine()
	for range 5 {
		srv := newServerForTest(t, testOAuthConfig(), db, nil)
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}
	stack := make([]byte, 1<<20)
	stack = stack[:runtime.Stack(stack, true)]
	if bytes.Contains(stack, []byte("server.init.")) && strings.Contains(string(stack), "logs_handlers.go") {
		t.Error("package-init query cleanup goroutine survives server shutdown")
	}
	if got := runtime.NumGoroutine(); got > before+2 {
		t.Fatalf("goroutines before=%d after=%d", before, got)
	}
}

func TestExportJobCancelledAtShutdown(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("LOGCHEF_TEST_CLICKHOUSE_ADDR not set")
	}
	db := newServerTestDB(t)
	ctx := context.Background()
	user := mkTestUser(t, db, "export-shutdown@example.com", models.UserRoleMember)
	team, source := mkTestTeam(t, db, "export-shutdown", user)
	source.Connection = models.ConnectionInfo{Host: addr, Username: "default", Database: "system", TableName: "numbers"}
	if err := db.UpdateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	ds := datasource.NewService(db, log)
	ds.Register(datasource.NewClickHouseProvider(manager, log))
	cfg := testOAuthConfig()
	cfg.Export = config.ExportConfig{MaxRows: 10000000, DefaultTimeoutSeconds: 60, MaxTimeoutSeconds: 60, MaxConcurrentPerUser: 1, MaxConcurrentGlobal: 1, ArtifactTTL: time.Hour, Formats: []string{"csv"}}
	srv := New(ServerOptions{Config: cfg, SQLite: db, ClickHouse: manager, Datasources: ds, Logger: log, FS: http.FS(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})})
	t.Cleanup(func() {
		select {
		case <-srv.stop:
			return
		default:
		}
		_ = srv.Shutdown(ctx)
	})
	app := fiber.New()
	app.Use(requestContext(context.Background()))
	path := fmt.Sprintf("/teams/%d/sources/%d/exports", team.ID, source.ID)
	withUser(app, http.MethodPost, "/teams/:teamID/sources/:sourceID/exports", user, srv.handleCreateExportJob)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"raw_sql":"SELECT number, sleepEachRow(0.00001) FROM system.numbers LIMIT 10000000","format":"csv","limit":10000000,"query_timeout":60}`))
	req.Header.Set("Content-Type", "application/json")
	response := testRequest(t, app, req)
	if response.StatusCode != http.StatusAccepted {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("create export = %d, %s", response.StatusCode, body)
	}
	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { queryTracker.CancelQuery(result.Data.ID, user.ID) })
	var files []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		files, _ = filepath.Glob(filepath.Join(os.TempDir(), "logchef-export-"+result.Data.ID+"-*"))
		if len(files) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(files) == 0 {
		job, _ := db.GetExportJob(ctx, result.Data.ID)
		t.Fatalf("export never created artifact: %+v", job)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	job, err := db.GetExportJob(ctx, result.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != models.ExportJobStatusFailed || job.ErrorMessage != "export cancelled" {
		t.Errorf("job after shutdown = %s, %q", job.Status, job.ErrorMessage)
	}
	for _, file := range files {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("artifact remains: %s", file)
		}
	}
}

func TestQueryHistoryBackpressure(t *testing.T) {
	db := newServerTestDB(t)
	srv := newServerForTest(t, testOAuthConfig(), db, nil)
	ctx := context.Background()
	user := mkTestUser(t, db, "history-bound@example.com", models.UserRoleMember)
	team, source := mkTestTeam(t, db, "history-bound", user)
	ready, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { done <- db.WithTx(ctx, func(store.StoreOps) error { close(ready); <-release; return nil }) }()
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	<-ready
	before := runtime.NumGoroutine()
	start := time.Now()
	for range 512 {
		srv.recordQueryHistory(user, team.ID, source.ID, "SELECT 1", models.QueryLanguageClickHouseSQL, 1, 1)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("history submission blocked for %s", elapsed)
	}
	runtime.Gosched()
	if got := runtime.NumGoroutine(); got > before+10 {
		t.Errorf("history writers are unbounded: before=%d after=%d", before, got)
	}
}

func TestHistoryDrainsOnShutdown(t *testing.T) {
	db := newServerTestDB(t)
	srv := newServerForTest(t, testOAuthConfig(), db, nil)
	user := mkTestUser(t, db, "drain@example.com", models.UserRoleMember)
	team, source := mkTestTeam(t, db, "drain", user)
	ready, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- db.WithTx(context.Background(), func(store.StoreOps) error { close(ready); <-release; return nil })
	}()
	<-ready
	for range 64 {
		srv.recordQueryHistory(user, team.ID, source.ID, "SELECT 1", models.QueryLanguageClickHouseSQL, 1, 1)
	}
	go func() { <-srv.baseCtx.Done(); close(release) }()
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	entries, err := db.ListQueryHistory(context.Background(), user.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 64 {
		t.Errorf("persisted %d of 64 queued entries, dropped=%d", len(entries), srv.historyDropped.Load())
	}
}

func TestExportDeadlineReason(t *testing.T) {
	db := newServerTestDB(t)
	srv := newServerForTest(t, testOAuthConfig(), db, nil)
	user := mkTestUser(t, db, "deadline@example.com", models.UserRoleMember)
	_, source := mkTestTeam(t, db, "deadline", user)
	now := time.Now()
	job := &models.ExportJob{ID: uuid.NewString(), SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusRunning, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := db.CreateExportJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), now.Add(-time.Second))
	defer cancel()
	srv.finishExportJob(ctx, job.ID, nil, "", false, nil)
	got, err := db.GetExportJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ErrorMessage != "export timed out" {
		t.Errorf("deadline reason = %q, want export timed out", got.ErrorMessage)
	}
}

func TestHistoryShutdownCountsUndrainedEntries(t *testing.T) {
	db := newServerTestDB(t)
	srv := newServerForTest(t, testOAuthConfig(), db, nil)
	user := mkTestUser(t, db, "drain-budget@example.com", models.UserRoleMember)
	team, source := mkTestTeam(t, db, "drain-budget", user)
	ready, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- db.WithTx(context.Background(), func(store.StoreOps) error { close(ready); <-release; return nil })
	}()
	<-ready
	for range 64 {
		srv.recordQueryHistory(user, team.ID, source.ID, "SELECT 1", models.QueryLanguageClickHouseSQL, 1, 1)
	}
	deadline := time.Now().Add(time.Second)
	for len(srv.historyQueue) == 64 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = srv.Shutdown(ctx)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-srv.backgroundDone:
	case <-time.After(time.Second):
		t.Fatal("history worker did not stop")
	}
	entries, err := db.ListQueryHistory(context.Background(), user.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	dropped := srv.historyDropped.Load()
	if dropped == 0 || uint64(len(entries))+dropped != 64 {
		t.Errorf("persisted=%d dropped=%d, want all 64 accounted for with drops", len(entries), dropped)
	}
}
