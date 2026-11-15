package alerts

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func (m *Manager) buildAlertMetadata(ctx context.Context, alert *models.Alert, status models.AlertStatus, value float64) (labels, annotations map[string]string) {
	labels = copyStringMap(alert.Labels)
	if labels == nil {
		labels = make(map[string]string, 8)
	}
	labels["alertname"] = alert.Name
	labels["alert_id"] = strconv.FormatInt(int64(alert.ID), 10)
	labels["severity"] = string(alert.Severity)

	if source, err := m.db.GetSource(ctx, alert.SourceID); err == nil && source != nil {
		labels["source"] = source.Name
		labels["source_id"] = strconv.FormatInt(int64(alert.SourceID), 10)
	} else {
		// Fallback to just ID if fetch fails
		labels["source_id"] = strconv.FormatInt(int64(alert.SourceID), 10)
		m.log.Warn("failed to fetch source name for alert metadata", "source_id", alert.SourceID, "error", err)
	}

	annotations = copyStringMap(alert.Annotations)
	if annotations == nil {
		annotations = make(map[string]string, 8)
	}
	if desc := strings.TrimSpace(alert.Description); desc != "" {
		annotations["description"] = desc
	}
	if query := strings.TrimSpace(alert.Query); query != "" {
		annotations["query"] = query
	}
	if alert.ConditionJSON != "" {
		annotations["condition_json"] = alert.ConditionJSON
	}
	annotations["threshold"] = fmt.Sprintf("%s %.4f", alert.ThresholdOperator, alert.ThresholdValue)
	annotations["value"] = strconv.FormatFloat(value, 'f', 4, 64)
	annotations["status"] = string(status)
	annotations["frequency_seconds"] = strconv.Itoa(alert.FrequencySeconds)
	if alert.LookbackSeconds > 0 {
		annotations["lookback_seconds"] = strconv.Itoa(alert.LookbackSeconds)
	}
	return labels, annotations
}

func (m *Manager) sendNotification(ctx context.Context, alert *models.Alert, history *models.AlertHistoryEntry, labels, annotations map[string]string, status models.AlertStatus, value float64) error {
	if m.sender == nil || history == nil {
		return nil
	}

	notification := m.buildNotification(ctx, alert, history, labels, annotations, status, value)
	return m.sender.Send(ctx, notification).Err()
}

func (m *Manager) buildNotification(ctx context.Context, alert *models.Alert, history *models.AlertHistoryEntry, labels, annotations map[string]string, status models.AlertStatus, value float64) AlertNotification {
	recipientEmails, missingRecipients, recipientTargets := m.resolveRecipientEmails(ctx, alert)
	sourceName := labels["source"]

	return AlertNotification{
		AlertID:                 alert.ID,
		AlertName:               alert.Name,
		Description:             strings.TrimSpace(alert.Description),
		Status:                  status,
		Severity:                alert.Severity,
		SourceID:                alert.SourceID,
		SourceName:              sourceName,
		Value:                   value,
		ThresholdOp:             alert.ThresholdOperator,
		ThresholdValue:          alert.ThresholdValue,
		FrequencySecs:           alert.FrequencySeconds,
		LookbackSecs:            alert.LookbackSeconds,
		Query:                   strings.TrimSpace(alert.Query),
		ConditionJSON:           strings.TrimSpace(alert.ConditionJSON),
		Labels:                  copyStringMap(labels),
		Annotations:             copyStringMap(annotations),
		TriggeredAt:             history.TriggeredAt,
		ResolvedAt:              history.ResolvedAt,
		GeneratorURL:            m.generatorURL(ctx, alert),
		Message:                 history.Message,
		RecipientUserIDs:        append([]models.UserID(nil), alert.RecipientUserIDs...),
		RecipientEmails:         recipientEmails,
		recipientTargets:        recipientTargets,
		MissingRecipientUserIDs: missingRecipients,
		WebhookURLs:             append([]string(nil), alert.WebhookURLs...),
	}
}

func (m *Manager) resolveRecipientEmails(ctx context.Context, alert *models.Alert) (recipientEmails []string, missingRecipients []models.UserID, targets map[string]DeliveryTargetID) {
	if alert == nil || len(alert.RecipientUserIDs) == 0 {
		return nil, nil, nil
	}

	// Recipients are users (not team-scoped). Resolve each by id directly.
	seen := make(map[string]struct{}, len(alert.RecipientUserIDs))
	emails := make([]string, 0, len(alert.RecipientUserIDs))
	targets = make(map[string]DeliveryTargetID)
	missing := make([]models.UserID, 0)
	for _, userID := range alert.RecipientUserIDs {
		user, err := m.db.GetUser(ctx, userID)
		if err != nil || user == nil {
			missing = append(missing, userID)
			continue
		}
		email := strings.TrimSpace(user.Email)
		if email == "" {
			missing = append(missing, userID)
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
		targets[email] = emailUserTarget(userID)
	}

	return emails, missing, targets
}

func (m *Manager) generatorURL(ctx context.Context, alert *models.Alert) string {
	if trimmed := strings.TrimSpace(alert.GeneratorURL); trimmed != "" {
		return trimmed
	}

	base := strings.TrimSpace(store.GetSettingWithDefault(ctx, m.db, "alerts.frontend_url", ""))
	if base == "" {
		base = strings.TrimSpace(store.GetSettingWithDefault(ctx, m.db, "alerts.external_url", ""))
	}
	if base == "" {
		return ""
	}

	base = strings.TrimSuffix(base, "/")
	return fmt.Sprintf("%s/logs/alerts/%d?source=%d", base, alert.ID, alert.SourceID)
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	maps.Copy(dst, src)
	return dst
}
