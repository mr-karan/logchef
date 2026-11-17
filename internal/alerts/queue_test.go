package alerts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestCommittedNotificationsOutliveTheEvaluator: an evaluation sends
// nothing itself; its commit leaves delivery rows that a drain in another
// process sends after the evaluator is gone, firing before resolved.
func TestCommittedNotificationsOutliveTheEvaluator(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	var mu sync.Mutex
	var statuses []string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		statuses = append(statuses, body.Status)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	linkWebhooks(t, db, alert, receiver.URL)

	m := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
	if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
		t.Fatal(err)
	}
	if err := m.handleResolved(t.Context(), alert, 1); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	sentByEvaluator := len(statuses)
	mu.Unlock()
	if sentByEvaluator != 0 {
		t.Fatalf("the evaluator sent %d requests itself, want none", sentByEvaluator)
	}

	drainQueue(t, db)
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"triggered", "resolved"}; !slices.Equal(statuses, want) {
		t.Fatalf("receiver got %v, want %v", statuses, want)
	}
	history, err := db.ListAlertHistory(t.Context(), alert.ID, 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	refs := []string{DeliveryRef(alert.ID, history[0].ID)}
	deliveries, err := db.ListDeliverySummaries(t.Context(), DeliveryProducer, refs)
	if err != nil {
		t.Fatal(err)
	}
	rows := deliveries[refs[0]]
	if len(rows) != 2 || rows[0].Event != "firing" || rows[1].Event != "resolved" || rows[0].Status != models.DeliverySent || rows[1].Status != models.DeliverySent {
		t.Errorf("deliveries of the episode = %+v, want firing and resolved sent", rows)
	}
}

// TestInactiveEmailDestinationDoesNotClaimAnAddress: an address reached by an
// inactive and an active email destination is still mailed through the
// active one.
func TestInactiveEmailDestinationDoesNotClaimAnAddress(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	email := func(active bool) models.DestinationID {
		d := &models.NotificationDestination{Name: "mail", Type: models.DestinationTypeEmail, Scope: models.DestinationScopeGlobal,
			Config: models.DestinationConfig{Addresses: []string{"same@example.com"}}, IsActive: true}
		if err := db.CreateNotificationDestination(t.Context(), d); err != nil {
			t.Fatal(err)
		}
		if !active {
			d.IsActive = false
			if err := db.UpdateNotificationDestination(t.Context(), d); err != nil {
				t.Fatal(err)
			}
		}
		return d.ID
	}
	inactive, active := email(false), email(true)
	linkDestinations(t, db, alert, inactive, active)
	m := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
	if err := m.handleTriggered(t.Context(), alert, 12); err != nil {
		t.Fatal(err)
	}
	queue := &queueRecorder{t: t, db: db}
	var reached []models.DestinationID
	for _, row := range queue.rows() {
		if row.Recipient == "same@example.com" {
			reached = append(reached, row.DestinationID)
		}
	}
	if !slices.Contains(reached, active) {
		t.Fatalf("rows for same@example.com went to %v, want one through the active destination %d", reached, active)
	}
}
