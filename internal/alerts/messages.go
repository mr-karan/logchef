package alerts

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// messageContext holds what every message of one evaluation shares, so a
// grouped rule reads the source, the recipients and the settings once.
type messageContext struct {
	alert             *models.Alert
	sourceName        string
	recipientEmails   []string
	missingRecipients []models.UserID
	recipientTargets  map[string]DeliveryTargetID
	generatorURL      string
}

func (m *Manager) newMessageContext(ctx context.Context, alert *models.Alert) *messageContext {
	mc := &messageContext{alert: alert}
	if source, err := m.db.GetSource(ctx, alert.SourceID); err == nil && source != nil {
		mc.sourceName = source.Name
	} else {
		m.log.Warn("failed to fetch source name for alert metadata", "source_id", alert.SourceID, "error", err)
	}
	mc.recipientEmails, mc.missingRecipients, mc.recipientTargets = m.resolveRecipientEmails(ctx, alert)
	mc.generatorURL = m.generatorURL(ctx, alert)
	return mc
}

// metadata returns the labels and annotations of one group's message. The
// group values join the rule's labels; the built-in labels win a name clash.
func (mc *messageContext) metadata(status models.AlertStatus, value float64, group map[string]string) (labels, annotations map[string]string) {
	alert := mc.alert
	labels = copyStringMap(alert.Labels)
	if labels == nil {
		labels = make(map[string]string, 8+len(group))
	}
	maps.Copy(labels, group)
	labels["alertname"] = alert.Name
	labels["alert_id"] = strconv.FormatInt(int64(alert.ID), 10)
	labels["severity"] = string(alert.Severity)
	if mc.sourceName != "" {
		labels["source"] = mc.sourceName
	}
	labels["source_id"] = strconv.FormatInt(int64(alert.SourceID), 10)

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
	// A group column may share a name with a built-in label, so the group
	// also travels whole in its own annotation.
	if len(group) > 0 {
		annotations["group"] = formatGroup(group)
	}
	return labels, annotations
}

func (mc *messageContext) notification(history *models.AlertHistoryEntry, labels, annotations map[string]string, status models.AlertStatus, value float64) AlertNotification {
	alert := mc.alert
	return AlertNotification{
		AlertID:                 alert.ID,
		AlertName:               alert.Name,
		Description:             strings.TrimSpace(alert.Description),
		Status:                  status,
		Severity:                alert.Severity,
		SourceID:                alert.SourceID,
		SourceName:              mc.sourceName,
		Value:                   value,
		ThresholdOp:             alert.ThresholdOperator,
		ThresholdValue:          alert.ThresholdValue,
		FrequencySecs:           alert.FrequencySeconds,
		LookbackSecs:            alert.LookbackSeconds,
		Query:                   strings.TrimSpace(alert.Query),
		ConditionJSON:           strings.TrimSpace(alert.ConditionJSON),
		Group:                   copyStringMap(history.Group),
		Labels:                  copyStringMap(labels),
		Annotations:             copyStringMap(annotations),
		TriggeredAt:             history.TriggeredAt,
		ResolvedAt:              history.ResolvedAt,
		GeneratorURL:            mc.generatorURL,
		Message:                 history.Message,
		RecipientUserIDs:        append([]models.UserID(nil), alert.RecipientUserIDs...),
		RecipientEmails:         append([]string(nil), mc.recipientEmails...),
		recipientTargets:        mc.recipientTargets,
		MissingRecipientUserIDs: append([]models.UserID(nil), mc.missingRecipients...),
		WebhookURLs:             append([]string(nil), alert.WebhookURLs...),
	}
}

// sendResolved sends the resolved message of one closed episode.
func (m *Manager) sendResolved(ctx context.Context, mc *messageContext, entry *models.AlertHistoryEntry, value float64, extra map[string]string) error {
	labels, annotations := mc.metadata(models.AlertStatusResolved, value, entry.Group)
	if entry.ResolvedAt != nil {
		annotations["resolved_at"] = entry.ResolvedAt.Format(time.RFC3339Nano)
	}
	maps.Copy(annotations, extra)
	return m.sender.Send(ctx, mc.notification(entry, labels, annotations, models.AlertStatusResolved, value)).Err()
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
