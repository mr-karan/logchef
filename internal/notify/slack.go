package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/pkg/models"
)

// Slack's limits, in characters.
const (
	slackHeaderLimit = 150
	slackTextLimit   = 3000
	slackFieldLimit  = 2000
	slackURLLimit    = 3000
)

// SlackSender posts a message to a Slack incoming webhook as text and Block
// Kit blocks. An incoming webhook returns no message id, so a resolved is a
// new message, not a thread reply, and a repeated send is a second message.
type SlackSender struct {
	http   httpPoster
	logger *slog.Logger
}

// NewSlackSender posts through web's client, so Slack follows the webhook
// address policy and request timeout.
func NewSlackSender(web *DynamicWebhookSender, logger *slog.Logger) *SlackSender {
	return &SlackSender{http: web, logger: logger.With("component", "slack_sender")}
}

// Send posts msg to the destination's webhook URL. Slack answers 200 with
// the body "ok"; any other answer is a failure, classified by its status.
func (s *SlackSender) Send(ctx context.Context, dest *models.NotificationDestination, _ string, _ int64, msg Message) error {
	body, err := slackBody(msg)
	if err != nil {
		return Permanent(fmt.Errorf("slack delivery failed: %w", err))
	}
	answer, err := s.http.post(ctx, dest.Name, dest.Secret, 0, body)
	if err == nil {
		err = s.check(dest.Name, answer)
	}
	if err != nil {
		return wrapClassified("slack delivery failed", err)
	}
	return nil
}

// slackErrorCode matches the error strings that Slack answers with, such as
// no_service or invalid_payload.
var slackErrorCode = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// check classifies Slack's answer. Only an error code leaves this function:
// a body that is not one may echo the webhook URL, so it is neither logged
// nor returned.
func (s *SlackSender) check(name string, a *httpAnswer) error {
	if a.ok() && a.body == "ok" {
		return nil
	}
	code := ""
	if slackErrorCode.MatchString(a.body) {
		code = a.body
	}
	s.logger.Warn("slack delivery failed", "destination", name, "status", a.status, "slack_error", code)
	switch {
	case a.ok() && a.bodyLost:
		// Slack may have posted the message; a retry may post it twice,
		// which at-least-once delivery accepts.
		return fmt.Errorf("status %d, then the connection failed", a.status)
	case a.ok():
		return Permanent(fmt.Errorf("status %d without ok", a.status))
	}
	return a.err(code)
}

type slackText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

type slackButton struct {
	Type string    `json:"type"`
	Text slackText `json:"text"`
	URL  string    `json:"url"`
}

type slackBlock struct {
	Type     string        `json:"type"`
	Text     *slackText    `json:"text,omitempty"`
	Fields   []slackText   `json:"fields,omitempty"`
	Elements []slackButton `json:"elements,omitempty"`
}

type slackMessage struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks"`
}

var slackEventHeaders = map[Event]string{
	EventFiring:   ":red_circle: Firing",
	EventResolved: ":large_green_circle: Resolved",
	EventNoData:   ":white_circle: No data",
	EventTest:     ":information_source: Test",
}

// slackBody renders msg as Slack's text and blocks: a header with the event
// and rule, the facts as fields, the group and the description as sections,
// and a button back to Logchef. Text from log data or users is escaped and
// cut to Slack's limits.
func slackBody(msg Message) ([]byte, error) {
	a := msg.Alert
	if a == nil {
		return nil, errors.New("the message has no alert")
	}
	header := slackEventHeaders[msg.Event]
	if header == "" {
		return nil, fmt.Errorf("unknown event %q", msg.Event)
	}
	blocks := []slackBlock{{Type: "header", Text: &slackText{Type: "plain_text", Text: cut(header+": "+a.AlertName, slackHeaderLimit), Emoji: true}}}

	value := "no data"
	if msg.Event != EventNoData {
		value = formatNumber(a.Value)
	}
	fields := []slackText{slackField("Severity", a.Severity)}
	if a.SourceName != "" {
		fields = append(fields, slackField("Source", a.SourceName))
	}
	fields = append(fields, slackField("Value", value), slackField("Threshold", thresholdText(a.ThresholdOperator, a.ThresholdValue)))
	if !a.TriggeredAt.IsZero() {
		fields = append(fields, slackField("Since", a.TriggeredAt.UTC().Format(time.RFC3339)))
	}
	if msg.Event == EventResolved && a.ResolvedAt != nil {
		fields = append(fields, slackField("Resolved", a.ResolvedAt.UTC().Format(time.RFC3339)))
	}
	blocks = append(blocks, slackBlock{Type: "section", Fields: fields})

	if len(msg.Group) > 0 {
		blocks = append(blocks, slackSection("Group", formatGroupValues(msg.Group)))
	}
	if a.Description != "" {
		blocks = append(blocks, slackSection("Description", a.Description))
	}
	if a.GeneratorURL != "" && utf8.RuneCountInString(a.GeneratorURL) <= slackURLLimit {
		blocks = append(blocks, slackBlock{Type: "actions", Elements: []slackButton{{
			Type: "button", Text: slackText{Type: "plain_text", Text: "Open in Logchef"}, URL: a.GeneratorURL,
		}}})
	}
	return json.Marshal(slackMessage{Text: mrkdwn(msg.Title, slackTextLimit), Blocks: blocks})
}

func slackField(label, value string) slackText {
	prefix := "*" + label + "*\n"
	return slackText{Type: "mrkdwn", Text: prefix + mrkdwn(value, slackFieldLimit-len(prefix))}
}

func slackSection(label, value string) slackBlock {
	prefix := "*" + label + "*\n"
	return slackBlock{Type: "section", Text: &slackText{Type: "mrkdwn", Text: prefix + mrkdwn(value, slackTextLimit-len(prefix))}}
}

var slackEscapes = map[rune]string{'&': "&amp;", '<': "&lt;", '>': "&gt;"}

// mrkdwn escapes s for Slack's mrkdwn and keeps it within limit characters,
// ending a cut with an ellipsis. An escape is never split.
func mrkdwn(s string, limit int) string {
	var b strings.Builder
	n, total := 0, utf8.RuneCountInString(s)
	i := 0
	for _, r := range s {
		i++
		piece, ok := slackEscapes[r]
		if !ok {
			piece = string(r)
		}
		width := utf8.RuneCountInString(piece)
		room := limit - 1
		if i == total {
			room = limit
		}
		if n+width > room {
			b.WriteString("…")
			return b.String()
		}
		b.WriteString(piece)
		n += width
	}
	return b.String()
}
