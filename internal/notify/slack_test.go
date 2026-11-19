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

	"github.com/mr-karan/logchef/pkg/models"
)

var updateSlackGolden = flag.Bool("update-slack-golden", false, "rewrite testdata/slack_*.golden.json")

// slackReceiver answers each request with status and body, and keeps the
// request bodies.
func slackReceiver(t *testing.T, status int, body string, header http.Header) (*httptest.Server, chan []byte, *atomic.Int32) {
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

func testSlackSender(t *testing.T, logger *slog.Logger) *SlackSender {
	t.Helper()
	web := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1"), Logger: logger})
	t.Cleanup(web.client.CloseIdleConnections)
	return &SlackSender{http: web, logger: logger}
}

// slackDest is a Slack destination whose URL is target. The sender trusts
// the stored URL; ValidateSlackWebhookURL guards it on save.
func slackDest(target string) *models.NotificationDestination {
	return &models.NotificationDestination{ID: 1, Name: "ops channel", Type: models.DestinationTypeSlack, Secret: target, IsActive: true}
}

func goldenSlackMessage(event Event) Message {
	triggered := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	resolved := triggered.Add(30 * time.Minute)
	msg := Message{
		Event: event,
		Title: "[Logchef] High error rate [region=eu, service=checkout] (CRITICAL) TRIGGERED",
		Group: map[string]string{"service": "checkout", "region": "eu"},
		Alert: &AlertPayload{
			AlertID: 42, AlertName: "High error rate", Description: "5xx above normal",
			Status: "triggered", Severity: "critical", SourceID: 7, SourceName: "app-logs",
			Value: 412.5, ThresholdOperator: "gt", ThresholdValue: 100,
			TriggeredAt:  triggered,
			GeneratorURL: "https://logchef.example.com/logs/alerts/42?source=7",
		},
	}
	if event == EventResolved {
		msg.Title = "[Logchef] High error rate [region=eu, service=checkout] (CRITICAL) RESOLVED"
		msg.Alert.Status, msg.Alert.Value, msg.Alert.ResolvedAt = "resolved", 12, &resolved
	}
	if event == EventNoData {
		msg.Title = "[Logchef] High error rate [region=eu, service=checkout] (CRITICAL) NO DATA"
		msg.Alert.Value = 0
	}
	return msg
}

// TestSlackBodyGolden: the body of a grouped rule's firing, resolved and
// no-data messages. Each fixture was checked by hand against Slack's
// incoming webhook and Block Kit references.
func TestSlackBodyGolden(t *testing.T) {
	for _, event := range []Event{EventFiring, EventResolved, EventNoData} {
		t.Run(string(event), func(t *testing.T) {
			receiver, bodies, _ := slackReceiver(t, http.StatusOK, "ok", nil)
			if err := testSlackSender(t, discardLogger()).Send(t.Context(), slackDest(receiver.URL), "", 9, goldenSlackMessage(event)); err != nil {
				t.Fatal(err)
			}
			var indented bytes.Buffer
			if err := json.Indent(&indented, <-bodies, "", "  "); err != nil {
				t.Fatal(err)
			}
			got := append(indented.Bytes(), '\n')
			path := filepath.Join("testdata", "slack_"+string(event)+".golden.json")
			if *updateSlackGolden {
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
				t.Errorf("Slack body changed\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

// TestSlackEscapesAndCuts: log data cannot mention a channel or a user, and
// long text stays within Slack's limits.
func TestSlackEscapesAndCuts(t *testing.T) {
	msg := goldenSlackMessage(EventFiring)
	msg.Title = "<!channel> & co"
	msg.Group = map[string]string{"user": "<@U123> <!here>"}
	msg.Alert.AlertName = strings.Repeat("n", 400)
	msg.Alert.Description = strings.Repeat("a&", 4000)
	body, err := slackBody(msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("<!")) || bytes.Contains(body, []byte("<@")) {
		t.Errorf("body keeps a mention: %s", body)
	}
	var got slackMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != "&lt;!channel&gt; &amp; co" {
		t.Errorf("text = %q", got.Text)
	}
	if n := utf8.RuneCountInString(got.Blocks[0].Text.Text); n != slackHeaderLimit {
		t.Errorf("header has %d characters, want %d", n, slackHeaderLimit)
	}
	var description string
	for _, b := range got.Blocks {
		if b.Text != nil && strings.HasPrefix(b.Text.Text, "*Description*") {
			description = b.Text.Text
		}
	}
	kept, cutOff := strings.CutSuffix(description, "…")
	if n := utf8.RuneCountInString(description); !cutOff || n > slackTextLimit || n < slackTextLimit-5 {
		t.Errorf("description has %d characters, want a cut just under %d", n, slackTextLimit)
	}
	if !strings.HasSuffix(kept, "a&amp;") && !strings.HasSuffix(kept, "&amp;a") {
		t.Errorf("the cut split an escape: %q", kept[len(kept)-12:])
	}
}

func TestSlackAnswers(t *testing.T) {
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
		{name: "ok", status: http.StatusOK, body: "ok", ok: true},
		{name: "200 without ok", status: http.StatusOK, body: "", permanent: true, errText: "status 200 without ok"},
		{name: "no_service", status: http.StatusNotFound, body: "no_service", permanent: true, errText: "status 404: no_service"},
		{name: "channel_is_archived", status: http.StatusGone, body: "channel_is_archived", permanent: true, errText: "status 410: channel_is_archived"},
		{name: "invalid_payload", status: http.StatusBadRequest, body: "invalid_payload", permanent: true, errText: "status 400: invalid_payload"},
		{name: "rate limited", status: http.StatusTooManyRequests, body: "rate_limited", header: http.Header{"Retry-After": {"120"}}, retryAfter: 2 * time.Minute, errText: "status 429: rate_limited"},
		{name: "server error", status: http.StatusInternalServerError, body: "<html>oops</html>", errText: "status 500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receiver, _, calls := slackReceiver(t, tc.status, tc.body, tc.header)
			err := testSlackSender(t, discardLogger()).Send(t.Context(), slackDest(receiver.URL), "", 1, testMessage)
			if calls.Load() != 1 {
				t.Fatalf("receiver handled %d requests, want 1", calls.Load())
			}
			if tc.ok {
				if err != nil {
					t.Fatalf("Send = %v, want success", err)
				}
				return
			}
			if err == nil || IsPermanent(err) != tc.permanent || !strings.Contains(err.Error(), tc.errText) || strings.Contains(err.Error(), "oops") {
				t.Fatalf("Send = %v (permanent %t), want %q with permanent %t", err, IsPermanent(err), tc.errText, tc.permanent)
			}
			if out := outcome(1, err); tc.retryAfter > 0 && (out.Status != models.DeliveryPending || out.RetryAfter != tc.retryAfter) {
				t.Errorf("outcome = %+v, want pending after %s", out, tc.retryAfter)
			}
		})
	}
}

// TestSlackDrain: a Slack row goes through the queue; no_service fails it at
// once, and a 429 waits for its Retry-After.
func TestSlackDrain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		header http.Header
		want   models.DeliveryStatus
		wait   time.Duration
	}{
		{name: "sent", status: http.StatusOK, body: "ok", want: models.DeliverySent},
		{name: "no_service", status: http.StatusNotFound, body: "no_service", want: models.DeliveryFailed},
		{name: "rate limited", status: http.StatusTooManyRequests, header: http.Header{"Retry-After": {"300"}}, want: models.DeliveryPending, wait: 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receiver, _, calls := slackReceiver(t, tc.status, tc.body, tc.header)
			dest := slackDest(receiver.URL)
			dest.ID = 0
			e := newQueueEnv(t, dest, map[models.DestinationType]Sender{models.DestinationTypeSlack: testSlackSender(t, discardLogger())})
			e.enqueue(t, "")
			e.drain(t)
			row := e.row(t, 1)
			if row.Status != tc.want || row.Attempts != 1 || calls.Load() != 1 {
				t.Fatalf("row = %+v after %d requests, want %s after one", row, calls.Load(), tc.want)
			}
			if wait := time.Until(row.NextAttemptAt); tc.wait > 0 && (wait < tc.wait-time.Minute || wait > tc.wait) {
				t.Errorf("next attempt in %s, want about %s", wait, tc.wait)
			}
		})
	}
}

// TestSlackNeverLeaksTheURL: the webhook URL carries the channel's token, so
// no error and no log line may hold it.
func TestSlackNeverLeaksTheURL(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusTooManyRequests, http.StatusFound} {
		receiver, _, _ := slackReceiver(t, status, "no_service", http.Header{"Location": {"https://elsewhere.example.com/"}})
		target := receiver.URL + "/services/T000/B000/secret-slack-token"
		err := testSlackSender(t, logger).Send(t.Context(), slackDest(target), "", 1, testMessage)
		if err == nil || strings.Contains(err.Error(), "secret-slack-token") || strings.Contains(err.Error(), "/services/") {
			t.Errorf("status %d: error %v, want a failure without the URL", status, err)
		}
	}
	// A receiver that reflects the request path, as an error page may.
	reflector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "bad request: "+r.URL.Path)
	}))
	t.Cleanup(reflector.Close)
	if err := testSlackSender(t, logger).Send(t.Context(), slackDest(reflector.URL+"/services/T000/B000/secret-slack-token"), "", 1, testMessage); err == nil || strings.Contains(err.Error(), "secret-slack-token") {
		t.Errorf("reflected path: error %v, want a failure without the URL", err)
	}
	refused := httptest.NewServer(http.NotFoundHandler())
	refused.Close()
	if err := testSlackSender(t, logger).Send(t.Context(), slackDest(refused.URL+"/services/secret-slack-token"), "", 1, testMessage); err == nil || strings.Contains(err.Error(), "secret-slack-token") {
		t.Errorf("transport error %v, want one without the URL", err)
	}
	if strings.Contains(logs.String(), "secret-slack-token") || strings.Contains(logs.String(), "/services/") {
		t.Errorf("logs hold the URL: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `msg="slack delivery failed" destination="ops channel"`) {
		t.Errorf("logs = %s, want the failure by destination name", logs.String())
	}
}

