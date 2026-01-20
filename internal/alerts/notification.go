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
	TeamID         models.TeamID
	TeamName       string
	SourceID       models.SourceID
	SourceName     string
	Value          float64
	ThresholdOp    models.AlertThresholdOperator
	ThresholdValue float64
	FrequencySecs  int
	LookbackSecs   int
	Query          string
	ConditionJSON  string
	Labels         map[string]string
	Annotations    map[string]string
	TriggeredAt    time.Time
	ResolvedAt     *time.Time
	GeneratorURL   string
	Message        string

	RecipientUserIDs        []models.UserID
	RecipientEmails         []string
	MissingRecipientUserIDs []models.UserID
	RecipientResolutionErr  string
	WebhookURLs             []string
}

// AlertSender abstracts the delivery mechanism for alert notifications.
type AlertSender interface {
	Send(ctx context.Context, notification AlertNotification) error
}
