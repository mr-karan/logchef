package alerts

import (
	"context"
	"fmt"
	"strings"
)

type MultiSender struct {
	senders []AlertSender
}

func NewMultiSender(senders ...AlertSender) AlertSender {
	filtered := make([]AlertSender, 0, len(senders))
	for _, sender := range senders {
		if sender == nil {
			continue
		}
		filtered = append(filtered, sender)
	}
	return MultiSender{senders: filtered}
}

func (m MultiSender) Send(ctx context.Context, notification AlertNotification) error {
	if len(m.senders) == 0 {
		return nil
	}
	var errs []string
	for _, sender := range m.senders {
		if err := sender.Send(ctx, notification); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("notification delivery failed: %s", strings.Join(errs, "; "))
	}
	return nil
}
