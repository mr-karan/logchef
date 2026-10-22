package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func testExportJobOwnership(t *testing.T, ctx context.Context, s store.Store) {
	user := mkUser(t, ctx, s, "export-owner@test.dev")
	source := mkSource(t, ctx, s, "export_owner")
	now := time.Now().UTC()
	current := models.ExportJobOwner{InstanceID: "replica-a", ProcessID: "process-new"}
	cases := []struct {
		id      string
		owner   models.ExportJobOwner
		status  models.ExportJobStatus
		updated time.Time
		want    models.ExportJobStatus
	}{
		{"own-pending", models.ExportJobOwner{InstanceID: "replica-a", ProcessID: "process-old"}, models.ExportJobStatusPending, now, models.ExportJobStatusFailed},
		{"own-running", models.ExportJobOwner{InstanceID: "replica-a", ProcessID: "process-old"}, models.ExportJobStatusRunning, now, models.ExportJobStatusFailed},
		{"other-live", models.ExportJobOwner{InstanceID: "replica-b", ProcessID: "process-other"}, models.ExportJobStatusRunning, now, models.ExportJobStatusRunning},
		{"current", current, models.ExportJobStatusRunning, now, models.ExportJobStatusRunning},
		{"own-complete", models.ExportJobOwner{InstanceID: "replica-a", ProcessID: "process-old"}, models.ExportJobStatusComplete, now, models.ExportJobStatusComplete},
		{"legacy-fresh", models.ExportJobOwner{}, models.ExportJobStatusRunning, now, models.ExportJobStatusRunning},
		{"legacy-stale", models.ExportJobOwner{}, models.ExportJobStatusRunning, now.Add(-2 * time.Hour), models.ExportJobStatusFailed},
		{"abandoned-stale", models.ExportJobOwner{InstanceID: "gone", ProcessID: "gone"}, models.ExportJobStatusPending, now.Add(-2 * time.Hour), models.ExportJobStatusFailed},
	}
	for _, tc := range cases {
		job := &models.ExportJob{ID: tc.id, Owner: tc.owner, SourceID: source.ID, CreatedBy: user.ID, Status: tc.status, Format: "csv", RequestPayload: []byte(`{}`), CreatedAt: now, UpdatedAt: tc.updated, ExpiresAt: now.Add(time.Hour)}
		if err := s.CreateExportJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReapInterruptedExportJobs(ctx, time.Hour, models.ExportJobOwner{}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		if tc.updated.Equal(now) {
			got, err := s.GetExportJob(ctx, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.status {
				t.Errorf("periodic reaper changed fresh %s job to %s", tc.id, got.Status)
			}
		}
	}
	for range 2 {
		if _, err := s.ReapInterruptedExportJobs(ctx, time.Hour, current); err != nil {
			t.Fatal(err)
		}
		for _, tc := range cases {
			job, err := s.GetExportJob(ctx, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if job.Status != tc.want {
				t.Errorf("%s status = %s, want %s", tc.id, job.Status, tc.want)
			}
			if job.Owner != tc.owner {
				t.Errorf("%s owner = %+v, want %+v", tc.id, job.Owner, tc.owner)
			}
			if tc.want == models.ExportJobStatusFailed && job.ErrorMessage != "interrupted by restart" {
				t.Errorf("%s error = %q", tc.id, job.ErrorMessage)
			}
		}
	}
}
