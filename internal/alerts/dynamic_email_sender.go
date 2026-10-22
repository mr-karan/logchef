package alerts

import (
	"context"
	"log/slog"

	"github.com/mr-karan/logchef/internal/store"
)

type DynamicEmailSender struct {
	settings store.SettingGetter
	logger   *slog.Logger
}

func NewDynamicEmailSender(settings store.SettingGetter, logger *slog.Logger) *DynamicEmailSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &DynamicEmailSender{
		settings: settings,
		logger:   logger.With("component", "dynamic_email_sender"),
	}
}

func (d *DynamicEmailSender) Send(ctx context.Context, notification AlertNotification) DeliveryResults {
	opts := EmailSenderOptions{
		Host:          store.GetSettingWithDefault(ctx, d.settings, "alerts.smtp_host", ""),
		Port:          store.GetIntSetting(ctx, d.settings, "alerts.smtp_port", 587),
		Username:      store.GetSettingWithDefault(ctx, d.settings, "alerts.smtp_username", ""),
		Password:      store.GetSettingWithDefault(ctx, d.settings, "alerts.smtp_password", ""),
		From:          store.GetSettingWithDefault(ctx, d.settings, "alerts.smtp_from", ""),
		ReplyTo:       store.GetSettingWithDefault(ctx, d.settings, "alerts.smtp_reply_to", ""),
		Security:      store.GetSettingWithDefault(ctx, d.settings, "alerts.smtp_security", "starttls"),
		Timeout:       store.GetDurationSetting(ctx, d.settings, "alerts.request_timeout", smtpDialTimeout),
		SkipTLSVerify: store.GetBoolSetting(ctx, d.settings, "alerts.tls_insecure_skip_verify", false),
		Logger:        d.logger,
	}
	sender := NewEmailSender(opts)
	return sender.Send(ctx, notification)
}
