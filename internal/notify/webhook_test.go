package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

var testMessage = Message{Event: EventFiring, Alert: &AlertPayload{AlertName: "errors", Value: 12}}

// hook is a webhook destination that posts to url.
func hook(target string) *models.NotificationDestination {
	return &models.NotificationDestination{ID: 1, Name: "hook", Type: models.DestinationTypeWebhook, Config: models.DestinationConfig{PayloadVersion: WebhookPayloadV1}, Secret: target, IsActive: true}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestWebhookRefusesLoopbackByDefault(t *testing.T) {
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer receiver.Close()
	sender := NewWebhookSender(WebhookSenderOptions{})
	defer sender.client.CloseIdleConnections()
	err := sender.Send(t.Context(), hook(receiver.URL), "", 1, testMessage)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("delivery error = %v; want destination not allowed", err)
	}
	if calls.Load() != 0 {
		t.Errorf("receiver handled %d requests; want none", calls.Load())
	}
}

func TestDynamicWebhookReusesConnections(t *testing.T) {
	db, logger := openStore(t), discardLogger()
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
		if err := sender.Send(t.Context(), hook(receiver.URL), "", 1, testMessage); err != nil {
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
			err := sender.Send(t.Context(), hook("http://"+host+":8888"), "", 1, testMessage)
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
		var payload AlertPayload
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
			if err := sender.Send(t.Context(), hook(destination), "", 1, testMessage); err != nil {
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
	err := sender.Send(t.Context(), hook(redirector.URL), "", 1, testMessage)
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
	go func() {
		done <- sender.Send(t.Context(), hook(receiver.URL), "", 1, testMessage)
	}()
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
	db, logger := openStore(t), discardLogger()
	var calls atomic.Int32
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer receiver.Close()
	sender := NewDynamicWebhookSender(db, WebhookSenderOptions{Logger: logger, AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
	if err := sender.Send(t.Context(), hook(receiver.URL), "", 1, testMessage); err == nil {
		t.Fatal("accepted untrusted TLS certificate")
	}
	if err := db.UpsertSetting(t.Context(), "alerts.tls_insecure_skip_verify", "true", "boolean", "alerts", "", false); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if err := sender.Send(t.Context(), hook(receiver.URL), "", 1, testMessage); err != nil {
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
	err := sender.Send(t.Context(), hook("http://127.0.0.1:8888/receiver?token=example-token"), "", 1, testMessage)
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
	err := sender.Send(t.Context(), hook(receiver.URL), "", 1, testMessage)
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("error = %v; want HTTP status", err)
	}
	if len(err.Error()) > 100 {
		t.Fatalf("error retained %d-byte status reason", len(err.Error()))
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
		err := sender.Send(t.Context(), hook(destination), "", 1, testMessage)
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
		{"2001:4860:4860::8888", false}, {"64:ff9b::808:808", false}, {"64:ff9b:1::808:808", true},
	} {
		t.Run(tc.address, func(t *testing.T) {
			if got := webhookAddressDenied(netip.MustParseAddr(tc.address)); got != tc.denied {
				t.Errorf("address denied = %t, want %t", got, tc.denied)
			}
		})
	}
}

func TestWebhookLocalNAT64Policy(t *testing.T) {
	for _, address := range []string{"64:ff9b:1:0:a:0:100:0", "64:ff9b:1:0:7f:0:100:0", "64:ff9b:1:0:64:4000:100:0", "64:ff9b:1::808:808"} {
		t.Run(address, func(t *testing.T) {
			if !webhookAddressDenied(netip.MustParseAddr(address)) {
				t.Fatal("local-use NAT64 address accepted")
			}
			sender := NewWebhookSender(WebhookSenderOptions{Timeout: 50 * time.Millisecond, AllowedHosts: webhookTestAllowlist(t, address)})
			defer sender.client.CloseIdleConnections()
			err := sender.Send(t.Context(), hook("http://["+address+"]/"), "", 1, testMessage)
			if err != nil && strings.Contains(err.Error(), "not allowed") {
				t.Fatal("explicit host exception was denied")
			}
		})
	}
}

func TestWebhookTransportErrorCategories(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"DNS", &net.DNSError{Err: "private resolver detail", Name: "internal.example"}, "DNS lookup failed"},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, "connection refused"},
		{"TLS", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "TLS verification failed"},
		{"TLS record", tls.RecordHeaderError{Msg: "private TLS detail"}, "TLS handshake failed"},
		{"timeout", context.DeadlineExceeded, "request timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := NewWebhookSender(WebhookSenderOptions{})
			sender.client.Transport = webhookErrorTransport{err: tc.err}
			want := "webhook delivery failed: " + tc.want
			err := sender.Send(t.Context(), hook("https://internal.example/"), "", 1, testMessage)
			if err == nil || err.Error() != want || IsPermanent(err) {
				t.Fatalf("delivery error = %v, want transient %q", err, want)
			}
		})
	}
}

