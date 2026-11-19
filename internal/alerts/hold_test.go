package alerts

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// drainWithHold sends every due row once, as drainQueue does, with the
// manager's silence check at send time.
func drainWithHold(t *testing.T, db store.Store, m *Manager) {
	t.Helper()
	allowlist, err := config.ParseWebhookAllowlist([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	drainer := notify.NewDrainer(notify.DrainerOptions{
		Store:   db,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Senders: map[models.DestinationType]notify.Sender{models.DestinationTypeWebhook: notify.NewWebhookSender(notify.WebhookSenderOptions{AllowedHosts: allowlist})},
		Hold:    m.HoldDelivery,
	})
	if err := drainer.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// webhookReceiver records the version 1 status of each request and answers
// the first one with answerFirst.
type webhookReceiver struct {
	mu       sync.Mutex
	statuses []string
	times    []time.Time
}

func newWebhookReceiver(t *testing.T, answerFirst func(w http.ResponseWriter)) (r *webhookReceiver, url string) {
	r = &webhookReceiver{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		r.mu.Lock()
		first := len(r.statuses) == 0
		r.statuses = append(r.statuses, body.Status)
		r.times = append(r.times, time.Now())
		r.mu.Unlock()
		if first && answerFirst != nil {
			answerFirst(w)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return r, server.URL
}

func (r *webhookReceiver) got() ([]string, []time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.statuses), slices.Clone(r.times)
}

// silenceSource stores a once silence of every rule on the alert's source,
// active from a minute ago until end.
func silenceSource(t *testing.T, db store.Store, alert *models.Alert, end time.Time) {
	t.Helper()
	start := time.Now().Add(-time.Minute)
	s := &models.AlertSilence{SourceID: alert.SourceID, Comment: "maintenance", ScheduleKind: models.SilenceOnce, StartsAt: &start, EndsAt: &end}
	if err := db.CreateAlertSilence(t.Context(), s); err != nil {
		t.Fatal(err)
	}
}

// TestSilenceHoldsQueuedRetry: a firing queued before a silence, retrying
// when the silence starts, does not go out during the silence, and neither
// does the resolved queued behind it. After the silence the retry goes out
// with the attempts it had left, then the resolved.
func TestSilenceHoldsQueuedRetry(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	receiver, url := newWebhookReceiver(t, func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	linkWebhooks(t, db, alert, url)
	m := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})

	if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
		t.Fatal(err)
	}
	drainWithHold(t, db, m)
	if got, _ := receiver.got(); !slices.Equal(got, []string{"triggered"}) {
		t.Fatalf("first drain sent %v, want the firing once, answered 429", got)
	}
	if err := m.handleResolved(t.Context(), alert, 1); err != nil {
		t.Fatal(err)
	}
	silenceEnd := time.Now().Add(1500 * time.Millisecond)
	silenceSource(t, db, alert, silenceEnd)

	time.Sleep(1100 * time.Millisecond) // the Retry-After of the first attempt
	drainWithHold(t, db, m)
	if got, _ := receiver.got(); len(got) != 1 {
		t.Fatalf("a drain during the silence sent %v, want nothing new", got[1:])
	}
	history, err := db.ListAlertHistory(t.Context(), alert.ID, 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	ref := DeliveryRef(alert.ID, history[0].ID)
	summaries := func() []models.DeliverySummary {
		t.Helper()
		rows, err := db.ListDeliverySummaries(t.Context(), DeliveryProducer, []string{ref})
		if err != nil {
			t.Fatal(err)
		}
		return rows[ref]
	}
	if rows := summaries(); len(rows) != 2 || rows[0].Status != models.DeliveryPending || rows[0].Attempts != 1 || rows[1].Attempts != 0 {
		t.Fatalf("held deliveries = %+v, want the firing pending with its one attempt and the resolved untried", rows)
	}

	deadline := time.Now().Add(5 * time.Second)
	for got, _ := receiver.got(); len(got) < 3 && time.Now().Before(deadline); got, _ = receiver.got() {
		time.Sleep(100 * time.Millisecond)
		drainWithHold(t, db, m)
	}
	got, times := receiver.got()
	if want := []string{"triggered", "triggered", "resolved"}; !slices.Equal(got, want) {
		t.Fatalf("receiver got %v, want %v", got, want)
	}
	if times[1].Before(silenceEnd) {
		t.Errorf("the held firing went out %s before the silence ended", silenceEnd.Sub(times[1]))
	}
	if rows := summaries(); rows[0].Status != models.DeliverySent || rows[0].Attempts != 2 || rows[1].Status != models.DeliverySent {
		t.Errorf("deliveries = %+v, want the firing sent at its second attempt, then the resolved", rows)
	}
}

// TestSilentResolveCancelsQueuedFiring: a firing still queued when a silence
// starts, whose group resolves inside the silence, never goes out: the
// resolve is muted, so the firing would open an incident that nothing
// closes.
func TestSilentResolveCancelsQueuedFiring(t *testing.T) {
	for _, notifyOnResolve := range []bool{true, false} {
		db, alert, logger := newManagerTestAlert(t)
		alert.NotifyOnResolve = notifyOnResolve
		if err := db.UpdateAlert(t.Context(), alert); err != nil {
			t.Fatal(err)
		}
		receiver, url := newWebhookReceiver(t, nil)
		linkWebhooks(t, db, alert, url)
		m := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})

		if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
			t.Fatal(err)
		}
		silenceSource(t, db, alert, time.Now().Add(time.Hour))
		if err := m.handleResolved(t.Context(), alert, 1); err != nil {
			t.Fatal(err)
		}
		drainWithHold(t, db, m)
		if got, _ := receiver.got(); len(got) != 0 {
			t.Fatalf("notify_on_resolve %v: receiver got %v, want nothing", notifyOnResolve, got)
		}
		history, err := db.ListAlertHistory(t.Context(), alert.ID, 10)
		if err != nil || len(history) != 1 {
			t.Fatalf("history = %+v, %v", history, err)
		}
		ref := DeliveryRef(alert.ID, history[0].ID)
		rows, err := db.ListDeliverySummaries(t.Context(), DeliveryProducer, []string{ref})
		if err != nil {
			t.Fatal(err)
		}
		if got := rows[ref]; len(got) != 1 || got[0].Event != string(notify.EventFiring) || got[0].Status != models.DeliveryFailed || got[0].LastError != discardedError {
			t.Errorf("notify_on_resolve %v: deliveries = %+v, want the firing failed with %q", notifyOnResolve, got, discardedError)
		}
	}
}

