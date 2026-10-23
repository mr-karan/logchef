package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func backgroundTestStore(t *testing.T, backend string) (store.Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var db store.Store
	var err error
	driver, address := "sqlite", filepath.Join(t.TempDir(), "background.db")
	if backend == "postgres" {
		driver, address = "pgx", os.Getenv("LOGCHEF_TEST_POSTGRES_DSN")
		if address == "" {
			t.Skip("LOGCHEF_TEST_POSTGRES_DSN not set")
		}
		db, err = postgres.New(ctx, postgres.Options{Logger: log, Config: config.PostgresConfig{DSN: address}})
	} else {
		db, err = sqlite.New(ctx, sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: address}})
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	raw, err := sql.Open(driver, address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return db, raw
}

func TestReapContinuesAfterArtifactRemoveError(t *testing.T) {
	db := newServerTestDB(t)
	ctx := context.Background()
	user := mkTestUser(t, db, "reap-remove@example.com", models.UserRoleMember)
	_, source := mkTestTeam(t, db, "reap-remove", user)
	var logs bytes.Buffer
	srv := &Server{store: db, baseCtx: ctx, config: testOAuthConfig(), log: slog.New(slog.NewTextHandler(&logs, nil))}
	ids := []string{uuid.NewString(), uuid.NewString()}
	sort.Strings(ids)
	var partials []string
	for _, id := range ids {
		old := time.Now().UTC().Add(-2 * time.Hour)
		job := &models.ExportJob{ID: id, SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusRunning, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: time.Now().Add(time.Hour), CreatedAt: old, UpdatedAt: old}
		if err := db.CreateExportJob(ctx, job); err != nil {
			t.Fatal(err)
		}
		dir, err := os.MkdirTemp("", "logchef-export-"+id+"-a-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if id == ids[0] {
			if err := os.WriteFile(filepath.Join(dir, "held"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		file, err := os.CreateTemp("", "logchef-export-"+id+"-z-*.csv")
		if err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		t.Cleanup(func() { _ = os.Remove(file.Name()) })
		partials = append(partials, file.Name())
	}
	srv.reapInterruptedExportJobs(models.ExportJobOwner{})
	for _, path := range partials {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("partial artifact remains after another remove failed: %s (%v)", path, err)
		}
	}
	if !strings.Contains(logs.String(), "failed to remove interrupted export artifact") || !strings.Contains(logs.String(), ids[0]) {
		t.Errorf("missing cleanup error with job ID: %s", logs.String())
	}
}

func TestExportRunningUsesDatabaseClock(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db, _ := backgroundTestStore(t, backend)
			ctx := context.Background()
			user := mkTestUser(t, db, uuid.NewString()+"@example.com", models.UserRoleMember)
			_, source := mkTestTeam(t, db, uuid.NewString(), user)
			now := time.Now().UTC()
			job := &models.ExportJob{ID: uuid.NewString(), SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusRunning, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now.Add(10 * time.Second)}
			if err := db.CreateExportJob(ctx, job); err != nil {
				t.Fatal(err)
			}
			srv := &Server{store: db, baseCtx: ctx, config: testOAuthConfig(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			srv.reapInterruptedExportJobs(models.ExportJobOwner{})
			got, err := db.GetExportJob(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != models.ExportJobStatusRunning {
				t.Fatalf("future-stamped job reaped within window: %s", got.Status)
			}
			if err := db.UpdateExportJobRunning(ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			got, err = db.GetExportJob(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.UpdatedAt.Before(now.Add(-2*time.Second)) || got.UpdatedAt.After(time.Now().Add(2*time.Second)) {
				t.Errorf("running timestamp used application clock: %s", got.UpdatedAt)
			}
			srv.reapInterruptedExportJobs(models.ExportJobOwner{})
			got, err = db.GetExportJob(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != models.ExportJobStatusRunning {
				t.Errorf("live job reaped after starting with skewed clock: %s", got.Status)
			}
			stale := *job
			stale.ID = uuid.NewString()
			stale.Status = models.ExportJobStatusRunning
			stale.UpdatedAt = now.Add(-2 * time.Hour)
			if err := db.CreateExportJob(ctx, &stale); err != nil {
				t.Fatal(err)
			}
			srv.reapInterruptedExportJobs(models.ExportJobOwner{})
			reaped, err := db.GetExportJob(ctx, stale.ID)
			if err != nil {
				t.Fatal(err)
			}
			if reaped.Status != models.ExportJobStatusFailed {
				t.Fatalf("stale job not reaped: %s", reaped.Status)
			}
			if reaped.UpdatedAt.Before(now.Add(-2*time.Second)) || reaped.UpdatedAt.After(time.Now().Add(2*time.Second)) {
				t.Errorf("reaped timestamp outside database clock window: %s", reaped.UpdatedAt)
			}
		})
	}
}

func TestTokenLastUsedCoalescesAcrossStores(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db, raw := backgroundTestStore(t, backend)
			ctx := context.Background()
			user := mkTestUser(t, db, uuid.NewString()+"@example.com", models.UserRoleMember)
			id, err := db.CreateAPIToken(ctx, &models.APIToken{UserID: user.ID, Name: "coalesced", TokenHash: uuid.NewString(), Prefix: "test", Scopes: []models.TokenScope{models.TokenScopeProfileRead}})
			if err != nil {
				t.Fatal(err)
			}
			setup := []string{`CREATE TABLE background_token_writes (id INTEGER)`, `CREATE TRIGGER background_count_token_writes AFTER UPDATE OF last_used_at ON api_tokens BEGIN INSERT INTO background_token_writes VALUES (NEW.id); END`}
			if backend == "postgres" {
				setup[1] = `CREATE FUNCTION background_count_token_writes() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN INSERT INTO background_token_writes VALUES (NEW.id); RETURN NEW; END'`
				setup = append(setup, `CREATE TRIGGER background_count_token_writes AFTER UPDATE OF last_used_at ON api_tokens FOR EACH ROW EXECUTE FUNCTION background_count_token_writes()`)
			}
			for _, statement := range setup {
				if _, err := raw.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				if backend == "postgres" {
					_, _ = raw.ExecContext(ctx, `DROP TRIGGER background_count_token_writes ON api_tokens; DROP FUNCTION background_count_token_writes(); DROP TABLE background_token_writes`)
				}
			})
			for range 2 {
				if err := db.UpdateAPITokenLastUsed(ctx, id); err != nil {
					t.Fatal(err)
				}
			}
			var writes int
			if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM background_token_writes`).Scan(&writes); err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Errorf("last-used writes within 60 seconds = %d, want 1", writes)
			}
			age := `UPDATE api_tokens SET last_used_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-61 seconds') WHERE id = $1`
			if backend == "postgres" {
				age = `UPDATE api_tokens SET last_used_at = now() - interval '61 seconds' WHERE id = $1`
			}
			if _, err := raw.ExecContext(ctx, age, id); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateAPITokenLastUsed(ctx, id); err != nil {
				t.Fatal(err)
			}
			if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM background_token_writes`).Scan(&writes); err != nil {
				t.Fatal(err)
			}
			if writes != 3 {
				t.Errorf("last-used writes after aging and refresh = %d, want 3", writes)
			}
		})
	}
}

func TestStartupExportReapLogs(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				instance string
				count    int
				age      time.Duration
				warn     bool
			}{
				{"empty", "replica-a", 0, 0, false},
				{"recent", "replica-a", 101, time.Second, true},
				{"older", "replica-a", 101, time.Minute, false},
				{"unconfigured", "", 1, 0, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.instance != "" {
						tc.instance += "-" + uuid.NewString()
					}
					db, _ := backgroundTestStore(t, backend)
					ctx := context.Background()
					user := mkTestUser(t, db, uuid.NewString()+"@example.com", models.UserRoleMember)
					_, source := mkTestTeam(t, db, uuid.NewString(), user)
					now := time.Now().UTC()
					for range tc.count {
						job := &models.ExportJob{ID: uuid.NewString(), Owner: models.ExportJobOwner{InstanceID: tc.instance, ProcessID: "previous"}, SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusRunning, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now.Add(-tc.age)}
						if err := db.CreateExportJob(ctx, job); err != nil {
							t.Fatal(err)
						}
					}
					output, err := os.Create(filepath.Join(t.TempDir(), "startup.log"))
					if err != nil {
						t.Fatal(err)
					}
					defer output.Close()
					cfg := testOAuthConfig()
					cfg.Export.InstanceID = tc.instance
					srv := New(ServerOptions{Config: cfg, SQLite: db, Logger: slog.New(slog.NewJSONHandler(output, nil)), FS: http.FS(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})})
					if err := srv.Shutdown(ctx); err != nil {
						t.Fatal(err)
					}
					if _, err := output.Seek(0, io.SeekStart); err != nil {
						t.Fatal(err)
					}
					decoder := json.NewDecoder(output)
					infos, warns := 0, 0
					for {
						var entry struct {
							Level    string `json:"level"`
							Message  string `json:"msg"`
							Instance string `json:"instance_id"`
							Reaped   int    `json:"reaped"`
						}
						if err := decoder.Decode(&entry); err == io.EOF {
							break
						} else if err != nil {
							t.Fatal(err)
						}
						switch entry.Message {
						case "reaped interrupted exports":
							infos++
							if entry.Level != "INFO" || entry.Instance != tc.instance || entry.Reaped != tc.count {
								t.Errorf("startup summary = %+v", entry)
							}
						case "reaped exports updated in the last 5s; check for a duplicate export instance_id or a crash restart":
							warns++
							if entry.Level != "WARN" || entry.Instance != tc.instance || entry.Reaped != tc.count {
								t.Errorf("recent update warning = %+v", entry)
							}
						}
					}
					wantInfos, wantWarns := 1, 0
					if tc.instance == "" {
						wantInfos = 0
					}
					if tc.warn {
						wantWarns = 1
					}
					if infos != wantInfos || warns != wantWarns {
						t.Errorf("summary lines = %d, warning lines = %d; want %d, %d", infos, warns, wantInfos, wantWarns)
					}
				})
			}
		})
	}
}

func TestExportJobsIgnoreExtraColumns(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db, raw := backgroundTestStore(t, backend)
			ctx := context.Background()
			if _, err := raw.ExecContext(ctx, "ALTER TABLE export_jobs ADD COLUMN test_future_column TEXT"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := raw.ExecContext(ctx, "ALTER TABLE export_jobs DROP COLUMN test_future_column"); err != nil {
					t.Error(err)
				}
			})
			user := mkTestUser(t, db, uuid.NewString()+"@example.com", models.UserRoleMember)
			_, source := mkTestTeam(t, db, uuid.NewString(), user)
			now := time.Now().UTC()
			owner := models.ExportJobOwner{InstanceID: uuid.NewString(), ProcessID: "previous"}
			job := &models.ExportJob{ID: uuid.NewString(), Owner: owner, SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusRunning, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
			if err := db.CreateExportJob(ctx, job); err != nil {
				t.Fatal(err)
			}
			got, err := db.GetExportJob(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Owner != owner || got.Status != job.Status {
				t.Errorf("get after adding column = %+v", got)
			}
			reaped, err := db.ReapInterruptedExportJobs(ctx, time.Hour, models.ExportJobOwner{InstanceID: owner.InstanceID, ProcessID: "current"})
			if err != nil {
				t.Fatal(err)
			}
			if len(reaped) != 1 || reaped[0].ID != job.ID {
				t.Fatalf("reaped after adding column = %+v", reaped)
			}
			if reaped[0].PreviousUpdatedAt.Sub(job.UpdatedAt).Abs() > time.Millisecond {
				t.Errorf("previous update timestamp = %s, want %s", reaped[0].PreviousUpdatedAt, job.UpdatedAt)
			}
			got, err = db.GetExportJob(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != models.ExportJobStatusFailed {
				t.Errorf("reaped job status = %s", got.Status)
			}
		})
	}
}

func TestStartupExportReapSummaryOnEarlyExit(t *testing.T) {
	for _, name := range []string{"later batch failure", "expired context"} {
		t.Run(name, func(t *testing.T) {
			db, raw := backgroundTestStore(t, "sqlite")
			ctx := context.Background()
			owner := models.ExportJobOwner{InstanceID: "replica-a", ProcessID: "current"}
			wantReaped := 0
			if name == "later batch failure" {
				user := mkTestUser(t, db, "reap-summary@example.com", models.UserRoleMember)
				_, source := mkTestTeam(t, db, "reap-summary", user)
				now := time.Now().UTC()
				for range 101 {
					job := &models.ExportJob{ID: uuid.NewString(), Owner: models.ExportJobOwner{InstanceID: owner.InstanceID, ProcessID: "previous"}, SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusRunning, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
					if err := db.CreateExportJob(ctx, job); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := raw.ExecContext(ctx, `CREATE TRIGGER fail_later_reap BEFORE UPDATE ON export_jobs WHEN (SELECT count(*) FROM export_jobs WHERE status = 'failed') >= 100 BEGIN SELECT RAISE(FAIL, 'later batch failed'); END`); err != nil {
					t.Fatal(err)
				}
				wantReaped = 100
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			var logs bytes.Buffer
			srv := &Server{store: db, baseCtx: ctx, config: testOAuthConfig(), log: slog.New(slog.NewTextHandler(&logs, nil))}
			srv.reapInterruptedExportJobs(owner)
			output := logs.String()
			if strings.Count(output, "msg=\"reaped interrupted exports\"") != 1 || !strings.Contains(output, fmt.Sprintf("reaped=%d", wantReaped)) {
				t.Errorf("missing startup summary for %d reaped jobs: %s", wantReaped, output)
			}
			if wantReaped > 0 {
				if !strings.Contains(output, "later batch failed") || !strings.Contains(output, "level=WARN") {
					t.Errorf("missing later-batch error or recent-update warning: %s", output)
				}
				var failed int
				if err := raw.QueryRow(`SELECT count(*) FROM export_jobs WHERE status = 'failed'`).Scan(&failed); err != nil {
					t.Fatal(err)
				}
				if failed != wantReaped {
					t.Errorf("failed jobs = %d, want %d", failed, wantReaped)
				}
			}
		})
	}
}
