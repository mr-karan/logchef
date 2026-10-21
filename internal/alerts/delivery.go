package alerts

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/mr-karan/logchef/pkg/models"
)

// DeliveryTargetID identifies a channel destination without exposing its address.
type DeliveryTargetID string

// DeliveryResult records one target's delivery outcome.
type DeliveryResult struct {
	Target DeliveryTargetID
	Err    error
}

// DeliveryResults contains the outcomes of all selected targets.
type DeliveryResults []DeliveryResult

// Err returns a bounded summary of failed deliveries.
func (results DeliveryResults) Err() error {
	var errs []error
	for _, result := range results {
		if result.Err != nil {
			errs = append(errs, result.Err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return boundedDeliveryError(errors.Join(errs...))
}

func deliveryTargetID(channel, address string) DeliveryTargetID {
	return DeliveryTargetID(fmt.Sprintf("%s:%x", channel, sha256.Sum256([]byte(address))))
}

func (n AlertNotification) selects(target DeliveryTargetID) bool {
	if n.deliveryTargets == nil {
		return true
	}
	_, ok := n.deliveryTargets[target]
	return ok
}

func (n AlertNotification) targetIDs() []string {
	targets := make(map[DeliveryTargetID]struct{})
	for _, address := range uniqueEmails(n.RecipientEmails) {
		targets[n.emailTarget(address)] = struct{}{}
	}
	for _, address := range n.WebhookURLs {
		targets[deliveryTargetID("webhook", address)] = struct{}{}
	}
	if n.RecipientResolutionErr != "" || (len(n.RecipientUserIDs) > 0 && len(n.RecipientEmails) == 0 && len(n.MissingRecipientUserIDs) == 0) {
		targets[deliveryTargetID("email", "recipient-resolution")] = struct{}{}
	}
	for _, id := range n.MissingRecipientUserIDs {
		targets[emailUserTarget(id)] = struct{}{}
	}
	ids := make([]string, 0, len(targets))
	for target := range targets {
		if n.selects(target) {
			ids = append(ids, string(target))
		}
	}
	sort.Strings(ids)
	return ids
}

func pendingDeliveryTargets(payload map[string]any) map[DeliveryTargetID]struct{} {
	values, exists := payload["delivery_pending"]
	if !exists {
		return nil
	}
	targets := make(map[DeliveryTargetID]struct{})
	if ids, ok := values.([]any); ok {
		for _, id := range ids {
			if key, ok := id.(string); ok {
				targets[DeliveryTargetID(key)] = struct{}{}
			}
		}
	}
	return targets
}

func emailUserTarget(id models.UserID) DeliveryTargetID {
	return deliveryTargetID("email-user", strconv.FormatInt(int64(id), 10))
}

func (n AlertNotification) emailTarget(address string) DeliveryTargetID {
	if target, ok := n.recipientTargets[address]; ok {
		return target
	}
	return deliveryTargetID("email", address)
}

func (results DeliveryResults) failedTargets() []string {
	targets := make([]string, 0)
	for _, result := range results {
		if result.Err != nil {
			targets = append(targets, string(result.Target))
		}
	}
	return targets
}

func deliveryRetryState(history *models.AlertHistoryEntry) (attempts int, retry bool) {
	if history == nil {
		return 0, false
	}
	// Legacy entries have already had one delivery attempt.
	attempts = 1
	if value, ok := history.Payload["delivery_attempts"].(float64); ok {
		attempts = int(value)
	}
	failed, _ := history.Payload["delivery_failed"].(bool)
	return attempts, failed && attempts < maxDeliveryAttempts
}
