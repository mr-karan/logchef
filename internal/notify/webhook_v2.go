package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// webhookTitleLimit bounds a version 2 title, in characters.
const webhookTitleLimit = 200

// WebhookContent is what a version 2 webhook body says about a rule and its
// instances. The sender adds the envelope: version, kind, event, sent_at,
// delivery_id and triage.
type WebhookContent struct {
	Title            string            `json:"title"`
	URL              string            `json:"url,omitempty"`
	Severity         string            `json:"severity"`
	Alert            WebhookRule       `json:"alert"`
	Instances        []WebhookInstance `json:"instances"`
	InstancesOmitted int               `json:"instances_omitted"`
}

// WebhookRule is the rule in a version 2 body. Labels and annotations are
// the rule's own.
type WebhookRule struct {
	ID                int64             `json:"id"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	SourceID          int64             `json:"source_id"`
	SourceName        string            `json:"source_name"`
	RuleType          string            `json:"rule_type"`
	QueryLanguage     string            `json:"query_language"`
	Query             string            `json:"query"`
	ThresholdOperator string            `json:"threshold_operator"`
	ThresholdValue    float64           `json:"threshold_value"`
	FrequencySeconds  int               `json:"frequency_seconds"`
	LookbackSeconds   int               `json:"lookback_seconds"`
	Labels            map[string]string `json:"labels"`
	Annotations       map[string]string `json:"annotations"`
}

// Instance statuses of a version 2 body.
const (
	InstanceFiring   = "firing"
	InstanceResolved = "resolved"
	InstanceNoData   = "no_data"
)

// WebhookInstance is one group of the rule in a version 2 body. Value is null
// for no_data; HistoryID is null for a test.
type WebhookInstance struct {
	Group       map[string]string `json:"group"`
	Status      string            `json:"status"`
	Value       *float64          `json:"value"`
	TriggeredAt time.Time         `json:"triggered_at"`
	ResolvedAt  *time.Time        `json:"resolved_at"`
	HistoryID   *int64            `json:"history_id"`
}

// webhookV2Body is the version 2 wire format. Every key is always present,
// so a strict receiver can rely on the shape.
type webhookV2Body struct {
	Version          int               `json:"version"`
	Kind             string            `json:"kind"`
	Event            Event             `json:"event"`
	Title            string            `json:"title"`
	URL              *string           `json:"url"`
	Severity         string            `json:"severity"`
	SentAt           time.Time         `json:"sent_at"`
	DeliveryID       *int64            `json:"delivery_id"`
	Alert            WebhookRule       `json:"alert"`
	Instances        []WebhookInstance `json:"instances"`
	InstancesOmitted int               `json:"instances_omitted"`
	// Triage stays null until triage on fire (RFC 0006 P2) fills it.
	Triage *struct{} `json:"triage"`
}

// webhookBody renders msg in the destination's payload version. Version 1 is
// the AlertPayload as it was, byte for byte.
func webhookBody(version int, deliveryID int64, msg Message, now time.Time) ([]byte, error) {
	switch version {
	case WebhookPayloadV1:
		if msg.Alert == nil {
			return nil, errors.New("the message has no webhook payload")
		}
		return json.Marshal(msg.Alert)
	case WebhookPayloadV2:
		return webhookV2(deliveryID, msg, now)
	default:
		return nil, fmt.Errorf("unknown payload version %d", version)
	}
}

var webhookEventWords = map[Event]string{
	EventFiring: "firing", EventResolved: "resolved", EventNoData: "no data", EventTest: "test",
}

// WebhookTitle is a version 2 title: the rule, its group and the event.
func WebhookTitle(alertName string, group map[string]string, event Event) string {
	if len(group) > 0 {
		return alertName + ": " + formatGroupValues(group) + " " + webhookEventWords[event]
	}
	return alertName + ": " + webhookEventWords[event]
}

func webhookV2(deliveryID int64, msg Message, now time.Time) ([]byte, error) {
	c := msg.Webhook
	if c == nil {
		if msg.Alert == nil {
			return nil, errors.New("the message has no version 2 payload")
		}
		c = legacyWebhookContent(msg)
	}
	body := webhookV2Body{
		Version: WebhookPayloadV2, Kind: "alert", Event: msg.Event,
		Title: cut(c.Title, webhookTitleLimit), Severity: c.Severity,
		SentAt: now.UTC().Truncate(time.Second),
		Alert:  c.Alert, Instances: slices.Clone(c.Instances), InstancesOmitted: c.InstancesOmitted,
	}
	if c.URL != "" {
		body.URL = &c.URL
	}
	if deliveryID > 0 {
		body.DeliveryID = &deliveryID
	}
	body.Alert.Labels = orEmpty(c.Alert.Labels)
	body.Alert.Annotations = orEmpty(c.Alert.Annotations)
	if body.Instances == nil {
		body.Instances = []WebhookInstance{}
	}
	for i := range body.Instances {
		body.Instances[i].Group = orEmpty(body.Instances[i].Group)
	}
	return json.Marshal(body)
}

// legacyWebhookContent is the version 2 content of a row queued before
// version 2 existed, which holds only the version 1 payload, so that a
// destination moved to version 2 still receives it. The version 1 payload
// lacks the query language, the rule's own labels and annotations (its
// labels and annotations mix in built-in entries) and the history row, so
// those stay empty.
func legacyWebhookContent(msg Message) *WebhookContent {
	a := msg.Alert
	instance := WebhookInstance{Group: msg.Group, Status: InstanceFiring, TriggeredAt: a.TriggeredAt.UTC()}
	switch msg.Event {
	case EventResolved:
		instance.Status = InstanceResolved
	case EventNoData:
		instance.Status = InstanceNoData
	case EventFiring, EventTest:
	}
	if instance.Status != InstanceNoData {
		value := a.Value
		instance.Value = &value
	}
	if a.ResolvedAt != nil {
		resolved := a.ResolvedAt.UTC()
		instance.ResolvedAt = &resolved
	}
	return &WebhookContent{
		Title: WebhookTitle(a.AlertName, msg.Group, msg.Event), URL: a.GeneratorURL, Severity: a.Severity,
		Alert: WebhookRule{
			ID: a.AlertID, Name: a.AlertName, Description: a.Description,
			SourceID: int64(a.SourceID), SourceName: a.SourceName, RuleType: "threshold", Query: a.Query,
			ThresholdOperator: a.ThresholdOperator, ThresholdValue: a.ThresholdValue,
			FrequencySeconds: a.FrequencySeconds, LookbackSeconds: a.LookbackSeconds,
		},
		Instances: []WebhookInstance{instance},
	}
}

// orEmpty keeps a JSON object an object, never null.
func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
