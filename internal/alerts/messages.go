package alerts

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// messageContext holds what every message of one evaluation shares, so a
// grouped rule reads the source, the destinations and the settings once.
type messageContext struct {
	alert        *models.Alert
	sourceName   string
	targets      targets
	generatorURL string
}

// targets are the resolved targets of a rule's destinations.
type targets struct {
	webhooks []WebhookTarget
	emails   []EmailTarget
	unusable []UnusableTarget
}

func (m *Manager) newMessageContext(ctx context.Context, alert *models.Alert) *messageContext {
	mc := &messageContext{alert: alert}
	if source, err := m.db.GetSource(ctx, alert.SourceID); err == nil && source != nil {
		mc.sourceName = source.Name
	} else {
		m.log.Warn("failed to fetch source name for alert metadata", "source_id", alert.SourceID, "error", err)
	}
	destinations, err := m.db.ListAlertDestinations(ctx, alert.ID)
	if err != nil {
		m.log.Error("failed to load alert destinations", "alert_id", alert.ID, "error", err)
		mc.targets.unusable = []UnusableTarget{{Target: deliveryTargetID("destinations", "lookup"), Err: "destination lookup failed"}}
	} else {
		mc.targets = m.resolveTargets(ctx, destinations, false)
	}
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
		AlertID:        alert.ID,
		AlertName:      alert.Name,
		Description:    strings.TrimSpace(alert.Description),
		Status:         status,
		Severity:       alert.Severity,
		SourceID:       alert.SourceID,
		SourceName:     mc.sourceName,
		Value:          value,
		ThresholdOp:    alert.ThresholdOperator,
		ThresholdValue: alert.ThresholdValue,
		FrequencySecs:  alert.FrequencySeconds,
		LookbackSecs:   alert.LookbackSeconds,
		Query:          strings.TrimSpace(alert.Query),
		ConditionJSON:  strings.TrimSpace(alert.ConditionJSON),
		Group:          copyStringMap(history.Group),
		Labels:         copyStringMap(labels),
		Annotations:    copyStringMap(annotations),
		TriggeredAt:    history.TriggeredAt,
		ResolvedAt:     history.ResolvedAt,
		GeneratorURL:   mc.generatorURL,
		Message:        history.Message,
		Webhooks:       slices.Clone(mc.targets.webhooks),
		Emails:         slices.Clone(mc.targets.emails),
		Unusable:       slices.Clone(mc.targets.unusable),
	}
}

// sendResolved sends the resolved message of one closed episode.
func (m *Manager) sendResolved(ctx context.Context, mc *messageContext, entry *models.AlertHistoryEntry, value float64, extra map[string]string) error {
	labels, annotations := mc.metadata(models.AlertStatusResolved, value, entry.Group)
	if entry.ResolvedAt != nil {
		annotations["resolved_at"] = entry.ResolvedAt.Format(time.RFC3339Nano)
	}
	maps.Copy(annotations, extra)
	return m.deliver(ctx, mc.notification(entry, labels, annotations, models.AlertStatusResolved, value)).Err()
}

// deliver sends to the selected targets and reports the selected unusable
// targets as failed.
func (m *Manager) deliver(ctx context.Context, notification AlertNotification) DeliveryResults {
	return append(m.sender.Send(ctx, notification), notification.unusableResults()...)
}

// resolveTargets resolves destinations into targets. An inactive destination
// is unusable unless includeInactive is set. An email address reached
// through two destinations is mailed once.
func (m *Manager) resolveTargets(ctx context.Context, destinations []*models.NotificationDestination, includeInactive bool) targets {
	var out targets
	mailed := make(map[string]struct{})
	for _, d := range destinations {
		if !d.IsActive && !includeInactive {
			out.unusable = append(out.unusable, UnusableTarget{Target: destinationTarget(d.ID), Err: fmt.Sprintf("destination %q is inactive", d.Name)})
			continue
		}
		switch d.Type {
		case models.DestinationTypeWebhook:
			out.webhooks = append(out.webhooks, WebhookTarget{Target: webhookTarget(d.ID), URL: d.Secret})
		case models.DestinationTypeEmail:
			recipients, err := notify.ExpandEmailDestination(ctx, m.db, d)
			if err != nil {
				m.log.Error("failed to resolve email recipients", "destination_id", d.ID, "error", err)
				out.unusable = append(out.unusable, UnusableTarget{Target: destinationTarget(d.ID), Err: "recipient resolution failed"})
				continue
			}
			for _, r := range recipients {
				target := emailAddressTarget(d.ID, r.Address)
				if r.UserID != nil {
					target = emailUserTarget(d.ID, *r.UserID)
				}
				if r.Address == "" {
					out.unusable = append(out.unusable, UnusableTarget{Target: target, Err: fmt.Sprintf("missing recipient user ID: %d", *r.UserID)})
					continue
				}
				if _, seen := mailed[r.Address]; seen {
					continue
				}
				mailed[r.Address] = struct{}{}
				out.emails = append(out.emails, EmailTarget{Target: target, Address: r.Address})
			}
		default:
			out.unusable = append(out.unusable, UnusableTarget{Target: destinationTarget(d.ID), Err: fmt.Sprintf("destination type %q has no sender yet", d.Type)})
		}
	}
	return out
}

// TestDestination sends a test notification to every target of d, active or
// not, and returns one result per target.
func (m *Manager) TestDestination(ctx context.Context, d *models.NotificationDestination) []models.DestinationTestResult {
	resolved := m.resolveTargets(ctx, []*models.NotificationDestination{d}, true)
	notification := AlertNotification{
		AlertName:      "Test Alert",
		Description:    "This is a test notification to verify a notification destination.",
		Status:         models.AlertStatusTriggered,
		Severity:       models.AlertSeverityInfo,
		SourceName:     "Test Source",
		Value:          1.0,
		ThresholdOp:    models.AlertThresholdGreaterThan,
		ThresholdValue: 0.0,
		FrequencySecs:  60,
		LookbackSecs:   300,
		Query:          "SELECT 1",
		TriggeredAt:    time.Now().UTC(),
		Message:        "Test notification - please ignore",
		Webhooks:       resolved.webhooks,
		Emails:         resolved.emails,
		Unusable:       resolved.unusable,
	}
	labels := make(map[DeliveryTargetID]string)
	for _, w := range resolved.webhooks {
		labels[w.Target] = string(d.Type)
	}
	for _, e := range resolved.emails {
		labels[e.Target] = e.Address
	}
	for _, u := range resolved.unusable {
		labels[u.Target] = string(d.Type)
	}
	results := m.deliver(ctx, notification)
	out := make([]models.DestinationTestResult, 0, len(results))
	for _, r := range results {
		result := models.DestinationTestResult{Target: labels[r.Target], OK: r.Err == nil}
		if r.Err != nil {
			result.Error = boundedDeliveryError(r.Err).Error()
		}
		out = append(out, result)
	}
	return out
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
