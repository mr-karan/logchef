package alerts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestManagerChangedWebhookTargets(t *testing.T) {
	for _, change := range []string{"fixed", "added", "removed"} {
		t.Run(change, func(t *testing.T) {
			db, alert, logger := newManagerTestAlert(t)
			var original, added atomic.Int32
			broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				original.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer broken.Close()
			fixed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { added.Add(1); w.WriteHeader(http.StatusNoContent) }))
			defer fixed.Close()
			alert.WebhookURLs = []string{broken.URL}
			sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
			m := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
			if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "fixed":
				alert.WebhookURLs = []string{fixed.URL}
			case "added":
				alert.WebhookURLs = append(alert.WebhookURLs, fixed.URL)
			case "removed":
				alert.WebhookURLs = nil
			}
			// A fresh manager must recover the selection from the persisted history.
			m = NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
			if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
				t.Fatal(err)
			}
			entry, err := db.GetLatestUnresolvedAlertHistory(t.Context(), alert.ID)
			if err != nil {
				t.Fatal(err)
			}
			if change == "removed" {
				if entry.Payload["delivery_failed"] != true || entry.Payload["delivery_error"] != "no pending notification targets remain" {
					t.Fatalf("removed-target outcome = %v, want failed with no pending targets", entry.Payload)
				}
			} else if added.Load() != 1 {
				t.Errorf("new target requests = %d, want 1", added.Load())
			}
			if change == "fixed" && entry.Payload["delivery_failed"] != false {
				t.Errorf("fixed target outcome = %v, want success", entry.Payload)
			}
			wantOriginal := int32(1)
			if change == "added" {
				wantOriginal = 2
			}
			if original.Load() != wantOriginal {
				t.Errorf("original requests = %d, want %d", original.Load(), wantOriginal)
			}
			if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
				t.Fatal(err)
			}
			if change != "removed" && added.Load() != 1 {
				t.Errorf("successful new target requests after another evaluation = %d, want 1", added.Load())
			}

		})
	}
}

type historyReadBarrier struct {
	store.Store
	afterRead func()
}

func (s *historyReadBarrier) GetLatestUnresolvedAlertHistory(ctx context.Context, id models.AlertID) (*models.AlertHistoryEntry, error) {
	entry, err := s.Store.GetLatestUnresolvedAlertHistory(ctx, id)
	if s.afterRead != nil {
		f := s.afterRead
		s.afterRead = nil
		f()
	}
	return entry, err
}

func TestManualResolveWinsEvaluationRace(t *testing.T) {
	for _, evaluation := range []string{"retry", "suppressed", "resolve", "in flight"} {
		t.Run(evaluation, func(t *testing.T) {
			db, alert, logger := newManagerTestAlert(t)
			var triggered, resolved int
			sender := alertSenderFunc(func(_ context.Context, n AlertNotification) error {
				if n.Status == models.AlertStatusResolved {
					resolved++
					return nil
				}
				triggered++
				if evaluation == "suppressed" {
					return nil
				}
				return errors.New("delivery unavailable")
			})
			m := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
			if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
				t.Fatal(err)
			}
			var resolvedPayload map[string]any
			manual := func() {
				if err := m.ManualResolve(t.Context(), alert.ID, "manual resolution"); err != nil {
					t.Fatal(err)
				}
				rows, err := db.ListAlertHistory(t.Context(), alert.ID, 1)
				if err != nil || len(rows) != 1 {
					t.Fatalf("resolved history = %v, %v", rows, err)
				}
				resolvedPayload = rows[0].Payload
			}
			before, err := db.GetLatestUnresolvedAlertHistory(t.Context(), alert.ID)
			if err != nil {
				t.Fatal(err)
			}
			if evaluation == "in flight" {
				m.sender = alertSenderFunc(func(_ context.Context, n AlertNotification) error {
					if n.Status == models.AlertStatusResolved {
						resolved++
						return nil
					}
					manual()
					return nil
				})
			} else {
				m.db = &historyReadBarrier{Store: db, afterRead: manual}
			}
			if evaluation == "resolve" {
				err = m.handleResolved(t.Context(), alert, 0)
			} else {
				err = m.handleTriggered(t.Context(), alert, 12)
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := db.GetAlert(t.Context(), alert.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.LastState != models.AlertStateResolved {
				t.Errorf("last_state = %s, want resolved", after.LastState)
			}
			if triggered != 1 || resolved != 1 {
				t.Errorf("notifications = triggered %d, resolved %d; want 1 each", triggered, resolved)
			}
			history, err := db.GetAlertHistoryEntry(t.Context(), alert.ID, before.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(history.Payload, resolvedPayload) {
				t.Errorf("resolved in-flight payload = %v, want unchanged resolved payload", history.Payload)
			}
			if history.Message != "manual resolution" {
				t.Errorf("history message = %q, want manual resolution", history.Message)
			}
		})
	}
}

func TestManagerDeliveryInProgress(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	attempts := 0
	m := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
	m.sender = alertSenderFunc(func(ctx context.Context, _ AlertNotification) error {
		attempts++
		entry, err := db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Payload["delivery_failed"] == true {
			t.Errorf("in-flight attempt %d is marked failed", attempts)
		}
		return errors.New("unavailable")
	})
	for range 2 {
		if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
			t.Fatal(err)
		}
	}
}
