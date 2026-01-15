package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"log/slog"
)

type WebhookSenderOptions struct {
	Timeout       time.Duration
	SkipTLSVerify bool
	Logger        *slog.Logger
}

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
	TeamID            int               `json:"team_id"`
	TeamName          string            `json:"team_name,omitempty"`
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
		TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.SkipTLSVerify}, // #nosec G402
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookSender{
		client: &http.Client{Timeout: timeout, Transport: transport},
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
		TeamID:            int(notification.TeamID),
		TeamName:          notification.TeamName,
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
	for _, url := range notification.WebhookURLs {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := s.client.Do(request)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		responseBody, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			if readErr != nil {
				errs = append(errs, fmt.Sprintf("%s: status %d (body read error: %v)", url, response.StatusCode, readErr))
				continue
			}
			trimmed := strings.TrimSpace(string(responseBody))
			if trimmed == "" {
				trimmed = response.Status
			}
			errs = append(errs, fmt.Sprintf("%s: status %d (%s)", url, response.StatusCode, trimmed))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("webhook delivery failed: %s", strings.Join(errs, "; "))
	}
	return nil
}