func TestValidateSlackWebhookURL(t *testing.T) {
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"https://hooks.slack.com/services/T000/B000/XXXX", true},
		{"https://hooks.slack-gov.com/services/T000/B000/XXXX", true},
		{"https://HOOKS.SLACK.COM/services/T000/B000/XXXX", true},
		{"http://hooks.slack.com/services/T000/B000/XXXX", false},
		{"https://hooks.slack.com.evil.example/services/X", false},
		{"https://evil.example/hooks.slack.com/services/X", false},
		{"https://hooks.slack.com:8443/services/X", false},
		{"https://user:pass@hooks.slack.com/services/X", false},
		{"https://example.com/services/X", false},
		{"hooks.slack.com/services/X", false},
		{"", false},
	} {
		if err := ValidateSlackWebhookURL(tc.url); (err == nil) != tc.ok {
			t.Errorf("ValidateSlackWebhookURL(%q) = %v, want ok %t", tc.url, err, tc.ok)
		}
	}
}

// TestSlackRetriesALostAnswer: a connection that fails after a 200 status
// line, before the body, is transient; Slack may not have said ok.
func TestSlackRetriesALostAnswer(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nok")
		_ = buf.Flush()
		_ = conn.Close()
	}))
	t.Cleanup(receiver.Close)
	err := testSlackSender(t, discardLogger()).Send(t.Context(), slackDest(receiver.URL), "", 1, testMessage)
	if err == nil || IsPermanent(err) {
		t.Fatalf("Send = %v, want a transient failure", err)
	}
	if out := outcome(1, err); out.Status != models.DeliveryPending {
		t.Errorf("outcome = %+v, want a retry", out)
	}
}
