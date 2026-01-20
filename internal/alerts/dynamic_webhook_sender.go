package alerts

import (
	"context"
	"log/slog"
	"time"
)

type DynamicWebhookSender struct {
	settings SettingsReader
	logger   *slog.Logger
}

func NewDynamicWebhookSender(settings SettingsReader, logger *slog.Logger) *DynamicWebhookSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &DynamicWebhookSender{
		settings: settings,
		logger:   logger.With("component", "dynamic_webhook_sender"),
	}
}

func (d *DynamicWebhookSender) Send(ctx context.Context, notification AlertNotification) error {
	opts := WebhookSenderOptions{
		Timeout:       d.settings.GetDurationSetting(ctx, "alerts.request_timeout", 5*time.Second),
		SkipTLSVerify: d.settings.GetBoolSetting(ctx, "alerts.tls_insecure_skip_verify", false),
		Logger:        d.logger,
	}
	sender := NewWebhookSender(opts)
	return sender.Send(ctx, notification)
}
