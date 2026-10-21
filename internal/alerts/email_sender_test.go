package alerts

import (
	"encoding/json"
	"testing"
)

func TestEmailSenderRetriesOnlyFailedRecipients(t *testing.T) {
	sender := NewEmailSender(EmailSenderOptions{})
	notification := AlertNotification{RecipientEmails: []string{"first@example.com", "second@example.com"}}
	results := sender.Send(t.Context(), notification)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.Err == nil {
			t.Fatal("unconfigured SMTP must fail each recipient")
		}
	}
	results[0].Err = nil
	raw, err := json.Marshal(map[string]any{"delivery_pending": results.failedTargets()})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	notification.deliveryTargets = pendingDeliveryTargets(payload)
	retry := sender.Send(t.Context(), notification)
	if len(retry) != 1 || retry[0].Target != results[1].Target {
		t.Fatalf("retry results = %+v, want only second recipient", retry)
	}
}
