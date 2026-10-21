package alerts

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/config"
)

type DynamicWebhookSender struct {
	settings      SettingsReader
	logger        *slog.Logger
	allowedHosts  config.WebhookAllowlist
	mu            sync.Mutex
	sender        *WebhookSender
	timeout       time.Duration
	skipTLSVerify bool
}

func NewDynamicWebhookSender(settings SettingsReader, opts WebhookSenderOptions) *DynamicWebhookSender {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &DynamicWebhookSender{
		settings:     settings,
		allowedHosts: opts.AllowedHosts,
		logger:       logger.With("component", "dynamic_webhook_sender"),
	}
}

func (d *DynamicWebhookSender) Send(ctx context.Context, notification AlertNotification) error {
	opts := WebhookSenderOptions{
		Timeout:       d.settings.GetDurationSetting(ctx, "alerts.request_timeout", 5*time.Second),
		SkipTLSVerify: d.settings.GetBoolSetting(ctx, "alerts.tls_insecure_skip_verify", false),
		Logger:        d.logger,
		AllowedHosts:  d.allowedHosts,
	}
	d.mu.Lock()
	if d.sender == nil || d.timeout != opts.Timeout || d.skipTLSVerify != opts.SkipTLSVerify {
		if d.sender != nil {
			d.sender.client.CloseIdleConnections()
		}
		d.sender = NewWebhookSender(opts)
		d.timeout, d.skipTLSVerify = opts.Timeout, opts.SkipTLSVerify
	}
	sender := d.sender
	d.mu.Unlock()
	return sender.Send(ctx, notification)
}
