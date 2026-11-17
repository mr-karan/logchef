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
}

// message renders the notification: the email subject and body, and the
// version 1 webhook payload.
func (n AlertNotification) message(event notify.Event) notify.Message {
	return notify.Message{
		Event: event,
		Title: emailSubject(n),
		Text:  emailBody(n),
		Group: copyStringMap(n.Group),
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
