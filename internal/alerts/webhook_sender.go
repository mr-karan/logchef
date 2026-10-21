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

	"log/slog"

	"github.com/mr-karan/logchef/internal/config"
)

type WebhookSenderOptions struct {
	Timeout       time.Duration
	SkipTLSVerify bool
	Logger        *slog.Logger
	AllowedHosts  config.WebhookAllowlist
}

const maxWebhookResponseBytes = 4096

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
						return fmt.Errorf("%w: %s", errWebhookDestinationDenied, ip)
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

func (s *WebhookSender) Send(ctx context.Context, notification AlertNotification) error {
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
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	var errs []string
	for index, destination := range notification.WebhookURLs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, destination, bytes.NewReader(body))
		if err != nil {
			errs = append(errs, fmt.Sprintf("webhook %d: invalid URL", index+1))
			continue
		}
		if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
			errs = append(errs, fmt.Sprintf("webhook %d: only http and https URLs are allowed", index+1))
			continue
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := s.client.Do(request)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return context.Canceled
			}
			errs = append(errs, fmt.Sprintf("webhook %d: %v", index+1, sanitizeWebhookTransportError(err)))
			continue
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxWebhookResponseBytes+1))
		_ = response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			if readErr != nil {
				errs = append(errs, fmt.Sprintf("webhook %d: status %d (body read error: %v)", index+1, response.StatusCode, sanitizeWebhookTransportError(readErr)))
				continue
			}
			truncated := len(responseBody) > maxWebhookResponseBytes
			if truncated {
				responseBody = responseBody[:maxWebhookResponseBytes]
			}
			trimmed := strings.TrimSpace(string(responseBody))
			if truncated {
				trimmed += " [truncated]"
			}
			detail := fmt.Sprintf("webhook %d: status %d", index+1, response.StatusCode)
			if trimmed != "" {
				detail += fmt.Sprintf(" (%s)", trimmed)
			}
			errs = append(errs, detail)
		}
	}
	if len(errs) > 0 {
		return boundedDeliveryError(fmt.Errorf("webhook delivery failed: %s", strings.Join(errs, "; ")))
	}
	return nil
}

var errWebhookDestinationDenied = errors.New("webhook destination is not allowed")

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
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("::/96"),
}

var translatedWebhookPrefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

func webhookAddressDenied(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	for _, prefix := range translatedWebhookPrefixes {
		if prefix.Contains(ip) {
			addressBytes := ip.As16()
			ip = netip.AddrFrom4([4]byte{addressBytes[12], addressBytes[13], addressBytes[14], addressBytes[15]})
			break
		}
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
		return context.DeadlineExceeded
	}
	if networkErr, ok := errors.AsType[net.Error](err); ok && networkErr.Timeout() {
		return errors.New("request timed out")
	}
	return errors.New("HTTP request failed")
}
