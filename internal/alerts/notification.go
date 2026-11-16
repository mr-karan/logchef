package alerts

import (
	"context"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// AlertNotification represents a fully resolved notification payload ready for delivery.
type AlertNotification struct {
	AlertID        models.AlertID
	AlertName      string
	Description    string
	Status         models.AlertStatus
	Severity       models.AlertSeverity
	SourceID       models.SourceID
	SourceName     string
	Value          float64
	ThresholdOp    models.AlertThresholdOperator
	ThresholdValue float64
	FrequencySecs  int
	LookbackSecs   int
	Query          string
	ConditionJSON  string
	// Group holds the group values of a grouped rule's message, nil otherwise.
	Group        map[string]string
	Labels       map[string]string
	Annotations  map[string]string
	TriggeredAt  time.Time
	ResolvedAt   *time.Time
	GeneratorURL string
	Message      string

	// The targets of the rule's destinations. Unusable targets cannot be
	// sent to and are reported as failed deliveries.
	Webhooks []WebhookTarget
	Emails   []EmailTarget
	Unusable []UnusableTarget

	deliveryTargets  map[DeliveryTargetID]struct{}
	attemptedTargets map[DeliveryTargetID]struct{}
}

// WebhookTarget is the URL of one webhook destination.
type WebhookTarget struct {
	Target DeliveryTargetID
	URL    string
}

// EmailTarget is one recipient address of an email destination.
type EmailTarget struct {
	Target  DeliveryTargetID
	Address string
}

// UnusableTarget is a target that cannot receive the notification: an
// inactive destination, or a recipient user without an address.
type UnusableTarget struct {
	Target DeliveryTargetID
	Err    string
}

// AlertSender abstracts the delivery mechanism for alert notifications.
type AlertSender interface {
	Send(ctx context.Context, notification AlertNotification) DeliveryResults
}
