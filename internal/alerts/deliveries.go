package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// holdRecheck is the longest a held delivery waits before its silences are
// read again, so a silence that is deleted or cut short releases it soon.
const holdRecheck = time.Minute

// discardedError is the last_error of a queued notification that a silent
// closure cancelled.
const discardedError = "not sent: the episode ended while a silence held its notifications back"

// HoldDelivery is the delivery drain's hold function, asked right before a
// row is sent. It holds back an alert row whose group a silence mutes now,
// until the earliest end of those silences or holdRecheck, whichever comes
// first. This covers rows queued before the silence started and their
// retries: the evaluation mutes only the rows it would enqueue. A row of a
// deleted rule is not held.
func (m *Manager) HoldDelivery(ctx context.Context, row *models.NotificationDelivery) (time.Duration, error) {
	if row.Producer != DeliveryProducer {
		return 0, nil
	}
	var msg struct {
		Group map[string]string `json:"group"`
	}
	if err := json.Unmarshal([]byte(row.MessageJSON), &msg); err != nil {
		// The drain fails a row whose message it cannot read, so it is not held.
		m.log.Debug("alert delivery message cannot be read; it is not held", "delivery_id", row.ID, "error", err)
		return 0, nil
	}
	alertID, err := deliveryAlertID(row.ProducerRef)
	if err != nil {
		m.log.Error("alert delivery has an unreadable producer ref; it is not held", "delivery_id", row.ID, "error", err)
		return 0, nil
	}
	alert, err := m.db.GetAlert(ctx, alertID)
	if models.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to load alert %d: %w", alertID, err)
	}
	silences, err := m.db.ListAlertSilences(ctx, alert.SourceID)
	if err != nil {
		return 0, fmt.Errorf("failed to list alert silences: %w", err)
	}
	now := time.Now()
	if ids, until := silencedBy(m.log, alert, silences, msg.Group, now); len(ids) > 0 {
		return min(until.Sub(now), holdRecheck), nil
	}
	return 0, nil
}

// deliveryAlertID reads the alert id of a DeliveryRef.
func deliveryAlertID(ref string) (models.AlertID, error) {
	id, _, ok := strings.Cut(ref, ":")
	if !ok {
		return 0, fmt.Errorf("producer ref %q has no history id", ref)
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("producer ref %q: %w", ref, err)
	}
	return models.AlertID(n), nil
}

// discardsQueued reports whether closing an episode cancels its queued
// notifications: when a silence mutes the closure, or a silence kept the
// episode's current state unannounced. A firing that the drain held back
// during the silence would otherwise go out after it, for an episode that
// is over and whose resolved never comes.
func discardsQueued(announced bool, silencedBy []int64) bool {
	return !announced || len(silencedBy) > 0
}

// discardQueued cancels the pending deliveries of one episode.
func discardQueued(ctx context.Context, tx store.StoreOps, alertID models.AlertID, historyID int64) error {
	if _, err := tx.CancelDeliveries(ctx, DeliveryProducer, DeliveryRef(alertID, historyID), discardedError); err != nil {
		return fmt.Errorf("failed to cancel the queued notifications of episode %d: %w", historyID, err)
	}
	return nil
}

// episodeCloser settles the notifications of episodes closed outside an
// evaluation: a manual resolve or a rule change.
type episodeCloser struct {
	alert    *models.Alert
	silences []*models.AlertSilence
	// mc is nil when the rule does not notify on resolve.
	mc      *messageContext
	out     *outbox
	log     *slog.Logger
	now     time.Time
	message string
	extra   map[string]string
	tag     string
}

// closed settles one episode that the caller closes in tx. An announced
// episode that no silence mutes now gets its resolved notification when the
// rule notifies on resolve. Otherwise its queued notifications are cancelled
// when discardsQueued says so.
func (c *episodeCloser) closed(ctx context.Context, tx store.StoreOps, entry *models.AlertHistoryEntry, announced bool) error {
	silenced, _ := silencedBy(c.log, c.alert, c.silences, entry.Group, c.now)
	if len(silenced) > 0 {
		c.log.Debug("silenced alert resolve", "alert_id", c.alert.ID, "group_key", entry.GroupKey, "silences", silenced)
	}
	if discardsQueued(announced, silenced) {
		if err := discardQueued(ctx, tx, c.alert.ID, entry.ID); err != nil {
			return err
		}
	}
	if !c.alert.NotifyOnResolve || !announced || len(silenced) > 0 {
		return nil
	}
	resolved := *entry
	resolved.Message = c.message
	resolved.ResolvedAt = &c.now
	resolved.Status = models.AlertStatusResolved
	value := float64(0)
	if entry.Value != nil {
		value = *entry.Value
	}
	return c.out.add(ctx, tx, c.mc.resolvedNotification(&resolved, value, c.extra), entry, c.tag)
}

// ruleChangedMessage closes the episodes of a rule whose groups changed.
const ruleChangedMessage = "rule changed"

// CloseForRuleChange closes every open episode of alert in tx with "rule
// changed" and deletes its instances, after an edit that leaves the stored
// group keys naming no group of the result. Each closed group that was
// announced gets its resolved notification, as a manual resolve does, so the
// incident its firing opened closes: unless the rule does not notify on
// resolve or a silence mutes the group now. The rows go to the rule's
// destinations as tx sees them. It returns how many rows it enqueued.
func CloseForRuleChange(ctx context.Context, tx store.StoreOps, log *slog.Logger, alert *models.Alert) (int, error) {
	open, err := tx.ListOpenAlertHistory(ctx, alert.ID)
	if err != nil {
		return 0, err
	}
	if len(open) == 0 {
		// The no_data and keep policies also track ok groups, which have no
		// episode to close.
		return 0, tx.DeleteAlertInstances(ctx, alert.ID)
	}
	instances, err := tx.ListAlertInstances(ctx, alert.ID)
	if err != nil {
		return 0, err
	}
	silences, err := tx.ListAlertSilences(ctx, alert.SourceID)
	if err != nil {
		return 0, fmt.Errorf("failed to list alert silences: %w", err)
	}
	c := &episodeCloser{alert: alert, silences: silences, out: &outbox{alert: alert}, log: log, now: time.Now().UTC(), message: ruleChangedMessage, tag: "rule_changed"}
	if alert.NotifyOnResolve {
		c.mc = newMessageContext(ctx, tx, log, alert)
	}
	// ResolveAlertHistory closes every open row and deletes every instance.
	if err := tx.ResolveAlertHistory(ctx, open[0].ID, ruleChangedMessage); err != nil {
		return 0, err
	}
	for _, entry := range newestPerGroup(open, nil) {
		if err := c.closed(ctx, tx, entry, announcedEpisode(instances, entry)); err != nil {
			return 0, err
		}
	}
	return c.out.enqueued, nil
}
