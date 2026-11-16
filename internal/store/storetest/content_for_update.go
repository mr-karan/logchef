package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// forUpdateKind is one object whose update reads its before under the row
// lock: a saved query, an alert or a dashboard.
type forUpdateKind struct {
	name string
	// create stores one object and returns its ID.
	create func(t *testing.T, ctx context.Context, s store.Store) int
	// read is the GetXForUpdate of the kind, returning the object's name.
	read func(ctx context.Context, tx store.StoreOps, id int) (string, error)
	// rename writes a new name.
	rename func(ctx context.Context, tx store.StoreOps, id int, name string) error
}

var forUpdateKinds = []forUpdateKind{
	{
		name: "saved query",
		create: func(t *testing.T, ctx context.Context, s store.Store) int {
			owner := mkUser(t, ctx, s, fmt.Sprintf("sq-lock-%d@test.dev", time.Now().UnixNano()))
			src := mkSource(t, ctx, s, fmt.Sprintf("sq_lock_%d", time.Now().UnixNano()))
			q, err := s.CreateSavedQuery(ctx, src.ID, nil, "before", "", models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, `{"content":"level:error"}`, &owner.ID)
			if err != nil {
				t.Fatalf("CreateSavedQuery: %v", err)
			}
			return q.ID
		},
		read: func(ctx context.Context, tx store.StoreOps, id int) (string, error) {
			q, err := tx.GetSavedQueryForUpdate(ctx, id)
			if err != nil {
				return "", err
			}
			return q.Name, nil
		},
		rename: func(ctx context.Context, tx store.StoreOps, id int, name string) error {
			return tx.UpdateSavedQuery(ctx, id, name, "", models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, `{"content":"level:error"}`)
		},
	},
	{
		name: "alert",
		create: func(t *testing.T, ctx context.Context, s store.Store) int {
			src := mkSource(t, ctx, s, fmt.Sprintf("alert_lock_%d", time.Now().UnixNano()))
			a := &models.Alert{
				SourceID: src.ID, Name: "before", QueryLanguage: models.QueryLanguageClickHouseSQL,
				EditorMode: models.AlertEditorModeNative, Query: "SELECT count() FROM logs",
				LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
				FrequencySeconds: 60, Severity: models.AlertSeverityWarning, IsActive: true,
				LastState: models.AlertStateResolved,
			}
			if err := s.CreateAlert(ctx, a); err != nil {
				t.Fatalf("CreateAlert: %v", err)
			}
			return int(a.ID)
		},
		read: func(ctx context.Context, tx store.StoreOps, id int) (string, error) {
			a, err := tx.GetAlertForUpdate(ctx, models.AlertID(id))
			if err != nil {
				return "", err
			}
			return a.Name, nil
		},
		rename: func(ctx context.Context, tx store.StoreOps, id int, name string) error {
			a, err := tx.GetAlert(ctx, models.AlertID(id))
			if err != nil {
				return err
			}
			a.Name = name
			return tx.UpdateAlert(ctx, a)
		},
	},
	{
		name: "dashboard",
		create: func(t *testing.T, ctx context.Context, s store.Store) int {
			d := &models.Dashboard{Name: "before", PanelsJSON: json.RawMessage(`{"version":1,"layout":[],"panels":[]}`)}
			if err := s.CreateDashboard(ctx, d); err != nil {
				t.Fatalf("CreateDashboard: %v", err)
			}
			return d.ID
		},
		read: func(ctx context.Context, tx store.StoreOps, id int) (string, error) {
			d, err := tx.GetDashboardForUpdate(ctx, id)
			if err != nil {
				return "", err
			}
			return d.Name, nil
		},
		rename: func(ctx context.Context, tx store.StoreOps, id int, name string) error {
			d, err := tx.GetDashboard(ctx, id)
			if err != nil {
				return err
			}
			d.Name = name
			return tx.UpdateDashboard(ctx, d)
		},
	},
}

// testContentForUpdate checks that each GetXForUpdate returns the stored
// object, sees its own transaction's write, and reports a missing object.
func testContentForUpdate(t *testing.T, ctx context.Context, s store.Store) {
	for _, k := range forUpdateKinds {
		t.Run(k.name, func(t *testing.T) {
			err := s.WithTx(ctx, func(tx store.StoreOps) error {
				_, err := k.read(ctx, tx, 987654)
				return err
			})
			if !models.IsNotFound(err) {
				t.Errorf("read of a missing %s: %v, want not found", k.name, err)
			}
			id := k.create(t, ctx, s)
			err = s.WithTx(ctx, func(tx store.StoreOps) error {
				if name, err := k.read(ctx, tx, id); err != nil || name != "before" {
					return fmt.Errorf("first read = %q, %w", name, err)
				}
				if err := k.rename(ctx, tx, id, "after"); err != nil {
					return err
				}
				if name, err := k.read(ctx, tx, id); err != nil || name != "after" {
					return fmt.Errorf("read inside the writing transaction = %q, %w", name, err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// RunContentForUpdateSerialized checks that each GetXForUpdate holds a second
// reader until the first transaction ends, and that the second reader then
// sees the first one's write. SQLite serializes every write transaction, so
// only a store with row locks needs it.
func RunContentForUpdateSerialized(t *testing.T, s store.Store) {
	t.Helper()
	for _, k := range forUpdateKinds {
		t.Run(k.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			id := k.create(t, ctx, s)

			started := make(chan struct{})
			seen := make(chan string, 1)
			result := make(chan error, 1)
			err := s.WithTx(ctx, func(tx store.StoreOps) error {
				if _, err := k.read(ctx, tx, id); err != nil {
					return err
				}
				if err := k.rename(ctx, tx, id, "written-under-lock"); err != nil {
					return err
				}
				go func() {
					result <- s.WithTx(ctx, func(second store.StoreOps) error {
						close(started)
						name, err := k.read(ctx, second, id)
						if err == nil {
							seen <- name
						}
						return err
					})
				}()
				select {
				case <-started:
				case err := <-result:
					return fmt.Errorf("second transaction failed to start: %w", err)
				case <-ctx.Done():
					return ctx.Err()
				}
				select {
				case err := <-result:
					return errors.Join(errors.New("second read did not wait for the first transaction"), err)
				case <-time.After(100 * time.Millisecond):
					return nil
				}
			})
			if err != nil {
				t.Fatalf("first transaction: %v", err)
			}
			select {
			case err := <-result:
				if err != nil {
					t.Fatalf("second transaction: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("second transaction did not finish after the first ended")
			}
			if got := <-seen; got != "written-under-lock" {
				t.Errorf("second reader saw %q, want the first transaction's write", got)
			}
		})
	}
}
