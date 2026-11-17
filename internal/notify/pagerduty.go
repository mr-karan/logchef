package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// PagerDutyEventsURL is the Events API v2 endpoint.
const PagerDutyEventsURL = "https://events.pagerduty.com/v2/enqueue"

// pagerDutySummaryLimit is the Events API's limit on summary, in characters.
const pagerDutySummaryLimit = 1024

// PagerDutySender sends a message as a PagerDuty Events API v2 event: a
// trigger for firing, no-data and test messages, and a resolve for a
// resolved one. The dedup key names the group's incident, so a resolve
// closes the incident that its trigger opened, and a repeated send of the
// same event changes nothing.
type PagerDutySender struct {
	http   httpPoster
	logger *slog.Logger
	url    string
}

// NewPagerDutySender posts through web's client, so PagerDuty follows the
// webhook address policy and request timeout.
func NewPagerDutySender(web *DynamicWebhookSender, logger *slog.Logger) *PagerDutySender {
	return &PagerDutySender{http: web, logger: logger.With("component", "pagerduty_sender"), url: PagerDutyEventsURL}
}

// Send posts msg as an event with the destination's routing key. PagerDuty
// answers 202; a 400 is permanent, a 429 or 5xx is retried.
func (s *PagerDutySender) Send(ctx context.Context, dest *models.NotificationDestination, _ string, _ int64, msg Message) error {
	body, err := pagerDutyBody(dest.Secret, msg)
	if err != nil {
		return Permanent(fmt.Errorf("pagerduty delivery failed: %w", err))
	}
	answer, err := s.http.post(ctx, dest.Name, s.url, 0, body)
	if err == nil && !answer.ok() {
		message := pagerDutyErrorMessage(answer.body, dest.Secret)
		s.logger.Warn("pagerduty delivery failed", "destination", dest.Name, "status", answer.status, "pagerduty_error", message)
		err = answer.err(message)
	}
	if err != nil {
		return wrapClassified("pagerduty delivery failed", err)
	}
	return nil
}

// pagerDutyErrorMessage is the message of an Events API error body, with
// the routing key masked, or "" when the body is not one. Nothing else of the
// body leaves the sender: a body cut at the read cap does not parse, so a key
// split by the cut is never shown.
func pagerDutyErrorMessage(body, routingKey string) string {
	var answer struct {
		Message string   `json:"message"`
		Errors  []string `json:"errors"`
	}
	if json.Unmarshal([]byte(body), &answer) != nil || answer.Message == "" {
		return ""
	}
	message := answer.Message
	if len(answer.Errors) > 0 {
		message += ": " + strings.Join(answer.Errors, "; ")
	}
	return strings.ReplaceAll(message, routingKey, "[routing key]")
}

type pagerDutyPayload struct {
	Summary       string         `json:"summary"`
	Source        string         `json:"source"`
	Severity      string         `json:"severity"`
	Timestamp     string         `json:"timestamp,omitempty"`
	Group         string         `json:"group,omitempty"`
	CustomDetails map[string]any `json:"custom_details,omitempty"`
}

type pagerDutyLink struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

type pagerDutyEvent struct {
	RoutingKey  string            `json:"routing_key"`
	EventAction string            `json:"event_action"`
	DedupKey    string            `json:"dedup_key"`
	Payload     *pagerDutyPayload `json:"payload,omitempty"`
	Links       []pagerDutyLink   `json:"links,omitempty"`
}

var pagerDutySeverities = map[string]string{"info": "info", "warning": "warning", "critical": "critical"}

// pagerDutyBody renders msg as an Events API v2 event. A resolve carries
// only the routing and dedup keys, which is all the API reads from one.
func pagerDutyBody(routingKey string, msg Message) ([]byte, error) {
	a := msg.Alert
	switch {
	case a == nil:
		return nil, errors.New("the message has no alert")
	case msg.DedupKey == "":
		return nil, errors.New("the message has no dedup key")
	}
	event := pagerDutyEvent{RoutingKey: routingKey, DedupKey: msg.DedupKey}
	switch msg.Event {
	case EventResolved:
		event.EventAction = "resolve"
		return json.Marshal(event)
	case EventFiring, EventNoData, EventTest:
		event.EventAction = "trigger"
	default:
		return nil, fmt.Errorf("unknown event %q", msg.Event)
	}
	severity := pagerDutySeverities[a.Severity]
	if severity == "" {
		return nil, fmt.Errorf("unknown severity %q", a.Severity)
	}

	name := a.AlertName
	if len(msg.Group) > 0 {
		name += " [" + formatGroupValues(msg.Group) + "]"
	}
	details := map[string]any{"event": string(msg.Event), "threshold": thresholdText(a.ThresholdOperator, a.ThresholdValue)}
	summary := name + ": no data"
	if msg.Event != EventNoData {
		summary = fmt.Sprintf("%s: value %s %s", name, formatNumber(a.Value), details["threshold"])
		details["value"] = a.Value
	}
	if a.Query != "" {
		details["query"] = a.Query
	}
	if a.Description != "" {
		details["description"] = a.Description
	}
	if len(a.Labels) > 0 {
		details["labels"] = a.Labels
	}
	source := a.SourceName
	if source == "" {
		source = fmt.Sprintf("source %d", a.SourceID)
	}
	event.Payload = &pagerDutyPayload{
		Summary: cut(summary, pagerDutySummaryLimit), Source: source, Severity: severity,
		Group: formatGroupValues(msg.Group), CustomDetails: details,
	}
	if !a.TriggeredAt.IsZero() {
		event.Payload.Timestamp = a.TriggeredAt.UTC().Format(time.RFC3339)
	}
	if a.GeneratorURL != "" {
		event.Links = []pagerDutyLink{{Href: a.GeneratorURL, Text: "Open in Logchef"}}
	}
	return json.Marshal(event)
}
