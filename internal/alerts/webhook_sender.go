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
	"net/url"
	"strings"
	"syscall"
	"time"

	"log/slog"
)

type WebhookSenderOptions struct {
	Timeout       time.Duration
	SkipTLSVerify bool
	Logger        *slog.Logger
	AllowedHosts  []string
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
	allowedHosts := make(map[string]bool, len(opts.AllowedHosts))
	for _, host := range opts.AllowedHosts {
		allowedHosts[strings.ToLower(strings.TrimSuffix(host, "."))] = true
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
			if !allowedHosts[strings.ToLower(strings.TrimSuffix(host, "."))] {
				// ControlContext checks the resolved IP used by this socket, preventing DNS rebinding.
				dialer.ControlContext = func(_ context.Context, _, address string, _ syscall.RawConn) error {
					target, err := netip.ParseAddrPort(address)
					if err != nil {
						return err
					}
					ip := target.Addr().Unmap()
					if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
						return fmt.Errorf("webhook destination %s is not allowed", ip)
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
			if urlErr, ok := errors.AsType[*url.Error](err); ok {
				err = urlErr.Err
			}
			errs = append(errs, fmt.Sprintf("webhook %d: %v", index+1, err))
			continue
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxWebhookResponseBytes+1))
		_ = response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			if readErr != nil {
				errs = append(errs, fmt.Sprintf("webhook %d: status %d (body read error: %v)", index+1, response.StatusCode, readErr))
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
		return fmt.Errorf("webhook delivery failed: %s", strings.Join(errs, "; "))
	}
	return nil
}
