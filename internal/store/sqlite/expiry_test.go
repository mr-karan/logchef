package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestPruneExpiredQuerySharesTimeZones(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()
	user := makeUser("shares@example.com")
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	sourceID := createTestSource(t, ctx, db)
	now, cases := expiryTimeZoneCases()
	cases = append(cases, expiryTimeZoneCase{"exact expiry", now, now, true})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			share := &models.QueryShare{
				Token: tc.name, SourceID: sourceID,
				CreatedBy: user.ID, Payload: []byte("{}"), ExpiresAt: tc.expiresAt,
			}
			if err := db.CreateQueryShare(ctx, share); err != nil {
				t.Fatal(err)
			}
			if err := db.PruneExpiredQueryShares(ctx, tc.before); err != nil {
				t.Fatal(err)
			}
			_, err := db.GetQueryShare(ctx, share.Token)
			if tc.wantAlive {
				if err != nil {
					t.Fatalf("live query share deleted: %v", err)
				}
			} else if !errors.Is(err, models.ErrNotFound) {
				t.Fatalf("expired query share survived: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestExpiredExportJobsTimeZones(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()
	user := makeUser("exports@example.com")
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	sourceID := createTestSource(t, ctx, db)
	now, cases := expiryTimeZoneCases()
	cases = append(cases, expiryTimeZoneCase{"exact expiry", now, now, true})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := &models.ExportJob{
				ID: tc.name, SourceID: sourceID, CreatedBy: user.ID,
				Status: models.ExportJobStatusPending, Format: "csv", RequestPayload: []byte("{}"),
				ExpiresAt: tc.expiresAt, CreatedAt: tc.before, UpdatedAt: tc.before,
			}
			if err := db.CreateExportJob(ctx, job); err != nil {
				t.Fatal(err)
			}
			if err := db.CompleteExportJob(ctx, job.ID, "export.csv", "export.csv", 1, 10, tc.before); err != nil {
				t.Fatal(err)
			}
			paths, err := db.ListExpiredExportJobPaths(ctx, tc.before)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantAlive {
				if len(paths) != 0 {
					t.Errorf("live export paths listed as expired: %v", paths)
				}
			} else if len(paths) != 1 || paths[0] != "export.csv" {
				t.Errorf("expired export paths = %v, want [export.csv]", paths)
			}
			if err := db.DeleteExpiredExportJobs(ctx, tc.before); err != nil {
				t.Fatal(err)
			}
			_, err = db.GetExportJob(ctx, job.ID)
			if tc.wantAlive {
				if err != nil {
					t.Fatalf("live export deleted: %v", err)
				}
			} else if !errors.Is(err, models.ErrNotFound) {
				t.Fatalf("expired export survived: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestLegacyArtifactExpiryReads(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()
	user := makeUser("legacy-artifacts@example.com")
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	sourceID := createTestSource(t, ctx, db)
	now, cases := expiryTimeZoneCases()
	for _, tc := range cases[:2] {
		t.Run(tc.name, func(t *testing.T) {
			legacy := tc.expiresAt.String() + " m=+7200.000000001"
			if _, err := db.writeDB.ExecContext(ctx, `INSERT INTO query_shares
				(token, source_id, created_by, payload_json, expires_at)
				VALUES (?, ?, ?, '{}', ?)`, tc.name, sourceID, user.ID, legacy); err != nil {
				t.Fatal(err)
			}
			share, err := db.GetQueryShare(ctx, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.writeDB.ExecContext(ctx, `INSERT INTO export_jobs
				(id, source_id, created_by, status, format, request_json, expires_at)
				VALUES (?, ?, ?, 'complete', 'csv', '{}', ?)`, tc.name, sourceID, user.ID, legacy); err != nil {
				t.Fatal(err)
			}
			job, err := db.GetExportJob(ctx, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			for _, expiry := range []time.Time{share.ExpiresAt, job.ExpiresAt} {
				if !expiry.Equal(tc.expiresAt) || expiry.Location() != time.UTC {
					t.Errorf("legacy expiry = %v, want %v in UTC", expiry, tc.expiresAt)
				}
				if expired := now.After(expiry); expired == tc.wantAlive {
					t.Errorf("Go expiry check = %v, want alive = %v", expired, tc.wantAlive)
				}
			}
		})
	}
}
