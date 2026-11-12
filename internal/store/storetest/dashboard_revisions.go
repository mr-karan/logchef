package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testDashboardRevisions proves the save compare-and-swap: of the saves that
// start from one revision, exactly one wins, however close together they land.
func testDashboardRevisions(t *testing.T, ctx context.Context, s store.Store) {
	owner := mkUser(t, ctx, s, "dash-revisions@test.dev")
	d := &models.Dashboard{Name: "Rev", PanelsJSON: revisionBlob("base"), CreatedBy: &owner.ID}
	if err := s.CreateDashboard(ctx, d); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}
	if d.Revision != 1 {
		t.Fatalf("new dashboard revision = %d, want 1", d.Revision)
	}

	t.Run("TwoStaleBodiesOneAfterAnother", func(t *testing.T) {
		first := loadDashboard(t, ctx, s, d.ID)
		second := loadDashboard(t, ctx, s, d.ID)

		first.PanelsJSON = revisionBlob("first")
		if err := s.UpdateDashboard(ctx, first); err != nil {
			t.Fatalf("first save: %v", err)
		}
		if first.Revision != 2 {
			t.Fatalf("first save revision = %d, want 2", first.Revision)
		}
		second.PanelsJSON = revisionBlob("second")
		err := s.UpdateDashboard(ctx, second)
		if !errors.Is(err, models.ErrConflict) {
			t.Fatalf("second save from the same revision: err = %v, want ErrConflict", err)
		}

		stored := loadDashboard(t, ctx, s, d.ID)
		if stored.Revision != 2 || string(stored.PanelsJSON) != string(revisionBlob("first")) {
			t.Fatalf("stored row after a refused save = revision %d, panels %s; want revision 2 and the first body", stored.Revision, stored.PanelsJSON)
		}
	})

	t.Run("TwoSavesWithinOneSecond", func(t *testing.T) {
		for range 3 {
			first := loadDashboard(t, ctx, s, d.ID)
			first.Name = "first fast save"
			if err := s.UpdateDashboard(ctx, first); err != nil {
				t.Fatal(err)
			}
			before := loadDashboard(t, ctx, s, d.ID)
			second := *before
			second.Name = "second fast save"
			if err := s.UpdateDashboard(ctx, &second); err != nil {
				t.Fatal(err)
			}
			after := loadDashboard(t, ctx, s, d.ID)
			if after.Revision != before.Revision+1 {
				t.Fatalf("revision did not advance: %+v", after)
			}
			if before.UpdatedAt.Truncate(time.Second).Equal(after.UpdatedAt.Truncate(time.Second)) {
				return
			}
		}
		t.Fatal("no pair of saves finished within one timestamp second")
	})

	for _, transactional := range []bool{false, true} {
		t.Run(fmt.Sprintf("TwentyConcurrentSaves/transaction=%t", transactional), func(t *testing.T) {
			testDashboardConcurrentSaves(t, ctx, s, d.ID, transactional)
		})
	}
}

func revisionBlob(body string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"version":1,"layout":[],"panels":[],"body":%q}`, body))
}

func loadDashboard(t *testing.T, ctx context.Context, s store.Store, id int) *models.Dashboard {
	t.Helper()
	d, err := s.GetDashboard(ctx, id)
	if err != nil {
		t.Fatalf("GetDashboard(%d): %v", id, err)
	}
	return d
}

func testDashboardConcurrentSaves(t *testing.T, ctx context.Context, s store.Store, id int, transactional bool) {
	base := loadDashboard(t, ctx, s, id)
	const savers = 20
	errs := make([]error, savers)
	var wg sync.WaitGroup
	for i := range savers {
		wg.Go(func() {
			mine := *base
			mine.PanelsJSON = revisionBlob(fmt.Sprintf("saver-%d", i))
			if transactional {
				errs[i] = s.WithTx(ctx, func(tx store.StoreOps) error {
					if _, err := tx.GetDashboard(ctx, mine.ID); err != nil {
						return err
					}
					return tx.UpdateDashboard(ctx, &mine)
				})
			} else {
				errs[i] = s.UpdateDashboard(ctx, &mine)
			}
		})
	}
	wg.Wait()

	winners := 0
	var winnerBody json.RawMessage
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
			winnerBody = revisionBlob(fmt.Sprintf("saver-%d", i))
		case errors.Is(err, models.ErrConflict):
		default:
			t.Fatalf("saver %d: err = %v, want nil or ErrConflict", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d saves from revision %d succeeded, want exactly 1", winners, savers, base.Revision)
	}
	stored := loadDashboard(t, ctx, s, id)
	if stored.Revision != base.Revision+1 || string(stored.PanelsJSON) != string(winnerBody) {
		t.Fatalf("stored row = revision %d, panels %s; want revision %d and the winner's body", stored.Revision, stored.PanelsJSON, base.Revision+1)
	}
}
