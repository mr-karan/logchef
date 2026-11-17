package alerts

import (
	"time"

	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/pkg/models"
)

// AlertNotification is what one notification of a rule says. message renders
// it for the delivery queue.
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
	QueryLanguage  models.QueryLanguage
	Query          string
	ConditionJSON  string
	// Group holds the group values of a grouped rule's message, nil otherwise.
	Group       map[string]string
	Labels      map[string]string
	Annotations map[string]string
	// RuleLabels and RuleAnnotations are the rule's own, without the
	// built-in entries that Labels and Annotations add for version 1.
	RuleLabels      map[string]string
	RuleAnnotations map[string]string
	// HistoryID is the episode's history row, zero for a test.
	HistoryID    int64
	TriggeredAt  time.Time
	ResolvedAt   *time.Time
	GeneratorURL string
	Message      string
}

// message renders the notification: the email subject and body, and the
// version 1 and version 2 webhook payloads.
func (n AlertNotification) message(event notify.Event) notify.Message {
	return notify.Message{
		Event:   event,
		Title:   emailSubject(n),
		Text:    emailBody(n),
		Group:   copyStringMap(n.Group),
		Webhook: n.webhookContent(event),
		Alert: &notify.AlertPayload{
			AlertID:           int64(n.AlertID),
			AlertName:         n.AlertName,
			Description:       n.Description,
			Status:            string(v1Status(n.Status)),
			Severity:          string(n.Severity),
			SourceID:          int(n.SourceID),
			SourceName:        n.SourceName,
			Value:             n.Value,
			ThresholdOperator: string(n.ThresholdOp),
			ThresholdValue:    n.ThresholdValue,
			FrequencySeconds:  n.FrequencySecs,
			LookbackSeconds:   n.LookbackSecs,
			Query:             n.Query,
			ConditionJSON:     n.ConditionJSON,
			Labels:            n.Labels,
			Annotations:       n.Annotations,
			TriggeredAt:       n.TriggeredAt,
			ResolvedAt:        n.ResolvedAt,
			GeneratorURL:      n.GeneratorURL,
			Message:           n.Message,
		},
	}
}

// ruleTypeThreshold is the type of every rule until new-pattern rules
// (RFC 0006 phase 2).
const ruleTypeThreshold = "threshold"

// webhookContent is the version 2 webhook content: the rule and the one
// group this notification is about.
func (n AlertNotification) webhookContent(event notify.Event) *notify.WebhookContent {
	instance := notify.WebhookInstance{
		Group:       copyStringMap(n.Group),
		TriggeredAt: n.TriggeredAt.UTC(),
	}
	switch n.Status {
	case models.AlertStatusResolved:
		instance.Status = notify.InstanceResolved
	case models.AlertStatusNoData:
		instance.Status = notify.InstanceNoData
	default:
		instance.Status = notify.InstanceFiring
	}
	if instance.Status != notify.InstanceNoData {
		value := n.Value
		instance.Value = &value
	}
	if n.ResolvedAt != nil {
		resolved := n.ResolvedAt.UTC()
		instance.ResolvedAt = &resolved
	}
	if n.HistoryID > 0 {
		id := n.HistoryID
		instance.HistoryID = &id
	}
	return &notify.WebhookContent{
		Title:    notify.WebhookTitle(n.AlertName, n.Group, event),
		URL:      n.GeneratorURL,
		Severity: string(n.Severity),
		Alert: notify.WebhookRule{
			ID: int64(n.AlertID), Name: n.AlertName, Description: n.Description,
			SourceID: int64(n.SourceID), SourceName: n.SourceName,
			RuleType: ruleTypeThreshold, QueryLanguage: string(n.QueryLanguage), Query: n.Query,
			ThresholdOperator: string(n.ThresholdOp), ThresholdValue: n.ThresholdValue,
			FrequencySeconds: n.FrequencySecs, LookbackSeconds: n.LookbackSecs,
			Labels: copyStringMap(n.RuleLabels), Annotations: copyStringMap(n.RuleAnnotations),
		},
		Instances: []notify.WebhookInstance{instance},
	}
}

// event is the queue event of a notification's status.
func event(status models.AlertStatus) notify.Event {
	switch status {
	case models.AlertStatusResolved:
		return notify.EventResolved
	case models.AlertStatusNoData:
		return notify.EventNoData
	default:
		return notify.EventFiring
	}
}

// v1Status keeps the version 1 payload's two statuses: a no_data episode is
// sent as triggered, and annotations.reason says why.
func v1Status(status models.AlertStatus) models.AlertStatus {
	if status == models.AlertStatusNoData {
		return models.AlertStatusTriggered
	}
	return status
}
