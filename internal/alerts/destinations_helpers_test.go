package alerts

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// webhookDestination stores an active version 1 webhook destination.
func webhookDestination(t *testing.T, db store.Store, url string) models.DestinationID {
	t.Helper()
	d := &models.NotificationDestination{Name: "hook", Type: models.DestinationTypeWebhook, Scope: models.DestinationScopeGlobal,
		Config: models.DestinationConfig{PayloadVersion: 1}, Secret: url, IsActive: true}
	if err := db.CreateNotificationDestination(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

// linkDestinations replaces the rule's destinations.
func linkDestinations(t *testing.T, db store.Store, alert *models.Alert, ids ...models.DestinationID) {
	t.Helper()
	if err := db.SetAlertDestinations(t.Context(), alert.ID, ids); err != nil {
		t.Fatal(err)
	}
}

// linkWebhooks gives the rule one new webhook destination per URL.
func linkWebhooks(t *testing.T, db store.Store, alert *models.Alert, urls ...string) {
	t.Helper()
	ids := make([]models.DestinationID, len(urls))
	for i, url := range urls {
		ids[i] = webhookDestination(t, db, url)
	}
	linkDestinations(t, db, alert, ids...)
}

// queueRecorder reads the notifications that evaluations enqueue for a rule.
// It links a webhook destination to the rule, so each notification is one
// row, and nothing sends it.
type queueRecorder struct {
	t    *testing.T
	db   store.Store
	last int64
}

func recordQueue(t *testing.T, db store.Store, alert *models.Alert) *queueRecorder {
	t.Helper()
	linkWebhooks(t, db, alert, "https://hooks.example.com/recorded")
	return &queueRecorder{t: t, db: db}
}

// take returns the notifications enqueued since the last take, in order.
func (r *queueRecorder) take() []AlertNotification {
	r.t.Helper()
	var out []AlertNotification
	for _, row := range r.rows() {
		var msg notify.Message
		if err := json.Unmarshal([]byte(row.MessageJSON), &msg); err != nil {
			r.t.Fatal(err)
		}
		out = append(out, notificationOf(msg))
	}
	return out
}

// rows returns the delivery rows written since the last take.
func (r *queueRecorder) rows() []*models.NotificationDelivery {
	r.t.Helper()
	var out []*models.NotificationDelivery
	for {
		row, err := r.db.GetDelivery(r.t.Context(), r.last+1)
		if models.IsNotFound(err) {
			return out
		}
		if err != nil {
			r.t.Fatal(err)
		}
		r.last = row.ID
		out = append(out, row)
	}
}

// notificationOf reads back the parts of a notification that tests check.
func notificationOf(msg notify.Message) AlertNotification {
	var status models.AlertStatus
	switch msg.Event {
	case notify.EventFiring, notify.EventTest:
		status = models.AlertStatusTriggered
	case notify.EventResolved:
		status = models.AlertStatusResolved
	case notify.EventNoData:
		status = models.AlertStatusNoData
	}
	n := AlertNotification{Status: status, Group: msg.Group}
	if a := msg.Alert; a != nil {
		n.AlertID, n.AlertName, n.Value, n.Message = models.AlertID(a.AlertID), a.AlertName, a.Value, a.Message
		n.Labels, n.Annotations, n.TriggeredAt, n.ResolvedAt = a.Labels, a.Annotations, a.TriggeredAt, a.ResolvedAt
	}
	return n
}

// drainQueue sends every due row once, through a webhook sender that may
// reach loopback receivers.
func drainQueue(t *testing.T, db store.Store) {
	t.Helper()
	allowlist, err := config.ParseWebhookAllowlist([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	drainer := notify.NewDrainer(notify.DrainerOptions{
		Store:   db,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Senders: map[models.DestinationType]notify.Sender{models.DestinationTypeWebhook: notify.NewWebhookSender(notify.WebhookSenderOptions{AllowedHosts: allowlist})},
	})
	if err := drainer.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
}
