package alerts

import (
	"context"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/store"
)

type DynamicWebhookSender struct {
	settings store.SettingGetter
	logger   *slog.Logger
}

func NewDynamicWebhookSender(settings store.SettingGetter, logger *slog.Logger) *DynamicWebhookSender {
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
		Timeout:       store.GetDurationSetting(ctx, d.settings, "alerts.request_timeout", 5*time.Second),
		SkipTLSVerify: store.GetBoolSetting(ctx, d.settings, "alerts.tls_insecure_skip_verify", false),
		Logger:        d.logger,
	}
	sender := NewWebhookSender(opts)
	return sender.Send(ctx, notification)
}
