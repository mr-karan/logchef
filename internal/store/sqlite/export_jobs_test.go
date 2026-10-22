package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestExportJobTimestamps(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()
	user := makeUser("export-times@example.com")
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "export-times", Connection: models.ConnectionInfo{Host: "localhost:9000", Database: "logs", TableName: "export_times"}}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, offset := range []int{0, 19800, -25200} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			old := now.Add(-2 * time.Hour).In(time.FixedZone("test", offset))
			job := &models.ExportJob{ID: fmt.Sprint(offset), SourceID: source.ID, CreatedBy: user.ID, Status: models.ExportJobStatusPending, Format: "csv", RequestPayload: []byte(`{}`), ExpiresAt: now.Add(time.Hour), CreatedAt: old, UpdatedAt: old}
			if err := db.CreateExportJob(ctx, job); err != nil {
				t.Fatal(err)
			}
			check := func(columns ...string) {
				t.Helper()
				for _, column := range columns {
					var value string
					if err := db.readDB.QueryRowContext(ctx, "SELECT CAST("+column+" AS TEXT) FROM export_jobs WHERE id = ?", job.ID).Scan(&value); err != nil {
						t.Fatal(err)
					}
					if _, err := time.Parse("2006-01-02T15:04:05.000Z", value); err != nil {
						t.Errorf("noncanonical %s timestamp %q", column, value)
					}
				}
			}
			check("created_at", "updated_at", "expires_at")
			got, err := db.GetExportJob(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !got.CreatedAt.Equal(old) {
				t.Errorf("created_at = %s, want %s", got.CreatedAt, old)
			}
			reaped, err := db.ReapInterruptedExportJobs(ctx, time.Hour, models.ExportJobOwner{})
			if err != nil {
				t.Fatal(err)
			}
			if len(reaped) != 1 || reaped[0].ID != job.ID {
				t.Fatalf("reaped = %+v, want stale job %s", reaped, job.ID)
			}
			check("updated_at")
			job.ID += "-complete"
			if err := db.CreateExportJob(ctx, job); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateExportJobRunning(ctx, job.ID); err != nil {
				t.Fatal(err)
			}
			check("updated_at")
			if err := db.CompleteExportJob(ctx, job.ID, "test.csv", "test.csv", 1, 1, now.In(time.FixedZone("test", offset))); err != nil {
				t.Fatal(err)
			}
			check("completed_at", "updated_at")
			paths, err := db.ListExpiredExportJobPaths(ctx, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(paths) != 0 {
				t.Errorf("listed unexpired artifacts: %v", paths)
			}
			paths, err = db.ListExpiredExportJobPaths(ctx, now.Add(2*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if len(paths) != 1 || paths[0] != "test.csv" {
				t.Errorf("expired artifacts = %v, want test.csv", paths)
			}
			if err := db.FailExportJob(ctx, job.ID, "test failure", now); err != nil {
				t.Fatal(err)
			}
			check("updated_at")
			if err := db.DeleteExpiredExportJobs(ctx, now); err != nil {
				t.Fatal(err)
			}
			if _, err := db.GetExportJob(ctx, job.ID); err != nil {
				t.Fatalf("unexpired job deleted: %v", err)
			}
			if err := db.DeleteExpiredExportJobs(ctx, now.Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExportJobTimestampUpgrade(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "export-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := newMigrator(t, db)
	if err := m.Migrate(37); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO users (id, email, full_name, role, status) VALUES (1, 'export-upgrade@example.com', 'Test', 'member', 'active')`)
	mustExec(t, db, `INSERT INTO sources (id, name, _meta_is_auto_created, connection_config, identity_key) VALUES (1, 'export-upgrade', 0, '{}', 'export-upgrade')`)
	formats := []string{
		"2026-01-02 03:04:05.123456789 +0000 UTC",
		"2026-01-02 08:34:05.123456789 +0530 IST",
		"2026-01-01 20:04:05.123456789 -0700 MST",
		"2026-01-02T04:04:05.123+01:00",
		"2026-01-02 03:04:05.123",
		"2026-01-02T03:04:05.123Z",
	}
	for i, value := range formats {
		if _, err := db.Exec(`INSERT INTO export_jobs (id, source_id, created_by, status, format, request_json, expires_at, completed_at, created_at, updated_at) VALUES (?, 1, 1, 'running', 'csv', '{}', ?, ?, ?, ?)`, fmt.Sprint(i), value, value, value, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"expires_at", "completed_at", "created_at", "updated_at"} {
		for _, value := range []string{"", "garbage"} {
			id := "invalid-" + column + "-" + value
			if _, err := db.Exec(`INSERT INTO export_jobs (id, source_id, created_by, status, format, request_json, expires_at, completed_at, created_at, updated_at) VALUES (?, 1, 1, 'running', 'csv', '{}', ?, ?, ?, ?)`, id, formats[0], formats[0], formats[0], formats[0]); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE export_jobs SET "+column+" = ? WHERE id = ?", value, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	mustExec(t, db, `INSERT INTO export_jobs (id, source_id, created_by, status, format, request_json, expires_at, created_at, updated_at) VALUES ('nullable', 1, 1, 'running', 'csv', '{}', '2026-01-02 03:04:05', '2026-01-02 03:04:05', '2026-01-02 03:04:05')`)
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	for i := range formats {
		var expires, completed, created, updated, instance, process string
		if err := db.QueryRow(`SELECT CAST(expires_at AS TEXT), CAST(completed_at AS TEXT), CAST(created_at AS TEXT), CAST(updated_at AS TEXT), owner_instance_id, owner_process_id FROM export_jobs WHERE id = ?`, fmt.Sprint(i)).Scan(&expires, &completed, &created, &updated, &instance, &process); err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{expires, completed, created, updated} {
			if value != "2026-01-02T03:04:05.123Z" {
				t.Errorf("format %q migrated to %q", formats[i], value)
			}
		}
		if instance != "" || process != "" {
			t.Errorf("legacy owner = %q/%q", instance, process)
		}
	}
	var invalidCount int
	if err := db.QueryRow(`SELECT count(*) FROM export_jobs WHERE id LIKE 'invalid-%'`).Scan(&invalidCount); err != nil {
		t.Fatal(err)
	}
	if invalidCount != 0 {
		t.Errorf("invalid timestamp rows retained = %d, want 0", invalidCount)
	}
	var completed sql.NullString
	if err := db.QueryRow(`SELECT completed_at FROM export_jobs WHERE id = 'nullable'`).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed.Valid {
		t.Error("null completion changed")
	}
}
