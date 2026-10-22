package server

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
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
	srv := &Server{sqlite: db, baseCtx: ctx, config: testOAuthConfig(), log: slog.New(slog.NewTextHandler(&logs, nil))}
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
	srv.reapInterruptedExportJobs()
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
			srv := &Server{sqlite: db, baseCtx: ctx, config: testOAuthConfig(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			srv.reapInterruptedExportJobs()
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
			srv.reapInterruptedExportJobs()
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
			srv.reapInterruptedExportJobs()
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
