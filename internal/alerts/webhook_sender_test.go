package alerts

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookRefusesLoopbackByDefault(t *testing.T) {
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer receiver.Close()
	sender := NewWebhookSender(WebhookSenderOptions{})
	defer sender.client.CloseIdleConnections()
	err := sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{receiver.URL}})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("delivery error = %v; want destination not allowed", err)
	}
	if calls.Load() != 0 {
		t.Errorf("receiver handled %d requests; want none", calls.Load())
	}
}

func TestWebhookResponseBodyBoundedInHistory(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, strings.Repeat("x", 64*1024)+"private-response-tail")
	}))
	defer receiver.Close()
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: []string{"127.0.0.1"}})
	defer sender.client.CloseIdleConnections()
	alert.WebhookURLs = []string{receiver.URL}
	manager := NewManager(Options{DB: db, Logger: logger, Sender: sender})
	if err := manager.handleTriggered(t.Context(), alert, 12); err != nil {
		t.Fatal(err)
	}
	history, err := db.ListAlertHistory(t.Context(), alert.ID, 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %v, %v", history, err)
	}
	deliveryError, ok := history[0].Payload["delivery_error"].(string)
	if !ok {
		t.Fatalf("missing delivery error in %v", history[0].Payload)
	}
	if strings.Count(deliveryError, "x") != 4096 || !strings.HasSuffix(deliveryError, " [truncated])") || strings.Contains(deliveryError, "private-response-tail") {
		t.Fatalf("history did not retain exactly 4096 response bytes with a truncation marker, error length %d", len(deliveryError))
	}
	if !strings.Contains(deliveryError, "status 502") {
		t.Errorf("missing HTTP status in %q", deliveryError)
	}
}

func TestDynamicWebhookReusesConnections(t *testing.T) {
	db, _, logger := newManagerTestAlert(t)
	var connections atomic.Int32
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	receiver.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	receiver.Start()
	defer receiver.Close()
	sender := NewDynamicWebhookSender(db, logger, "127.0.0.1")
	for range 3 {
		if err := sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{receiver.URL}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := connections.Load(); got != 1 {
		t.Errorf("opened %d connections for three sends; want one", got)
	}
}

func TestWebhookRejectsPrivateDestinations(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "0.0.0.0", "10.1.2.3", "172.16.1.1", "192.168.1.1", "169.254.169.254", "[::1]", "[::]", "[::ffff:127.0.0.1]", "[fc00::1]", "[fe80::1]"} {
		t.Run(host, func(t *testing.T) {
			sender := NewWebhookSender(WebhookSenderOptions{Timeout: time.Second})
			defer sender.client.CloseIdleConnections()
			err := sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{"http://" + host + ":8888"}})
			if err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("delivery error = %v; want destination not allowed", err)
			}
		})
	}
}

func TestWebhookAllowsListedHost(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid webhook request")
		}
		var payload webhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.AlertName != "errors" || payload.Value != 12 {
			t.Errorf("unexpected payload %+v", payload)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	parsed, err := url.Parse(receiver.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "localhost"} {
		t.Run(host, func(t *testing.T) {
			sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: []string{strings.ToUpper(host) + "."}})
			defer sender.client.CloseIdleConnections()
			destination := "http://" + net.JoinHostPort(host, parsed.Port())
			if err := sender.Send(t.Context(), AlertNotification{AlertName: "errors", Value: 12, WebhookURLs: []string{destination}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWebhookDoesNotFollowRedirects(t *testing.T) {
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer receiver.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, receiver.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: []string{"127.0.0.1"}})
	defer sender.client.CloseIdleConnections()
	err := sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{redirector.URL}})
	if err == nil || !strings.Contains(err.Error(), "status 307") {
		t.Errorf("redirect error = %v; want status 307", err)
	}
	if calls.Load() != 0 {
		t.Errorf("redirect target handled %d requests; want none", calls.Load())
	}
}

func TestWebhookTimeout(t *testing.T) {
	release := make(chan struct{})
	receiver := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer receiver.Close()
	defer close(release)
	sender := NewWebhookSender(WebhookSenderOptions{Timeout: 30 * time.Millisecond, AllowedHosts: []string{"127.0.0.1"}})
	defer sender.client.CloseIdleConnections()
	done := make(chan error, 1)
	go func() { done <- sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{receiver.URL}}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("delivery succeeded despite receiver timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("webhook delivery exceeded one second")
	}
}

func TestDynamicWebhookConcurrentSendsAndSettingsChange(t *testing.T) {
	db, _, logger := newManagerTestAlert(t)
	var calls atomic.Int32
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer receiver.Close()
	sender := NewDynamicWebhookSender(db, logger, "127.0.0.1")
	notification := AlertNotification{WebhookURLs: []string{receiver.URL}}
	if err := sender.Send(t.Context(), notification); err == nil {
		t.Fatal("accepted untrusted TLS certificate")
	}
	if err := db.UpsertSetting(t.Context(), "alerts.tls_insecure_skip_verify", "true", "boolean", "alerts", "", false); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if err := sender.Send(t.Context(), notification); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if calls.Load() != 8 {
		t.Errorf("delivered %d requests; want 8", calls.Load())
	}
}

func TestWebhookErrorOmitsDestinationURL(t *testing.T) {
	sender := NewWebhookSender(WebhookSenderOptions{})
	defer sender.client.CloseIdleConnections()
	err := sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{"http://127.0.0.1:8888/receiver?token=example-token"}})
	if err == nil {
		t.Fatal("accepted loopback destination")
	}
	if strings.Contains(err.Error(), "example-token") || strings.Contains(err.Error(), "/receiver") {
		t.Fatalf("error includes webhook URL: %v", err)
	}
}

func TestWebhookDoesNotRetainStatusReason(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 500 " + strings.Repeat("x", 8192) + "\r\nContent-Length: 0\r\n\r\n")
		_ = buffer.Flush()
	}))
	defer receiver.Close()
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: []string{"127.0.0.1"}})
	defer sender.client.CloseIdleConnections()
	err := sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{receiver.URL}})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("error = %v; want HTTP status", err)
	}
	if len(err.Error()) > 100 {
		t.Fatalf("error retained %d-byte status reason", len(err.Error()))
	}
}
