package alerts

import (
	"context"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/store"
)

type DynamicWebhookSender struct {
	settings store.SettingsReader
	logger   *slog.Logger
}

func NewDynamicWebhookSender(settings store.SettingGetter, logger *slog.Logger) *DynamicWebhookSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &DynamicWebhookSender{
		settings: store.SettingsReader{SettingGetter: settings},
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
