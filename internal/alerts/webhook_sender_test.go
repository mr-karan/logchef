package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
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
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
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
	if len(deliveryError) > 512 || !strings.HasSuffix(deliveryError, " [truncated]") || strings.Contains(deliveryError, "private-response-tail") {
		t.Fatalf("history did not bound the response error with a truncation marker, error length %d", len(deliveryError))
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
	sender := NewDynamicWebhookSender(db, WebhookSenderOptions{Logger: logger, AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
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
	for _, host := range []string{"localhost", "127.0.0.1", "0.0.0.0", "10.1.2.3", "172.16.1.1", "192.168.1.1", "169.254.169.254", "[::1]", "[::]", "[::ffff:127.0.0.1]", "[fc00::1]", "[fe80::1]", "0.1.2.3", "100.100.100.200", "100.64.0.1", "192.0.0.1", "198.18.0.1", "240.0.0.1", "255.255.255.255", "224.0.0.1", "[ff02::1]", "[fec0::1]", "[64:ff9b::a00:1]", "[64:ff9b::a9fe:a9fe]", "[64:ff9b:1::a00:1]", "[2002:a9fe:a9fe::1]", "[::7f00:1]"} {
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
			sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, strings.ToUpper(host)+".")})
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
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
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
	sender := NewWebhookSender(WebhookSenderOptions{Timeout: 30 * time.Millisecond, AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
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
	sender := NewDynamicWebhookSender(db, WebhookSenderOptions{Logger: logger, AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
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
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
	defer sender.client.CloseIdleConnections()
	err := sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{receiver.URL}})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("error = %v; want HTTP status", err)
	}
	if len(err.Error()) > 100 {
		t.Fatalf("error retained %d-byte status reason", len(err.Error()))
	}
}

func TestWebhookMalformedStatusBoundedInHistory(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 " + strings.Repeat("x", 10000) + "\r\n\r\n")
		_ = buffer.Flush()
	}))
	defer receiver.Close()
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
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
	if !ok || len(deliveryError) > 512 || strings.Contains(deliveryError, strings.Repeat("x", 100)) {
		t.Fatalf("malformed-status delivery error retained parser text, length %d", len(deliveryError))
	}
}

func TestManagerStopRecordsInFlightDeliveryError(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	started := make(chan struct{})
	release := make(chan struct{})
	receiver := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { close(started); <-release }))
	defer receiver.Close()
	defer close(release)
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
	defer sender.client.CloseIdleConnections()
	alert.WebhookURLs = []string{receiver.URL}
	manager := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{Enabled: true, EvaluationInterval: time.Hour, HistoryLimit: 100}})
	manager.evalFn = func(ctx context.Context, _ *models.Alert) error { return manager.handleTriggered(ctx, alert, 12) }
	manager.Start(t.Context())
	defer manager.Stop()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	stopped := make(chan struct{})
	go func() { manager.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not finish")
	}
	history, err := db.ListAlertHistory(t.Context(), alert.ID, 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %v, %v", history, err)
	}
	deliveryError, ok := history[0].Payload["delivery_error"].(string)
	if !ok || !strings.Contains(deliveryError, "canceled") || history[0].Payload["delivery_failed"] != true {
		t.Fatalf("missing canceled delivery outcome in %v", history[0].Payload)
	}
}

func TestWebhookProxyEnvironmentCannotBypassGuard(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	sender := NewWebhookSender(WebhookSenderOptions{Timeout: time.Second})
	defer sender.client.CloseIdleConnections()
	transport, ok := sender.client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("webhook transport must ignore environment proxies")
	}
	for _, destination := range []string{"http://169.254.169.254/", "https://169.254.169.254/"} {
		err := sender.Send(t.Context(), AlertNotification{WebhookURLs: []string{destination}})
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("proxy delivery = %v; want blocked destination", err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("proxy received %d requests", requests.Load())
	}
}

func webhookTestAllowlist(t *testing.T, hosts ...string) config.WebhookAllowlist {
	t.Helper()
	allowlist, err := config.ParseWebhookAllowlist(hosts)
	if err != nil {
		t.Fatal(err)
	}
	return allowlist
}

func TestWebhookAddressPolicy(t *testing.T) {
	for _, tc := range []struct {
		address string
		denied  bool
	}{
		{"fe80::1%lo", true}, {"fc00::1%lo", true}, {"0.255.255.255", true}, {"10.255.255.255", true}, {"100.127.255.255", true},
		{"127.255.255.255", true}, {"169.254.255.255", true}, {"172.31.255.255", true},
		{"192.0.0.255", true}, {"192.168.255.255", true}, {"198.19.255.255", true},
		{"255.255.255.254", true}, {"fdff:ffff::1", true}, {"febf:ffff::1", true},
		{"feff:ffff::1", true}, {"64:ff9b::7f00:1", true}, {"::ffff:100.100.100.200", true},
		{"8.8.8.8", false}, {"100.128.0.1", false}, {"198.20.0.1", false},
		{"2001:4860:4860::8888", false}, {"64:ff9b::808:808", false}, {"64:ff9b:1::808:808", false},
	} {
		t.Run(tc.address, func(t *testing.T) {
			if got := webhookAddressDenied(netip.MustParseAddr(tc.address)); got != tc.denied {
				t.Errorf("address denied = %t, want %t", got, tc.denied)
			}
		})
	}
}
