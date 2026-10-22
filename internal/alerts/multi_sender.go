package alerts

import (
	"context"
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

func (m MultiSender) Send(ctx context.Context, notification AlertNotification) DeliveryResults {
	var results DeliveryResults
	for _, sender := range m.senders {
		results = append(results, sender.Send(ctx, notification)...)
	}
	return results
}
