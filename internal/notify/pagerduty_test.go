package notify

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var updatePagerDutyGolden = flag.Bool("update-pagerduty-golden", false, "rewrite testdata/pagerduty_*.golden.json")

// Test-only routing key; not a credential.
const testRoutingKey = "R0UT1NGKEYR0UT1NGKEYR0UT1NGKEY12"

// pagerDutyReceiver stands in for the Events API: it answers each request
// with status and body, and keeps the request bodies.
func pagerDutyReceiver(t *testing.T, status int, body string, header http.Header) (*httptest.Server, chan []byte, *atomic.Int32) {
	t.Helper()
	bodies := make(chan []byte, 16)
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		got, _ := io.ReadAll(r.Body)
		bodies <- got
		maps.Copy(w.Header(), header)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(receiver.Close)
	return receiver, bodies, &calls
}

func testPagerDutySender(t *testing.T, logger *slog.Logger, url string) *PagerDutySender {
	t.Helper()
	web := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1"), Logger: logger})
	t.Cleanup(web.client.CloseIdleConnections)
	return &PagerDutySender{http: web, logger: logger, url: url}
}

func pagerDutyDest() *models.NotificationDestination {
	return &models.NotificationDestination{Name: "payments on-call", Type: models.DestinationTypePagerDuty, Secret: testRoutingKey, IsActive: true}
}

const accepted = `{"status":"success","message":"Event processed","dedup_key":"x"}`

// goldenPagerDutyMessage is one group's message. The dedup key is the one
// internal/alerts derives for the group, and the same for each event.
func goldenPagerDutyMessage(event Event, service string) Message {
	triggered := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	keys := map[string]string{"checkout": "logchef:alert:42:f9f77b923b27b024", "search": "logchef:alert:42:f0d9e5f71a7c8bca"}
	msg := Message{
		Event: event, Title: "[Logchef] High error rate", DedupKey: keys[service],
		Group: map[string]string{"service": service},
		Alert: &AlertPayload{
			AlertID: 42, AlertName: "High error rate", Description: "5xx above normal",
			Severity: "critical", SourceID: 7, SourceName: "app-logs",
			Value: 412.5, ThresholdOperator: "gt", ThresholdValue: 100,
			Query:       "SELECT service, count() AS value FROM logs WHERE status >= 500 GROUP BY service",
			Labels:      map[string]string{"team": "payments", "service": service},
			TriggeredAt: triggered, GeneratorURL: "https://logchef.example.com/logs/alerts/42?source=7",
		},
	}
	if event == EventResolved {
		resolved := triggered.Add(30 * time.Minute)
		msg.Alert.Value, msg.Alert.ResolvedAt = 12, &resolved
	}
	return msg
}

