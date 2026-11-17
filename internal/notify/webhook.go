package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

type WebhookSenderOptions struct {
	Timeout       time.Duration
	SkipTLSVerify bool
	Logger        *slog.Logger
	AllowedHosts  config.WebhookAllowlist
}

const maxWebhookResponseBytes = 256

// maxRetryAfter caps a receiver's Retry-After.
const maxRetryAfter = 10 * time.Minute

// WebhookSender posts a message's version 1 payload to a webhook
// destination's URL.
type WebhookSender struct {
	client *http.Client
	logger *slog.Logger
}

// AlertPayload is the version 1 webhook payload: the 20 fields a rule's
// webhook received before destinations existed, with the same names and
// types. It must not change.
type AlertPayload struct {
	AlertID           int64             `json:"alert_id"`
	AlertName         string            `json:"alert_name"`
	Description       string            `json:"description,omitempty"`
	Status            string            `json:"status"`
	Severity          string            `json:"severity"`
	SourceID          int               `json:"source_id"`
	SourceName        string            `json:"source_name,omitempty"`
	Value             float64           `json:"value"`
	ThresholdOperator string            `json:"threshold_operator"`
	ThresholdValue    float64           `json:"threshold_value"`
	FrequencySeconds  int               `json:"frequency_seconds"`
	LookbackSeconds   int               `json:"lookback_seconds,omitempty"`
	Query             string            `json:"query,omitempty"`
	ConditionJSON     string            `json:"condition_json,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	TriggeredAt       time.Time         `json:"triggered_at"`
	ResolvedAt        *time.Time        `json:"resolved_at,omitempty"`
	GeneratorURL      string            `json:"generator_url,omitempty"`
	Message           string            `json:"message,omitempty"`
}

func NewWebhookSender(opts WebhookSenderOptions) *WebhookSender {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	transport := &http.Transport{
		TLSClientConfig:        &tls.Config{InsecureSkipVerify: opts.SkipTLSVerify}, // #nosec G402
		IdleConnTimeout:        30 * time.Second,
		MaxIdleConns:           20,
		MaxIdleConnsPerHost:    2,
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 16 * 1024,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
			if !opts.AllowedHosts.Contains(host) {
				// ControlContext checks the resolved IP used by this socket, preventing DNS rebinding.
				dialer.ControlContext = func(_ context.Context, _, address string, _ syscall.RawConn) error {
					target, err := netip.ParseAddrPort(address)
					if err != nil {
						return err
					}
					ip := target.Addr()
					if webhookAddressDenied(ip) {
						return &webhookDestinationDeniedError{ip: ip}
					}
					return nil
				}
			}
			return dialer.DialContext(ctx, network, address)
		},
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookSender{
		client: &http.Client{
			Timeout: timeout, Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
		logger: logger.With("component", "webhook_sender"),
	}
}

// Send posts msg's version 1 payload to dest's URL. A deliveryID above zero
// goes in the Logchef-Delivery-Id header, so a receiver can drop a repeat.
func (s *WebhookSender) Send(ctx context.Context, dest *models.NotificationDestination, _ string, deliveryID int64, msg Message) error {
	if msg.Alert == nil {
		return Permanent(errors.New("webhook delivery failed: the message has no webhook payload"))
	}
	body, err := json.Marshal(msg.Alert)
	if err != nil {
		return Permanent(fmt.Errorf("webhook delivery failed: %w", err))
	}
	answer, err := s.post(ctx, dest.Name, dest.Secret, deliveryID, body)
	if err == nil && !answer.ok() {
		s.logger.Warn("webhook delivery failed", "destination", dest.Name, "status", answer.status, "response_excerpt", answer.body)
		err = answer.err("")
	}
	if err != nil {
		return wrapClassified("webhook delivery failed", err)
	}
	return nil
}

// httpPoster is the outbound HTTP client that the webhook, Slack and
// PagerDuty senders share, with its address policy, timeout and body cap.
type httpPoster interface {
	post(ctx context.Context, name, target string, deliveryID int64, body []byte) (*httpAnswer, error)
}

// httpAnswer is a receiver's answer: its status, its headers and an excerpt
// of its body. bodyLost is set, and body empty, when the connection failed
// while the body was read.
type httpAnswer struct {
	status   int
	header   http.Header
	body     string
	bodyLost bool
}

func (a *httpAnswer) ok() bool {
	return a.status >= http.StatusOK && a.status < http.StatusMultipleChoices
}

// err classifies an answer that is not a success: 408 and 5xx are
// transient, 429 is transient after its Retry-After, any other status is
// permanent. A detail follows the status in the error's text.
func (a *httpAnswer) err(detail string) error {
	statusErr := fmt.Errorf("status %d", a.status)
	if detail != "" {
		statusErr = fmt.Errorf("status %d: %s", a.status, detail)
	}
	switch code := a.status; {
	case code == http.StatusRequestTimeout || code >= 500:
		return statusErr
	case code == http.StatusTooManyRequests:
		if after, ok := retryAfter(a.header.Get("Retry-After"), time.Now()); ok {
			return RetryAfter(statusErr, after)
		}
		return statusErr
	default:
		return Permanent(statusErr)
	}
}

// post sends body to target through the shared client and returns the
// receiver's answer. The error is a transport failure, classified and
// stripped of the URL.
func (s *WebhookSender) post(ctx context.Context, name, target string, deliveryID int64, body []byte) (*httpAnswer, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, Permanent(errors.New("invalid URL"))
	}
	if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return nil, Permanent(errors.New("only http and https URLs are allowed"))
	}
	request.Header.Set("Content-Type", "application/json")
	if deliveryID > 0 {
		request.Header.Set("Logchef-Delivery-Id", strconv.FormatInt(deliveryID, 10))
	}
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		if denied, ok := errors.AsType[*webhookDestinationDeniedError](err); ok {
			s.logger.Warn("webhook destination blocked by policy", "destination", name, "resolved_ip", denied.ip.String())
			return nil, Permanent(errWebhookDestinationDenied)
		}
		return nil, sanitizeWebhookTransportError(err)
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxWebhookResponseBytes+1))
	_ = response.Body.Close()
	answer := &httpAnswer{status: response.StatusCode, header: response.Header, bodyLost: readErr != nil}
	if readErr == nil {
		answer.body = responseExcerpt(responseBody)
	}
	return answer, nil
}

// responseExcerpt is a body read up to one byte past the cap, as trimmed
// valid UTF-8 within the cap.
func responseExcerpt(body []byte) string {
	excerpt := strings.TrimSpace(strings.ToValidUTF8(string(body), "?"))
	if len(body) > maxWebhookResponseBytes {
		const marker = " [truncated]"
		limit := min(len(excerpt), maxWebhookResponseBytes-len(marker))
		for !utf8.ValidString(excerpt[:limit]) {
			limit--
		}
		excerpt = excerpt[:limit] + marker
	}
	return excerpt
}

// retryAfter reads a Retry-After header in seconds or as an HTTP date, capped
// at maxRetryAfter.
func retryAfter(header string, now time.Time) (time.Duration, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, false
	}
	var after time.Duration
	if seconds, err := strconv.Atoi(header); err == nil {
		after = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(header); err == nil {
		after = at.Sub(now)
	} else {
		return 0, false
	}
	return min(max(after, 0), maxRetryAfter), true
}

// DynamicWebhookSender reads the request timeout and TLS settings at each
// send, so a settings change applies without a restart. It keeps one client
// while the settings stay the same, so connections are reused.
type DynamicWebhookSender struct {
	settings             store.SettingGetter
	logger               *slog.Logger
	allowedHosts         config.WebhookAllowlist
	defaultTimeout       time.Duration
	defaultSkipTLSVerify bool
	mu                   sync.Mutex
	sender               *WebhookSender
	timeout              time.Duration
	skipTLSVerify        bool
}

func NewDynamicWebhookSender(settings store.SettingGetter, opts WebhookSenderOptions) *DynamicWebhookSender {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	return &DynamicWebhookSender{
		settings:             settings,
		defaultTimeout:       opts.Timeout,
		defaultSkipTLSVerify: opts.SkipTLSVerify,
		allowedHosts:         opts.AllowedHosts,
		logger:               logger,
	}
}

func (d *DynamicWebhookSender) Send(ctx context.Context, dest *models.NotificationDestination, recipient string, deliveryID int64, msg Message) error {
	return d.current(ctx).Send(ctx, dest, recipient, deliveryID, msg)
}

func (d *DynamicWebhookSender) post(ctx context.Context, name, target string, deliveryID int64, body []byte) (*httpAnswer, error) {
	return d.current(ctx).post(ctx, name, target, deliveryID, body)
}

func (d *DynamicWebhookSender) current(ctx context.Context) *WebhookSender {
	opts := WebhookSenderOptions{
		Timeout:       store.GetDurationSetting(ctx, d.settings, "alerts.request_timeout", d.defaultTimeout),
		SkipTLSVerify: store.GetBoolSetting(ctx, d.settings, "alerts.tls_insecure_skip_verify", d.defaultSkipTLSVerify),
		Logger:        d.logger,
		AllowedHosts:  d.allowedHosts,
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sender == nil || d.timeout != opts.Timeout || d.skipTLSVerify != opts.SkipTLSVerify {
		if d.sender != nil {
			d.sender.client.CloseIdleConnections()
		}
		d.sender = NewWebhookSender(opts)
		d.timeout, d.skipTLSVerify = opts.Timeout, opts.SkipTLSVerify
	}
	return d.sender
}

var errWebhookDestinationDenied = errors.New("webhook destination is not allowed")

type webhookDestinationDeniedError struct {
	ip netip.Addr
}

func (e *webhookDestinationDeniedError) Error() string {
	return errWebhookDestinationDenied.Error()
}

func (e *webhookDestinationDeniedError) Unwrap() error {
	return errWebhookDestinationDenied
}

var deniedWebhookPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("::ffff:0:0:0/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

var translatedWebhookPrefix = netip.MustParsePrefix("64:ff9b::/96")

func webhookAddressDenied(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	if translatedWebhookPrefix.Contains(ip) {
		addressBytes := ip.As16()
		ip = netip.AddrFrom4([4]byte{addressBytes[12], addressBytes[13], addressBytes[14], addressBytes[15]})
	}
	if !ip.IsGlobalUnicast() {
		return true
	}
	for _, prefix := range deniedWebhookPrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func sanitizeWebhookTransportError(err error) error {
	if errors.Is(err, errWebhookDestinationDenied) {
		return errWebhookDestinationDenied
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("request timed out")
	}
	if networkErr, ok := errors.AsType[net.Error](err); ok && networkErr.Timeout() {
		return errors.New("request timed out")
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return errors.New("DNS lookup failed")
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return errors.New("connection refused")
	}
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return errors.New("TLS verification failed")
	}
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return errors.New("TLS handshake failed")
	}
	return errors.New("HTTP request failed")
}
