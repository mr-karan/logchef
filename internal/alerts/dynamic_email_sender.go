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
	return NewEmailSender(EmailSenderOptionsFromSettings(ctx, d.settings, d.logger)).Send(ctx, notification)
}

// EmailSenderOptionsFromSettings reads the instance SMTP settings, which an
// admin can change at any time, so a sender reads them for each send.
func EmailSenderOptionsFromSettings(ctx context.Context, settings store.SettingGetter, logger *slog.Logger) EmailSenderOptions {
	return EmailSenderOptions{
		Host:          store.GetSettingWithDefault(ctx, settings, "alerts.smtp_host", ""),
		Port:          store.GetIntSetting(ctx, settings, "alerts.smtp_port", 587),
		Username:      store.GetSettingWithDefault(ctx, settings, "alerts.smtp_username", ""),
		Password:      store.GetSettingWithDefault(ctx, settings, "alerts.smtp_password", ""),
		From:          store.GetSettingWithDefault(ctx, settings, "alerts.smtp_from", ""),
		ReplyTo:       store.GetSettingWithDefault(ctx, settings, "alerts.smtp_reply_to", ""),
		Security:      store.GetSettingWithDefault(ctx, settings, "alerts.smtp_security", "starttls"),
		Timeout:       store.GetDurationSetting(ctx, settings, "alerts.request_timeout", smtpDialTimeout),
		SkipTLSVerify: store.GetBoolSetting(ctx, settings, "alerts.tls_insecure_skip_verify", false),
		Logger:        logger,
	}
}
