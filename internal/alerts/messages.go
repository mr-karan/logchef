package alerts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// messageContext holds what every message of one evaluation shares, so a
// grouped rule reads the source and the settings once.
type messageContext struct {
	alert        *models.Alert
	sourceName   string
	generatorURL string
}

func (m *Manager) newMessageContext(ctx context.Context, alert *models.Alert) *messageContext {
	mc := &messageContext{alert: alert}
	if source, err := m.db.GetSource(ctx, alert.SourceID); err == nil && source != nil {
		mc.sourceName = source.Name
	} else {
		m.log.Warn("failed to fetch source name for alert metadata", "source_id", alert.SourceID, "error", err)
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
	if status == models.AlertStatusNoData {
		// A group without data has no value to report.
		annotations["reason"] = "no_data"
	} else {
		annotations["value"] = strconv.FormatFloat(value, 'f', 4, 64)
	}
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
	}
}

// resolvedNotification is the resolved message of one closed episode.
func (mc *messageContext) resolvedNotification(entry *models.AlertHistoryEntry, value float64, extra map[string]string) AlertNotification {
	labels, annotations := mc.metadata(models.AlertStatusResolved, value, entry.Group)
	if entry.ResolvedAt != nil {
		annotations["resolved_at"] = entry.ResolvedAt.Format(time.RFC3339Nano)
	}
	maps.Copy(annotations, extra)
	return mc.notification(entry, labels, annotations, models.AlertStatusResolved, value)
}

// DeliveryProducer names alert rows in the delivery queue.
const DeliveryProducer = "alert"

// DeliveryRef is the producer ref of the rows of one history entry.
func DeliveryRef(alertID models.AlertID, historyID int64) string {
	return fmt.Sprintf("%d:%d", alertID, historyID)
}

// IncidentKey names the incident of one group of a rule, across its
// episodes: logchef:alert:<id>: and the first 16 hex characters of the
// SHA-256 of the group key (RFC 0006, PagerDuty).
func IncidentKey(alertID models.AlertID, groupKey string) string {
	sum := sha256.Sum256([]byte(groupKey))
	return fmt.Sprintf("logchef:alert:%d:%x", alertID, sum[:8])
}

// outbox enqueues the notifications of one transaction. It reads the rule's
// destinations once, in that transaction, so the rows match the destinations
// that the commit sees.
type outbox struct {
	alert        *models.Alert
	loaded       bool
	destinations []*models.NotificationDestination
	recipients   map[models.DestinationID][]notify.Recipient
	enqueued     int
}

// add writes the rows of one notification of the episode: one per
// destination, and one per recipient of an email destination. An address
// that two active email destinations reach is mailed once. tag tells apart the
// notifications of one episode, such as the evaluation's eval_seq.
func (o *outbox) add(ctx context.Context, tx store.StoreOps, n AlertNotification, episode *models.AlertHistoryEntry, tag string) error {
	if !o.loaded {
		destinations, err := tx.ListAlertDestinations(ctx, o.alert.ID)
		if err != nil {
			return fmt.Errorf("failed to load alert destinations: %w", err)
		}
		o.destinations = destinations
		o.recipients = make(map[models.DestinationID][]notify.Recipient)
		for _, d := range destinations {
			if d.Type != models.DestinationTypeEmail {
				continue
			}
			recipients, err := notify.ExpandEmailDestination(ctx, tx, d)
			if err != nil {
				return fmt.Errorf("failed to resolve email recipients of destination %d: %w", d.ID, err)
			}
			o.recipients[d.ID] = recipients
		}
		o.loaded = true
	}
	ev := event(n.Status)
	msg := n.message(ev)
	msg.DedupKey = IncidentKey(o.alert.ID, episode.GroupKey)
	ref := DeliveryRef(o.alert.ID, episode.ID)
	// A group's notifications go out in order: its resolved never overtakes
	// its firing.
	sequence := fmt.Sprintf("alert:%d:%x", o.alert.ID, sha256.Sum256([]byte(episode.GroupKey)))
	row := func(d models.DestinationID, recipient string) notify.Delivery {
		return notify.Delivery{
			DestinationID: d, Producer: DeliveryProducer, ProducerRef: ref, Recipient: recipient, SequenceKey: sequence,
			DedupKey: fmt.Sprintf("alert:%s:%s:%s:%d:%s", ref, ev, tag, d, recipient),
			Message:  msg,
		}
	}
	var rows []notify.Delivery
	mailed := make(map[string]struct{})
	for _, d := range o.destinations {
		if d.Type != models.DestinationTypeEmail {
			rows = append(rows, row(d.ID, ""))
			continue
		}
		for _, r := range o.recipients[d.ID] {
			recipient, key := r.Address, r.Address
			if r.UserID != nil {
				recipient = notify.UserRecipient(*r.UserID)
				if key == "" {
					key = recipient
				}
			}
			// An inactive destination's rows fail and show it in history;
			// they do not claim the address from an active destination.
			if d.IsActive {
				if _, seen := mailed[key]; seen {
					continue
				}
				mailed[key] = struct{}{}
			}
			rows = append(rows, row(d.ID, recipient))
		}
	}
	o.enqueued += len(rows)
	return notify.Enqueue(ctx, tx, rows)
}

func testNotification(description string) AlertNotification {
	return AlertNotification{
		AlertName:      "Test Alert",
		Description:    description,
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
	}
}

// TestEmail is the message that tests the SMTP settings.
func TestEmail() notify.MailMessage {
	n := testNotification("This is a test notification to verify your SMTP configuration.")
	return notify.MailMessage{Subject: emailSubject(n), Text: emailBody(n)}
}

// TestDestination sends a test notification to every target of d, active or
// not, now, and returns one result per target.
func (m *Manager) TestDestination(ctx context.Context, d *models.NotificationDestination) []models.DestinationTestResult {
	msg := testNotification("This is a test notification to verify a notification destination.").message(notify.EventTest)
	// Each test opens its own PagerDuty incident, apart from any rule's.
	msg.DedupKey = fmt.Sprintf("logchef:test:%d", time.Now().UnixNano())
	result := func(target string, err error) models.DestinationTestResult {
		r := models.DestinationTestResult{Target: target, OK: err == nil}
		if err != nil {
			r.Error = notify.BoundedError(err)
		}
		return r
	}
	sender := m.senders[d.Type]
	if sender == nil {
		return []models.DestinationTestResult{result(string(d.Type), fmt.Errorf("destination type %q has no sender yet", d.Type))}
	}
	if d.Type != models.DestinationTypeEmail {
		return []models.DestinationTestResult{result(string(d.Type), sender.Send(ctx, d, "", 0, msg))}
	}
	recipients, err := notify.ExpandEmailDestination(ctx, m.db, d)
	if err != nil {
		m.log.Error("failed to resolve email recipients", "destination_id", d.ID, "error", err)
		return []models.DestinationTestResult{result(string(d.Type), errors.New("recipient resolution failed"))}
	}
	out := make([]models.DestinationTestResult, 0, len(recipients))
	for _, r := range recipients {
		recipient, target := r.Address, r.Address
		if r.UserID != nil {
			recipient = notify.UserRecipient(*r.UserID)
			if target == "" {
				target = recipient
			}
		}
		out = append(out, result(target, sender.Send(ctx, d, recipient, 0, msg)))
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
