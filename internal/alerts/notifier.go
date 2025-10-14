package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// NotificationPayload captures the essential information passed to notifier implementations.
type NotificationPayload struct {
	Value   float64
	Message string
}

// Notifier defines the contract for sending alert notifications.
type Notifier interface {
	Notify(ctx context.Context, alert *models.Alert, channel models.AlertChannel, payload NotificationPayload) error
}

// DefaultNotifier implements a simple multi-channel notifier using logging for email/slack
// and HTTP POST requests for webhooks.
type DefaultNotifier struct {
	log        *slog.Logger
	httpClient *http.Client
}

// NewDefaultNotifier returns a notifier that uses best-effort delivery for each channel type.
func NewDefaultNotifier(log *slog.Logger) *DefaultNotifier {
	client := &http.Client{Timeout: 5 * time.Second}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &DefaultNotifier{log: log.With("component", "alert_notifier"), httpClient: client}
}

// Notify dispatches the alert to the configured target.
func (n *DefaultNotifier) Notify(ctx context.Context, alert *models.Alert, channel models.AlertChannel, payload NotificationPayload) error {
	switch channel.Type {
	case models.AlertChannelEmail:
		n.log.Info("email notification", "alert_id", alert.ID, "target", channel.Target, "message", payload.Message, "value", payload.Value)
		return nil
	case models.AlertChannelSlack:
		n.log.Info("slack notification", "alert_id", alert.ID, "target", channel.Target, "message", payload.Message, "value", payload.Value)
		return nil
	case models.AlertChannelWebhook:
		body := map[string]any{
			"alert_id": alert.ID,
			"name":     alert.Name,
			"severity": alert.Severity,
			"value":    payload.Value,
			"message":  payload.Message,
		}
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal webhook payload: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.Target, bytes.NewReader(buf))
		if err != nil {
			return fmt.Errorf("failed to create webhook request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := n.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("webhook request failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("webhook returned status %d", resp.StatusCode)
		}
		return nil
	default:
		return fmt.Errorf("unsupported channel type %q", channel.Type)
	}
}
