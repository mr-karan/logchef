package notify

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// v2TestMessage carries both payloads, as every alert message does.
func v2TestMessage() Message {
	return Message{
		Event: EventFiring,
		Alert: &AlertPayload{AlertName: "errors", Value: 12},
		Webhook: &WebhookContent{
			Title: "errors: firing", Severity: "critical",
			Alert:     WebhookRule{ID: 3, Name: "errors", RuleType: "threshold"},
			Instances: []WebhookInstance{{Status: InstanceFiring, TriggeredAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}},
		},
	}
}

func hookV2(target string) *models.NotificationDestination {
	d := hook(target)
	d.Config.PayloadVersion = WebhookPayloadV2
	return d
}

// TestWebhookV2AlwaysHasEveryKey: absent values are null or empty objects,
// never missing keys, so a strict receiver can rely on the shape.
func TestWebhookV2AlwaysHasEveryKey(t *testing.T) {
	msg := v2TestMessage()
	msg.Webhook.Instances[0].Status = InstanceNoData
	body, err := webhookBody(WebhookPayloadV2, 0, msg, time.Date(2026, 1, 2, 3, 4, 6, 500, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"version":2,"kind":"alert","event":"firing","title":"errors: firing","url":null,"severity":"critical","sent_at":"2026-01-02T03:04:06Z","delivery_id":null,` +
		`"alert":{"id":3,"name":"errors","description":"","source_id":0,"source_name":"","rule_type":"threshold","query_language":"","query":"","threshold_operator":"","threshold_value":0,"frequency_seconds":0,"lookback_seconds":0,"labels":{},"annotations":{}},` +
		`"instances":[{"group":{},"status":"no_data","value":null,"triggered_at":"2026-01-02T03:04:05Z","resolved_at":null,"history_id":null}],"instances_omitted":0,"triage":null}`
	if string(body) != want {
		t.Errorf("body\n got: %s\nwant: %s", body, want)
	}
	if msg.Webhook.Instances[0].Group != nil {
		t.Error("rendering changed the stored message")
	}
}

func TestWebhookBodyRefusesWhatItCannotRender(t *testing.T) {
	noContent := v2TestMessage()
	noContent.Webhook, noContent.Alert = nil, nil
	for name, tc := range map[string]struct {
		version int
		msg     Message
	}{
		"unknown version":            {3, v2TestMessage()},
		"unset version":              {0, v2TestMessage()},
		"version 2 without any body": {WebhookPayloadV2, noContent},
		"version 1 without body":     {WebhookPayloadV1, Message{Event: EventFiring, Webhook: v2TestMessage().Webhook}},
	} {
		t.Run(name, func(t *testing.T) {
			sender := NewWebhookSender(WebhookSenderOptions{})
			dest := hook("https://hooks.example.com/x")
			dest.Config.PayloadVersion = tc.version
			if err := sender.Send(t.Context(), dest, "", 1, tc.msg); err == nil || !IsPermanent(err) {
				t.Errorf("error = %v, want permanent", err)
			}
		})
	}
}

func TestWebhookV2CutsTheTitle(t *testing.T) {
	msg := v2TestMessage()
	msg.Webhook.Title = strings.Repeat("é", 300)
	body, err := webhookBody(WebhookPayloadV2, 0, msg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("é", 199) + "…"; got.Title != want {
		t.Errorf("title has %d characters, want 200 ending in an ellipsis", len([]rune(got.Title)))
	}
}

// TestWebhookV2Classification: a version 2 destination classifies answers
// as every webhook does: 2xx sent, 429 after Retry-After, 408 and 5xx
// retried, any other 4xx failed at once.
func TestWebhookV2Classification(t *testing.T) {
	for _, tc := range []struct {
		status     int
		retryAfter string
		want       string // sent, retry or permanent
		after      time.Duration
	}{
		{http.StatusOK, "", "sent", 0},
		{http.StatusAccepted, "", "sent", 0},
		{http.StatusNoContent, "", "sent", 0},
		{http.StatusTooManyRequests, "120", "retry", 2 * time.Minute},
		{http.StatusTooManyRequests, "", "retry", 0},
		{http.StatusRequestTimeout, "", "retry", 0},
		{http.StatusInternalServerError, "", "retry", 0},
		{http.StatusServiceUnavailable, "", "retry", 0},
		{http.StatusBadRequest, "", "permanent", 0},
		{http.StatusUnauthorized, "", "permanent", 0},
		{http.StatusNotFound, "", "permanent", 0},
		{http.StatusFound, "", "permanent", 0},
	} {
		t.Run(http.StatusText(tc.status)+tc.retryAfter, func(t *testing.T) {
			var got map[string]any
			var deliveryHeader string
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &got)
				deliveryHeader = r.Header.Get("Logchef-Delivery-Id")
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				if tc.status == http.StatusFound {
					w.Header().Set("Location", "https://elsewhere.example.com/")
				}
				w.WriteHeader(tc.status)
			}))
			defer receiver.Close()
			sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
			defer sender.client.CloseIdleConnections()
			err := sender.Send(t.Context(), hookV2(receiver.URL), "", 77, v2TestMessage())
			if got["version"] != float64(2) || got["delivery_id"] != float64(77) || deliveryHeader != "77" {
				t.Errorf("request body %v, header %q; want version 2 with delivery 77 in both", got, deliveryHeader)
			}
			outcome := "retry"
			switch {
			case err == nil:
				outcome = "sent"
			case IsPermanent(err):
				outcome = "permanent"
			}
			if outcome != tc.want {
				t.Fatalf("error = %v, outcome %s, want %s", err, outcome, tc.want)
			}
			if r, ok := errors.AsType[*retryAfterError](err); tc.after > 0 && (!ok || r.after != tc.after) {
				t.Errorf("retry after = %v; want %v", r, tc.after)
			}
		})
	}
}