// TestPagerDutyBodyGolden: one group's firing is a trigger and its resolved
// a resolve with the same dedup_key, and no data is a trigger. Each fixture
// was checked by hand against PagerDuty's "Send an alert event" reference.
func TestPagerDutyBodyGolden(t *testing.T) {
	for _, event := range []Event{EventFiring, EventResolved, EventNoData} {
		t.Run(string(event), func(t *testing.T) {
			receiver, bodies, _ := pagerDutyReceiver(t, http.StatusAccepted, accepted, nil)
			if err := testPagerDutySender(t, discardLogger(), receiver.URL).Send(t.Context(), pagerDutyDest(), "", 9, goldenPagerDutyMessage(event, "checkout")); err != nil {
				t.Fatal(err)
			}
			var indented bytes.Buffer
			if err := json.Indent(&indented, <-bodies, "", "  "); err != nil {
				t.Fatal(err)
			}
			got := append(indented.Bytes(), '\n')
			path := filepath.Join("testdata", "pagerduty_"+string(event)+".golden.json")
			if *updatePagerDutyGolden {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("PagerDuty body changed\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

// TestPagerDutyDedupKeys: a trigger and its resolve share a dedup_key, and
// two groups have two.
func TestPagerDutyDedupKeys(t *testing.T) {
	type sent struct {
		Action string `json:"event_action"`
		Key    string `json:"dedup_key"`
	}
	read := func(msg Message) sent {
		t.Helper()
		body, err := pagerDutyBody(testRoutingKey, msg)
		if err != nil {
			t.Fatal(err)
		}
		var s sent
		if err := json.Unmarshal(body, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	trigger, resolve := read(goldenPagerDutyMessage(EventFiring, "checkout")), read(goldenPagerDutyMessage(EventResolved, "checkout"))
	other := read(goldenPagerDutyMessage(EventFiring, "search"))
	if trigger.Action != "trigger" || resolve.Action != "resolve" || trigger.Key != resolve.Key {
		t.Errorf("trigger %+v and resolve %+v, want one dedup_key", trigger, resolve)
	}
	if other.Key == trigger.Key {
		t.Errorf("two groups share dedup_key %q", other.Key)
	}
}

func TestPagerDutyRefusesAMessageWithoutDedupKey(t *testing.T) {
	receiver, _, calls := pagerDutyReceiver(t, http.StatusAccepted, accepted, nil)
	msg := goldenPagerDutyMessage(EventFiring, "checkout")
	msg.DedupKey = ""
	if err := testPagerDutySender(t, discardLogger(), receiver.URL).Send(t.Context(), pagerDutyDest(), "", 1, msg); !IsPermanent(err) || calls.Load() != 0 {
		t.Errorf("Send = %v after %d requests, want a permanent error and none", err, calls.Load())
	}
}

func TestPagerDutyCutsTheSummary(t *testing.T) {
	msg := goldenPagerDutyMessage(EventFiring, strings.Repeat("s", 2000))
	msg.DedupKey = "logchef:alert:42:0000000000000000"
	body, err := pagerDutyBody(testRoutingKey, msg)
	if err != nil {
		t.Fatal(err)
	}
	var event pagerDutyEvent
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(event.Payload.Summary); n != pagerDutySummaryLimit || !strings.HasSuffix(event.Payload.Summary, "…") {
		t.Errorf("summary has %d characters, want a cut at %d", n, pagerDutySummaryLimit)
	}
}

func TestPagerDutyAnswers(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		header     http.Header
		ok         bool
		permanent  bool
		retryAfter time.Duration
		errText    string
	}{
		{name: "accepted", status: http.StatusAccepted, body: accepted, ok: true},
		{name: "invalid event", status: http.StatusBadRequest, body: `{"status":"invalid event","message":"Event object is invalid","errors":["'payload.severity' is invalid"]}`, permanent: true, errText: "status 400: Event object is invalid: 'payload.severity' is invalid"},
		{name: "invalid event without JSON", status: http.StatusBadRequest, body: "Bad Request", permanent: true, errText: "status 400"},
		{name: "throttled", status: http.StatusTooManyRequests, header: http.Header{"Retry-After": {"90"}}, retryAfter: 90 * time.Second, errText: "status 429"},
		{name: "throttled without Retry-After", status: http.StatusTooManyRequests, errText: "status 429"},
		{name: "server error", status: http.StatusBadGateway, body: "<html>bad gateway</html>", errText: "status 502"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receiver, _, calls := pagerDutyReceiver(t, tc.status, tc.body, tc.header)
			err := testPagerDutySender(t, discardLogger(), receiver.URL).Send(t.Context(), pagerDutyDest(), "", 1, goldenPagerDutyMessage(EventFiring, "checkout"))
			if calls.Load() != 1 {
				t.Fatalf("receiver handled %d requests, want 1", calls.Load())
			}
			if tc.ok {
				if err != nil {
					t.Fatalf("Send = %v, want success", err)
				}
				return
			}
			if err == nil || IsPermanent(err) != tc.permanent || !strings.HasSuffix(err.Error(), tc.errText) {
				t.Fatalf("Send = %v (permanent %t), want %q with permanent %t", err, IsPermanent(err), tc.errText, tc.permanent)
			}
			out := outcome(1, err)
			if tc.retryAfter > 0 && (out.Status != models.DeliveryPending || out.RetryAfter != tc.retryAfter) {
				t.Errorf("outcome = %+v, want pending after %s", out, tc.retryAfter)
			}
			if !tc.permanent && out.Status != models.DeliveryPending {
				t.Errorf("outcome = %+v, want a retry", out)
			}
		})
	}
}

// TestPagerDutyDrain: a PagerDuty row goes through the queue; 202 is sent,
// 400 fails at once, and 429 waits for its retry.
func TestPagerDutyDrain(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   models.DeliveryStatus
	}{
		{http.StatusAccepted, models.DeliverySent},
		{http.StatusBadRequest, models.DeliveryFailed},
		{http.StatusTooManyRequests, models.DeliveryPending},
	} {
		receiver, _, calls := pagerDutyReceiver(t, tc.status, accepted, nil)
		e := newQueueEnv(t, pagerDutyDest(), map[models.DestinationType]Sender{models.DestinationTypePagerDuty: testPagerDutySender(t, discardLogger(), receiver.URL)})
		if err := e.db.WithTx(t.Context(), func(tx store.StoreOps) error {
			return Enqueue(t.Context(), tx, []Delivery{{DestinationID: e.dest.ID, Producer: "test", ProducerRef: "ref", SequenceKey: "s", DedupKey: "d", Message: goldenPagerDutyMessage(EventFiring, "checkout")}})
		}); err != nil {
			t.Fatal(err)
		}
		e.drain(t)
		if row := e.row(t, 1); row.Status != tc.want || row.Attempts != 1 || calls.Load() != 1 {
			t.Errorf("status %d: row %+v after %d requests, want %s after one", tc.status, row, calls.Load(), tc.want)
		}
	}
}

// TestPagerDutyNeverLeaksTheRoutingKey: an Events API answer that echoes the
// routing key reaches neither the error nor the log.
func TestPagerDutyNeverLeaksTheRoutingKey(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	echoes := []string{
		`{"status":"invalid event","message":"Unknown routing key ` + testRoutingKey + `"}`,
		// The key straddles the 256-byte read cap.
		`{"message":"` + strings.Repeat("x", 201) + testRoutingKey + `", "errors":["more"]}`,
		// The key in a JSON escape.
		`{"message":"key \u0052` + testRoutingKey[1:] + `"}`,
		"plain text " + testRoutingKey,
	}
	for _, echo := range echoes {
		receiver, _, _ := pagerDutyReceiver(t, http.StatusBadRequest, echo, nil)
		err := testPagerDutySender(t, logger, receiver.URL).Send(t.Context(), pagerDutyDest(), "", 1, goldenPagerDutyMessage(EventFiring, "checkout"))
		if err == nil || strings.Contains(err.Error(), testRoutingKey[:8]) {
			t.Errorf("echo %q: error %v, want a failure without the routing key", echo[:20], err)
		}
	}
	if strings.Contains(logs.String(), testRoutingKey[:8]) {
		t.Errorf("logs hold the routing key: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `msg="pagerduty delivery failed" destination="payments on-call"`) {
		t.Errorf("logs = %s, want the failure by destination name", logs.String())
	}
}

func TestValidatePagerDutyRoutingKey(t *testing.T) {
	for _, tc := range []struct {
		key string
		ok  bool
	}{
		{"0123456789abcdef0123456789ABCDEF", true},
		{"0123456789abcdef0123456789abcde", false},
		{"0123456789abcdef0123456789abcdef0", false},
		{"0123456789abcdef0123456789abcd-f", false},
		{"https://events.pagerduty.com/x12", false},
		{"", false},
	} {
		if err := ValidatePagerDutyRoutingKey(tc.key); (err == nil) != tc.ok {
			t.Errorf("ValidatePagerDutyRoutingKey(%q) = %v, want ok %t", tc.key, err, tc.ok)
		}
	}
}
