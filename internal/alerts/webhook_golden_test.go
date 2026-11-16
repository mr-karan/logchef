package alerts

import (
	"bytes"
	"context"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

var updateWebhookGolden = flag.Bool("update-webhook-golden", false, "rewrite testdata/webhook_v1.golden.json")

// TestWebhookV1PayloadGolden: a version 1 webhook destination receives the
// payload that a rule's webhook URL received before destinations existed,
// byte for byte. The fixture was written by the sender before the move.
func TestWebhookV1PayloadGolden(t *testing.T) {
	bodies := make(chan []byte, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
	}))
	defer receiver.Close()
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
	defer sender.client.CloseIdleConnections()

	triggered := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	resolved := triggered.Add(time.Hour)
	notification := goldenNotification(receiver.URL, triggered, &resolved)
	if err := sender.Send(context.Background(), notification).Err(); err != nil {
		t.Fatal(err)
	}
	got := <-bodies
	path := filepath.Join("testdata", "webhook_v1.golden.json")
	if *updateWebhookGolden {
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
		t.Errorf("version 1 payload changed\n got: %s\nwant: %s", got, want)
	}
}

func goldenNotification(url string, triggered time.Time, resolved *time.Time) AlertNotification {
	return AlertNotification{
		AlertID: 42, AlertName: "High error rate", Description: "5xx above normal",
		Status: models.AlertStatusResolved, Severity: models.AlertSeverityCritical,
		SourceID: 7, SourceName: "app-logs", Value: 412.5,
		ThresholdOp: models.AlertThresholdGreaterThan, ThresholdValue: 100,
		FrequencySecs: 60, LookbackSecs: 300,
		Query: "SELECT count() FROM logs WHERE status >= 500", ConditionJSON: `{"field":"status"}`,
		Labels:      map[string]string{"alertname": "High error rate", "team": "payments"},
		Annotations: map[string]string{"runbook": "https://runbooks.example.com/5xx", "resolved_at": resolved.Format(time.RFC3339Nano)},
		TriggeredAt: triggered, ResolvedAt: resolved,
		GeneratorURL: "https://logchef.example.com/logs/alerts/42?source=7",
		Message:      "alert High error rate resolved with value 412.5000",
		Webhooks:     testWebhooks(url),
	}
}
