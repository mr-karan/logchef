package alerts

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// webhookV2Destination creates a global version 2 webhook destination.
func webhookV2Destination(t *testing.T, db store.Store, url string) models.DestinationID {
	t.Helper()
	d := &models.NotificationDestination{Name: "hook v2", Type: models.DestinationTypeWebhook, Scope: models.DestinationScopeGlobal,
		Config: models.DestinationConfig{PayloadVersion: notify.WebhookPayloadV2}, Secret: url, IsActive: true}
	if err := db.CreateNotificationDestination(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

type receivedRequest struct {
	body       []byte
	deliveryID string
}

// recordingReceiver is a local receiver that keeps every request.
func recordingReceiver(t *testing.T) (receiver *httptest.Server, received func() []receivedRequest) {
	t.Helper()
	var mu sync.Mutex
	var got []receivedRequest
	receiver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, receivedRequest{body: body, deliveryID: r.Header.Get("Logchef-Delivery-Id")})
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	return receiver, func() []receivedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]receivedRequest(nil), got...)
	}
}

var sentAtField = regexp.MustCompile(`"sent_at":"([^"]+)"`)

// TestWebhookV2PayloadGolden: a version 2 destination receives the RFC 0006
// body, after the message went through the delivery queue. The fixtures
// were written by hand from the RFC. sent_at is the send time, so the test
// checks it falls within the drain and then pins it.
func TestWebhookV2PayloadGolden(t *testing.T) {
	triggered := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	resolved := triggered.Add(time.Hour)
	for _, tc := range []struct {
		fixture string
		event   notify.Event
		edit    func(*AlertNotification)
	}{
		{"webhook_v2_firing.golden.json", notify.EventFiring, func(*AlertNotification) {}},
		{"webhook_v2_resolved.golden.json", notify.EventResolved, func(n *AlertNotification) {
			n.Status, n.Value, n.ResolvedAt = models.AlertStatusResolved, 80, &resolved
		}},
		{"webhook_v2_no_data.golden.json", notify.EventNoData, func(n *AlertNotification) {
			n.Status, n.Value = models.AlertStatusNoData, 0
		}},
		{"webhook_v2_grouped.golden.json", notify.EventFiring, func(n *AlertNotification) {
			n.Group = map[string]string{"service": "checkout", "region": "eu"}
		}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			receiver, received := recordingReceiver(t)
			db, alert, _ := newManagerTestAlert(t)
			destination := webhookV2Destination(t, db, receiver.URL)
			n := goldenV2Notification(triggered)
			tc.edit(&n)
			if err := notify.Enqueue(t.Context(), db, []notify.Delivery{{
				DestinationID: destination, Producer: DeliveryProducer, ProducerRef: DeliveryRef(alert.ID, n.HistoryID),
				SequenceKey: "golden", DedupKey: "golden", Message: n.message(tc.event),
			}}); err != nil {
				t.Fatal(err)
			}
			start := time.Now().UTC().Truncate(time.Second)
			drainQueue(t, db)
			end := time.Now().UTC()
			requests := received()
			if len(requests) != 1 {
				t.Fatalf("receiver got %d requests, want 1", len(requests))
			}
			got := requests[0]
			match := sentAtField.FindSubmatch(got.body)
			if match == nil {
				t.Fatalf("body has no sent_at: %s", got.body)
			}
			sentAt, err := time.Parse(time.RFC3339, string(match[1]))
			if err != nil || sentAt.Before(start) || sentAt.After(end) {
				t.Fatalf("sent_at = %s, %v; want the send time, between %s and %s", match[1], err, start, end)
			}
			var envelope struct {
				DeliveryID int64 `json:"delivery_id"`
			}
			if err := json.Unmarshal(got.body, &envelope); err != nil || strconv.FormatInt(envelope.DeliveryID, 10) != got.deliveryID {
				t.Errorf("delivery_id %d, header %q, %v; want the row id in both", envelope.DeliveryID, got.deliveryID, err)
			}
			pinned := sentAtField.ReplaceAll(got.body, []byte(`"sent_at":"2026-01-02T05:00:00Z"`))
			want, err := os.ReadFile(filepath.Join("testdata", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(pinned, want) {
				t.Errorf("version 2 body\n got: %s\nwant: %s", pinned, want)
			}
		})
	}
}

func goldenV2Notification(triggered time.Time) AlertNotification {
	return AlertNotification{
		AlertID: 42, AlertName: "High error rate", Description: "5xx above normal",
		Status: models.AlertStatusTriggered, Severity: models.AlertSeverityCritical,
		SourceID: 7, SourceName: "app-logs", Value: 412.5,
		ThresholdOp: models.AlertThresholdGreaterThan, ThresholdValue: 100,
		FrequencySecs: 60, LookbackSecs: 300,
		QueryLanguage: models.QueryLanguageClickHouseSQL, Query: "SELECT count() FROM logs WHERE status >= 500",
		Labels:          map[string]string{"alertname": "High error rate", "team": "payments"},
		Annotations:     map[string]string{"runbook": "https://runbooks.example.com/5xx", "threshold": "gt 100.0000"},
		RuleLabels:      map[string]string{"team": "payments"},
		RuleAnnotations: map[string]string{"runbook": "https://runbooks.example.com/5xx"},
		HistoryID:       981, TriggeredAt: triggered,
		GeneratorURL: "https://logchef.example.com/logs/alerts/42?source=7",
		Message:      "alert High error rate triggered with value 412.5000",
	}
}

// TestGroupedWebhookV2FromEvaluation: an evaluation of a grouped rule sends
// a version 2 destination one request per group, each naming its group, its
// history row and the rule's own labels.
func TestGroupedWebhookV2FromEvaluation(t *testing.T) {
	f := newGroupedFixture(t)
	f.alert.Labels = map[string]string{"team": "payments"}
	if err := f.db.UpdateAlert(t.Context(), f.alert); err != nil {
		t.Fatal(err)
	}
	receiver, received := recordingReceiver(t)
	linkDestinations(t, f.db, f.alert, webhookV2Destination(t, f.db, receiver.URL))
	f.provider.results = append(f.provider.results, groupedResult(row("checkout", 12), row("search", 30)))
	alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.manager.evaluateAlert(t.Context(), alert, time.Now()); err != nil {
		t.Fatal(err)
	}
	drainQueue(t, f.db)

	history, err := f.db.ListAlertHistory(t.Context(), alert.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	historyOf := map[string]int64{}
	for _, h := range history {
		historyOf[h.Group["service"]] = h.ID
	}
	requests := received()
	if len(requests) != 2 {
		t.Fatalf("receiver got %d requests, want one per group", len(requests))
	}
	seen := map[string]bool{}
	for _, r := range requests {
		var body struct {
			Version int    `json:"version"`
			Event   string `json:"event"`
			Alert   struct {
				QueryLanguage string            `json:"query_language"`
				Labels        map[string]string `json:"labels"`
			} `json:"alert"`
			Instances []struct {
				Group     map[string]string `json:"group"`
				Status    string            `json:"status"`
				HistoryID int64             `json:"history_id"`
			} `json:"instances"`
		}
		if err := json.Unmarshal(r.body, &body); err != nil {
			t.Fatal(err)
		}
		if body.Version != 2 || body.Event != "firing" || len(body.Instances) != 1 {
			t.Fatalf("body = %s, want one firing instance in version 2", r.body)
		}
		if body.Alert.QueryLanguage != string(alert.QueryLanguage) || len(body.Alert.Labels) != 1 || body.Alert.Labels["team"] != "payments" {
			t.Errorf("alert = %+v, want the rule's language and only its own labels", body.Alert)
		}
		instance := body.Instances[0]
		service := instance.Group["service"]
		if len(instance.Group) != 1 || instance.Status != "firing" || instance.HistoryID == 0 || instance.HistoryID != historyOf[service] {
			t.Errorf("instance = %+v, want group service=%s firing with its history row %d", instance, service, historyOf[service])
		}
		seen[service] = true
	}
	if !seen["checkout"] || !seen["search"] {
		t.Errorf("groups sent = %v, want checkout and search", seen)
	}
}

// TestWebhookV2SendsRowsQueuedBeforeIt: a row queued before version 2
// existed holds only the version 1 payload. When its destination moves to
// version 2 before the row is sent, the row still reaches the receiver as
// version 2, with what the version 1 payload holds.
func TestWebhookV2SendsRowsQueuedBeforeIt(t *testing.T) {
	receiver, received := recordingReceiver(t)
	db, alert, _ := newManagerTestAlert(t)
	destination := webhookDestination(t, db, receiver.URL)
	triggered := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	n := goldenV2Notification(triggered)
	n.Status, n.Group = models.AlertStatusNoData, map[string]string{"service": "checkout"}
	legacy := n.message(notify.EventNoData)
	legacy.Webhook = nil
	if err := notify.Enqueue(t.Context(), db, []notify.Delivery{{
		DestinationID: destination, Producer: DeliveryProducer, ProducerRef: DeliveryRef(alert.ID, n.HistoryID),
		SequenceKey: "legacy", DedupKey: "legacy", Message: legacy,
	}}); err != nil {
		t.Fatal(err)
	}
	d, err := db.GetNotificationDestination(t.Context(), destination)
	if err != nil {
		t.Fatal(err)
	}
	d.Config.PayloadVersion = notify.WebhookPayloadV2
	if err := db.UpdateNotificationDestination(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	drainQueue(t, db)

	requests := received()
	if len(requests) != 1 {
		t.Fatalf("receiver got %d requests, want the queued row sent once", len(requests))
	}
	var body struct {
		Version int    `json:"version"`
		Event   string `json:"event"`
		Title   string `json:"title"`
		Alert   struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"alert"`
		Instances []struct {
			Group  map[string]string `json:"group"`
			Status string            `json:"status"`
			Value  *float64          `json:"value"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(requests[0].body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != 2 || body.Event != "no_data" || body.Title != "High error rate: service=checkout no data" || body.Alert.ID != 42 || body.Alert.Name != "High error rate" {
		t.Errorf("body = %s, want version 2 no_data for rule 42 and its group", requests[0].body)
	}
	if len(body.Instances) != 1 || body.Instances[0].Group["service"] != "checkout" || body.Instances[0].Status != "no_data" || body.Instances[0].Value != nil {
		t.Errorf("instances = %+v, want the one no_data group without a value", body.Instances)
	}
}