type webhookErrorTransport struct{ err error }

func (transport webhookErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.err
}

// TestWebhookNeverLogsTheResponseBody: a receiver that echoes the request
// path, where a webhook URL carries its token, reaches neither the error
// nor the log.
func TestWebhookNeverLogsTheResponseBody(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "no route for "+r.URL.String())
	}))
	defer receiver.Close()
	var logs bytes.Buffer
	sender := NewWebhookSender(WebhookSenderOptions{Logger: slog.New(slog.NewJSONHandler(&logs, nil)), AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
	defer sender.client.CloseIdleConnections()
	for _, version := range []int{WebhookPayloadV1, WebhookPayloadV2} {
		dest := hook(receiver.URL + "/hooks/leak-token?key=leak-query")
		dest.Config.PayloadVersion = version
		err := sender.Send(t.Context(), dest, "", 1, v2TestMessage())
		if err == nil || err.Error() != "webhook delivery failed: status 502" || IsPermanent(err) {
			t.Fatalf("version %d: delivery error = %v, want transient status 502 alone", version, err)
		}
	}
	if strings.Count(logs.String(), `"status":502`) != 2 {
		t.Errorf("logs = %s, want one failure line per send", logs.String())
	}
	for _, leak := range []string{"leak-token", "leak-query", "no route"} {
		if strings.Contains(logs.String(), leak) {
			t.Errorf("log leaks %q: %s", leak, logs.String())
		}
	}
}

func TestDynamicWebhookHonorsOptionDefaults(t *testing.T) {
	db, logger := openStore(t), discardLogger()
	release := make(chan struct{})
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	defer close(release)
	sender := NewDynamicWebhookSender(db, WebhookSenderOptions{Logger: logger, Timeout: 200 * time.Millisecond, SkipTLSVerify: true, AllowedHosts: webhookTestAllowlist(t, "127.0.0.1")})
	if err := sender.Send(t.Context(), hook(receiver.URL), "", 1, testMessage); err != nil {
		t.Fatalf("option TLS default ignored: %v", err)
	}
	started := time.Now()
	if err := sender.Send(t.Context(), hook(receiver.URL+"/slow"), "", 1, testMessage); err == nil {
		t.Fatal("timeout default ignored")
	}
	if time.Since(started) > time.Second {
		t.Fatal("configured timeout exceeded one second")
	}
	if err := db.UpsertSetting(t.Context(), "alerts.tls_insecure_skip_verify", "false", "boolean", "alerts", "", false); err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(t.Context(), hook(receiver.URL), "", 1, testMessage); err == nil {
		t.Fatal("saved TLS setting did not override option default")
	}
}

func TestWebhookTranslationPrefixes(t *testing.T) {
	for _, address := range []string{
		"::ffff:0:10.0.0.1",
		"::ffff:0:127.0.0.1",
		"::ffff:0:100.64.0.1",
		"::ffff:0:8.8.8.8",
		"2001:0:4136:e378:8000:63bf:f5ff:fffe",
		"2001:0:4136:e378:8000:63bf:80ff:fffe",
		"2001:0:4136:e378:8000:63bf:f7f7:f7f7",
	} {
		t.Run(address, func(t *testing.T) {
			for _, allowed := range []bool{false, true} {
				opts := WebhookSenderOptions{Timeout: 50 * time.Millisecond}
				if allowed {
					opts.AllowedHosts = webhookTestAllowlist(t, address)
				}
				sender := NewWebhookSender(opts)
				err := sender.Send(t.Context(), hook("http://["+address+"]/"), "", 1, testMessage)
				sender.client.CloseIdleConnections()
				blocked := err != nil && strings.Contains(err.Error(), "not allowed")
				if blocked == allowed {
					t.Errorf("allowlisted = %t, delivery error = %v; want blocked = %t", allowed, err, !allowed)
				}
			}
		})
	}
}
