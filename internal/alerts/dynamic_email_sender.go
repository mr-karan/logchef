package alerts

import (
	"context"
	"log/slog"
	"time"
)

type SettingsReader interface {
	GetSettingWithDefault(ctx context.Context, key, defaultValue string) string
	GetIntSetting(ctx context.Context, key string, defaultValue int) int
	GetBoolSetting(ctx context.Context, key string, defaultValue bool) bool
	GetDurationSetting(ctx context.Context, key string, defaultValue time.Duration) time.Duration
}

type DynamicEmailSender struct {
	settings SettingsReader
	logger   *slog.Logger
}

func NewDynamicEmailSender(settings SettingsReader, logger *slog.Logger) *DynamicEmailSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &DynamicEmailSender{
		settings: settings,
		logger:   logger.With("component", "dynamic_email_sender"),
	}
}

func (d *DynamicEmailSender) Send(ctx context.Context, notification AlertNotification) error {
	opts := EmailSenderOptions{
		Host:          d.settings.GetSettingWithDefault(ctx, "alerts.smtp_host", ""),
		Port:          d.settings.GetIntSetting(ctx, "alerts.smtp_port", 587),
		Username:      d.settings.GetSettingWithDefault(ctx, "alerts.smtp_username", ""),
		Password:      d.settings.GetSettingWithDefault(ctx, "alerts.smtp_password", ""),
		From:          d.settings.GetSettingWithDefault(ctx, "alerts.smtp_from", ""),
		ReplyTo:       d.settings.GetSettingWithDefault(ctx, "alerts.smtp_reply_to", ""),
		Security:      d.settings.GetSettingWithDefault(ctx, "alerts.smtp_security", "starttls"),
		Timeout:       d.settings.GetDurationSetting(ctx, "alerts.request_timeout", 5*time.Second),
		SkipTLSVerify: d.settings.GetBoolSetting(ctx, "alerts.tls_insecure_skip_verify", false),
		Logger:        d.logger,
	}
	sender := NewEmailSender(opts)
	return sender.Send(ctx, notification)
}