// TestSilenceWindowEnd: a held delivery waits until the end of the window
// that silences it, which a weekly silence's last day can cut short.
func TestSilenceWindowEnd(t *testing.T) {
	kolkata := mustLocation(t, "Asia/Kolkata")
	at := time.Date(2026, 11, 15, 2, 30, 0, 0, kolkata) // a Sunday
	ptr := func(t time.Time) *time.Time { return &t }
	for _, tc := range []struct {
		name    string
		silence *models.AlertSilence
		want    time.Time
	}{
		{"once", &models.AlertSilence{ScheduleKind: models.SilenceOnce, StartsAt: ptr(at.Add(-time.Hour)), EndsAt: ptr(at.Add(time.Hour))}, at.Add(time.Hour)},
		{"weekly window from the day before", &models.AlertSilence{ScheduleKind: models.SilenceWeekly,
			Weekly: &models.SilenceWeeklyWindow{Weekdays: []int{6}, Start: "23:00", DurationMinutes: 240, Timezone: "Asia/Kolkata"}}, time.Date(2026, 11, 15, 3, 0, 0, 0, kolkata)},
		{"weekly cut by its last day", &models.AlertSilence{ScheduleKind: models.SilenceWeekly, EndsAt: ptr(at.Add(10 * time.Minute)),
			Weekly: &models.SilenceWeeklyWindow{Weekdays: []int{6}, Start: "23:00", DurationMinutes: 240, Timezone: "Asia/Kolkata"}}, at.Add(10 * time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			end, active, err := activeUntil(tc.silence, at)
			if err != nil || !active || !end.Equal(tc.want) {
				t.Errorf("activeUntil = %s, %v, %v; want %s, active", end, active, err, tc.want)
			}
		})
	}
	sundayNight := &models.AlertSilence{ScheduleKind: models.SilenceWeekly,
		Weekly: &models.SilenceWeeklyWindow{Weekdays: []int{0}, Start: "23:00", DurationMinutes: 240, Timezone: "Asia/Kolkata"}}
	if _, active, err := activeUntil(sundayNight, at); err != nil || active {
		t.Errorf("a Sunday 23:00 window at 02:30 Sunday: active = %v, %v; want inactive", active, err)
	}
}
