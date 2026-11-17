package alerts

import (
	"fmt"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// emailSubject is the subject of a notification's email.
func emailSubject(notification AlertNotification) string {
	name := notification.AlertName
	if len(notification.Group) > 0 {
		// Group values come from log data; keep them on one header line.
		name += " [" + strings.Map(dropLineBreaks, formatGroup(notification.Group)) + "]"
	}
	return fmt.Sprintf("[Logchef] %s (%s) %s", name, strings.ToUpper(string(notification.Severity)), statusLabel(notification.Status))
}

func statusLabel(status models.AlertStatus) string {
	if status == models.AlertStatusNoData {
		return "NO DATA"
	}
	return strings.ToUpper(string(status))
}

// emailBody is the plain-text body of a notification's email.
func emailBody(notification AlertNotification) string {
	status := statusLabel(notification.Status)
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
	if notification.Status == models.AlertStatusNoData {
		lines = append(lines, "Value: no data")
	} else {
		lines = append(lines, fmt.Sprintf("Value: %.4f", notification.Value))
	}
	lines = append(lines, fmt.Sprintf("Threshold: %s %.4f", notification.ThresholdOp, notification.ThresholdValue))
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
