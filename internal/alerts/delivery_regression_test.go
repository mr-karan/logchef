package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
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

func TestDeliveryRecovery(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := func(t *testing.T) (store.Store, *models.Alert, *slog.Logger) {
				t.Helper()
				if backend == "sqlite" {
					return newManagerTestAlert(t)
				}
				dsn := os.Getenv("LOGCHEF_TEST_ALERT_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("LOGCHEF_TEST_ALERT_POSTGRES_DSN not set; run separately from store conformance")
				}
				conn, err := pgx.Connect(t.Context(), dsn)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = conn.Close(t.Context()) }()
				if _, err := conn.Exec(t.Context(), "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
					t.Fatal(err)
				}
				logger := slog.New(slog.NewTextHandler(io.Discard, nil))
				db, err := postgres.New(t.Context(), postgres.Options{Logger: logger, Config: config.PostgresConfig{DSN: dsn}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				return db, createManagerTestAlert(t, db), logger
			}
			t.Run("interrupted final attempt", func(t *testing.T) {
				db, alert, logger := open(t)
				entry, err := db.InsertAlertHistory(t.Context(), alert.ID, models.AlertStatusTriggered, nil, "fired", map[string]any{
					"delivery_attempts": 3, "delivery_failed": false, "delivery_in_progress": true,
					"delivery_retries_exhausted": true,
				})
				if err != nil {
					t.Fatal(err)
				}
				sends := 0
				sender := alertSenderFunc(func(context.Context, AlertNotification) error { sends++; return nil })
				for range 2 {
					m := NewManager(Options{DB: db, Logger: logger, Sender: sender})
					if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
						t.Fatal(err)
					}
				}
				after, err := db.GetAlertHistoryEntry(t.Context(), alert.ID, entry.ID)
				if err != nil {
					t.Fatal(err)
				}
				if after.Payload["delivery_failed"] != true || after.Payload["delivery_in_progress"] != false || after.Payload["delivery_retries_exhausted"] != true || after.Payload["delivery_error"] != "delivery interrupted" {
					t.Errorf("recovered payload = %v, want failed, exhausted, and no longer in progress", after.Payload)
				}
				if sends != 0 {
					t.Errorf("sends = %d, want 0 after final reservation", sends)
				}
			})
			t.Run("legacy pending fixture", func(t *testing.T) {
				db, alert, logger := open(t)
				var succeeded, pending atomic.Int32
				first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { succeeded.Add(1); w.WriteHeader(http.StatusNoContent) }))
				defer first.Close()
				second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if pending.Add(1) == 1 {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				defer second.Close()
				alert.WebhookURLs = []string{first.URL, second.URL}
				raw, err := os.ReadFile("testdata/legacy_delivery_pending.json")
				if err != nil {
					t.Fatal(err)
				}
				var payload map[string]any
				if err := json.Unmarshal(raw, &payload); err != nil {
					t.Fatal(err)
				}
				payload["delivery_pending"] = []string{string(deliveryTargetID("webhook", second.URL))}
				if _, err := db.InsertAlertHistory(t.Context(), alert.ID, models.AlertStatusTriggered, nil, "fired", payload); err != nil {
					t.Fatal(err)
				}
				m := NewManager(Options{DB: db, Logger: logger, Sender: NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})})
				if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
					t.Fatal(err)
				}
				if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
					t.Fatal(err)
				}
				if succeeded.Load() != 0 || pending.Load() != 2 {
					t.Errorf("requests = succeeded %d, pending %d; want 0, 2", succeeded.Load(), pending.Load())
				}
			})
			t.Run("manual resolution during delivery", func(t *testing.T) {
				db, alert, _ := open(t)
				var logs bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
				m := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
				m.sender = alertSenderFunc(func(ctx context.Context, n AlertNotification) error {
					if n.Status == models.AlertStatusTriggered {
						if err := m.ManualResolve(ctx, alert.ID, "manual resolution"); err != nil {
							t.Fatal(err)
						}
					}
					return nil
				})
				if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
					t.Fatal(err)
				}
				for _, record := range decodeLogs(t, &logs, "failed to update alert history payload after delivery") {
					if record.Level == "ERROR" {
						t.Errorf("resolved delivery logged an error: %+v", record)
					}
				}
			})
		})
	}
}
