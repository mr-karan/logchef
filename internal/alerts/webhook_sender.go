package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"log/slog"

	"github.com/mr-karan/logchef/internal/config"
)

type WebhookSenderOptions struct {
	Timeout       time.Duration
	SkipTLSVerify bool
	Logger        *slog.Logger
	AllowedHosts  config.WebhookAllowlist
}

const maxWebhookResponseBytes = 256

type WebhookSender struct {
	client *http.Client
	logger *slog.Logger
}

type webhookPayload struct {
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
		logger: logger.With("component", "alert_webhook_sender"),
	}
}

func (s *WebhookSender) Send(ctx context.Context, notification AlertNotification) DeliveryResults {
	if len(notification.WebhookURLs) == 0 {
		return nil
	}
	payload := webhookPayload{
		AlertID:           int64(notification.AlertID),
		AlertName:         notification.AlertName,
		Description:       notification.Description,
		Status:            string(notification.Status),
		Severity:          string(notification.Severity),
		SourceID:          int(notification.SourceID),
		SourceName:        notification.SourceName,
		Value:             notification.Value,
		ThresholdOperator: string(notification.ThresholdOp),
		ThresholdValue:    notification.ThresholdValue,
		FrequencySeconds:  notification.FrequencySecs,
		LookbackSeconds:   notification.LookbackSecs,
		Query:             notification.Query,
		ConditionJSON:     notification.ConditionJSON,
		Labels:            notification.Labels,
		Annotations:       notification.Annotations,
		TriggeredAt:       notification.TriggeredAt,
		ResolvedAt:        notification.ResolvedAt,
		GeneratorURL:      notification.GeneratorURL,
		Message:           notification.Message,
	}
	body, marshalErr := json.Marshal(payload)
	var results DeliveryResults
	for index, destination := range notification.WebhookURLs {
		target := deliveryTargetID("webhook", destination)
		if !notification.selects(target) {
			continue
		}
		err := marshalErr
		if err == nil {
			err = s.sendWebhook(ctx, notification.AlertName, index, destination, body)
		}
		if err != nil {
			err = fmt.Errorf("webhook delivery failed: %w", err)
		}
		results = append(results, DeliveryResult{Target: target, Err: err})
	}
	return results
}

func (s *WebhookSender) sendWebhook(ctx context.Context, alertName string, index int, destination string, body []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, destination, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook %d: invalid URL", index+1)
	}
	if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return fmt.Errorf("webhook %d: only http and https URLs are allowed", index+1)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if denied, ok := errors.AsType[*webhookDestinationDeniedError](err); ok {
			s.logger.Warn("webhook destination blocked by policy", "rule", alertName, "resolved_ip", denied.ip.String())
		}
		return fmt.Errorf("webhook %d: %w", index+1, sanitizeWebhookTransportError(err))
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxWebhookResponseBytes+1))
	_ = response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if readErr != nil {
			return fmt.Errorf("webhook %d: status %d (body read error: %w)", index+1, response.StatusCode, sanitizeWebhookTransportError(readErr))
		}
		excerpt := strings.TrimSpace(strings.ToValidUTF8(string(responseBody), "?"))
		if len(responseBody) > maxWebhookResponseBytes {
			const marker = " [truncated]"
			limit := min(len(excerpt), maxWebhookResponseBytes-len(marker))
			for !utf8.ValidString(excerpt[:limit]) {
				limit--
			}
			excerpt = excerpt[:limit] + marker
		}
		s.logger.Warn("webhook delivery failed", "webhook", index+1, "status", response.StatusCode, "response_excerpt", excerpt)
		detail := fmt.Sprintf("webhook %d: status %d", index+1, response.StatusCode)
		return errors.New(detail)
	}
	return nil
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
