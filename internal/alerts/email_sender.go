package alerts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/notify"
)

// smtpDialTimeout is the SMTP budget inside an alert delivery attempt.
const smtpDialTimeout = notify.DefaultSMTPTimeout

// EmailSender mails a notification to the email targets, one message per
// recipient, through a notify.Mailer.
type EmailSender struct {
	mailer *notify.Mailer
}

func NewEmailSender(mailer *notify.Mailer) *EmailSender {
	return &EmailSender{mailer: mailer}
}

func (s *EmailSender) Send(ctx context.Context, notification AlertNotification) DeliveryResults {
	var results DeliveryResults
	for index, target := range notification.Emails {
		if !notification.selects(target.Target) {
			continue
		}
		err := s.mailer.Send(ctx, target.Address, notify.MailMessage{Subject: emailSubject(notification), Text: s.buildBody(notification)})
		if err != nil {
			err = fmt.Errorf("email delivery failed: email recipient %d: %w", index+1, err)
		}
		results = append(results, DeliveryResult{Target: target.Target, Err: err})
	}
	return results
}

func emailSubject(notification AlertNotification) string {
	name := notification.AlertName
	if len(notification.Group) > 0 {
		// Group values come from log data; keep them on one header line.
		name += " [" + strings.Map(dropLineBreaks, formatGroup(notification.Group)) + "]"
	}
	return fmt.Sprintf("[Logchef] %s (%s) %s", name, strings.ToUpper(string(notification.Severity)), strings.ToUpper(string(notification.Status)))
}

func (s *EmailSender) buildBody(notification AlertNotification) string {
	status := strings.ToUpper(string(notification.Status))
	severity := strings.ToUpper(string(notification.Severity))
	lines := []string{
		fmt.Sprintf("Alert: %s", notification.AlertName),
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("Severity: %s", severity),
	}
	if len(notification.Group) > 0 {
		lines = append(lines, fmt.Sprintf("Group: %s", formatGroup(notification.Group)))
	}
	if notification.SourceName != "" {
		lines = append(lines, fmt.Sprintf("Source: %s", notification.SourceName))
	} else if notification.SourceID > 0 {
		lines = append(lines, fmt.Sprintf("Source ID: %d", notification.SourceID))
	}
	lines = append(lines,
		fmt.Sprintf("Value: %.4f", notification.Value),
		fmt.Sprintf("Threshold: %s %.4f", notification.ThresholdOp, notification.ThresholdValue),
	)
	if notification.FrequencySecs > 0 {
		lines = append(lines, fmt.Sprintf("Frequency: %ds", notification.FrequencySecs))
	}
	if notification.LookbackSecs > 0 {
		lines = append(lines, fmt.Sprintf("Lookback: %ds", notification.LookbackSecs))
	}
	lines = append(lines, fmt.Sprintf("Triggered At: %s", notification.TriggeredAt.Format(time.RFC3339)))
	if notification.ResolvedAt != nil {
		lines = append(lines, fmt.Sprintf("Resolved At: %s", notification.ResolvedAt.Format(time.RFC3339)))
	}
	if notification.Message != "" {
		lines = append(lines, fmt.Sprintf("Message: %s", notification.Message))
	}
	if notification.Description != "" {
		lines = append(lines, fmt.Sprintf("Description: %s", notification.Description))
	}
	if notification.Query != "" {
		lines = append(lines, fmt.Sprintf("Query: %s", notification.Query))
	}
	if notification.GeneratorURL != "" {
		lines = append(lines, fmt.Sprintf("View: %s", notification.GeneratorURL))
	}
	return strings.Join(lines, "\n") + "\n"
}

func dropLineBreaks(r rune) rune {
	if r == '\r' || r == '\n' {
		return ' '
	}
	return r
}
