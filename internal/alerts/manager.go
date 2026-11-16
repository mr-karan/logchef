package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// alertEvaluationTimeout bounds a single alert's evaluation wall-clock time. It
// sits above the per-query timeout (models.DefaultQueryTimeoutSeconds) with
// headroom for result processing, so a query's own timeout normally fires
// first; this deadline is the backstop that frees the job runner's slot when a
// source wedges.
const alertEvaluationTimeout = (models.DefaultQueryTimeoutSeconds + 30) * time.Second

const maxDeliveryAttempts = 3

const deliveryBookkeepingTimeout = 2 * time.Second

const deliveryAttemptTimeout = alertEvaluationTimeout + smtpDialTimeout + deliveryBookkeepingTimeout

// Options encapsulates the dependencies required to run the alerting manager.
type Options struct {
	Config      config.AlertsConfig
	DB          store.Store
	Datasources *datasource.Service
	Logger      *slog.Logger
	Sender      AlertSender
}

// Manager evaluates alert rules, as the handler of the alert.evaluate job, and
// dispatches notifications when thresholds are met.
type Manager struct {
	cfg        config.AlertsConfig
	db         store.Store
	datasource *datasource.Service
	log        *slog.Logger
	sender     AlertSender
}

// NewManager constructs a new alert manager instance.
func NewManager(opts Options) *Manager {
	sender := opts.Sender
	if sender == nil {
		sender = noopSender{}
	}
	return &Manager{
		cfg:        opts.Config,
		db:         opts.DB,
		datasource: opts.Datasources,
		log:        opts.Logger.With("component", "alert_manager"),
		sender:     sender,
	}
}

// RecoverDeliveries finalizes the delivery reservations that a stopped process
// left. Call it once at startup, before the job runner starts evaluations. A
// failure is logged and does not prevent evaluation.
func (m *Manager) RecoverDeliveries(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, alertEvaluationTimeout)
	defer cancel()
	if err := m.db.RecoverAlertDeliveries(ctx); err != nil && !errors.Is(err, context.Canceled) {
		m.log.Error("failed to recover interrupted alert deliveries", "error", err)
	}
}

type noopSender struct{}

func (noopSender) Send(_ context.Context, _ AlertNotification) DeliveryResults {
	return nil
}

// ManualResolve closes the open episode of one group, or of every group when
// groupKey is nil. When the rule notifies on resolve, it sends one resolved
// notification per closed group that was announced and that no silence mutes
// now. It returns models.ErrNotFound when nothing is open.
func (m *Manager) ManualResolve(ctx context.Context, alertID models.AlertID, groupKey *string, message string) error {
	alert, err := m.db.GetAlert(ctx, alertID)
	if err != nil {
		return fmt.Errorf("failed to get alert: %w", err)
	}
	silences, err := m.db.ListAlertSilences(ctx, alert.SourceID)
	if err != nil {
		return fmt.Errorf("failed to list alert silences: %w", err)
	}
	// Close exactly the groups that were read, in one transaction, so every
	// closed group gets its resolved notification. A group that opens after
	// the read stays open.
	var entries []*models.AlertHistoryEntry
	err = m.db.WithTx(ctx, func(tx store.StoreOps) error {
		open, err := tx.ListOpenAlertHistory(ctx, alertID)
		if err != nil {
			return err
		}
		entries = newestPerGroup(open, groupKey)
		if len(entries) == 0 {
			return models.ErrNotFound
		}
		for _, entry := range entries {
			if err := tx.ResolveAlertGroup(ctx, alertID, entry.GroupKey, message); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to resolve alert history: %w", err)
	}

	if !alert.NotifyOnResolve {
		return nil
	}
	mc := m.newMessageContext(ctx, alert)
	now := time.Now().UTC()
	for _, entry := range entries {
		if !announced(entry) {
			continue
		}
		if silenced := m.silencedBy(alert, silences, entry.Group, now); len(silenced) > 0 {
			m.log.Debug("silenced manual resolve", "alert_id", alertID, "group_key", entry.GroupKey, "silences", silenced)
			continue
		}
		entry.Message = message
		entry.ResolvedAt = &now
		entry.Status = models.AlertStatusResolved
		value := float64(0)
		if entry.Value != nil {
			value = *entry.Value
		}
		if sendErr := m.sendResolved(ctx, mc, entry, value, map[string]string{"resolved_by": "manual"}); sendErr != nil {
			m.log.Warn("failed to send manual resolution notifications", "alert_id", alertID, "group_key", entry.GroupKey, "error", sendErr)
		} else {
			m.log.Debug("manual resolution notifications sent", "alert_id", alertID, "group_key", entry.GroupKey)
		}
	}
	return nil
}

// newestPerGroup keeps the newest open row of each group, in the order of
// open (newest first), and only the given group when groupKey is set.
func newestPerGroup(open []*models.AlertHistoryEntry, groupKey *string) []*models.AlertHistoryEntry {
	seen := make(map[string]struct{}, len(open))
	entries := make([]*models.AlertHistoryEntry, 0, len(open))
	for _, entry := range open {
		if groupKey != nil && entry.GroupKey != *groupKey {
			continue
		}
		if _, ok := seen[entry.GroupKey]; ok {
			continue
		}
		seen[entry.GroupKey] = struct{}{}
		entries = append(entries, entry)
	}
	return entries
}
