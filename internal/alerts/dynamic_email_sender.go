package alerts

import (
	"context"

	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
)

// DynamicEmailSender reads the SMTP settings at each send, so a settings
// change applies without a restart.
type DynamicEmailSender struct {
	settings store.SettingGetter
}

func NewDynamicEmailSender(settings store.SettingGetter) *DynamicEmailSender {
	return &DynamicEmailSender{settings: settings}
}

func (d *DynamicEmailSender) Send(ctx context.Context, notification AlertNotification) DeliveryResults {
	mailer := notify.NewMailer(notify.MailerOptionsFromSettings(ctx, d.settings))
	return NewEmailSender(mailer).Send(ctx, notification)
}
