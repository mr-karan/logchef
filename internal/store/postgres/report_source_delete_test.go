package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/pkg/models"
)

// A report whose insert is still open when its dashboard's delete starts is
// counted in the delete's reports_removed: the delete waits for the insert
// to commit instead of counting without it and then cascading it.
func TestDashboardDeleteCountsAReportInsertedConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dsn := postgrestest.DSN(t)
	s := opener(dsn)(t)
	monitor := connect(t, ctx, dsn)
	owner := createUser(t, ctx, s, "raced-report@test.dev", models.UserRoleMember)
	dash := &models.Dashboard{Name: "Raced", PanelsJSON: json.RawMessage(`{"version":1,"layout":[],"panels":[]}`), CreatedBy: &owner.ID}
	if err := s.CreateDashboard(ctx, dash); err != nil {
		t.Fatal(err)
	}

	type deleted struct {
		reports int
		err     error
	}
	result := make(chan deleted, 1)
	done := make(chan struct{})
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		r := &models.Report{Name: "raced", OwnerID: &owner.ID, SourceKind: models.ReportSourceDashboard, DashboardID: &dash.ID, Schedule: models.ReportSchedule{Frequency: models.ReportHourly}, Timezone: "UTC", RangeAnchor: models.ReportAnchorSendTime, MaxRows: 25}
		if err := tx.CreateReport(ctx, r); err != nil {
			return err
		}
		go func() {
			_, n, err := core.DeleteDashboard(ctx, s, discardLogger(), dash.ID)
			result <- deleted{n, err}
			close(done)
		}()
		awaitLockWaitOrDone(t, ctx, monitor, done)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil || got.reports != 1 {
		t.Errorf("DeleteDashboard = %d reports, %v, want the concurrently inserted one", got.reports, got.err)
	}
}
